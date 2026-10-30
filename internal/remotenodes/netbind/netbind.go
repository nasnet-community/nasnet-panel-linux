// SPDX-License-Identifier: AGPL-3.0-or-later

// Package netbind makes connections that leave from one local address. The
// router sends a container's traffic by its source address, so the address a
// connection starts from decides where it goes: downloads, nodes and the
// tunnel's exit each have one of their own.
//
// A name has to be looked up from the same address, or the lookup would be
// routed somewhere else, so the dialer carries a resolver bound to it too.
// The package imports only the standard library.
package netbind

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"time"
)

const dialTimeout = 15 * time.Second

// Dialer returns a dialer whose connections, and the DNS lookups it makes for
// a host name, leave from local. An invalid or unspecified address leaves the
// choice to the system, which is what a development machine wants.
func Dialer(local netip.Addr) *net.Dialer {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	if local = local.Unmap(); local.IsValid() && !local.IsUnspecified() {
		d.LocalAddr = &net.TCPAddr{IP: local.AsSlice(), Zone: local.Zone()}
		// the system's resolver would ask from whichever address it likes
		d.Resolver = &net.Resolver{PreferGo: true, Dial: boundDial(local)}
	}
	return d
}

// boundDial dials from local, as the resolver wants for UDP and for TCP.
func boundDial(local netip.Addr) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{Timeout: dialTimeout}
		if strings.HasPrefix(network, "udp") {
			d.LocalAddr = &net.UDPAddr{IP: local.AsSlice(), Zone: local.Zone()}
		} else {
			d.LocalAddr = &net.TCPAddr{IP: local.AsSlice(), Zone: local.Zone()}
		}
		return d.DialContext(ctx, network, address)
	}
}
