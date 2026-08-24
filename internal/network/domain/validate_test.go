package domain

import (
	"strings"
	"testing"
)

func rows() []NetworkInterface {
	return []NetworkInterface{
		{ID: 1, Key: "k1", IfName: "enp1s0", Source: "eth_onboard", Present: true, Role: RoleUnassigned},
		{ID: 2, Key: "k2", IfName: "enp2s0", Source: "eth_pci", Present: true, Role: RoleUnassigned},
		{ID: 3, Key: "k3", IfName: "enp3s0", Source: "eth_usb", Present: true, Role: RoleUnassigned},
		{ID: 4, Key: "k4", IfName: "wlp4s0", Source: "wifi_pci", Present: true, Role: RoleUnassigned},
		{ID: 5, Key: "k5", IfName: "wwan0", Source: "wwan_usb", Present: true, Role: RoleUnassigned},
		{ID: 6, Key: "k6", IfName: "wg0", Source: "virt_other", Present: true, Role: RoleUnassigned},
		{ID: 7, Key: "k7", IfName: "usb0", Source: "tether_android", Present: true,
			Role: RoleUnassigned, Ephemeral: true},
		{ID: 8, Key: "k8", IfName: "br-lan", Source: "virt_bridge", Present: true, Role: RoleUnassigned},
	}
}

func in(req ChangeRequest) ValidationInput {
	return ValidationInput{Rows: rows(), Req: req, MgmtCIDR: "192.168.99.1/24",
		HostapdInstalled: true, IWDInstalled: true}
}

func firstReject(vs []Verdict) *Verdict {
	for i := range vs {
		if vs[i].Level == LevelReject {
			return &vs[i]
		}
	}
	return nil
}

func TestValidate_AcceptsAnOrdinaryUplinkAssignment(t *testing.T) {
	vs := Validate(in(ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: SlotDomestic}))
	if Rejected(vs) {
		t.Fatalf("plain wan assignment rejected: %+v", vs)
	}
}

func TestValidate_Rejections(t *testing.T) {
	cases := []struct {
		name     string
		input    ValidationInput
		wantRule string
	}{
		{"V1 unknown interface", in(ChangeRequest{InterfaceID: 99, Role: RoleWAN}), "V1"},
		{"V2 bogus role", in(ChangeRequest{InterfaceID: 1, Role: "router"}), "V2"},
		{"V3 hidden source", in(ChangeRequest{InterfaceID: 6, Role: RoleWAN}), "V3"},
		// A bridge is the only source in this fixture that fails SourceAllows
		// without being caught earlier by V3 (hidden), V5 (ephemeral) or V6 (wwan).
		{"V4 bridge cannot be an uplink", in(ChangeRequest{InterfaceID: 8, Role: RoleWAN}), "V4"},
		{"V5 ephemeral cannot hold lan", in(ChangeRequest{InterfaceID: 7, Role: RoleLAN}), "V5"},
		{"V6 wwan is uplink-only", in(ChangeRequest{InterfaceID: 5, Role: RoleLANMember}), "V6"},
		{"V10 lan_member needs a master", in(ChangeRequest{InterfaceID: 3, Role: RoleLANMember}), "V10"},
		{"V11 wifi cannot be an AP without hostapd", func() ValidationInput {
			i := in(ChangeRequest{InterfaceID: 4, Role: RoleLAN})
			i.HostapdInstalled = false
			return i
		}(), "V11"},
		{"V12 wifi cannot be an uplink without iwd", func() ValidationInput {
			i := in(ChangeRequest{InterfaceID: 4, Role: RoleWAN})
			i.IWDInstalled = false
			return i
		}(), "V12"},
		{"V21 rename attempt", func() ValidationInput {
			i := in(ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: SlotDomestic})
			i.Req.NewIfName = "wan1"
			return i
		}(), "V21"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vs := Validate(c.input)
			r := firstReject(vs)
			if r == nil {
				t.Fatalf("accepted; verdicts = %+v", vs)
			}
			if r.Rule != c.wantRule {
				t.Errorf("first reject = %s (%s), want %s", r.Rule, r.Message, c.wantRule)
			}
		})
	}
}

