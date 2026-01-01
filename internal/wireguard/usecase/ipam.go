package usecase

import (
	"fmt"
	"net"
)

// nextFreeIP returns the lowest unused /32 host in poolCIDR, skipping network,
// broadcast, the server IP, and any used IPs. IPv4 only.
func nextFreeIP(poolCIDR, serverIP string, used map[string]bool) (string, error) {
	_, ipnet, err := net.ParseCIDR(poolCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid pool cidr %q: %w", poolCIDR, err)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return "", fmt.Errorf("wireguard IPAM v1 supports IPv4 pools only")
	}

	network := ipnet.IP.Mask(ipnet.Mask).To4()
	if network == nil {
		return "", fmt.Errorf("invalid IPv4 network")
	}
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = network[i] | ^ipnet.Mask[i]
	}

	for ip := cloneIP(network); ipnet.Contains(ip); incIP(ip) {
		s := ip.String()
		if ones < 31 && (ip.Equal(network) || ip.Equal(broadcast)) {
			continue
		}
		if s == serverIP || used[s] {
			continue
		}
		return s, nil
	}
	return "", fmt.Errorf("wireguard IP pool %s exhausted", poolCIDR)
}

func cloneIP(ip net.IP) net.IP { c := make(net.IP, len(ip)); copy(c, ip); return c }

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
