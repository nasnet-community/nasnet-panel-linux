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
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	featstats "github.com/xtls/xray-core/features/stats"
	"google.golang.org/protobuf/proto"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// bridge is one Xray instance and what watches it.
type bridge struct {
	spec    *bridgespec.Spec
	xray    *core.Instance
	stats   featstats.Manager
	checks  observer          // what the outbounds' check found; nil without outbounds
	refused map[string]string // outbounds left out, by tag, and why
	tracker *tracker
	target  *targetForwarder // nil without a local entry
	entry   *entry           // the local entry's clients; nil without one
	started time.Time

	stopping  atomic.Bool
	closeOnce sync.Once
	done      chan struct{} // closed on close, to stop the refresh loop
}

// newBridge builds the instance for spec. Nothing runs, and nothing listens,
// until start. An outbound Xray won't take is left out, said so on logw, and
// the rest runs without it.
func newBridge(spec *bridgespec.Spec, logw io.Writer) (*bridge, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	b := &bridge{spec: spec, started: time.Now(), done: make(chan struct{})}
	b.refused = refusedOutbounds(spec)
	for _, o := range spec.Outbounds {
		if why, left := b.refused[o.Tag]; left {
			fmt.Fprintf(logw, "remote-nodes-bridge: outbound %s left out: %s\n", o.Tag, why)
		}
	}
	localTarget := ""
	if l := spec.Local; l != nil {
		fwd, err := newTargetForwarder(l.Target, netip.MustParseAddr(l.ListenAddr).Unmap())
		if err != nil {
			return nil, fmt.Errorf("local: the Reality target's forwarder: %w", err)
		}
		b.target, localTarget = fwd, fwd.addr()
	}
	cfg, err := buildConfigWithout(spec, localTarget, b.refused)
	if err != nil {
		b.closeTarget()
		return nil, err
	}
	if adjustConfig != nil {
		adjustConfig(cfg)
	}
	b.tracker = newTracker(spec, time.Now)
	if spec.Local != nil {
		b.entry = newEntry(spec, b.tracker.local)
	}
	useTracker(b.tracker)
	if b.xray, err = core.New(cfg); err != nil {
		useTracker(nil)
		b.closeTarget()
		return nil, fmt.Errorf("xray: %w", err)
	}
	if b.entry != nil {
		if err := b.entry.attach(b.xray); err != nil {
			b.xray.Close()
			useTracker(nil)
			b.closeTarget()
			return nil, fmt.Errorf("local: %w", err)
		}
	}
	b.stats, _ = b.xray.GetFeature(featstats.ManagerType()).(featstats.Manager)
	if len(spec.Outbounds) > 0 {
		b.checks, _ = b.xray.GetFeature(extension.ObservatoryType()).(observer)
	}
	return b, nil
}

// refusedOutbounds tries each of the spec's outbounds on its own, as a probe
// that only builds does, and says why for each one Xray won't take. A stored
// outbound can turn bad: restored from elsewhere, or refused by a newer Xray.
func refusedOutbounds(s *bridgespec.Spec) map[string]string {
	if len(s.Outbounds) == 0 {
		return nil
	}
	from := netip.MustParseAddr(s.OutboundAddr).Unmap()
	var out map[string]string
	for _, o := range s.Outbounds {
		if why := refusal(o, from); why != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[o.Tag] = why
		}
	}
	return out
}

// adjustConfig, when set, changes the built config before the instance is
// made of it. Only a test build sets it (see testroots_bridgetest.go).
var adjustConfig func(*core.Config)

// observer is the part of the core's observatory the status page reads.
type observer interface {
	GetObservation(context.Context) (proto.Message, error)
}

// closeTarget ends the Reality target's forwarder, if there is one.
func (b *bridge) closeTarget() {
	if b.target != nil {
		b.target.close()
	}
}

// start runs the instance: the tunnels begin to dial, and the local entry, if
// there is one, to listen.
func (b *bridge) start() error {
	if b.target != nil {
		// first, so that the entry's very first handshake finds its target
		go b.target.serve()
	}
	if err := b.xray.Start(); err != nil {
		return fmt.Errorf("xray: %w", err)
	}
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				b.tracker.refresh()
			case <-b.done:
				return
			}
		}
	}()
	return nil
}

// close stops the instance. It is safe to call twice.
func (b *bridge) close() error {
	var err error
	b.closeOnce.Do(func() {
		b.stopping.Store(true)
		close(b.done)
		err = b.xray.Close()
		b.closeTarget()
		useTracker(nil)
	})
	return err
}

