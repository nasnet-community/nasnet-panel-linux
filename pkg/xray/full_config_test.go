package xray

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func TestFullConfigBuilder_StatsKey(t *testing.T) {
	node := &domain.Node{
		Inbounds:     []domain.Inbound{},
		Outbounds:    []domain.Outbound{},
		RoutingRules: []domain.RoutingRule{},
	}

	builder := NewFullConfigBuilder(node)
	jsonStr, err := builder.Build()
	if err != nil {
		t.Fatalf("Failed to build config: %v", err)
	}

	// Verify stats key presence in JSON string
	if !strings.Contains(jsonStr, `"stats": {}`) {
		t.Error("Config JSON missing 'stats' key or it is not empty object")
	}

	// Double check by unmarshaling to a map
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &configMap); err != nil {
		t.Fatalf("Failed to unmarshal config JSON: %v", err)
	}

	if _, ok := configMap["stats"]; !ok {
		t.Error("Config map missing 'stats' key")
	}
}

func TestVLESSFlowFallback(t *testing.T) {
	// 1. Setup VLESS inbound with default flow
	inbound := domain.Inbound{
		Tag:      "vless-in",
		Protocol: "vless",
		Port:     443,
		VLESSSettings: &domain.VLESSSettings{
			Flow: "xtls-rprx-vision",
		},
	}

	node := &domain.Node{
		Inbounds:     []domain.Inbound{inbound},
		Outbounds:    []domain.Outbound{},
		RoutingRules: []domain.RoutingRule{},
	}

	// 2. Setup users
	users := map[string][]*User{
		"vless-in": {
			{Email: "user1@test.com", UUID: "uuid1", Flow: ""},       // Should inherit default
			{Email: "user2@test.com", UUID: "uuid2", Flow: "custom"}, // Should keep custom
		},
	}

	// 3. Build config
	builder := NewFullConfigBuilder(node).WithUsers(users).WithAPI(false, 0)
	jsonStr, err := builder.Build()
	if err != nil {
		t.Fatalf("Failed to build config: %v", err)
	}

	// 4. Verify results by unmarshaling
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &configMap); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Navigate to clients list: inbounds[0].settings.clients
	inbounds, ok := configMap["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		t.Fatal("Inbounds missing or empty")
	}
	settings, ok := inbounds[0].(map[string]interface{})["settings"].(map[string]interface{})
	if !ok {
		t.Fatal("Settings missing")
	}
	clients, ok := settings["clients"].([]interface{})
	if !ok {
		t.Fatal("Clients missing")
	}

	foundUser1 := false
	foundUser2 := false

	for _, c := range clients {
		client := c.(map[string]interface{})
		email := client["email"].(string)
		flow := client["flow"].(string)

		if email == "user1@test.com" {
			foundUser1 = true
			if flow != "xtls-rprx-vision" {
				t.Errorf("User1 (empty flow) should inherit 'xtls-rprx-vision', got '%s'", flow)
			}
		}
		if email == "user2@test.com" {
			foundUser2 = true
			if flow != "custom" {
				t.Errorf("User2 (custom flow) should keep 'custom', got '%s'", flow)
			}
		}
	}

	if !foundUser1 || !foundUser2 {
		t.Error("Failed to find test users in config")
	}
}

func TestHysteria2OutboundTransport(t *testing.T) {
	outbound := domain.Outbound{
		Tag:      "hy2-out",
		Protocol: "hysteria2",
		Address:  "example.com",
		Port:     443,
		Security: "tls",
		HysteriaSettings: &domain.HysteriaSettings{
			Auth:           "secret-auth",
			UdpIdleTimeout: 30,
		},
		TLSSettings: &domain.TLSSettings{ServerName: "example.com"},
	}
	node := &domain.Node{
		Inbounds:     []domain.Inbound{},
		Outbounds:    []domain.Outbound{outbound},
		RoutingRules: []domain.RoutingRule{},
	}

	jsonStr, err := NewFullConfigBuilder(node).WithAPI(false, 0).Build()
	if err != nil {
		t.Fatalf("Failed to build config: %v", err)
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &configMap); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	outbounds, _ := configMap["outbounds"].([]interface{})
	var ob map[string]interface{}
	for _, o := range outbounds {
		m := o.(map[string]interface{})
		if m["tag"] == "hy2-out" {
			ob = m
			break
		}
	}
	if ob == nil {
		t.Fatal("hy2-out outbound missing from config")
	}

	if ob["protocol"] != "hysteria" {
		t.Errorf("protocol should be translated to 'hysteria', got %v", ob["protocol"])
	}

	stream, ok := ob["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("streamSettings missing on hysteria2 outbound")
	}
	if stream["network"] != "hysteria" {
		t.Errorf(`streamSettings.network should be "hysteria", got %v`, stream["network"])
	}
	if stream["security"] != "tls" {
		t.Errorf(`streamSettings.security should be "tls", got %v`, stream["security"])
	}
	hs, ok := stream["hysteriaSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("streamSettings.hysteriaSettings missing")
	}
	if hs["auth"] != "secret-auth" {
		t.Errorf("hysteriaSettings.auth should carry the client auth, got %v", hs["auth"])
	}
	if hs["version"] == nil {
		t.Error("hysteriaSettings.version missing")
	}

	settings, ok := ob["settings"].(map[string]interface{})
	if !ok {
		t.Fatal("protocol settings missing")
	}
	if _, leaked := settings["auth"]; leaked {
		t.Error("auth must not leak into protocol settings (HysteriaClientConfig ignores it)")
	}
	if _, leaked := settings["congestion"]; leaked {
		t.Error("congestion must not be emitted in protocol settings")
	}
	if settings["address"] != "example.com" {
		t.Errorf("protocol settings.address should be the server address, got %v", settings["address"])
	}
}

