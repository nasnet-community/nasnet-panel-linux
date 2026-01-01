package link

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateVLESS(t *testing.T) {
	outbound := &domain.Outbound{
		Tag:      "test-vless",
		Protocol: "vless",
		Address:  "example.com",
		Port:     443,
		Network:  "ws",
		Security: "tls",
		Remark:   "Test VLESS",
		VLESSSettings: &domain.VLESSSettings{
			UUID:       "00000000-0000-0000-0000-000000000000",
			Encryption: "none",
		},
		TLSSettings: &domain.TLSSettings{
			ServerName:  "example.com",
			Fingerprint: "chrome",
			ALPN:        []string{"h2", "http/1.1"},
		},
		TransportSettings: &domain.TransportSettings{
			Path: "/ws",
			Host: "example.com",
		},
	}

	link, err := Generate(outbound)
	require.NoError(t, err)
	assert.Contains(t, link, "vless://")
	assert.Contains(t, link, "00000000-0000-0000-0000-000000000000")
	assert.Contains(t, link, "example.com:443")
	assert.Contains(t, link, "security=tls")
	assert.Contains(t, link, "type=ws")
	assert.Contains(t, link, "Test%20VLESS")
}

func TestGenerateVMess(t *testing.T) {
	outbound := &domain.Outbound{
		Tag:      "test-vmess",
		Protocol: "vmess",
		Address:  "127.0.0.1",
		Port:     443,
		Network:  "tcp",
		Security: "none",
		Remark:   "Test VMess",
		VMessSettings: &domain.VMessSettings{
			UUID: "b2c64549-a060-4981-aace-d8932e13338e",
		},
	}

	link, err := Generate(outbound)
	require.NoError(t, err)
	assert.Contains(t, link, "vmess://")

	// Verify round-trip
	parsed, err := Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "vmess", parsed.Protocol)
	assert.Equal(t, "127.0.0.1", parsed.Address)
	assert.Equal(t, 443, parsed.Port)

	parsedProto := parsed.GetVMessSettingsOrDefault()
	assert.Equal(t, "b2c64549-a060-4981-aace-d8932e13338e", parsedProto.UUID)
}

func TestGenerateTrojan(t *testing.T) {
	outbound := &domain.Outbound{
		Tag:      "test-trojan",
		Protocol: "trojan",
		Address:  "trojan.server.com",
		Port:     443,
		Network:  "tcp",
		Security: "tls",
		Remark:   "Test Trojan",
		TrojanSettings: &domain.TrojanSettings{
			Password: "mypassword123",
		},
		TLSSettings: &domain.TLSSettings{
			ServerName: "trojan.server.com",
		},
	}

	link, err := Generate(outbound)
	require.NoError(t, err)
	assert.Contains(t, link, "trojan://")
	assert.Contains(t, link, "mypassword123")
	assert.Contains(t, link, "trojan.server.com:443")
}

func TestGenerateShadowsocks(t *testing.T) {
	outbound := &domain.Outbound{
		Tag:      "test-ss",
		Protocol: "shadowsocks",
		Address:  "ss.server.com",
		Port:     8388,
		Remark:   "Test SS",
		ShadowsocksSettings: &domain.ShadowsocksSettings{
			Method:   "aes-256-gcm",
			Password: "sspassword",
		},
	}

	link, err := Generate(outbound)
	require.NoError(t, err)
	assert.Contains(t, link, "ss://")
	assert.Contains(t, link, "ss.server.com:8388")
}

func TestGenerateSocks(t *testing.T) {
	outbound := &domain.Outbound{
		Tag:      "test-socks",
		Protocol: "socks",
		Address:  "socks.proxy.com",
		Port:     1080,
		Remark:   "Test Socks",
		SOCKSSettings: &domain.SOCKSSettings{
			Auth: "password",
			Accounts: []domain.SOCKSAccount{
				{User: "user", Pass: "pass"},
			},
		},
	}

	link, err := Generate(outbound)
	require.NoError(t, err)
	assert.Contains(t, link, "socks://")
	assert.Contains(t, link, "user:pass@")
	assert.Contains(t, link, "socks.proxy.com:1080")
}

func TestGenerateHTTP(t *testing.T) {
	outbound := &domain.Outbound{
		Tag:      "test-http",
		Protocol: "http",
		Address:  "http.proxy.com",
		Port:     8080,
		Remark:   "Test HTTP",
	}

	link, err := Generate(outbound)
	require.NoError(t, err)
	assert.Contains(t, link, "http://")
	assert.Contains(t, link, "http.proxy.com:8080")
}

func TestRoundTripVLESS(t *testing.T) {
	original := &domain.Outbound{
		Tag:      "roundtrip-vless",
		Protocol: "vless",
		Address:  "example.com",
		Port:     443,
		Network:  "xhttp",
		Security: "tls",
		Remark:   "Ultimate-proxy",
		VLESSSettings: &domain.VLESSSettings{
			UUID:       "00000000-0000-0000-0000-000000000000",
			Encryption: "none",
		},
		TLSSettings: &domain.TLSSettings{
			ServerName:  "cdn.example.com",
			Fingerprint: "firefox",
			ALPN:        []string{"h3", "h2", "http/1.1"},
		},
		TransportSettings: &domain.TransportSettings{
			Path: "/example",
			Host: "cdn.example.com",
			Mode: "packet-up",
		},
	}

	// Generate link
	link, err := Generate(original)
	require.NoError(t, err)
	t.Logf("Generated link: %s", link)

	// Parse it back
	parsed, err := Parse(link)
	require.NoError(t, err)

	// Verify key fields match
	assert.Equal(t, original.Protocol, parsed.Protocol)
	assert.Equal(t, original.Address, parsed.Address)
	assert.Equal(t, original.Port, parsed.Port)
	assert.Equal(t, original.Security, parsed.Security)

	originalProto := original.GetVLESSSettingsOrDefault()
	parsedProto := parsed.GetVLESSSettingsOrDefault()
	assert.Equal(t, originalProto.UUID, parsedProto.UUID)
}