// status is what the status page says.
func (b *bridge) status() bridgespec.Status {
	b.tracker.refresh()
	st := bridgespec.Status{
		Running: !b.stopping.Load(),
		Started: b.started,
		Xray:    core.Version(),
		Nodes:   make([]bridgespec.NodeStatus, len(b.spec.Nodes)),
	}
	if b.entry != nil {
		st.Local = b.entry.status(b.counter)
	}
	for i, n := range b.spec.Nodes {
		up, since, flows := b.tracker.nodes[i].snapshot()
		st.Nodes[i] = bridgespec.NodeStatus{
			Tag:         n.Tag,
			Up:          up,
			Since:       since,
			BytesUp:     b.counter("user>>>" + n.Tag + ">>>traffic>>>uplink"),
			BytesDown:   b.counter("user>>>" + n.Tag + ">>>traffic>>>downlink"),
			Connections: flows,
		}
	}
	if len(b.spec.Outbounds) > 0 {
		st.Outbounds = b.outbounds()
	}
	return st
}

// outbounds is each outbound as its last check found it, and what it has
// carried. Before the first check comes back it reads as unchecked.
func (b *bridge) outbounds() []bridgespec.OutboundStatus {
	found := map[string]*observatory.OutboundStatus{}
	if b.checks != nil {
		if res, err := b.checks.GetObservation(context.Background()); err == nil {
			if r, ok := res.(*observatory.ObservationResult); ok {
				for _, s := range r.Status {
					found[s.OutboundTag] = s
				}
			}
		}
	}
	out := make([]bridgespec.OutboundStatus, len(b.spec.Outbounds))
	for i, o := range b.spec.Outbounds {
		st := bridgespec.OutboundStatus{
			Tag:       o.Tag,
			BytesUp:   b.counter("outbound>>>" + o.Tag + ">>>traffic>>>uplink"),
			BytesDown: b.counter("outbound>>>" + o.Tag + ">>>traffic>>>downlink"),
		}
		if why, left := b.refused[o.Tag]; left {
			// down for good, as far as this run goes
			st.Checked, st.Error, st.Fallback = true, why, o.Fallback
		} else if c := found[o.Tag]; c != nil {
			st.Checked, st.Alive = true, c.Alive
			if c.Alive {
				st.DelayMS = c.Delay
			} else {
				st.Error = checkReason(c.LastErrorReason, o.Tag)
			}
			st.LastTry = unixTime(c.LastTryTime)
			st.LastSeen = unixTime(c.LastSeenTime)
			st.Fallback = o.Fallback && !c.Alive
		}
		out[i] = st
	}
	return out
}

// checkTimeout is how long the core's check waits for an answer. Xray fixes
// it; the spec can't change it.
const checkTimeout = 5

var (
	checkRe   = regexp.MustCompile(`^the outbound .* is dead: GET request failed:`)
	requestRe = regexp.MustCompile(`\b(Get|Head|Post) "[^"]*": `)
)

// checkReason turns the core's account of a failed check into a sentence an
// admin can read: no tag, no address asked for, no package paths. The core
// keeps the outbound's own error out of it; Test asks again and gets that.
func checkReason(raw, tag string) string {
	s := checkRe.ReplaceAllString(raw, "")
	s = strings.TrimSuffix(s, "with outbound handler report underlying connection failed")
	s = requestRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "the outbound "+tag, "the outbound"), tag, "the outbound")
	if strings.Contains(s, "Client.Timeout exceeded") || strings.Contains(s, "context deadline exceeded") {
		return fmt.Sprintf("No answer within %d seconds.", checkTimeout)
	}
	r := reason(errors.New(s))
	switch r {
	case "the server closed the connection", "the outbound gave up before an answer came":
		return strings.ToUpper(r[:1]) + r[1:] + "."
	}
	return "The check failed: " + r + "."
}

// unixTime reads the observatory's seconds, where zero means never.
func unixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// counter reads one of the core's byte counters. It doesn't exist until the
// first flow runs, which reads as zero.
func (b *bridge) counter(name string) uint64 {
	if b.stats == nil {
		return 0
	}
	c := b.stats.GetCounter(name)
	if c == nil || c.Value() < 0 {
		return 0
	}
	return uint64(c.Value())
}

// handler is the status page and, with a local entry, the hub's way to change
// its clients.
func (b *bridge) handler() http.Handler {
	mux := http.NewServeMux()
	if b.entry != nil {
		mux.HandleFunc("PUT "+bridgespec.ClientsPath, b.entry.serveClients)
	}
	mux.HandleFunc("GET "+bridgespec.StatusPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(b.status())
	})
	return mux
}

// serve answers status requests on l until the server is shut down.
func (b *bridge) serve(l net.Listener) *http.Server {
	srv := &http.Server{
		Handler:           b.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go srv.Serve(l)
	return srv
}