// A singleton reassignment must name the evictee. Never auto-evict: silently
// stealing the LAN role is how an operator loses the only segment their devices
// are on.
func TestValidate_V8_SingletonReassignmentNeedsAnExplicitEvictee(t *testing.T) {
	i := in(ChangeRequest{InterfaceID: 2, Role: RoleLAN})
	i.Rows[0].Role = RoleLAN // enp1s0 already holds it

	vs := Validate(i)
	r := firstReject(vs)
	if r == nil || r.Rule != "V8" {
		t.Fatalf("reassignment without an evictee was accepted: %+v", vs)
	}
	if !strings.Contains(r.Message, "enp1s0") {
		t.Errorf("message must name the current holder: %q", r.Message)
	}

	evict := uint(1)
	i.Req.EvictID = &evict
	if Rejected(Validate(i)) {
		t.Errorf("naming the evictee should be accepted: %+v", Validate(i))
	}
}

// An uplink with no slot renders RouteTable=0, which networkd drops on the
// floor, so the default route stays in main and failover has nothing to move.
func TestValidate_V20_UplinkMustNameASlot(t *testing.T) {
	vs := Validate(in(ChangeRequest{InterfaceID: 1, Role: RoleWAN}))
	if r := firstReject(vs); r == nil || r.Rule != "V20" {
		t.Fatalf("slotless uplink was accepted: %+v", vs)
	}

	for _, slot := range []UplinkSlot{SlotDomestic, SlotSecondary} {
		if Rejected(Validate(in(ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: slot}))) {
			t.Errorf("%s slot rejected", slot)
		}
	}
}

// A slot on anything but an uplink is meaningless and would sit in the DB
// waiting to confuse the renderer.
func TestValidate_V20_OnlyAnUplinkCarriesASlot(t *testing.T) {
	vs := Validate(in(ChangeRequest{InterfaceID: 1, Role: RoleLAN, Slot: SlotDomestic}))
	if r := firstReject(vs); r == nil || r.Rule != "V20" {
		t.Fatalf("lan with a slot was accepted: %+v", vs)
	}
}

// Two uplinks in one slot render the same filename, so the second silently
// overwrites the first and one NIC ends up with no unit at all.
func TestValidate_V25_OneInterfacePerSlot(t *testing.T) {
	i := in(ChangeRequest{InterfaceID: 2, Role: RoleWAN, Slot: SlotDomestic})
	i.Rows[0].Role, i.Rows[0].Slot = RoleWAN, SlotDomestic // enp1s0 holds it

	vs := Validate(i)
	r := firstReject(vs)
	if r == nil || r.Rule != "V25" {
		t.Fatalf("duplicate slot was accepted: %+v", vs)
	}
	if !strings.Contains(r.Message, "enp1s0") {
		t.Errorf("message must name the holder: %q", r.Message)
	}

	evict := uint(1)
	i.Req.EvictID = &evict
	if Rejected(Validate(i)) {
		t.Errorf("naming the evictee should be accepted: %+v", Validate(i))
	}

	// The other slot is free, so it needs no eviction.
	i.Req.EvictID, i.Req.Slot = nil, SlotSecondary
	if Rejected(Validate(i)) {
		t.Errorf("the free slot was rejected: %+v", Validate(i))
	}
}

// One role per interface, and a bridge cannot hold a role while its members do.
func TestValidate_V9_ParentAndMemberCannotBothHoldRoles(t *testing.T) {
	i := in(ChangeRequest{InterfaceID: 3, Role: RoleWAN})
	master := uint(2)
	i.Rows[2].Role = RoleLANMember
	i.Rows[2].MasterID = &master
	vs := Validate(i)
	if r := firstReject(vs); r == nil || r.Rule != "V9" {
		t.Fatalf("verdicts = %+v", vs)
	}
}

// Cutting the address off the interface carrying the live admin session is the
// most common self-inflicted outage there is.
func TestValidate_V18_LockoutNeedsConfirmation(t *testing.T) {
	i := in(ChangeRequest{InterfaceID: 1, Role: RoleUnassigned})
	i.PeerIfName = "enp1s0"

	vs := Validate(i)
	r := firstReject(vs)
	if r == nil || r.Rule != "V18" {
		t.Fatalf("unassigning the admin path was accepted: %+v", vs)
	}

	i.Req.Confirmed = true
	if Rejected(Validate(i)) {
		t.Error("confirmed lockout should pass with the dead-man armed")
	}
}

