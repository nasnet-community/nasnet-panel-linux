//go:build linux

package system

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type netlinkBackend struct{}

// NewNetlinkBackend returns the privileged Backend
func NewNetlinkBackend() (Backend, error) { return &netlinkBackend{}, nil }

func (b *netlinkBackend) Links(context.Context) ([]string, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("link list: %w", err)
	}
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, l.Attrs().Name)
	}
	return out, nil
}

func (b *netlinkBackend) Addrs(context.Context) ([]Addr, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("link list: %w", err)
	}
	var out []Addr
	for _, l := range links {
		as, err := netlink.AddrList(l, unix.AF_INET)
		if err != nil {
			continue
		}
		for _, a := range as {
			out = append(out, Addr{IfName: l.Attrs().Name, CIDR: a.IPNet.String()})
		}
	}
	return out, nil
}

func (b *netlinkBackend) toNetlinkRule(r Rule) *netlink.Rule {
	nr := netlink.NewRule()
	nr.Priority = r.Pref
	if r.FwMask != 0 {
		nr.Mark = r.FwMark
		mask := r.FwMask
		nr.Mask = &mask
	}
	if r.OifName != "" {
		nr.OifName = r.OifName
	}
	if r.Blackhole {
		nr.Type = unix.RTN_BLACKHOLE
		nr.Table = 0
	} else {
		nr.Table = r.Table
	}
	if r.SuppressSet {
		nr.SuppressPrefixlen = r.SuppressPrefixLen
	}
	return nr
}

func (b *netlinkBackend) RuleAdd(_ context.Context, r Rule) error {
	if err := netlink.RuleAdd(b.toNetlinkRule(r)); err != nil {
		if os.IsExist(err) || strings.Contains(err.Error(), "file exists") {
			return nil // idempotent
		}
		return fmt.Errorf("rule add pref %d: %w", r.Pref, err)
	}
	return nil
}

func (b *netlinkBackend) RuleDel(_ context.Context, r Rule) error {
	if err := netlink.RuleDel(b.toNetlinkRule(r)); err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such") {
			return nil // idempotent
		}
		return fmt.Errorf("rule del pref %d: %w", r.Pref, err)
	}
	return nil
}

func (b *netlinkBackend) RuleList(context.Context) ([]Rule, error) {
	rules, err := netlink.RuleList(unix.AF_INET)
	if err != nil {
		return nil, fmt.Errorf("rule list: %w", err)
	}
	out := make([]Rule, 0, len(rules))
	for _, nr := range rules {
		r := Rule{Pref: nr.Priority, FwMark: nr.Mark, Table: nr.Table, OifName: nr.OifName}
		if nr.Mask != nil {
			r.FwMask = *nr.Mask
		}
		// NewRule leaves SuppressPrefixlen at -1, so >= 0 means it was set.
		if nr.SuppressPrefixlen >= 0 {
			r.SuppressSet, r.SuppressPrefixLen = true, nr.SuppressPrefixlen
		}
		// Type never round-trips; table 0 + no suppress = blackhole (kernel rules use 255/254/253).
		r.Blackhole = nr.Table == 0 && !r.SuppressSet
		out = append(out, r)
	}
	return out, nil
}

func (b *netlinkBackend) toNetlinkRoute(r Route) (*netlink.Route, error) {
	nr := &netlink.Route{Table: r.Table, Priority: r.Metric}
	if r.Dest == "" || r.Dest == "default" {
		// netlink needs Dst, Src, or Gw set; nil Dst can't express 0.0.0.0/0.
		nr.Dst = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	} else {
		dst, err := netlink.ParseIPNet(r.Dest)
		if err != nil {
			return nil, fmt.Errorf("parse dest %q: %w", r.Dest, err)
		}
		nr.Dst = dst
	}
	if r.Gateway != "" {
		gw := net.ParseIP(r.Gateway)
		if gw == nil {
			return nil, fmt.Errorf("parse gateway %q", r.Gateway)
		}
		nr.Gw = gw
	}
	if r.OifName != "" {
		link, err := netlink.LinkByName(r.OifName)
		if err != nil {
			return nil, fmt.Errorf("link %q: %w", r.OifName, err)
		}
		nr.LinkIndex = link.Attrs().Index
	}
	if r.Scope == "link" {
		nr.Scope = netlink.SCOPE_LINK
	}
	for _, nh := range r.Nexthops {
		link, err := netlink.LinkByName(nh.OifName)
		if err != nil {
			return nil, fmt.Errorf("nexthop link %q: %w", nh.OifName, err)
		}
		w := nh.Weight
		if w < 1 {
			w = 1
		}
		// The kernel stores weight as hops+1.
		nr.MultiPath = append(nr.MultiPath, &netlink.NexthopInfo{
			LinkIndex: link.Attrs().Index, Hops: w - 1,
		})
	}
	return nr, nil
}

