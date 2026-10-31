// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build live

package agentops

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// TestLiveLifecycle puts the agent on a real server and takes it away again:
// probe, install, the link, provision, renewal, uninstall. It changes the
// server, so it never runs unless asked, and it takes the agent off again
// whatever happens.
//
//	REMOTE_NODES_LIVE_HOST=203.0.113.7 REMOTE_NODES_LIVE_PASSWORD=... \
//	RELEASE_BASE_URL=http://127.0.0.1:8099 \
//	  go test -tags=live -count=1 -run TestLive -v \
//	    -ldflags "-X github.com/nasnet-community/nasnet-panel-linux/pkg/signing.extraKey=<the drill key's public half>" \
//	    ./internal/remotenodes/agentops
//
// The server needs a root login (or one that can use sudo, with the password
// given), systemd, and its agent and entry ports free. This machine has to
// reach the agent's port and the entry port, which is the check the router
// makes: a firewall that blocks them fails the install, as it should.
//
//	REMOTE_NODES_LIVE_HOST      the server (an address or a name); nothing runs without it
//	REMOTE_NODES_LIVE_SSH_PORT  default 22
//	REMOTE_NODES_LIVE_USER      default root
//	REMOTE_NODES_LIVE_PASSWORD  its password, which sudo is given too
//	REMOTE_NODES_LIVE_KEY       a file with a private key, instead of a password
//	REMOTE_NODES_LIVE_AGENT_PORT, REMOTE_NODES_LIVE_ENTRY_PORT   default 9090 and 8443
//	REMOTE_NODES_LIVE_LOCAL     the address to connect from; default the system's choice
//	RELEASE_BASE_URL            a release mirror, until the project's own has the agent
//	                            this plugin needs. Its files are checked with the keys this
//	                            build trusts, so a mirror of a drill release needs the
//	                            ldflag above. Xray comes from its own releases, checked
//	                            against the pin.
func TestLiveLifecycle(t *testing.T) {
	host := os.Getenv("REMOTE_NODES_LIVE_HOST")
	if host == "" {
		t.Skip("set REMOTE_NODES_LIVE_HOST (and REMOTE_NODES_LIVE_PASSWORD) to try a real server; see the comment on this test")
	}
	login := Login{Password: os.Getenv("REMOTE_NODES_LIVE_PASSWORD")}
	if p := os.Getenv("REMOTE_NODES_LIVE_KEY"); p != "" {
		key, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		login.PrivateKey = string(key)
	}
	if login.Password == "" && login.PrivateKey == "" {
		t.Fatal("set REMOTE_NODES_LIVE_PASSWORD or REMOTE_NODES_LIVE_KEY")
	}
	var local netip.Addr
	if s := os.Getenv("REMOTE_NODES_LIVE_LOCAL"); s != "" {
		var err error
		if local, err = netip.ParseAddr(s); err != nil {
			t.Fatal(err)
		}
	}
	src, err := artifacts.SourceFromEnv(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}

	ca, err := pki.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	holder := artifacts.NewHolder(artifacts.NewClient(local), src)
	defer holder.Release()
	env := &Env{Local: local, CA: ca, Files: holder}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	progress := func(s Step) { t.Logf("%-28s %-8s %s", s.Name, s.State, s.Detail) }

	fp, err := env.Probe(ctx, host, liveInt("REMOTE_NODES_LIVE_SSH_PORT", 22))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("host key %s", fp)
	node := model.Node{
		ID: 1, Name: "live test", Host: host, SSHPort: liveInt("REMOTE_NODES_LIVE_SSH_PORT", 22), SSHUser: liveEnv("REMOTE_NODES_LIVE_USER", "root"),
		AgentPort: liveInt("REMOTE_NODES_LIVE_AGENT_PORT", 9090), EntryPort: liveInt("REMOTE_NODES_LIVE_ENTRY_PORT", 8443), SSHHostKey: fp,
	}

	// never leave a half-installed server behind
	removed := false
	defer func() {
		if !removed {
			if err := env.Uninstall(context.Background(), node, login, progress); err != nil {
				t.Errorf("the cleanup failed, and the server may still have the agent: %v", err)
			}
		}
	}()

	res, err := env.InstallAgent(ctx, node, login, InstallOptions{}, progress)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Logf("installed: %s, %s, agent %s, certificate until %s, repaired %v", res.Arch, res.OS, res.AgentVersion, res.CertNotAfter.Format(time.DateOnly), res.Repaired)
	node.Arch, node.AgentVersion = res.Arch, res.AgentVersion

	c, err := env.Connect(ctx, node)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	pr, err := env.Provision(ctx, c, ProvisionInput{
		Node:    node,
		Clients: []nodecfg.Client{{UUID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Email: nodecfg.SubscriptionEmail(1)}},
		Bridge:  nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(1), ReverseTag: "bridge-1"},
	}, progress)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	t.Logf("provisioned: target %s (%d bytes, %d ms), Xray %s", pr.HandshakeTarget, pr.Check.HandshakeBytes, pr.Check.LatencyMs, pr.XrayVersion)
	if pr.XrayVersion != pin.Version {
		t.Errorf("the node runs Xray %q, the pin is %s", pr.XrayVersion, pin.Version)
	}

	if st, err := c.GetStatus(ctx); err != nil || !st.XrayRunning {
		t.Errorf("status %+v, %v", st, err)
	}
	if chk, err := env.CheckTarget(ctx, c, pr.HandshakeTarget); err != nil || !chk.OK {
		t.Errorf("the chosen target again: %+v, %v", chk, err)
	}
	cert, err := env.RenewCert(ctx, c, node)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	t.Logf("renewed: serial %s until %s", cert.Serial, cert.NotAfter.Format(time.DateOnly))

	// and away again
	c.Close()
	if err := env.Uninstall(ctx, node, login, progress); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	removed = true
	if _, err := env.Connect(ctx, node); !errors.Is(err, ErrUnreachable) {
		t.Errorf("after the uninstall the agent still answers, or fails some other way: %v", err)
	}
}

func liveEnv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func liveInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
