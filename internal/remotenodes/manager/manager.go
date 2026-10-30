// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

// Defaults for the fields of Config left at zero.
const (
	DefaultPollEvery   = 5 * time.Second
	DefaultStateEvery  = 5 * time.Second
	DefaultEntryEvery  = 30 * time.Second
	DefaultBridgeEvery = 2 * time.Second

	// DefaultStatusAddr is where the bridge serves its status page. It is
	// loopback, so only the hub in the same container can read it.
	DefaultStatusAddr = "127.0.0.1:18090"
)

// DefaultDNS are the resolvers the bridge's exit asks, through the VPN.
var DefaultDNS = []string{"1.1.1.1", "8.8.8.8"}

// Config says what the manager runs on. Store, Usage and Connector are
// required; the rest may be left at zero.
type Config struct {
	Store     Store
	Usage     Usage
	Connector Connector

	// Jobs, if nil, means no job ever runs, so no node is ever installing.
	Jobs Jobs
	// Bridge, if nil, means there is no bridge: the nodes and the router's
	// entry gate are still watched, and every tunnel reads as down.
	Bridge Bridge
	// Facts, if nil, means the router never reports, which keeps the entry off.
	Facts Facts
	// Resolver turns a node's host name into an address for the bridge's
	// spec. Nil means net.DefaultResolver; the hub gives it one that asks
	// from the nodes' source address, since a lookup from another would
	// leave through another link.
	Resolver Resolver

	// Addrs are the source addresses of the three roles, as netroles.Set.Check
	// marked them. The bridge dials from Nodes and leaves from Exit, and stays
	// stopped unless both are held here.
	Addrs netroles.Set
	// DNS are the bridge's resolvers, as IP addresses with an optional port.
	// Nil means DefaultDNS.
	DNS []string
	// StatusAddr is the bridge's status page, a loopback address and port. Empty
	// means DefaultStatusAddr.
	StatusAddr string

	// Now is the clock. Nil means time.Now.
	Now func() time.Time
	// Log, if nil, is slog.Default.
	Log *slog.Logger

	// How often each thing happens. Zero is the default.
	PollEvery   time.Duration // a node's stats
	StateEvery  time.Duration // looking at the state for an edit nobody announced
	EntryEvery  time.Duration // reading the router's report
	BridgeEvery time.Duration // the bridge's status, and the spec it should have
}

// Manager keeps the hub's live state. All its methods are safe for concurrent
// use, and the ones that answer for the page never wait for a node, the disk
// or the bridge.
//
// New makes one, Run starts its loops and returns when ctx ends or Close is
// called, and Close stops them and waits.
type Manager struct {
	cfg Config
	log *slog.Logger

	// The lifecycle. Run sets run, Close and the end of Run set closed, and
	// nothing starts once it is set.
	runMu  sync.Mutex
	run    context.Context
	cancel context.CancelFunc
	closed bool
	wg     sync.WaitGroup

	syncMu sync.Mutex // one Sync at a time

	// mu guards the live data below and everything the page reads.
	mu     sync.RWMutex
	ca     *pki.CA
	caErr  error
	caText string // the certificate text of the state's CA that ca was loaded from
	caSaid string // the last problem with the CA that was logged, so it is said once
}

// New checks the config and makes a manager. It makes the CA if the state has
// none and loads it otherwise, but starts nothing: that is Run's.
//
// It fails only for a config that can't work. A CA that can't be used, a
// router that has not reported and a bridge program that is missing are all
// things the hub starts without, and says so in the log.
func New(cfg Config) (*Manager, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("manager: no store")
	case cfg.Usage == nil:
		return nil, errors.New("manager: no usage collector")
	case cfg.Connector == nil:
		return nil, errors.New("manager: no connector")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Addrs == (netroles.Set{}) {
		cfg.Addrs = netroles.Defaults() // none of them held: the bridge stays stopped
	}
	if cfg.DNS == nil {
		cfg.DNS = DefaultDNS
	}
	if cfg.StatusAddr == "" {
		cfg.StatusAddr = DefaultStatusAddr
	}
	for _, d := range []struct {
		v   *time.Duration
		def time.Duration
	}{
		{&cfg.PollEvery, DefaultPollEvery}, {&cfg.StateEvery, DefaultStateEvery},
		{&cfg.EntryEvery, DefaultEntryEvery}, {&cfg.BridgeEvery, DefaultBridgeEvery},
	} {
		if *d.v <= 0 {
			*d.v = d.def
		}
	}
	m := &Manager{cfg: cfg, log: cfg.Log}
	m.checkCA()
	return m, nil
}

func (m *Manager) now() time.Time { return m.cfg.Now() }

// Addrs are the three source addresses, for the code that dials from them.
func (m *Manager) Addrs() netroles.Set { return m.cfg.Addrs }

// Run starts the loops and returns when ctx ends or Close is called, once they
// have stopped. A manager runs once: after Run returns, a second call does
// nothing.
func (m *Manager) Run(ctx context.Context) {
	run, ok := m.begin(ctx)
	if !ok {
		return
	}
	m.Sync()
	m.spawn(m.watchState)
	<-run.Done()
	// nothing may start while the rest are waited for
	m.runMu.Lock()
	m.closed = true
	m.runMu.Unlock()
	m.wg.Wait()
}

func (m *Manager) begin(parent context.Context) (context.Context, bool) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if m.closed || m.run != nil {
		return nil, false
	}
	m.run, m.cancel = context.WithCancel(parent)
	return m.run, true
}

// spawn runs fn in a goroutine that Close waits for. It does nothing, and says
// so, before Run or once the manager is stopping.
func (m *Manager) spawn(fn func(ctx context.Context)) bool {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if m.closed || m.run == nil || m.run.Err() != nil {
		return false
	}
	m.wg.Add(1)
	ctx := m.run
	go func() {
		defer m.wg.Done()
		fn(ctx)
	}()
	return true
}

// running is the context of Run, or nil when nothing is running.
func (m *Manager) running() context.Context {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if m.closed || m.run == nil || m.run.Err() != nil {
		return nil
	}
	return m.run
}

// Close stops everything Run started and waits for it, or for ctx to end,
// whichever comes first. It may be called more than once, and before Run.
func (m *Manager) Close(ctx context.Context) error {
	m.runMu.Lock()
	m.closed = true
	cancel := m.cancel
	m.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	idle := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// watchState looks at the state every few seconds, so that an edit nobody
// told the manager of is noticed too.
func (m *Manager) watchState(ctx context.Context) {
	t := time.NewTicker(m.cfg.StateEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Sync()
		}
	}
}

// Sync takes in the state as it is now. A slice that has edited the state
// calls it, so that a node it added is polled, and one it removed is let go,
// without waiting for the next look.
func (m *Manager) Sync() {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	m.checkCA()
}
