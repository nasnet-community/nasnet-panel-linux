package domain

import (
	"fmt"
	"net"
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

	// MACPolicy mirrors networkd's MACAddressPolicy. "random" is rejected on
	// uplinks and on APs.
	MACPolicy string `json:"mac_policy"`
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
	// RadioSupportsAP and RadioSupportsSTA are probed at runtime, keyed by phy.
	// Absent means unknown, treated as unsupported: offering a role a radio
	// cannot hold produces a daemon crash the operator cannot diagnose.
	RadioSupportsAP  map[string]bool
	RadioSupportsSTA map[string]bool
	// CountryCode is the current regulatory domain. Mandatory before an AP.
	CountryCode string
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
	// V3 — and the bridge nasnet creates is not a port. Giving it a role would
	// enslave it to itself and strip the address dnsmasq binds.
	if target.IfName == ManagedBridgeName && in.Req.Role != RoleUnassigned {
		reject("V3", "%s is the bridge nasnet creates for the LAN, not a port; "+
			"assign the port you want on the LAN instead", target.IfName)
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

	// V11 / V12 | Wi-Fi gates, from probed capability rather than assumption.
	if strings.HasPrefix(source, "wifi_") {
		if in.Req.Role == RoleLAN || in.Req.Role == RoleLANMember {
			if !in.HostapdInstalled {
				reject("V11", "hostapd is not installed, so %s cannot be an access point", target.IfName)
				return vs
			}
			if !in.RadioSupportsAP[target.Phy()] {
				reject("V11", "radio %s does not support access point mode, so %s cannot hold %s",
					target.Phy(), target.IfName, in.Req.Role)
				return vs
			}
			if RegDomainUnset(in.CountryCode) {
				reject("V11", "set a country code before enabling an access point: the default "+
					"regulatory domain marks nearly all 5 GHz as no-initiating-radiation and "+
					"hostapd refuses to start")
				return vs
			}
		}
		if in.Req.Role == RoleWAN {
			if !in.IWDInstalled {
				reject("V12", "iwd is not installed, so %s cannot join a network "+
					"(networkd cannot associate)", target.IfName)
				return vs
			}
			if !in.RadioSupportsSTA[target.Phy()] {
				reject("V12", "radio %s does not support station mode", target.Phy())
				return vs
			}
		}
	}

	// V24 a random MAC invalidates every client's saved network on an AP, and
	// breaks DHCP reservations and our own identity key on an uplink.
	if in.Req.MACPolicy == "random" &&
		(in.Req.Role == RoleWAN || in.Req.Role == RoleLAN || in.Req.Role == RoleLANMember) {
		reject("V24", "a random MAC policy cannot be used on %s: clients and DHCP "+
			"reservations both depend on a stable address", in.Req.Role)
		return vs
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
	if in.LAN != nil {
		if lanVs := ValidateLANConfig(*in.LAN, in.Rows, in.MgmtCIDR); Rejected(lanVs) {
			return append(vs, lanVs...)
		}
	}

	// V15 raw-IP uplinks force Method=rawip.
	if strings.HasPrefix(source, "wwan_") && in.Req.Method == MethodDHCP4 {
		reject("V15", "%s is a raw-IP link; networkd cannot DHCP on it, choose rawip or a static address",
			target.IfName)
		return vs
	}

	// V20 the slot picks the routing table and the unit filename
	if in.Req.Role == RoleWAN {
		if in.Req.Slot != SlotDomestic && !in.Req.Slot.IsSecondary() {
			reject("V20", "%s must be assigned to the domestic slot or a secondary slot", target.IfName)
			return vs
		}
	} else if in.Req.Slot != SlotNone {
		reject("V20", "only an uplink carries a slot, %s does not", in.Req.Role)
		return vs
	}

	// V25 one interface per slot
	if in.Req.Role == RoleWAN {
		for i := range in.Rows {
			holder := &in.Rows[i]
			if holder.ID == target.ID || holder.Role != RoleWAN || holder.Slot != in.Req.Slot {
				continue
			}
			if in.Req.EvictID == nil || *in.Req.EvictID != holder.ID {
				reject("V25", "%s already holds the %s slot; name it explicitly to take it over",
					holder.IfName, in.Req.Slot)
				return vs
			}
		}
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

// ValidateLANConfig runs V14 on a LAN definition. Pure, and shared: a role
// change and an edit of the LAN itself must reject the same overlaps.
func ValidateLANConfig(lan LANConfig, rows []NetworkInterface, mgmtCIDR string) []Verdict {
	var vs []Verdict
	reject := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelReject, Message: fmt.Sprintf(format, args...)})
	}

	if lan.CIDR == "" {
		return vs
	}
	if _, err := netip.ParsePrefix(lan.CIDR); err != nil {
		reject("V14", "%q is not a valid address and prefix, e.g. 10.77.0.1/24", lan.CIDR)
		return vs
	}

	forbidden := append([]string{}, reservedLANOverlaps...)
	if mgmtCIDR != "" {
		forbidden = append(forbidden, mgmtCIDR)
	}
	for i := range rows {
		if rows[i].Role == RoleWAN && rows[i].StaticAddress != "" {
			forbidden = append(forbidden, rows[i].StaticAddress)
		}
	}
	for _, f := range forbidden {
		if overlaps(lan.CIDR, f) {
			reject("V14", "LAN %s overlaps %s", lan.CIDR, f)
			return vs
		}
	}

	// The DHCP pool has to be inside the subnet it hands addresses out of.
	prefix, _ := netip.ParsePrefix(lan.CIDR)
	for label, addr := range map[string]string{
		"the DHCP range start": lan.DHCPRangeLow,
		"the DHCP range end":   lan.DHCPRangeHigh,
	} {
		if addr == "" {
			continue
		}
		ip, err := netip.ParseAddr(addr)
		if err != nil {
			reject("V14", "%s (%q) is not an IP address", label, addr)
			return vs
		}
		if !prefix.Contains(ip) {
			reject("V14", "%s (%s) is outside the LAN %s", label, addr, lan.CIDR)
			return vs
		}
	}
	return vs
}

// PortForwardInput is everything a port-forward validation needs.
type PortForwardInput struct {
	Existing  []PortForward
	New       PortForward
	LANCIDR   string
	PanelPort int
	SSHPort   int
	// InboundPorts is proto -> ports, derived from the enabled xray inbound
	// rows. Passed in rather than looked up so this stays pure.
	InboundPorts map[string][]int
	UplinkKeys   []string
	Confirmed    bool
}

// ValidatePortForward runs V26-V28 plus the field checks. Pure.
func ValidatePortForward(in PortForwardInput) []Verdict {
	var vs []Verdict
	reject := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelReject, Message: fmt.Sprintf(format, args...)})
	}
	confirm := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelConfirm, Message: fmt.Sprintf(format, args...)})
	}
	pf := in.New

	// Field checks first — a malformed row cannot be collision-checked.
	switch pf.Proto {
	case "tcp", "udp":
	default:
		reject("V26", "protocol %q must be tcp or udp", pf.Proto)
		return vs
	}
	if pf.DPort < 1 || pf.DPort > 65535 {
		reject("V26", "external port %d is out of range", pf.DPort)
		return vs
	}
	if pf.ToPort < 1 || pf.ToPort > 65535 {
		reject("V26", "target port %d is out of range", pf.ToPort)
		return vs
	}
	target := net.ParseIP(pf.ToAddr)
	if target == nil {
		reject("V26", "target %q is not an IP address", pf.ToAddr)
		return vs
	}
	if pf.UplinkKey != "" {
		known := false
		for _, k := range in.UplinkKeys {
			if k == pf.UplinkKey {
				known = true
			}
		}
		if !known {
			reject("V26", "uplink %q is not an assigned uplink", pf.UplinkKey)
			return vs
		}
	}

	// V26 — the target must be inside the LAN, or a local address when the
	// forward terminates on the box itself.
	if in.LANCIDR != "" {
		_, lan, err := net.ParseCIDR(in.LANCIDR)
		if err == nil && !lan.Contains(target) && !target.IsLoopback() {
			reject("V26", "target %s is outside the LAN %s", pf.ToAddr, in.LANCIDR)
			return vs
		}
	}

	// V27 — collisions. An empty UplinkKey means "any uplink", so it collides
	// with every per-uplink row on the same (proto, dport).
	sameUplink := func(a, b string) bool { return a == "" || b == "" || a == b }

	for _, port := range in.InboundPorts[pf.Proto] {
		if port == pf.DPort {
			reject("V27", "%s/%d is already used by an enabled xray inbound", pf.Proto, pf.DPort)
			return vs
		}
	}
	for _, ex := range in.Existing {
		if ex.ID == pf.ID || !ex.Enabled {
			continue
		}
		if ex.Proto == pf.Proto && ex.DPort == pf.DPort && sameUplink(ex.UplinkKey, pf.UplinkKey) {
			reject("V27", "%s/%d already forwards to %s:%d on this uplink",
				pf.Proto, pf.DPort, ex.ToAddr, ex.ToPort)
			return vs
		}
	}

	// V28 — legitimate and dangerous, so CONFIRM not reject. Two hazards: taking
	// the port from the box, and handing it to the internet.
	if pf.Proto == "tcp" && in.PanelPort > 0 && pf.DPort == in.PanelPort {
		confirm("V28", "this forward takes over port %d on that uplink, so the panel "+
			"stops answering there. Type CONFIRM to continue.", pf.DPort)
	}
	if pf.Proto == "tcp" && (pf.ToPort == in.PanelPort || pf.ToPort == in.SSHPort) {
		what := "the panel"
		if pf.ToPort == in.SSHPort {
			what = "SSH"
		}
		confirm("V28", "this forward exposes %s (port %d) to the internet. Anyone who "+
			"reaches your uplink can attempt to log in. Type CONFIRM to continue.",
			what, pf.ToPort)
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

// RegDomainUnset mirrors system.RegDomainIsUnset. Duplicated so domain never
// depends on the privileged system package.
func RegDomainUnset(code string) bool {
	c := strings.ToUpper(strings.TrimSpace(code))
	return c == "" || c == "00" || c == "0"
}

// ValidateWifiConfig checks the settings form. Role rules (V11-V13, V24) run in
// Validate; this covers what hostapd or iwd would reject at startup.
func ValidateWifiConfig(cfg WifiConfig) []Verdict {
	var vs []Verdict
	reject := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelReject, Message: fmt.Sprintf(format, args...)})
	}

	// V38 the 802.11 SSID element is at most 32 octets of the encoding.
	if n := len(cfg.SSID); n == 0 || n > 32 {
		reject("V38", "the network name must be 1-32 bytes; %q is %d", cfg.SSID, n)
	}

	// V39 a WPA passphrase is 8-63 printable ASCII, or exactly 64 hex digits for
	// a raw PSK. A station on an open network has none, so empty is only an
	// error on an AP.
	if (cfg.Mode == "ap" || cfg.PSK != "") && !validWPAPassphrase(cfg.PSK) {
		reject("V39", "the passphrase must be 8-63 printable characters, or 64 hex digits")
	}

	switch cfg.Band {
	case "2g", "5g", "6g":
	default:
		reject("V40", "unknown band %q", cfg.Band)
	}
	switch cfg.Mode {
	case "ap", "station":
	default:
		reject("V41", "unknown mode %q", cfg.Mode)
	}

	// V42 the render and the daemon both refuse this, so catch it as a form error
	if cfg.Mode == "ap" && RegDomainUnset(cfg.CountryCode) {
		reject("V42", "a country code is required before an access point can start")
	}
	return vs
}

func validWPAPassphrase(psk string) bool {
	if len(psk) == 64 {
		for _, r := range psk {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
		return true
	}
	if len(psk) < 8 || len(psk) > 63 {
		return false
	}
	for _, r := range psk {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}
