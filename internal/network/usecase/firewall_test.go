package usecase

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func filterSpec() FilterInputSpec {
	return FilterInputSpec{
		Uplinks:          uplinksWithKeys(),
		LocalIfNames:     []string{"lo", "lan0", "enp3s0"},
		PanelPort:        9761,
		AdvertisedIfName: "enp1s0",
		Inbounds: []InboundSpec{
			{Tag: "vless-tcp", Proto: "tcp", Port: 443, Enabled: true},
			{Tag: "wg", Proto: "udp", Port: 51820, Enabled: true},
			{Tag: "old-trojan", Proto: "tcp", Port: 8443, Enabled: false},
		},
	}
}

func acceptFor(f *nft.FilterInput, proto string, port int) *nft.InputAccept {
	for i := range f.Accepts {
		if f.Accepts[i].Proto == proto && f.Accepts[i].Port == port {
			return &f.Accepts[i]
		}
	}
	return nil
}

// Every enabled inbound gets an accept and no disabled one does: an inbound
// cannot exist without its accept, and a stale accept is an open port.
func TestDeriveFilterInput_AcceptsExactlyTheEnabledInbounds(t *testing.T) {
	f := DeriveFilterInput(filterSpec())
	if f == nil {
		t.Fatal("nil filter input")
	}
	if acceptFor(f, "tcp", 443) == nil {
		t.Error("the enabled TCP inbound has no accept — this would kill somebody's VPN")
	}
	if acceptFor(f, "udp", 51820) == nil {
		t.Error("the enabled UDP inbound has no accept")
	}
	if acceptFor(f, "tcp", 8443) != nil {
		t.Error("a disabled inbound got an accept")
	}
}

// The panel port is accepted on the advertised uplink only, not on the secondary
// one. Closing it on Starlink is half the point of this task.
func TestDeriveFilterInput_PanelPortOnTheAdvertisedUplinkOnly(t *testing.T) {
	f := DeriveFilterInput(filterSpec())
	a := acceptFor(f, "tcp", 9761)
	if a == nil {
		t.Fatal("the panel port has no accept; the operator would be locked out")
	}
	if len(a.IfNames) != 1 || a.IfNames[0] != "enp1s0" {
		t.Errorf("panel accept covers %v, want only the advertised uplink", a.IfNames)
	}
}

// xray inbounds are reachable on every uplink, unlike the panel.
func TestDeriveFilterInput_InboundsAcceptedOnEveryUplink(t *testing.T) {
	a := acceptFor(DeriveFilterInput(filterSpec()), "tcp", 443)
	if len(a.IfNames) != 2 {
		t.Errorf("inbound accept covers %v, want both uplinks", a.IfNames)
	}
}

// Loopback, the LAN and the management port are always accepted: a firewall that
// can cut the recovery path is not a recovery path.
func TestDeriveFilterInput_AlwaysAcceptsLocalInterfaces(t *testing.T) {
	f := DeriveFilterInput(filterSpec())
	want := map[string]bool{"lo": true, "lan0": true, "enp3s0": true}
	for _, n := range f.LocalIfNames {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing always-accept interfaces: %v", want)
	}
}

// On a two-port box with no LAN, the panel accept is the only management path,
// so it must be provisioned automatically rather than left to the operator.
func TestDeriveFilterInput_TwoWANNoLANStillKeepsThePanelReachable(t *testing.T) {
	spec := filterSpec()
	spec.LocalIfNames = []string{"lo"}
	spec.TwoWANNoLAN = true

	f := DeriveFilterInput(spec)
	a := acceptFor(f, "tcp", 9761)
	if a == nil {
		t.Fatal("no panel accept on a two-WAN box; the firewall would cut the only management path")
	}
	if len(a.IfNames) == 0 {
		t.Error("the panel accept names no interface")
	}
	// Losing the advertised uplink on that box shape leaves no way back in.
	if len(a.IfNames) != 2 {
		t.Errorf("panel accept covers %v, want every uplink when it is the only path", a.IfNames)
	}
}

