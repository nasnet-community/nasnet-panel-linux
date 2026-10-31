// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
)

// epoch is the time the tests start at.
var epoch = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// simNow is the epoch plus the time that has passed. In a synctest bubble the
// time is the bubble's, which only moves when everything in it is waiting, so
// a test can sleep an hour and the manager sees it.
func simNow() func() time.Time {
	start := time.Now()
	return func() time.Time { return epoch.Add(time.Since(start)) }
}

// testClock is a time that moves when the test says so.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *testClock { return &testClock{t: t} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// shiftClock is simNow with a jump the test can make: a clock that was set
// wrong and then right again.
type shiftClock struct {
	mu    sync.Mutex
	start time.Time
	shift time.Duration
}

func newShiftClock() *shiftClock { return &shiftClock{start: time.Now()} }

func (c *shiftClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return epoch.Add(time.Since(c.start) + c.shift)
}

func (c *shiftClock) Shift(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shift += d
}

// logBuf is what the manager logged, safe to read while it still logs.
type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// count is how many log lines contain all of the words.
func (l *logBuf) count(words ...string) int {
	n := 0
	for _, line := range bytes.Split([]byte(l.String()), []byte("\n")) {
		ok := len(line) > 0
		for _, w := range words {
			ok = ok && bytes.Contains(line, []byte(w))
		}
		if ok {
			n++
		}
	}
	return n
}

func (l *logBuf) logf(format string, args ...any) {
	fmt.Fprintf(l, "usage: "+format+"\n", args...)
}

func testLog() (*slog.Logger, *logBuf) {
	buf := &logBuf{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func openStore(t testing.TB) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	must(t, err)
	return st
}

// memStore is a state in memory that takes any edit, valid or not. The real
// store refuses a CA that won't load, and a test of what the manager does with
// one needs a state that holds it.
type memStore struct {
	mu     sync.Mutex
	st     model.State
	writes uint64
}

func (m *memStore) Read() model.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.Clone()
}

func (m *memStore) Update(fn func(*model.State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.st.Clone()
	if err := fn(&next); err != nil {
		return err
	}
	m.st = next
	m.writes++
	return nil
}

func (m *memStore) Writes() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writes
}

var errNoNode = errors.New("no node is there")

// refuse is a connector that never reaches anything.
var refuse = ConnectorFunc(func(ctx context.Context, n model.Node) (Client, error) { return nil, errNoNode })

// baseConfig is what a test starts from: a real store in a temporary folder
// and a real collector in memory, both on the test's clock, a connector that
// refuses, and a log the test can read. Tests change the fields they care about.
func baseConfig(t testing.TB) (Config, *logBuf) {
	t.Helper()
	log, buf := testLog()
	now := simNow()
	coll, err := usage.Open(usage.Config{Now: now, Logf: buf.logf})
	must(t, err)
	return Config{Store: openStore(t), Usage: coll, Connector: refuse, Log: log, Now: now}, buf
}

func newCA(t testing.TB, at time.Time) *pki.CA {
	t.Helper()
	ca, err := pki.NewCA(at)
	must(t, err)
	return ca
}

// setCA puts a CA into the state, as a restore does.
func setCA(t testing.TB, st Store, ca *pki.CA) {
	t.Helper()
	must(t, st.Update(func(s *model.State) error {
		s.CACert, s.CAKey = string(ca.CertPEM()), string(ca.KeyPEM())
		return nil
	}))
}
