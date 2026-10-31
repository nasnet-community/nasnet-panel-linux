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
}

func (b *fakeBridge) Update(spec *bridgespec.Spec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updates++
	cp := *spec
	cp.Nodes = slices.Clone(spec.Nodes)
	if b.failUpdate != nil {
		if b.failState != "" {
			b.state, b.spec = b.failState, &cp
		}
		return b.failUpdate
	}
	b.specs = append(b.specs, &cp)
	b.spec, b.state = &cp, bridgectl.StateRunning
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
		for _, n := range b.spec.Nodes {
			ns := b.nodes[n.Tag]
			ns.Tag = n.Tag
			page.Nodes = append(page.Nodes, ns)
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
