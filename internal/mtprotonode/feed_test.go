package mtprotonode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/hubnode"
	"github.com/nasnet-community/mtproto-remote-plugin/link"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func kinds(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestTheFeedKeepsTheNewest200NewestFirst(t *testing.T) {
	f := &feed{}
	base := time.Now()
	for i := 0; i < 250; i++ {
		f.add(Event{At: base.Add(time.Duration(i) * time.Second), Kind: "applied", Level: levelInfo, Detail: fmt.Sprint(i)})
	}
	all := f.list(feedCap)
	if len(all) != feedCap || all[0].Detail != "249" || all[feedCap-1].Detail != "50" {
		t.Fatalf("kept %d, newest %q, oldest %q", len(all), all[0].Detail, all[len(all)-1].Detail)
	}
	if top := f.list(3); len(top) != 3 || top[0].Detail != "249" || top[2].Detail != "247" {
		t.Fatalf("top 3 = %+v", top)
	}
	var none *feed
	if got := none.list(10); len(got) != 0 {
		t.Fatalf("a node with no feed listed %d events", len(got))
	}
}

func TestADetailIsClipped(t *testing.T) {
	f := &feed{}
	f.add(Event{Kind: "push_failed", Detail: strings.Repeat("x", 5000)})
	if d := f.list(1)[0].Detail; len(d) > detailMax {
		t.Fatalf("detail is %d bytes", len(d))
	}
}

// A flood folds into one entry with a count, even with another flood interleaved.
func TestAFloodIsOneEntryWithACount(t *testing.T) {
	f := &feed{}
	at := time.Now()
	for i := 0; i < 5; i++ {
		f.flood(Event{At: at, Kind: kindRefused, Level: levelWarn, Count: 10})
		f.flood(Event{At: at, Kind: kindEgressFailed, Level: levelWarn, Count: 3, Detail: fmt.Sprint("timeout ", i)})
		at = at.Add(time.Second)
	}
	got := f.list(10)
	if len(got) != 2 || got[0].Kind != kindEgressFailed || got[0].Count != 15 || got[0].Detail != "timeout 4" || got[1].Count != 50 {
		t.Fatalf("feed = %+v", got)
	}
	// quiet for a while: the next burst is news
	f.flood(Event{At: at.Add(2 * floodMerge), Kind: kindRefused, Level: levelWarn, Count: 1})
	if got := f.list(10); len(got) != 3 || got[0].Count != 1 {
		t.Fatalf("after a pause = %+v", got)
	}
}

func TestLinkTransitionsOnly(t *testing.T) {
	f := &feed{}
	f.linkSeen(true, "")
	if n := len(f.list(10)); n != 0 {
		t.Fatalf("the first look at a working link recorded %d events", n)
	}
	f.linkSeen(true, "")
	f.linkSeen(false, "i/o timeout")
	f.linkSeen(false, "i/o timeout")
	f.linkSeen(true, "")
	got := f.list(10)
	if strings.Join(kinds(got), ",") != "link_up,link_down" || got[1].Level != levelError || got[1].Detail != "i/o timeout" || got[0].Level != levelInfo {
		t.Fatalf("feed = %+v", got)
	}
	down := &feed{}
	down.linkSeen(false, "connection refused")
	if got := down.list(10); len(got) != 1 || got[0].Kind != kindLinkDown {
		t.Fatalf("a link down from the first look = %+v", got)
	}
}

func telemt(running, looping bool, held, exit string) *ctrl.Status {
	var st ctrl.Status
	st.Telemt.Running, st.Telemt.CrashLoop, st.Telemt.Held, st.Telemt.LastExit = running, looping, held, exit
	return &st
}

func TestProxyTransitions(t *testing.T) {
	f := &feed{}
	f.proxySeen(telemt(true, false, "", ""))
	f.proxySeen(telemt(true, false, "", ""))
	f.proxySeen(telemt(false, false, "", "exit status 1"))
	f.proxySeen(telemt(true, false, "", ""))
	// a loop is one entry, however telemt flips inside it
	f.proxySeen(telemt(false, true, "", "signal: killed"))
	f.proxySeen(telemt(true, true, "", "signal: killed"))
	f.proxySeen(telemt(false, true, "", "signal: killed"))
	f.proxySeen(telemt(true, false, "", ""))
	f.proxySeen(telemt(false, false, "no users", ""))
	got := f.list(20)
	want := "proxy_down,proxy_up,proxy_crash_loop,proxy_up,proxy_down"
	if strings.Join(kinds(got), ",") != want {
		t.Fatalf("kinds = %v, want %s", kinds(got), want)
	}
	if got[0].Level != levelInfo || got[0].Detail != "no users" {
		t.Errorf("held on purpose = %+v", got[0])
	}
	if got[2].Level != levelError || got[2].Detail != "signal: killed" {
		t.Errorf("crash loop = %+v", got[2])
	}
	if got[4].Level != levelWarn || got[4].Detail != "exit status 1" {
		t.Errorf("stopped = %+v", got[4])
	}

	held := &feed{}
	held.proxySeen(telemt(false, false, "no users", ""))
	if got := held.list(5); len(got) != 1 || got[0].Kind != kindProxyDown || got[0].Level != levelInfo {
		t.Fatalf("a proxy held from the first look = %+v", got)
	}
}

func TestCoverAppliedAndPushFailures(t *testing.T) {
	f := &feed{}
	f.coverSeen(true, "example.com")
	f.coverSeen(false, "no h2")
	f.coverSeen(false, "no h2")
	f.coverSeen(true, "example.com")
	f.applied("aa", 1)
	f.applied("aa", 1) // the same state again, after a reconnect
	f.pushFailed("i/o timeout")
	f.pushFailed("i/o timeout")
	f.pushFailed("bad_state: port 0")
	f.applied("bb", 3)
	f.pushFailed("i/o timeout") // a new failure after a success is news
	got := f.list(20)
	want := "push_failed,applied,push_failed,push_failed,applied,cover_ok,cover_failed"
	if strings.Join(kinds(got), ",") != want {
		t.Fatalf("kinds = %v, want %s", kinds(got), want)
	}
	if got[1].Detail != "3 users" || got[4].Detail != "1 user" || got[6].Level != levelWarn || got[6].Detail != "no h2" || got[0].Level != levelError {
		t.Fatalf("feed = %+v", got)
	}
}

// statsRig is a manager that is never started, for feeding it snapshots by hand.
func statsRig(t *testing.T) (*Manager, *entry, *fakeNodes) {
	t.Helper()
	nodes := &fakeNodes{node: nodeDomain.Node{ID: 1, Name: "vps-1", IsActive: true}}
	st := newMemStore()
	st.Save(context.Background(), &LiteNode{NodeID: 1, MaxStreams: 1000})
	m := NewManager(Config{Store: st, Nodes: nodes, StatusEvery: 10 * time.Second, StatsEvery: 30 * time.Second})
	e := &entry{id: 1, cancel: func() {}, feed: m.feed(1), started: time.Now()}
	return m, e, nodes
}

func snapAt(at time.Time, to, from uint64, streams int64) hubnode.Snapshot {
	s := hubnode.Snapshot{AgentAt: at, Agent: telemt(true, false, "", "")}
	s.Link.BytesToDC, s.Link.BytesFromDC, s.Link.Streams = to, from, streams
	return s
}

func TestThroughputFromTheLinkCounters(t *testing.T) {
	m, e, _ := statsRig(t)
	ctx := context.Background()
	t0 := time.Now()
	m.onStatus(ctx, e, snapAt(t0, 1000, 5000, 4))
	if _, _, tp := m.measured(e); tp != nil {
		t.Fatalf("one sample gave a rate: %+v", tp)
	}
	m.onStatus(ctx, e, snapAt(t0.Add(2*time.Second), 3000, 15000, 4))
	if _, _, tp := m.measured(e); tp == nil || tp.UpBps != 1000 || tp.DownBps != 5000 {
		t.Fatalf("rate = %+v, want 1000 up / 5000 down", tp)
	}
	// a new link starts its counters again: no negative rate, and no rate until the next sample
	m.onStatus(ctx, e, snapAt(t0.Add(4*time.Second), 100, 200, 4))
	if _, _, tp := m.measured(e); tp != nil {
		t.Fatalf("a counter reset gave %+v", tp)
	}
	m.onStatus(ctx, e, snapAt(t0.Add(5*time.Second), 600, 1200, 4))
	if _, _, tp := m.measured(e); tp == nil || tp.UpBps != 500 || tp.DownBps != 1000 {
		t.Fatalf("after the reset = %+v", tp)
	}
	// status polls that stop leave no stale rate behind
	e.mu.Lock()
	e.rateSeen = time.Now().Add(-time.Hour)
	e.mu.Unlock()
	if _, _, tp := m.measured(e); tp != nil {
		t.Fatalf("an hour-old rate = %+v", tp)
	}
}

func TestActiveUsersAreThePushedOnesThatMovedTraffic(t *testing.T) {
	m, e, _ := statsRig(t)
	ctx := context.Background()
	if users, active, _ := m.measured(e); users != nil || active != nil {
		t.Fatalf("before anything = %v %v", users, active)
	}
	e.mu.Lock()
	e.desired = &ctrl.State{Users: []ctrl.User{{Name: "alice"}, {Name: "bob"}, {Name: "carol"}}}
	e.pushed = map[string]bool{"alice": true, "bob": true, "carol": true}
	e.mu.Unlock()
	err := m.onUsage(ctx, e, hubnode.UsageReport{
		Deltas:     map[string]hubnode.Usage{"alice": {Up: 10}, "bob": {Down: 5}, "ghost": {Up: 99}},
		Cumulative: map[string]hubnode.Usage{"alice": {Up: 10}, "bob": {Down: 5}, "carol": {}, "ghost": {Up: 99}},
	})
	if err != nil {
		t.Fatal(err)
	}
	users, active, _ := m.measured(e)
	if users == nil || *users != 3 || active == nil || *active != 2 {
		t.Fatalf("users=%v active=%v, want 3 and 2", users, active)
	}
	// a quiet poll says nobody is active, not "unknown"
	if err := m.onUsage(ctx, e, hubnode.UsageReport{Deltas: map[string]hubnode.Usage{}, Cumulative: map[string]hubnode.Usage{"alice": {Up: 10}}}); err != nil {
		t.Fatal(err)
	}
	if _, active, _ := m.measured(e); active == nil || *active != 0 {
		t.Fatalf("after a quiet poll active = %v", active)
	}
	// no report for three polls: unknown
	e.mu.Lock()
	e.activeAt = time.Now().Add(-91 * time.Second)
	e.mu.Unlock()
	if _, active, _ := m.measured(e); active != nil {
		t.Fatalf("a stale count was kept: %d", *active)
	}
}

func TestStatsRowsWaitForARateAndComeAtMostEvery10s(t *testing.T) {
	m, e, nodes := statsRig(t)
	ctx := context.Background()
	e.mu.Lock()
	e.pushed = map[string]bool{"alice": true}
	e.mu.Unlock()
	_ = m.onUsage(ctx, e, hubnode.UsageReport{Deltas: map[string]hubnode.Usage{"alice": {Up: 1}}, Cumulative: map[string]hubnode.Usage{"alice": {Up: 1}}})
	t0 := time.Now()
	for i := 0; i <= 25; i++ {
		// counters restart at 13 s: no row until a rate is known again
		to := uint64(i) * 1000
		if i >= 13 {
			to = uint64(i-13) * 1000
		}
		m.onStatus(ctx, e, snapAt(t0.Add(time.Duration(i)*time.Second), to, to*2, 7))
	}
	nodes.mu.Lock()
	defer nodes.mu.Unlock()
	var at []int
	for _, s := range nodes.stats {
		at = append(at, int(s.CreatedAt.Sub(t0)/time.Second))
	}
	if fmt.Sprint(at) != "[1 11 21]" {
		t.Fatalf("rows at %v s, want [1 11 21]", at)
	}
	s := nodes.stats[0]
	if s.NodeID != 1 || s.UpSpeed != 1000 || s.DownSpeed != 2000 || s.TcpCount != 7 || s.OnlineUsers != 1 || s.CPU != 0 || s.CreatedAt.Location() != time.UTC {
		t.Fatalf("row = %+v", s)
	}
}

// One manager driving a real agent: the feed sees the link, the proxy and a push, once each.
func TestTheFeedFollowsALiveNode(t *testing.T) {
	e := newEnv(t)
	var down atomic.Bool
	down.Store(true)
	cut := &cutDialer{}
	dial := func(ctx context.Context, n, a string) (net.Conn, error) {
		if down.Load() {
			return nil, errors.New("network is unreachable")
		}
		return cut.dial(ctx, n, a)
	}
	mg := e.managerWith(t, func(c *Config) {
		c.Dialers = Dialers{Link: func(*WAN) link.DialFunc { return dial }, DC: plainDialers().DC}
	})
	feedOf := func() []Event { return mg.m.Events(1, feedCap).Events }
	count := func(kind, detail string) int {
		n := 0
		for _, ev := range feedOf() {
			if ev.Kind == kind && strings.Contains(ev.Detail, detail) {
				n++
			}
		}
		return n
	}
	waitFor(t, func() bool { return count(kindLinkDown, "unreachable") == 1 }, "link_down while the link can't connect")
	down.Store(false)
	waitFor(t, func() bool { return count(kindLinkUp, "") == 1 }, "link_up")

	// a restart that comes straight back is not an outage
	if err := mg.m.Ensure(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }, "the link after the restart")
	time.Sleep(500 * time.Millisecond)
	if count(kindLinkDown, "") != 1 || count(kindLinkUp, "") != 1 {
		t.Fatalf("a quick restart was recorded: %v", kinds(feedOf()))
	}

	e.agent.set(func(a *mtprotoAgent) { a.running = false })
	waitFor(t, func() bool { return count(kindProxyDown, "") == 1 }, "proxy_down")
	e.agent.set(func(a *mtprotoAgent) { a.running = true })
	waitFor(t, func() bool { return count(kindProxyUp, "") == 1 }, "proxy_up")

	mg.push(t, "alice")
	waitFor(t, func() bool { return count(kindApplied, "1 user") == 1 }, "the push to be recorded")

	down.Store(true)
	cut.cut()
	waitFor(t, func() bool { return count(kindLinkDown, "") == 2 }, "link_down after a drop")
	if f := mg.m.Events(1, 2); len(f.Events) != 2 || f.Events[0].Kind != kindLinkDown || f.Since.IsZero() {
		t.Fatalf("feed = %+v", f)
	}
}