func TestBuildDNS_AllFields(t *testing.T) {
	boolTrue := true
	boolFalse := false
	ttl := uint32(3600)
	globalTTL := uint32(7200)

	node := &domain.Node{
		DNSSettings: &domain.DNSSettings{
			Servers: []domain.DNSServer{
				{Address: "8.8.8.8"}, // simple server
				{
					Address:         "https://dns.google/dns-query",
					Port:            443,
					Domains:         []string{"geosite:google"},
					ExpectedIPs:     []string{"geoip:us", "*"},
					UnexpectedIPs:   []string{"geoip:cn"},
					SkipFallback:    true,
					QueryStrategy:   "UseIPv4",
					Tag:             "google-doh",
					ClientIP:        "5.6.7.8",
					TimeoutMs:       5000,
					DisableCache:    &boolFalse,
					ServeStale:      &boolTrue,
					ServeExpiredTTL: &ttl,
					FinalQuery:      true,
				},
			},
			Hosts:                  map[string]any{"example.com": "1.2.3.4"},
			ClientIP:               "1.2.3.4",
			QueryStrategy:          "UseSystem",
			DisableCache:           false,
			DisableFallback:        true,
			DisableFallbackIfMatch: true,
			Tag:                    "dns-in",
			ServeStale:             true,
			ServeExpiredTTL:        &globalTTL,
			EnableParallelQuery:    true,
			UseSystemHosts:         true,
		},
		Inbounds:     []domain.Inbound{},
		Outbounds:    []domain.Outbound{},
		RoutingRules: []domain.RoutingRule{},
	}

	builder := NewFullConfigBuilder(node).WithAPI(false, 0)
	jsonStr, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	dns, ok := cfg["dns"].(map[string]interface{})
	if !ok {
		t.Fatal("dns section missing or not an object")
	}

	// Global fields
	if dns["clientIp"] != "1.2.3.4" {
		t.Errorf("clientIp = %v, want 1.2.3.4", dns["clientIp"])
	}
	if dns["queryStrategy"] != "UseSystem" {
		t.Errorf("queryStrategy = %v, want UseSystem", dns["queryStrategy"])
	}
	if dns["disableFallback"] != true {
		t.Errorf("disableFallback = %v, want true", dns["disableFallback"])
	}
	if dns["serveStale"] != true {
		t.Errorf("serveStale = %v, want true", dns["serveStale"])
	}
	if dns["serveExpiredTTL"] != float64(7200) {
		t.Errorf("serveExpiredTTL = %v, want 7200", dns["serveExpiredTTL"])
	}
	if dns["enableParallelQuery"] != true {
		t.Errorf("enableParallelQuery = %v, want true", dns["enableParallelQuery"])
	}
	if dns["useSystemHosts"] != true {
		t.Errorf("useSystemHosts = %v, want true", dns["useSystemHosts"])
	}
	if dns["tag"] != "dns-in" {
		t.Errorf("tag = %v, want dns-in", dns["tag"])
	}

	// Servers
	servers, ok := dns["servers"].([]interface{})
	if !ok || len(servers) != 2 {
		t.Fatalf("servers = %v, want 2 entries", dns["servers"])
	}

	// First server should be a simple string
	if servers[0] != "8.8.8.8" {
		t.Errorf("servers[0] = %v, want string '8.8.8.8'", servers[0])
	}

	// Second server should be an object with all fields
	s2, ok := servers[1].(map[string]interface{})
	if !ok {
		t.Fatalf("servers[1] not an object: %v", servers[1])
	}
	if s2["address"] != "https://dns.google/dns-query" {
		t.Errorf("servers[1].address = %v", s2["address"])
	}
	if s2["clientIp"] != "5.6.7.8" {
		t.Errorf("servers[1].clientIp = %v", s2["clientIp"])
	}
	if s2["timeoutMs"] != float64(5000) {
		t.Errorf("servers[1].timeoutMs = %v", s2["timeoutMs"])
	}
	if s2["disableCache"] != false {
		t.Errorf("servers[1].disableCache = %v, want false", s2["disableCache"])
	}
	if s2["serveStale"] != true {
		t.Errorf("servers[1].serveStale = %v, want true", s2["serveStale"])
	}
	if s2["serveExpiredTTL"] != float64(3600) {
		t.Errorf("servers[1].serveExpiredTTL = %v, want 3600", s2["serveExpiredTTL"])
	}
	if s2["finalQuery"] != true {
		t.Errorf("servers[1].finalQuery = %v, want true", s2["finalQuery"])
	}
	if s2["skipFallback"] != true {
		t.Errorf("servers[1].skipFallback = %v", s2["skipFallback"])
	}
}