// Unassigning the last remaining uplink is a reject; unassigning an interface
// that never held one is not.
func TestValidate_V16_OnlyFiresWhenAnUplinkIsActuallyLost(t *testing.T) {
	i := in(ChangeRequest{InterfaceID: 1, Role: RoleUnassigned})
	i.Rows[0].Role = RoleWAN
	i.Rows[0].Slot = SlotDomestic
	r := firstReject(Validate(i))
	if r == nil || r.Rule != "V16" {
		t.Fatalf("removing the only uplink was accepted: %+v", Validate(i))
	}

	// Same request against an interface holding no role: nothing is lost.
	clean := in(ChangeRequest{InterfaceID: 1, Role: RoleUnassigned})
	if r := firstReject(Validate(clean)); r != nil && r.Rule == "V16" {
		t.Errorf("V16 fired for an interface that held no uplink: %+v", r)
	}
}

// A two-interface box with both assigned wan is supported, never silently.
func TestValidate_V17_TwoWANsOnATwoPortBoxNeedsATypedConfirm(t *testing.T) {
	i := ValidationInput{
		Rows: []NetworkInterface{
			{ID: 1, Key: "k1", IfName: "enp1s0", Source: "eth_onboard", Present: true, Role: RoleWAN, Slot: SlotDomestic},
			{ID: 2, Key: "k2", IfName: "enp2s0", Source: "eth_pci", Present: true, Role: RoleUnassigned},
		},
		Req:      ChangeRequest{InterfaceID: 2, Role: RoleWAN, Slot: SlotSecondary},
		MgmtCIDR: "192.168.99.1/24",
	}
	vs := Validate(i)
	if Rejected(vs) {
		t.Fatalf("two-WAN box must be supported: %+v", vs)
	}
	var sawConfirm bool
	for _, v := range vs {
		if v.Rule == "V17" && v.Level == LevelConfirm {
			sawConfirm = true
			if !strings.Contains(v.Message, "enp1s0") {
				t.Errorf("confirm text must name the remaining management path: %q", v.Message)
			}
		}
	}
	if !sawConfirm {
		t.Errorf("no V17 confirm emitted: %+v", vs)
	}
}

func TestValidate_V14_LANCIDROverlaps(t *testing.T) {
	for _, cidr := range []string{
		"100.64.5.1/24",    // Starlink bypass space
		"192.168.99.1/24",  // mgmt
		"192.168.100.1/24", // Starlink dish API
		"127.0.0.1/8", "169.254.1.1/16", "224.0.0.1/4",
	} {
		i := in(ChangeRequest{InterfaceID: 1, Role: RoleLAN})
		i.LAN = &LANConfig{CIDR: cidr}
		vs := Validate(i)
		if r := firstReject(vs); r == nil || r.Rule != "V14" {
			t.Errorf("LAN CIDR %s accepted: %+v", cidr, vs)
		}
	}
	i := in(ChangeRequest{InterfaceID: 1, Role: RoleLAN})
	i.LAN = &LANConfig{CIDR: "10.77.0.1/24"}
	if Rejected(Validate(i)) {
		t.Errorf("the default LAN CIDR was rejected: %+v", Validate(i))
	}
}

func TestValidate_Warnings(t *testing.T) {
	// V22: a non-permaddr key ties the role to the port, not the device.
	i := in(ChangeRequest{InterfaceID: 3, Role: RoleWAN, Slot: SlotSecondary})
	i.Rows[2].KeyKind = "idpath"
	var sawV22 bool
	for _, v := range Validate(i) {
		if v.Rule == "V22" && v.Level == LevelWarn {
			sawV22 = true
			if !strings.Contains(strings.ToLower(v.Message), "port") {
				t.Errorf("V22 must explain the role is tied to the port: %q", v.Message)
			}
		}
	}
	if !sawV22 {
		t.Error("no V22 warning for an idpath key")
	}

	// V19: three or more assignable interfaces and no mgmt reserved.
	var sawV19 bool
	for _, v := range Validate(in(ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: SlotDomestic})) {
		if v.Rule == "V19" {
			sawV19 = true
		}
	}
	if !sawV19 {
		t.Error("no V19 warning offering to reserve a management port")
	}

	// V23: a USB 2.0 uplink tops out well below its nominal 480.
	i2 := in(ChangeRequest{InterfaceID: 3, Role: RoleWAN, Slot: SlotSecondary})
	i2.Rows[2].USBSpeedMbit = 480
	var sawV23 bool
	for _, v := range Validate(i2) {
		if v.Rule == "V23" {
			sawV23 = true
		}
	}
	if !sawV23 {
		t.Error("no V23 warning for a USB 2.0 uplink")
	}
}