// With no advertised uplink recorded, being reachable too widely is
// recoverable; being unreachable is not.
func TestDeriveFilterInput_NoAdvertisedUplinkFallsBackToAll(t *testing.T) {
	spec := filterSpec()
	spec.AdvertisedIfName = ""
	a := acceptFor(DeriveFilterInput(spec), "tcp", 9761)
	if a == nil || len(a.IfNames) != 2 {
		t.Fatalf("panel accept = %+v, want every uplink", a)
	}
}

// A port forward's DNAT is matched by `ct status dnat` in filter_fwd, but a
// forward terminating on the box needs an input accept of its own.
func TestDeriveFilterInput_BoxTerminatedForwardGetsAnAccept(t *testing.T) {
	spec := filterSpec()
	spec.PortForwards = []domain.PortForward{
		{UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
			ToAddr: "127.0.0.1", ToPort: 8080, Enabled: true},
	}
	a := acceptFor(DeriveFilterInput(spec), "tcp", 8080)
	if a == nil {
		t.Fatal("a box-terminated forward has no input accept, so it would be dropped")
	}
	if len(a.IfNames) != 1 || a.IfNames[0] != "enp1s0" {
		t.Errorf("accept covers %v, want only the uplink the row names", a.IfNames)
	}
}

// A forward to a LAN host is handled by filter_fwd; opening the port on the box
// itself would be an exposure nobody asked for.
func TestDeriveFilterInput_LANTargetedForwardGetsNoInputAccept(t *testing.T) {
	spec := filterSpec()
	spec.PortForwards = []domain.PortForward{
		{UplinkKey: "", Proto: "tcp", DPort: 8080,
			ToAddr: "10.77.0.5", ToPort: 80, Enabled: true},
	}
	if acceptFor(DeriveFilterInput(spec), "tcp", 8080) != nil {
		t.Error("a LAN-targeted forward opened a port on the box")
	}
}

// With no uplinks there is nothing to filter, and emitting a drop policy would
// be a self-inflicted outage.
func TestDeriveFilterInput_NoUplinksMeansNoInputChain(t *testing.T) {
	spec := filterSpec()
	spec.Uplinks = nil
	if DeriveFilterInput(spec) != nil {
		t.Error("an input drop policy was emitted with no uplinks assigned")
	}
}

// The transport a protocol listens on decides tcp or udp. Either-transport
// protocols emit both: an extra accept is recoverable, a missing one is not.
func TestInboundSpecsFor(t *testing.T) {
	cases := []struct {
		protocol string
		want     []string
	}{
		{"vless", []string{"tcp"}},
		{"vmess", []string{"tcp"}},
		{"trojan", []string{"tcp"}},
		{"wireguard", []string{"udp"}},
		{"hysteria2", []string{"udp"}},
		{"shadowsocks", []string{"tcp", "udp"}},
		{"dokodemo-door", []string{"tcp", "udp"}},
		{"", []string{"tcp"}},
	}
	for _, c := range cases {
		t.Run(c.protocol, func(t *testing.T) {
			got := InboundSpecsFor("tag", c.protocol, 443, "", true)
			if len(got) != len(c.want) {
				t.Fatalf("%s -> %d specs, want %d: %+v", c.protocol, len(got), len(c.want), got)
			}
			for i, proto := range c.want {
				if got[i].Proto != proto || got[i].Port != 443 || !got[i].Enabled {
					t.Errorf("spec %d = %+v, want %s/443 enabled", i, got[i], proto)
				}
			}
		})
	}
}

// A disabled inbound still produces a spec — DeriveFilterInput is what drops it,
// and keeping the row visible means the count never silently disagrees.
func TestInboundSpecsFor_DisabledIsCarriedThrough(t *testing.T) {
	got := InboundSpecsFor("old", "vless", 8443, "", false)
	if len(got) != 1 || got[0].Enabled {
		t.Errorf("got %+v, want one disabled spec", got)
	}
}

func TestInboundSpecsFor_NoPortIsSkipped(t *testing.T) {
	if got := InboundSpecsFor("broken", "vless", 0, "", true); len(got) != 0 {
		t.Errorf("got %+v, want nothing for a portless inbound", got)
	}
}
