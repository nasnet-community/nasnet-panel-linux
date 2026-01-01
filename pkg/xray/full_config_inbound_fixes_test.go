package xray

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// buildOneInbound builds a config for a node with a single inbound and returns
// that inbound's config map plus the raw JSON.
func buildOneInbound(t *testing.T, inbound domain.Inbound, users map[string][]*User) (map[string]interface{}, string) {
	t.Helper()
	node := &domain.Node{
		Inbounds:     []domain.Inbound{inbound},
		Outbounds:    []domain.Outbound{},
		RoutingRules: []domain.RoutingRule{},
	}
	b := NewFullConfigBuilder(node)
	if users != nil {
		b = b.WithUsers(users)
	}
	jsonStr, err := b.WithAPI(false, 0).Build()
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		t.Fatalf("no inbounds in config: %s", jsonStr)
	}
	return inbounds[0].(map[string]interface{}), jsonStr
}

// C3: hysteria2 must emit protocol "hysteria" + a "hysteria" transport with
// hysteriaSettings{version:2, udpIdleTimeout}, NOT udpIdleTimeout in settings.
func TestHysteria2Wiring(t *testing.T) {
	inb, _ := buildOneInbound(t, domain.Inbound{
		Tag:      "hy2",
		Protocol: "hysteria2",
		Port:     443,
		Security: "tls",
		TLSSettings: &domain.TLSSettings{
			ServerName:   "example.com",
			ALPN:         []string{"H3"}, // wrong case; must be forced to "h3"
			Certificates: []domain.Certificate{{CertificateFile: "/c", KeyFile: "/k"}},
		},
		HysteriaSettings: &domain.HysteriaSettings{UdpIdleTimeout: 30},
	}, nil)

	if inb["protocol"] != "hysteria" {
		t.Errorf("protocol = %v, want hysteria", inb["protocol"])
	}
	stream := inb["streamSettings"].(map[string]interface{})
	if stream["network"] != "hysteria" {
		t.Errorf("network = %v, want hysteria", stream["network"])
	}
	hs, ok := stream["hysteriaSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing hysteriaSettings: %v", stream)
	}
	if hs["version"] != float64(2) {
		t.Errorf("hysteriaSettings.version = %v, want 2", hs["version"])
	}
	if hs["udpIdleTimeout"] != float64(30) {
		t.Errorf("hysteriaSettings.udpIdleTimeout = %v, want 30", hs["udpIdleTimeout"])
	}
	settings := inb["settings"].(map[string]interface{})
	if _, present := settings["udpIdleTimeout"]; present {
		t.Error("udpIdleTimeout must NOT be in protocol settings (HysteriaServerConfig has no such field)")
	}
	// ALPN must be forced to lowercase "h3" (QUIC/HTTP3 requirement); "H3" fails
	// the octet-compared ALPN handshake.
	tlsMap := stream["tlsSettings"].(map[string]interface{})
	alpn, _ := json.Marshal(tlsMap["alpn"])
	if string(alpn) != `["h3"]` {
		t.Errorf("hysteria2 alpn must be [\"h3\"], got %s", alpn)
	}
}

// C4: multi-user legacy-AEAD shadowsocks must give each client its own method.
func TestShadowsocksLegacyMultiUserMethod(t *testing.T) {
	inb, _ := buildOneInbound(t, domain.Inbound{
		Tag:                 "ss",
		Protocol:            "shadowsocks",
		Port:                8388,
		ShadowsocksSettings: &domain.ShadowsocksSettings{Method: "aes-256-gcm"},
	}, map[string][]*User{
		"ss": {
			{Email: "a@t", UUID: "pw-a"},
			{Email: "b@t", UUID: "pw-b"},
		},
	})
	settings := inb["settings"].(map[string]interface{})
	clients, ok := settings["clients"].([]interface{})
	if !ok || len(clients) != 2 {
		t.Fatalf("want 2 clients, got %v", settings["clients"])
	}
	for i, c := range clients {
		cm := c.(map[string]interface{})
		if cm["method"] != "aes-256-gcm" {
			t.Errorf("client[%d].method = %v, want aes-256-gcm", i, cm["method"])
		}
	}
}

