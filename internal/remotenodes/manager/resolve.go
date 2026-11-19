// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
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
	return m.resolve(ctx, nameLookup{
		cache: m.res, id: n.ID, host: n.Host, from: dial, rs: m.cfg.Resolver,
		what: "node", whose: "a node's", none: "nothing: the node is left out",
	})
}

// outAddrOf is the IP address the bridge reaches an outbound at, looked up
// from the Foreign link the way addrOf looks up a node's from the Domestic one.
// A name with several addresses keeps the one it had for as long as it still
// has it: a new address is a new spec, and so a restart of the bridge.
func (m *Manager) outAddrOf(ctx context.Context, o model.Outbound) (netip.Addr, error) {
	return m.resolve(ctx, nameLookup{
		cache: m.outRes, id: o.ID, host: o.Settings.Host, from: m.cfg.Addrs.Downloads.IP, rs: m.cfg.OutboundResolver,
		sticky: true, what: "outbound", whose: "an outbound's", none: "nothing",
	})
}

// nameLookup is a host name to look up and where to keep what is found.
type nameLookup struct {
	cache  map[uint]*resolved // under resMu
	id     uint
	host   string
	from   netip.Addr // the address it is reached from, whose family the answer has to have
	rs     Resolver
	sticky bool // keep the last answer while the name still has it

	// what and whose name the record in the log, and none says what is kept
	// when a name that never resolved fails again
	what, whose, none string
}

func (m *Manager) resolve(ctx context.Context, q nameLookup) (netip.Addr, error) {
	if a, err := netip.ParseAddr(q.host); err == nil {
		return a.Unmap(), nil
	}
	now := m.now()
	m.resMu.Lock()
	r := q.cache[q.id]
	if r == nil || r.host != q.host {
		r = &resolved{host: q.host}
		q.cache[q.id] = r
	}
	cur := *r
	m.resMu.Unlock()

	switch {
	case cur.addr.IsValid() && now.Sub(cur.at) < resolveTTL:
		return cur.addr, nil
	case cur.err != "" && now.Sub(cur.tried) < resolveRetry:
		return stale(cur)
	}

	addrs, err := m.lookupAll(ctx, q.rs, q.host, q.from)
	var addr netip.Addr
	if err == nil {
		addr = addrs[0]
		if q.sticky && slices.Contains(addrs, cur.addr) {
			addr = cur.addr
		}
	}
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
			m.log.Info(q.whose+" host name resolves again", q.what, q.id, "host", q.host, "addr", addr)
		}
		return addr, nil
	}
	if ctx.Err() == nil && next.err != cur.err {
		m.log.Warn("could not resolve "+q.whose+" host name", q.what, q.id, "host", q.host, "err", err,
			"keeping", keeping(next, q.none))
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

func keeping(r resolved, none string) string {
	if r.addr.IsValid() {
		return r.addr.String()
	}
	return none
}

// lookupAll asks the resolver for the addresses of the family of from, the
// address they are reached from, and gives the usable ones in its order.
func (m *Manager) lookupAll(ctx context.Context, rs Resolver, host string, from netip.Addr) ([]netip.Addr, error) {
	if rs == nil {
		rs = net.DefaultResolver
	}
	network := "ip6"
	if from.Is4() {
		network = "ip4"
	}
	lctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	addrs, err := rs.LookupNetIP(lctx, network, host)
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, a := range addrs {
		if a = a.Unmap(); a.Is4() == from.Is4() && !a.IsUnspecified() && !a.IsMulticast() {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, errNoAddress
	}
	return out, nil
}

// forgetResolved drops what is remembered of nodes and outbounds that are not
// in the state.
func (m *Manager) forgetResolved(st model.State) {
	m.resMu.Lock()
	defer m.resMu.Unlock()
	for id := range m.res {
		if st.FindNode(id) == nil {
			delete(m.res, id)
		}
	}
	for id := range m.outRes {
		if st.FindOutbound(id) == nil {
			delete(m.outRes, id)
		}
	}
}
