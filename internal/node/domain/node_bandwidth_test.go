package domain

import (
	"strings"
	"testing"
)

// Derived from the role, not typed by hand. eth0 was only right by accident.
func TestResolveShapingInterface_DerivesFromTheIngressUplink(t *testing.T) {
	n := &Node{BandwidthSettings: &BandwidthSettings{Enabled: true, TotalBW: 1000}}
	iface, warn := n.ResolveShapingInterface("enp1s0")
	if iface != "enp1s0" {
		t.Errorf("iface = %q, want the ingress uplink", iface)
	}
	if warn != "" {
		t.Errorf("unexpected warning: %q", warn)
	}
}

func TestResolveShapingInterface_OverrideWinsButWarnsWhenItIsNotTheUplink(t *testing.T) {
	n := &Node{BandwidthSettings: &BandwidthSettings{
		Enabled: true, TotalBW: 1000, InterfaceOverride: "enp9s0",
	}}
	iface, warn := n.ResolveShapingInterface("enp1s0")
	if iface != "enp9s0" {
		t.Errorf("iface = %q, want the override", iface)
	}
	if warn == "" || !strings.Contains(warn, "enp1s0") {
		t.Errorf("warning must name the derived uplink: %q", warn)
	}
}

func TestResolveShapingInterface_OverrideMatchingTheUplinkDoesNotWarn(t *testing.T) {
	n := &Node{BandwidthSettings: &BandwidthSettings{
		Enabled: true, TotalBW: 1000, InterfaceOverride: "enp1s0",
	}}
	if _, warn := n.ResolveShapingInterface("enp1s0"); warn != "" {
		t.Errorf("unexpected warning: %q", warn)
	}
}

// With no uplink known, fall back rather than shape the wrong device.
func TestResolveShapingInterface_NoUplinkFallsBackToTheStoredValue(t *testing.T) {
	n := &Node{BandwidthSettings: &BandwidthSettings{Enabled: true, Interface: "eth0", TotalBW: 1000}}
	iface, warn := n.ResolveShapingInterface("")
	if iface != "eth0" {
		t.Errorf("iface = %q, want the stored legacy value", iface)
	}
	if warn == "" {
		t.Error("falling back to a hand-typed interface should warn")
	}
}

// The existing default must not change shape.
func TestGetBandwidthSettingsOrDefault_Unchanged(t *testing.T) {
	def := (&Node{}).GetBandwidthSettingsOrDefault()
	if def.Enabled || def.Interface != "eth0" || def.TotalBW != 1000 {
		t.Errorf("default changed: %+v", def)
	}
	if def.InterfaceOverride != "" {
		t.Error("InterfaceOverride must default empty")
	}
}
