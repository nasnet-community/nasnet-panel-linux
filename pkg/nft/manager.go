package nft

import (
	"context"
	"sync"
)

// Manager is the only writer of `table inet nasnet`. Bandwidth shaping, the
// dual-WAN reconciler and (later) the LAN and port-forward code all mutate one
// shared Ruleset through it
type Manager struct {
	mu      sync.Mutex
	applier Applier
	rs      Ruleset
}

func NewManager(a Applier) *Manager {
	return &Manager{applier: a}
}

// Update applies mutate to the live ruleset and rerenders the whole table.
func (m *Manager) Update(ctx context.Context, mutate func(*Ruleset)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.rs
	next := m.rs
	mutate(&next)

	if err := m.applier.Apply(ctx, next.Render()); err != nil {
		m.rs = prev
		return err
	}
	m.rs = next
	return nil
}

// Snapshot returns a copy of the live ruleset.
func (m *Manager) Snapshot() Ruleset {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.rs
	if m.rs.IngressPins != nil {
		out.IngressPins = append([]Pin(nil), m.rs.IngressPins...)
	}
	return out
}

// Replace sets the desired state wholesale and reapplies
func (m *Manager) Replace(ctx context.Context, rs Ruleset) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.rs
	if err := m.applier.Apply(ctx, rs.Render()); err != nil {
		m.rs = prev
		return err
	}
	m.rs = rs
	return nil
}

// Teardown removes the table and clears the desired state.
func (m *Manager) Teardown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.applier.Delete(ctx); err != nil {
		return err
	}
	m.rs = Ruleset{}
	return nil
}