func TestBuildDNS_NilSettings(t *testing.T) {
	node := &domain.Node{
		DNSSettings:  nil,
		Inbounds:     []domain.Inbound{},
		Outbounds:    []domain.Outbound{},
		RoutingRules: []domain.RoutingRule{},
	}

	builder := NewFullConfigBuilder(node).WithAPI(false, 0)
	jsonStr, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	var cfg map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &cfg)
	if _, ok := cfg["dns"]; ok {
		t.Error("dns section should be omitted when settings are nil")
	}
}

func TestBuildDNS_FakeDNS(t *testing.T) {
	node := &domain.Node{
		FakeDNSSettings: []domain.FakeDNSPool{
			{IPPool: "198.18.0.0/15", LRUSize: 65535},
			{IPPool: "fc00::/18", LRUSize: 65535},
		},
		Inbounds:     []domain.Inbound{},
		Outbounds:    []domain.Outbound{},
		RoutingRules: []domain.RoutingRule{},
	}

	builder := NewFullConfigBuilder(node).WithAPI(false, 0)
	jsonStr, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	var cfg map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &cfg)

	// xray-core HEAD reads top-level FakeDNS from JSON key "fakeDns".
	if _, oldKey := cfg["fakedns"]; oldKey {
		t.Errorf("legacy lower-case 'fakedns' key emitted; xray-core HEAD expects 'fakeDns'")
	}
	fakedns, ok := cfg["fakeDns"]
	if !ok {
		t.Fatal("fakeDns section missing")
	}
	pools, ok := fakedns.([]interface{})
	if !ok {
		t.Fatalf("fakeDns should be an array for multiple pools, got %T", fakedns)
	}
	if len(pools) != 2 {
		t.Errorf("fakeDns pools count = %d, want 2", len(pools))
	}
	for i, p := range pools {
		obj, ok := p.(map[string]interface{})
		if !ok {
			t.Fatalf("pool[%d] not an object: %T", i, p)
		}
		if _, oldKey := obj["lruSize"]; oldKey {
			t.Errorf("pool[%d]: legacy 'lruSize' key emitted; xray-core HEAD expects 'poolSize'", i)
		}
		if _, ok := obj["poolSize"]; !ok {
			t.Errorf("pool[%d]: 'poolSize' key missing", i)
		}
	}
}

func TestBuildDNSOutbound_BlockTypes(t *testing.T) {
	node := &domain.Node{
		Inbounds: []domain.Inbound{},
		Outbounds: []domain.Outbound{
			{
				Tag:      "dns-out",
				Protocol: "dns",
				DNSOutboundSettings: &domain.DNSOutboundSettings{
					Network:    "tcp",
					Address:    "8.8.8.8",
					Port:       53,
					NonIPQuery: "reject",
					BlockTypes: []int32{28},
				},
			},
		},
		RoutingRules: []domain.RoutingRule{},
	}

	builder := NewFullConfigBuilder(node).WithAPI(false, 0)
	jsonStr, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	var cfg map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &cfg)

	outbounds := cfg["outbounds"].([]interface{})
	var dnsOut map[string]interface{}
	for _, ob := range outbounds {
		o := ob.(map[string]interface{})
		if o["protocol"] == "dns" {
			dnsOut = o
			break
		}
	}
	if dnsOut == nil {
		t.Fatal("dns outbound not found")
	}
	settings := dnsOut["settings"].(map[string]interface{})
	if settings["nonIPQuery"] != "reject" {
		t.Errorf("nonIPQuery = %v, want reject", settings["nonIPQuery"])
	}
	bt := settings["blockTypes"].([]interface{})
	if len(bt) != 1 || bt[0] != float64(28) {
		t.Errorf("blockTypes = %v, want [28]", bt)
	}
}
