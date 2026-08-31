package domain

import (
	"strings"
	"testing"
)

func wifiRows() []NetworkInterface {
	return []NetworkInterface{
		{ID: 1, Key: "k1", IfName: "enp1s0", Source: "eth_onboard", Present: true,
			Role: RoleWAN, Slot: SlotDomestic},
		{ID: 2, Key: "k2", IfName: "wlp3s0", Source: "wifi_pci", Present: true,
			Role: RoleUnassigned, PhyName: "phy0"},
	}
}

func wifiInput(req ChangeRequest) ValidationInput {
	return ValidationInput{
		Rows: wifiRows(), Req: req, MgmtCIDR: "192.168.99.1/24",
		HostapdInstalled: true, IWDInstalled: true,
		RadioSupportsAP:  map[string]bool{"phy0": true},
		RadioSupportsSTA: map[string]bool{"phy0": true},
		CountryCode:      "IR",
	}
}

// hostapd, AP support and a country code. All three, and the message says which
func TestValidate_V11_APGate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ValidationInput)
		wantSub string
	}{
		{"hostapd absent", func(i *ValidationInput) { i.HostapdInstalled = false }, "hostapd"},
		{"radio cannot be an AP", func(i *ValidationInput) {
			i.RadioSupportsAP = map[string]bool{"phy0": false}
		}, "access point"},
		{"unknown radio is unsupported", func(i *ValidationInput) { i.RadioSupportsAP = nil }, "access point"},
		{"no country code", func(i *ValidationInput) { i.CountryCode = "" }, "country"},
		{"world regdomain", func(i *ValidationInput) { i.CountryCode = "00" }, "country"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := wifiInput(ChangeRequest{InterfaceID: 2, Role: RoleLAN})
			c.mutate(&in)
			r := firstReject(Validate(in))
			if r == nil {
				t.Fatal("accepted")
			}
			if r.Rule != "V11" {
				t.Fatalf("first reject = %s (%s), want V11", r.Rule, r.Message)
			}
			if !strings.Contains(strings.ToLower(r.Message), c.wantSub) {
				t.Errorf("message %q does not mention %q", r.Message, c.wantSub)
			}
		})
	}
}

// A station needs iwd and STA support, because networkd cannot associate
func TestValidate_V12_StationGate(t *testing.T) {
	for name, mutate := range map[string]func(*ValidationInput){
		"iwd absent":     func(i *ValidationInput) { i.IWDInstalled = false },
		"no STA support": func(i *ValidationInput) { i.RadioSupportsSTA = map[string]bool{"phy0": false} },
		"unknown radio":  func(i *ValidationInput) { i.RadioSupportsSTA = nil },
	} {
		t.Run(name, func(t *testing.T) {
			in := wifiInput(ChangeRequest{InterfaceID: 2, Role: RoleWAN, Slot: SlotSecondary2})
			mutate(&in)
			r := firstReject(Validate(in))
			if r == nil || r.Rule != "V12" {
				t.Fatalf("got %+v, want V12", r)
			}
		})
	}
}

// An ethernet port must not be touched by the wifi gates
func TestValidate_V11V12_IgnoreWiredPorts(t *testing.T) {
	in := wifiInput(ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: SlotDomestic})
	in.HostapdInstalled, in.IWDInstalled = false, false
	in.RadioSupportsAP, in.RadioSupportsSTA = nil, nil
	in.CountryCode = ""
	for _, v := range Validate(in) {
		if v.Rule == "V11" || v.Rule == "V12" {
			t.Fatalf("a wired port hit %s: %s", v.Rule, v.Message)
		}
	}
}

// V13 unchanged, but must keep firing with the new inputs present
func TestValidate_V13_StillFiresWithCapabilityInputs(t *testing.T) {
	in := wifiInput(ChangeRequest{InterfaceID: 3, Role: RoleLAN})
	in.Rows = append(in.Rows, NetworkInterface{
		ID: 3, Key: "k3", IfName: "wlp3s0-1", Source: "wifi_pci",
		Present: true, Role: RoleUnassigned, PhyName: "phy0",
	})
	in.Rows[1].Role, in.Rows[1].Slot = RoleWAN, SlotSecondary2

	r := firstReject(Validate(in))
	if r == nil || r.Rule != "V13" {
		t.Fatalf("AP+STA on one radio was accepted: %+v", r)
	}
	if !strings.Contains(r.Message, "phy0") {
		t.Errorf("message must name the radio: %q", r.Message)
	}
}

