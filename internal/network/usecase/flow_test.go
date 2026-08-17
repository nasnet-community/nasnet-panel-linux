package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

type flowOpts struct {
	vpnActive bool
	wgFresh   bool
	noLAN     bool
	nftErr    bool
	noUplinks bool
}

// flowIfRepo answers GetByRole for real; the shared stub always returns nil.
type flowIfRepo struct {
	stubIfRepo
}

func (f *flowIfRepo) GetByRole(_ context.Context, role domain.InterfaceRole) ([]domain.NetworkInterface, error) {
	var out []domain.NetworkInterface
	for _, r := range f.rows {
		if r.Role == role {
			out = append(out, r)
		}
	}
	return out, nil
}

// The prefs the repository seeds; the rule generator reads them from here.
func flowGroups() []domain.WANGroup {
	return []domain.WANGroup{
		{ID: 1, NodeID: 1, Name: "domestic", GroupIndex: netmark.GroupDomestic,
			RuleBase: 110, RuleBlackhole: 149, Policy: domain.PolicyFailover},
		{ID: 2, NodeID: 1, Name: "foreign", GroupIndex: netmark.GroupForeign,
			RuleBase: 150, RuleBlackhole: 199, Policy: domain.PolicyFailover},
	}
}

func flowIfRows() []domain.NetworkInterface {
	return []domain.NetworkInterface{
		{ID: 1, IfName: "eth0", Key: "eth0", Role: domain.RoleWAN, Slot: domain.SlotDomestic,
			Present: true, Healthy: true, LearnedGateway: "192.0.2.1"},
		{ID: 2, IfName: "eth1", Key: "eth1", Role: domain.RoleWAN, Slot: domain.SlotSecondary,
			Present: true, Healthy: true, LearnedGateway: "100.64.0.1"},
	}
}

func newFlowFixture(t *testing.T, o flowOpts) *networkUsecase {
	t.Helper()

	rows := flowIfRows()
	if o.noUplinks {
		rows = nil
	}
	be := system.NewFakeBackend()
	be.Routes = []system.Route{
		{Table: 201, Dest: "default", Gateway: "192.0.2.1", OifName: "eth0"},
		{Table: 202, Dest: "default", Gateway: "100.64.0.1", OifName: "eth1"},
	}
	if o.vpnActive {
		be.Routes = append(be.Routes,
			system.Route{Table: system.WGTable, Dest: "default", OifName: system.WGLinkName},
			system.Route{Table: system.WGTable, Dest: StarlinkDishSubnet, OifName: "eth1", Scope: "link"})
	}

	repo := &fakeVPNRepo{}
	wg := &system.FakeWGDevice{}
	if o.vpnActive {
		repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true, Config: wgConfigJSON(t, nil)}}
		wg.Applied = &system.WGApplyConfig{}
		if o.wgFresh {
			wg.Stat = &system.WGStatus{
				LastHandshake: time.Now().Add(-10 * time.Second), RxBytes: 4096, TxBytes: 8192,
				Endpoint: "1.2.3.4:51820", PublicKey: tPub,
			}
		} else {
			wg.Stat = &system.WGStatus{Endpoint: "1.2.3.4:51820", PublicKey: tPub}
		}
	}

	fnft := &system.FakeNft{
		Objects: system.NftObjects{
			Chains: []string{"mangle_pre", "filter_in", "filter_fwd",
				"killswitch_out", "killswitch_fwd", "mangle_post", "nat_post"},
			Sets: []string{"ir_v4", "ir_dom_v4", nft.SetDoHBootstrap},
			Counters: map[string]system.NftCounter{
				nft.CounterDomestic:   {Packets: 10, Bytes: 1000},
				nft.CounterForeign:    {Packets: 20, Bytes: 2000},
				nft.CounterKillSwitch: {Packets: 5, Bytes: 300},
			},
		},
		RulesetText: "table inet nasnet {\n\tchain mangle_pre {\n\t\tct mark != 0x0 meta mark set ct mark and 0xfffffff\n\t}\n}\n",
	}
	if o.nftErr {
		fnft.Err = errors.New("nft: command not found")
	}

	var lanRepo *stubLANRepo
	if o.noLAN {
		lanRepo = &stubLANRepo{cfg: &domain.LANConfig{BridgeName: "lan0", Enabled: false}}
	} else {
		lanRepo = &stubLANRepo{cfg: &domain.LANConfig{
			BridgeName: "lan0", CIDR: "10.77.0.1/24", Enabled: true,
			DHCPRangeLow: "10.77.0.100", DHCPRangeHigh: "10.77.0.200", LeaseHours: 12,
		}}
	}

	u := &networkUsecase{Deps: Deps{
		IfRepo:     &flowIfRepo{stubIfRepo{rows: rows}},
		GroupRepo:  &stubGroupRepo{groups: flowGroups()},
		LANRepo:    lanRepo,
		VPNRepo:    repo,
		WG:         wg,
		Nftr:       fnft,
		Flow: &system.FakeFlowSource{Stats: map[string]system.LinkStat{
			"eth0":            {RxBytes: 1000, TxBytes: 2000},
			"eth1":            {RxBytes: 3000, TxBytes: 4000},
			system.WGLinkName: {RxBytes: 4096, TxBytes: 8192},
		}},
		Backend:    be,
		Nft:        nft.NewManager(&nft.FakeApplier{}),
		Paths:      testPaths(t),
		RouterMode: true,
	}}
	u.dnsmasq = system.NewDNSMasq()
	return u
}

