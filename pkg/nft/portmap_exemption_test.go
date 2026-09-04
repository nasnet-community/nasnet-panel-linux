package nft

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func TestPortmapExemption(t *testing.T) {
	rs := Ruleset{KillSwitch: &KillSwitch{
		Legs:        []KillSwitchLeg{{IfName: "eth1", GatewayIP: "192.168.8.1", PinValue: netmark.PinMark(2)}},
		MarkMask:    netmark.MaskPin,
		PortmapMark: netmark.PinMark(netmark.PinPortmap),
	}}
	out := rs.Render()
	want := "meta mark and " + netmark.Hex(netmark.MaskPin) + " == " +
		netmark.Hex(netmark.PinMark(netmark.PinPortmap)) + " udp dport { 1900, 5351 } accept"
	if !strings.Contains(out, want) {
		t.Fatalf("kill switch missing portmap exemption %q in:\n%s", want, out)
	}

	rs.KillSwitch.PortmapMark = 0
	if strings.Contains(rs.Render(), "1900, 5351") {
		t.Fatal("exemption rendered with zero mark")
	}
}
