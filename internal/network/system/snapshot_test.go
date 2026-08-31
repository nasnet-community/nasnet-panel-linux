package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func tmpPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	p := Paths{
		NetworkdDir:        filepath.Join(root, "etc/systemd/network"),
		NetworkdConfDir:    filepath.Join(root, "etc/systemd/networkd.conf.d"),
		NetplanDir:         filepath.Join(root, "etc/netplan"),
		NetplanDisabledDir: filepath.Join(root, "etc/netplan.disabled"),
		SysctlDir:          filepath.Join(root, "etc/sysctl.d"),
		RTTablesDir:        filepath.Join(root, "etc/iproute2/rt_tables.d"),
		CloudInitDir:       filepath.Join(root, "etc/cloud/cloud.cfg.d"),
		RunNetworkdDir:     filepath.Join(root, "run/systemd/network"),
		StateDir:           filepath.Join(root, "var/lib/nasnet"),
		HostapdDir:         filepath.Join(root, "etc/hostapd"),
	}
	for _, d := range []string{p.NetworkdDir, p.NetworkdConfDir, p.NetplanDir, p.SysctlDir,
		p.RTTablesDir, p.CloudInitDir, p.RunNetworkdDir, p.StateDir, p.HostapdDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func newSnapshotter(t *testing.T) (*Snapshotter, *FakeBackend, Paths) {
	t.Helper()
	p := tmpPaths(t)
	be := NewFakeBackend()
	return &Snapshotter{Backend: be, Nft: nft.NewManager(&nft.FakeApplier{}), Paths: p}, be, p
}

// The takeover is an apply, so its snapshot must include the netplan YAML and
// the masked-unit state, not just kernel state.
func TestCapture_IncludesNetplanAndUnitState(t *testing.T) {
	s, be, p := newSnapshotter(t)
	if err := os.WriteFile(filepath.Join(p.NetplanDir, "50-cloud-init.yaml"),
		[]byte("network:\n  version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "10-nasnet-wan1.network"),
		[]byte("[Match]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	be.Rules = []Rule{{Pref: 110, FwMark: netmark.GroupMark(1), FwMask: netmark.MaskGroup, Table: 201}}
	be.Routes = []Route{{Table: 201, Dest: "default", Gateway: "192.168.1.1", OifName: "enp1s0"}}
	be.AddrList = []Addr{{IfName: "enp1s0", CIDR: "192.168.1.34/24"}}

	snap, err := s.Capture(context.Background(), []int{201, 202})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.NetplanFiles) != 1 {
		t.Errorf("netplan files = %d, want 1", len(snap.NetplanFiles))
	}
	if len(snap.NetworkdFiles) != 1 {
		t.Errorf("networkd files = %d, want 1", len(snap.NetworkdFiles))
	}
	if len(snap.Rules) != 1 || len(snap.Routes[201]) != 1 || len(snap.Addrs) != 1 {
		t.Errorf("kernel state not captured: %+v", snap)
	}
	if snap.Version == 0 || snap.TakenAt.IsZero() {
		t.Error("snapshot metadata missing")
	}
}

func TestSaveAndLoad_RoundTrips(t *testing.T) {
	s, _, p := newSnapshotter(t)
	if err := os.WriteFile(filepath.Join(p.NetplanDir, "a.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Capture(context.Background(), []int{201})
	if err != nil {
		t.Fatal(err)
	}
	snap.MaskedUnits = []string{"NetworkManager.service"}

	path := filepath.Join(p.StateDir, "snap-7.json")
	if err := s.Save(snap, path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back.NetplanFiles["a.yaml"]) != "x: 1\n" {
		t.Errorf("netplan content lost: %q", back.NetplanFiles["a.yaml"])
	}
	if len(back.MaskedUnits) != 1 {
		t.Error("masked-unit state lost")
	}
}

// Restore must be the exact inverse: files nasnet added after the snapshot are
// removed, files present in the snapshot are recreated byte-for-byte.
func TestRestore_RemovesFilesAddedAfterTheSnapshot(t *testing.T) {
	s, _, p := newSnapshotter(t)
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "10-nasnet-old.network"),
		[]byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Capture(context.Background(), []int{201})
	if err != nil {
		t.Fatal(err)
	}

	// A bad apply writes a new file and mangles the old one.
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "10-nasnet-new.network"),
		[]byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.NetworkdDir, "10-nasnet-old.network"),
		[]byte("mangled\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.NetworkdDir, "10-nasnet-new.network")); !os.IsNotExist(err) {
		t.Error("restore left a file the snapshot did not contain")
	}
	got, err := os.ReadFile(filepath.Join(p.NetworkdDir, "10-nasnet-old.network"))
	if err != nil || string(got) != "old\n" {
		t.Errorf("restore did not put the original content back: %q %v", got, err)
	}
}

func TestRestore_PutsRulesAndRoutesBack(t *testing.T) {
	s, be, _ := newSnapshotter(t)
	be.Rules = []Rule{{Pref: 30, SuppressSet: true, Table: 254}}
	be.Routes = []Route{{Table: 201, Dest: "default", Gateway: "192.168.1.1"}}
	snap, err := s.Capture(context.Background(), []int{201})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a wrong apply.
	be.Rules = []Rule{{Pref: 999, Table: 202}}
	be.Routes = nil

	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	rules, _ := be.RuleList(context.Background())
	if len(rules) != 1 || rules[0].Pref != 30 {
		t.Errorf("rules after restore = %+v", rules)
	}
	routes, _ := be.RouteList(context.Background(), 201)
	if len(routes) != 1 || routes[0].Gateway != "192.168.1.1" {
		t.Errorf("routes after restore = %+v", routes)
	}
}

func TestLoadSnapshot_MissingFileIsAnError(t *testing.T) {
	if _, err := LoadSnapshot(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("LoadSnapshot accepted a missing path")
	}
}

// The cloud-init drop-in has to come from Paths. Hardcoding /etc/cloud would
// make `go test` as root delete a real file off the dev box.
func TestRestore_CloudInitMarkerIsPathInjected(t *testing.T) {
	s, _, p := newSnapshotter(t)
	marker := filepath.Join(p.CloudInitDir, cloudInitDropIn)

	// Captured before nasnet disabled cloud-init networking.
	snap, err := s.Capture(context.Background(), []int{201})
	if err != nil {
		t.Fatal(err)
	}
	if snap.CloudInitDisabled {
		t.Fatal("CloudInitDisabled true with no marker present")
	}

	// An apply drops it in; restore must take it back out.
	if err := os.WriteFile(marker, []byte("network: {config: disabled}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("restore left the cloud-init drop-in behind")
	}

	// If it was already disabled before the snapshot, restore must keep it.
	if err := os.WriteFile(marker, []byte("network: {config: disabled}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap2, err := s.Capture(context.Background(), []int{201})
	if err != nil {
		t.Fatal(err)
	}
	if !snap2.CloudInitDisabled {
		t.Fatal("marker present but CloudInitDisabled false")
	}
	if err := s.Restore(context.Background(), snap2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("restore removed a drop-in that predated the snapshot")
	}
}

func TestUnitMasked_CountsRuntimeMasks(t *testing.T) {
	for _, s := range []string{"masked", "masked-runtime", " masked\n"} {
		if !unitIsMasked(s) {
			t.Errorf("%q should count as masked", s)
		}
	}
	// An absent unit was never masked, so restore must not try to re-mask it.
	for _, s := range []string{"enabled", "disabled", "not-found", ""} {
		if unitIsMasked(s) {
			t.Errorf("%q must not count as masked", s)
		}
	}
}

// The AP config holds a passphrase, so it must ride the snapshot's side channel
// and never the model's own JSON
func TestSaveAndLoad_CarriesWifiIntentAndPSK(t *testing.T) {
	s, _, p := newSnapshotter(t)
	s.CaptureWifi = func(context.Context) ([]domain.WifiConfig, error) {
		return []domain.WifiConfig{{InterfaceID: 7, Mode: "ap", SSID: "nasnet",
			PSK: "sekritsekrit", CountryCode: "IR", Band: "2g", Enabled: true}}, nil
	}
	if err := os.WriteFile(filepath.Join(p.HostapdDir, "nasnet-ap.conf"),
		[]byte("interface=wlan0\nwpa_passphrase=sekritsekrit\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := s.Capture(context.Background(), []int{201})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.WifiCaptured || len(snap.WifiConfigs) != 1 {
		t.Fatalf("wifi rows not captured: %+v", snap.WifiConfigs)
	}
	if snap.WifiPSKs[0] != "sekritsekrit" {
		t.Errorf("PSK side channel = %q", snap.WifiPSKs[0])
	}
	if snap.HostapdModes["nasnet-ap.conf"] != 0o600 {
		t.Errorf("conf mode not captured: %o", snap.HostapdModes["nasnet-ap.conf"])
	}

	path := filepath.Join(p.StateDir, "snap-9.json")
	if err := s.Save(snap, path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.WifiConfigs) != 1 || back.WifiConfigs[0].PSK != "sekritsekrit" {
		t.Fatalf("PSK lost across the round trip: %+v", back.WifiConfigs)
	}
	if back.WifiConfigs[0].SSID != "nasnet" || !back.WifiConfigs[0].Enabled {
		t.Errorf("intent lost: %+v", back.WifiConfigs[0])
	}

	// json:"-" on the model is what keeps the secret out of wifi_configs
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	configs := strings.Split(strings.Split(string(blob), `"wifi_psks"`)[0], `"wifi_configs"`)
	if len(configs) > 1 && strings.Contains(configs[1], "sekritsekrit") {
		t.Error("the PSK leaked into wifi_configs; json:\"-\" on the model is gone")
	}
}

// A rolled-back AP enable must not leave the row saying "on"
func TestRestore_PutsTheWifiRowsBack(t *testing.T) {
	s, _, _ := newSnapshotter(t)
	var restored []domain.WifiConfig
	s.RestoreWifi = func(_ context.Context, cfgs []domain.WifiConfig) error {
		restored = cfgs
		return nil
	}
	snap := &Snapshot{Version: snapshotVersion, Routes: map[int][]Route{},
		WifiCaptured: true, WifiConfigs: []domain.WifiConfig{{InterfaceID: 7, SSID: "before"}}}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].SSID != "before" {
		t.Fatalf("restored = %+v", restored)
	}
}

// An older snapshot has no wifi at all, and empty must not read as "clear it"
func TestRestore_OlderSnapshotLeavesWifiAlone(t *testing.T) {
	s, _, _ := newSnapshotter(t)
	called := false
	s.RestoreWifi = func(context.Context, []domain.WifiConfig) error {
		called = true
		return nil
	}
	snap := &Snapshot{Version: snapshotVersion, Routes: map[int][]Route{}}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("a snapshot with no wifi section triggered a restore")
	}
}
