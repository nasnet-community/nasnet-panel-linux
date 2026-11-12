// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// fakeBridge is a supervisor that runs nothing: it holds the spec it was
// given and says what the bridge's page would.
type fakeBridge struct {
	mu sync.Mutex

	state bridgectl.State
	spec  *bridgespec.Spec

	specs   []*bridgespec.Spec // every spec Update was given and took, in order
	updates int                // every call to Update, failed ones too
	stops   int

	failUpdate error                            // Update fails with it
	failState  bridgectl.State                  // and leaves the supervisor in this state, "" meaning stopped
	pageDown   bool                             // the bridge's own page doesn't answer
	nodes      map[string]bridgespec.NodeStatus // what the page says, by tag

	// The router's own entry, when the spec has one.
	started     time.Time                          // when this bridge started; the epoch unless a test says
	clients     []bridgespec.Client                // the entry's clients as the bridge holds them
	sets        [][]bridgespec.Client              // every set SetClients took, in order
	failClients error                              // SetClients fails with it
	local       map[string]bridgespec.ClientStatus // what the page says of a client, by email
}

// SetClients takes the entry's clients, as the real bridge does: the whole
// set, and only while it runs a spec that has the entry.
func (b *fakeBridge) SetClients(clients []bridgespec.Client) (bridgespec.ClientsResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	res := bridgespec.ClientsResult{Added: []string{}, Removed: []string{}, Replaced: []string{}}
	if b.state != bridgectl.StateRunning || b.spec == nil || b.spec.Local == nil {
		return res, bridgectl.ErrNoEntry
	}
	if b.failClients != nil {
		return res, b.failClients
	}
	had := map[string]string{}
	for _, c := range b.clients {
		had[c.Email] = c.UUID
	}
	for _, c := range clients {
		switch id, was := had[c.Email]; {
		case !was:
			res.Added = append(res.Added, c.Email)
		case id != c.UUID:
			res.Replaced = append(res.Replaced, c.Email)
		}
		delete(had, c.Email)
	}
	for email := range had {
		res.Removed = append(res.Removed, email)
	}
	slices.Sort(res.Removed)
	b.clients = slices.Clone(clients)
	b.sets = append(b.sets, slices.Clone(clients))
	return res, nil
}

// emails is who the bridge holds on the entry, in the order it was given.
func (b *fakeBridge) emails() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []string{}
	for _, c := range b.clients {
		out = append(out, c.Email)
	}
	return out
}

func (b *fakeBridge) setCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.sets)
}

// client says what the bridge's page says of one of the entry's clients.
func (b *fakeBridge) client(cs bridgespec.ClientStatus) {
	b.set(func(b *fakeBridge) {
		if b.local == nil {
			b.local = map[string]bridgespec.ClientStatus{}
		}
		b.local[cs.Email] = cs
	})
}

func (b *fakeBridge) Update(spec *bridgespec.Spec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updates++
	cp := *spec
	cp.Nodes = slices.Clone(spec.Nodes)
	if spec.Local != nil {
		l := *spec.Local
		l.Clients = slices.Clone(spec.Local.Clients)
		cp.Local = &l
	}
	if b.failUpdate != nil {
		if b.failState != "" {
			b.state, b.spec = b.failState, &cp
		}
		return b.failUpdate
	}
	b.specs = append(b.specs, &cp)
	b.spec, b.state = &cp, bridgectl.StateRunning
	// a bridge that starts holds the clients its spec came with
	b.clients = nil
	if cp.Local != nil {
		b.clients = slices.Clone(cp.Local.Clients)
	}
	return nil
}

func (b *fakeBridge) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stops++
	b.spec, b.state = nil, bridgectl.StateStopped
}

