package cmd

import (
	"testing"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func TestTunnelHost_RemoteUsesNodeIP(t *testing.T) {
	n := &nodeDomain.Node{IP: "198.51.100.8", ConnectMode: nodeDomain.ConnectModeDirect}
	if got := tunnelHost(n, "http://192.168.1.10:9761", "203.0.113.10"); got != "198.51.100.8" {
		t.Fatalf("got %q", got)
	}
}

func TestTunnelHost_LocalNonLoopbackIPWins(t *testing.T) {
	n := &nodeDomain.Node{IP: "203.0.113.50", ConnectMode: nodeDomain.ConnectModeLocal}
	if got := tunnelHost(n, "http://192.168.1.10:9761", "203.0.113.10"); got != "203.0.113.50" {
		t.Fatalf("got %q", got)
	}
}

func TestTunnelHost_LocalLoopbackPrefersPublicHost(t *testing.T) {
	n := &nodeDomain.Node{IP: "127.0.0.1", ConnectMode: nodeDomain.ConnectModeLocal}
	if got := tunnelHost(n, "http://192.168.1.10:9761", "203.0.113.10"); got != "203.0.113.10" {
		t.Fatalf("got %q, want public host so APP_BASE_URL can stay LAN-only", got)
	}
}

func TestTunnelHost_LocalFallsBackToBaseURL(t *testing.T) {
	n := &nodeDomain.Node{IP: "127.0.0.1", ConnectMode: nodeDomain.ConnectModeLocal}
	if got := tunnelHost(n, "http://192.168.1.10:9761", ""); got != "192.168.1.10" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizePublicHost(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"  ":                        "",
		"203.0.113.10":              "203.0.113.10",
		"203.0.113.10:8448":         "203.0.113.10",
		"http://203.0.113.10:9761/": "203.0.113.10",
		"proxy.example.com":         "proxy.example.com",
		"https://proxy.example.com": "proxy.example.com",
	}
	for in, want := range cases {
		if got := normalizePublicHost(in); got != want {
			t.Errorf("normalizePublicHost(%q)=%q want %q", in, got, want)
		}
	}
}
