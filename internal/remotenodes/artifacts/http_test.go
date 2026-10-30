// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"syscall"
	"testing"
	"time"
)

func TestNewClientSettings(t *testing.T) {
	c := NewClient(netip.MustParseAddr("192.168.50.16"))
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.Transport)
	}
	if tr.Proxy != nil {
		t.Error("the client reads a proxy from the environment")
	}
	for name, d := range map[string]time.Duration{
		"TLS handshake": tr.TLSHandshakeTimeout, "response header": tr.ResponseHeaderTimeout, "idle connection": tr.IdleConnTimeout,
	} {
		if d <= 0 || d > time.Minute {
			t.Errorf("the %s timeout is %v", name, d)
		}
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Errorf("TLS config %+v", tr.TLSClientConfig)
	}
	if !tr.DisableCompression || tr.ForceAttemptHTTP2 {
		t.Error("the client asks for compression or HTTP/2")
	}
	// a whole transfer has no deadline here: stalls and the context end it
	if c.Timeout != 0 {
		t.Errorf("the client times out after %v, which would cut a slow link's download", c.Timeout)
	}
}

// seen answers with the address a request came from.
func seen(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		io.WriteString(w, host)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, c *http.Client, url string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestTheClientLeavesFromTheAddressItIsGiven(t *testing.T) {
	srv := seen(t)
	for _, local := range []string{"127.0.0.1", "::ffff:127.0.0.1"} {
		got, err := get(t, NewClient(netip.MustParseAddr(local)), srv.URL)
		if err != nil || got != "127.0.0.1" {
			t.Errorf("from %s the server saw %q, %v", local, got, err)
		}
	}
	// no address: the system picks, which for a loopback server is the loopback
	if got, err := get(t, NewClient(netip.Addr{}), srv.URL); err != nil || got != "127.0.0.1" {
		t.Errorf("unbound: %q, %v", got, err)
	}
}

// 127.0.0.1 is where a connection to 127.0.0.1 comes from anyway, so it shows
// little. Linux owns all of 127/8, which lets another address show that the
// choice is the client's. Elsewhere the check can't be made.
func TestTheServerSeesTheAddressTheClientChose(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("this system has no 127.0.0.2 on its loopback, so a second local address can't be tried: %v", err)
	}
	l.Close()
	srv := seen(t)
	got, err := get(t, NewClient(netip.MustParseAddr("127.0.0.2")), srv.URL)
	if err != nil || got != "127.0.0.2" {
		t.Fatalf("the server saw %q, %v; want 127.0.0.2", got, err)
	}
}

// The client binds before it connects, so an address the machine doesn't have
// can't be used. That shows the choice is made on every system, whether or not
// there is a second local address to see it with.
func TestAnAddressTheMachineLacksCannotBeUsed(t *testing.T) {
	srv := seen(t)
	_, err := get(t, NewClient(netip.MustParseAddr("192.0.2.1")), srv.URL) // TEST-NET-1, nobody's
	if !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Fatalf("a connection from an address that isn't ours: %v", err)
	}
	_, err = FetchAgent(bg, NewClient(netip.MustParseAddr("192.0.2.1")), Source{Base: srv.URL}, "arm64")
	if !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Fatalf("FetchAgent from an address that isn't ours: %v", err)
	}
}

func TestTheClientCanLeaveFromAnIPv6Address(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback here: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		io.WriteString(w, host)
	}))
	srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	defer srv.Close()
	got, err := get(t, NewClient(netip.MustParseAddr("::1")), srv.URL)
	if err != nil || got != "::1" {
		t.Fatalf("the server saw %q, %v", got, err)
	}
	// an IPv4 source can't reach an IPv6-only server, and says so instead of going out some other way
	if _, err := get(t, NewClient(netip.MustParseAddr("127.0.0.1")), srv.URL); err == nil {
		t.Fatal("a connection from an IPv4 address reached an IPv6 server")
	}
}
