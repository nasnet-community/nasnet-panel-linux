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
	t, m := n.hist.days[today], span(n.hist.days, from, today)
	return NodeUsage{TodayUp: t.Up, TodayDown: t.Down, MonthUp: m.Up, MonthDown: m.Down}
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
	return subUsage(n.hist.subs[subID], today, from)
}

// SubscriptionTotal is what the page shows for a subscription over every
// node. A node it was never on counts for nothing, and one it was taken off
// still counts what it used while it was on.
func (c *Collector) SubscriptionTotal(subID uint) SubUsage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	today, from := c.window()
	var u SubUsage
	for _, n := range c.nodes {
		s := subUsage(n.hist.subs[subID], today, from)
		u.TodayUp, u.TodayDown = sat(u.TodayUp, s.TodayUp), sat(u.TodayDown, s.TodayDown)
		u.MonthUp, u.MonthDown = sat(u.MonthUp, s.MonthUp), sat(u.MonthDown, s.MonthDown)
	}
	return u
}

func subUsage(series map[day]Totals, today, from day) SubUsage {
	t, m := series[today], span(series, from, today)
	return SubUsage{TodayUp: t.Up, TodayDown: t.Down, MonthUp: m.Up, MonthDown: m.Down}
}

// Bridge is what the node's bridge client has carried since the node was
// first counted, both ways: the subscriptions' traffic once more, seen from
// the tunnel's side.
func (c *Collector) Bridge(nodeID uint) Totals {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n := c.nodes[nodeID]; n != nil {
		return n.hist.bridge
	}
	return Totals{}
}
