package sshcore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/core"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	gossh "golang.org/x/crypto/ssh"
)

// settings is the per-inbound JSON blob (core_inbounds.settings) for SSH.
type settings struct {
	HostKeyPEM string `json:"host_key"` // PEM-encoded ed25519 private key
	Strategy   string `json:"strategy"` // "accept" (default) | "reject"
}

// ibConfig is the hot-reloadable part of an inbound: the fields Apply rewrites
// on every reconcile and that connection handlers read. It is published as an
// immutable snapshot via inbound.cfg (atomic.Pointer), so handlers read a
// consistent view lock-free while Apply swaps in a fresh snapshot — the same
// hot-swap idiom accountStore uses for credentials. Treat a loaded *ibConfig as
// read-only; never mutate it in place.
type ibConfig struct {
	signer   gossh.Signer
	strategy string
}

// inbound is one bound SSH listener.
type inbound struct {
	id        string
	ln        net.Listener
	reqListen string // requested listen (to detect rebind)
	reqPort   int    // requested port (0 = ephemeral)
	port      int    // resolved port
	accounts  *accountStore

	// cfg is the current hot-swappable config snapshot. Apply publishes it via
	// Store; connection handlers read it via Load. Always non-nil once the
	// inbound is registered (Apply stores it before starting the accept loop).
	cfg atomic.Pointer[ibConfig]

	// enabledEmails is the set of emails currently allowed (from the pushed
	// user set). Used by the sweeper to kill disabled accounts' sessions.
	// Only accessed under Core.mu.
	enabledEmails map[string]bool

	quit chan struct{}
}

// Core is the SSH protocol core. One instance handles all SSH inbounds on the
// node (keyed by inbound id); the session registry is shared for accounting.
type Core struct {
	mu       sync.Mutex
	inbounds map[string]*inbound
	sessions *sessionRegistry

	// dial opens the egress connection for a direct-tcpip channel.
	dial DialFunc
}

// DialFunc opens an egress connection.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Option configures a Core.
type Option func(*Core)

// WithDialer sets the egress dialer (the filtered, marked core.Dialer in
// production). Without it the core dials directly.
func WithDialer(dial DialFunc) Option {
	return func(c *Core) { c.dial = dial }
}

// egressTimeout bounds resolving and dialing one direct-tcpip destination.
const egressTimeout = 15 * time.Second

// New builds an empty SSH core.
func New(opts ...Option) *Core {
	c := &Core{
		inbounds: make(map[string]*inbound),
		sessions: newSessionRegistry(),
	}
	for _, o := range opts {
		o(c)
	}
	if c.dial == nil {
		c.dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	return c
}

// Type implements core.Core.
func (c *Core) Type() core.CoreType { return core.CoreTypeSSH }

// Apply reconciles one SSH inbound to the desired state.
func (c *Core) Apply(ctx context.Context, d core.DesiredState) error {
	if d.CoreType != core.CoreTypeSSH {
		return fmt.Errorf("ssh core: wrong core type %q", d.CoreType)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if !d.Enabled {
		c.teardownLocked(d.InboundID)
		return nil
	}

	var st settings
	if len(d.Settings) > 0 {
		if err := json.Unmarshal(d.Settings, &st); err != nil {
			return fmt.Errorf("ssh core: bad settings: %w", err)
		}
	}
	if st.HostKeyPEM == "" {
		return fmt.Errorf("ssh core: host_key required in settings")
	}
	signer, err := gossh.ParsePrivateKey([]byte(st.HostKeyPEM))
	if err != nil {
		return fmt.Errorf("ssh core: parse host key: %w", err)
	}
	strategy := st.Strategy
	if strategy == "" {
		strategy = strategyAccept
	}

	newCfg := &ibConfig{
		signer:   signer,
		strategy: strategy,
	}

	ib := c.inbounds[d.InboundID]
	if ib == nil || ib.reqListen != d.Listen || ib.reqPort != d.Port {
		if ib != nil {
			c.teardownLocked(d.InboundID)
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(d.Listen, strconv.Itoa(d.Port)))
		if err != nil {
			return fmt.Errorf("ssh core: listen %s:%d: %w", d.Listen, d.Port, err)
		}
		ib = &inbound{
			id:        d.InboundID,
			ln:        ln,
			reqListen: d.Listen,
			reqPort:   d.Port,
			port:      ln.Addr().(*net.TCPAddr).Port,
			accounts:  newAccountStore(),
			quit:      make(chan struct{}),
		}
		// Publish config before the accept loop can hand a connection to a
		// reader — this closes the startup-ordering race.
		ib.cfg.Store(newCfg)
		c.inbounds[d.InboundID] = ib
		go c.acceptLoop(ib)
	} else {
		// Hot-swap config on the running listener; in-flight handlers keep the
		// snapshot they already loaded.
		ib.cfg.Store(newCfg)
	}

	ib.accounts.replace(d.Users)

	enabled := make(map[string]bool, len(d.Users))
	for _, u := range d.Users {
		if u.Enabled {
			enabled[u.Email] = true
		}
	}
	ib.enabledEmails = enabled

	// Immediately kill sessions for accounts that became disabled in this push.
	c.killDisabledLocked(ib)
	return nil
}

// boundPort returns the resolved listener port for an inbound (test helper).
func (c *Core) boundPort(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ib := c.inbounds[id]; ib != nil {
		return ib.port
	}
	return 0
}

func (c *Core) teardownLocked(id string) {
	ib := c.inbounds[id]
	if ib == nil {
		return
	}
	close(ib.quit)
	_ = ib.ln.Close()
	delete(c.inbounds, id)
	// Kill live sessions belonging to this inbound.
	for _, list := range c.sessions.sessionsByEmail() {
		for _, s := range list {
			if s.inbound == id && s.closeFn != nil {
				s.closeFn()
			}
		}
	}
}

// killDisabledLocked closes sessions on ib whose account is no longer enabled.
func (c *Core) killDisabledLocked(ib *inbound) {
	for _, list := range c.sessions.sessionsByEmail() {
		for _, s := range list {
			if s.inbound == ib.id && !ib.enabledEmails[s.email] && s.closeFn != nil {
				s.closeFn()
			}
		}
	}
}

func (c *Core) acceptLoop(ib *inbound) {
	for {
		conn, err := ib.ln.Accept()
		if err != nil {
			select {
			case <-ib.quit:
			default:
			}
			return
		}
		go c.handleConn(ib, conn)
	}
}

func (c *Core) handleConn(ib *inbound, nConn net.Conn) {
	// Load the config snapshot once so every read below sees a consistent view,
	// even if Apply hot-swaps a new snapshot mid-connection.
	icfg := ib.cfg.Load()

	cfg := &gossh.ServerConfig{
		MaxAuthTries: 6,
		PasswordCallback: func(cm gossh.ConnMetadata, pw []byte) (*gossh.Permissions, error) {
			a, ok := ib.accounts.authenticate(cm.User(), string(pw))
			if !ok {
				return nil, fmt.Errorf("authentication failed")
			}
			return &gossh.Permissions{Extensions: map[string]string{
				"email":  a.email,
				"klimit": strconv.Itoa(a.kLimit),
			}}, nil
		},
	}
	cfg.AddHostKey(icfg.signer)

	_ = nConn.SetDeadline(time.Now().Add(20 * time.Second))
	sc, chans, reqs, err := gossh.NewServerConn(nConn, cfg)
	if err != nil {
		_ = nConn.Close()
		return
	}
	_ = nConn.SetDeadline(time.Time{})

	email := sc.Permissions.Extensions["email"]
	kLimit, _ := strconv.Atoi(sc.Permissions.Extensions["klimit"])
	srcIP := hostOnly(sc.RemoteAddr().String())

	sess, evicted, ok := c.sessions.admit(email, srcIP, ib.id, kLimit, icfg.strategy, func() { _ = sc.Close() })
	if !ok {
		_ = sc.Close()
		return
	}
	for _, e := range evicted {
		if e.closeFn != nil {
			e.closeFn()
		}
	}
	defer func() {
		c.sessions.unregister(email, sess.id)
		_ = sc.Close()
	}()

	go gossh.DiscardRequests(reqs)

	// Hand the copy path the session's shared per-email counter so each chunk
	// adds atomically without the registry lock.
	ac := sess.account
	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" {
			_ = nc.Reject(gossh.Prohibited, "only direct-tcpip is permitted")
			continue
		}
		go c.handleDirectTCPIP(ac, nc)
	}
}

