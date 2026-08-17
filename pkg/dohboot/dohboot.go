// Package dohboot resolves a VPN endpoint hostname before the tunnel that
// would normally carry DNS exists.
//
// The kill switch drops raw egress on the secondary uplink, so the only way out
// for this lookup is a narrow exemption: marked sockets, TCP 443, to these four
// addresses. That is why the server list is hardcoded — an operator-editable
// list would be an operator-editable hole in the kill switch.
package dohboot

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"golang.org/x/net/dns/dnsmessage"
)

const (
	contentType   = "application/dns-message"
	perServerWait = 5 * time.Second
	// Long enough to span one outage's re-resolves.
	idleConnTimeout = 90 * time.Second
	// A DoH answer is small; anything larger is not one.
	maxResponse = 64 << 10
)

// Server is one bootstrap resolver, addressed by IP so no other resolver is
// needed to reach it. ServerName is what the certificate must match.
type Server struct {
	IP         string
	ServerName string
}

// Servers is the fixed set: two operators, so one being unreachable from a
// given Starlink cell is survivable.
var Servers = []Server{
	{IP: "1.1.1.1", ServerName: "cloudflare-dns.com"},
	{IP: "1.0.0.1", ServerName: "cloudflare-dns.com"},
	{IP: "8.8.8.8", ServerName: "dns.google"},
	{IP: "8.8.4.4", ServerName: "dns.google"},
}

// BootstrapIPs is the allowlist the kill switch has to punch through.
func BootstrapIPs() []string {
	out := make([]string, 0, len(Servers))
	for _, s := range Servers {
		out = append(out, s.IP)
	}
	return out
}

// Resolver turns an endpoint host into an address.
type Resolver interface {
	Resolve(ctx context.Context, host string) (netip.Addr, error)
}

type resolver struct {
	servers []Server
	client  func(Server) *http.Client
}

// New returns a resolver whose sockets carry mark, so they match the kill
// switch exemption and leave by the secondary uplink.
func New(mark uint32) Resolver {
	// One client per server: a Transport per request leaks its idle sockets.
	var mu sync.Mutex
	clients := map[string]*http.Client{}

	return &resolver{
		servers: Servers,
		client: func(s Server) *http.Client {
			mu.Lock()
			defer mu.Unlock()
			if c, ok := clients[s.ServerName]; ok {
				return c
			}
			dialer := httpclient.MarkedDialer(mark, perServerWait)
			c := &http.Client{
				Timeout: perServerWait,
				Transport: &http.Transport{
					DialContext: dialer.DialContext,
					// Connecting by IP, so the name to verify has to be said
					// explicitly or the certificate never matches.
					TLSClientConfig:   &tls.Config{ServerName: s.ServerName, MinVersion: tls.VersionTLS12},
					ForceAttemptHTTP2: true,
					IdleConnTimeout:   idleConnTimeout,
					MaxIdleConns:      2,
				},
			}
			clients[s.ServerName] = c
			return c
		},
	}
}

func newWith(servers []Server, client func(Server) *http.Client) Resolver {
	return &resolver{servers: servers, client: client}
}

func (r *resolver) Resolve(ctx context.Context, host string) (netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return netip.Addr{}, errors.New("no host to resolve")
	}
	// An endpoint that is already an address needs nothing: this path has to
	// work on a box whose only working resolver is inside the tunnel.
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip, nil
	}

	query, err := packQuery(host)
	if err != nil {
		return netip.Addr{}, err
	}

	var errs []error
	for _, s := range r.servers {
		addr, err := r.ask(ctx, s, host, query)
		if err == nil {
			return addr, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", s.IP, err))
		if ctx.Err() != nil {
			break
		}
	}
	return netip.Addr{}, fmt.Errorf("could not resolve %q over the bootstrap resolvers: %w",
		host, errors.Join(errs...))
}

func (r *resolver) ask(ctx context.Context, s Server, host string, query []byte) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, perServerWait)
	defer cancel()

	url := "https://" + net.JoinHostPort(hostOf(s.IP), portOf(s.IP)) + "/dns-query"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(query))
	if err != nil {
		return netip.Addr{}, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", contentType)

	resp, err := r.client(s).Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return netip.Addr{}, err
	}
	return firstA(body, host)
}

func packQuery(host string) ([]byte, error) {
	name, err := dnsmessage.NewName(fqdn(host))
	if err != nil {
		return nil, fmt.Errorf("%q is not a hostname: %w", host, err)
	}
	// RFC 8484 wants ID 0: the transport already correlates the answer.
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 0, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name:  name,
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
		}},
	}
	return msg.Pack()
}

func firstA(body []byte, host string) (netip.Addr, error) {
	var msg dnsmessage.Message
	if err := msg.Unpack(body); err != nil {
		return netip.Addr{}, fmt.Errorf("unreadable answer: %w", err)
	}
	if msg.Header.RCode != dnsmessage.RCodeSuccess {
		return netip.Addr{}, fmt.Errorf("answered %s", msg.Header.RCode)
	}
	for _, a := range msg.Answers {
		if rec, ok := a.Body.(*dnsmessage.AResource); ok {
			return netip.AddrFrom4(rec.A), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no IPv4 address for %q", host)
}

func fqdn(host string) string {
	if strings.HasSuffix(host, ".") {
		return host
	}
	return host + "."
}

// The production servers are bare IPs; tests point at a host:port.
func hostOf(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

func portOf(s string) string {
	if _, p, err := net.SplitHostPort(s); err == nil {
		return p
	}
	return "443"
}
