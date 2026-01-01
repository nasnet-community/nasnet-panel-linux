package domain

import "testing"

func TestNode_BeforeCreate_GeneratesUUIDWhenEmpty(t *testing.T) {
	n := &Node{}
	if err := n.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate: %v", err)
	}
	if n.UUID == "" {
		t.Fatal("expected a generated UUID, got empty")
	}
}

// A node imported with a known UUID must keep it across saves.
func TestNode_BeforeCreate_PreservesExistingUUID(t *testing.T) {
	n := &Node{UUID: "fixed-uuid"}
	if err := n.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate: %v", err)
	}
	if n.UUID != "fixed-uuid" {
		t.Errorf("UUID changed: got %q", n.UUID)
	}
}

func TestWireGuardSettings_WGServerIP(t *testing.T) {
	tests := []struct {
		name     string
		endpoint []string
		want     string
	}{
		{"cidr drops mask", []string{"10.8.0.1/24"}, "10.8.0.1"},
		{"bare ip kept as-is", []string{"10.8.0.1"}, "10.8.0.1"},
		{"garbage returned verbatim", []string{"not-an-ip"}, "not-an-ip"},
		{"no endpoint", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &WireGuardSettings{Endpoint: tt.endpoint}
			if got := w.WGServerIP(); got != tt.want {
				t.Errorf("WGServerIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNode_GetAccessLogPath(t *testing.T) {
	tests := []struct {
		name      string
		logAccess string
		enabled   bool
		want      string
	}{
		{"explicit path wins", "/custom/access.log", false, "/custom/access.log"},
		{"enabled falls back to default", "", true, DefaultAccessLogPath},
		{"disabled and unset is empty", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{LogAccess: tt.logAccess, EnableAccessLog: tt.enabled}
			if got := n.GetAccessLogPath(); got != tt.want {
				t.Errorf("GetAccessLogPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNode_GetBandwidthSettingsOrDefault(t *testing.T) {
	def := (&Node{}).GetBandwidthSettingsOrDefault()
	if def.Interface != "eth0" || def.TotalBW != 1000 || def.Enabled {
		t.Errorf("default bandwidth = %+v", def)
	}

	custom := &BandwidthSettings{Enabled: true, Interface: "ens3", TotalBW: 500}
	if got := (&Node{BandwidthSettings: custom}).GetBandwidthSettingsOrDefault(); got != custom {
		t.Error("set bandwidth settings should pass through unchanged")
	}
}

func TestNode_GetStarlinkSettingsOrDefault(t *testing.T) {
	def := (&Node{}).GetStarlinkSettingsOrDefault()
	if def.DishAddress != "192.168.100.1:9200" || def.Enabled {
		t.Errorf("default starlink = %+v", def)
	}

	// Enabled but address left blank should be back-filled with the default dish.
	filled := (&Node{StarlinkSettings: &StarlinkSettings{Enabled: true}}).GetStarlinkSettingsOrDefault()
	if filled.DishAddress != "192.168.100.1:9200" || !filled.Enabled {
		t.Errorf("blank address not back-filled: %+v", filled)
	}

	custom := (&Node{StarlinkSettings: &StarlinkSettings{Enabled: true, DishAddress: "1.2.3.4:9200"}}).GetStarlinkSettingsOrDefault()
	if custom.DishAddress != "1.2.3.4:9200" {
		t.Errorf("custom dish address lost: %+v", custom)
	}
}

func TestNode_GetCrashRecoverySettingsOrDefault(t *testing.T) {
	def := (&Node{}).GetCrashRecoverySettingsOrDefault()
	if def.CommandTimeout != 60 || def.Cooldown != 30 || def.MaxAttempts != 3 {
		t.Errorf("default crash recovery = %+v", def)
	}

	// Non-positive timeout/cooldown are clamped back to defaults.
	clamped := (&Node{CrashRecoverySettings: &CrashRecoverySettings{Enabled: true, CommandTimeout: 0, Cooldown: -5}}).GetCrashRecoverySettingsOrDefault()
	if clamped.CommandTimeout != 60 || clamped.Cooldown != 30 {
		t.Errorf("expected clamped timeout/cooldown, got %+v", clamped)
	}
}

func TestNode_GetRoutingSettingsOrDefault(t *testing.T) {
	if got := (&Node{}).GetRoutingSettingsOrDefault(); got.DomainStrategy != "IPIfNonMatch" {
		t.Errorf("default routing strategy = %q", got.DomainStrategy)
	}
	custom := &RoutingSettings{DomainStrategy: "AsIs"}
	if got := (&Node{RoutingSettings: custom}).GetRoutingSettingsOrDefault(); got != custom {
		t.Error("set routing settings should pass through unchanged")
	}
}

// DNS getters are plain passthroughs — nil stays nil, set value is returned.
func TestNode_GetDNSSettings_Passthrough(t *testing.T) {
	if got := (&Node{}).GetDNSSettingsOrDefault(); got != nil {
		t.Errorf("nil DNS settings should stay nil, got %+v", got)
	}
	if got := (&Node{}).GetFakeDNSSettingsOrDefault(); got != nil {
		t.Errorf("nil fake DNS settings should stay nil, got %+v", got)
	}
}

func TestInbound_SettingsOrDefault_MeaningfulDefaults(t *testing.T) {
	in := &Inbound{}

	if s := in.GetSniffingSettingsOrDefault(); !s.Enabled || len(s.DestOverride) != 2 {
		t.Errorf("sniffing default = %+v", s)
	}
	if s := in.GetShadowsocksSettingsOrDefault(); s.Method != "2022-blake3-aes-128-gcm" || s.Network != "tcp,udp" {
		t.Errorf("shadowsocks default = %+v", s)
	}
	if s := in.GetWireGuardSettingsOrDefault(); s.MTU != 1420 {
		t.Errorf("wireguard default MTU = %d", s.MTU)
	}
	if s := in.GetSOCKSSettingsOrDefault(); s.Auth != "noauth" {
		t.Errorf("socks default auth = %q", s.Auth)
	}
	if s := in.GetVLESSSettingsOrDefault(); s.Encryption != "none" {
		t.Errorf("vless default encryption = %q", s.Encryption)
	}
	if s := in.GetVMessSettingsOrDefault(); s.Security != "auto" {
		t.Errorf("vmess default security = %q", s.Security)
	}
	if s := in.GetDokodemoSettingsOrDefault(); s.Networks != "tcp,udp" {
		t.Errorf("dokodemo default networks = %q", s.Networks)
	}
}

// The "empty struct" getters must never return nil so callers can deref safely.
func TestInbound_SettingsOrDefault_NonNilWhenUnset(t *testing.T) {
	in := &Inbound{}
	if in.GetTLSSettingsOrDefault() == nil ||
		in.GetRealitySettingsOrDefault() == nil ||
		in.GetTransportSettingsOrDefault() == nil ||
		in.GetHTTPSettingsOrDefault() == nil ||
		in.GetSockoptSettingsOrDefault() == nil ||
		in.GetHysteriaSettingsOrDefault() == nil ||
		in.GetTrojanSettingsOrDefault() == nil {
		t.Fatal("inbound getters returned nil for unset settings")
	}
}

func TestInbound_SettingsOrDefault_Passthrough(t *testing.T) {
	tls := &TLSSettings{ServerName: "example.com"}
	if got := (&Inbound{TLSSettings: tls}).GetTLSSettingsOrDefault(); got != tls {
		t.Error("set TLS settings should pass through unchanged")
	}
}

func TestOutbound_SettingsOrDefault_MeaningfulDefaults(t *testing.T) {
	out := &Outbound{}
	if s := out.GetFreedomSettingsOrDefault(); s.DomainStrategy != "AsIs" {
		t.Errorf("freedom default strategy = %q", s.DomainStrategy)
	}
	if s := out.GetBlackholeSettingsOrDefault(); s.ResponseType != "none" {
		t.Errorf("blackhole default response = %q", s.ResponseType)
	}
	if s := out.GetWireGuardSettingsOrDefault(); s.MTU != 1420 {
		t.Errorf("wireguard default MTU = %d", s.MTU)
	}
	if s := out.GetVLESSSettingsOrDefault(); s.Encryption != "none" {
		t.Errorf("vless default encryption = %q", s.Encryption)
	}
	if s := out.GetVMessSettingsOrDefault(); s.Security != "auto" {
		t.Errorf("vmess default security = %q", s.Security)
	}
}

func TestOutbound_SettingsOrDefault_NonNilWhenUnset(t *testing.T) {
	out := &Outbound{}
	if out.GetTLSSettingsOrDefault() == nil ||
		out.GetRealitySettingsOrDefault() == nil ||
		out.GetTransportSettingsOrDefault() == nil ||
		out.GetHTTPSettingsOrDefault() == nil ||
		out.GetDNSOutboundSettingsOrDefault() == nil ||
		out.GetLoopbackSettingsOrDefault() == nil ||
		out.GetSockoptSettingsOrDefault() == nil ||
		out.GetHysteriaSettingsOrDefault() == nil ||
		out.GetMuxSettingsOrDefault() == nil ||
		out.GetProxySettingsOrDefault() == nil ||
		out.GetTrojanSettingsOrDefault() == nil {
		t.Fatal("outbound getters returned nil for unset settings")
	}
}

// Shared pointer helper for the node domain test package.
func uintPtr(v uint) *uint { return &v }
