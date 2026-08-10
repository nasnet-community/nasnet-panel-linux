package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func TestRenderPortForwards_AnyUplinkExpandsToEveryUplink(t *testing.T) {
	rows := []domain.PortForward{
		{UplinkKey: "", Proto: "tcp", DPort: 443, ToAddr: "10.77.0.5", ToPort: 443, Enabled: true},
	}
	got := RenderPortForwards(rows, twoUplinks())
	if len(got) != 2 {
		t.Fatalf("got %d rules for an any-uplink forward, want one per uplink: %+v", len(got), got)
	}
	names := map[string]bool{got[0].IfName: true, got[1].IfName: true}
	if !names["enp1s0"] || !names["enp2s0"] {
		t.Errorf("rules do not cover both uplinks: %+v", got)
	}
}

func TestRenderPortForwards_SkipsDisabledAndUnknownUplinks(t *testing.T) {
	rows := []domain.PortForward{
		{UplinkKey: "", Proto: "tcp", DPort: 1, ToAddr: "10.77.0.5", ToPort: 1, Enabled: false},
		{UplinkKey: "zz", Proto: "tcp", DPort: 2, ToAddr: "10.77.0.5", ToPort: 2, Enabled: true},
	}
	if got := RenderPortForwards(rows, twoUplinks()); len(got) != 0 {
		t.Errorf("rendered %d rules, want 0: %+v", len(got), got)
	}
}

func TestRenderPortForwards_CarriesTheComment(t *testing.T) {
	rows := []domain.PortForward{{UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
		ToAddr: "10.77.0.5", ToPort: 80, Comment: "nas web ui", Enabled: true}}
	got := RenderPortForwards(rows, uplinksWithKeys())
	if len(got) != 1 || got[0].Comment != "nas web ui" {
		t.Errorf("got %+v", got)
	}
	if got[0].IfName != "enp1s0" {
		t.Errorf("rendered on %q, want the uplink the key names", got[0].IfName)
	}
}

// Port forwards are independent of the LAN: a forward may target the box
// itself, and the DNAT chain must not depend on a bridge existing.
func TestApplyPortForwards_DoesNotTouchLANState(t *testing.T) {
	ctx := context.Background()
	m := nft.NewManager(&nft.FakeApplier{})
	if err := ApplyNftState(ctx, m, twoUplinks()); err != nil {
		t.Fatal(err)
	}

	rows := []domain.PortForward{{UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
		ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}}
	if err := ApplyPortForwards(ctx, m, rows, uplinksWithKeys()); err != nil {
		t.Fatal(err)
	}

	rs := m.Snapshot()
	if len(rs.PortForwards) != 1 {
		t.Fatalf("PortForwards = %+v", rs.PortForwards)
	}
	if rs.LANClassify != nil || rs.FilterForward != nil {
		t.Error("applying a port forward turned on LAN state")
	}
	if !rs.Connmark || len(rs.IngressPins) != 2 {
		t.Errorf("the ingress pins the reply path needs were damaged: %+v", rs)
	}
}
