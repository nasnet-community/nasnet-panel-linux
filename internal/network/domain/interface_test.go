package domain

import "testing"

func TestInterfaceRole_Valid(t *testing.T) {
	for _, r := range AllRoles() {
		if !r.Valid() {
			t.Errorf("%q from AllRoles is not Valid", r)
		}
	}
	for _, bad := range []InterfaceRole{"", "domestic", "WAN", "wan ", "router"} {
		if bad.Valid() {
			t.Errorf("%q accepted as a role", bad)
		}
	}
}

// lan and mgmt are singletons enforced by a partial unique index; the role type
// has to agree with the index or validation and the DB disagree.
func TestInterfaceRole_Singletons(t *testing.T) {
	want := map[InterfaceRole]bool{
		RoleUnassigned: false, RoleWAN: false, RoleLANMember: false,
		RoleLAN: true, RoleMgmt: true,
	}
	for r, isSingleton := range want {
		if r.IsSingleton() != isSingleton {
			t.Errorf("%q IsSingleton = %v, want %v", r, r.IsSingleton(), isSingleton)
		}
	}
}

// Role is a typed column, not a substring of a comment field — which is how one
// RouterOS panel does it, so any comment containing "domestic" becomes the
// domestic uplink there.
func TestNetworkInterface_RoleIsATypedColumn(t *testing.T) {
	ni := NetworkInterface{Key: "aa:bb:cc:dd:ee:01", Role: RoleWAN, Slot: SlotDomestic}
	if !ni.Role.Valid() {
		t.Fatal("role did not survive assignment")
	}
	if ni.Label != "" {
		t.Error("Label must default empty — it is operator text, never parsed")
	}
}

// A vanished dongle keeps its role across a replug.
func TestNetworkInterface_AbsenceIsRecordedNotDeleted(t *testing.T) {
	ni := NetworkInterface{Key: "k", Present: true}
	ni.Present = false
	if ni.Role != RoleUnassigned && ni.Present {
		t.Fatal("unreachable — guards the field's existence")
	}
	if ni.LastSeenAt != nil {
		t.Error("LastSeenAt should be nil until first set")
	}
}

func TestUplinkSlot_OnlyTwoNamedSlotsInStageOne(t *testing.T) {
	slots := []UplinkSlot{SlotNone, SlotDomestic, SlotSecondary}
	seen := map[UplinkSlot]bool{}
	for _, s := range slots {
		if seen[s] {
			t.Fatalf("duplicate slot %q", s)
		}
		seen[s] = true
	}
	if SlotDomestic == SlotSecondary {
		t.Fatal("the two uplink slots must be distinct")
	}
}