// One radio is a station or an access point, never both.
func TestValidate_V13_OneRadioOneRole(t *testing.T) {
	i := in(ChangeRequest{InterfaceID: 4, Role: RoleLAN})
	i.Rows[3].PhyName = "phy0"
	i.Rows = append(i.Rows, NetworkInterface{
		ID: 9, Key: "k9", IfName: "wlp4s0-sta", Source: "wifi_pci",
		Present: true, Role: RoleWAN, PhyName: "phy0",
	})
	if r := firstReject(Validate(i)); r == nil || r.Rule != "V13" {
		t.Fatalf("AP+STA on one radio was accepted: %+v", Validate(i))
	}
}

func TestSourceAllows(t *testing.T) {
	cases := []struct {
		source string
		role   InterfaceRole
		want   bool
	}{
		{"eth_onboard", RoleWAN, true},
		{"eth_onboard", RoleLAN, true},
		{"eth_usb", RoleLANMember, true},
		{"wifi_pci", RoleWAN, true},  // station
		{"wifi_pci", RoleLAN, true},  // AP — gated further by V11
		{"wwan_usb", RoleLAN, false}, // uplink-only
		{"virt_bridge", RoleWAN, false},
		{"virt_bridge", RoleLAN, true},
		{"virt_other", RoleWAN, false},
		{"loopback", RoleWAN, false},
		{"tether_android", RoleWAN, true},
		{"tether_android", RoleLAN, false},
	}
	for _, c := range cases {
		if got := SourceAllows(c.source, c.role); got != c.want {
			t.Errorf("SourceAllows(%q, %q) = %v, want %v", c.source, c.role, got, c.want)
		}
	}
}

// lan0 is the bridge nasnet creates, not a port. Giving it a role renders a
// member file that enslaves it to itself, which shadows its address file and
// leaves dnsmasq with nothing to bind.
func TestValidate_V3RejectsTheManagedBridge(t *testing.T) {
	in := ValidationInput{
		Rows: []NetworkInterface{
			{ID: 3, IfName: "lan0", Source: "virt_bridge", Present: true},
		},
		Req: ChangeRequest{InterfaceID: 3, Role: RoleLAN},
	}
	vs := Validate(in)
	r := firstReject(vs)
	if r == nil {
		t.Fatalf("the managed bridge was accepted as a LAN port: %+v", vs)
	}
	if r.Rule != "V3" {
		t.Errorf("first reject = %s (%s), want V3", r.Rule, r.Message)
	}
}

// A bridge somebody else made is still a legitimate LAN.
func TestValidate_V3AllowsAForeignBridge(t *testing.T) {
	in := ValidationInput{
		Rows: []NetworkInterface{
			{ID: 3, IfName: "br-lab", Source: "virt_bridge", Present: true},
		},
		Req: ChangeRequest{InterfaceID: 3, Role: RoleLAN},
	}
	if r := firstReject(Validate(in)); r != nil && r.Rule == "V3" {
		t.Errorf("an unmanaged bridge was rejected: %+v", r)
	}
}

func TestSecondarySlotsAreOrderedAndSecondary(t *testing.T) {
	slots := SecondarySlots()
	want := []UplinkSlot{SlotSecondary, SlotSecondary2, SlotSecondary3, SlotSecondary4}
	if len(slots) != len(want) {
		t.Fatalf("got %d slots, want %d", len(slots), len(want))
	}
	for i := range want {
		if slots[i] != want[i] {
			t.Fatalf("slot %d = %q, want %q", i, slots[i], want[i])
		}
		if !slots[i].IsSecondary() {
			t.Fatalf("%q must report IsSecondary", slots[i])
		}
	}
	if SlotDomestic.IsSecondary() || SlotNone.IsSecondary() {
		t.Fatal("domestic and none are not secondaries")
	}
}

func TestValidate_V20_AcceptsEverySecondarySlot(t *testing.T) {
	for _, slot := range SecondarySlots() {
		vs := Validate(in(ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: slot}))
		if Rejected(vs) {
			t.Errorf("%s slot rejected: %+v", slot, vs)
		}
	}
}
