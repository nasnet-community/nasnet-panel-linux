package domain

import (
	"fmt"
	"net/netip"
	"strings"
)

// Level is how hard a verdict bites.
type Level string

const (
	LevelReject  Level = "reject"
	LevelConfirm Level = "confirm" // typed CONFIRM from the operator
	LevelWarn    Level = "warn"
)

// Verdict is one rule's finding
type Verdict struct {
	Rule    string `json:"rule"`
	Level   Level  `json:"level"`
	Message string `json:"message"`
}

// ChangeRequest is one role assignment.
type ChangeRequest struct {
	InterfaceID uint          `json:"interface_id"`
	Role        InterfaceRole `json:"role"`
	Slot        UplinkSlot    `json:"slot"`
	// EvictID names the interface losing a singleton role. Required, never inferred
	EvictID *uint `json:"evict_id"`
	// Confirmed is the operator having typed CONFIRM.
	Confirmed bool  `json:"confirmed"`
	MasterID  *uint `json:"master_id"`

	Method        AddressMethod `json:"method"`
	StaticAddress string        `json:"static_address"`
	StaticGateway string        `json:"static_gateway"`

	// NewIfName exists only so V21 can reject it. We never rename.
	NewIfName string `json:"new_if_name"`
}

// ValidationInput is the whole world a validation needs
type ValidationInput struct {
	Rows []NetworkInterface
	Req  ChangeRequest
	LAN  *LANConfig
	// MgmtCIDR is the frozen management subnet, "" when no mgmt role exists.
	MgmtCIDR string
	// PeerIfName is the interface carrying the live admin session, from
	// `ip route get <peer>` taking the oif.
	PeerIfName       string
	HostapdInstalled bool
	IWDInstalled     bool
}

// Ranges a LAN CIDR may never overlap.
var reservedLANOverlaps = []string{
	"100.64.0.0/10",    // Starlink bypass space
	"192.168.100.0/24", // Starlink dish API
	"127.0.0.0/8",
	"169.254.0.0/16",
	"224.0.0.0/4",
}

// Rejected reports whether any verdict is a hard reject.
func Rejected(vs []Verdict) bool {
	for _, v := range vs {
		if v.Level == LevelReject {
			return true
		}
	}
	return false
}

