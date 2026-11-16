// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/dispatcher"
	xlog "github.com/xtls/xray-core/app/log"
	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/utils"
	"github.com/xtls/xray-core/core"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// probe is the program's other mode: it reads one ProbeRequest from in, tries
// the outbound and writes one ProbeResult to out. It says 0 whenever it wrote
// a result, whatever the result says, and 1 when the request was unusable.
// Nothing listens, and nothing is left running.
func probe(in io.Reader, out, logw io.Writer) int {
	req, err := readProbe(in)
	if err != nil {
		fmt.Fprintln(logw, "remote-nodes-bridge probe:", err)
		return 1
	}
	res := runProbe(context.Background(), req)
	if err := json.NewEncoder(out).Encode(res); err != nil {
		fmt.Fprintln(logw, "remote-nodes-bridge probe:", err)
		return 1
	}
	return 0
}

// readProbe takes the first JSON value on in, without waiting for in to close.
func readProbe(in io.Reader) (bridgespec.ProbeRequest, error) {
	var req bridgespec.ProbeRequest
	dec := json.NewDecoder(io.LimitReader(in, bridgespec.MaxSize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, errors.New("no request on stdin")
		}
		return req, fmt.Errorf("the request is not a probe request: %w", err)
	}
	if err := req.Validate(); err != nil {
		return req, err
	}
	return req, nil
}

// runProbe builds an instance holding nothing but the outbound and, if the
// request asks, sends one GET through it.
func runProbe(ctx context.Context, req bridgespec.ProbeRequest) bridgespec.ProbeResult {
	secrets := secretsOf(req.Outbound)
	fail := func(format string, args ...any) bridgespec.ProbeResult {
		return bridgespec.ProbeResult{Error: scrub(fmt.Sprintf(format, args...), secrets)}
	}

	var from netip.Addr
	if req.From != "" {
		from = netip.MustParseAddr(req.From).Unmap()
	}
	inst, refused := outboundInstance(req.Outbound, from)
	if refused != "" {
		return bridgespec.ProbeResult{Error: refused}
	}
	defer inst.Close()
	if !req.Connect {
		return bridgespec.ProbeResult{OK: true}
	}
	if err := inst.Start(); err != nil {
		return fail("The outbound did not start: %s.", reason(err))
	}

	seconds := req.TimeoutOrDefault()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	// The core reports why an outbound gave up here, not on the connection.
	why := &firstError{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(session.TrackedConnectionError(ctx, why), inst, dest)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URLOrDefault(), nil)
	if err != nil {
		return fail("The address to ask for is not usable: %s.", err)
	}
	// the headers the bridge's own check sends
	utils.TryDefaultHeadersWith(get.Header, "nav")

	start := time.Now()
	resp, err := client.Do(get)
	if err != nil {
		if ctx.Err() != nil {
			return fail("No answer within %d second%s.", seconds, plural(seconds))
		}
		if cause := why.get(); cause != nil {
			err = cause
		}
		return fail("The request through the outbound failed: %s.", reason(err))
	}
	resp.Body.Close()
	// any answer is one, whatever its status; under a millisecond still counts
	return bridgespec.ProbeResult{OK: true, DelayMS: max(time.Since(start).Milliseconds(), 1), Status: resp.StatusCode}
}

// outboundInstance is an instance of the outbound alone, not started, or why
// Xray won't make one: a sentence for an admin, with no secret in it.
func outboundInstance(o bridgespec.Outbound, from netip.Addr) (*core.Instance, string) {
	refused := func(why string) string {
		return scrub(fmt.Sprintf("Xray does not accept these settings: %s.", why), secretsOf(o))
	}
	cfg, err := probeConfig(o, from)
	if err != nil {
		return nil, refused(err.Error())
	}
	if adjustConfig != nil {
		adjustConfig(cfg)
	}
	inst, err := core.New(cfg)
	if err != nil {
		return nil, refused(reason(err))
	}
	return inst, ""
}

// refusal says why Xray won't take an outbound, or "" if it does. Nothing is
// started: the instance is made and closed.
func refusal(o bridgespec.Outbound, from netip.Addr) string {
	inst, refused := outboundInstance(o, from)
	if inst != nil {
		inst.Close()
	}
	return refused
}

// probeConfig is the outbound alone, with the apps it needs to be dialed
// through. The core fills in the rest with its defaults. No log: the result
// is what goes to stdout, and Xray's console log would go there too.
func probeConfig(o bridgespec.Outbound, from netip.Addr) (*core.Config, error) {
	h, err := outboundHandler(o, from)
	if err != nil {
		return nil, err
	}
	return &core.Config{
		Outbound: []*core.OutboundHandlerConfig{h},
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(&xlog.Config{ErrorLogType: xlog.LogType_None, AccessLogType: xlog.LogType_None}),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
		},
	}, nil
}

// firstError keeps the first reason the core gives for a failed connection.
type firstError struct {
	mu  sync.Mutex
	err error
}

func (f *firstError) SubmitError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.err = err
	}
}

func (f *firstError) get() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// callerRe is the package path the core puts in front of its messages.
var callerRe = regexp.MustCompile(`^[a-z0-9_]+(/[a-z0-9_]+)+: `)

// reason is the end of a chain of core errors, where it says what happened:
// "connection refused", not the layers it passed on the way up.
func reason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "the server closed the connection"
	}
	parts := strings.Split(err.Error(), " > ")
	for i := len(parts) - 1; i >= 0; i-- {
		p := callerRe.ReplaceAllString(strings.Trim(parts[i], " []"), "")
		switch p {
		case "", "all retry attempts failed":
			continue
		case "EOF", "unexpected EOF":
			return "the server closed the connection"
		case "io: read/write on closed pipe":
			// the outbound dropped the request when it failed; why is told elsewhere
			return "the outbound gave up before an answer came"
		}
		if len(p) > 200 {
			p = p[:200] + "..."
		}
		return strings.TrimSuffix(p, ".")
	}
	return "no reason given"
}

// secretsOf is what must never show in an error: the id and the password, and
// each key of a Shadowsocks 2022 password on its own.
func secretsOf(o bridgespec.Outbound) []string {
	out := []string{o.ID, o.Password}
	if bridgespec.Shadowsocks2022(o.Method) {
		out = append(out, strings.Split(o.Password, ":")...)
	}
	return out
}

// scrub blanks every secret in s. One shorter than 3 characters would match
// all over a sentence, and could not be told apart there anyway.
func scrub(s string, secrets []string) string {
	for _, v := range secrets {
		if len(v) >= 3 {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