// A random MAC on an AP invalidates every client's saved network
func TestValidate_V24_MACPolicy(t *testing.T) {
	for _, role := range []InterfaceRole{RoleLAN, RoleLANMember, RoleWAN} {
		in := wifiInput(ChangeRequest{InterfaceID: 2, Role: role, Slot: SlotSecondary2})
		in.Req.MACPolicy = "random"
		if role == RoleLANMember {
			// V10 runs first: a member needs a bridge to join
			in.Rows = append(in.Rows, NetworkInterface{ID: 9, Key: "k9", IfName: "lan0",
				Source: "virt_bridge", Present: true, Role: RoleLAN})
			master := uint(9)
			in.Req.MasterID = &master
		}
		r := firstReject(Validate(in))
		if r == nil || r.Rule != "V24" {
			t.Fatalf("role %s: got %+v, want V24", role, r)
		}
	}
}

func TestValidate_V24_AllowsAStablePolicy(t *testing.T) {
	in := wifiInput(ChangeRequest{InterfaceID: 2, Role: RoleLAN})
	in.Req.MACPolicy = "none"
	if Rejected(Validate(in)) {
		t.Error("MACPolicy=none was rejected")
	}
}

func TestValidate_WifiHappyPaths(t *testing.T) {
	if vs := Validate(wifiInput(ChangeRequest{InterfaceID: 2, Role: RoleLAN})); Rejected(vs) {
		t.Fatalf("a fully capable AP assignment was rejected: %+v", vs)
	}
	sta := wifiInput(ChangeRequest{InterfaceID: 2, Role: RoleWAN, Slot: SlotSecondary2})
	if vs := Validate(sta); Rejected(vs) {
		t.Fatalf("a fully capable station assignment was rejected: %+v", vs)
	}
}

func TestValidateWifiConfig(t *testing.T) {
	good := WifiConfig{Mode: "ap", SSID: "nasnet", PSK: "hunter2hunter2",
		CountryCode: "IR", Band: "2g"}
	if vs := ValidateWifiConfig(good); Rejected(vs) {
		t.Fatalf("a valid config was rejected: %+v", vs)
	}

	cases := []struct {
		name   string
		mutate func(*WifiConfig)
		rule   string
	}{
		{"empty SSID", func(c *WifiConfig) { c.SSID = "" }, "V38"},
		{"SSID over 32 bytes", func(c *WifiConfig) { c.SSID = strings.Repeat("x", 33) }, "V38"},
		// 32 is the limit on the encoded SSID, so multibyte counts
		{"SSID over 32 bytes in UTF-8", func(c *WifiConfig) { c.SSID = strings.Repeat("é", 17) }, "V38"},
		{"PSK too short", func(c *WifiConfig) { c.PSK = "short" }, "V39"},
		{"PSK too long", func(c *WifiConfig) { c.PSK = strings.Repeat("x", 63) + "y" }, "V39"},
		{"64 hex is a raw key", func(c *WifiConfig) { c.PSK = strings.Repeat("ab", 32) }, ""},
		{"PSK with control chars", func(c *WifiConfig) { c.PSK = "hunter2\x07hunter2" }, "V39"},
		{"unknown band", func(c *WifiConfig) { c.Band = "7g" }, "V40"},
		{"unknown mode", func(c *WifiConfig) { c.Mode = "repeater" }, "V41"},
		{"AP without a country code", func(c *WifiConfig) { c.CountryCode = "" }, "V42"},
		{"AP under the world regdomain", func(c *WifiConfig) { c.CountryCode = "00" }, "V42"},
		// A station joining an open network has no PSK, and no country of its own
		{"station with no PSK", func(c *WifiConfig) { c.Mode = "station"; c.PSK = "" }, ""},
		{"station with no country", func(c *WifiConfig) { c.Mode = "station"; c.CountryCode = "" }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.mutate(&c)
			r := firstReject(ValidateWifiConfig(c))
			if tc.rule == "" {
				if r != nil {
					t.Fatalf("rejected: %+v", r)
				}
				return
			}
			if r == nil || r.Rule != tc.rule {
				t.Fatalf("got %+v, want %s", r, tc.rule)
			}
		})
	}
}
