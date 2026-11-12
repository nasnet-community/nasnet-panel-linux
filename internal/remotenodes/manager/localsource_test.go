// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// The router's own entry is asked about under one id by the API and counted
// under it by the usage collector, so the two have to be the same number.
func TestTheRoutersIDIsOne(t *testing.T) {
	if api.RouterID != usage.RouterID {
		t.Fatalf("the API calls the router %d and the usage collector %d", api.RouterID, usage.RouterID)
	}
}

// entryPage is a bridge's word on the entry that a test changes by hand.
type entryPage struct {
	started time.Time
	clients []bridgespec.ClientStatus
	down    bool
}

func (p *entryPage) status() (*bridgespec.LocalStatus, time.Time, bool) {
	if p.down {
		return nil, time.Time{}, false
	}
	return &bridgespec.LocalStatus{Clients: slices.Clone(p.clients)}, p.started, true
}

func (p *entryPage) moved(email string, up, down uint64, conns int, addrs ...string) {
	for i := range p.clients {
		if p.clients[i].Email == email {
			p.clients[i].BytesUp, p.clients[i].BytesDown, p.clients[i].Connections, p.clients[i].Addrs = up, down, conns, addrs
			return
		}
	}
	p.clients = append(p.clients, bridgespec.ClientStatus{Email: email, On: true, BytesUp: up, BytesDown: down, Connections: conns, Addrs: addrs})
}

// totals adds up what a bundle holds for an email, over every bucket.
func totals(b *agent.StatsBundle, email string) (up, down int64) {
	for _, r := range b.Buffered.Records {
		up += r.UserUplink[email]
		down += r.UserDownlink[email]
	}
	return up, down
}

func stamps(b *agent.StatsBundle) []int64 {
	var out []int64
	for _, r := range b.Buffered.Records {
		out = append(out, r.Timestamp)
	}
	slices.Sort(out)
	return out
}

