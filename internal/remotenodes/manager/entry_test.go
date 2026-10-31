// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
)

// fakeFacts answers each read with the next result it was given, and keeps
// answering with the last. It notes when it was read.
type fakeFacts struct {
	t0 time.Time

	mu      sync.Mutex
	results []factsResult
	reads   []time.Duration
}

type factsResult struct {
	f   routerfacts.Facts
	err error
}

func newFacts(results ...factsResult) *fakeFacts { return &fakeFacts{t0: time.Now(), results: results} }

func (f *fakeFacts) Read() (routerfacts.Facts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := min(len(f.reads), len(f.results)-1)
	f.reads = append(f.reads, time.Since(f.t0))
	return f.results[i].f, f.results[i].err
}

func (f *fakeFacts) set(results ...factsResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results, f.reads = results, nil
}

func (f *fakeFacts) readTimes() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reads)
}

// healthy is a report from a router with both links up, the three routes
// ready and a new enough RouterOS, written at the given time.
func healthy(at time.Time) routerfacts.Facts {
	return routerfacts.Facts{
		Domestic: true, VPN: true, VPNKind: "wireguard", VPNLabel: "wg-vpn",
		RouteForeign: true, RouteVPN: true, RouteDomestic: true, ROS: "7.24.5", Updated: at,
	}
}

func ok(f routerfacts.Facts) factsResult { return factsResult{f: f} }

func failing(err error) factsResult { return factsResult{err: err} }

func withFacts(f Facts) func(*Config) { return func(c *Config) { c.Facts = f } }

func TestTheEntryIsReadAtStartAndEveryThirtySeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(healthy(epoch)))
		r := newRig(t, withFacts(facts))
		if got := r.m.Entry(); !got.On {
			t.Fatalf("the entry before Run = %+v: it should have been read by New", got)
		}
		r.start()
		defer r.stop()
		r.advance(65 * time.Second)
		if got := facts.readTimes(); !slices.Equal(got, secs(0, 30, 60)) {
			t.Errorf("the report was read at %v", got)
		}
		e := r.m.Entry()
		if !e.On || e.Domestic != "online" || e.VPN.State != "online" || e.VPN.Kind != "wireguard" || e.VPN.Label != "wg-vpn" {
			t.Errorf("entry = %+v", e)
		}
		if e.RouterOS.Version != "7.24.5" || e.RouterOS.Min != "7.11" || !e.RouterOS.OK {
			t.Errorf("RouterOS = %+v", e.RouterOS)
		}
		want := []struct{ addr, usedFor, via string }{
			{"192.168.50.16", "downloads", "foreign"}, {"192.168.50.17", "entry-exit", "vpn"}, {"192.168.50.18", "nodes", "domestic"},
		}
		if len(e.Routes) != 3 {
			t.Fatalf("routes = %+v", e.Routes)
		}
		for i, w := range want {
			if r := e.Routes[i]; r.Address != w.addr || r.UsedFor != w.usedFor || r.Via != w.via || !r.Ready {
				t.Errorf("route %d = %+v", i, r)
			}
		}
		if e.CheckedAt == nil || !e.CheckedAt.Equal(epoch) {
			t.Errorf("checked at %v", e.CheckedAt)
		}
		// the report is a minute old by now and that is nothing to warn of
		if e.Stale {
			t.Error("a report a minute old is stale")
		}
	})
}

func TestAMissingReportMeansTheEntryIsOffAndNothingIsKnown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		missing := &fs.PathError{Op: "stat", Path: "/data/router/state", Err: fs.ErrNotExist}
		r := newRig(t, withFacts(newFacts(failing(missing))))
		r.start()
		defer r.stop()
		e := r.m.Entry()
		if e.On || e.Domestic != "missing" || e.VPN.State != "missing" || e.VPN.Kind != "" || e.Bridge != "stopped" || e.CheckedAt != nil || e.Stale {
			t.Fatalf("entry = %+v", e)
		}
		if e.RouterOS.Version != "" || e.RouterOS.Min != "7.11" || e.RouterOS.OK {
			t.Errorf("RouterOS = %+v", e.RouterOS)
		}
		if len(e.Routes) != 3 || e.Routes[0].Ready || e.Routes[1].Ready || e.Routes[2].Ready {
			t.Errorf("routes = %+v", e.Routes)
		}
		// the log says why, once, however long it stays so
		r.advance(5 * time.Minute)
		if r.log.count("the router's entry is off", "has not written its report") != 1 || strings.Contains(r.log.String(), "/data/router") {
			t.Errorf("log:\n%s", r.log)
		}
	})
}

func TestAReportWithNoFactsSourceAtAllIsTheSame(t *testing.T) {
	cfg, log := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	if e := m.Entry(); e.On || e.Domestic != "missing" || e.Stale {
		t.Errorf("entry = %+v", e)
	}
	if log.count("the router's entry is off", "not set up to read") != 1 {
		t.Errorf("log:\n%s", log)
	}
}

