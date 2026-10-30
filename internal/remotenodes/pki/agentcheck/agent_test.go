// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Both ends here are the code the plugin will meet in production, not a copy
// of its settings. The agent is internal/agent/server's Server, started the
// way cmd/node-agent starts it with TLS on, so its own TLS setup, its client
// check (the node-id rule and the serial denylist), InstallCertificate and
// UpdateCertDenylist are all in play. The hub's side is pkg/agent.NewClient,
// which is what the plugin dials with.
//
// The agent's TLS settings don't depend on its profile, so the managed
// profile (not in this tree yet) changes nothing here.

// The agent checks dates against the real clock, so these are real times. A
// CA made two years ago can sign a hub certificate that has run out by now.
var (
	now    = time.Now()
	twoYrs = now.AddDate(-2, 0, 0)
)

const (
	timeout = 5 * time.Second
	short   = 1500 * time.Millisecond // for a connection that is meant to fail
)

func quiet(t *testing.T) {
	prev := logrus.GetLevel()
	logrus.SetLevel(logrus.PanicLevel)
	t.Cleanup(func() { logrus.SetLevel(prev) })
}

func newCA(t *testing.T) *pki.CA {
	t.Helper()
	ca, err := pki.NewCA(twoYrs)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func nodeCert(t *testing.T, ca *pki.CA, id uint, name, host string, at time.Time) *pki.Issued {
	t.Helper()
	is, err := ca.IssueNodeCert(id, name, host, at)
	if err != nil {
		t.Fatal(err)
	}
	return is
}

func hubCert(t *testing.T, ca *pki.CA, at time.Time) *pki.Issued {
	t.Helper()
	is, err := ca.IssueHubClientCert(at)
	if err != nil {
		t.Fatal(err)
	}
	return is
}

type node struct {
	addr string
	cfg  *config.Config
}

func write(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// startAgent runs an agent on a loopback port, with the CA file and server
// certificate it would have on a node.
func startAgent(t *testing.T, caPEM []byte, cert *pki.Issued) *node {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.TLS = config.TLSConfig{
		Enabled:    true,
		CACert:     write(t, dir, "ca.crt", caPEM),
		ServerCert: write(t, dir, "server.crt", cert.CertPEM),
		ServerKey:  write(t, dir, "server.key", cert.KeyPEM),
	}
	// nothing outside the temp folder, and no Xray or log capture to start
	cfg.Process.ServiceMode = false
	cfg.Traffic.BufferEnabled = false
	cfg.Xray.AccessLogPath = ""
	cfg.Xray.BinaryPath = filepath.Join(dir, "xray")
	cfg.Xray.ConfigPath = filepath.Join(dir, "config.json")
	cfg.Cores.DataDir = filepath.Join(dir, "cores")

	for attempt := 0; attempt < 5; attempt++ {
		if n := tryStart(t, cfg); n != nil {
			return n
		}
	}
	t.Fatal("no free port to start the agent on")
	return nil
}

// tryStart starts the agent on a port that was free a moment ago. Start can't
// say which port it got, so one is asked for first, and another process may
// take it in between: Start then fails at once, and the caller tries again.
func tryStart(t *testing.T, cfg *config.Config) *node {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ListenAddr = l.Addr().String()
	l.Close()

	srv, err := server.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Start() }()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-done:
			if errors.Is(err, syscall.EADDRINUSE) {
				return nil
			}
			t.Fatalf("the agent stopped: %v", err)
		default:
		}
		if c, err := net.Dial("tcp", cfg.ListenAddr); err == nil {
			c.Close()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				srv.Stop(ctx)
			})
			return &node{addr: cfg.ListenAddr, cfg: cfg}
		}
	}
	t.Fatal("the agent never listened")
	return nil
}

// hub is what the plugin puts in agent.TLSConfig.
type hub struct {
	caPEM []byte
	cert  *pki.Issued
	// seen is the serial of the agent's certificate at the last handshake
	seen string
}

