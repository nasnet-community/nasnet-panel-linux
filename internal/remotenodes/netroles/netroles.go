// SPDX-License-Identifier: AGPL-3.0-or-later

// Package netroles names the three addresses the plugin's veth holds and what
// each is for. The router routes by source address alone: downloads leave
// through the foreign link, the bridge's exit through the VPN, and everything
// that goes to a node through the Domestic link. A socket that doesn't bind
// the address of its role would leave from the wrong one, so the hub binds
// each to its own.
//
// An address that no interface here holds, as on a laptop or a lab machine,
// can't be bound. For the hub's own sockets that means running unbound, with
// one warning; the bridge is held to a stricter rule (see the manager).
package netroles

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
)

// The environment variables that name the three addresses.
const (
	EnvDownloads = "ADDR_FOREIGN"
	EnvExit      = "ADDR_EXIT"
	EnvNodes     = "ADDR_NODES"
)

// Addr is one role's source address.
type Addr struct {
	// Name is what the address is called in messages: the variable that sets it.
	Name string
	IP   netip.Addr
	// Local is whether an interface here holds IP. Only then can a socket bind
	// it, and Check is what finds out.
	Local bool
}

func (a Addr) String() string { return a.IP.String() }

// DialContext dials from the address, or from wherever the system chooses
// when it isn't held here. It has the shape of net.Dialer.DialContext, so it
// can be a gRPC context dialer, an http.Transport's DialContext or a resolver's
// Dial.
func (a Addr) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	if a.Local {
		// the type of the local address has to be the network's
		switch {
		case strings.HasPrefix(network, "tcp"):
			d.LocalAddr = &net.TCPAddr{IP: a.IP.AsSlice()}
		case strings.HasPrefix(network, "udp"):
			d.LocalAddr = &net.UDPAddr{IP: a.IP.AsSlice()}
		}
	}
	return d.DialContext(ctx, network, address)
}

// Resolver looks names up from the address, so the query leaves where the
// traffic to the name's owner will. Unbound, it is the system's resolver.
func (a Addr) Resolver() *net.Resolver {
	if !a.Local {
		return net.DefaultResolver
	}
	// PreferGo: the system's own resolver wouldn't ask the Dial below
	return &net.Resolver{PreferGo: true, Dial: a.DialContext}
}

// Set is the three roles' addresses.
type Set struct {
	Downloads Addr // release downloads, and anything else that isn't node-bound
	Exit      Addr // where the bridge's traffic leaves, through the router's VPN
	Nodes     Addr // ssh and gRPC to the nodes, and the bridge's dial to the portals
}

// Defaults are the addresses the plugin's manifest gives the veth.
func Defaults() Set {
	return Set{
		Downloads: Addr{Name: EnvDownloads, IP: netip.MustParseAddr(routerfacts.AddrDownloads)},
		Exit:      Addr{Name: EnvExit, IP: netip.MustParseAddr(routerfacts.AddrExit)},
		Nodes:     Addr{Name: EnvNodes, IP: netip.MustParseAddr(routerfacts.AddrNodes)},
	}
}

// FromEnv reads the three addresses, each falling back to its default when
// the variable is empty. Whether they are held here is not looked at; Check
// does that.
//
// The exit and the nodes' address must differ, or the exit would leave
// through the Domestic link. The bridge makes the same rule, with the same
// exception for a loopback address, which a dev run uses for both.
func FromEnv(getenv func(string) string) (Set, error) {
	s := Defaults()
	for _, a := range []*Addr{&s.Downloads, &s.Exit, &s.Nodes} {
		v := strings.TrimSpace(getenv(a.Name))
		if v == "" {
			continue
		}
		ip, err := parse(v)
		if err != nil {
			return Set{}, fmt.Errorf("%s %q: %w", a.Name, v, err)
		}
		a.IP = ip
	}
	if s.Exit.IP == s.Nodes.IP && !s.Exit.IP.IsLoopback() {
		return Set{}, fmt.Errorf("%s and %s are both %s: the bridge's exit would leave through the Domestic link", EnvExit, EnvNodes, s.Exit.IP)
	}
	return s, nil
}

func parse(v string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(v)
	switch {
	case err != nil || ip.Zone() != "":
		return netip.Addr{}, fmt.Errorf("not an IP address")
	case ip.Unmap().IsUnspecified() || ip.Unmap().IsMulticast():
		return netip.Addr{}, fmt.Errorf("not an address a socket can leave from")
	}
	return ip.Unmap(), nil
}

// Check marks which addresses are held here, and calls warn once for each
// that isn't, with a sentence for the log. It returns the marked copy.
func (s Set) Check(held func(netip.Addr) bool, warn func(string)) Set {
	for _, c := range []struct {
		a    *Addr
		what string
	}{
		{&s.Downloads, "downloads leave from whatever address the system picks"},
		{&s.Exit, "the bridge stays stopped: its exit must not leave from another address"},
		{&s.Nodes, "connections to the nodes leave from whatever address the system picks, and the bridge stays stopped"},
	} {
		c.a.Local = held(c.a.IP)
		if !c.a.Local && warn != nil {
			warn(fmt.Sprintf("%s %s is not assigned to any interface here, so %s", c.a.Name, c.a.IP, c.what))
		}
	}
	return s
}

// Held says whether an interface of this machine holds an address.
func Held() func(netip.Addr) bool {
	return func(ip netip.Addr) bool {
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return false
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if have, ok := netip.AddrFromSlice(n.IP); ok && have.Unmap() == ip.Unmap() {
					return true
				}
			}
		}
		return false
	}
}