// C1: XHTTP range fields must be xray's string form ("from-to"), never objects.
func TestXHTTPRangeStringForm(t *testing.T) {
	_, raw := buildOneInbound(t, domain.Inbound{
		Tag:      "vx",
		Protocol: "vless",
		Port:     443,
		Network:  "xhttp",
		TransportSettings: &domain.TransportSettings{
			XPaddingBytes:      &domain.RangeConfig{From: 100, To: 200},
			ScMaxEachPostBytes: &domain.RangeConfig{From: 1000000, To: 1000000},
		},
		VLESSSettings: &domain.VLESSSettings{},
	}, nil)
	if !strings.Contains(raw, `"xPaddingBytes": "100-200"`) {
		t.Errorf("xPaddingBytes not emitted as \"100-200\":\n%s", raw)
	}
	// From==To collapses to a plain int.
	if !strings.Contains(raw, `"scMaxEachPostBytes": 1000000`) {
		t.Errorf("scMaxEachPostBytes single value not emitted as int:\n%s", raw)
	}
	if strings.Contains(raw, `"from":`) || strings.Contains(raw, `"from" :`) {
		t.Errorf("range still emitted as {from,to} object:\n%s", raw)
	}
}

// H1: REALITY shortIds must be stable — empty stored id -> [""], never random.
func TestRealityShortIDStable(t *testing.T) {
	mk := func() string {
		inb, _ := buildOneInbound(t, domain.Inbound{
			Tag:      "vr",
			Protocol: "vless",
			Port:     443,
			Network:  "tcp",
			Security: "reality",
			RealitySettings: &domain.RealitySettings{
				PrivateKey:  "aGVsbG8taGVsbG8taGVsbG8taGVsbG8taGVsbG8", // arbitrary
				ServerNames: []string{"example.com"},
				Dest:        "example.com:443",
			},
			VLESSSettings: &domain.VLESSSettings{Flow: "xtls-rprx-vision"},
		}, nil)
		stream := inb["streamSettings"].(map[string]interface{})
		rs := stream["realitySettings"].(map[string]interface{})
		sids, _ := json.Marshal(rs["shortIds"])
		return string(sids)
	}
	first, second := mk(), mk()
	if first != `[""]` {
		t.Errorf("empty shortId should emit [\"\"], got %s", first)
	}
	if first != second {
		t.Errorf("shortIds not stable across builds: %s vs %s", first, second)
	}
}

// C2 + wrong-key fixes: allowInsecure must not be emitted; sockopt uses
// tcpMptcp (not mptcp); happyEyeballs uses tryDelayMs/maxConcurrentTry.
func TestSockoptKeysAndNoAllowInsecure(t *testing.T) {
	_, raw := buildOneInbound(t, domain.Inbound{
		Tag:      "vs",
		Protocol: "vless",
		Port:     443,
		Network:  "tcp",
		Security: "tls",
		TLSSettings: &domain.TLSSettings{
			ServerName:    "e.com",
			AllowInsecure: true,
			Certificates:  []domain.Certificate{{CertificateFile: "/c", KeyFile: "/k"}},
		},
		SockoptSettings: &domain.SockoptSettings{
			TcpMptcp:      true,
			HappyEyeballs: &domain.HappyEyeballsConfig{TryDelay: 250, MaxConcurrency: 4},
		},
		VLESSSettings: &domain.VLESSSettings{},
	}, nil)
	if strings.Contains(raw, "allowInsecure") {
		t.Errorf("allowInsecure must not be emitted (removed feature):\n%s", raw)
	}
	if !strings.Contains(raw, `"tcpMptcp": true`) {
		t.Errorf("expected tcpMptcp key:\n%s", raw)
	}
	if strings.Contains(raw, `"mptcp":`) {
		t.Errorf("stale mptcp key still emitted:\n%s", raw)
	}
	if !strings.Contains(raw, `"tryDelayMs":`) || !strings.Contains(raw, `"maxConcurrentTry":`) {
		t.Errorf("happyEyeballs keys wrong:\n%s", raw)
	}
}

// Capability: mixed protocol + port range.
func TestMixedProtocolAndPortRange(t *testing.T) {
	inb, raw := buildOneInbound(t, domain.Inbound{
		Tag:           "mx",
		Protocol:      "mixed",
		Port:          1080,
		PortRange:     "1080-1090",
		SOCKSSettings: &domain.SOCKSSettings{Auth: "noauth", UDP: true},
	}, nil)
	if inb["protocol"] != "mixed" {
		t.Errorf("protocol = %v, want mixed", inb["protocol"])
	}
	if inb["port"] != "1080-1090" {
		t.Errorf("port = %v (%T), want string \"1080-1090\"", inb["port"], inb["port"])
	}
	stream := inb["streamSettings"].(map[string]interface{})
	if stream["network"] != "tcp" {
		t.Errorf("mixed network = %v, want tcp", stream["network"])
	}
	if !strings.Contains(raw, `"udp": true`) {
		t.Errorf("socks settings not built for mixed:\n%s", raw)
	}
}

