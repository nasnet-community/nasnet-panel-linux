// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"crypto/tls"
	"os"
	"slices"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// A node set up end to end by the real agent: an Xray that reports the pinned
// version (so the push is not needed, which a test build can't take), handshake
// targets that are TLS servers on loopback, and the config written and run by
// the agent, with the entry answering.
func TestProvisionWithTheRealAgent(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, version: pin.Version})
	c := r.connect()
	r.startXray(c)
	r.node.EntryPort = standIn(t)

	small := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "small.example.test", 0)}, MinVersion: tls.VersionTLS13})
	bigger := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, rsaKey(t), "bigger.example.test", 20)}, MinVersion: tls.VersionTLS13})
	old := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "old.example.test", 0)}, MaxVersion: tls.VersionTLS12})

	in := agentops.ProvisionInput{
		Node:    r.node,
		Targets: []string{old, bigger, small},
		SNI:     "small.example.test",
		Clients: []nodecfg.Client{{UUID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Email: nodecfg.SubscriptionEmail(1)}},
		Bridge:  nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(7), ReverseTag: "r7"},
	}
	var steps []agentops.Step
	res, err := r.env.Provision(bg, c, in, func(s agentops.Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}

	if res.HandshakeTarget != small || res.SNI != "small.example.test" {
		t.Errorf("target %q (want %q), name %q", res.HandshakeTarget, small, res.SNI)
	}
	if res.XrayVersion != pin.Version || res.AgentVersion == "" {
		t.Errorf("versions agent %q, Xray %q", res.AgentVersion, res.XrayVersion)
	}
	keys := agentops.RealityKeys{PrivateKey: res.PrivateKey, PublicKey: res.PublicKey, ShortID: res.ShortID}
	if err := keys.Valid(); err != nil {
		t.Errorf("keys: %v", err)
	}
	if !res.Check.OK || !res.Check.TLS13 || !res.Check.X25519 {
		t.Errorf("check %+v", res.Check)
	}

	// the config on the node is the one built from the result
	want, err := nodecfg.Build(nodecfg.Input{
		EntryPort: r.node.EntryPort, RealityDest: res.HandshakeTarget, ServerNames: []string{res.SNI},
		PrivateKey: res.PrivateKey, ShortIDs: []string{res.ShortID}, Clients: in.Clients, Bridge: in.Bridge,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(r.cfg.Xray.ConfigPath); string(got) != string(want) {
		t.Error("the config on the node isn't what the result describes")
	}
	if st, err := c.GetStatus(bg); err != nil || !st.XrayRunning {
		t.Errorf("status %+v, %v", st, err)
	}
	// every step of the plan was done
	var done []string
	for _, s := range steps {
		if s.State == agentops.StateOK {
			done = append(done, s.Name)
		}
	}
	if !slices.Equal(done, agentops.ProvisionPlan()) {
		t.Errorf("steps done %q, want %q", done, agentops.ProvisionPlan())
	}
}

// A node whose Xray the install kept (the pinned build, checked by checksum over
// SSH) is provisioned without a build being fetched or sent, and the real agent
// is still asked what runs: Download Xray and Upload Xray are done as steps with
// nothing to do, Check Xray is a real one.
func TestProvisionOfAKeptBuildWithTheRealAgentSendsNothing(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged, version: pin.Version, files: build{plain: []byte("never sent")}})
	c := r.connect()
	r.startXray(c)
	r.node.EntryPort = standIn(t)
	small := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "small.example.test", 0)}, MinVersion: tls.VersionTLS13})
	in := agentops.ProvisionInput{
		Node: r.node, Targets: []string{small}, SNI: "small.example.test", XrayKept: true,
		Clients: []nodecfg.Client{{UUID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", Email: nodecfg.SubscriptionEmail(1)}},
		Bridge:  nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(7), ReverseTag: "r7"},
	}
	var steps []agentops.Step
	res, err := r.env.Provision(bg, c, in, func(s agentops.Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if res.XrayVersion != pin.Version {
		t.Errorf("the node reports Xray %q", res.XrayVersion)
	}
	last := map[string]agentops.Step{}
	began := map[string]bool{}
	for _, s := range steps {
		last[s.Name] = s
		began[s.Name] = began[s.Name] || s.State == agentops.StateRunning
	}
	for _, name := range []string{agentops.StepDownloadXray, agentops.StepUploadXray} {
		if last[name].State != agentops.StateOK || last[name].Detail != "Not needed" || began[name] {
			t.Errorf("%s: %+v (began %v)", name, last[name], began[name])
		}
	}
	// Check Xray looked at the node: it began, and says what runs
	if !began[agentops.StepCheckXray] || last[agentops.StepCheckXray].Detail != "Xray "+pin.Version {
		t.Errorf("Check Xray: %+v (began %v)", last[agentops.StepCheckXray], began[agentops.StepCheckXray])
	}
	if st, err := c.GetStatus(bg); err != nil || !st.XrayRunning {
		t.Errorf("status %+v, %v", st, err)
	}
}
