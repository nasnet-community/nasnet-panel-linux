// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
)

func TestNewNeedsTheThreeDependenciesAndNothingElse(t *testing.T) {
	full, _ := baseConfig(t)
	for name, c := range map[string]struct {
		mod  func(*Config)
		want string
	}{
		"no store":     {func(c *Config) { c.Store = nil }, "store"},
		"no usage":     {func(c *Config) { c.Usage = nil }, "usage"},
		"no connector": {func(c *Config) { c.Connector = nil }, "connector"},
	} {
		cfg := full
		c.mod(&cfg)
		if m, err := New(cfg); err == nil || !strings.Contains(err.Error(), c.want) || m != nil {
			t.Errorf("%s: %v, %v", name, m, err)
		}
	}

	// everything else may be left out: a hub with nothing configured starts
	bare := Config{Store: full.Store, Usage: full.Usage, Connector: full.Connector}
	m, err := New(bare)
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.Log == nil || m.cfg.Now == nil || m.cfg.StatusAddr != DefaultStatusAddr || len(m.cfg.DNS) != 2 {
		t.Errorf("defaults not filled: %+v", m.cfg)
	}
	if m.cfg.PollEvery != DefaultPollEvery || m.cfg.StateEvery != DefaultStateEvery || m.cfg.EntryEvery != DefaultEntryEvery || m.cfg.BridgeEvery != DefaultBridgeEvery {
		t.Errorf("timings not filled: %+v", m.cfg)
	}
	if m.Addrs() != netroles.Defaults() {
		t.Errorf("addresses = %+v", m.Addrs())
	}
}

func TestRunStopsWhenClosedAndOnlyRunsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, _ := baseConfig(t)
		m, err := New(cfg)
		must(t, err)
		done := make(chan struct{})
		go func() { m.Run(context.Background()); close(done) }()
		synctest.Wait()

		// a second Run on a manager that is running does nothing, and returns
		second := make(chan struct{})
		go func() { m.Run(context.Background()); close(second) }()
		synctest.Wait()
		select {
		case <-second:
		default:
			t.Fatal("a second Run did not return at once")
		}
		select {
		case <-done:
			t.Fatal("Run returned before it was closed")
		default:
		}

		must(t, m.Close(context.Background()))
		<-done
		must(t, m.Close(context.Background())) // and again
		m.Run(context.Background())            // once stopped, a manager stays so
	})
}

func TestRunEndsWithItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, _ := baseConfig(t)
		m, err := New(cfg)
		must(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { m.Run(ctx); close(done) }()
		synctest.Wait()
		cancel()
		<-done
		// nothing may start in a stopped manager
		if m.spawn(func(context.Context) { t.Error("started after the context ended") }) {
			t.Error("spawn started something in a stopped manager")
		}
		must(t, m.Close(context.Background()))
	})
}

func TestCloseBeforeRunIsFineAndKeepsRunFromStarting(t *testing.T) {
	cfg, _ := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	must(t, m.Close(context.Background()))
	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run on a closed manager did not return")
	}
}

func TestCloseGivesUpWhenItsContextDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg, _ := baseConfig(t)
		m, err := New(cfg)
		must(t, err)
		release := make(chan struct{})
		m.run, m.cancel = context.WithCancel(context.Background())
		m.spawn(func(context.Context) { <-release }) // a goroutine that ignores the stop
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Close(ctx); err == nil {
			t.Fatal("Close said everything stopped")
		}
		close(release)
		must(t, m.Close(context.Background()))
	})
}
