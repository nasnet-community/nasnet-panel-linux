// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

// The same cycle against the agent's real server: its GetStatsBundle and
// AckBufferedTraffic, over a traffic buffer that is the agent's own file
// store, loaded from a file the way a restarted agent loads it.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/embedded"
)

func bucketAt(hour int, sub1Up, sub1Down int64) *traffic.TrafficBucket {
	return &traffic.TrafficBucket{
		Timestamp:        at(hour),
		UserUplink:       map[string]int64{email(1): sub1Up, nodecfg.BridgeEmail(1): 5 * sub1Up},
		UserDownlink:     map[string]int64{email(1): sub1Down, nodecfg.BridgeEmail(1): 5 * sub1Down},
		InboundUplink:    map[string]int64{},
		InboundDownlink:  map[string]int64{},
		OutboundUplink:   map[string]int64{},
		OutboundDownlink: map[string]int64{},
		TotalUplink:      6 * sub1Up,
		TotalDownlink:    6 * sub1Down,
	}
}

// realAgent is the agent's server with the buckets in its traffic buffer. No
// Xray is started and no background service runs; only the buffer is real.
func realAgent(t *testing.T, buckets ...*traffic.TrafficBucket) *embedded.Client {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "traffic.json")
	raw, err := json.Marshal(struct {
		Version int                      `json:"version"`
		Buckets []*traffic.TrafficBucket `json:"buckets"`
	}{2, buckets})
	must(t, err)
	must(t, os.WriteFile(store, raw, 0o600))

	cfg := config.DefaultConfig()
	cfg.Xray.BinaryPath = filepath.Join(dir, "xray") // never run
	cfg.Xray.ConfigPath = filepath.Join(dir, "config.json")
	cfg.Xray.AccessLogPath = ""
	cfg.Process.ServiceMode = false
	cfg.Process.WatchdogEnabled = false
	cfg.Traffic.BufferEnabled = true
	cfg.Traffic.StorePath = store
	cfg.Cores.Embedded = true
	srv, err := server.NewServer(cfg)
	must(t, err)
	return embedded.New(srv)
}

func TestRealAgentBucketsAreCountedOnceAndDropped(t *testing.T) {
	ag := realAgent(t, bucketAt(12, 10, 100), bucketAt(13, 10, 100), bucketAt(14, 10, 100))
	r := newRig(t)
	r.clk.Set(when(14, 10, 0))
	ctx := context.Background()

	s, err := r.c.Poll(ctx, 1, ag)
	must(t, err)
	// hours 12 and 13 are complete; 14 is the one still growing
	if s.Counted != (Totals{Up: 20, Down: 200}) {
		t.Fatalf("counted %+v, want two hours", s.Counted)
	}
	if got := r.c.Node(1); !got.Live || got.TodayUp != 30 || got.TodayDown != 300 {
		t.Fatalf("node reads %+v", got)
	}
	if got := r.c.Bridge(1); got != (Totals{Up: 100, Down: 1000}) {
		t.Fatalf("bridge = %+v: the two hours' worth of the bridge's client", got)
	}

	must(t, r.flush())
	if got := r.c.Writes(); got != 1 {
		t.Fatalf("writes = %d", got)
	}

	// what the agent holds now: the growing bucket and nothing else
	b, err := ag.GetStatsBundle(ctx, true)
	must(t, err)
	var held []int64
	for _, rec := range b.Buffered.Records {
		held = append(held, rec.Timestamp)
	}
	if !slices.Equal(held, []int64{at(14)}) {
		t.Fatalf("the agent holds %v after the acknowledgement, want only %d", held, at(14))
	}

	// a later poll counts nothing again, and the hour that completes is counted whole
	s, err = r.c.Poll(ctx, 1, ag)
	must(t, err)
	if s.Counted != (Totals{}) {
		t.Fatalf("a second poll counted %+v", s.Counted)
	}
	if got := r.c.Node(1); got.TodayUp != 30 || got.TodayDown != 300 {
		t.Fatalf("node reads %+v", got)
	}
}

// The agent comes back after a crash with the bucket the hub was told to
// drop. The server is real; the file it loads says so.
func TestRealAgentThatComesBackWithADroppedBucket(t *testing.T) {
	r := newRig(t)
	r.clk.Set(when(14, 10, 0))
	ctx := context.Background()
	first := realAgent(t, bucketAt(12, 10, 100), bucketAt(13, 10, 100), bucketAt(14, 10, 100))
	_, err := r.c.Poll(ctx, 1, first)
	must(t, err)
	must(t, r.flush())

	// the agent restarted from a copy of its buffer older than the acknowledgement
	again := realAgent(t, bucketAt(12, 10, 100), bucketAt(13, 10, 100), bucketAt(14, 10, 100))
	s, err := r.c.Poll(ctx, 1, again)
	must(t, err)
	if s.Counted != (Totals{}) {
		t.Fatalf("counted %+v again", s.Counted)
	}
	b, err := again.GetStatsBundle(ctx, true)
	must(t, err)
	var held []int64
	for _, rec := range b.Buffered.Records {
		held = append(held, rec.Timestamp)
	}
	if !slices.Equal(held, []int64{at(14)}) {
		t.Fatalf("the restarted agent holds %v, want only %d: the poll should have told it to drop the rest", held, at(14))
	}
	if got := r.c.Node(1); got.MonthUp != 30 || got.MonthDown != 300 {
		t.Fatalf("node reads %+v", got)
	}
}
