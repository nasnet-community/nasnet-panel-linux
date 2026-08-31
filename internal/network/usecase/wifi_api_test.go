package usecase

import (
	"context"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

// memWifiRepo is the WifiRepository, in a map
type memWifiRepo struct {
	rows   []domain.WifiConfig
	nextID uint
}

func (m *memWifiRepo) List(context.Context) ([]domain.WifiConfig, error) {
	return append([]domain.WifiConfig(nil), m.rows...), nil
}

func (m *memWifiRepo) GetByInterface(_ context.Context, id uint) (*domain.WifiConfig, error) {
	for i := range m.rows {
		if m.rows[i].InterfaceID == id {
			c := m.rows[i]
			return &c, nil
		}
	}
	return &domain.WifiConfig{InterfaceID: id, Mode: "ap", Band: "2g"}, nil
}

func (m *memWifiRepo) Save(_ context.Context, cfg *domain.WifiConfig) error {
	for i := range m.rows {
		if m.rows[i].InterfaceID == cfg.InterfaceID {
			m.rows[i] = *cfg
			return nil
		}
	}
	m.nextID++
	cfg.ID = m.nextID
	m.rows = append(m.rows, *cfg)
	return nil
}

func (m *memWifiRepo) ReplaceAll(_ context.Context, cfgs []domain.WifiConfig) error {
	m.rows = append([]domain.WifiConfig(nil), cfgs...)
	return nil
}

func wifiUsecase(t *testing.T, rows []domain.NetworkInterface) *networkUsecase {
	t.Helper()
	applier, _, p := newApplier(t)
	u := &networkUsecase{Deps: Deps{
		IfRepo:   &stubIfRepo{rows: rows},
		WifiRepo: &memWifiRepo{},
		Station:  system.NewFakeStationClient(),
		RadioProber: &system.FakeRadioProber{Caps: []system.RadioCaps{
			{Phy: "phy0", SupportsAP: true, SupportsSTA: true,
				Bands: map[system.Band][]system.Channel{
					system.Band2G: {{Number: 6, FreqMHz: 2437}},
				}},
		}},
		Paths: p,
	}}
	u.applier = applier
	u.hostapd = system.NewHostapd(p)
	return u
}

func apRadioRow() domain.NetworkInterface {
	return domain.NetworkInterface{ID: 2, Key: "k2", IfName: "wlp3s0", Source: "wifi_pci",
		Present: true, Role: domain.RoleLAN, PhyName: "phy0"}
}

func staRadioRow() domain.NetworkInterface {
	return domain.NetworkInterface{ID: 2, Key: "k2", IfName: "wlp3s0", Source: "wifi_pci",
		Present: true, Role: domain.RoleWAN, Slot: domain.SlotSecondary2, PhyName: "phy0"}
}

// The message explains; it does not just 500
func TestEnableAP_RefusesAStationRadio(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{staRadioRow()})
	_, _, err := u.EnableAP(context.Background(), domain.WifiConfig{
		InterfaceID: 2, SSID: "nasnet", PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"})
	if err == nil || !strings.Contains(err.Error(), "station") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnableAP_RefusesAWiredPort(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{{ID: 2, Key: "k2", IfName: "enp2s0",
		Source: "eth_onboard", Present: true, Role: domain.RoleLANMember}})
	_, _, err := u.EnableAP(context.Background(), domain.WifiConfig{
		InterfaceID: 2, SSID: "nasnet", PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"})
	if err == nil || !strings.Contains(err.Error(), "not a radio") {
		t.Fatalf("err = %v", err)
	}
}

// A bad form comes back as verdicts, not an error, so the UI can render them
func TestEnableAP_ReturnsFormVerdicts(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{apRadioRow()})
	vs, view, err := u.EnableAP(context.Background(), domain.WifiConfig{
		InterfaceID: 2, SSID: "", PSK: "short", Band: "2g"})
	if err != nil {
		t.Fatal(err)
	}
	if view != nil || !domain.Rejected(vs) {
		t.Fatalf("vs=%+v view=%+v", vs, view)
	}
}

// One hostapd config file means one AP, refused loudly where the operator is
func TestEnableAP_RefusesASecondAP(t *testing.T) {
	rows := []domain.NetworkInterface{apRadioRow(), {ID: 3, Key: "k3", IfName: "wlx0",
		Source: "wifi_usb", Present: true, Role: domain.RoleLANMember, PhyName: "phy1"}}
	u := wifiUsecase(t, rows)
	repo := u.WifiRepo.(*memWifiRepo)
	repo.rows = []domain.WifiConfig{{ID: 1, InterfaceID: 2, Mode: "ap", SSID: "one",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g", Enabled: true}}

	_, _, err := u.EnableAP(context.Background(), domain.WifiConfig{
		InterfaceID: 3, SSID: "two", PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g"})
	if err == nil || !strings.Contains(err.Error(), "one access point") {
		t.Fatalf("err = %v", err)
	}
}

// Re-enabling the same radio is an edit, not a second AP
func TestOtherEnabledAP_IgnoresTheSameInterface(t *testing.T) {
	cfgs := []domain.WifiConfig{{InterfaceID: 2, Mode: "ap", Enabled: true}}
	if _, found := otherEnabledAP(cfgs, 2); found {
		t.Error("the row being edited counted as another AP")
	}
	if _, found := otherEnabledAP(cfgs, 3); !found {
		t.Error("a different interface's AP was missed")
	}
	// A station row is not an AP
	if _, found := otherEnabledAP([]domain.WifiConfig{{InterfaceID: 2, Mode: "station", Enabled: true}}, 3); found {
		t.Error("a station row counted as an AP")
	}
}

func TestScanWifi_RefusesAnAPRadio(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{apRadioRow()})
	if _, err := u.ScanWifi(context.Background(), "k2"); err == nil ||
		!strings.Contains(err.Error(), "access point") {
		t.Fatalf("err = %v", err)
	}
}

func TestScanWifi_UnknownKeyIsAnError(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{staRadioRow()})
	if _, err := u.ScanWifi(context.Background(), "nope"); err == nil {
		t.Error("an unknown key was accepted")
	}
}

// iwd fails association asynchronously with no error back, so validate first
func TestConnectWifi_RefusesBadCredentials(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{staRadioRow()})
	if _, err := u.ConnectWifi(context.Background(), "k2", "", "hunter2hunter2"); err == nil {
		t.Error("an empty SSID was accepted")
	}
	if _, err := u.ConnectWifi(context.Background(), "k2", "home", "short"); err == nil {
		t.Error("a 5-char PSK was accepted")
	}
}

func TestConnectWifi_RefusesAnAPRadio(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{apRadioRow()})
	if _, err := u.ConnectWifi(context.Background(), "k2", "home", "hunter2hunter2"); err == nil ||
		!strings.Contains(err.Error(), "access point") {
		t.Errorf("err = %v", err)
	}
}

// With no storage or no platform support, say so instead of panicking
func TestWifiMethods_RefuseWithoutDeps(t *testing.T) {
	u := &networkUsecase{Deps: Deps{IfRepo: &stubIfRepo{}}}
	if _, _, err := u.EnableAP(context.Background(), domain.WifiConfig{}); err == nil {
		t.Error("EnableAP ran with no storage")
	}
	if _, err := u.DisableWifi(context.Background(), "k2"); err == nil {
		t.Error("DisableWifi ran with no storage")
	}
	if _, err := u.ScanWifi(context.Background(), "k2"); err == nil {
		t.Error("ScanWifi ran with no station client")
	}
	if _, err := u.ConnectWifi(context.Background(), "k2", "x", ""); err == nil {
		t.Error("ConnectWifi ran with no station client")
	}
	// And Radios reports an empty list rather than failing the page
	if got, err := u.Radios(context.Background()); err != nil || len(got) != 0 {
		t.Errorf("Radios = %+v, %v", got, err)
	}
}

// The stored PSK survives an edit that does not resend it
func TestEnableAP_EmptyPSKKeepsTheStoredOne(t *testing.T) {
	u := wifiUsecase(t, []domain.NetworkInterface{apRadioRow()})
	repo := u.WifiRepo.(*memWifiRepo)
	repo.rows = []domain.WifiConfig{{ID: 1, InterfaceID: 2, Mode: "ap", SSID: "old",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "2g", Enabled: true}}

	// No applier here, so the apply fails after validation. The verdicts are
	// what this checks: an empty PSK must not read as a short one.
	vs, _, _ := u.EnableAP(context.Background(), domain.WifiConfig{
		InterfaceID: 2, SSID: "renamed", PSK: "", CountryCode: "IR", Band: "2g"})
	for _, v := range vs {
		if v.Rule == "V39" {
			t.Fatalf("an empty PSK was validated as the passphrase: %s", v.Message)
		}
	}
}
