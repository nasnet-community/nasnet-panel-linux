// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// sentence checks what the hub shows an admin: one line, a capital first, a
// full stop last, and none of the outbound's secrets.
func sentence(t *testing.T, msg string, o bridgespec.Outbound) {
	t.Helper()
	if msg == "" || strings.ContainsAny(msg, "\r\n") || !unicode.IsUpper([]rune(msg)[0]) || !strings.HasSuffix(msg, ".") {
		t.Errorf("not a sentence: %q", msg)
	}
	for _, secret := range secretsOf(o) {
		if len(secret) >= 3 && strings.Contains(msg, secret) {
			t.Errorf("%q gives away %q", msg, secret)
		}
	}
}

// Building alone tells whether Xray takes the settings, and nothing connects.
func TestProbeBuildsEveryKindWithoutConnecting(t *testing.T) {
	all := testOutbounds()
	for _, name := range slices.Sorted(maps.Keys(all)) {
		t.Run(name, func(t *testing.T) {
			for _, from := range []string{"192.168.150.80", ""} {
				res := runProbe(context.Background(), bridgespec.ProbeRequest{Outbound: all[name], From: from})
				if !res.OK || res.Error != "" || res.Status != 0 || res.DelayMS != 0 {
					t.Errorf("from %q: %+v", from, res)
				}
			}
		})
	}
}

func TestProbeRefusesWhatXrayRefuses(t *testing.T) {
	all := testOutbounds()
	for _, tt := range []struct {
		name string
		base string
		edit func(*bridgespec.Outbound)
		want string
	}{
		{"a fingerprint Xray doesn't know", "trojan", func(o *bridgespec.Outbound) { o.Fingerprint = "netscape" }, "fingerprint"},
		{"no fingerprint at all over reality", "vless over reality", func(o *bridgespec.Outbound) { o.Fingerprint = "unsafe" }, "fingerprint"},
		{"plain vless to a public address", "vless over httpupgrade, plain", func(o *bridgespec.Outbound) { o.Addr = "198.51.100.7" }, "private address"},
		{"plain trojan to a public address", "trojan", func(o *bridgespec.Outbound) { o.Security, o.ServerName = "", "" }, "private address"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := all[tt.base]
			tt.edit(&o)
			for _, connect := range []bool{false, true} {
				res := runProbe(context.Background(), bridgespec.ProbeRequest{Outbound: o, Connect: connect})
				if res.OK || !strings.HasPrefix(res.Error, "Xray does not accept these settings: ") || !strings.Contains(res.Error, tt.want) {
					t.Errorf("connect %v: %+v", connect, res)
				}
				sentence(t, res.Error, o)
			}
		})
	}
}

// A request through the outbound reaches a real server of its kind; any
// status is an answer.
func TestProbeConnects(t *testing.T) {
	ca := newTestCA(t)
	for _, kind := range []string{"vless over reality", "vless over xhttp, plain", "shadowsocks 2022", "socks with a login"} {
		t.Run(kind, func(t *testing.T) {
			end := newFarEnd(t, kind, ca)
			for _, status := range []int{http.StatusNoContent, http.StatusServiceUnavailable} {
				res := runProbe(context.Background(), bridgespec.ProbeRequest{
					Outbound: end.out, From: "127.0.0.1", Connect: true, URL: checkServer(t, status), TimeoutSeconds: 10,
				})
				if !res.OK || res.Status != status || res.DelayMS < 1 || res.Error != "" {
					t.Errorf("asked for %d: %+v", status, res)
				}
			}
			if end.proxy.carried() == 0 {
				t.Error("the server carried nothing")
			}
		})
	}
}

// Nothing turns the check of a TLS server's certificate off: one the system
// doesn't trust is refused.
func TestProbeChecksTheServersCertificate(t *testing.T) {
	end := newFarEnd(t, "trojan over tls", newTestCA(t))
	res := runProbe(context.Background(), bridgespec.ProbeRequest{Outbound: end.out, Connect: true, URL: checkServer(t, http.StatusNoContent), TimeoutSeconds: 10})
	if res.OK || !strings.Contains(res.Error, "certificate") {
		t.Errorf("%+v", res)
	}
	sentence(t, res.Error, end.out)
}

