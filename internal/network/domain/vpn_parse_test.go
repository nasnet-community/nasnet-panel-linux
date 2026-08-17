package domain

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// A key straight out of wgtypes: 32 bytes, standard base64, so it carries the
// characters that break naive URI parsing.
const (
	testPriv = "iNb2CSuC4vfa1UAvOoNGeI9DoLR1s1zCVEfmzHnAHFE="
	testPub  = "Ntq1x3JYRTMHTIfNMpkKCPMBHfJhFtjM2sM82nz0ZW4="
	testPSK  = "0X4rGrHtIEUFBoZxLm8ZDXlDCwQoi4EQYwQZ7Jj4gVo="
)

func TestDetectWGInput_TellsTheTwoFormatsApart(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"wireguard://key@host:51820", "uri"},
		{"  WIREGUARD://key@host:51820  ", "uri"},
		{"[Interface]\nPrivateKey = x\n", "conf"},
		{"\n\n  [interface]\n", "conf"},
		{"", ""},
		{"hello", ""},
		{"vless://something", ""},
	} {
		if got := DetectWGInput(tc.in); got != tc.want {
			t.Errorf("DetectWGInput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseWireGuardURI_ReadsTheV2rayNForm(t *testing.T) {
	uri := "wireguard://" + urlenc(testPriv) + "@vpn.example.com:51820" +
		"?publickey=" + urlenc(testPub) +
		"&presharedkey=" + urlenc(testPSK) +
		"&address=10.66.0.2/32&mtu=1380&keepalive=15#Frankfurt"

	cfg, err := ParseWireGuardURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != testPriv {
		t.Errorf("private key = %q", cfg.PrivateKey)
	}
	if cfg.Peer.PublicKey != testPub || cfg.Peer.PresharedKey != testPSK {
		t.Errorf("peer keys = %q / %q", cfg.Peer.PublicKey, cfg.Peer.PresharedKey)
	}
	if cfg.Peer.Endpoint != "vpn.example.com:51820" {
		t.Errorf("endpoint = %q", cfg.Peer.Endpoint)
	}
	if cfg.Address != "10.66.0.2/32" {
		t.Errorf("address = %q", cfg.Address)
	}
	if cfg.MTU != 1380 || cfg.Peer.PersistentKeepalive != 15 {
		t.Errorf("mtu = %d keepalive = %d", cfg.MTU, cfg.Peer.PersistentKeepalive)
	}
	if cfg.SuggestedName != "Frankfurt" {
		t.Errorf("name = %q", cfg.SuggestedName)
	}
	// Nothing said AllowedIPs, so the tunnel takes everything.
	if len(cfg.Peer.AllowedIPs) != 1 || cfg.Peer.AllowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("allowed ips = %v", cfg.Peer.AllowedIPs)
	}
}

// An unencoded "/" in the key used to read as "no private key in the URI".
func TestParseWireGuardURI_UnencodedKeyWithASlash(t *testing.T) {
	const slashKey = "bmFzbmV0Zml4dHVyZWtleQAAAAAAAAAAAAAAAAAA+/A="
	uri := "wireguard://" + slashKey + "@1.2.3.4:51820" +
		"?publickey=" + urlenc(testPub) + "&address=10.66.0.2/32#Frankfurt"

	cfg, err := ParseWireGuardURI(uri)
	if err != nil {
		t.Fatalf("a link with an unencoded key was rejected: %v", err)
	}
	if cfg.PrivateKey != slashKey {
		t.Errorf("private key = %q, want the one in the link", cfg.PrivateKey)
	}
	if cfg.Peer.Endpoint != "1.2.3.4:51820" {
		t.Errorf("endpoint = %q", cfg.Peer.Endpoint)
	}
	if cfg.SuggestedName != "Frankfurt" {
		t.Errorf("name = %q", cfg.SuggestedName)
	}
}

// A WARP config handshakes forever with the kernel driver. Say so instead.
func TestParseWireGuardURI_RejectsReserved(t *testing.T) {
	uri := "wireguard://" + urlenc(testPriv) + "@1.2.3.4:51820" +
		"?publickey=" + urlenc(testPub) + "&address=10.66.0.2/32&reserved=1,2,3"
	_, err := ParseWireGuardURI(uri)
	if !errors.Is(err, ErrReservedParam) {
		t.Fatalf("err = %v, want ErrReservedParam", err)
	}
	if !strings.Contains(err.Error(), "userspace") {
		t.Errorf("message does not say what to do instead: %v", err)
	}
}

func TestParseWireGuardURI_UnknownParamIsANoticeNotAnError(t *testing.T) {
	uri := "wireguard://" + urlenc(testPriv) + "@1.2.3.4:51820" +
		"?publickey=" + urlenc(testPub) + "&address=10.66.0.2/32&obfs=whatever"
	cfg, err := ParseWireGuardURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNotice(cfg.Notices, "obfs") {
		t.Errorf("notices = %v", cfg.Notices)
	}
}

func TestParseWireGuardURI_DropsIPv6(t *testing.T) {
	uri := "wireguard://" + urlenc(testPriv) + "@1.2.3.4:51820" +
		"?publickey=" + urlenc(testPub) + "&address=10.66.0.2/32,fd00::2/128"
	cfg, err := ParseWireGuardURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "10.66.0.2/32" {
		t.Errorf("address = %q", cfg.Address)
	}
	if !hasNotice(cfg.Notices, "IPv6") {
		t.Errorf("notices = %v", cfg.Notices)
	}
}

func TestParseWireGuardURI_NoIPv4AddressIsFatal(t *testing.T) {
	uri := "wireguard://" + urlenc(testPriv) + "@1.2.3.4:51820" +
		"?publickey=" + urlenc(testPub) + "&address=fd00::2/128"
	if _, err := ParseWireGuardURI(uri); !errors.Is(err, ErrNoIPv4Address) {
		t.Fatalf("err = %v, want ErrNoIPv4Address", err)
	}
}

const mullvadStyleConf = `[Interface]
# Device: Loud Panther
PrivateKey = ` + testPriv + `
Address = 10.66.0.2/32,fc00:bbbb::2/128
DNS = 10.64.0.1
MTU = 1380

[Peer]
PublicKey = ` + testPub + `
PresharedKey = ` + testPSK + `
AllowedIPs = 0.0.0.0/0,::0/0
Endpoint = 185.65.135.1:51820
PersistentKeepalive = 25
`

func TestParseWireGuardConf_ReadsAProviderConfig(t *testing.T) {
	cfg, err := ParseWireGuardConf(mullvadStyleConf)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != testPriv || cfg.Peer.PublicKey != testPub || cfg.Peer.PresharedKey != testPSK {
		t.Errorf("keys = %+v", cfg)
	}
	if cfg.Address != "10.66.0.2/32" || cfg.DNS != "10.64.0.1" || cfg.MTU != 1380 {
		t.Errorf("interface = %+v", cfg)
	}
	if cfg.Peer.Endpoint != "185.65.135.1:51820" || cfg.Peer.PersistentKeepalive != 25 {
		t.Errorf("peer = %+v", cfg.Peer)
	}
	if len(cfg.Peer.AllowedIPs) != 1 || cfg.Peer.AllowedIPs[0] != "0.0.0.0/0" {
		t.Errorf("allowed ips = %v", cfg.Peer.AllowedIPs)
	}
	if !hasNotice(cfg.Notices, "IPv6") {
		t.Errorf("dropped v6 silently: %v", cfg.Notices)
	}
}

// The whole reason this parser exists instead of wg-quick.
func TestParseWireGuardConf_RefusesScriptKeysByName(t *testing.T) {
	for _, key := range []string{"PostUp", "PreUp", "PostDown", "PreDown", "Table", "SaveConfig"} {
		conf := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.66.0.2/32\n" +
			key + " = rm -rf /\n\n[Peer]\nPublicKey = " + testPub + "\nEndpoint = 1.2.3.4:51820\n"
		_, err := ParseWireGuardConf(conf)
		if !errors.Is(err, ErrScriptKey) {
			t.Fatalf("%s: err = %v, want ErrScriptKey", key, err)
		}
		if !strings.Contains(err.Error(), key) {
			t.Errorf("%s: message does not name the key: %v", key, err)
		}
	}
}

func TestParseWireGuardConf_RefusesASecondPeer(t *testing.T) {
	conf := mullvadStyleConf + "\n[Peer]\nPublicKey = " + testPSK + "\nEndpoint = 9.9.9.9:51820\n"
	if _, err := ParseWireGuardConf(conf); !errors.Is(err, ErrMultiplePeers) {
		t.Fatalf("err = %v, want ErrMultiplePeers", err)
	}
}

func TestParseWireGuardConf_UnknownKeyIsANotice(t *testing.T) {
	conf := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.66.0.2/32\nFwMark = 0x1234\n" +
		"\n[Peer]\nPublicKey = " + testPub + "\nEndpoint = 1.2.3.4:51820\n"
	cfg, err := ParseWireGuardConf(conf)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNotice(cfg.Notices, "FwMark") {
		t.Errorf("notices = %v", cfg.Notices)
	}
}

func TestParseWireGuardConf_MissingAllowedIPsMeansEverything(t *testing.T) {
	conf := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.66.0.2/32\n" +
		"\n[Peer]\nPublicKey = " + testPub + "\nEndpoint = 1.2.3.4:51820\n"
	cfg, err := ParseWireGuardConf(conf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Peer.AllowedIPs) != 1 || cfg.Peer.AllowedIPs[0] != "0.0.0.0/0" {
		t.Fatalf("allowed ips = %v", cfg.Peer.AllowedIPs)
	}
	if !hasNotice(cfg.Notices, "0.0.0.0/0") {
		t.Errorf("notices = %v", cfg.Notices)
	}
}

func TestParseWireGuardConf_KeysAreCaseInsensitive(t *testing.T) {
	conf := "[interface]\nprivatekey=" + testPriv + "\naddress=10.66.0.2/32\n" +
		"[peer]\npublickey=" + testPub + "\nendpoint=1.2.3.4:51820\n"
	cfg, err := ParseWireGuardConf(conf)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != testPriv || cfg.Peer.PublicKey != testPub {
		t.Errorf("got %+v", cfg)
	}
}

func TestParseWireGuardConf_OnlyIPv6DNSFallsBackToNothing(t *testing.T) {
	conf := "[Interface]\nPrivateKey = " + testPriv + "\nAddress = 10.66.0.2/32\nDNS = fd00::1\n" +
		"[Peer]\nPublicKey = " + testPub + "\nEndpoint = 1.2.3.4:51820\n"
	cfg, err := ParseWireGuardConf(conf)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DNS != "" {
		t.Errorf("kept a v6 resolver on an IPv4-only router: %q", cfg.DNS)
	}
}

func TestValidateWireGuardConfig_CatchesBadInput(t *testing.T) {
	good := func() WireGuardConfig {
		return WireGuardConfig{
			PrivateKey: testPriv,
			Address:    "10.66.0.2/32",
			Peer: WGPeerConfig{
				PublicKey:  testPub,
				AllowedIPs: []string{"0.0.0.0/0"},
				Endpoint:   "1.2.3.4:51820",
			},
		}
	}
	if err := ValidateWireGuardConfig(&WireGuardConfig{}); err == nil {
		t.Error("empty config accepted")
	}
	for name, mutate := range map[string]func(*WireGuardConfig){
		"truncated private key": func(c *WireGuardConfig) { c.PrivateKey = "abc" },
		"truncated public key":  func(c *WireGuardConfig) { c.Peer.PublicKey = "abc" },
		"bad preshared key":     func(c *WireGuardConfig) { c.Peer.PresharedKey = "nope" },
		"address without mask":  func(c *WireGuardConfig) { c.Address = "10.66.0.2" },
		"endpoint without port": func(c *WireGuardConfig) { c.Peer.Endpoint = "1.2.3.4" },
		"endpoint bad port":     func(c *WireGuardConfig) { c.Peer.Endpoint = "1.2.3.4:nope" },
		"allowed ip not a cidr": func(c *WireGuardConfig) { c.Peer.AllowedIPs = []string{"1.2.3.4"} },
		"mtu too small":         func(c *WireGuardConfig) { c.MTU = 100 },
		"mtu too large":         func(c *WireGuardConfig) { c.MTU = 90000 },
	} {
		cfg := good()
		mutate(&cfg)
		if err := ValidateWireGuardConfig(&cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	cfg := good()
	if err := ValidateWireGuardConfig(&cfg); err != nil {
		t.Errorf("good config rejected: %v", err)
	}
}

func TestGenerateWGKeypair_DerivesThePublicKey(t *testing.T) {
	priv, pub, err := GenerateWGKeypair()
	if err != nil {
		t.Fatal(err)
	}
	if priv == "" || pub == "" || priv == pub {
		t.Fatalf("priv = %q pub = %q", priv, pub)
	}
	cfg := WireGuardConfig{
		PrivateKey: priv,
		Address:    "10.66.0.2/32",
		Peer:       WGPeerConfig{PublicKey: pub, AllowedIPs: []string{"0.0.0.0/0"}, Endpoint: "1.2.3.4:51820"},
	}
	if err := ValidateWireGuardConfig(&cfg); err != nil {
		t.Errorf("generated keys do not validate: %v", err)
	}
	// Same private key must always derive the same public one.
	again, err := WGPublicKeyOf(priv)
	if err != nil || again != pub {
		t.Errorf("derive = %q, %v; want %q", again, err, pub)
	}
}

func TestCoversDefaultRoute(t *testing.T) {
	for _, tc := range []struct {
		ips  []string
		want bool
	}{
		{[]string{"0.0.0.0/0"}, true},
		{[]string{"10.0.0.0/8", "0.0.0.0/0"}, true},
		{[]string{"10.0.0.0/8"}, false},
		{nil, false},
	} {
		if got := CoversDefaultRoute(tc.ips); got != tc.want {
			t.Errorf("CoversDefaultRoute(%v) = %v", tc.ips, got)
		}
	}
}

func urlenc(s string) string { return url.QueryEscape(s) }

func hasNotice(notices []string, want string) bool {
	for _, n := range notices {
		if strings.Contains(n, want) {
			return true
		}
	}
	return false
}
