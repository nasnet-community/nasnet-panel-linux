// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// t0 is noon on the 30th, so a test can move a few hours either way and stay
// on the same day.
var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// clock is a time that moves when the test says so.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *clock { return &clock{t: t} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// logs collects what the collector says.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logs) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// open starts a collector on dir with the clock and the log of the test.
func open(t *testing.T, dir string, clk *clock) (*Collector, *logs) {
	t.Helper()
	l := &logs{}
	c, err := Open(Config{Dir: dir, Now: clk.Now, Logf: l.Logf})
	if err != nil {
		t.Fatal(err)
	}
	return c, l
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// corrupt lists the copies of a damaged file in dir.
func corrupt(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, n := range names(t, dir) {
		if strings.HasPrefix(n, FileName+".corrupt-") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// inject counts a bucket the way a poll does, without a poll.
func (c *Collector) inject(nodeID uint, ts int64, b bucket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.nodeLocked(nodeID)
	n.hist.add(ts, b)
	if ts > n.hist.through {
		n.hist.through = ts
	}
}

// subs is a bucket of the subscriptions' bytes: id, up, down, and again.
func subs(idUpDown ...uint64) bucket {
	b := bucket{subs: map[uint]Totals{}}
	for i := 0; i+2 < len(idUpDown); i += 3 {
		b.subs[uint(idUpDown[i])] = Totals{Up: idUpDown[i+1], Down: idUpDown[i+2]}
	}
	return b
}
