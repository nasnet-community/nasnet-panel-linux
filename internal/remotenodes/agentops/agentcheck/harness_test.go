// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

// The agent in these tests is internal/agent/server's Server, started the way
// cmd/node-agent starts it with TLS on, behind the certificates package pki
// issues, so its own TLS settings, its profile interceptors and its handlers
// are what answer. The hub's side is the agentops Env, which dials it the
// way the plugin does.

func quiet(t *testing.T) {
	prev := logrus.GetLevel()
	logrus.SetLevel(logrus.PanicLevel)
	t.Cleanup(func() { logrus.SetLevel(prev) })
}

func newCA(t *testing.T) *pki.CA {
	t.Helper()
	ca, err := pki.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// A fake Xray: a script that answers what the agent asks of the binary. It
// reports the version it is given, fails a config check when the config holds
// "reject.example" (so a test can make the node refuse a config, with a
// message), and otherwise runs until it is stopped.
func xrayScript(version string) string {
	return `#!/bin/sh
case "$1" in
version) echo "Xray ` + version + ` (Xray, Penetrates Everything.) test (go1.27 linux/amd64)" ;;
run) exec sleep 60 ;;
-test)
  if grep -q 'reject.example' "$3"; then
    echo "main: failed to load config files: infra/conf: the handshake target reject.example is not allowed" >&2
    exit 1
  fi ;;
esac
`
}

// rig is a real agent, the hub's Env and the node the Env knows it as.
type rig struct {
	t    *testing.T
	ca   *pki.CA
	env  *agentops.Env
	node model.Node
	cfg  *config.Config
	srv  *server.Server
	dir  string
}

type options struct {
	profile config.Profile
	version string // Xray's, as the fake reports it
	files   agentops.Files
	ca      *pki.CA // for the agent's certificate, when it isn't the hub's
}

func startRig(t *testing.T, o options) *rig {
	t.Helper()
	quiet(t)
	ca := newCA(t)
	if o.version == "" {
		o.version = "26.10.10"
	}
	agentCA := ca
	if o.ca != nil {
		agentCA = o.ca
	}
	node := model.Node{ID: 7, Name: "Frankfurt", Host: "127.0.0.1", SSHPort: 22, EntryPort: 443}
	cert, err := agentCA.IssueNodeCert(node.ID, node.Name, node.Host, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	write := func(name string, b []byte, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := config.DefaultConfig()
	cfg.Profile = o.profile
	cfg.TLS = config.TLSConfig{
		Enabled:    true,
		CACert:     write("ca.crt", agentCA.CertPEM(), 0o600),
		ServerCert: write("server.crt", cert.CertPEM, 0o600),
		ServerKey:  write("server.key", cert.KeyPEM, 0o600),
	}
	// nothing outside the temp folder
	cfg.Process.ServiceMode = false
	cfg.Process.WatchdogEnabled = false
	cfg.Traffic.BufferEnabled = false
	cfg.Xray.AccessLogPath = ""
	cfg.Xray.BinaryPath = write("xray", []byte(xrayScript(o.version)), 0o755)
	cfg.Xray.ConfigPath = filepath.Join(dir, "xray-conf", "config.json")
	cfg.Cores.DataDir = filepath.Join(dir, "cores")

	var srv *server.Server
	for attempt := 0; srv == nil; attempt++ {
		if attempt == 5 {
			t.Fatal("no free port to start the agent on")
		}
		srv = tryStart(t, cfg)
	}
	_, port, _ := net.SplitHostPort(cfg.ListenAddr)
	node.AgentPort, _ = strconv.Atoi(port)

	return &rig{
		t: t, ca: ca, node: node, cfg: cfg, srv: srv, dir: dir,
		env: &agentops.Env{
			Local: netip.MustParseAddr("127.0.0.1"), CA: ca, Files: o.files, DialTimeout: 3 * time.Second,
			Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		},
	}
}

// tryStart starts the agent on a port that was free a moment ago. Start can't
// say which port it got, so one is asked for first, and another process may
// take it in between: Start then fails at once, and the caller tries again.
func tryStart(t *testing.T, cfg *config.Config) *server.Server {
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
			return srv
		}
	}
	t.Fatal("the agent never listened")
	return nil
}
