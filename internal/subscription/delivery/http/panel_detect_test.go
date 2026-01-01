package http

import (
	"net/http"
	"testing"
)

func TestIsBrowserRequest(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		accept    string
		want      bool
	}{
		// Browsers
		{
			name:      "Chrome desktop",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			accept:    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			want:      true,
		},
		{
			name:      "Firefox desktop",
			userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
			accept:    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			want:      true,
		},
		{
			name:      "Safari macOS",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_2) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
			accept:    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			want:      true,
		},
		{
			name:      "Edge",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			accept:    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			want:      true,
		},
		{
			name:      "Chrome mobile",
			userAgent: "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			accept:    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			want:      true,
		},
		{
			name:      "Browser with only Accept header",
			userAgent: "",
			accept:    "text/html,application/xhtml+xml",
			want:      true,
		},
		{
			name:      "Browser UA without Accept text/html",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0",
			accept:    "application/json",
			want:      true,
		},

		// Proxy clients
		{
			name:      "v2rayNG",
			userAgent: "v2rayNG/1.8.5",
			accept:    "",
			want:      false,
		},
		{
			name:      "v2rayN",
			userAgent: "v2rayN/6.30",
			accept:    "",
			want:      false,
		},
		{
			name:      "Clash",
			userAgent: "clash-verge/1.4.2",
			accept:    "",
			want:      false,
		},
		{
			name:      "Sing-box",
			userAgent: "sing-box/1.7.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Shadowrocket",
			userAgent: "Shadowrocket/1900 CFNetwork/1490.0.4 Darwin/23.2.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Streisand",
			userAgent: "Streisand/2.0.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Hiddify",
			userAgent: "HiddifyNext/1.0.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "NekoBox",
			userAgent: "NekoBox/1.2.3",
			accept:    "",
			want:      false,
		},
		{
			name:      "NekoRay",
			userAgent: "NekoRay/3.26",
			accept:    "",
			want:      false,
		},
		{
			name:      "Surfboard",
			userAgent: "Surfboard/3.0.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Stash",
			userAgent: "Stash/2.5.0 CFNetwork/1474 Darwin/23.0.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Loon",
			userAgent: "Loon/665 CFNetwork/1474 Darwin/23.0.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Quantumult X",
			userAgent: "Quantumult%20X/1.3.0",
			accept:    "",
			want:      false,
		},
		{
			name:      "Surge",
			userAgent: "Surge iOS/3000",
			accept:    "",
			want:      false,
		},
		{
			name:      "OkHttp (Android proxy client)",
			userAgent: "okhttp/4.12.0",
			accept:    "",
			want:      false,
		},

		// Proxy client with Accept text/html should still be detected as proxy
		{
			name:      "v2rayNG with Accept text/html",
			userAgent: "v2rayNG/1.8.5",
			accept:    "text/html,*/*",
			want:      false,
		},
		{
			name:      "Clash with Accept text/html",
			userAgent: "clash-verge/1.4.2",
			accept:    "text/html",
			want:      false,
		},

		// Edge cases
		{
			name:      "curl (no UA, no Accept)",
			userAgent: "curl/8.4.0",
			accept:    "*/*",
			want:      false,
		},
		{
			name:      "empty UA and Accept",
			userAgent: "",
			accept:    "",
			want:      false,
		},
		{
			name:      "wget",
			userAgent: "Wget/1.21.4",
			accept:    "*/*",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/sub/test-uuid", nil)
			if tt.userAgent != "" {
				r.Header.Set("User-Agent", tt.userAgent)
			}
			if tt.accept != "" {
				r.Header.Set("Accept", tt.accept)
			}

			got := isBrowserRequest(r)
			if got != tt.want {
				t.Errorf("isBrowserRequest() = %v, want %v (UA: %q, Accept: %q)",
					got, tt.want, tt.userAgent, tt.accept)
			}
		})
	}
}