func (h *hub) tls() *agent.TLSConfig {
	return &agent.TLSConfig{
		CACert: string(h.caPEM), ClientCert: string(h.cert.CertPEM), ClientKey: string(h.cert.KeyPEM),
		VerifyLeaf: func(leaf *x509.Certificate) error { h.seen = leaf.SerialNumber.String(); return nil },
	}
}

// dial connects the way the plugin does and makes one call. The error says why
// a connection failed (WithReturnConnectionError), which is how the refusals
// below are told from a broken harness. target may be a name, if a dialer is
// passed to reach it.
func (h *hub) dial(target string, wait time.Duration, extra ...grpc.DialOption) (*agent.Client, error) {
	c, err := agent.NewClient(target, h.tls(), wait, append([]grpc.DialOption{grpc.WithReturnConnectionError()}, extra...)...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if _, err := c.GetVersion(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// A hub's own check fails the handshake, with the reason in the error. The
// agent's check of the hub comes after the client thinks the handshake is
// done (TLS 1.3), so the client sees the alert or a closed connection,
// whichever comes first. Those tests lean on a control: the same agent lets a
// good hub in.
var afterHandshake = []string{"remote error: tls:", "broken pipe", "connection reset", "EOF"}

// refuse wants the connection turned away, with one of the reasons in the error.
func (h *hub) refuse(t *testing.T, target string, reasons []string, extra ...grpc.DialOption) {
	t.Helper()
	c, err := h.dial(target, short, extra...)
	if err == nil {
		c.Close()
		t.Fatalf("it was let in, want a refusal (%q)", reasons)
	}
	for _, why := range reasons {
		if strings.Contains(err.Error(), why) {
			return
		}
	}
	t.Fatalf("refused, but not for any of %q: %v", reasons, err)
}

func TestTheAgentTakesTheHubByIPAddress(t *testing.T) {
	quiet(t)
	root := newCA(t)
	cert := nodeCert(t, root, 1, "Frankfurt", "127.0.0.1", now)
	a := startAgent(t, root.CertPEM(), cert)
	h := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}

	c, err := h.dial(a.addr, timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h.seen != cert.Serial {
		t.Fatalf("the hub saw serial %s, the node's certificate is %s", h.seen, cert.Serial)
	}
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTheAgentTakesTheHubByName(t *testing.T) {
	quiet(t)
	root := newCA(t)
	a := startAgent(t, root.CertPEM(), nodeCert(t, root, 2, "تهران", "node2.example.test", now))
	h := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}
	// the name has nowhere to resolve to here, so the dialer goes to the port
	viaPort := grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", a.addr)
	})
	_, port, _ := net.SplitHostPort(a.addr)

	c, err := h.dial("node2.example.test:"+port, timeout, viaPort)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	// a name the certificate doesn't carry is not the node
	h.refuse(t, "node3.example.test:"+port, []string{"valid for node2.example.test, not node3.example.test"}, viaPort)
	// and an address is not the name, since the certificate holds no IP
	h.refuse(t, a.addr, []string{"doesn't contain any IP SANs"})
}

func TestTheAgentTakesARenewedCertificate(t *testing.T) {
	quiet(t)
	root := newCA(t)
	a := startAgent(t, root.CertPEM(), nodeCert(t, root, 7, "old name", "127.0.0.1", now))
	h := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}

	c, err := h.dial(a.addr, timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	refused := func(what string, cert *pki.Issued) {
		t.Helper()
		_, err := c.InstallCertificate(ctx, &pb.InstallCertificateRequest{CertPem: cert.CertPEM, KeyPem: cert.KeyPEM})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v, want InvalidArgument", what, err)
		}
	}
	// the id is what InstallCertificate keeps: another node's certificate is turned away
	refused("a certificate for node 8", nodeCert(t, root, 8, "old name", "127.0.0.1", now))
	// and so is one from another CA, however well it is made
	refused("a certificate from another CA", nodeCert(t, newCA(t), 7, "old name", "127.0.0.1", now))
	// a hub certificate has no serverAuth, so it can't be a node's
	refused("the hub's certificate", h.cert)

	// renaming the node changes the CN's name and not its id, which is fine
	renewed := nodeCert(t, root, 7, "نام جدید", "127.0.0.1", now)
	resp, err := c.InstallCertificate(ctx, &pb.InstallCertificateRequest{CertPem: renewed.CertPEM, KeyPem: renewed.KeyPEM})
	if err != nil {
		t.Fatalf("a renewal for the same node: %v", err)
	}
	if resp.Serial != renewed.Serial || resp.NotAfterUnix != renewed.NotAfter.Unix() {
		t.Fatalf("the agent reports %s ending %d, the certificate is %s ending %d", resp.Serial, resp.NotAfterUnix, renewed.Serial, renewed.NotAfter.Unix())
	}
	written, err := os.ReadFile(a.cfg.TLS.ServerCert)
	if err != nil || string(written) != string(renewed.CertPEM) {
		t.Fatalf("the agent did not write the renewed certificate (%v)", err)
	}
	// a new connection meets the new certificate and still trusts it
	h2 := &hub{caPEM: root.CertPEM(), cert: h.cert}
	c2, err := h2.dial(a.addr, timeout)
	if err != nil {
		t.Fatalf("after the renewal: %v", err)
	}
	c2.Close()
	if h2.seen != renewed.Serial {
		t.Fatalf("a new connection met serial %s, want the renewed %s", h2.seen, renewed.Serial)
	}
}