// Validate runs every form-time rule in order and returns all findings. Caller
// stops on the first reject. Address-dependent rules (V29-V31) run at apply and
// after every DHCP lease instead — not knowable here.
func Validate(in ValidationInput) []Verdict {
	var vs []Verdict
	reject := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelReject, Message: fmt.Sprintf(format, args...)})
	}
	warn := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelWarn, Message: fmt.Sprintf(format, args...)})
	}
	confirm := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelConfirm, Message: fmt.Sprintf(format, args...)})
	}

	byID := map[uint]*NetworkInterface{}
	for i := range in.Rows {
		byID[in.Rows[i].ID] = &in.Rows[i]
	}

	// V1  the target row exists, is ours, and is present.
	target := byID[in.Req.InterfaceID]
	if target == nil {
		reject("V1", "interface %d not found", in.Req.InterfaceID)
		return vs
	}
	if !target.Present {
		reject("V1", "interface %s is not present", target.IfName)
		return vs
	}

	// V2 the role is a real enum value.
	if !in.Req.Role.Valid() {
		reject("V2", "%q is not a valid role", in.Req.Role)
		return vs
	}

	// V21 — we never rename. Early, because a rename invalidates every nft rule,
	// dnsmasq interface= and hostapd interface= naming the old device.
	if in.Req.NewIfName != "" && in.Req.NewIfName != target.IfName {
		reject("V21", "interfaces are never renamed (%s -> %s); use the operator label instead",
			target.IfName, in.Req.NewIfName)
		return vs
	}

	source := target.Source
	if target.SourceOverride != "" {
		source = target.SourceOverride
	}

	// V3 loopback and non-bridge virtual devices are hidden entirely.
	if source == "loopback" || source == "virt_other" || source == "unknown" {
		reject("V3", "%s (%s) can never hold a role", target.IfName, source)
		return vs
	}

	if in.Req.Role != RoleUnassigned {
		// V6  cellular is uplink-only, no override.
		if strings.HasPrefix(source, "wwan_") && in.Req.Role != RoleWAN {
			reject("V6", "%s is a cellular modem and can only be an uplink", target.IfName)
			return vs
		}
		// V5 ephemeral sources cannot hold a LAN role.
		if target.Ephemeral && (in.Req.Role == RoleLAN || in.Req.Role == RoleLANMember) {
			reject("V5", "%s is an ephemeral device (%s) and cannot hold %s",
				target.IfName, source, in.Req.Role)
			return vs
		}
		// V4 source x role.
		if !SourceAllows(source, in.Req.Role) {
			reject("V4", "a %s interface cannot hold the %s role", source, in.Req.Role)
			return vs
		}
	}

	// V7 not already enslaved to something we did not create.
	if target.MasterID != nil && in.Req.Role != RoleLANMember {
		if m := byID[*target.MasterID]; m == nil {
			reject("V7", "%s is enslaved to an interface nasnet does not manage", target.IfName)
			return vs
		}
	}

	// V8 singleton reassignment must name the evictee. Never auto-evict.
	if in.Req.Role.IsSingleton() {
		for i := range in.Rows {
			holder := &in.Rows[i]
			if holder.Role != in.Req.Role || holder.ID == target.ID {
				continue
			}
			if in.Req.EvictID == nil || *in.Req.EvictID != holder.ID {
				reject("V8", "%s already holds the %s role; name it explicitly to take the role over",
					holder.IfName, in.Req.Role)
				return vs
			}
		}
	}

	// V9 one role per interface; a parent and its members cannot both hold one.
	if in.Req.Role != RoleUnassigned {
		if target.Role != RoleUnassigned && target.Role != in.Req.Role {
			reject("V9", "%s already holds the %s role; clear it first", target.IfName, target.Role)
			return vs
		}
		for i := range in.Rows {
			r := &in.Rows[i]
			if r.MasterID != nil && *r.MasterID == target.ID && in.Req.Role != RoleLAN {
				reject("V9", "%s is enslaved to %s, so %s cannot also hold %s",
					r.IfName, target.IfName, target.IfName, in.Req.Role)
				return vs
			}
		}
	}

	// V10 a LAN member needs a master pointing at the LAN row.
	if in.Req.Role == RoleLANMember {
		if in.Req.MasterID == nil {
			reject("V10", "%s needs a LAN bridge to join", target.IfName)
			return vs
		}
		master := byID[*in.Req.MasterID]
		if master == nil || master.Role != RoleLAN {
			reject("V10", "master interface %d does not hold the lan role", *in.Req.MasterID)
			return vs
		}
	}

	// V11 / V12 | Wi-Fi gates.
	if strings.HasPrefix(source, "wifi_") {
		if in.Req.Role == RoleLAN || in.Req.Role == RoleLANMember {
			if !in.HostapdInstalled {
				reject("V11", "hostapd is not installed, so %s cannot be an access point", target.IfName)
				return vs
			}
		}
		if in.Req.Role == RoleWAN && !in.IWDInstalled {
			reject("V12", "iwd is not installed, so %s cannot join a network (networkd cannot associate)",
				target.IfName)
			return vs
		}
	}

	// V13 one radio, one role. AP+STA concurrency is never offered.
	if target.Phy() != "" && in.Req.Role != RoleUnassigned {
		for i := range in.Rows {
			sib := &in.Rows[i]
			if sib.ID == target.ID || sib.Phy() != target.Phy() || sib.Role == RoleUnassigned {
				continue
			}
			apRole := func(r InterfaceRole) bool { return r == RoleLAN || r == RoleLANMember }
			if apRole(sib.Role) != apRole(in.Req.Role) {
				reject("V13", "%s shares radio %s with %s; a radio is a station or an access point, never both",
					target.IfName, target.Phy(), sib.IfName)
				return vs
			}
		}
	}

	// V14 — LAN CIDR overlaps.
	if in.LAN != nil && in.LAN.CIDR != "" {
		forbidden := append([]string{}, reservedLANOverlaps...)
		if in.MgmtCIDR != "" {
			forbidden = append(forbidden, in.MgmtCIDR)
		}
		for i := range in.Rows {
			if in.Rows[i].Role == RoleWAN && in.Rows[i].StaticAddress != "" {
				forbidden = append(forbidden, in.Rows[i].StaticAddress)
			}
		}
		for _, f := range forbidden {
			if overlaps(in.LAN.CIDR, f) {
				reject("V14", "LAN %s overlaps %s", in.LAN.CIDR, f)
				return vs
			}
		}
	}

	// V15 raw-IP uplinks force Method=rawip.
	if strings.HasPrefix(source, "wwan_") && in.Req.Method == MethodDHCP4 {
		reject("V15", "%s is a raw-IP link; networkd cannot DHCP on it, choose rawip or a static address",
			target.IfName)
		return vs
	}

	// Counts for the box-shape rules below
	assignable, wans, hasMgmt, hasLAN := 0, 0, false, false
	for i := range in.Rows {
		r := &in.Rows[i]
		if r.Source == "loopback" || r.Source == "virt_other" || r.Source == "unknown" || !r.Present {
			continue
		}
		assignable++
		role := r.Role
		if r.ID == target.ID {
			role = in.Req.Role
		}
		switch role {
		case RoleWAN:
			wans++
		case RoleMgmt:
			hasMgmt = true
		case RoleLAN:
			hasLAN = true
		}
	}

	// V16 apply needs at least one uplink; failover needs two. Only a reject
	// when this change actually gives one up: clearing an interface that held no
	// uplink cannot remove the last one.
	if in.Req.Role == RoleUnassigned && target.Role == RoleWAN && wans == 0 {
		reject("V16", "at least one uplink must remain assigned")
		return vs
	}
	if wans < 2 {
		warn("V16", "only %d uplink assigned — no failover and no split routing", wans)
	}

	// V17 exactly two assignable interfaces, both uplinks: no LAN and no local
	// management path. Supported, but never silently.
	if assignable == 2 && wans == 2 && !hasLAN {
		other := ""
		for i := range in.Rows {
			if in.Rows[i].ID != target.ID && in.Rows[i].Present {
				other = in.Rows[i].IfName
			}
		}
		confirm("V17", "no local network: the panel will be reachable only via %s. "+
			"An nft accept rule for the panel port on the domestic uplink is provisioned "+
			"automatically so the firewall cannot cut the only management path. Type CONFIRM to continue.",
			other)
	}

	// V18 lockout. Resolving the peer's oif and refusing to sever it is nearly
	// free and prevents the most common self-inflicted outage.
	if in.PeerIfName != "" && in.PeerIfName == target.IfName &&
		in.Req.Role == RoleUnassigned && !in.Req.Confirmed {
		reject("V18", "%s carries this admin session; unassigning it will cut you off. "+
			"Confirm to proceed with the auto-revert armed", target.IfName)
		return vs
	}

	// V19 three or more assignable interfaces and no mgmt reserved.
	if assignable >= 3 && !hasMgmt {
		warn("V19", "%d assignable interfaces and no management port reserved; "+
			"reserving one gives a headless box a recovery path", assignable)
	}

	// V22 the key is not a permanent MAC.
	if target.KeyKind != "" && target.KeyKind != "permaddr" && in.Req.Role != RoleUnassigned {
		warn("V22", "%s has no usable permanent MAC, so this role is tied to the port, not the device — "+
			"swapping the adapter keeps the role", target.IfName)
	}

	// V23 USB 2.0 uplink.
	if in.Req.Role == RoleWAN && target.USBSpeedMbit == 480 {
		warn("V23", "%s is on a USB 2.0 port; expect a ceiling near 280 Mbit/s", target.IfName)
	}

	return vs
}

// SourceAllows is the source x role capability matrix.
func SourceAllows(source string, role InterfaceRole) bool {
	if role == RoleUnassigned {
		return true
	}
	switch source {
	case "eth_onboard", "eth_pci", "eth_platform", "eth_usb", "virt_vlan", "virt_bond":
		return role == RoleWAN || role == RoleLAN || role == RoleLANMember
	case "wifi_pci", "wifi_usb":
		// Station for an uplink (via iwd), AP for a LAN. Gated further by
		// V11/V12 and by one-radio-one-role.
		return role == RoleWAN || role == RoleLAN || role == RoleLANMember
	case "tether_android", "tether_iphone", "wwan_usb", "wwan_pcie":
		return role == RoleWAN
	case "virt_bridge":
		return role == RoleLAN
	}
	return false
}

// overlaps reports whether two CIDRs (either may be host/prefix form) intersect.
func overlaps(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil {
		return false
	}
	return unmap4in6(pa).Overlaps(unmap4in6(pb))
}

// unmap4in6 rewrites a ::ffff:a.b.c.d prefix into its plain IPv4 form
func unmap4in6(p netip.Prefix) netip.Prefix {
	if !p.Addr().Is4In6() || p.Bits() < 96 {
		return p
	}
	return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
}
