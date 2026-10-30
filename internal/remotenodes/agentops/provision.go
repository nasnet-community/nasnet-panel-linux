// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"net"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// ProvisionInput is what a new node needs to become an entry: the node, the
// subscriptions already on it (none for a new one), and the router's bridge.
type ProvisionInput struct {
	// Node's ID, Name, Host and EntryPort are used, and Arch when the agent
	// doesn't say.
	Node model.Node

	// Target is a handshake target to use as it is: host or host:port. It is
	// checked, and the node isn't set up if it doesn't work. Empty means one is
	// chosen from Targets.
	Target string
	// Targets are the candidates to choose from; none means DefaultTargets.
	Targets []string
	// SNI is the name in the hello. Empty means the target's host, which an
	// address can't be, so for one the name has to be given.
	SNI string

	// Keys are kept keys, for putting a node in order without changing its
	// links. Zero means new ones.
	Keys RealityKeys

	Clients []nodecfg.Client // the subscriptions on this node, one client each
	Bridge  nodecfg.Bridge   // the client the router's bridge logs in as
}

// ProvisionResult is what the manager keeps of a node that is now an entry.
type ProvisionResult struct {
	HandshakeTarget string // host:port
	SNI             string
	PublicKey       string
	PrivateKey      string
	ShortID         string
	AgentVersion    string
	XrayVersion     string
	// Check is what the node found out about the target it uses.
	Check HandshakeCheck
}

// Provision makes a node with an agent into an entry, over the agent's link:
// the pinned Xray is put on it, a handshake target is chosen or checked from
// the node itself, the keys are made, and the config is pushed, with Xray
// running and the entry answering. It is the second half of adding a node,
// after InstallAgent, and the steps run in the order of ProvisionPlan.
//
// It stops before it changes anything if the input can't make a config, or
// the keys it was given don't pair up, or no target works from the node. A
// node that already has the pinned Xray is not sent it again.
func (e *Env) Provision(ctx context.Context, c *agent.Client, in ProvisionInput, progress Progress) (_ ProvisionResult, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	st := newStepper(progress)
	node := in.Node
	if err := e.checkProvision(in); err != nil {
		return ProvisionResult{}, err
	}

	// Xray comes first: the target is judged by what the node can do, and the
	// config needs the build that will read it. A new node has no config for
	// Xray to start with, so it is not started here.
	if err := e.pushXray(ctx, c, node, false, st); err != nil {
		return ProvisionResult{}, err
	}

	var res ProvisionResult
	done := st.begin(StepChooseTarget)
	target, check, err := e.chooseTarget(ctx, c, in)
	if err != nil {
		return res, done("", err)
	}
	sni, err := sniFor(target, in.SNI)
	if err != nil {
		return res, done("", err)
	}
	res.HandshakeTarget, res.SNI, res.Check = target, sni, check
	done(target, nil)

	done = st.begin(StepCreateKeys)
	keys := in.Keys
	if keys.IsZero() {
		if keys, err = NewRealityKeys(); err != nil {
			return res, done("", err)
		}
	}
	res.PublicKey, res.PrivateKey, res.ShortID = keys.PublicKey, keys.PrivateKey, keys.ShortID
	done("", nil)

	done = st.begin(StepApplyConfig)
	if err := e.PushConfig(ctx, c, nodecfg.Input{
		EntryPort: node.EntryPort, RealityDest: target, ServerNames: []string{sni},
		PrivateKey: keys.PrivateKey, ShortIDs: []string{keys.ShortID}, Clients: in.Clients, Bridge: in.Bridge,
	}); err != nil {
		return res, done("", err)
	}
	v, err := e.versions(ctx, c, node)
	if err != nil {
		return res, done("", err)
	}
	res.AgentVersion, res.XrayVersion = v.Agent, v.Xray
	done("Xray "+v.Xray+" runs, and the entry answers on port "+strconv.Itoa(node.EntryPort), nil)
	e.log().Info("provisioned the node", "node", node.ID, "host", node.Host, "target", target, "xray", v.Xray)
	return res, nil
}

// checkProvision refuses an input before anything is sent: a config can be
// made of it, the keys pair up, and the target and name are the sort that go
// together.
func (e *Env) checkProvision(in ProvisionInput) error {
	if err := model.ValidPort(in.Node.EntryPort); err != nil {
		return err
	}
	if !in.Keys.IsZero() {
		if err := in.Keys.Valid(); err != nil {
			return err
		}
	}
	if in.SNI != "" {
		if h, err := model.CleanHost(in.SNI); err != nil || net.ParseIP(h) != nil {
			return &model.InvalidError{Msg: "the name in the hello must be a host name, not an address"}
		}
	}
	if in.Target != "" {
		host, _, err := parseTarget(in.Target)
		if err != nil {
			return err
		}
		if _, err := sniFor(net.JoinHostPort(host, "443"), in.SNI); err != nil {
			return err
		}
	}
	// a dry run of the config, with a stand-in for what is made later, so that a
	// client or bridge that can't go in it is found before the node is touched
	_, err := nodecfg.Build(nodecfg.Input{
		EntryPort: in.Node.EntryPort, RealityDest: "www.example.com:443", ServerNames: []string{"www.example.com"},
		PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", ShortIDs: []string{"00"}, Clients: in.Clients, Bridge: in.Bridge,
	})
	return err
}

// chooseTarget is the target the node will use and what the node found out
// about it: the one it was told to use, if it works, or the best of the
// candidates.
func (e *Env) chooseTarget(ctx context.Context, c *agent.Client, in ProvisionInput) (string, HandshakeCheck, error) {
	if in.Target != "" {
		host, port, err := parseTarget(in.Target)
		if err != nil {
			return "", HandshakeCheck{}, err
		}
		check, err := checkTarget(ctx, c, host, port)
		if err != nil {
			return "", HandshakeCheck{}, err
		}
		target := net.JoinHostPort(host, strconv.Itoa(port))
		if !check.OK {
			return "", HandshakeCheck{}, &NoTargetError{Results: []TargetResult{{Target: target, Check: check}}}
		}
		return target, check, nil
	}
	choice, err := e.ProbeTargets(ctx, c, in.Targets)
	if err != nil {
		return "", HandshakeCheck{}, err
	}
	if choice.Best == "" {
		return "", HandshakeCheck{}, &NoTargetError{Results: choice.Results}
	}
	for _, r := range choice.Results {
		if r.Target == choice.Best {
			return choice.Best, r.Check, nil
		}
	}
	return choice.Best, HandshakeCheck{}, nil // not reached: the best is one of the results
}

// sniFor is the name in the hello: the one asked for, or the target's host.
func sniFor(target, asked string) (string, error) {
	if asked != "" {
		h, err := model.CleanHost(asked)
		if err != nil {
			return "", err
		}
		return h, nil
	}
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) != nil || strings.Contains(host, ":") {
		return "", &model.InvalidError{Msg: "the target is an address, so the name in the hello has to be given"}
	}
	return host, nil
}