func (b *fakeBridge) Status() bridgectl.Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := bridgectl.Status{State: b.state}
	if st.State == "" {
		st.State = bridgectl.StateStopped
	}
	if st.State == bridgectl.StateRunning && !b.pageDown {
		page := &bridgespec.Status{Running: true, Started: epoch, Xray: pin.Version}
		if !b.started.IsZero() {
			page.Started = b.started
		}
		for _, n := range b.spec.Nodes {
			ns := b.nodes[n.Tag]
			ns.Tag = n.Tag
			page.Nodes = append(page.Nodes, ns)
		}
		if b.spec.Local != nil {
			page.Local = &bridgespec.LocalStatus{Clients: []bridgespec.ClientStatus{}}
			on := map[string]bool{}
			for _, c := range b.clients {
				on[c.Email] = true
				cs := b.local[c.Email]
				cs.Email, cs.On = c.Email, true
				page.Local.Clients = append(page.Local.Clients, cs)
			}
			for email, cs := range b.local {
				if !on[email] {
					cs.Email, cs.On = email, false
					page.Local.Clients = append(page.Local.Clients, cs)
				}
			}
		}
		st.Bridge = page
	}
	return st
}

func (b *fakeBridge) set(f func(*fakeBridge)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f(b)
}

// tunnel says what the bridge's page says of a node's tunnel.
func (b *fakeBridge) tunnel(tag string, ns bridgespec.NodeStatus) {
	b.set(func(b *fakeBridge) {
		if b.nodes == nil {
			b.nodes = map[string]bridgespec.NodeStatus{}
		}
		b.nodes[tag] = ns
	})
}

func (b *fakeBridge) counts() (updates, stops int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.specs), b.stops
}

// last is the spec the bridge holds, nil when it runs none.
func (b *fakeBridge) last() *bridgespec.Spec {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spec
}

func (b *fakeBridge) tags() []string {
	var out []string
	if s := b.last(); s != nil {
		for _, n := range s.Nodes {
			out = append(out, n.Tag)
		}
	}
	return out
}

// fakeResolver answers from a table, and keeps count.
type fakeResolver struct {
	mu    sync.Mutex
	table map[string][]netip.Addr
	err   map[string]error
	calls []string
}

func (r *fakeResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, network+" "+host)
	if err := r.err[host]; err != nil {
		return nil, err
	}
	return slices.Clone(r.table[host]), nil
}

func (r *fakeResolver) set(host string, addrs ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.table == nil {
		r.table = map[string][]netip.Addr{}
	}
	r.table[host] = nil
	for _, a := range addrs {
		r.table[host] = append(r.table[host], netip.MustParseAddr(a))
	}
}

func (r *fakeResolver) fail(host string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = map[string]error{}
	}
	r.err[host] = err
}

func (r *fakeResolver) lookups() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// heldAddrs are the three addresses as a router has them: held here.
func heldAddrs() netroles.Set {
	return netroles.Defaults().Check(func(netip.Addr) bool { return true }, nil)
}

// bridgeRig is a rig whose router is healthy, with a fake bridge and
// resolver.
type bridgeRig struct {
	*rig
	bridge *fakeBridge
	dns    *fakeResolver
	facts  *fakeFacts
}

func newBridgeRig(t *testing.T, mods ...func(*Config)) *bridgeRig {
	t.Helper()
	b, rs, f := &fakeBridge{}, &fakeResolver{}, newFacts(ok(healthy(epoch)))
	setup := func(c *Config) {
		c.Bridge, c.Resolver, c.Facts, c.Addrs = b, rs, f, heldAddrs()
	}
	return &bridgeRig{rig: newRig(t, append([]func(*Config){setup}, mods...)...), bridge: b, dns: rs, facts: f}
}

// flip sets what the router reports and waits for the manager to read it.
func (r *bridgeRig) flip(f func(*routerfacts.Facts)) {
	h := healthy(epoch)
	f(&h)
	r.facts.set(ok(h))
	r.advance(31 * time.Second)
}

// tunnelNow is what the manager says a node's tunnel is doing.
func (m *Manager) tunnelNow(id uint) api.TunnelState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnelLocked(id)
}

// up is a tunnel status that says the tunnel has been up since the epoch.
func up(streams int, down, upBytes uint64) bridgespec.NodeStatus {
	return bridgespec.NodeStatus{Up: true, Since: epoch, Connections: streams, BytesDown: down, BytesUp: upBytes}
}
