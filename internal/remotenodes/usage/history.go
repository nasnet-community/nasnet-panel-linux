// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"math"
	"time"
)

// Totals is a pair of byte counts: Up is what the clients sent, Down what was
// sent to them.
type Totals struct {
	Up   uint64 `json:"up"`
	Down uint64 `json:"down"`
}

func (t Totals) plus(o Totals) Totals { return Totals{sat(t.Up, o.Up), sat(t.Down, o.Down)} }
func (t Totals) isZero() bool         { return t == Totals{} }

// sat adds without wrapping: a counter that can't grow any more stays put.
func sat(a, b uint64) uint64 {
	if s := a + b; s >= a {
		return s
	}
	return math.MaxUint64
}

// The days kept. A day is a UTC day, so "today" changes at 00:00 UTC whatever
// the router's own zone says. The month is the last thirty days with today
// among them; one more day is kept so a reader whose clock is a little ahead
// of the writer's still finds the whole window.
const (
	monthDays = 30
	keepDays  = 31
)

// day is a UTC calendar day, counted from 1970-01-01.
type day int64

const (
	secondsPerDay = 24 * 60 * 60
	dayLayout     = "2006-01-02"
)

func dayOf(unix int64) day {
	d := unix / secondsPerDay
	if unix%secondsPerDay < 0 {
		d--
	}
	return day(d)
}

func dayOfTime(t time.Time) day { return dayOf(t.Unix()) }

func (d day) String() string {
	return time.Unix(int64(d)*secondsPerDay, 0).UTC().Format(dayLayout)
}

func parseDay(s string) (day, error) {
	t, err := time.ParseInLocation(dayLayout, s, time.UTC)
	if err != nil {
		return 0, err
	}
	return dayOfTime(t), nil
}

// bucket is what one of the agent's buckets held that counts: each
// subscription's bytes, and the bridge's apart from them. The bridge's client
// carries the same traffic a second time on its way through the portal, so it
// is not part of anyone's usage.
type bucket struct {
	subs   map[uint]Totals
	bridge Totals
}

// node is all the subscriptions' bytes in the bucket.
func (b bucket) node() Totals {
	var t Totals
	for _, s := range b.subs {
		t = t.plus(s)
	}
	return t
}

// nodeHist is what is kept of one node: a pair of counts per day for the node
// and for each subscription on it, the bridge's lifetime totals, and the mark.
// It is what the file holds.
type nodeHist struct {
	days   map[day]Totals          // the subscriptions' bytes, all of them
	subs   map[uint]map[day]Totals // and each one's
	bridge Totals

	// through is the timestamp of the newest bucket counted above. Everything
	// up to it is in these totals, and the agent may be told to drop it.
	through int64
}

func newNodeHist() *nodeHist {
	return &nodeHist{days: map[day]Totals{}, subs: map[uint]map[day]Totals{}}
}

// add counts a bucket under the day its timestamp falls in. That is the day
// the bytes went through, give or take the hour the bucket spans, and it holds
// when the hub was away and several buckets arrive at once.
func (h *nodeHist) add(ts int64, b bucket) {
	d := dayOf(ts)
	if n := b.node(); !n.isZero() {
		h.days[d] = h.days[d].plus(n)
	}
	for id, t := range b.subs {
		if t.isZero() {
			continue
		}
		m := h.subs[id]
		if m == nil {
			m = map[day]Totals{}
			h.subs[id] = m
		}
		m[d] = m[d].plus(t)
	}
	h.bridge = h.bridge.plus(b.bridge)
}

// prune drops the days before keepFrom, and a subscription with none left.
func (h *nodeHist) prune(keepFrom day) {
	for d := range h.days {
		if d < keepFrom {
			delete(h.days, d)
		}
	}
	for id, m := range h.subs {
		for d := range m {
			if d < keepFrom {
				delete(m, d)
			}
		}
		if len(m) == 0 {
			delete(h.subs, id)
		}
	}
}

// empty is a node with nothing worth a place in the file.
func (h *nodeHist) empty() bool {
	return len(h.days) == 0 && len(h.subs) == 0 && h.bridge.isZero() && h.through == 0
}

// span adds up a series over the days from through to, both included.
func span(series map[day]Totals, from, to day) Totals {
	var t Totals
	for d, v := range series {
		if d >= from && d <= to {
			t = t.plus(v)
		}
	}
	return t
}