// silentServer takes connections and never says a thing.
func silentServer(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var held []net.Conn
	t.Cleanup(func() {
		l.Close()
		for _, c := range held {
			c.Close()
		}
	})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// When it fails, the result says why in a sentence an admin can read, with no
// id or password in it.
func TestProbeSaysWhatWentWrong(t *testing.T) {
	ca := newTestCA(t)
	check := checkServer(t, http.StatusNoContent)
	socks := newFarEnd(t, "socks with a login", ca)
	vless := newFarEnd(t, "vless over reality", ca)
	trojan := newFarEnd(t, "trojan over ws and tls", ca)

	for _, tt := range []struct {
		name    string
		out     func() bridgespec.Outbound
		timeout int
		want    string
	}{
		{"nothing at the server's port", func() bridgespec.Outbound {
			o := socks.out
			o.Port = uint16(freePort(t))
			return o
		}, 5, "refused"},
		{"a wrong password", func() bridgespec.Outbound {
			o := socks.out
			o.Password = "not-the-password"
			return o
		}, 5, "failed"},
		{"a wrong id", func() bridgespec.Outbound {
			o := vless.out
			o.ID = newUUID()
			return o
		}, 5, ""},
		{"a server the system doesn't trust", func() bridgespec.Outbound { return trojan.out }, 5, "certificate"},
		{"a server that never answers", func() bridgespec.Outbound {
			o := socks.out
			o.Port = uint16(silentServer(t))
			return o
		}, 1, "No answer within 1 second."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := tt.out()
			start := time.Now()
			res := runProbe(context.Background(), bridgespec.ProbeRequest{Outbound: o, Connect: true, URL: check, TimeoutSeconds: tt.timeout})
			if res.OK || res.Status != 0 || res.DelayMS != 0 || !strings.Contains(res.Error, tt.want) {
				t.Errorf("%+v, want an error with %q", res, tt.want)
			}
			sentence(t, res.Error, o)
			if took := time.Since(start); took > time.Duration(tt.timeout)*time.Second+5*time.Second {
				t.Errorf("took %s with a limit of %d s", took, tt.timeout)
			}
		})
	}
}

func TestProbeErrorsKeepSecretsOut(t *testing.T) {
	key := "AQEBAQEBAQEBAQEBAQEBAQ=="
	o := bridgespec.Outbound{Protocol: bridgespec.ProtoShadowsocks, Method: "2022-blake3-aes-128-gcm", Password: key + ":" + key}
	for _, raw := range []string{
		"proxy/shadowsocks_2022: bad key " + key,
		"failed with " + o.Password,
		"user " + outboundID + " rejected",
	} {
		vless := bridgespec.Outbound{ID: outboundID}
		for _, oo := range []bridgespec.Outbound{o, vless} {
			got := scrub(reason(errors.New(raw)), secretsOf(oo))
			for _, s := range secretsOf(oo) {
				if s != "" && strings.Contains(got, s) {
					t.Errorf("%q kept %q", got, s)
				}
			}
		}
	}
}