// A report that is being rewritten reads as empty or as unsettled. It is read
// again a moment later, up to three times, before the entry is called off.
func TestATornReportIsReadAgainAndGivesUpAfterThreeTries(t *testing.T) {
	t.Run("it settles", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			facts := newFacts(failing(routerfacts.ErrEmpty), failing(routerfacts.ErrUnsettled), ok(healthy(epoch)))
			r := newRig(t, withFacts(facts))
			if got := facts.readTimes(); !slices.Equal(got, []time.Duration{0, 300 * time.Millisecond, 600 * time.Millisecond}) {
				t.Errorf("read at %v", got)
			}
			if e := r.m.Entry(); !e.On {
				t.Errorf("entry = %+v", e)
			}
			if r.log.count("the router's entry is off") != 0 {
				t.Errorf("the entry was called off for a report that settled:\n%s", r.log)
			}
		})
	})
	t.Run("it doesn't", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			facts := newFacts(failing(routerfacts.ErrEmpty))
			r := newRig(t, withFacts(facts))
			if got := facts.readTimes(); !slices.Equal(got, []time.Duration{0, 300 * time.Millisecond, 600 * time.Millisecond}) {
				t.Errorf("read at %v, want three tries 300 ms apart", got)
			}
			if e := r.m.Entry(); e.On || e.Domestic != "missing" {
				t.Errorf("entry = %+v", e)
			}
			if r.log.count("the router's entry is off", "report is empty") != 1 {
				t.Errorf("log:\n%s", r.log)
			}
		})
	})
	t.Run("any other failure is not retried", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			facts := newFacts(failing(routerfacts.ErrTooLarge))
			newRig(t, withFacts(facts))
			if got := facts.readTimes(); len(got) != 1 {
				t.Errorf("read at %v: a report that is too large won't shrink", got)
			}
			facts = newFacts(failing(errors.New("permission denied")))
			r := newRig(t, withFacts(facts))
			if got := facts.readTimes(); len(got) != 1 {
				t.Errorf("read at %v", got)
			}
			if r.log.count("can't be read", "permission denied") != 1 {
				t.Errorf("log:\n%s", r.log)
			}
		})
	})
}

func TestAStaleReportIsWarnedOfAndTheEntryStaysOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, withFacts(newFacts(ok(healthy(epoch.Add(-20*time.Minute))))))
		e := r.m.Entry()
		if !e.On || !e.Stale || e.CheckedAt == nil || !e.CheckedAt.Equal(epoch.Add(-20*time.Minute)) {
			t.Fatalf("entry = %+v", e)
		}
	})
}

// A report that is fresh when it is read goes stale on its own: the script
// stopped, and nothing writes the file again.
func TestAReportGoesStaleWhenTheRouterStopsWritingIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, withFacts(newFacts(ok(healthy(epoch)))))
		r.start()
		defer r.stop()
		r.advance(14 * time.Minute)
		if e := r.m.Entry(); e.Stale || !e.On {
			t.Fatalf("after 14 minutes: %+v", e)
		}
		r.advance(2 * time.Minute)
		if e := r.m.Entry(); !e.Stale || !e.On {
			t.Fatalf("after 16 minutes: %+v", e)
		}
	})
}

func TestTheGateFlipsOffAndOnWithTheLinks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		facts := newFacts(ok(healthy(epoch)))
		r := newRig(t, withFacts(facts))
		r.start()
		defer r.stop()
		if !r.m.Entry().On {
			t.Fatal("the entry is off with both links up")
		}

		down := healthy(epoch)
		down.Domestic = false
		facts.set(ok(down))
		r.advance(31 * time.Second)
		e := r.m.Entry()
		if e.On || e.Domestic != "offline" || e.VPN.State != "online" {
			t.Fatalf("with the Domestic link down: %+v", e)
		}
		if r.log.count("the router's entry is off", "the Domestic link is down") != 1 {
			t.Errorf("log:\n%s", r.log)
		}

		novpn := healthy(epoch)
		novpn.VPN, novpn.VPNKind, novpn.VPNLabel, novpn.RouteVPN = false, "", "", false
		facts.set(ok(novpn))
		r.advance(30 * time.Second)
		if e := r.m.Entry(); e.On || e.VPN.State != "missing" {
			t.Fatalf("with no VPN: %+v", e)
		}

		old := healthy(epoch)
		old.ROS = "7.9"
		facts.set(ok(old))
		r.advance(30 * time.Second)
		if e := r.m.Entry(); e.On || e.RouterOS.OK || e.RouterOS.Version != "7.9" {
			t.Fatalf("with RouterOS 7.9: %+v", e)
		}

		facts.set(ok(healthy(epoch)))
		r.advance(30 * time.Second)
		if !r.m.Entry().On || r.log.count("the router's entry is on") != 2 {
			t.Errorf("entry = %+v, log:\n%s", r.m.Entry(), r.log)
		}
	})
}

func TestAnEntryIsACopy(t *testing.T) {
	cfg, _ := baseConfig(t)
	cfg.Facts = newFacts(ok(healthy(epoch)))
	m, err := New(cfg)
	must(t, err)
	e := m.Entry()
	e.Routes[0].Ready = false
	*e.CheckedAt = epoch.Add(-time.Hour)
	e.On = false
	if got := m.Entry(); !got.On || !got.Routes[0].Ready || !got.CheckedAt.Equal(epoch) {
		t.Errorf("a change to an answer reached the manager: %+v", got)
	}
}