type directTCPIPMsg struct {
	HostToConnect  string
	PortToConnect  uint32
	OriginatorIP   string
	OriginatorPort uint32
}

func (c *Core) handleDirectTCPIP(ac *acct, nc gossh.NewChannel) {
	var msg directTCPIPMsg
	if err := gossh.Unmarshal(nc.ExtraData(), &msg); err != nil {
		_ = nc.Reject(gossh.ConnectionFailed, "bad direct-tcpip payload")
		return
	}
	dest := net.JoinHostPort(msg.HostToConnect, strconv.Itoa(int(msg.PortToConnect)))
	ctx, cancel := context.WithTimeout(context.Background(), egressTimeout)
	remote, err := c.dial(ctx, "tcp", dest)
	cancel()
	if err != nil {
		_ = nc.Reject(gossh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	go gossh.DiscardRequests(reqs)

	// ch (client) -> remote (dest) is uplink; remote -> ch is downlink.
	go func() {
		c.copyMetered(remote, ch, ac, true)
		_ = remote.Close()
		_ = ch.Close()
	}()
	c.copyMetered(ch, remote, ac, false)
	_ = ch.Close()
	_ = remote.Close()
}

// copyMetered proxies src->dst, tallying transferred bytes into ac (uplink when
// isUp, downlink otherwise). ac is the session's shared per-email counter; a nil
// ac disables accounting. The tally is a lockless atomic add, so this per-chunk
// hot path never contends on the session-registry mutex.
func (c *Core) copyMetered(dst io.Writer, src io.Reader, ac *acct, isUp bool) {
	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
			if ac != nil {
				if isUp {
					ac.up.Add(int64(n))
				} else {
					ac.down.Add(int64(n))
				}
			}
		}
		if rerr != nil {
			return
		}
	}
}

// Status implements core.Core.
func (c *Core) Status(ctx context.Context) (core.CoreStatus, error) {
	c.mu.Lock()
	running := len(c.inbounds) > 0
	c.mu.Unlock()
	return core.CoreStatus{
		Running:      running,
		LiveSessions: c.sessions.totalLive(),
	}, nil
}

// CollectTraffic implements core.Core: the per-email bytes since the last call.
func (c *Core) CollectTraffic(ctx context.Context) (*traffic.XrayStatsSnapshot, error) {
	return c.sessions.collect(), nil
}

// Sweep implements core.Core: kill sessions whose account is disabled. Device
// limit K is enforced at admit time.
func (c *Core) Sweep(ctx context.Context, d core.DesiredState) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ib := c.inbounds[d.InboundID]; ib != nil {
		c.killDisabledLocked(ib)
	}
	return nil
}

func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// Shutdown closes every listener and live session.
func (c *Core) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.inbounds {
		c.teardownLocked(id)
	}
	return nil
}
