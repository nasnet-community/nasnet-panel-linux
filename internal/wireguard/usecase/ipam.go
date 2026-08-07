package usecase

import (
	"fmt"
	"net/netip"
)

// nextFreeIP returns the lowest unused /32 host in poolCIDR, skipping network,
// broadcast, the server IP, and any used IPs. IPv4 only.
func nextFreeIP(poolCIDR, serverIP string, used map[string]bool) (string, error) {
	prefix, err := netip.ParsePrefix(poolCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid pool cidr %q: %w", poolCIDR, err)
	}
	if !prefix.Addr().Is4() {
		return "", fmt.Errorf("wireguard IPAM v1 supports IPv4 pools only")
	}
	prefix = prefix.Masked()
	network := prefix.Addr()

	for ip := network; prefix.Contains(ip); ip = ip.Next() {
		// /31 and /32 have no network or broadcast to skip. Everywhere else the
		// broadcast is whatever address falls out of the prefix when bumped.
		if prefix.Bits() < 31 && (ip == network || !prefix.Contains(ip.Next())) {
			continue
		}
		s := ip.String()
		if s == serverIP || used[s] {
			continue
		}
		return s, nil
	}
	return "", fmt.Errorf("wireguard IP pool %s exhausted", poolCIDR)
}
