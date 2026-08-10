package usecase

import (
	"context"
	"fmt"
	"sort"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// ApplyNftState puts the ingress pins into the owned table
func ApplyNftState(ctx context.Context, m *nft.Manager, uplinks []Uplink) error {
	ordered := append([]Uplink(nil), uplinks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })

	pins := make([]nft.Pin, 0, len(ordered))
	for _, u := range ordered {
		pins = append(pins, nft.Pin{IfName: u.IfName, Index: u.UplinkIndex})
	}

	if err := m.Update(ctx, func(rs *nft.Ruleset) {
		rs.Connmark = true
		rs.IngressPins = pins
	}); err != nil {
		return fmt.Errorf("apply nft ingress pins: %w", err)
	}
	return nil
}

// ApplySysctls sets the runtime values; bridgeName is "" with no LAN. The
// drop-in only lands at boot, so everything it sets is repeated here.
func ApplySysctls(ctx context.Context, be system.Backend, uplinks []Uplink,
	forwarding bool, bridgeName string) error {
	set := func(key, value string) error {
		if err := be.SysctlSet(ctx, key, value); err != nil {
			return fmt.Errorf("sysctl %s=%s: %w", key, value, err)
		}
		return nil
	}

	if forwarding {
		if err := set("net.ipv4.ip_forward", "1"); err != nil {
			return err
		}
	}

	// Kernel takes max(all, per-interface) so both must be loose
	if err := set("net.ipv4.conf.all.rp_filter", "2"); err != nil {
		return err
	}
	if err := set("net.ipv4.tcp_fwmark_accept", "1"); err != nil {
		return err
	}
	if err := set("net.ipv4.fwmark_reflect", "1"); err != nil {
		return err
	}

	// IPv4-only by design: an IPv6 path would bypass the routing policy, and the
	// drop-in alone would leave it up until the next reboot.
	if err := set("net.ipv6.conf.all.disable_ipv6", "1"); err != nil {
		return err
	}
	if err := set("net.ipv6.conf.default.disable_ipv6", "1"); err != nil {
		return err
	}

	if bridgeName != "" {
		if err := set("net.ipv4.conf."+bridgeName+".rp_filter", "2"); err != nil {
			return err
		}
		if err := set("net.ipv6.conf."+bridgeName+".disable_ipv6", "1"); err != nil {
			return err
		}
	}

	for _, u := range uplinks {
		if err := set("net.ipv4.conf."+u.IfName+".rp_filter", "2"); err != nil {
			return err
		}
		// Deliberately not the route-lookup variant: it answers by looking up
		// the ARP target, and RouteTable= moves connected routes out of main
		if err := set("net.ipv4.conf."+u.IfName+".arp_ignore", "1"); err != nil {
			return err
		}
		if err := set("net.ipv4.conf."+u.IfName+".arp_announce", "2"); err != nil {
			return err
		}
		if err := set("net.ipv6.conf."+u.IfName+".disable_ipv6", "1"); err != nil {
			return err
		}
	}
	return nil
}
