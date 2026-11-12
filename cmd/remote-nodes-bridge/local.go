// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/vless"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// localFlows follows what the entry's clients have open. The core tells
// nobody who is connected, so this watches where every flow passes: the
// system dialer, which sees the client's own connection in the flow's context.
//
// It also holds who may be connected at all. Taking a client off the inbound
// stops new logins, and Xray lets what is open run to its end; here the
// client's connections are closed, and a flow that slips in afterwards from a
// login made just before is closed too.
type localFlows struct {
	now func() time.Time

	mu      sync.Mutex
	allowed map[string]string       // email to id, the clients on the entry now
	clients map[string]*clientFlows // by email, every client seen since the start
}

type clientFlows struct {
	conns    map[any]*clientConn // the client's connections that carry a flow now
	lastSeen time.Time
}

// clientConn is one connection from a client to the entry.
type clientConn struct {
	conn  net.Conn // nil if the core didn't say; then it can't be closed from here
	addr  string   // where it comes from, without the port
	flows int
}

func newLocalFlows(now func() time.Time) *localFlows {
	return &localFlows{now: now, allowed: map[string]string{}, clients: map[string]*clientFlows{}}
}

// allow sets who may be connected, email to id.
func (f *localFlows) allow(clients map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowed = clients
}

// open notes a flow the client behind in has just started, and returns what
// to call when the flow ends. It returns nil for a client that is not on the
// entry any more, or is on it under another id, after closing its connection.
func (f *localFlows) open(in *session.Inbound) (done func()) {
	email, id := in.User.Email, userID(in.User)
	var key any = in.Conn
	if in.Conn == nil {
		key = in // still one entry for each connection, by its session
	}

	f.mu.Lock()
	if want, on := f.allowed[email]; !on || want != id {
		f.mu.Unlock()
		if in.Conn != nil {
			in.Conn.Close()
		}
		return nil
	}
	c := f.clients[email]
	if c == nil {
		c = &clientFlows{conns: map[any]*clientConn{}}
		f.clients[email] = c
	}
	c.lastSeen = f.now()
	cc := c.conns[key]
	if cc == nil {
		cc = &clientConn{conn: in.Conn}
		if in.Source.Address != nil {
			cc.addr = in.Source.Address.String()
		}
		c.conns[key] = cc
	}
	cc.flows++
	f.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			if cc.flows--; cc.flows <= 0 && c.conns[key] == cc {
				delete(c.conns, key)
			}
		})
	}
}

// userID is the id a client logged in with, lower case, or "" if the core's
// account isn't a VLESS one.
func userID(u *protocol.MemoryUser) string {
	if acct, ok := u.Account.(*vless.MemoryAccount); ok && acct.ID != nil {
		return strings.ToLower(acct.ID.String())
	}
	return ""
}

// cut closes every connection the client has to the entry and says how many
// there were. Their flows end with them.
func (f *localFlows) cut(email string) int {
	f.mu.Lock()
	c := f.clients[email]
	var conns []net.Conn
	n := 0
	if c != nil {
		n = len(c.conns)
		for _, cc := range c.conns {
			if cc.conn != nil {
				conns = append(conns, cc.conn)
			}
		}
		c.conns = map[any]*clientConn{}
	}
	f.mu.Unlock()
	// outside the lock: a close ends flows, and each of those calls back in
	for _, conn := range conns {
		conn.Close()
	}
	return n
}

// snapshot is what one client has open now.
func (f *localFlows) snapshot(email string) (connections int, addrs []string, lastSeen time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.clients[email]
	if c == nil {
		return 0, nil, time.Time{}
	}
	for _, cc := range c.conns {
		if cc.addr != "" && !slices.Contains(addrs, cc.addr) {
			addrs = append(addrs, cc.addr)
		}
	}
	slices.Sort(addrs)
	return len(c.conns), addrs, c.lastSeen
}

// localTCP is a TCP connection one of the entry's clients opened to the
// internet. Embedding the TCP connection keeps the rest of its methods for
// the core.
type localTCP struct {
	*net.TCPConn
	done func()
}

func (c *localTCP) Close() error {
	c.done()
	return c.TCPConn.Close()
}

// localPacket is the socket under a UDP flow of one of the entry's clients.
// It goes inside the dialer's own wrapper, whose type the core's packet code
// looks for, so only the socket's close is seen here.
type localPacket struct {
	net.PacketConn
	done func()
}

func (c *localPacket) Close() error {
	c.done()
	return c.PacketConn.Close()
}

// userManager is the part of the entry's inbound that takes clients on and
// off while it runs. The VLESS inbound's proxy.UserManager fits.
type userManager interface {
	AddUser(context.Context, *protocol.MemoryUser) error
	RemoveUser(context.Context, string) error
}

// entry is the router's own entry while the bridge runs: its clients, and the
// way to change them.
type entry struct {
	flow  string
	token string
	tags  []string // the tunnels' tags, which no client may be named like
	flows *localFlows
	users userManager // set by attach, before the bridge starts

	mu      sync.Mutex        // one change of clients at a time
	clients map[string]string // email to id, as the inbound has them
	seen    []string          // every email since the start, sorted
}

// newEntry takes the clients the spec starts the inbound with.
func newEntry(spec *bridgespec.Spec, flows *localFlows) *entry {
	e := &entry{flow: spec.Local.Flow, token: spec.Local.Token, flows: flows, clients: map[string]string{}}
	for _, n := range spec.Nodes {
		e.tags = append(e.tags, n.Tag)
	}
	for _, c := range spec.Local.Clients {
		e.clients[c.Email] = strings.ToLower(c.UUID)
		e.see(c.Email)
	}
	flows.allow(e.allowedCopy())
	return e
}