// Capability: REALITY multiple shortIds + TLS curvePreferences/verifyPeerCertByName.
func TestRealityMultiShortIDAndTLSExtras(t *testing.T) {
	inb, _ := buildOneInbound(t, domain.Inbound{
		Tag:      "vr2",
		Protocol: "vless",
		Port:     443,
		Network:  "tcp",
		Security: "reality",
		RealitySettings: &domain.RealitySettings{
			PrivateKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			ServerNames: []string{"example.com"},
			Dest:        "example.com:443",
			ShortID:     "01ab",
			ShortIDs:    []string{"01ab", "beef"},
			Mldsa65Seed: "seed-value",
		},
		VLESSSettings: &domain.VLESSSettings{Flow: "xtls-rprx-vision"},
	}, nil)
	stream := inb["streamSettings"].(map[string]interface{})
	rs := stream["realitySettings"].(map[string]interface{})
	sids := rs["shortIds"].([]interface{})
	if len(sids) != 2 || sids[0] != "01ab" || sids[1] != "beef" {
		t.Errorf("shortIds merge wrong: %v", sids)
	}
	if rs["mldsa65Seed"] != "seed-value" {
		t.Errorf("mldsa65Seed not emitted: %v", rs["mldsa65Seed"])
	}
}

// Capability: WS uses dedicated host field + acceptProxyProtocol, not headers.Host.
func TestWSHostFieldAndProxyProtocol(t *testing.T) {
	inb, raw := buildOneInbound(t, domain.Inbound{
		Tag:      "vw",
		Protocol: "vless",
		Port:     443,
		Network:  "ws",
		TransportSettings: &domain.TransportSettings{
			Path:                "/x",
			Host:                "cdn.example.com",
			AcceptProxyProtocol: true,
		},
		VLESSSettings: &domain.VLESSSettings{},
	}, nil)
	stream := inb["streamSettings"].(map[string]interface{})
	ws := stream["wsSettings"].(map[string]interface{})
	if ws["host"] != "cdn.example.com" {
		t.Errorf("ws host field missing: %v", ws["host"])
	}
	if ws["acceptProxyProtocol"] != true {
		t.Errorf("ws acceptProxyProtocol missing")
	}
	if strings.Contains(raw, `"Host": "cdn.example.com"`) {
		t.Errorf("host should not be emitted via headers.Host:\n%s", raw)
	}
}

// Hysteria2 must carry stream-level finalmask (e.g. salamander UDP mask) and
// sockopt — the minimal hysteria2 stream previously dropped them.
func TestHysteria2FinalMaskEmitted(t *testing.T) {
	inb, _ := buildOneInbound(t, domain.Inbound{
		Tag:      "hyfm",
		Protocol: "hysteria2",
		Port:     443,
		Security: "tls",
		TLSSettings: &domain.TLSSettings{
			ServerName:   "example.com",
			Certificates: []domain.Certificate{{CertificateFile: "/c", KeyFile: "/k"}},
		},
		FinalMask: &domain.FinalMask{
			// A lone object (as the editor placeholder produced) must be coerced
			// to a one-element array — xray wants finalmask.udp as []Mask.
			UDP: json.RawMessage(`{"type":"salamander","settings":{"password":"secret"}}`),
		},
		SockoptSettings: &domain.SockoptSettings{TcpFastOpen: true},
	}, nil)
	stream := inb["streamSettings"].(map[string]interface{})
	fm, ok := stream["finalmask"].(map[string]interface{})
	if !ok {
		t.Fatalf("finalmask missing from hysteria2 stream: %v", stream)
	}
	udp, ok := fm["udp"].([]interface{})
	if !ok {
		t.Fatalf("finalmask.udp must be an array (xray []Mask), got %T: %v", fm["udp"], fm["udp"])
	}
	if len(udp) != 1 {
		t.Errorf("finalmask.udp should wrap the lone mask into a 1-element array, got %v", udp)
	}
	if _, ok := stream["sockopt"]; !ok {
		t.Errorf("sockopt missing from hysteria2 stream: %v", stream)
	}
}
