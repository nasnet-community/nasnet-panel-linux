// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

const (
	// rateWindow is how far back a node's rate looks. The agent adds to its
	// buffer every five seconds whatever the poll does, so one poll can see
	// two of its steps and the next none; a few polls' worth smooths that out.
	rateWindow = 15 * time.Second

	// onlineMaxAge is how long the last list of who is online stands when the
	// agent can't make a new one. Its own sweep is good for half a minute, so
	// a list older than this is not about now.
	onlineMaxAge = time.Minute

	// maxAhead is how far past the hub's clock a bucket may be dated before it
	// is taken for the work of a wrong clock. Counting one would put the mark
	// in the future, and every bucket after it would look already counted.
	maxAhead = 24 * time.Hour
)

// Source is the part of a node's agent the collector uses. The hub's
// *agent.Client fits, and so do the reverse and embedded clients.
type Source interface {
	GetStatsBundle(ctx context.Context, includeOnlineIPs bool) (*agent.StatsBundle, error)
	AckBufferedTraffic(ctx context.Context, throughTimestamp int64) error
}

// Sample is what one poll found.
type Sample struct {
	At time.Time // when the node answered
	// The node's rate in bytes per second, as NodeUsage has it. It is zero on
	// the first poll of a run: there is nothing yet to take a difference of.
	RateUp, RateDown uint64
	Online           int // as NodeUsage has it
	Connections      int
	// Counted is what this poll added to the history: the subscriptions' bytes
	// in the buckets that completed since the last one. The newest bucket is
	// still growing and is not in it.
	Counted Totals
}

// partial is the newest bucket as the last poll found it. It is still
// growing, so it is shown and not counted.
type partial struct {
	ts   int64
	b    bucket
	node Totals // all the subscriptions' bytes in it, kept apart from b so forgetting a subscription leaves the node's total alone
}

// observation is how many bytes appeared between two polls.
type observation struct {
	from, to time.Time
	inc      Totals
}

// record is one of the agent's buckets, read for what counts.
type record struct {
	ts   int64
	b    bucket
	node Totals
}

// Poll asks a node's agent for its stats and takes them in: each bucket of the
// agent's buffer that is complete and not yet counted is added to the history,
// the newest is shown as it stands, and the rate, the chart and who is online
// are updated. It keeps src for Flush to acknowledge through. Polls of
// different nodes may overlap; two of one node run in turn.
//
// Whose bytes are whose goes by the email alone: those nodecfg.ParseSubscriptionEmail
// accepts are subscriptions', this node's BridgeEmail goes to the bridge
// totals, and anything else is not ours and is dropped.
//
// A subscription is online when the agent lists at least one client address
// for it; an email with an empty list is Xray's counter lingering after a
// disconnect. Connections are those addresses added up. Xray tracks addresses,
// not sockets, so two devices behind one address are one connection and a
// phone on wifi and on mobile data is two. If the agent can't say who is
// online, the last answer stands for a minute.
//
// A failed poll is counted. After Config.MissedPolls in a row the node is not
// live: its rates, Online and Connections read zero and the history stays. The
// first poll that answers again makes it live and starts its rate afresh.
//
// Buckets that are counted already but still at the agent are not counted
// again. Those the file holds are dropped and the agent is told here, which is
// how a hub that crashed between the write and the acknowledgement settles
// it on its own.
func (c *Collector) Poll(ctx context.Context, nodeID uint, src Source) (Sample, error) {
	c.mu.Lock()
	n := c.nodeLocked(nodeID)
	n.src = src
	c.mu.Unlock()

	// two polls of a node at once would take in the same buckets side by side
	n.pollMu.Lock()
	defer n.pollMu.Unlock()

	bundle, err := src.GetStatsBundle(ctx, true)
	now := c.now()
	if err != nil {
		c.mu.Lock()
		c.missLocked(n)
		c.mu.Unlock()
		return Sample{}, fmt.Errorf("usage: node %d: %w", nodeID, err)
	}
	if bundle == nil {
		bundle = &agent.StatsBundle{}
	}

	c.mu.Lock()
	if c.nodes[nodeID] != n {
		// the node was forgotten while its agent was answering
		c.mu.Unlock()
		return Sample{}, nil
	}
	s, ackThrough := c.foldLocked(n, nodeID, bundle, now)
	c.mu.Unlock()
	if ackThrough > 0 {
		// not the poll's failure: it is only said, once while it lasts, and the next flush tries again
		err := c.ackNode(ctx, nodeID, n, src, ackThrough)
		c.mu.Lock()
		warn := err != nil && !n.ackFailing
		n.ackFailing = err != nil
		c.mu.Unlock()
		if warn {
			c.logf("%v; the next flush tries again", err)
		}
	}
	return s, nil
}

