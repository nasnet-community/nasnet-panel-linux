package system

import (
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// LANBridgeName is the one name we choose, because we create the bridge
const LANBridgeName = domain.ManagedBridgeName

// DefaultLANCIDR avoids 192.168.1.0/24 (every ISP router) and Starlink
const DefaultLANCIDR = "10.77.0.1/24"

func RenderLANNetdev(name string) UplinkFile {
	if name == "" {
		name = LANBridgeName
	}
	var b strings.Builder
	b.WriteString("# Managed by nasnet. Do not edit.\n")
	b.WriteString("[NetDev]\n")
	fmt.Fprintf(&b, "Name=%s\n", name)
	b.WriteString("Kind=bridge\n")
	return UplinkFile{Name: "20-nasnet-lan.netdev", Content: b.String()}
}

// RenderLANNetwork addresses the bridge. Up without carrier so dnsmasq can bind
// :53 with nothing plugged in; no RouteTable= — the LAN is not an egress path.
func RenderLANNetwork(cfg domain.LANConfig) UplinkFile {
	name := cfg.BridgeName
	if name == "" {
		name = LANBridgeName
	}
	cidr := cfg.CIDR
	if cidr == "" {
		cidr = DefaultLANCIDR
	}

	var b strings.Builder
	b.WriteString("# Managed by nasnet. Do not edit — regenerated on every apply.\n")
	b.WriteString("[Match]\n")
	fmt.Fprintf(&b, "Name=%s\n", name)
	b.WriteString("\n[Network]\n")
	fmt.Fprintf(&b, "Address=%s\n", cidr)
	b.WriteString("IPv6AcceptRA=no\n")
	b.WriteString("LinkLocalAddressing=ipv4\n")
	b.WriteString("ConfigureWithoutCarrier=yes\n")
	b.WriteString("IgnoreCarrierLoss=yes\n")
	return UplinkFile{Name: "30-nasnet-lan.network", Content: b.String()}
}

// RenderLANMember enslaves one port, matched on the permanent MAC. The filename
// carries the MAC too, so two members cannot collide.
func RenderLANMember(in domain.NetworkInterface, bridgeName string) UplinkFile {
	if bridgeName == "" {
		bridgeName = LANBridgeName
	}
	// The bridge is not its own member. That file matches the same link and
	// sorts before 30-nasnet-lan.network, so it would shadow the address.
	if in.IfName == bridgeName {
		return UplinkFile{}
	}
	var b strings.Builder
	b.WriteString("# Managed by nasnet. Do not edit.\n")
	b.WriteString("[Match]\n")

	slug := strings.ReplaceAll(in.PermMAC, ":", "")
	if slug != "" {
		fmt.Fprintf(&b, "PermanentMACAddress=%s\n", in.PermMAC)
	} else {
		// No permanent MAC, so the role is tied to the port. V22 warns about it.
		slug = in.IfName
		fmt.Fprintf(&b, "Name=%s\n", in.IfName)
	}

	// A member has no L3 of its own — no Address=, no DHCP=.
	b.WriteString("\n[Network]\n")
	fmt.Fprintf(&b, "Bridge=%s\n", bridgeName)

	return UplinkFile{
		Name:    fmt.Sprintf("21-nasnet-lanmember-%s.network", slug),
		Content: b.String(),
	}
}
