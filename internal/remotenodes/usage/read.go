// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import "time"

// What the page reads. Every method here answers from memory: nothing waits
// for the disk or for a node.
//
// "Today" is the current UTC day and "month" the last thirty UTC days with
// today among them, whatever zone the router is set to.

// NodeUsage is the page's numbers for one node. The bytes are the
// subscriptions': the bridge's own client carries the same traffic again on
// its way through the portal and is counted apart.
type NodeUsage struct {
	// Live is whether the node has answered lately. When it hasn't, the
	// rates, Online and Connections are zero rather than stale, and the bytes
	// are what the history held.
	Live bool
	// Online is how many subscriptions have a client connected, and
	// Connections how many client addresses that makes.
	Online      int
	Connections int
	// The rates are bytes per second.
	RateUp, RateDown                       uint64
	TodayUp, TodayDown, MonthUp, MonthDown uint64
	// UpdatedAt is when the node last answered; zero if it hasn't since the
	// hub started.
	UpdatedAt time.Time
}

// SubUsage is the page's numbers for one subscription, on one node or over
// all of them.
type SubUsage struct {
	// Live is whether the node (for the total, any node) has answered lately.
	// When it is false Online and Connections are not known, and are zero.
	Live bool
	// Online is whether a client of the subscription is connected, and
	// Connections how many addresses that is.
	Online      bool
	Connections int
	// LastSeen is when a poll last found the subscription online; zero if none
	// has since the hub started. It is not kept across restarts: it would be a
	// write on every poll.
	LastSeen                               time.Time
	TodayUp, TodayDown, MonthUp, MonthDown uint64
}

// window is today and the first day of the month that ends with it.
func (c *Collector) window() (today, monthFrom day) {
	today = dayOfTime(c.now())
	return today, today - (monthDays - 1)
}

// sums are a series' bytes today and over the month. The newest bucket of the
// agent is still growing and is not in the history yet; share is what of it
// belongs to this series, and it is added on the day it falls in, so the page
// shows what has been used and not only what has been counted.
func sums(series map[day]Totals, p *partial, share Totals, today, from day) (t, m Totals) {
	t, m = series[today], span(series, from, today)
	if p != nil {
		if d := dayOf(p.ts); d == today {
			t = t.plus(share)
		}
		if d := dayOf(p.ts); d >= from && d <= today {
			m = m.plus(share)
		}
	}
	return t, m
}

// Node is what the page shows for a node. A node the collector has not heard
// of reads as zeros.
func (c *Collector) Node(nodeID uint) NodeUsage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := c.nodes[nodeID]
	if n == nil {
		return NodeUsage{}
	}
	today, from := c.window()
	var share Totals
	if n.partial != nil {
		share = n.partial.node
	}
	t, m := sums(n.hist.days, n.partial, share, today, from)
	u := NodeUsage{Live: n.live, TodayUp: t.Up, TodayDown: t.Down, MonthUp: m.Up, MonthDown: m.Down, UpdatedAt: n.updatedAt}
	if n.live {
		u.Online, u.Connections = n.counts()
		u.RateUp, u.RateDown = n.rateUp, n.rateDown
	}
	return u
}

// Subscription is what the page shows for a subscription on one node.
func (c *Collector) Subscription(subID, nodeID uint) SubUsage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := c.nodes[nodeID]
	if n == nil {
		return SubUsage{}
	}
	today, from := c.window()
	return c.subUsage(n, subID, today, from)
}

func (c *Collector) subUsage(n *node, subID uint, today, from day) SubUsage {
	var share Totals
	if n.partial != nil {
		share = n.partial.b.subs[subID]
	}
	t, m := sums(n.hist.subs[subID], n.partial, share, today, from)
	u := SubUsage{Live: n.live, LastSeen: n.lastSeen[subID], TodayUp: t.Up, TodayDown: t.Down, MonthUp: m.Up, MonthDown: m.Down}
	if n.live {
		u.Connections = n.online[subID]
		u.Online = u.Connections > 0
	}
	return u
}

// SubscriptionTotal is what the page shows for a subscription over every
// node: the bytes added up, and it is online if it is on any live node. A node
// it was never on counts for nothing, and one it was taken off still counts
// what it used while it was on. Live means some node is, and a node that
// isn't can't say, so a subscription that is offline on every live node may
// still be online on one that is down; the page, which knows where the
// subscription is switched on, can tell from the nodes' own numbers.
func (c *Collector) SubscriptionTotal(subID uint) SubUsage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	today, from := c.window()
	var u SubUsage
	for _, n := range c.nodes {
		s := c.subUsage(n, subID, today, from)
		u.Live = u.Live || s.Live
		u.Online = u.Online || s.Online
		u.Connections += s.Connections
		if s.LastSeen.After(u.LastSeen) {
			u.LastSeen = s.LastSeen
		}
		u.TodayUp, u.TodayDown = sat(u.TodayUp, s.TodayUp), sat(u.TodayDown, s.TodayDown)
		u.MonthUp, u.MonthDown = sat(u.MonthUp, s.MonthUp), sat(u.MonthDown, s.MonthDown)
	}
	return u
}

// Through is the timestamp of the newest bucket that was counted for a node,
// zero when none was. A source that makes its own buckets (the router's own
// entry) dates them after it: one dated at or before it would be taken for
// counted already, which a clock that went back over a restart would do.
func (c *Collector) Through(nodeID uint) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n := c.nodes[nodeID]; n != nil {
		return n.hist.through
	}
	return 0
}

// SubscriptionLifetime is everything a subscription has moved, both ways, on
// every node since it was first counted, nodes since removed included. The
// bucket that is still growing is part of it, so a limit sees what has been
// used and not only what has been written down. It only grows, until the
// subscription is deleted, which makes it a count a limit can be measured
// against.
func (c *Collector) SubscriptionLifetime(subID uint) Totals {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t := c.retired[subID]
	for _, n := range c.nodes {
		t = t.plus(n.hist.life[subID])
		if n.partial != nil {
			t = t.plus(n.partial.b.subs[subID])
		}
	}
	return t
}

// Bridge is what the node's bridge client has carried since the node was
// first counted, both ways: the subscriptions' traffic once more, seen from
// the tunnel's side. It counts completed buckets, like the history.
func (c *Collector) Bridge(nodeID uint) Totals {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n := c.nodes[nodeID]; n != nil {
		return n.hist.bridge
	}
	return Totals{}
}

// Traffic is a node's chart: a point for each Step of the last Window that a
// poll fell in, oldest first. Never nil.
func (c *Collector) Traffic(nodeID uint) []Point {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n := c.nodes[nodeID]; n != nil {
		return n.ring.points(c.now())
	}
	return []Point{}
}
