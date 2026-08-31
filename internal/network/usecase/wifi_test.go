package usecase

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

// hostapd's bridge= line enslaves the AP port. A Bridge= .network unit for the
// same port would fight it.
func TestWantsLANMemberUnit_SkipsRadios(t *testing.T) {
	eth := domain.NetworkInterface{IfName: "enp2s0", Source: "eth_onboard",
		Present: true, Role: domain.RoleLANMember}
	if !wantsLANMemberUnit(eth) {
		t.Error("an ethernet member lost its .network unit")
	}
	for _, src := range []string{"wifi_pci", "wifi_usb"} {
		radio := domain.NetworkInterface{IfName: "wlp3s0", Source: src,
			Present: true, Role: domain.RoleLAN, PhyName: "phy0"}
		if wantsLANMemberUnit(radio) {
			t.Errorf("a %s AP radio got a Bridge= unit; hostapd enslaves it", src)
		}
	}
}

func TestRadioView_CarriesTheCapabilityAndTheCountryState(t *testing.T) {
	v := RadioView{
		Phy: "phy0", IfName: "wlp3s0", Role: "unassigned",
		SupportsAP: true, SupportsSTA: true,
		CountryCode: "", CountryCodeSet: false,
		Bands: map[system.Band][]system.Channel{
			system.Band2G: {{Number: 6, FreqMHz: 2437}},
			system.Band5G: {{Number: 36, FreqMHz: 5180, NoIR: true}},
		},
	}
	if v.CountryCodeSet {
		t.Error("an empty country code must not report as set")
	}
	if !v.SupportsAP || !v.SupportsSTA {
		t.Error("capability lost")
	}
}

func TestRadios_ReportsTheSiblingRole(t *testing.T) {
	v := RadioView{Phy: "phy0", SiblingRole: "wan"}
	if v.SiblingRole != "wan" {
		t.Fatal("SiblingRole lost")
	}
}

func TestDescribeWifiRoleError(t *testing.T) {
	if got := describeWifiRoleError("wlp3s0", "lan"); !strings.Contains(got, "access point") {
		t.Errorf("error text = %q; it must explain the radio is an AP", got)
	}
	if got := describeWifiRoleError("wlp3s0", "lan_member"); !strings.Contains(got, "access point") {
		t.Errorf("error text = %q", got)
	}
	if got := describeWifiRoleError("wlp3s0", "wan"); !strings.Contains(got, "station") {
		t.Errorf("error text = %q; it must explain the radio is a station", got)
	}
	if got := describeWifiRoleError("wlp3s0", "unassigned"); got == "" {
		t.Error("an unassigned radio needs an explanation too")
	}
}

// hostapd config for the AP row, from the stored intent
func TestHostapdConfigFor(t *testing.T) {
	row := domain.NetworkInterface{ID: 2, IfName: "wlp3s0", PhyName: "phy0"}
	cfg := domain.WifiConfig{InterfaceID: 2, Mode: "ap", SSID: "nasnet",
		PSK: "hunter2hunter2", CountryCode: "IR", Band: "5g", Channel: 36, Hidden: true}

	got := hostapdConfigFor(row, cfg, "lan0")
	if got.IfName != "wlp3s0" || got.BridgeName != "lan0" {
		t.Errorf("wrong device or bridge: %+v", got)
	}
	if got.Band != system.Band5G || got.Channel != 36 {
		t.Errorf("band/channel lost: %+v", got)
	}
	if got.SSID != "nasnet" || got.PSK != "hunter2hunter2" || !got.Hidden {
		t.Errorf("intent lost: %+v", got)
	}
	// ax is asked for; the binary probe decides whether it survives
	if !got.EnableAX {
		t.Error("EnableAX should be requested and settled by the binary probe")
	}
}

// Only an enabled AP row on an AP-role radio with the LAN up gets served
func TestWifiIntentFor(t *testing.T) {
	lanOn := &domain.LANConfig{BridgeName: "lan0", Enabled: true}
	apRow := domain.NetworkInterface{ID: 2, IfName: "wlp3s0", Source: "wifi_pci",
		Present: true, Role: domain.RoleLAN, PhyName: "phy0"}
	apCfg := &domain.WifiConfig{InterfaceID: 2, Mode: "ap", SSID: "x", Enabled: true}

	if !wantsAP(apRow, apCfg, lanOn) {
		t.Error("an enabled AP row was not served")
	}
	if wantsAP(apRow, apCfg, &domain.LANConfig{Enabled: false}) {
		t.Error("an AP was served with the LAN off; it has nothing to bridge into")
	}
	if wantsAP(apRow, apCfg, nil) {
		t.Error("an AP was served with no LAN row at all")
	}
	off := *apCfg
	off.Enabled = false
	if wantsAP(apRow, &off, lanOn) {
		t.Error("a disabled row was served")
	}
	if wantsAP(apRow, nil, lanOn) {
		t.Error("a radio with no stored intent was served")
	}
	staRow := apRow
	staRow.Role = domain.RoleWAN
	if wantsAP(staRow, apCfg, lanOn) {
		t.Error("a station radio was served an AP config")
	}
	wired := apRow
	wired.Source = "eth_onboard"
	if wantsAP(wired, apCfg, lanOn) {
		t.Error("an ethernet port was served an AP config")
	}

	staCfg := &domain.WifiConfig{InterfaceID: 2, Mode: "station", SSID: "up", Enabled: true}
	if !wantsStation(staRow, staCfg) {
		t.Error("an enabled station row was not recognised")
	}
	if wantsStation(apRow, staCfg) {
		t.Error("an AP-role radio was recognised as a station")
	}
	if wantsStation(staRow, apCfg) {
		t.Error("an ap-mode row was recognised as a station")
	}
}
