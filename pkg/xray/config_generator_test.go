package xray

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func parseURI(t *testing.T, link string) *url.URL {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	return u
}

func decodeVmess(t *testing.T, link string) map[string]interface{} {
	t.Helper()
	if !strings.HasPrefix(link, "vmess://") {
		t.Fatalf("not a vmess link: %q", link)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
	if err != nil {
		t.Fatalf("decode vmess: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal vmess: %v", err)
	}
	return out
}

func TestGenerateConfigLink_VLESS_TLSWithAllowInsecureAndALPN(t *testing.T) {
	info := &InboundInfo{
		Protocol: "vless",
		Network:  "tcp",
		Security: "tls",
		Port:     443,
		TLSConfig: &TLSInfoConfig{
			SNI:           "example.com",
			ALPN:          []string{"h2", "http/1.1"},
			Fingerprint:   "chrome",
			AllowInsecure: true,
		},
	}
	link, err := GenerateConfigLink(info, "uuid-1", "1.2.3.4", "name")
	if err != nil {
		t.Fatal(err)
	}
	q := parseURI(t, link).Query()
	if q.Get("security") != "tls" {
		t.Fatalf("security = %q", q.Get("security"))
	}
	if q.Get("sni") != "example.com" {
		t.Fatalf("sni = %q", q.Get("sni"))
	}
	if q.Get("alpn") != "h2,http/1.1" {
		t.Fatalf("alpn = %q", q.Get("alpn"))
	}
	if q.Get("fp") != "chrome" {
		t.Fatalf("fp = %q", q.Get("fp"))
	}
	if q.Get("allowInsecure") != "1" {
		t.Fatalf("allowInsecure missing: %v", q)
	}
}

func TestGenerateConfigLink_VLESS_RealityWithSpiderX(t *testing.T) {
	info := &InboundInfo{
		Protocol: "vless",
		Network:  "tcp",
		Security: "reality",
		Port:     443,
		RealityConfig: &RealityInfoConfig{
			ServerName:  "www.cloudflare.com",
			PublicKey:   "PBK",
			ShortID:     "abcd",
			Fingerprint: "chrome",
			SpiderX:     "/path",
		},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("pbk") != "PBK" || q.Get("sid") != "abcd" || q.Get("fp") != "chrome" {
		t.Fatalf("reality core fields missing: %v", q)
	}
	if q.Get("spx") != "/path" {
		t.Fatalf("spx missing: %q", q.Get("spx"))
	}
}

func TestGenerateConfigLink_VLESS_FragmentParam(t *testing.T) {
	info := &InboundInfo{
		Protocol: "vless",
		Network:  "tcp",
		Security: "none",
		Port:     80,
		Fragment: &FragmentInfoConfig{Packets: "tlshello", Length: "100-200", Interval: "10-20"},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("fragment") != "tlshello,100-200,10-20" {
		t.Fatalf("fragment = %q", q.Get("fragment"))
	}
}

func TestGenerateConfigLink_VLESS_GRPCServiceName(t *testing.T) {
	info := &InboundInfo{
		Protocol:        "vless",
		Network:         "grpc",
		Security:        "tls",
		Port:            443,
		GRPCServiceName: "MyGrpcSvc",
		TLSConfig:       &TLSInfoConfig{SNI: "x"},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("type") != "grpc" || q.Get("serviceName") != "MyGrpcSvc" {
		t.Fatalf("grpc params wrong: %v", q)
	}
}

func TestGenerateConfigLink_VLESS_XHTTPParamsIncludeMode(t *testing.T) {
	info := &InboundInfo{
		Protocol:  "vless",
		Network:   "xhttp",
		Security:  "tls",
		Port:      443,
		XHTTPPath: "/dl",
		XHTTPHost: "cdn.example.com",
		XHTTPMode: "stream-up",
		TLSConfig: &TLSInfoConfig{SNI: "cdn.example.com"},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("path") != "/dl" || q.Get("host") != "cdn.example.com" || q.Get("mode") != "stream-up" {
		t.Fatalf("xhttp params wrong: %v", q)
	}
}

func TestGenerateConfigLink_TLSConfig_NotEmittedWhenSecurityNone(t *testing.T) {
	// Even if leftover TLSConfig is present, security="none" must NOT leak sni/alpn/fp.
	info := &InboundInfo{
		Protocol:  "vless",
		Network:   "tcp",
		Security:  "none",
		Port:      80,
		TLSConfig: &TLSInfoConfig{SNI: "leaked.example.com", Fingerprint: "chrome"},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("sni") != "" || q.Get("fp") != "" {
		t.Fatalf("TLS leaked into security=none link: %v", q)
	}
}

func TestGenerateConfigLink_Trojan_TLSEmitsAllFingerprintAndAllowInsecure(t *testing.T) {
	info := &InboundInfo{
		Protocol: "trojan",
		Network:  "tcp",
		Security: "tls",
		Port:     443,
		TLSConfig: &TLSInfoConfig{
			SNI:           "trojan.example.com",
			ALPN:          []string{"h2"},
			Fingerprint:   "chrome",
			AllowInsecure: true,
		},
	}
	link, _ := GenerateConfigLink(info, "passwd", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("fp") != "chrome" {
		t.Fatalf("trojan fp missing: %v", q)
	}
	if q.Get("allowInsecure") != "1" {
		t.Fatalf("trojan allowInsecure missing: %v", q)
	}
	if q.Get("alpn") != "h2" {
		t.Fatalf("trojan alpn missing: %v", q)
	}
}

func TestGenerateConfigLink_Trojan_RealitySupported(t *testing.T) {
	info := &InboundInfo{
		Protocol: "trojan",
		Network:  "tcp",
		Security: "reality",
		Port:     443,
		RealityConfig: &RealityInfoConfig{
			ServerName: "www.example.com",
			PublicKey:  "PBK",
			ShortID:    "abcd",
			SpiderX:    "/p",
		},
	}
	link, _ := GenerateConfigLink(info, "p", "1.1.1.1", "n")
	q := parseURI(t, link).Query()
	if q.Get("security") != "reality" {
		t.Fatalf("trojan security wrong: %v", q)
	}
	if q.Get("pbk") != "PBK" || q.Get("sid") != "abcd" || q.Get("spx") != "/p" {
		t.Fatalf("trojan reality params wrong: %v", q)
	}
}

func TestGenerateConfigLink_VMess_AlterIdAndScyOverridable(t *testing.T) {
	info := &InboundInfo{
		Protocol:      "vmess",
		Network:       "tcp",
		Security:      "none",
		Port:          80,
		VMessAlterId:  64,
		VMessSecurity: "aes-128-gcm",
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	cfg := decodeVmess(t, link)
	if cfg["aid"] != "64" {
		t.Fatalf("aid = %v", cfg["aid"])
	}
	if cfg["scy"] != "aes-128-gcm" {
		t.Fatalf("scy = %v", cfg["scy"])
	}
}

func TestGenerateConfigLink_VMess_TLSFieldsPopulated(t *testing.T) {
	info := &InboundInfo{
		Protocol: "vmess",
		Network:  "tcp",
		Security: "tls",
		Port:     443,
		TLSConfig: &TLSInfoConfig{
			SNI:           "vmess.example.com",
			ALPN:          []string{"h2"},
			Fingerprint:   "firefox",
			AllowInsecure: true,
		},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	cfg := decodeVmess(t, link)
	if cfg["tls"] != "tls" {
		t.Fatalf("tls field = %v", cfg["tls"])
	}
	if cfg["sni"] != "vmess.example.com" {
		t.Fatalf("sni = %v", cfg["sni"])
	}
	if cfg["fp"] != "firefox" {
		t.Fatalf("fp = %v", cfg["fp"])
	}
	if cfg["alpn"] != "h2" {
		t.Fatalf("alpn = %v", cfg["alpn"])
	}
	if cfg["allowInsecure"] != true {
		t.Fatalf("allowInsecure = %v", cfg["allowInsecure"])
	}
}

func TestGenerateConfigLink_VMess_RealitySupported(t *testing.T) {
	info := &InboundInfo{
		Protocol: "vmess",
		Network:  "tcp",
		Security: "reality",
		Port:     443,
		RealityConfig: &RealityInfoConfig{
			ServerName: "www.example.com",
			PublicKey:  "PBK",
			ShortID:    "abcd",
			SpiderX:    "/p",
		},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	cfg := decodeVmess(t, link)
	if cfg["tls"] != "reality" {
		t.Fatalf("tls = %v", cfg["tls"])
	}
	if cfg["pbk"] != "PBK" || cfg["sid"] != "abcd" || cfg["spx"] != "/p" {
		t.Fatalf("vmess reality fields wrong: %v", cfg)
	}
}

func TestGenerateConfigLink_VMess_XHTTPTransport(t *testing.T) {
	info := &InboundInfo{
		Protocol:  "vmess",
		Network:   "xhttp",
		Security:  "none",
		Port:      80,
		XHTTPPath: "/dl",
		XHTTPHost: "cdn.example.com",
		XHTTPMode: "stream-up",
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	cfg := decodeVmess(t, link)
	if cfg["net"] != "xhttp" || cfg["path"] != "/dl" || cfg["host"] != "cdn.example.com" || cfg["mode"] != "stream-up" {
		t.Fatalf("vmess xhttp fields wrong: %v", cfg)
	}
}

func TestGenerateConfigLink_VMess_HTTPUpgradeTransport(t *testing.T) {
	info := &InboundInfo{
		Protocol:        "vmess",
		Network:         "httpupgrade",
		Security:        "none",
		Port:            80,
		HTTPUpgradePath: "/u",
		HTTPUpgradeHost: "h.example.com",
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	cfg := decodeVmess(t, link)
	if cfg["net"] != "httpupgrade" || cfg["path"] != "/u" || cfg["host"] != "h.example.com" {
		t.Fatalf("vmess httpupgrade fields wrong: %v", cfg)
	}
}

func TestGenerateConfigLink_VMess_FragmentEmitted(t *testing.T) {
	info := &InboundInfo{
		Protocol: "vmess",
		Network:  "tcp",
		Security: "none",
		Port:     80,
		Fragment: &FragmentInfoConfig{Packets: "tlshello", Length: "100-200", Interval: "10-20"},
	}
	link, _ := GenerateConfigLink(info, "u", "1.1.1.1", "n")
	cfg := decodeVmess(t, link)
	if cfg["fragment"] != "tlshello,100-200,10-20" {
		t.Fatalf("vmess fragment = %v", cfg["fragment"])
	}
}

func TestGenerateConfigLink_Hysteria2(t *testing.T) {
	info := &InboundInfo{
		Protocol:             "hysteria2",
		Port:                 443,
		Security:             "tls",
		TLSConfig:            &TLSInfoConfig{SNI: "example.com", AllowInsecure: true},
		HysteriaObfsPassword: "NasNet1234",
		PortRange:            "5000-6000",
	}
	link, err := GenerateConfigLink(info, "11111111-1111-1111-1111-111111111111", "1.2.3.4", "MyNode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link == "" {
		t.Fatal("hysteria2 link is empty (the reported bug)")
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("generated link does not parse: %v", err)
	}
	q := u.Query()
	checks := map[string]string{
		"sni":           "example.com",
		"insecure":      "1",
		"obfs":          "salamander",
		"obfs-password": "NasNet1234",
		"mport":         "5000-6000",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("param %q = %q, want %q (link=%s)", k, got, want, link)
		}
	}
	if !strings.HasPrefix(link, "hysteria2://11111111-1111-1111-1111-111111111111@1.2.3.4:443") {
		t.Errorf("bad prefix: %s", link)
	}
	if u.Fragment != "MyNode" {
		t.Errorf("remark = %q, want MyNode", u.Fragment)
	}
}

func TestGenerateConfigLink_WireGuard(t *testing.T) {
	info := &InboundInfo{
		Protocol:          "wireguard",
		Port:              5060,
		WGPrivateKey:      "cGriv+privateKey/base64ExampleAAAAAAAAAAAA=",
		WGServerPublicKey: "serverPub/base64ExampleBBBBBBBBBBBBBBBBBBBB=",
		WGAddress:         "10.8.0.2",
		WGPresharedKey:    "psk/base64ExampleCCCCCCCCCCCCCCCCCCCCCCCC=",
		WGMTU:             1420,
		WGReserved:        []int{1, 2, 3},
	}
	link, err := GenerateConfigLink(info, "unused-uuid", "1.2.3.4", "WG Node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("link does not parse: %v (%s)", err, link)
	}
	if u.Scheme != "wireguard" {
		t.Errorf("scheme = %q, want wireguard", u.Scheme)
	}
	if u.User == nil || u.User.Username() != info.WGPrivateKey {
		t.Errorf("private key not in userinfo: %v", u.User)
	}
	if u.Host != "1.2.3.4:5060" {
		t.Errorf("host = %q, want 1.2.3.4:5060", u.Host)
	}
	q := u.Query()
	checks := map[string]string{
		"publickey":    info.WGServerPublicKey,
		"presharedkey": info.WGPresharedKey,
		"address":      "10.8.0.2/32",
		"mtu":          "1420",
		"reserved":     "1,2,3",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("param %q = %q, want %q", k, got, want)
		}
	}
	if u.Fragment != "WG Node" {
		t.Errorf("remark = %q, want WG Node", u.Fragment)
	}
}

func TestGenerateConfigLink_WireGuard_NoPeerEmpty(t *testing.T) {
	info := &InboundInfo{Protocol: "wireguard", Port: 5060, WGServerPublicKey: "x"}
	link, err := GenerateConfigLink(info, "u", "1.2.3.4", "r")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link != "" {
		t.Errorf("expected empty link without private key, got %q", link)
	}
}