// missLocked counts a poll that got no answer.
func (c *Collector) missLocked(n *node) {
	if n.misses < math.MaxInt32 {
		n.misses++
	}
	if n.misses >= c.cfg.MissedPolls {
		n.live, n.online = false, nil
		n.rateUp, n.rateDown, n.obs, n.havePrev = 0, 0, nil, false
	}
}

// foldLocked takes in a bundle. It returns the sample, and the mark to
// acknowledge at once if the agent was found holding buckets the file counts
// already; zero otherwise.
func (c *Collector) foldLocked(n *node, nodeID uint, b *agent.StatsBundle, now time.Time) (Sample, int64) {
	n.misses, n.live, n.updatedAt = 0, true, now

	var counted Totals
	var ack int64
	added := false
	if b.Buffered != nil {
		recs, ahead := records(b.Buffered.Records, nodeID, now.Add(maxAhead).Unix())
		switch {
		case ahead > 0 && !n.warnedAhead:
			n.warnedAhead = true
			c.logf("usage: node %d: %d buffered record(s) are dated more than a day ahead of this clock; they are skipped and left with the agent, and one of the two clocks is wrong", nodeID, ahead)
		case ahead == 0:
			n.warnedAhead = false
		}

		var newest int64
		if len(recs) > 0 {
			newest = recs[len(recs)-1].ts
		}
		var fresh Totals // what the page can see now and couldn't at the last poll
		var live *partial
		stale := false
		for _, r := range recs {
			switch {
			case r.ts <= n.hist.through:
				// counted already, and the agent still has it
				stale = stale || r.ts <= n.diskThrough
			case r.ts < newest:
				// a newer bucket exists, so nothing is added to this one any more
				n.hist.add(r.ts, r.b)
				n.hist.through = r.ts
				counted, fresh = counted.plus(r.node), fresh.plus(r.node)
			default:
				live = &partial{ts: r.ts, b: r.b, node: r.node}
				fresh = fresh.plus(r.node)
			}
		}
		if stale {
			n.staleSeen = true
			if !c.noWrite {
				ack = n.diskThrough
			}
		}

		// The newest bucket was already on the page as far as the last poll saw
		// it, whether it has completed since or grown; the difference is new.
		var shown Totals
		if n.partial != nil {
			shown = n.partial.node
		}
		inc := fresh.minus(shown)
		n.partial = live
		switch {
		case !n.havePrev:
			n.havePrev, n.lastData = true, now
		case now.After(n.lastData):
			n.obs = append(n.obs, observation{from: n.lastData, to: now, inc: inc})
			n.lastData, added = now, true
		}
	}
	c.rateLocked(n, now)
	if added {
		n.ring.put(now, n.rateUp, n.rateDown)
	}
	if b.OnlineIPs != nil {
		onlineLocked(n, b.OnlineIPs, now)
	} else if n.online != nil && now.Sub(n.onlineAt) > onlineMaxAge {
		n.online = nil
	}

	online, conns := n.counts()
	return Sample{At: now, RateUp: n.rateUp, RateDown: n.rateDown, Online: online, Connections: conns, Counted: counted}, ack
}

// records reads the agent's buckets for what counts: valid ones only, oldest
// first, a timestamp once. A second record under a timestamp is added to the
// first; the agent never sends that, and it is what the numbers would mean if
// it did. ahead is how many were dropped for being past limit.
func records(in []*agent.TrafficRecord, nodeID uint, limit int64) (out []record, ahead int) {
	byTime := map[int64]*record{}
	for _, r := range in {
		if r == nil || r.Timestamp <= 0 {
			continue
		}
		if r.Timestamp > limit {
			ahead++
			continue
		}
		rec := byTime[r.Timestamp]
		if rec == nil {
			rec = &record{ts: r.Timestamp, b: bucket{subs: map[uint]Totals{}}}
			byTime[r.Timestamp] = rec
		}
		for email, v := range r.UserUplink {
			rec.b.take(email, nodeID, Totals{Up: nonNegative(v)})
		}
		for email, v := range r.UserDownlink {
			rec.b.take(email, nodeID, Totals{Down: nonNegative(v)})
		}
	}
	for _, rec := range byTime {
		rec.node = rec.b.node()
		out = append(out, *rec)
	}
	slices.SortFunc(out, func(a, b record) int { return cmp.Compare(a.ts, b.ts) })
	return out, ahead
}

