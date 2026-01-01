package httpclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	socks5 "github.com/armon/go-socks5"
)

// startSocks5 starts an in-process SOCKS5 server on an ephemeral port and
// returns its address. The server runs for the duration of the test.
func startSocks5(t *testing.T) string {
	t.Helper()
	srv, err := socks5.New(&socks5.Config{})
	if err != nil {
		t.Fatalf("socks5.New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String()
}

func TestFactory_ProxyClientReachesUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(upstream.Close)

	proxyAddr := startSocks5(t)

	f := NewFactory()
	f.Update(Config{
		ProxyURL: "socks5h://" + proxyAddr,
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	c := f.ClientFor(FeatureGeofiles, 5*time.Second)

	resp, err := c.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET via SOCKS5 proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
}

func TestFactory_DirectClient_NoProxyHop(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	t.Cleanup(upstream.Close)

	f := NewFactory() // empty config = no proxy
	c := f.ClientFor(FeatureGeofiles, 5*time.Second)
	resp, err := c.Get(upstream.URL)
	if err != nil {
		t.Fatalf("direct GET: %v", err)
	}
	resp.Body.Close()
}

func TestFactory_ContextCancellationDuringDial(t *testing.T) {
	// Point at a non-routable address so dial blocks. The transport's
	// DialContext should honor a cancelled context and return promptly.
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "socks5h://10.255.255.1:1080",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})
	c := f.ClientFor(FeatureGeofiles, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/", nil)
	start := time.Now()
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error due to context cancellation, got nil")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("dial took %v, expected prompt cancellation", elapsed)
	}
}