func nodeByID(t *testing.T, v *FlowView, id string) FlowNode {
	t.Helper()
	for _, n := range v.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %q in %v", id, nodeIDs(v))
	return FlowNode{}
}

func nodeIDs(v *FlowView) []string {
	out := make([]string, 0, len(v.Nodes))
	for _, n := range v.Nodes {
		out = append(out, n.ID)
	}
	return out
}

func edgeByID(t *testing.T, v *FlowView, id string) FlowEdge {
	t.Helper()
	for _, e := range v.Edges {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no edge %q", id)
	return FlowEdge{}
}

func TestFlowGraphVPNActivePath(t *testing.T) {
	u := newFlowFixture(t, flowOpts{vpnActive: true, wgFresh: true})
	view, err := u.FlowGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n := nodeByID(t, view, "wg"); n.Status != "ok" {
		t.Fatalf("wg status %q hint %q", n.Status, n.Hint)
	}
	if e := edgeByID(t, view, "e-for-203"); e.Status != "ok" {
		t.Fatalf("foreign→203 %q", e.Status)
	}
	if edgeByID(t, view, "e-for-ks").Status != "ghost" {
		t.Fatal("killswitch edge must be ghost while VPN is up")
	}
	if view.Counters["wg"].RxBytes == 0 {
		t.Fatal("wg counter missing")
	}
	if _, ok := view.Counters["if:eth0"]; !ok {
		t.Fatal("interface counter missing")
	}
	if view.Counters["nft:killswitch"].Packets == 0 {
		t.Fatal("nft counter not mapped")
	}
}

func TestFlowGraphVPNInactiveShowsTheDrop(t *testing.T) {
	u := newFlowFixture(t, flowOpts{vpnActive: false})
	view, err := u.FlowGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n := nodeByID(t, view, "wg"); n.Status != "ghost" || n.Hint == "" {
		t.Fatalf("wg should be a hinted ghost: %+v", n)
	}
	if edgeByID(t, view, "e-for-ks").Status != "ok" {
		t.Fatal("foreign traffic must visibly die at the kill switch")
	}
	if edgeByID(t, view, "e-for-203").Status != "ghost" {
		t.Fatal("203 path must be ghosted")
	}
	if nodeByID(t, view, "killswitch").Status != "warn" {
		t.Fatal("killswitch should draw attention while VPN is down")
	}
	// A live secondary uplink is not reachability: the kill switch stops
	// everything, so claiming foreign sites are up would be a lie.
	if n := nodeByID(t, view, "world-foreign"); n.Status != "ghost" || n.Hint == "" {
		t.Fatalf("world-foreign: %+v", n)
	}
	if n := nodeByID(t, view, "world-domestic"); n.Status != "ok" {
		t.Fatalf("domestic must stay reachable: %+v", n)
	}
}

func TestFlowGraphNoLANIsGhosted(t *testing.T) {
	u := newFlowFixture(t, flowOpts{noLAN: true})
	view, err := u.FlowGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n := nodeByID(t, view, "src-lan"); n.Status != "ghost" || n.Hint == "" {
		t.Fatalf("src-lan: %+v", n)
	}
}

func TestFlowGraphSurvivesReadErrors(t *testing.T) {
	u := newFlowFixture(t, flowOpts{nftErr: true})
	view, err := u.FlowGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range view.Mismatches {
		if m.Rule == "nft-unreadable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("nft read failure must surface as a mismatch, got %+v", view.Mismatches)
	}
}

func TestFlowGraphGhostsMissingUplinks(t *testing.T) {
	u := newFlowFixture(t, flowOpts{noUplinks: true})
	view, err := u.FlowGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"uplink-domestic", "uplink-secondary", "killswitch"} {
		if n := nodeByID(t, view, id); n.Status != "ghost" {
			t.Errorf("%s status %q, want ghost", id, n.Status)
		}
	}
}
