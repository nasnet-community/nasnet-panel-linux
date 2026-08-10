package system

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// The bridge is the ONE interface we name ourselves, because we create it.
func TestRenderLANNetdev(t *testing.T) {
	f := RenderLANNetdev(LANBridgeName)
	if f.Name != "20-nasnet-lan.netdev" {
		t.Errorf("Name = %q", f.Name)
	}
	for _, want := range []string{"[NetDev]", "Name=lan0", "Kind=bridge"} {
		if !strings.Contains(f.Content, want) {
			t.Errorf("missing %q in:\n%s", want, f.Content)
		}
	}
}

func TestRenderLANNetwork(t *testing.T) {
	f := RenderLANNetwork(domain.LANConfig{BridgeName: "lan0", CIDR: "10.77.0.1/24"})
	if f.Name != "30-nasnet-lan.network" {
		t.Errorf("Name = %q", f.Name)
	}
	for _, want := range []string{
		"Name=lan0", // the bridge is matched by name; it is ours
		"Address=10.77.0.1/24",
		"ConfigureWithoutCarrier=yes", // the bridge must have an address with no member up
		"IgnoreCarrierLoss=yes",
	} {
		if !strings.Contains(f.Content, want) {
			t.Errorf("missing %q in:\n%s", want, f.Content)
		}
	}
	// The LAN is a destination, not an egress path: no RouteTable=, no gateway.
	if strings.Contains(f.Content, "RouteTable=") || strings.Contains(f.Content, "Gateway=") {
		t.Errorf("the LAN bridge must not be an egress path:\n%s", f.Content)
	}
	if !strings.Contains(f.Content, "IPv6AcceptRA=no") {
		t.Error("IPv6 not disabled on the bridge")
	}
}

func TestRenderLANMember(t *testing.T) {
	f := RenderLANMember(domain.NetworkInterface{
		IfName: "enp4s0", PermMAC: "aa:bb:cc:dd:ee:04",
	}, "lan0")

	if !strings.Contains(f.Name, "21-nasnet-lanmember") {
		t.Errorf("Name = %q, want a 21-nasnet-lanmember prefix so it sorts after the bridge", f.Name)
	}
	for _, want := range []string{"PermanentMACAddress=aa:bb:cc:dd:ee:04", "Bridge=lan0"} {
		if !strings.Contains(f.Content, want) {
			t.Errorf("missing %q in:\n%s", want, f.Content)
		}
	}
	// No L3 of its own. Line-anchored, or PermanentMACAddress= matches.
	if strings.Contains(f.Content, "\nAddress=") || strings.Contains(f.Content, "\nDHCP=") {
		t.Errorf("a bridge member must have no L3 configuration:\n%s", f.Content)
	}
}

func TestRenderLANMember_UniqueFilenames(t *testing.T) {
	a := RenderLANMember(domain.NetworkInterface{IfName: "enp4s0", PermMAC: "aa:bb:cc:dd:ee:04"}, "lan0")
	b := RenderLANMember(domain.NetworkInterface{IfName: "enp5s0", PermMAC: "aa:bb:cc:dd:ee:05"}, "lan0")
	if a.Name == b.Name {
		t.Fatalf("both members rendered to %q; one would overwrite the other", a.Name)
	}
}

// A device with no permanent MAC still needs a stable, unique filename.
func TestRenderLANMember_NoPermMACFallsBackToTheIfName(t *testing.T) {
	f := RenderLANMember(domain.NetworkInterface{IfName: "enx001122"}, "lan0")
	if !strings.Contains(f.Name, "enx001122") {
		t.Errorf("Name = %q, want the interface name in it", f.Name)
	}
	if !strings.Contains(f.Content, "Name=enx001122") {
		t.Errorf("with no permanent MAC the match must be by name:\n%s", f.Content)
	}
}

// networkd applies files in basename order, so the names have to sort right.
func TestLANFileOrdering(t *testing.T) {
	names := []string{
		RenderLANNetdev(LANBridgeName).Name,
		RenderLANMember(domain.NetworkInterface{PermMAC: "aa:bb:cc:dd:ee:04"}, "lan0").Name,
		RenderLANNetwork(domain.LANConfig{BridgeName: "lan0", CIDR: "10.77.0.1/24"}).Name,
	}
	if !(names[0] < names[1] && names[1] < names[2]) {
		t.Errorf("basename order is wrong: %v", names)
	}
}

// Enslaving the bridge to itself renders a file that matches the same link as
// the bridge's own and sorts before it, so the address disappears.
func TestRenderLANMember_RefusesToEnslaveTheBridgeToItself(t *testing.T) {
	f := RenderLANMember(domain.NetworkInterface{IfName: "lan0"}, "lan0")
	if f.Name != "" || f.Content != "" {
		t.Errorf("rendered a self-enslaving member file: %+v", f)
	}
}
