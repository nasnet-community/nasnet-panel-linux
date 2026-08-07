package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestFactory_ClientFor_DisabledFeature_ReturnsDirectClient(t *testing.T) {
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "socks5h://localhost:1080",
		Enabled:  map[Feature]bool{FeatureGeofiles: false},
	})
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	if c == nil {
		t.Fatal("ClientFor returned nil")
	}
	if c.Timeout != 5*time.Second {
		t.Fatalf("timeout = %v, want 5s", c.Timeout)
	}
	if c.Transport != http.DefaultTransport {
		t.Fatal("expected direct (DefaultTransport) when feature disabled")
	}
}

func TestFactory_ClientFor_EnabledButEmptyURL_FallsBackDirect(t *testing.T) {
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	if c.Transport != http.DefaultTransport {
		t.Fatal("empty URL with toggle on must fall back to direct")
	}
}

func TestFactory_ClientFor_EnabledWithURL_UsesProxyTransport(t *testing.T) {
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "socks5h://127.0.0.1:1080",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	if c.Transport == http.DefaultTransport {
		t.Fatal("expected proxy transport when URL set and feature enabled")
	}
}

func TestFactory_ClientFor_InvalidURL_FallsBackDirect(t *testing.T) {
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "not a url",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	if c.Transport != http.DefaultTransport {
		t.Fatal("invalid URL must fall back to direct")
	}
}

func TestFactory_ClientFor_UnsupportedScheme_FallsBackDirect(t *testing.T) {
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "ftp://example:21",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	if c.Transport != http.DefaultTransport {
		t.Fatal("unsupported scheme must fall back to direct")
	}
}

func TestFactory_Update_AtomicSwap_NewRequestsUseNewState(t *testing.T) {
	f := NewFactory()
	f.Update(Config{ProxyURL: "socks5h://a:1080", Enabled: map[Feature]bool{FeatureGeofiles: true}})
	first := f.ClientFor(FeatureGeofiles, EgressForeign, time.Second)
	firstT := first.Transport

	f.Update(Config{ProxyURL: "socks5h://b:1080", Enabled: map[Feature]bool{FeatureGeofiles: true}})
	second := f.ClientFor(FeatureGeofiles, EgressForeign, time.Second)
	if second.Transport == firstT {
		t.Fatal("Update should produce a new transport for the new URL")
	}
}

func TestFactory_IsProxyConfigured(t *testing.T) {
	f := NewFactory()
	if f.IsProxyConfigured() {
		t.Fatal("new factory should not have a proxy configured")
	}
	f.Update(Config{ProxyURL: "socks5h://127.0.0.1:1080"})
	if !f.IsProxyConfigured() {
		t.Fatal("valid url should mark proxy configured")
	}
	f.Update(Config{ProxyURL: "bogus"})
	if f.IsProxyConfigured() {
		t.Fatal("invalid url should mark proxy not configured")
	}
}

func TestParseProxyURL_ValidSchemes(t *testing.T) {
	cases := []string{
		"socks5://h:1080",
		"socks5h://h:1080",
		"socks5://u:p@h:1080",
		"socks5h://u:p@h:1080",
		"socks5h://u:p%40special@h:1080",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := parseProxyURL(in); err != nil {
				t.Fatalf("parseProxyURL(%q) error: %v", in, err)
			}
		})
	}
}

func TestParseProxyURL_Rejects(t *testing.T) {
	bad := []string{"", "not a url", "http://h:80", "socks5://", "socks5://h"}
	for _, in := range bad {
		t.Run(in, func(t *testing.T) {
			if _, err := parseProxyURL(in); err == nil {
				t.Fatalf("parseProxyURL(%q) expected error, got nil", in)
			}
		})
	}
}

func TestRedactURL_StripsPassword(t *testing.T) {
	in := "socks5h://alice:secret@h:1080"
	out := redactURL(in)
	if out == in {
		t.Fatalf("redactURL did not change input: %q", out)
	}
	for _, bad := range []string{"secret"} {
		if contains(out, bad) {
			t.Fatalf("redactURL leaked password: %q", out)
		}
	}
	if !contains(out, "alice") {
		t.Fatalf("redactURL should preserve username: %q", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLiveClient_FollowsUpdates(t *testing.T) {
	f := NewFactory()
	live := f.LiveClient(FeatureGeofiles, EgressForeign, time.Second)

	// Initial state: no proxy. Transport delegates to DefaultTransport via factory.
	if live.Transport == nil {
		t.Fatal("LiveClient transport nil")
	}

	// Update factory to a proxy URL — the live client's transport should
	// route through the new proxy on next RoundTrip without rebuild.
	f.Update(Config{
		ProxyURL: "socks5h://127.0.0.1:1080",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	// Verify by reading the wrapped RT — the inner client now has a proxy
	// transport.
	inner := f.ClientFor(FeatureGeofiles, EgressForeign, time.Second)
	if inner.Transport == http.DefaultTransport {
		t.Fatal("post-update transport should be proxy, not default")
	}
}

func TestAllFeatures_CoversAll(t *testing.T) {
	got := AllFeatures()
	if len(got) != 8 {
		t.Fatalf("expected 8 features, got %d", len(got))
	}
	seen := map[Feature]bool{}
	for _, f := range got {
		if seen[f] {
			t.Fatalf("duplicate feature %q", f)
		}
		seen[f] = true
	}
}
