package netmark

import "testing"

func TestFields_DoNotOverlap(t *testing.T) {
	all := MaskTier | MaskGroup | MaskPin | MaskReserved
	if all != 0xffffffff {
		t.Fatalf("fields do not tile the word: got 0x%08x", all)
	}
	pairs := []struct {
		name string
		a, b uint32
	}{
		{"tier/group", MaskTier, MaskGroup},
		{"tier/pin", MaskTier, MaskPin},
		{"group/pin", MaskGroup, MaskPin},
		{"pin/reserved", MaskPin, MaskReserved},
	}
	for _, p := range pairs {
		if p.a&p.b != 0 {
			t.Errorf("%s overlap: 0x%08x", p.name, p.a&p.b)
		}
	}
}

// MaskAll is what every conntrack save/restore and every tc fw filter uses.
func TestMaskAll_CoversTierGroupPin_ExcludesReserved(t *testing.T) {
	if want := MaskTier | MaskGroup | MaskPin; MaskAll != want {
		t.Fatalf("MaskAll = 0x%08x, want 0x%08x", MaskAll, want)
	}
	if MaskAll&MaskReserved != 0 {
		t.Errorf("MaskAll must not include the reserved nibble")
	}
	if MaskAll == 0x00ffffff {
		t.Fatal("MaskAll is 0x00ffffff — that zeroes the ingress pin on every restore")
	}
}

func TestGroupAndPin_RoundTrip(t *testing.T) {
	for _, g := range []uint32{GroupDomestic, GroupForeign, 3, 255} {
		if got := Group(GroupMark(g)); got != g {
			t.Errorf("Group(GroupMark(%d)) = %d", g, got)
		}
	}
	for _, i := range []uint32{1, 2, 15} {
		if got := Pin(PinMark(i)); got != i {
			t.Errorf("Pin(PinMark(%d)) = %d", i, got)
		}
	}
}

// The exact constants every rendered rule in this feature depends on.
func TestKnownMarkValues(t *testing.T) {
	cases := []struct {
		got  uint32
		want uint32
		name string
	}{
		{GroupMark(GroupDomestic), 0x00010000, "domestic group"},
		{GroupMark(GroupForeign), 0x00020000, "foreign group"},
		{PinMark(1), 0x01000000, "pin uplink 1"},
		{PinMark(2), 0x02000000, "pin uplink 2"},
		{MaskAll, 0x0fffffff, "MaskAll"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = 0x%08x, want 0x%08x", c.name, c.got, c.want)
		}
	}
}

// The tier field must survive a group and a pin being merged + the highest existing tier (500 Mbit = 0x1f4) must fit
func TestTierSurvivesGroupAndPin(t *testing.T) {
	m := WithPin(WithGroup(500, GroupForeign), 1)
	if got := Tier(m); got != 500 {
		t.Errorf("tier = %d, want 500 (mark 0x%08x)", got, m)
	}
	if got := Group(m); got != GroupForeign {
		t.Errorf("group = %d, want %d", got, GroupForeign)
	}
	if got := Pin(m); got != 1 {
		t.Errorf("pin = %d, want 1", got)
	}
}

// Merging replaces the target field and leaves the others alone.
func TestWith_ReplacesOnlyItsOwnField(t *testing.T) {
	m := WithGroup(WithGroup(42, GroupDomestic), GroupForeign)
	if got := Group(m); got != GroupForeign {
		t.Errorf("group = %d, want %d", got, GroupForeign)
	}
	if got := Tier(m); got != 42 {
		t.Errorf("tier clobbered: %d", got)
	}
	p := WithPin(WithPin(0, 1), 2)
	if got := Pin(p); got != 2 {
		t.Errorf("pin = %d, want 2", got)
	}
}

func TestHex(t *testing.T) {
	if got := Hex(MaskAll); got != "0xfffffff" {
		t.Errorf("Hex(MaskAll) = %q", got)
	}
	if got := Hex(PinMark(1)); got != "0x1000000" {
		t.Errorf("Hex(PinMark(1)) = %q", got)
	}
}

func TestProbePinStaysInsidePinField(t *testing.T) {
	m := PinMark(PinProbe)
	if m&^MaskPin != 0 {
		t.Fatalf("probe pin leaks outside the pin field: %#x", m)
	}
	if Pin(m) != PinProbe {
		t.Fatalf("round trip: got %d", Pin(m))
	}
}
