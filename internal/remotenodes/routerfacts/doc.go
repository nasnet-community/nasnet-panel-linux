// SPDX-License-Identifier: AGPL-3.0-or-later

// Package routerfacts reads what the router reports about itself, such as
// its links and its RouterOS version, so the hub knows whether entry traffic
// can flow.
//
// The router's script rewrites router/state in the plugin's data folder from
// time to time. The file is plain key=value lines:
//
//	# comments and blank lines are fine
//	domestic=1            a Domestic link is up (1 or 0)
//	domestic_addr=192.168.1.20
//	                      the router's IPv4 address on that link; empty when
//	                      there is no link or it has no address
//	vpn=1                 a VPN is up
//	vpn_kind=wireguard    wireguard, l2tp, openvpn or other
//	vpn_label=wg-vpn      the VPN's interface name
//	route_foreign=1       the routing table behind 192.168.50.16 exists
//	route_vpn=1           ... behind 192.168.50.17
//	route_domestic=1      ... behind 192.168.50.18
//	local_port=41873      the port on the Domestic link the router forwards
//	                      to its own entry; empty or 0 when it has picked none
//	local_rules=1         that forward is in place (the entry is switched on)
//	cloud_name=abc.sn.mynetname.net
//	                      the router's IP Cloud name; empty when it has none
//	public_addr=198.51.100.24
//	                      the router's address as IP Cloud sees it
//	ros=7.24.5            the RouterOS version
//	free_mem=412000000    bytes
//	free_disk=9000000     bytes
//	updated=1793376777    when the script wrote it, unix seconds
//
// Keys this package doesn't know are ignored, so a newer script can add
// facts, and a line it can't make sense of is skipped and counted. A script
// from before domestic_addr leaves it out, which reads the same as an empty
// one, and so does a value that is not an IPv4 address: there is no address
// to go by, which is never a change of address. The script rewrites the file
// in place, so a reader can arrive halfway through: Reader only takes a copy
// of the file that holds still.
//
// The other way, the hub leaves the script what it wants of the router in
// small files beside the report (see Asks): whether to forward a port to the
// router's own entry, and when to pick another.
package routerfacts