func (e *entry) see(email string) {
	if i, found := slices.BinarySearch(e.seen, email); !found {
		e.seen = slices.Insert(e.seen, i, email)
	}
}

func (e *entry) allowedCopy() map[string]string {
	out := make(map[string]string, len(e.clients))
	for email, id := range e.clients {
		out[email] = id
	}
	return out
}

// attach finds the inbound's user manager in a built instance.
func (e *entry) attach(inst *core.Instance) error {
	im, ok := inst.GetFeature(inbound.ManagerType()).(inbound.Manager)
	if !ok {
		return errors.New("the instance has no inbound manager")
	}
	h, err := im.GetHandler(context.Background(), bridgespec.LocalTag)
	if err != nil {
		return err
	}
	gi, ok := h.(proxy.GetInbound)
	if !ok {
		return errors.New("the entry's handler doesn't give out its inbound")
	}
	um, ok := gi.GetInbound().(proxy.UserManager)
	if !ok {
		return errors.New("the entry's inbound can't change its users")
	}
	e.users = um
	return nil
}

// errBadClients is a set the entry refuses as a whole; nothing was changed.
var errBadClients = errors.New("bad clients")

// set makes the inbound's clients the given set, without a restart: those not
// in it are taken off and what they have open is closed, new ones are added,
// and one whose id changed is taken off and put back. If a step fails the
// clients are left as far as it got, and sending the set again goes on from
// there.
func (e *entry) set(ctx context.Context, want []bridgespec.Client) (bridgespec.ClientsResult, error) {
	res := bridgespec.ClientsResult{Added: []string{}, Removed: []string{}, Replaced: []string{}}
	if err := bridgespec.CheckClients(want, e.tags); err != nil {
		return res, fmt.Errorf("%w: %v", errBadClients, err)
	}
	wanted := make(map[string]bridgespec.Client, len(want))
	for _, c := range want {
		wanted[c.Email] = c
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	// whatever happens below, the flows follow what the inbound really has
	defer func() { e.flows.allow(e.allowedCopy()) }()

	// Off first: a client that is both leaving and being counted against a
	// limit should stop as early as this call can make it.
	var gone []string
	for email, id := range e.clients {
		if c, keep := wanted[email]; !keep || strings.ToLower(c.UUID) != id {
			gone = append(gone, email)
		}
	}
	slices.Sort(gone)
	replaced := map[string]bool{}
	for _, email := range gone {
		if err := e.users.RemoveUser(ctx, email); err != nil && !notThere(err, email) {
			return res, fmt.Errorf("take %s off: %w", email, err)
		}
		delete(e.clients, email)
		// before the cut, so a flow that arrives during it is turned away
		e.flows.allow(e.allowedCopy())
		res.Closed += e.flows.cut(email)
		if _, back := wanted[email]; back {
			replaced[email] = true
		} else {
			res.Removed = append(res.Removed, email)
		}
	}

	var fresh []string
	for email := range wanted {
		if _, has := e.clients[email]; !has {
			fresh = append(fresh, email)
		}
	}
	slices.Sort(fresh)
	for _, email := range fresh {
		c := wanted[email]
		u, err := localUser(c, e.flow)
		if err != nil {
			return res, err
		}
		mu, err := u.ToMemoryUser()
		if err != nil {
			return res, fmt.Errorf("%s: %w", email, err)
		}
		if err := e.users.AddUser(ctx, mu); err != nil {
			return res, fmt.Errorf("put %s on: %w", email, err)
		}
		e.clients[email] = strings.ToLower(c.UUID)
		e.see(email)
		if replaced[email] {
			res.Replaced = append(res.Replaced, email)
		} else {
			res.Added = append(res.Added, email)
		}
	}
	return res, nil
}

// notThere is Xray's answer to removing a user the inbound doesn't have,
// "User <email> not found.", which leaves things as the caller wanted them.
func notThere(err error, email string) bool {
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower("user "+email+" not found"))
}

// status is the entry as the status page shows it. counter reads one of the
// core's byte counters.
func (e *entry) status(counter func(name string) uint64) *bridgespec.LocalStatus {
	e.mu.Lock()
	seen := slices.Clone(e.seen)
	on := e.allowedCopy()
	e.mu.Unlock()

	st := &bridgespec.LocalStatus{Clients: make([]bridgespec.ClientStatus, len(seen))}
	for i, email := range seen {
		conns, addrs, last := e.flows.snapshot(email)
		_, isOn := on[email]
		st.Clients[i] = bridgespec.ClientStatus{
			Email:       email,
			On:          isOn,
			BytesUp:     counter("user>>>" + email + ">>>traffic>>>uplink"),
			BytesDown:   counter("user>>>" + email + ">>>traffic>>>downlink"),
			Connections: conns,
			Addrs:       addrs,
			LastSeen:    last,
		}
	}
	return st
}

// serveClients answers the hub's change of clients.
func (e *entry) serveClients(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(bridgespec.TokenHeader)), []byte(e.token)) != 1 {
		http.Error(w, "wrong or missing token", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, bridgespec.MaxSize+1))
	if err != nil || len(body) > bridgespec.MaxSize {
		http.Error(w, "the request is too large or could not be read", http.StatusBadRequest)
		return
	}
	var req bridgespec.ClientsRequest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "the request is not a set of clients: "+err.Error(), http.StatusBadRequest)
		return
	}
	// not the request's context: the hub hanging up must not leave the set half made
	res, err := e.set(context.WithoutCancel(r.Context()), req.Clients)
	switch {
	case errors.Is(err, errBadClients):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}
