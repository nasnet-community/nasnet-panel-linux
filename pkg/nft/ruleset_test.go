package nft

import (
	"os"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(b)
}

func TestRender_ConnmarkOnly(t *testing.T) {
	got := Ruleset{Connmark: true}.Render()
	if want := golden(t, "connmark_only.nft"); got != want {
		t.Errorf("render mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The first three lines are the atomic replace: implicit add, delete, re-add.
// A bare `delete` on a missing table aborts the whole transaction.
func TestRender_StartsWithIdempotentReplacePreamble(t *testing.T) {
	lines := strings.Split(Ruleset{}.Render(), "\n")
	want := []string{
		"table inet nasnet",
		"delete table inet nasnet",
		"table inet nasnet {",
	}
	for i, w := range want {
		if i >= len(lines) || lines[i] != w {
			t.Fatalf("line %d = %q, want %q", i, lines[i], w)
		}
	}
}

// An empty ruleset must still be valid input that clears the table.
func TestRender_EmptyRulesetIsAWellFormedTable(t *testing.T) {
	got := Ruleset{}.Render()
	if !strings.HasSuffix(got, "}\n") {
		t.Errorf("render does not close the table: %q", got)
	}
	if strings.Contains(got, "chain ") {
		t.Errorf("empty ruleset emitted a chain:\n%s", got)
	}
}

func TestRender_IngressPins(t *testing.T) {
	got := Ruleset{
		Connmark: true,
		IngressPins: []Pin{
			{IfName: "enp1s0", Index: 1},
			{IfName: "enp2s0", Index: 2},
		},
	}.Render()

	// The pin is stamped into ct mark in PREROUTING, not postrouting:
	// locally-terminated packets go prerouting -> input and never traverse
	// postrouting, so a save rule there would never see them.
	for _, want := range []string{
		`iifname "enp1s0" ct state new ct mark set ct mark and 0xf0ffffff or 0x1000000`,
		`iifname "enp2s0" ct state new ct mark set ct mark and 0xf0ffffff or 0x2000000`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing pin rule %q in:\n%s", want, got)
		}
	}

	// The restore must come AFTER both pin rules, so the first packet of an
	// inbound flow already carries its pin when rp_filter and
	// tcp_fwmark_accept look at it.
	restore := strings.Index(got, "meta mark set ct mark and")
	lastPin := strings.LastIndex(got, "ct mark set ct mark and 0xf0ffffff")
	if restore < lastPin {
		t.Errorf("restore rule precedes a pin rule; ordering is load-bearing:\n%s", got)
	}
}

// Nothing may write the reserved nibble: tc's `action connmark` copies ct mark
// into the skb mark with no mask available.
func TestRender_NeverEmitsReservedBits(t *testing.T) {
	rs := Ruleset{Connmark: true, IngressPins: []Pin{{IfName: "enp1s0", Index: 1}}}
	if strings.Contains(rs.Render(), netmark.Hex(netmark.MaskReserved)) {
		t.Error("rendered ruleset references the reserved nibble")
	}
}

// tc.Teardown clears Connmark, so pins must imply it or pinning breaks.
func TestRender_IngressPinsImplyConnmark(t *testing.T) {
	got := Ruleset{IngressPins: []Pin{{IfName: "enp1s0", Index: 1}}}.Render()

	if !strings.Contains(got, "meta mark set ct mark and") {
		t.Errorf("pins rendered without the restore rule:\n%s", got)
	}
	if !strings.Contains(got, "chain mangle_post") {
		t.Errorf("pins rendered without the save chain:\n%s", got)
	}
}