func TestTheAgentRefusesWhatIsNotTheHub(t *testing.T) {
	quiet(t)
	root := newCA(t)
	cert := nodeCert(t, root, 1, "n", "127.0.0.1", now)
	a := startAgent(t, root.CertPEM(), cert)

	// the agent lets the hub in, so what follows is about the certificate
	good := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}
	c, err := good.dial(a.addr, timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	cases := []struct {
		name string
		cert *pki.Issued
	}{
		{"a hub certificate from another CA", hubCert(t, newCA(t), now)},
		{"a node's certificate as a client's", cert},
		{"a hub certificate that ran out", hubCert(t, root, twoYrs)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			(&hub{caPEM: root.CertPEM(), cert: c.cert}).refuse(t, a.addr, afterHandshake)
		})
	}

	// the denylist names a certificate by its serial, as a decimal
	t.Run("a denylisted serial", func(t *testing.T) {
		listed := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}
		spared := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}
		cl, err := listed.dial(a.addr, timeout)
		if err != nil {
			t.Fatal(err)
		}
		defer cl.Close()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := c.UpdateCertDenylist(ctx, []string{listed.cert.Serial}, "test"); err != nil {
			t.Fatal(err)
		}
		listed.refuse(t, a.addr, afterHandshake)
		// the next hub certificate has another serial, so it is not caught
		cs, err := spared.dial(a.addr, timeout)
		if err != nil {
			t.Fatalf("another hub certificate: %v", err)
		}
		cs.Close()
		// and the list is replaced, not added to
		if err := c.UpdateCertDenylist(ctx, nil, "empty"); err != nil {
			t.Fatal(err)
		}
		again, err := listed.dial(a.addr, timeout)
		if err != nil {
			t.Fatalf("after the list was emptied: %v", err)
		}
		again.Close()
	})
}

func TestTheHubRefusesWhatIsNotTheNode(t *testing.T) {
	quiet(t)
	root := newCA(t)
	stranger := newCA(t)
	cases := []struct {
		name string
		cert *pki.Issued
		why  string
	}{
		{"a certificate from another CA", nodeCert(t, stranger, 1, "n", "127.0.0.1", now), "signed by unknown authority"},
		{"a certificate for another address", nodeCert(t, root, 1, "n", "203.0.113.7", now), "valid for 203.0.113.7, not 127.0.0.1"},
		{"a certificate that ran out", nodeCert(t, root, 1, "n", "127.0.0.1", twoYrs), "certificate has expired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// the agent's own CA file is ours, and the same hub reaches a good agent in the other tests,
			// so only the hub's check can have turned this one away
			a := startAgent(t, root.CertPEM(), c.cert)
			h := &hub{caPEM: root.CertPEM(), cert: hubCert(t, root, now)}
			h.refuse(t, a.addr, []string{c.why})
		})
	}
}
