package dohboot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeDoH answers every A question with the given address.
func fakeDoH(t *testing.T, answer string, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != contentType {
			t.Errorf("request = %s %q", r.Method, r.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var q dnsmessage.Message
		if err := q.Unpack(body); err != nil {
			t.Errorf("unparsable query: %v", err)
			return
		}
		if len(q.Questions) != 1 || q.Questions[0].Type != dnsmessage.TypeA {
			t.Errorf("questions = %+v", q.Questions)
			return
		}

		resp := dnsmessage.Message{
			Header:    dnsmessage.Header{Response: true, RecursionAvailable: true},
			Questions: q.Questions,
			Answers: []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{
					Name:  q.Questions[0].Name,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   60,
				},
				Body: &dnsmessage.AResource{A: netip.MustParseAddr(answer).As4()},
			}},
		}
		packed, err := resp.Pack()
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(packed)
	}))
}

// serversFor points the resolver at test servers, using each one's own client
// so the self-signed certificate verifies.
func serversFor(ts ...*httptest.Server) ([]Server, func(Server) *http.Client) {
	servers := make([]Server, 0, len(ts))
	clients := map[string]*http.Client{}
	for _, s := range ts {
		addr := strings.TrimPrefix(s.URL, "https://")
		servers = append(servers, Server{IP: addr, ServerName: "example.com"})
		clients[addr] = s.Client()
	}
	return servers, func(s Server) *http.Client { return clients[s.IP] }
}

func TestResolve_ReturnsTheAnswer(t *testing.T) {
	ts := fakeDoH(t, "185.65.135.1", 0)
	defer ts.Close()
	servers, client := serversFor(ts)

	got, err := newWith(servers, client).Resolve(context.Background(), "vpn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "185.65.135.1" {
		t.Errorf("got %v", got)
	}
}

// An IP endpoint must not touch the network: the tunnel has to come up on a box
// whose only resolver lives inside it.
func TestResolve_IPLiteralSkipsTheNetwork(t *testing.T) {
	r := newWith([]Server{{IP: "127.0.0.1:1", ServerName: "nope"}}, func(Server) *http.Client {
		t.Fatal("an IP literal made a request")
		return nil
	})
	got, err := r.Resolve(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "1.2.3.4" {
		t.Errorf("got %v", got)
	}
}

// One operator being unreachable from this cell is exactly why there are four.
func TestResolve_FallsThroughToTheNextServer(t *testing.T) {
	dead := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer dead.Close()
	alive := fakeDoH(t, "9.9.9.9", 0)
	defer alive.Close()

	servers, client := serversFor(dead, alive)
	got, err := newWith(servers, client).Resolve(context.Background(), "vpn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "9.9.9.9" {
		t.Errorf("got %v", got)
	}
}

func TestResolve_AllServersFailing(t *testing.T) {
	dead := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer dead.Close()

	servers, client := serversFor(dead)
	_, err := newWith(servers, client).Resolve(context.Background(), "vpn.example.com")
	if err == nil {
		t.Fatal("no error when every server failed")
	}
	if !strings.Contains(err.Error(), "vpn.example.com") {
		t.Errorf("error does not name the host: %v", err)
	}
}

func TestResolve_HonoursTheContext(t *testing.T) {
	slow := fakeDoH(t, "1.2.3.4", 300*time.Millisecond)
	defer slow.Close()
	servers, client := serversFor(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := newWith(servers, client).Resolve(ctx, "vpn.example.com"); err == nil {
		t.Fatal("a cancelled context still resolved")
	}
}

func TestResolve_RejectsAnEmptyHost(t *testing.T) {
	if _, err := New(0).Resolve(context.Background(), ""); err == nil {
		t.Error("an empty host was accepted")
	}
}

// The set is hardcoded on purpose: an editable list would be an editable hole
// in the kill switch.
func TestBootstrapIPs_AreTwoIndependentOperators(t *testing.T) {
	ips := BootstrapIPs()
	if len(ips) != 4 {
		t.Fatalf("got %v", ips)
	}
	for _, want := range []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"} {
		found := false
		for _, ip := range ips {
			if ip == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing from %v", want, ips)
		}
	}
}
