package usecase

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func base(p string) *domain.Inbound {
	return &domain.Inbound{Tag: "t", Protocol: p, Port: 443}
}

func TestValidateInbound(t *testing.T) {
	tests := []struct {
		name    string
		build   func() *domain.Inbound
		wantErr string // "" = expect success
	}{
		{
			name: "reality without dest rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.Security = "reality"
				i.RealitySettings = &domain.RealitySettings{
					PrivateKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // 43 base64url chars = 32B
					ServerNames: []string{"e.com"},
				}
				return i
			},
			wantErr: "dest",
		},
		{
			name: "reality full valid",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.Security = "reality"
				i.RealitySettings = &domain.RealitySettings{
					PrivateKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
					ServerNames: []string{"e.com"},
					Dest:        "e.com:443",
					ShortID:     "01ab",
				}
				return i
			},
			wantErr: "",
		},
		{
			name: "reality odd shortId rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.Security = "reality"
				i.RealitySettings = &domain.RealitySettings{
					PrivateKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
					ServerNames: []string{"e.com"},
					Dest:        "e.com:443",
					ShortID:     "abc",
				}
				return i
			},
			wantErr: "shortId",
		},
		{
			name: "shadowsocks 2022 missing key rejected",
			build: func() *domain.Inbound {
				i := base("shadowsocks")
				i.ShadowsocksSettings = &domain.ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm"}
				return i
			},
			wantErr: "key",
		},
		{
			name: "shadowsocks 2022 wrong-length key rejected",
			build: func() *domain.Inbound {
				i := base("shadowsocks")
				i.ShadowsocksSettings = &domain.ShadowsocksSettings{
					Method:   "2022-blake3-aes-256-gcm",  // needs 32 bytes
					Password: "MTIzNDU2Nzg5MDEyMzQ1Ng==", // 16 bytes
				}
				return i
			},
			wantErr: "32 bytes",
		},
		{
			name: "shadowsocks 2022 correct key ok",
			build: func() *domain.Inbound {
				i := base("shadowsocks")
				i.ShadowsocksSettings = &domain.ShadowsocksSettings{
					Method:   "2022-blake3-aes-128-gcm",  // needs 16 bytes
					Password: "MTIzNDU2Nzg5MDEyMzQ1Ng==", // 16 bytes
				}
				return i
			},
			wantErr: "",
		},
		{
			name: "wireguard without secretKey rejected",
			build: func() *domain.Inbound {
				i := base("wireguard")
				i.WireGuardSettings = &domain.WireGuardSettings{Endpoint: []string{"10.0.0.1/32"}}
				return i
			},
			wantErr: "secretKey",
		},
		{
			name: "vless fallback empty dest rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.VLESSSettings = &domain.VLESSSettings{
					Fallbacks: []domain.Fallback{{Dest: ""}},
				}
				return i
			},
			wantErr: "dest",
		},
		{
			name: "removed transport quic rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "quic"
				i.VLESSSettings = &domain.VLESSSettings{}
				return i
			},
			wantErr: "transport",
		},
		// --- relaxations (previously wrongly rejected) ---
		{
			name: "http + tls now allowed",
			build: func() *domain.Inbound {
				i := base("http")
				i.Network = "tcp"
				i.Security = "tls"
				i.TLSSettings = &domain.TLSSettings{
					ServerName:   "e.com",
					Certificates: []domain.Certificate{{CertificateFile: "/c", KeyFile: "/k"}},
				}
				return i
			},
			wantErr: "",
		},
		{
			name: "tag with dot now allowed",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Tag = "vless.tcp.reality"
				i.Network = "tcp"
				i.VLESSSettings = &domain.VLESSSettings{}
				return i
			},
			wantErr: "",
		},
		{
			name: "fingerprint Chrome (mixed case) now allowed",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.Security = "tls"
				i.TLSSettings = &domain.TLSSettings{
					ServerName:   "e.com",
					Fingerprint:  "Chrome",
					Certificates: []domain.Certificate{{CertificateFile: "/c", KeyFile: "/k"}},
				}
				i.VLESSSettings = &domain.VLESSSettings{}
				return i
			},
			wantErr: "",
		},
		{
			name: "http + reality still rejected",
			build: func() *domain.Inbound {
				i := base("http")
				i.Network = "tcp"
				i.Security = "reality"
				return i
			},
			wantErr: "Reality",
		},
		{
			name: "invalid tcp headerType rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.VLESSSettings = &domain.VLESSSettings{}
				i.TransportSettings = &domain.TransportSettings{HeaderType: "bogus"}
				return i
			},
			wantErr: "headerType",
		},
		{
			name: "invalid xhttp mode rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "xhttp"
				i.VLESSSettings = &domain.VLESSSettings{}
				i.TransportSettings = &domain.TransportSettings{Mode: "turbo"}
				return i
			},
			wantErr: "mode",
		},
		{
			name: "unknown sniffing destOverride rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "tcp"
				i.VLESSSettings = &domain.VLESSSettings{}
				i.SniffingSettings = &domain.SniffingSettings{Enabled: true, DestOverride: []string{"http", "gopher"}}
				return i
			},
			wantErr: "destOverride",
		},
		{
			name: "socks password auth without accounts rejected",
			build: func() *domain.Inbound {
				i := base("socks")
				i.Network = "tcp"
				i.SOCKSSettings = &domain.SOCKSSettings{Auth: "password"}
				return i
			},
			wantErr: "account",
		},
		{
			name: "httpupgrade Host custom header rejected",
			build: func() *domain.Inbound {
				i := base("vless")
				i.Network = "httpupgrade"
				i.VLESSSettings = &domain.VLESSSettings{}
				i.TransportSettings = &domain.TransportSettings{Headers: map[string]string{"Host": "e.com"}}
				return i
			},
			wantErr: "Host",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInbound(tc.build())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
