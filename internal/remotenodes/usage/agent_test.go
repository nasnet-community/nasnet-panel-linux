// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// Every client the hub talks to a node with can be handed to the collector.
var (
	_ Source = agent.NodeClient(nil)
	_ Source = (*agent.Client)(nil)
	_ Source = (*agent.ReverseClient)(nil)
)

var errBoom = errors.New("rpc error: code = Unavailable desc = connection error")

// agentBuffer stands in for a node's agent. Its traffic buffer is the agent's
// own, with the agent's default hour-wide buckets: it grows the newest bucket
// as traffic comes in, and an acknowledgement drops whole buckets, the
// newest included. The calls hand the buffer over the way the agent's server
// does, through the same conversion the hub's client uses.
type agentBuffer struct {
	mu    sync.Mutex
	store *traffic.MemoryStore

	online map[string]map[string]int64

	// what the test can make go wrong
	failGet     error   // GetStatsBundle fails with this
	failAcks    []error // each acknowledgement takes one; nil means it works
	landsAnyway bool    // a failed acknowledgement is dropped by the agent all the same
	noBuffered  bool    // the bundle has no buffered traffic in it, as when the agent's collector failed
	noOnline    bool    // ... and no online addresses

	acks     []int64 // every acknowledgement that arrived, landed or not, in order
	landed   []int64 // the ones that dropped buckets
	bundles  int
	wantedIP []bool // includeOnlineIPs of each call

	// slow makes GetStatsBundle take a while, and inflight and most count how
	// many were in it at once
	slow     time.Duration
	inflight int
	most     int
}

func newAgentBuffer() *agentBuffer {
	return &agentBuffer{store: traffic.NewMemoryStore(time.Hour, 7*24*time.Hour), online: map[string]map[string]int64{}}
}

// add is traffic Xray counted at the time now, which the agent adds to the
// bucket of that hour.
func (a *agentBuffer) add(now time.Time, email string, up, down int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store.Accumulate(&traffic.XrayStatsSnapshot{
		UserUplink: map[string]int64{email: up}, UserDownlink: map[string]int64{email: down},
		TotalUplink: up, TotalDownlink: down,
	}, now)
}

// wipe is an agent that lost its buffer.
func (a *agentBuffer) wipe() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store = traffic.NewMemoryStore(time.Hour, 7*24*time.Hour)
}

func (a *agentBuffer) setOnline(email string, addrs ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := map[string]int64{}
	for i, ip := range addrs {
		m[ip] = int64(1000 + i)
	}
	a.online[email] = m
}

func (a *agentBuffer) GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*agent.StatsBundle, error) {
	a.mu.Lock()
	a.inflight++
	a.most = max(a.most, a.inflight)
	slow := a.slow
	a.mu.Unlock()
	if slow > 0 {
		time.Sleep(slow)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inflight--
	a.bundles++
	a.wantedIP = append(a.wantedIP, includeOnlineIPs)
	if a.failGet != nil {
		return nil, a.failGet
	}
	b := &agent.StatsBundle{}
	if !a.noBuffered {
		// what Server.GetBufferedTraffic does, and then the hub client's conversion
		resp := &pb.BufferedTrafficStats{}
		for _, bk := range a.store.GetAll() {
			resp.Records = append(resp.Records, &pb.TrafficRecord{
				Timestamp: bk.Timestamp, UserUplink: bk.UserUplink, UserDownlink: bk.UserDownlink,
				InboundUplink: bk.InboundUplink, InboundDownlink: bk.InboundDownlink,
				OutboundUplink: bk.OutboundUplink, OutboundDownlink: bk.OutboundDownlink,
				TotalUplink: bk.TotalUplink, TotalDownlink: bk.TotalDownlink,
			})
			if resp.BufferStartTime == 0 || bk.Timestamp < resp.BufferStartTime {
				resp.BufferStartTime = bk.Timestamp
			}
			resp.BufferEndTime = max(resp.BufferEndTime, bk.Timestamp)
		}
		b.Buffered = agent.BufferedTrafficFromPB(resp)
	}
	if includeOnlineIPs && !a.noOnline {
		resp := &pb.AllOnlineIPsResponse{Users: map[string]*pb.OnlineIPMap{}}
		for email, ips := range a.online {
			resp.Users[email] = &pb.OnlineIPMap{Ips: ips}
		}
		b.OnlineIPs = agent.OnlineIPsFromPB(resp)
	}
	return b, nil
}

func (a *agentBuffer) AckBufferedTraffic(ctx context.Context, through int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acks = append(a.acks, through)
	var err error
	if len(a.failAcks) > 0 {
		err, a.failAcks = a.failAcks[0], a.failAcks[1:]
	}
	if err == nil || a.landsAnyway {
		a.store.Drain(through)
		a.landed = append(a.landed, through)
	}
	return err
}

// held is the timestamps of the buckets the agent still has.
func (a *agentBuffer) held() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []int64
	for _, b := range a.store.GetAll() {
		out = append(out, b.Timestamp)
	}
	return out
}

// peak is the most polls that were in GetStatsBundle at once.
func (a *agentBuffer) peak() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.most
}

func (a *agentBuffer) acked() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]int64(nil), a.acks...)
}

// rig is a collector on a folder, a clock and a few agents.
type rig struct {
	t    *testing.T
	dir  string
	clk  *clock
	logs *logs
	c    *Collector
	ag   map[uint]*agentBuffer
	cfg  Config
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, dir: t.TempDir(), clk: newClock(t0), logs: &logs{}, ag: map[uint]*agentBuffer{}}
	r.cfg = Config{Dir: r.dir, Now: r.clk.Now, Logf: r.logs.Logf}
	r.reopen()
	return r
}

// reopen is the hub starting again on the same folder.
func (r *rig) reopen() {
	r.t.Helper()
	c, err := Open(r.cfg)
	must(r.t, err)
	r.c = c
}

func (r *rig) agent(id uint) *agentBuffer {
	if r.ag[id] == nil {
		r.ag[id] = newAgentBuffer()
	}
	return r.ag[id]
}

func (r *rig) poll(id uint) Sample {
	r.t.Helper()
	s, err := r.c.Poll(context.Background(), id, r.agent(id))
	must(r.t, err)
	return s
}

func (r *rig) flush() error { return r.c.Flush(context.Background()) }

// pollEvery steps the clock by d and polls, n times, running before(i) first
// each time to put traffic in the agent.
func (r *rig) pollEvery(id uint, d time.Duration, n int, before func(i int)) []Sample {
	r.t.Helper()
	var out []Sample
	for i := 0; i < n; i++ {
		r.clk.Advance(d)
		if before != nil {
			before(i)
		}
		out = append(out, r.poll(id))
	}
	return out
}

func email(sub int) string { return nodecfg.SubscriptionEmail(sub) }

// when is a time on the 30th.
func when(h, m, s int) time.Time { return time.Date(2026, 10, 30, h, m, s, 0, time.UTC) }
