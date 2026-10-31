// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

const (
	// resolveTTL is how long a name's address is trusted. The bridge is
	// restarted when it changes, so it is not asked for more often than a
	// change of address is worth.
	resolveTTL = 5 * time.Minute
	// resolveRetry is how long after a failed lookup the next one is made.
	resolveRetry   = 30 * time.Second
	resolveTimeout = 3 * time.Second
)

var errNoAddress = errors.New("the name has no address of the family the bridge dials from")

// resolved is what the manager remembers of a node's host name.
type resolved struct {
	host  string
	addr  netip.Addr // the last good answer, invalid before there is one
	at    time.Time  // when it was got
	tried time.Time  // the last lookup, good or not
	err   string     // how the last lookup failed, "" if it didn't
}

// addrOf is the IP address the bridge dials a node at. A node given as an
// address is dialled there. One given as a name is looked up through the
// resolver, which asks from the nodes' source address, and the answer is kept
// for resolveTTL. A lookup that fails keeps the last good address, which is
// right more often than not for a name that has worked, and leaves a node
// that never had one out; the log says which, once, and the lookup is made
// again after resolveRetry.
//
// The bridge can't resolve it itself, since a lookup from its dial address
// would not be the one the node-bound traffic uses, and the spec has no place
// for a name.
func (m *Manager) addrOf(ctx context.Context, n model.Node, dial netip.Addr) (netip.Addr, error) {
	if a, err := netip.ParseAddr(n.Host); err == nil {
		return a.Unmap(), nil
	}
	now := m.now()
	m.resMu.Lock()
	r := m.res[n.ID]
	if r == nil || r.host != n.Host {
		r = &resolved{host: n.Host}
		m.res[n.ID] = r
	}
	cur := *r
	m.resMu.Unlock()

	switch {
	case cur.addr.IsValid() && now.Sub(cur.at) < resolveTTL:
		return cur.addr, nil
	case cur.err != "" && now.Sub(cur.tried) < resolveRetry:
		return stale(cur)
	}

	addr, err := m.lookup(ctx, n.Host, dial)
	m.resMu.Lock()
	r.tried = now
	if err == nil {
		r.addr, r.at, r.err = addr, now, ""
	} else {
		r.err = err.Error()
	}
	next := *r
	m.resMu.Unlock()

	if err == nil {
		if cur.err != "" {
			m.log.Info("a node's host name resolves again", "node", n.ID, "host", n.Host, "addr", addr)
		}
		return addr, nil
	}
	if ctx.Err() == nil && next.err != cur.err {
		m.log.Warn("could not resolve a node's host name", "node", n.ID, "host", n.Host, "err", err,
			"keeping", keeping(next))
	}
	return stale(next)
}

// stale is the answer for a name that can't be looked up now: its last good
// address, or the reason there is none.
func stale(r resolved) (netip.Addr, error) {
	if r.addr.IsValid() {
		return r.addr, nil
	}
	return netip.Addr{}, errors.New("the host name does not resolve: " + r.err)
}

func keeping(r resolved) string {
	if r.addr.IsValid() {
		return r.addr.String()
	}
	return "nothing: the node is left out"
}

// lookup asks the resolver for addresses of the family the bridge dials from
// and takes the first usable one.
func (m *Manager) lookup(ctx context.Context, host string, dial netip.Addr) (netip.Addr, error) {
	rs := m.cfg.Resolver
	if rs == nil {
		rs = net.DefaultResolver
	}
	network := "ip6"
	if dial.Is4() {
		network = "ip4"
	}
	lctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	addrs, err := rs.LookupNetIP(lctx, network, host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, a := range addrs {
		if a = a.Unmap(); a.Is4() == dial.Is4() && !a.IsUnspecified() && !a.IsMulticast() {
			return a, nil
		}
	}
	return netip.Addr{}, errNoAddress
}

// forgetResolved drops what is remembered of nodes that are not in the state.
func (m *Manager) forgetResolved(st model.State) {
	m.resMu.Lock()
	defer m.resMu.Unlock()
	for id := range m.res {
		if st.FindNode(id) == nil {
			delete(m.res, id)
		}
	}
}