func mustBundle(t *testing.T, s *localSource) *agent.StatsBundle {
	t.Helper()
	b, err := s.GetStatsBundle(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// What the bridge counts since it started becomes what an agent would hold:
// the growth since the last look, in a bucket with a timestamp.
func TestTheEntrysCountsBecomeBuckets(t *testing.T) {
	clk := newClock(epoch)
	page := &entryPage{started: epoch}
	s := newLocalSource(clk.Now, page.status, 0)
	sub := subEmail(1)

	// nothing moved yet: nothing to hold
	if b := mustBundle(t, s); len(b.Buffered.Records) != 0 || s.pending() {
		t.Fatalf("with nothing moved: %+v", b.Buffered)
	}
	page.moved(sub, 100, 1000, 0)
	b := mustBundle(t, s)
	if up, down := totals(b, sub); up != 100 || down != 1000 || len(b.Buffered.Records) != 1 {
		t.Fatalf("after the first bytes: %d up, %d down in %d buckets", up, down, len(b.Buffered.Records))
	}
	first := b.Buffered.Records[0].Timestamp
	if first != epoch.Unix() {
		t.Errorf("the bucket is stamped %d, want when it was begun", first)
	}

	// more in the same hour grows the same bucket; a look with nothing new changes nothing
	clk.Advance(10 * time.Minute)
	page.moved(sub, 150, 1500, 1, "198.51.100.7")
	mustBundle(t, s)
	b = mustBundle(t, s)
	if up, down := totals(b, sub); up != 150 || down != 1500 || !slices.Equal(stamps(b), []int64{first}) {
		t.Fatalf("in the same hour: %d up, %d down, buckets %v", up, down, stamps(b))
	}

	// past the hour a new bucket begins, which makes the first one complete
	clk.Advance(51 * time.Minute)
	page.moved(sub, 160, 1600, 1, "198.51.100.7")
	b = mustBundle(t, s)
	if got := stamps(b); len(got) != 2 || got[0] != first || got[1] <= first {
		t.Fatalf("after an hour the buckets are %v", got)
	}
	if up, down := totals(b, sub); up != 160 || down != 1600 {
		t.Fatalf("over both buckets: %d up, %d down", up, down)
	}

	// the collector wrote the first down: it goes, the growing one stays
	if err := s.AckBufferedTraffic(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	b = mustBundle(t, s)
	if up, _ := totals(b, sub); up != 10 || len(b.Buffered.Records) != 1 {
		t.Fatalf("after the acknowledgement: %d up in %d buckets", up, len(b.Buffered.Records))
	}
	// acknowledging the open one too early must not lose it
	open := b.Buffered.Records[0].Timestamp
	if err := s.AckBufferedTraffic(context.Background(), open); err != nil {
		t.Fatal(err)
	}
	if up, _ := totals(mustBundle(t, s), sub); up != 10 {
		t.Fatal("the bucket still being filled was dropped")
	}
}

// A bridge that started again counts from nothing, and that is not bytes
// taken back: what the new one has moved is all new.
func TestABridgeThatStartedAgainCountsFromNothing(t *testing.T) {
	clk := newClock(epoch)
	page := &entryPage{started: epoch}
	s := newLocalSource(clk.Now, page.status, 0)
	sub := subEmail(1)
	page.moved(sub, 500, 5000, 0)
	mustBundle(t, s)

	page.started = epoch.Add(time.Minute)
	page.moved(sub, 20, 200, 0)
	if up, down := totals(mustBundle(t, s), sub); up != 520 || down != 5200 {
		t.Fatalf("after a restart: %d up, %d down, want what both moved", up, down)
	}
	// and a counter that fell with no restart seen is taken the same way
	page.moved(sub, 5, 50, 0)
	if up, down := totals(mustBundle(t, s), sub); up != 525 || down != 5250 {
		t.Fatalf("after a counter fell: %d up, %d down", up, down)
	}
}

// With no bridge to ask, what was counted before is still owed: the hour is
// closed so the collector takes it, nobody is connected, and once it is taken
// there is nothing to say.
func TestTheEntrysCountsWithNoBridge(t *testing.T) {
	clk := newClock(epoch)
	page := &entryPage{started: epoch, down: true}
	s := newLocalSource(clk.Now, page.status, 0)
	sub := subEmail(1)
	if _, err := s.GetStatsBundle(context.Background(), true); !errors.Is(err, errNoEntry) {
		t.Fatalf("got %v", err)
	}
	page.down = false
	page.moved(sub, 7, 70, 2, "198.51.100.7")
	mustBundle(t, s)

	page.down = true
	b := mustBundle(t, s)
	got := stamps(b)
	if up, _ := totals(b, sub); up != 7 || len(got) != 2 {
		t.Fatalf("what is owed: %d up in buckets %v, want the hour closed", up, got)
	}
	if b.OnlineIPs == nil || len(b.OnlineIPs) != 0 {
		t.Fatalf("with the bridge gone the connected are %v, want nobody", b.OnlineIPs)
	}
	// asked again before it is taken, it says the same
	if again := stamps(mustBundle(t, s)); !slices.Equal(again, got) {
		t.Fatalf("a second look changed the buckets to %v", again)
	}

	// taken: nothing is left to say
	if err := s.AckBufferedTraffic(context.Background(), got[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetStatsBundle(context.Background(), true); !errors.Is(err, errNoEntry) {
		t.Fatalf("after the counts were taken: %v", err)
	}
	if s.pending() {
		t.Error("something is still held")
	}

	// the bridge is back in the same hour: what it moves goes into a later bucket
	page.down, page.started = false, epoch.Add(time.Minute)
	page.clients = nil
	page.moved(sub, 5, 50, 0)
	b = mustBundle(t, s)
	if up, _ := totals(b, sub); up != 5 || stamps(b)[0] <= got[1] {
		t.Fatalf("after it came back: %d up in buckets %v, the old ones were %v", up, stamps(b), got)
	}
}

// Who is connected: an entry for each connection, since the collector counts
// entries, under the address it comes from.
func TestTheEntrysConnections(t *testing.T) {
	clk := newClock(epoch)
	page := &entryPage{started: epoch}
	s := newLocalSource(clk.Now, page.status, 0)
	page.moved(subEmail(1), 1, 1, 3, "198.51.100.7", "203.0.113.9")
	page.moved(subEmail(2), 0, 0, 0)
	b := mustBundle(t, s)
	if got := len(b.OnlineIPs[subEmail(1)]); got != 3 {
		t.Errorf("three connections from two addresses read as %d: %v", got, b.OnlineIPs[subEmail(1)])
	}
	if _, listed := b.OnlineIPs[subEmail(2)]; listed {
		t.Error("a client with nothing open is listed as connected")
	}
	if b.OnlineIPsCollectedAtUnixMs != epoch.UnixMilli() {
		t.Errorf("collected at %d", b.OnlineIPsCollectedAtUnixMs)
	}
	// asked without them, it leaves them out
	if b, _ := s.GetStatsBundle(context.Background(), false); b.OnlineIPs != nil {
		t.Error("the connections were sent though they were not asked for")
	}
}

// At a tidy stop the hour is closed: an empty bucket after it makes the
// collector take it for complete. What is counted afterwards starts another.
func TestSealingClosesTheHour(t *testing.T) {
	clk := newClock(epoch)
	page := &entryPage{started: epoch}
	s := newLocalSource(clk.Now, page.status, 0)
	sub := subEmail(1)
	page.moved(sub, 100, 1000, 0)
	open := stamps(mustBundle(t, s))[0]

	s.seal()
	s.seal() // twice is once
	b := mustBundle(t, s)
	got := stamps(b)
	if len(got) != 2 || got[0] != open || got[1] <= open {
		t.Fatalf("after sealing the buckets are %v", got)
	}
	if up, _ := totals(b, sub); up != 100 {
		t.Fatalf("sealing changed the count to %d", up)
	}

	// the collector takes the sealed one; more bytes go into a later bucket
	if err := s.AckBufferedTraffic(context.Background(), open); err != nil {
		t.Fatal(err)
	}
	page.moved(sub, 130, 1300, 0)
	b = mustBundle(t, s)
	if up, _ := totals(b, sub); up != 30 {
		t.Fatalf("after the sealed hour was taken: %d up", up)
	}
	for _, ts := range stamps(b) {
		if ts <= open {
			t.Fatalf("a bucket is stamped %d, not after the sealed one (%d)", ts, open)
		}
	}
	// sealing with nothing counted does nothing
	fresh := newLocalSource(clk.Now, page.status, 0)
	fresh.seal()
	if fresh.pending() {
		t.Error("sealing an empty source made a bucket")
	}
}

// The whole way to the disk, with the real collector: what the entry's
// clients move is written under the router's own record, is counted once, and
// is there again after a restart. A tidy stop writes the hour it stops in.
func TestTheEntrysCountsReachTheFileAndComeBack(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(epoch)
	open := func() *usage.Collector {
		c, err := usage.Open(usage.Config{Dir: dir, Now: clk.Now, Logf: t.Logf})
		must(t, err)
		return c
	}
	ctx := context.Background()
	page := &entryPage{started: epoch}
	src := newLocalSource(clk.Now, page.status, 0)
	coll := open()
	sub := uint(1)
	lifetime := func(c *usage.Collector) uint64 {
		tot := c.SubscriptionLifetime(sub)
		return tot.Up + tot.Down
	}
	poll := func(c *usage.Collector) {
		t.Helper()
		_, err := c.Poll(ctx, usage.RouterID, src)
		must(t, err)
	}

	page.moved(subEmail(sub), 100, 900, 1, "198.51.100.7")
	poll(coll)
	if got := lifetime(coll); got != 1000 {
		t.Fatalf("after the first poll it has moved %d", got)
	}
	if u := coll.Subscription(sub, usage.RouterID); !u.Live || !u.Online || u.Connections != 1 || u.TodayUp != 100 {
		t.Fatalf("on the router: %+v", u)
	}

	// an hour on: the first hour is complete, written down, and dropped by the source
	clk.Advance(61 * time.Minute)
	page.moved(subEmail(sub), 150, 1350, 1, "198.51.100.7")
	poll(coll)
	must(t, coll.Flush(ctx))
	if got := lifetime(coll); got != 1500 {
		t.Fatalf("after the second hour began it has moved %d", got)
	}
	if got := len(mustBundle(t, src).Buffered.Records); got != 1 {
		t.Fatalf("the source still holds %d buckets after the first was written down", got)
	}
	// looked at again and again, nothing is counted twice
	for range 3 {
		poll(coll)
	}
	if got := lifetime(coll); got != 1500 {
		t.Fatalf("polling again changed the count to %d", got)
	}

	// a tidy stop: the hour is closed and written
	poll(coll)
	src.seal()
	poll(coll)
	must(t, coll.Flush(ctx))

	// another hub, same folder: everything is there, under the router's record
	again := open()
	if got := lifetime(again); got != 1500 {
		t.Fatalf("after a restart it has moved %d, want all 1500", got)
	}
	if u := again.Subscription(sub, usage.RouterID); u.MonthUp != 150 || u.MonthDown != 1350 {
		t.Fatalf("on the router after a restart: %+v", u)
	}

	// and what the new hub's bridge moves is added, not taken for counted
	// already, though the clock stands where the last bucket was begun
	if again.Through(usage.RouterID) == 0 {
		t.Fatal("the restarted collector has no mark for the router")
	}
	src = newLocalSource(clk.Now, page.status, again.Through(usage.RouterID))
	page.started = clk.Now()
	page.clients = nil
	page.moved(subEmail(sub), 10, 90, 0)
	poll(again)
	if got := lifetime(again); got != 1600 {
		t.Fatalf("after the restart's first bytes it has moved %d, want 1600", got)
	}

	// the same with a clock that came back an hour behind
	clk.Set(clk.Now().Add(-time.Hour))
	must(t, again.Flush(ctx))
	third := open()
	src = newLocalSource(clk.Now, page.status, third.Through(usage.RouterID))
	page.started = clk.Now()
	page.clients = nil
	page.moved(subEmail(sub), 1, 9, 0)
	poll(third)
	if got := lifetime(third); got < 1510 {
		t.Fatalf("with the clock an hour back the new bytes were dropped: it has moved %d", got)
	}
}
