package mtproto

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/armon/go-socks5"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/core"
)

// relay is the loopback SOCKS5 server telemt reaches Telegram through when the
// node marks tunnel egress. Every outbound connection is resolved and dialed
// by the tunnel dialer, so it carries the foreign mark (the VPN pool, or its
// blackhole when the pool is down) and passes the same block list as SSH.
type relay struct {
	ln net.Listener
}

// startRelay listens on addr and serves CONNECT only, for user/pass.
func startRelay(addr, user, pass string, d *core.Dialer) (*relay, error) {
	srv, err := socks5.New(&socks5.Config{
		Credentials: socks5.StaticCredentials{user: pass},
		Rules:       &socks5.PermitCommand{EnableConnect: true},
		Resolver:    dialerResolver{d},
		Dial:        d.DialContext,
		Logger:      log.New(io.Discard, "", 0),
	})
	if err != nil {
		return nil, fmt.Errorf("mtproto relay: %w", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mtproto relay: listen %s: %w", addr, err)
	}
	go func() { _ = srv.Serve(ln) }()
	return &relay{ln: ln}, nil
}

func (r *relay) addr() string { return r.ln.Addr().String() }

func (r *relay) close() {
	if r != nil {
		_ = r.ln.Close()
	}
}

// dialerResolver resolves names the way the dialer does (DoH over the mark in
// router mode), so a name telemt asks for is not resolved by the uplink's DNS.
type dialerResolver struct{ d *core.Dialer }

func (r dialerResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	ip, err := r.d.Lookup(ctx, name)
	if err != nil {
		return ctx, nil, err
	}
	return ctx, net.IP(ip.AsSlice()), nil
}