// The feed lives on the manager: a restart keeps it, a removal drops it.
func TestTheFeedOutlivesAnEntryButNotTheNode(t *testing.T) {
	e := newEnv(t)
	mg := e.managerWith(t, func(c *Config) { c.Tick = time.Hour })
	waitFor(t, func() bool { return mg.m.IsRunning(1) }, "start")
	mg.m.Record(1, Event{Kind: kindJobOK, Level: levelInfo, Subject: "rotate"})
	if err := mg.m.Ensure(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, ev := range mg.m.Events(1, 10).Events {
		kept = kept || ev.Subject == "rotate"
	}
	if !kept {
		t.Fatalf("the restart lost the feed: %+v", mg.m.Events(1, 10).Events)
	}
	mg.m.Remove(1)
	if got := mg.m.Events(1, 10).Events; len(got) != 0 {
		t.Fatalf("a removed node kept %d events", len(got))
	}
	// nothing is recorded for a node the panel does not have
	mg.m.Record(99, Event{Kind: kindJobFailed, Level: levelError})
	if got := mg.m.Events(99, 10).Events; len(got) != 0 {
		t.Fatalf("an unknown node got %+v", got)
	}
}

// The cover check and refusals reach the feed through the hub's events.
func TestHubEventsReachTheFeed(t *testing.T) {
	m, e, _ := statsRig(t)
	at := time.Now()
	m.onEvent(e, hubnode.Event{Kind: hubnode.EventDisguiseUp, Detail: "example.com", At: at})
	m.onEvent(e, hubnode.Event{Kind: hubnode.EventDisguiseDown, Detail: "example.com", Err: errors.New("no h2"), At: at})
	m.onEvent(e, hubnode.Event{Kind: hubnode.EventPushFailed, Detail: "abc", Err: errors.New("i/o timeout"), At: at})
	for i := 0; i < 50; i++ {
		m.onEvent(e, hubnode.Event{Kind: hubnode.EventViolation, Detail: "198.51.100.1:443", At: time.Now()})
		m.onEvent(e, hubnode.Event{Kind: hubnode.EventDialFailure, Detail: "149.154.167.51:443", Err: errors.New("i/o timeout"), At: time.Now()})
	}
	waitFor(t, func() bool {
		var refused, egress int
		for _, ev := range e.feed.list(feedCap) {
			switch ev.Kind {
			case kindRefused:
				refused += ev.Count
			case kindEgressFailed:
				egress += ev.Count
			}
		}
		return refused == 50 && egress == 50
	}, "every refusal and failed dial to be counted")
	got := e.feed.list(feedCap)
	if strings.Join(kinds(got), ",") != "egress_failed,refused,push_failed,cover_failed" && strings.Join(kinds(got), ",") != "refused,egress_failed,push_failed,cover_failed" {
		t.Fatalf("kinds = %v", kinds(got))
	}
	for _, ev := range got {
		switch ev.Kind {
		case kindEgressFailed:
			if ev.Detail != "149.154.167.51:443: i/o timeout" || ev.Level != levelWarn {
				t.Errorf("egress = %+v", ev)
			}
		case kindRefused:
			if ev.Detail != "" {
				t.Errorf("a refusal names its destination: %+v", ev)
			}
		case kindCoverFailed:
			if ev.Detail != "no h2" {
				t.Errorf("cover = %+v", ev)
			}
		case kindPushFailed:
			if ev.Detail != "i/o timeout" || ev.Level != levelError {
				t.Errorf("push = %+v", ev)
			}
		}
	}
}
