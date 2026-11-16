// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg

import "slices"

// privateRanges is Xray's own private set (26.11.9), spelled out: geoip:private
// would need a geoip.dat on the node. Same ranges as the panel's reverse exit.
var privateRanges = []string{
	"0.0.0.0/8", "0.0.0.0/32", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/29", "192.0.0.8/32", "192.0.0.9/32",
	"192.0.0.10/32", "192.0.0.170/32", "192.0.0.171/32", "192.0.2.0/24", "192.52.193.0/24",
	"192.88.99.2/32", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"240.0.0.0/4", "255.255.255.255/32", "224.0.0.0/4",
	"::1/128", "::/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "100:0:0:1::/64",
	"2001::/32", "2001:1::1/128", "2001:1::2/128", "2001:1::3/128", "2001:2::/48",
	"2001:3::/32", "2001:20::/28", "2001:30::/28", "2001:db8::/32", "2002::/16",
	"3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "ff00::/8",
}

// PrivateCIDRs returns the ranges the node blocks before anything else.
func PrivateCIDRs() []string {
	return slices.Clone(privateRanges)
}