func nonNegative(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// take files bytes under whose they are: a subscription's, this node's
// bridge's, or nobody's we know, which is dropped.
func (b *bucket) take(email string, nodeID uint, t Totals) {
	if t.isZero() {
		return
	}
	if id, ok := nodecfg.ParseSubscriptionEmail(email); ok {
		b.subs[uint(id)] = b.subs[uint(id)].plus(t)
	} else if id, ok := nodecfg.ParseBridgeEmail(email); ok && uint(id) == nodeID {
		b.bridge = b.bridge.plus(t)
	}
}

func (t Totals) minus(o Totals) Totals {
	return Totals{Up: monus(t.Up, o.Up), Down: monus(t.Down, o.Down)}
}

// monus is a - b, or zero where b is more: when the agent's buffer is wiped
// the newest bucket can start again below what was shown.
func monus(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}

// rateLocked sets the node's rate from the polls of the last rateWindow: the
// bytes that appeared in them over the time they cover.
func (c *Collector) rateLocked(n *node, now time.Time) {
	cut := now.Add(-rateWindow)
	i := 0
	for i < len(n.obs) && !n.obs[i].to.After(cut) {
		i++
	}
	n.obs = slices.Clone(n.obs[i:])
	n.rateUp, n.rateDown = 0, 0
	if len(n.obs) == 0 {
		return
	}
	var sum Totals
	for _, o := range n.obs {
		sum = sum.plus(o.inc)
	}
	secs := n.obs[len(n.obs)-1].to.Sub(n.obs[0].from).Seconds()
	if secs <= 0 {
		return
	}
	n.rateUp = uint64(math.Round(float64(sum.Up) / secs))
	n.rateDown = uint64(math.Round(float64(sum.Down) / secs))
}

// onlineLocked takes in who the agent says is connected.
func onlineLocked(n *node, ips map[string]map[string]int64, now time.Time) {
	online := map[uint]int{}
	for email, addrs := range ips {
		if len(addrs) == 0 {
			continue
		}
		if id, ok := nodecfg.ParseSubscriptionEmail(email); ok {
			online[uint(id)] += len(addrs)
		}
	}
	n.online, n.onlineAt = online, now
	if n.lastSeen == nil {
		n.lastSeen = map[uint]time.Time{}
	}
	for id := range online {
		n.lastSeen[id] = now
	}
}

// counts is how many subscriptions are online on the node and how many
// client addresses that makes.
func (n *node) counts() (online, connections int) {
	for _, k := range n.online {
		online++
		connections += k
	}
	return online, connections
}

// ackNode tells a node's agent to drop the buckets up to through, which the
// caller has seen on disk, and notes that it did.
func (c *Collector) ackNode(ctx context.Context, nodeID uint, n *node, src Source, through int64) error {
	err := src.AckBufferedTraffic(ctx, through)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("usage: node %d: acknowledge traffic through %d: %w", nodeID, through, err)
	}
	n.acked = max(n.acked, through)
	n.staleSeen = false
	return nil
}

// acknowledge tells each node's agent to drop the buckets the file counts. It
// goes through each node's own mark and Source, one node's failure doesn't
// stop the others, and the error joins the failures.
func (c *Collector) acknowledge(ctx context.Context) error {
	type job struct {
		id      uint
		n       *node
		src     Source
		through int64
	}
	var jobs []job
	c.mu.RLock()
	for id, n := range c.nodes {
		if n.src != nil && n.diskThrough > 0 && (n.diskThrough > n.acked || n.staleSeen) {
			jobs = append(jobs, job{id, n, n.src, n.diskThrough})
		}
	}
	c.mu.RUnlock()
	slices.SortFunc(jobs, func(a, b job) int { return int(a.id) - int(b.id) })

	var errs []error
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := c.ackNode(ctx, j.id, j.n, j.src, j.through); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