func (b *netlinkBackend) RouteReplace(_ context.Context, r Route) error {
	nr, err := b.toNetlinkRoute(r)
	if err != nil {
		return err
	}
	if err := netlink.RouteReplace(nr); err != nil {
		return fmt.Errorf("route replace table %d %s: %w", r.Table, r.Dest, err)
	}
	return nil
}

func (b *netlinkBackend) RouteDel(_ context.Context, r Route) error {
	nr, err := b.toNetlinkRoute(r)
	if err != nil {
		return err
	}
	if err := netlink.RouteDel(nr); err != nil {
		if strings.Contains(err.Error(), "no such process") ||
			strings.Contains(err.Error(), "no such file") {
			return nil // already gone; deleting a default is how failover marks down
		}
		return fmt.Errorf("route del table %d %s: %w", r.Table, r.Dest, err)
	}
	return nil
}

func (b *netlinkBackend) RouteList(_ context.Context, table int) ([]Route, error) {
	routes, err := netlink.RouteListFiltered(unix.AF_INET,
		&netlink.Route{Table: table}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return nil, fmt.Errorf("route list table %d: %w", table, err)
	}
	out := make([]Route, 0, len(routes))
	for _, nr := range routes {
		r := Route{Table: nr.Table, Dest: "default", Metric: nr.Priority}
		if nr.Dst != nil && nr.Dst.IP != nil {
			// Normalise 0.0.0.0/0 back to "default" to match FakeBackend's key.
			if ones, _ := nr.Dst.Mask.Size(); ones != 0 {
				r.Dest = nr.Dst.String()
			}
		}
		if nr.Gw != nil {
			r.Gateway = nr.Gw.String()
		}
		if l, err := netlink.LinkByIndex(nr.LinkIndex); err == nil {
			r.OifName = l.Attrs().Name
		}
		if nr.Scope == netlink.SCOPE_LINK {
			r.Scope = "link"
		}
		for _, nh := range nr.MultiPath {
			hop := Nexthop{Weight: nh.Hops + 1}
			if l, err := netlink.LinkByIndex(nh.LinkIndex); err == nil {
				hop.OifName = l.Attrs().Name
			}
			r.Nexthops = append(r.Nexthops, hop)
		}
		out = append(out, r)
	}
	return out, nil
}

func (b *netlinkBackend) RouteGet(_ context.Context, dst string, mark uint32) (*Route, error) {
	ip := net.ParseIP(dst)
	if ip == nil {
		return nil, fmt.Errorf("not an address: %q", dst)
	}
	routes, err := netlink.RouteGetWithOptions(ip, &netlink.RouteGetOptions{Mark: mark})
	if err != nil {
		return nil, fmt.Errorf("route get %s mark 0x%x: %w", dst, mark, err)
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("route get %s mark 0x%x: no route", dst, mark)
	}
	r := routes[0]
	out := &Route{Table: r.Table}
	if r.Gw != nil {
		out.Gateway = r.Gw.String()
	}
	if l, lerr := netlink.LinkByIndex(r.LinkIndex); lerr == nil {
		out.OifName = l.Attrs().Name
	}
	return out, nil
}

func sysctlPath(key string) string {
	return "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
}

func (b *netlinkBackend) SysctlSet(_ context.Context, key, value string) error {
	if err := os.WriteFile(sysctlPath(key), []byte(value), 0o644); err != nil {
		return fmt.Errorf("set %s=%s: %w", key, value, err)
	}
	return nil
}

func (b *netlinkBackend) SysctlGet(_ context.Context, key string) (string, error) {
	v, err := os.ReadFile(sysctlPath(key))
	if err != nil {
		return "", fmt.Errorf("get %s: %w", key, err)
	}
	return strings.TrimSpace(string(v)), nil
}
