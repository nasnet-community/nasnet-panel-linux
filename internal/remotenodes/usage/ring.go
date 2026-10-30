// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"slices"
	"time"
)

// The traffic chart: one point every Step for the last Window. Memory only.
const (
	Step   = 5 * time.Second
	Window = 15 * time.Minute

	ringSlots = int(Window / Step)
)

// Point is one step of a node's traffic chart: the rates in bytes per second
// averaged over the last few polls, as they were at At. Up is what the
// subscriptions sent, Down what was sent to them.
type Point struct {
	At       time.Time
	Up, Down uint64
}

// ring holds the last Window of points by the Step they fall in. A point
// takes the place of the one a whole window older in its slot, and a poll
// that lands in a slot that has one already replaces it, so the chart never
// shows two points for a step. Steps nothing was polled in stay empty: a gap
// in the chart is a gap in the polling.
type ring struct{ slots [ringSlots]slot }

type slot struct {
	step     int64 // unix time / Step; zero while the slot is empty
	up, down uint64
}

func stepOf(t time.Time) int64 { return t.Unix() / int64(Step/time.Second) }

func (r *ring) put(at time.Time, up, down uint64) {
	s := stepOf(at)
	if s <= 0 {
		return
	}
	r.slots[s%int64(ringSlots)] = slot{s, up, down}
}

// points are those of the window that ends at now, oldest first.
func (r *ring) points(now time.Time) []Point {
	last := stepOf(now)
	out := []Point{}
	for _, p := range r.slots {
		if p.step > last-int64(ringSlots) && p.step <= last {
			out = append(out, Point{At: time.Unix(p.step*int64(Step/time.Second), 0).UTC(), Up: p.up, Down: p.down})
		}
	}
	slices.SortFunc(out, func(a, b Point) int { return a.At.Compare(b.At) })
	return out
}
