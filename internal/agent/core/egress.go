package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

// ErrDestinationBlocked is returned for destinations on this box or its networks.
var ErrDestinationBlocked = errors.New("destination not allowed")

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

const egressDialTimeout = 10 * time.Second

// Dialer opens tunnel egress connections. Destinations are resolved here and
// dialed as IP literals, so the block list applies to exactly what is dialed:
// a name that resolves to loopback, the LAN or this box's own addresses is
// refused the same as the literal would be. In router mode every socket
// carries the foreign group mark, so it follows the VPN pool (or its
// blackhole) the same way xray's direct-foreign does.
type Dialer struct {
	mark    uint32
	resolve func(ctx context.Context, host string) (netip.Addr, error)

	mu      sync.Mutex
	local   map[netip.Addr]struct{}
	localAt time.Time
}

// NewDialer returns a dialer that marks its sockets with mark (0 = unmarked)
// and resolves names with resolve (nil = the system resolver).
func NewDialer(mark uint32, resolve func(ctx context.Context, host string) (netip.Addr, error)) *Dialer {
	return &Dialer{mark: mark, resolve: resolve}
}

// DialContext dials address after resolving it and checking the block list.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip, err := d.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if d.blocked(ip) {
		return nil, fmt.Errorf("%w: %s", ErrDestinationBlocked, ip)
	}
	nd := &net.Dialer{Timeout: egressDialTimeout}
	if d.mark != 0 {
		nd = httpclient.MarkedDialer(d.mark, egressDialTimeout)
	}
	return nd.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

func (d *Dialer) lookup(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap(), nil
	}
	if d.resolve != nil {
		ip, err := d.resolve(ctx, host)
		return ip.Unmap(), err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, ip := range ips {
		if !d.blocked(ip.Unmap()) {
			return ip.Unmap(), nil
		}
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("no address for %s", host)
	}
	return ips[0].Unmap(), nil
}

func (d *Dialer) blocked(ip netip.Addr) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip) {
		return true
	}
	_, own := d.localAddrs()[ip]
	return own
}

// localAddrs caches this box's interface addresses for 30s.
func (d *Dialer) localAddrs() map[netip.Addr]struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.local != nil && time.Since(d.localAt) < 30*time.Second {
		return d.local
	}
	m := map[netip.Addr]struct{}{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil {
				m[p.Addr().Unmap()] = struct{}{}
			}
		}
	}
	d.local, d.localAt = m, time.Now()
	return m
}
