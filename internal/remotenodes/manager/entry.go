// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
)

const (
	// The router's script rewrites its report in place, so a read can land
	// between the emptying and the filling. routerfacts says so with ErrEmpty
	// or ErrUnsettled, and the answer is to look again in a moment: a few
	// tries, a little apart, before the report counts as unreadable.
	factsTries = 3
	factsPause = 300 * time.Millisecond
)

// watchEntry reads the router's report every EntryEvery. New read it once
// already, so the first wait comes first.
func (m *Manager) watchEntry(ctx context.Context) {
	for sleep(ctx, m.cfg.EntryEvery) {
		m.evalEntry(ctx)
	}
}

// sleep waits d, and says whether it did: false means ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// evalEntry reads the router's report and decides the entry gate from it. A
// report that can't be had gives a gate that is off, since an entry that can't
// be shown to have both its links is not one to carry traffic on.
func (m *Manager) evalEntry(ctx context.Context) {
	now := m.now()
	var e routerfacts.Entry
	switch f, err := m.readFacts(ctx); {
	case ctx.Err() != nil:
		return
	case err == nil:
		e = f.Entry(now, routerfacts.MinRouterOS)
	default:
		e = routerfacts.Facts{}.Entry(now, routerfacts.MinRouterOS)
		e.Stale = false // there is no report to be old
		e.Reason = factsProblem(err)
	}
	m.setEntry(e)
}

func (m *Manager) readFacts(ctx context.Context) (routerfacts.Facts, error) {
	if m.cfg.Facts == nil {
		return routerfacts.Facts{}, errNoFacts
	}
	var f routerfacts.Facts
	var err error
	for try := 0; try < factsTries; try++ {
		if try > 0 && !sleep(ctx, factsPause) {
			return f, ctx.Err()
		}
		if f, err = m.cfg.Facts.Read(); !errors.Is(err, routerfacts.ErrEmpty) && !errors.Is(err, routerfacts.ErrUnsettled) {
			break
		}
	}
	return f, err
}

var errNoFacts = errors.New("this hub is not set up to read the router's report")

// factsProblem is why there is no report, in a sentence for the log.
func factsProblem(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "the router has not written its report yet"
	case errors.Is(err, routerfacts.ErrEmpty), errors.Is(err, routerfacts.ErrUnsettled), errors.Is(err, routerfacts.ErrTooLarge), errors.Is(err, errNoFacts):
		return err.Error()
	}
	return "the router's report can't be read: " + err.Error()
}

// setEntry takes in a new reading. A change of what it says, or why, is
// logged once; the same reading again says nothing.
func (m *Manager) setEntry(e routerfacts.Entry) {
	got := apiEntry(e)
	key := fmt.Sprintf("%t|%s", e.On, e.Reason)
	m.mu.Lock()
	changed := !m.entrySet || key != m.entryKey
	m.entry, m.entryKey, m.entrySet = got, key, true
	m.mu.Unlock()
	switch {
	case !changed:
	case e.On:
		m.log.Info("the router's entry is on")
	default:
		m.log.Info("the router's entry is off", "why", e.Reason)
	}
}

func apiEntry(e routerfacts.Entry) api.Entry {
	out := api.Entry{
		On: e.On, Domestic: e.Domestic, Bridge: e.Bridge, Stale: e.Stale,
		VPN:      api.VPN{State: e.VPN.State, Kind: e.VPN.Kind, Label: e.VPN.Label},
		RouterOS: api.RouterOS{Version: e.RouterOS.Version, Min: e.RouterOS.Min, OK: e.RouterOS.OK},
		Routes:   make([]api.Route, len(e.Routes)),
	}
	for i, r := range e.Routes {
		out.Routes[i] = api.Route{Address: r.Address, UsedFor: r.UsedFor, Via: r.Via, Ready: r.Ready}
	}
	if e.CheckedAt != nil {
		at := *e.CheckedAt
		out.CheckedAt = &at
	}
	return out
}

// Entry is the router's entry gate as the page shows it. It is on when a
// Domestic link and a VPN are up, the router's routes are ready and RouterOS
// is new enough; a report that is old is warned of, and doesn't turn it off,
// since the router's own rules fail closed.
func (m *Manager) Entry() api.Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.entry
	e.Routes = append([]api.Route(nil), e.Routes...)
	if e.CheckedAt != nil {
		at := *e.CheckedAt
		e.CheckedAt = &at
	}
	return e
}

// entryOn is whether the gate is on right now.
func (m *Manager) entryOn() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entry.On
}
