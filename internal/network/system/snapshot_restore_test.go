package system

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// netplan refuses to load a world-readable file, so 0644 everywhere is wrong.
func TestRestore_KeepsFileModes(t *testing.T) {
	p := tmpPaths(t)
	yaml := filepath.Join(p.NetplanDir, "50-cloud-init.yaml")
	if err := os.WriteFile(yaml, []byte("network: {version: 2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Snapshotter{Backend: NewFakeBackend(), Paths: p}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.NetplanModes["50-cloud-init.yaml"]; got != 0o600 {
		t.Fatalf("captured mode = %#o, want 0600", got)
	}

	// Someone loosens it, then we revert.
	if err := os.Chmod(yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	info, err := os.Stat(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("restored mode = %#o, want 0600", got)
	}
}

func TestUnitsToUnmask(t *testing.T) {
	nm := "NetworkManager.service"
	wait := "NetworkManager-wait-online.service"

	// The takeover masked both; neither was masked before.
	got := unitsToUnmask([]string{nm, wait}, nil)
	if !slices.Equal(got, []string{nm, wait}) {
		t.Errorf("unmask = %v, want both units back", got)
	}

	// Already masked by the operator: leave it alone.
	if got := unitsToUnmask([]string{nm}, []string{nm}); len(got) != 0 {
		t.Errorf("unmask = %v, want nothing — it was masked before the apply", got)
	}
}

// Restoring in listed order puts `default via <gw>` before the subnet its
// gateway lives on: EINVAL, and the revert leaves no default route at all.
func TestRestoreOrder_ConnectedRoutesBeforeGatewayRoutes(t *testing.T) {
	// The order `ip route show` prints, which is the order we snapshot.
	in := []Route{
		{Table: 201, Dest: "default", Gateway: "10.0.2.2", OifName: "enp1s0"},
		{Table: 201, Dest: "10.0.2.0/24", OifName: "enp1s0", Scope: "link", Metric: 100},
		{Table: 201, Dest: "10.0.2.2/32", OifName: "enp1s0", Scope: "link", Metric: 100},
	}
	got := restoreOrder(in)

	gwAt := slices.IndexFunc(got, func(r Route) bool { return r.Gateway != "" })
	for i, r := range got {
		if r.Gateway == "" && i > gwAt {
			t.Errorf("connected route %s restored after a gateway route", r.Dest)
		}
	}
	if len(got) != len(in) {
		t.Fatalf("restoreOrder dropped routes: %d in, %d out", len(in), len(got))
	}
}

// Routes with the same shape keep their snapshot order, so a restore is
// reproducible rather than shuffling on every run.
func TestRestoreOrder_IsStable(t *testing.T) {
	in := []Route{
		{Table: 201, Dest: "10.0.2.0/24", OifName: "enp1s0", Scope: "link"},
		{Table: 201, Dest: "10.0.3.0/24", OifName: "enp2s0", Scope: "link"},
	}
	got := restoreOrder(in)
	if got[0].Dest != "10.0.2.0/24" || got[1].Dest != "10.0.3.0/24" {
		t.Errorf("order changed: %+v", got)
	}
}

// With no table there are no ingress pins, so replies leave by the wrong uplink.
// Seen on the target: SSH stayed dead until the table came back.
func TestRestore_PutsTheOwnedTableBack(t *testing.T) {
	p := tmpPaths(t)
	fa := &nft.FakeApplier{}
	mgr := nft.NewManager(fa)
	s := &Snapshotter{Backend: NewFakeBackend(), Nft: mgr, Paths: p}

	// A router already carrying stage-1 state.
	if err := mgr.Update(context.Background(), func(rs *nft.Ruleset) {
		rs.Connmark = true
		rs.IngressPins = []nft.Pin{{IfName: "enp1s0", Index: 1}}
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// The apply adds LAN state, then fails and rolls back.
	if err := mgr.Update(context.Background(), func(rs *nft.Ruleset) {
		rs.FilterForward = &nft.FilterForward{BridgeName: "lan0", UplinkNames: []string{"enp1s0"}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}

	got := mgr.Snapshot()
	if !got.Connmark || len(got.IngressPins) != 1 {
		t.Errorf("the pins the reply path needs did not come back: %+v", got)
	}
	if got.FilterForward != nil {
		t.Errorf("the rolled-back LAN state survived: %+v", got.FilterForward)
	}
	if fa.Deletes != 0 {
		t.Error("the table was deleted instead of being restored")
	}
}

// Before the takeover there was no owned table, so a rollback must remove it
// rather than leave an empty one behind.
func TestRestore_EmptyNftStateStillTearsDown(t *testing.T) {
	p := tmpPaths(t)
	fa := &nft.FakeApplier{}
	mgr := nft.NewManager(fa)
	s := &Snapshotter{Backend: NewFakeBackend(), Nft: mgr, Paths: p}

	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(context.Background(), func(rs *nft.Ruleset) { rs.Connmark = true }); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if fa.Deletes != 1 {
		t.Errorf("deletes = %d, want the table removed", fa.Deletes)
	}
}

// A reload puts the files back but leaves the link running under whatever the
// removed file left it in — verified on the target, where lan0 stayed
// addressless until networkd restarted. So a restore that changed a .network
// file has to restart, not reload.
func TestRestore_RestartsNetworkdWhenAFileChanged(t *testing.T) {
	p := tmpPaths(t)
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "30-lan.network"),
		[]byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Snapshotter{Backend: NewFakeBackend(), Paths: p}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// The apply replaced it, exactly as a role change would.
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "30-lan.network"),
		[]byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var restarts int
	s.Restart = func(context.Context) error { restarts++; return nil }
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restarts != 1 {
		t.Errorf("networkd restarts = %d, want 1; a reload would leave the link wrong", restarts)
	}
}

// Restarting networkd drops every link briefly, so a restore that changed
// nothing must not do it.
func TestRestore_DoesNotRestartWhenNothingChanged(t *testing.T) {
	p := tmpPaths(t)
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "30-lan.network"),
		[]byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Snapshotter{Backend: NewFakeBackend(), Paths: p}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var restarts int
	s.Restart = func(context.Context) error { restarts++; return nil }
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restarts != 0 {
		t.Errorf("networkd restarts = %d, want 0 when the files already match", restarts)
	}
}

// The dead-man restores files, rules and nft, but the LAN lives in the
// database. Without this a reverted "disable the LAN" leaves the row disabled,
// so dnsmasq stays down and the next reconcile undoes the revert.
func TestRestore_PutsTheLANRowBack(t *testing.T) {
	p := tmpPaths(t)
	live := &domain.LANConfig{Enabled: true, CIDR: "10.77.0.1/24"}

	s := &Snapshotter{
		Backend:    NewFakeBackend(),
		Paths:      p,
		CaptureLAN: func(context.Context) (*domain.LANConfig, error) { return live, nil },
		RestoreLAN: func(_ context.Context, c *domain.LANConfig) error { live = c; return nil },
	}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.LANConfig == nil || !snap.LANConfig.Enabled {
		t.Fatalf("the LAN was not captured: %+v", snap.LANConfig)
	}

	live = &domain.LANConfig{Enabled: false, CIDR: "10.77.0.1/24"} // the apply disabled it
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !live.Enabled {
		t.Error("the LAN row stayed disabled after a revert")
	}
}

// A snapshot from a build that never recorded the LAN must still restore.
func TestRestore_NoLANInSnapshotIsNotAnError(t *testing.T) {
	s := &Snapshotter{Backend: NewFakeBackend(), Paths: tmpPaths(t)}
	if err := s.Restore(context.Background(), &Snapshot{Version: 1}); err != nil {
		t.Errorf("restore without a captured LAN: %v", err)
	}
}
