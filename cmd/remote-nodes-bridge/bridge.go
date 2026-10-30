// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtls/xray-core/core"
	featstats "github.com/xtls/xray-core/features/stats"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// bridge is one Xray instance and what watches it.
type bridge struct {
	spec    *bridgespec.Spec
	xray    *core.Instance
	stats   featstats.Manager
	tracker *tracker
	started time.Time

	stopping  atomic.Bool
	closeOnce sync.Once
	done      chan struct{} // closed on close, to stop the refresh loop
}

// newBridge builds the instance for spec. Nothing runs, and nothing listens,
// until start.
func newBridge(spec *bridgespec.Spec) (*bridge, error) {
	cfg, err := buildConfig(spec)
	if err != nil {
		return nil, err
	}
	b := &bridge{spec: spec, tracker: newTracker(spec, time.Now), started: time.Now(), done: make(chan struct{})}
	useTracker(b.tracker)
	if b.xray, err = core.New(cfg); err != nil {
		useTracker(nil)
		return nil, fmt.Errorf("xray: %w", err)
	}
	b.stats, _ = b.xray.GetFeature(featstats.ManagerType()).(featstats.Manager)
	return b, nil
}

// start runs the instance: the tunnels begin to dial.
func (b *bridge) start() error {
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
	return st
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

// handler is the status page.
func (b *bridge) handler() http.Handler {
	mux := http.NewServeMux()
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
