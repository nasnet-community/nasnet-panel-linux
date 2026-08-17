package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

type mismatchOpts struct {
	vpnActive    bool
	dropRulePref int
	dropChain    string
	dropVPNRoute bool
	extraRule    bool
}

// The expected state IS the generator: a fixture that hand-writes rules would
// only prove the fixture agrees with itself.
func newMismatchFixture(t *testing.T, o mismatchOpts) (*networkUsecase, flowMismatchInput) {
	t.Helper()
	u := newFlowFixture(t, flowOpts{vpnActive: o.vpnActive, wgFresh: true})

	uplinks, err := u.uplinks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	plane := u.vpnPlaneNow(t.Context())
	vpn := VPNRouteState{Active: plane.Active()}

	live := AllRules(flowGroups(), uplinks, vpn)
	if o.dropRulePref > 0 {
		kept := live[:0]
		for _, r := range live {
			if r.Pref != o.dropRulePref {
				kept = append(kept, r)
			}
		}
		live = kept
	}
	if o.extraRule {
		live = append(live, system.Rule{Pref: 175, FwMark: 0x30000, FwMask: 0xff0000, Table: 201})
	}

	routes := map[int][]system.Route{
		201: {{Table: 201, Dest: "default", Gateway: "192.0.2.1", OifName: "eth0"}},
		202: {{Table: 202, Dest: "default", Gateway: "100.64.0.1", OifName: "eth1"}},
	}
	if o.vpnActive && !o.dropVPNRoute {
		routes[system.WGTable] = vpnRoutes(uplinks)
	} else if o.vpnActive {
		routes[system.WGTable] = nil
	}

	// The desired nft state has to be real too, or every chain reads as missing.
	if err := u.Nft.Update(t.Context(), func(rs *nft.Ruleset) {
		rs.Connmark, rs.Counters = true, true
		rs.KillSwitch = &nft.KillSwitch{SecondaryIfName: "eth1", MarkMask: 0xf000000, MarkValue: 0x2000000}
	}); err != nil {
		t.Fatal(err)
	}
	desired := u.Nft.Snapshot()
	obj := &system.NftObjects{Counters: map[string]system.NftCounter{}}
	for _, c := range desired.ChainNames() {
		if c == o.dropChain {
			continue
		}
		obj.Chains = append(obj.Chains, c)
	}
	obj.Sets = append(obj.Sets, desired.SetNames()...)

	// dnsmasq is not running under test, and that check has its own case.
	return u, flowMismatchInput{
		uplinks: uplinks, vpn: vpn, plane: plane,
		liveRules: live, routes: routes, nftObj: obj,
		lan: &domain.LANConfig{BridgeName: "lan0", Enabled: false},
	}
}

func mismatchByRule(t *testing.T, got []FlowMismatch, rule string) FlowMismatch {
	t.Helper()
	for _, m := range got {
		if m.Rule == rule {
			return m
		}
	}
	t.Fatalf("no %q mismatch in %+v", rule, got)
	return FlowMismatch{}
}

func TestMismatchCleanSystemIsQuiet(t *testing.T) {
	u, in := newMismatchFixture(t, mismatchOpts{vpnActive: true})
	if got := u.flowMismatches(t.Context(), in); len(got) != 0 {
		t.Fatalf("clean fixture produced: %+v", got)
	}
}

func TestMismatchRuleMissing(t *testing.T) {
	u, in := newMismatchFixture(t, mismatchOpts{vpnActive: true, dropRulePref: 150})
	m := mismatchByRule(t, u.flowMismatches(t.Context(), in), "rule-missing")
	if m.NodeID != "mark-foreign" || m.Severity != "error" {
		t.Fatalf("%+v", m)
	}
	if m.Expected == "" || m.Actual != "absent" {
		t.Fatalf("evidence missing: %+v", m)
	}
}

func TestMismatchUnexpectedRule(t *testing.T) {
	u, in := newMismatchFixture(t, mismatchOpts{vpnActive: true, extraRule: true})
	m := mismatchByRule(t, u.flowMismatches(t.Context(), in), "rule-unexpected")
	if m.Severity != "warn" {
		t.Fatalf("%+v", m)
	}
}

func TestMismatchMissingChain(t *testing.T) {
	u, in := newMismatchFixture(t, mismatchOpts{vpnActive: true, dropChain: "killswitch_out"})
	m := mismatchByRule(t, u.flowMismatches(t.Context(), in), "nft-chain-missing")
	if m.NodeID != "killswitch" {
		t.Fatalf("%+v", m)
	}
}

func TestMismatchVPNRouteGone(t *testing.T) {
	u, in := newMismatchFixture(t, mismatchOpts{vpnActive: true, dropVPNRoute: true})
	m := mismatchByRule(t, u.flowMismatches(t.Context(), in), "route-missing")
	if m.NodeID != "table-203" {
		t.Fatalf("%+v", m)
	}
}

func TestMismatchDNSMasqDown(t *testing.T) {
	u, in := newMismatchFixture(t, mismatchOpts{vpnActive: true})
	in.lan = &domain.LANConfig{BridgeName: "lan0", Enabled: true}
	u.resolverStatus = func(context.Context) system.DNSMasqStatus {
		return system.DNSMasqStatus{Installed: true, Running: false}
	}
	m := mismatchByRule(t, u.flowMismatches(t.Context(), in), "dnsmasq-not-running")
	if m.NodeID != "dns" || m.Severity != "error" {
		t.Fatalf("%+v", m)
	}
}