func TestReason(t *testing.T) {
	for raw, want := range map[string]string{
		"app/proxyman/outbound: failed to process outbound traffic > proxy/socks: failed to find an available destination > " +
			"common/retry: [transport/internet: failed to dial to tcp:127.0.0.1:9 > dial tcp 127.0.0.1:0->127.0.0.1:9: connect: connection refused] > " +
			"common/retry: all retry attempts failed": "dial tcp 127.0.0.1:0->127.0.0.1:9: connect: connection refused",
		"proxy/trojan: failed > tls: failed to verify certificate: x509: certificate signed by unknown authority": "tls: failed to verify certificate: x509: certificate signed by unknown authority",
		"EOF":                   "the server closed the connection",
		"":                      "no reason given",
		"app/dispatcher: done.": "done",
	} {
		if got := reason(errors.New(raw)); got != want {
			t.Errorf("reason(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := reason(fmt.Errorf("wrapped: %w", io.EOF)); got != "the server closed the connection" {
		t.Errorf("a wrapped EOF: %q", got)
	}
}

func TestProbeRefusesUnusableInput(t *testing.T) {
	good := testOutbounds()["trojan"]
	request := func(edit func(map[string]any)) string {
		data, _ := json.Marshal(good)
		var o map[string]any
		json.Unmarshal(data, &o)
		r := map[string]any{"outbound": o}
		if edit != nil {
			edit(r)
		}
		data, _ = json.Marshal(r)
		return string(data)
	}
	for _, tt := range []struct {
		name, in, want string
	}{
		{"nothing", "", "no request"},
		{"not JSON", "hello", "not a probe request"},
		{"a field it doesn't know", request(func(r map[string]any) { r["extra"] = 1 }), "unknown field"},
		{"an outbound that isn't one", `{"outbound":{"protocol":"vless"}}`, "outbound.addr"},
		{"too long a wait", request(func(r map[string]any) { r["timeout_seconds"] = 99 }), "timeout_seconds"},
		{"a name for the server", request(func(r map[string]any) { r["outbound"].(map[string]any)["addr"] = "t.example.com" }), "outbound.addr"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, logw bytes.Buffer
			if code := probe(strings.NewReader(tt.in), &out, &logw); code != 1 {
				t.Errorf("exit code %d, want 1", code)
			}
			if out.Len() != 0 {
				t.Errorf("wrote a result for unusable input: %s", out.String())
			}
			if !strings.Contains(logw.String(), tt.want) {
				t.Errorf("said %q, want it to mention %q", logw.String(), tt.want)
			}
		})
	}

	var out, logw bytes.Buffer
	if code := probe(strings.NewReader(request(nil)), &out, &logw); code != 0 {
		t.Fatalf("exit code %d for a good request: %s", code, logw.String())
	}
	var res bridgespec.ProbeResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil || !res.OK {
		t.Fatalf("result %q: %v", out.String(), err)
	}
}

// The request is read as soon as it is whole: the hub may keep stdin open.
func TestProbeDoesNotWaitForStdinToClose(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	data, _ := json.Marshal(bridgespec.ProbeRequest{Outbound: testOutbounds()["socks"]})
	go w.Write(data)
	done := make(chan int, 1)
	var out bytes.Buffer
	go func() { done <- probe(r, &out, io.Discard) }()
	select {
	case code := <-done:
		if code != 0 || !strings.Contains(out.String(), `"ok":true`) {
			t.Errorf("exit code %d, result %q", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("still waiting for stdin to close")
	}
}

// The program itself: "probe" reads one request, writes one result and exits
// 0, whatever the result; unusable input makes it exit 1 with nothing on stdout.
func TestProcessProbe(t *testing.T) {
	end := newFarEnd(t, "socks with a login", newTestCA(t))
	run := func(in string) (code int, stdout, stderr string) {
		t.Helper()
		cmd := exec.Command(bridgeBinary(t), bridgespec.ProbeArg)
		cmd.Stdin = strings.NewReader(in)
		var o, e bytes.Buffer
		cmd.Stdout, cmd.Stderr = &o, &e
		err := cmd.Run()
		var ee *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &ee):
			code = ee.ExitCode()
		default:
			t.Fatal(err)
		}
		return code, o.String(), e.String()
	}
	encode := func(r bridgespec.ProbeRequest) string {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	check := checkServer(t, http.StatusNoContent)
	for _, tt := range []struct {
		name string
		req  bridgespec.ProbeRequest
		ok   bool
	}{
		{"a request that gets an answer", bridgespec.ProbeRequest{Outbound: end.out, From: "127.0.0.1", Connect: true, URL: check}, true},
		{"one that doesn't", func() bridgespec.ProbeRequest {
			o := end.out
			o.Port = uint16(freePort(t))
			return bridgespec.ProbeRequest{Outbound: o, Connect: true, URL: check, TimeoutSeconds: 5}
		}(), false},
		{"settings Xray refuses", func() bridgespec.ProbeRequest {
			o := testOutbounds()["trojan"]
			o.Fingerprint = "netscape"
			return bridgespec.ProbeRequest{Outbound: o}
		}(), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(encode(tt.req))
			if code != 0 {
				t.Fatalf("exit code %d\n%s", code, stderr)
			}
			if strings.Count(stdout, "\n") != 1 {
				t.Errorf("stdout holds more than the result: %q", stdout)
			}
			var res bridgespec.ProbeResult
			dec := json.NewDecoder(strings.NewReader(stdout))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&res); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			if res.OK != tt.ok || (res.OK && res.Status != http.StatusNoContent) || (!res.OK && res.Error == "") {
				t.Errorf("result %+v", res)
			}
		})
	}

	code, stdout, stderr := run("not a request")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "probe") {
		t.Errorf("unusable input: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
