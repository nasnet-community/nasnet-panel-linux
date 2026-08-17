package netif

import "strings"

// Source is how an interface is physically attached
type Source string

const (
	SourceLoopback      Source = "loopback"
	SourceEthOnboard    Source = "eth_onboard"
	SourceEthPCI        Source = "eth_pci"
	SourceEthUSB        Source = "eth_usb"
	SourceEthPlatform   Source = "eth_platform"
	SourceWifiPCI       Source = "wifi_pci"
	SourceWifiUSB       Source = "wifi_usb"
	SourceTetherAndroid Source = "tether_android"
	SourceTetherIPhone  Source = "tether_iphone"
	SourceWWANUSB       Source = "wwan_usb"
	SourceWWANPCIe      Source = "wwan_pcie"
	SourceVirtBridge    Source = "virt_bridge"
	SourceVirtVLAN      Source = "virt_vlan"
	SourceVirtBond      Source = "virt_bond"
	SourceVirtOther     Source = "virt_other"
	SourceUnknown       Source = "unknown"
)

// ARP hardware types that mean "no ethernet header", i.e. a cellular raw-IP
// link. networkd's link_dhcp_enabled() silently refuses DHCP on both below
// systemd 257, so these need an external client or carrier static config.
const (
	arpTypeLoopback uint16 = 772
	arpTypeRawIP    uint16 = 519
	arpTypeNone     uint16 = 65534
)

// Probe is everything the classifier needs, gathered by List from sysfs and udev
type Probe struct {
	IfName              string
	ARPType             uint16
	Subsystem           string // "pci" | "usb" | "platform" | ""
	Driver              string
	HasPHY80211         bool
	HasLinkInfoKind     bool
	LinkKind            string   // bridge | vlan | bond | wireguard | veth | tun | dummy | vrf | …
	OnboardName         string   // udev ID_NET_NAME_ONBOARD, empty when absent
	USBSiblingFunctions []string // lists other USB interface classes on the same device (MTP or ADB -> strongest phone signal)
}

// Classify returns the source and a 0-100 confidence. Anything below 100 is
// ambiguous and must be surfaced with an operator override (low-confidence device is never admitted to a LAN role without explicit confirmation)
func Classify(p Probe) (Source, int) {
	// 1. loopback
	if p.IfName == "lo" || p.ARPType == arpTypeLoopback {
		return SourceLoopback, 100
	}

	// 2. wifi (checked before the bus, since a radio also sits on PCI or USB)
	if p.HasPHY80211 {
		if p.Subsystem == "usb" {
			return SourceWifiUSB, 100
		}
		return SourceWifiPCI, 100
	}

	// 3. virtual (a netlink link kind, or no backing device at all)
	if p.HasLinkInfoKind {
		switch p.LinkKind {
		case "bridge":
			return SourceVirtBridge, 100
		case "vlan":
			return SourceVirtVLAN, 100
		case "bond":
			return SourceVirtBond, 100
		default:
			return SourceVirtOther, 100
		}
	}

	rawIP := p.ARPType == arpTypeRawIP || p.ARPType == arpTypeNone

	switch p.Subsystem {
	case "usb":
		// 4. USB
		if p.Driver == "ipheth" {
			return SourceTetherIPhone, 100
		}
		if p.Driver == "qmi_wwan" || p.Driver == "cdc_mbim" || rawIP {
			return SourceWWANUSB, 100
		}
		switch p.Driver {
		case "rndis_host", "cdc_ncm", "cdc_ether":
			// Genuinely ambiguous: phones and generic dongles both use these.
			if score := phoneScore(p.USBSiblingFunctions); score > 0 {
				return SourceTetherAndroid, score
			}
			return SourceEthUSB, 60
		}
		return SourceEthUSB, 100

	case "pci":
		// 5. PCI
		if p.Driver == "mhi_net" || p.Driver == "t7xx" || rawIP {
			return SourceWWANPCIe, 100
		}
		if p.OnboardName != "" {
			return SourceEthOnboard, 100
		}
		return SourceEthPCI, 100

	case "platform":
		// 6. platform
		return SourceEthPlatform, 100
	}

	// 7. fallback
	return SourceUnknown, 0
}

// phoneScore rates how phone like a USB device's sibling interfaces look.
func phoneScore(siblings []string) int {
	score := 0
	for _, s := range siblings {
		// udev spells these as hex class/subclass/protocol triplets.
		switch strings.ToLower(s) {
		case "mtp", "ptp", "ffff00", "060101":
			score += 45
		case "adb", "ff4201":
			score += 40
		case "usbmux", "fffe02":
			score += 45
		}
	}
	if score > 100 {
		score = 100
	}
	return score
}

// Assignable reports whether a source may ever hold a role. Loopback and
// virtual devices (veth, tun, wireguard, dummy, vrf) are hidden from the UI
func Assignable(s Source) bool {
	switch s {
	case SourceLoopback, SourceVirtOther, SourceUnknown:
		return false
	}
	return true
}
