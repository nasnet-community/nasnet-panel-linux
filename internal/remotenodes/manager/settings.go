// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

var (
	errNodeGone = errors.New("the node was removed while this was going on")
	errNoKey    = errors.New("This node has no REALITY key on record, so its configuration can't be built. Remove the node and add it again.")
	errNoBridge = errors.New("This node has no client id for the router's bridge, so its configuration can't be built. Remove the node and add it again.")
)

// RenameNode gives a node another name. Names are compared the way a person
// would, so one that differs from another node's only in case is taken. The
// node's certificate keeps the name it was made with until it is renewed,
// since what a node is known by in it is its id.
func (m *Manager) RenameNode(_ context.Context, id uint, name string) error {
	name, err := model.CleanName(name)
	if err != nil {
		return err
	}
	return m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil {
			return notFound("node", id)
		}
		for _, o := range s.Nodes {
			if o.ID != id && strings.EqualFold(o.Name, name) {
				return fmt.Errorf("%w: %q", model.ErrNameTaken, name)
			}
		}
		n.Name = name
		return nil
	})
}

// inputFor is the config a set-up node should run now: its own REALITY values
// and the clients the state switches on there.
func inputFor(st model.State, n model.Node) (nodecfg.Input, error) {
	switch {
	case n.RealityPrivateKey == "":
		return nodecfg.Input{}, errNoKey
	case n.BridgeUUID == "":
		return nodecfg.Input{}, errNoBridge
	}
	return nodecfg.Input{
		EntryPort: n.EntryPort, RealityDest: n.RealityTarget, ServerNames: []string{serverName(n)},
		PrivateKey: n.RealityPrivateKey, ShortIDs: []string{n.RealityShortID},
		Clients: clientsOf(st, n.ID), Bridge: bridgeOf(n),
	}, nil
}

// settingsChange is what a request for new settings comes to.
type settingsChange struct {
	entryPort int
	target    string
	sni       string
	// targetMoved is whether the handshake target is another site than it
	// was, which is when the node is asked whether it can use it.
	targetMoved bool
}

// planSettings works out the settings a request makes of a node, and refuses
// ones that can't be.
//
// A server name that isn't given is what it was, except that a name that was
// the target's host (which is what it is unless a person chose another) follows
// the target to its new host. An empty one says to follow it.
func planSettings(n model.Node, req api.SettingsRequest) (settingsChange, error) {
	if req.EntryPort == nil && req.HandshakeTarget == nil && req.SNI == nil {
		return settingsChange{}, invalidf("there is nothing to change")
	}
	ch := settingsChange{entryPort: n.EntryPort, target: n.RealityTarget, sni: n.RealitySNI}
	if req.EntryPort != nil {
		if *req.EntryPort < 1 || *req.EntryPort > 65535 {
			return settingsChange{}, invalidf("the entry port must be between 1 and 65535")
		}
		ch.entryPort = *req.EntryPort
	}
	custom := n.RealitySNI != "" && n.RealitySNI != hostOf(n.RealityTarget)
	if req.HandshakeTarget != nil {
		target, err := cleanTarget(*req.HandshakeTarget)
		if err != nil {
			return settingsChange{}, err
		}
		ch.target, ch.targetMoved = target, target != n.RealityTarget
	}
	switch {
	case req.SNI != nil && *req.SNI != "":
		sni, err := cleanSNI(*req.SNI)
		if err != nil {
			return settingsChange{}, err
		}
		ch.sni = sni
	case req.SNI != nil, ch.targetMoved && !custom:
		ch.sni = hostOf(ch.target)
		if _, err := netip.ParseAddr(ch.sni); err == nil {
			return settingsChange{}, invalidf("the handshake target is an address, so the server name has to be given as well")
		}
	}
	// the same checks the state would make, before the node is touched
	cand := n
	cand.EntryPort, cand.RealityTarget, cand.RealitySNI = ch.entryPort, ch.target, ch.sni
	if err := cand.Validate(); err != nil {
		return settingsChange{}, err
	}
	if ch.entryPort == n.EntryPort && ch.target == n.RealityTarget && ch.sni == n.RealitySNI {
		return settingsChange{}, invalidf("those are the node's settings already")
	}
	return ch, nil
}

// ChangeSettings puts new settings on a node's entry: its port, the site its
// handshake borrows, the server name. It starts a job, which gives the id.
// Every link the node has carries these, so every link changes.
//
// The values are checked, and the node has to be set up, free of other jobs
// and reachable, before the job starts. The job has the node try a new handshake
// target, pushes the config built from the state, and only when the node
// runs it saves the settings. A push that fails after the node may have taken
// it is followed by one that puts the old config back, so a change that is
// refused (the entry port is shut at the server's firewall, say) leaves the
// node as it was.
func (m *Manager) ChangeSettings(_ context.Context, id uint, req api.SettingsRequest) (string, error) {
	n, err := m.settled(id)
	if err != nil {
		return "", err
	}
	ch, err := planSettings(n, req)
	if err != nil {
		return "", err
	}
	ops, err := m.operate()
	if err != nil {
		return "", err
	}
	if _, err := m.link(n); err != nil {
		return "", err
	}
	steps := []string{agentops.StepApplyConfig, StepSaveNode}
	if ch.targetMoved {
		steps = append([]string{agentops.StepChooseTarget}, steps...)
	}
	return m.startJob(jobs.Spec{
		Kind: jobs.KindChangeSettings, NodeID: id, Target: n.Name, Steps: steps, Secrets: []string{n.RealityPrivateKey},
	}, func(ctx context.Context, r *jobs.Run) error { return m.runSettings(ctx, r, ops, id, ch) })
}

func (m *Manager) runSettings(ctx context.Context, r *jobs.Run, ops Ops, id uint, ch settingsChange) error {
	if ch.targetMoved {
		n, err := m.nodeFor(id)
		if err != nil {
			return err
		}
		c, err := m.link(n)
		if err != nil {
			return err
		}
		r.Begin(agentops.StepChooseTarget)
		chk, err := ops.CheckTarget(ctx, c, ch.target)
		if err == nil && !chk.OK {
			err = &agentops.NoTargetError{Results: []agentops.TargetResult{{Target: ch.target, Check: chk}}}
		}
		if err != nil {
			r.Fail(agentops.StepChooseTarget, err)
			return err
		}
		r.Detail(agentops.StepChooseTarget, ch.target)
		r.Done(agentops.StepChooseTarget)
	}

	p, err := m.beginPush(ctx, id)
	if err != nil {
		return err
	}
	defer p.unlock()
	next := p.old
	next.EntryPort, next.RealityDest, next.ServerNames = ch.entryPort, ch.target, []string{ch.sni}
	r.Begin(agentops.StepApplyConfig)
	if err := m.pushOrUndo(ctx, ops, p.link, p.old, next); err != nil {
		r.Fail(agentops.StepApplyConfig, err)
		return err
	}
	r.Done(agentops.StepApplyConfig)

	r.Begin(StepSaveNode)
	err = m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil {
			return errNodeGone
		}
		n.EntryPort, n.RealityTarget, n.RealitySNI = ch.entryPort, ch.target, ch.sni
		return nil
	})
	if err != nil {
		err = m.restore(ctx, ops, p.link, p.old, err)
		r.Fail(StepSaveNode, err)
		return err
	}
	// the bridge dials the new port and name
	m.Sync()
	r.Done(StepSaveNode)
	m.log.Info("a node's settings changed", "node", id, "entry_port", ch.entryPort, "target", ch.target, "sni", ch.sni)
	return nil
}

// push is a node's turn at being pushed a config, with what the push needs.
type push struct {
	node   model.Node
	link   Client
	old    nodecfg.Input // what the node runs now, as the state makes it
	unlock func()
}

// beginPush takes the node's turn at having its clients changed, and reads
// under it what the node should be running. A config replaces every client a
// node has, so a subscription switched on while one is pushed would be lost to
// it, and the node's loop would have to put it back. Under the node's lock a
// switch comes before the config is read, and so is in it, or after the push,
// and so is added to the node that runs it. The caller lets go with unlock once
// it has pushed and saved, or given up.
func (m *Manager) beginPush(ctx context.Context, id uint) (*push, error) {
	unlock, err := m.lockNodes(ctx, id)
	if err != nil {
		return nil, err
	}
	st := m.cfg.Store.Read()
	n := st.FindNode(id)
	if n == nil {
		unlock()
		return nil, errNodeGone
	}
	c, err := m.link(*n)
	if err != nil {
		unlock()
		return nil, err
	}
	old, err := inputFor(st, *n)
	if err != nil {
		unlock()
		return nil, err
	}
	// the push restarts Xray
	done := m.expectRestarts(id)
	return &push{node: *n, link: c, old: old, unlock: func() { done(); unlock() }}, nil
}

// pushOrUndo pushes a config to a node. When the push fails after the node may
// have taken the config, the old one is pushed back.
func (m *Manager) pushOrUndo(ctx context.Context, ops Ops, c Client, old, next nodecfg.Input) error {
	err := ops.PushConfig(ctx, c, next)
	if err == nil || untouched(err) || ctx.Err() != nil {
		return err
	}
	return m.restore(ctx, ops, c, old, err)
}

// untouched is whether an error from a push says the node still has the config
// it had: it was refused before it was applied.
func untouched(err error) bool {
	return errors.Is(err, agentops.ErrConfigRejected) || errors.Is(err, nodecfg.ErrInvalid) || errors.Is(err, agentops.ErrAgentOutdated)
}

// restore puts a node's old config back after a change that couldn't be kept,
// and says so with the cause: a node that runs what the state has no record of
// can't be reached by any of the links the state makes.
func (m *Manager) restore(ctx context.Context, ops Ops, c Client, old nodecfg.Input, cause error) error {
	if ctx.Err() != nil {
		return cause
	}
	if err := ops.PushConfig(ctx, c, old); err != nil {
		m.log.Warn("a node's old configuration could not be put back", "err", without(err.Error(), old.PrivateKey))
		return &putBack{cause, "The previous settings could not be put back either, so the node may still run the new ones."}
	}
	m.log.Info("a node's old configuration was put back")
	return &putBack{cause, "The node is back on its previous settings."}
}

// putBack is the cause of a change that failed, and how the node was left.
type putBack struct {
	cause error
	note  string
}

func (e *putBack) Error() string { return strings.TrimSpace(e.cause.Error()) + " " + e.note }
func (e *putBack) Unwrap() error { return e.cause }

// without is a text with the secrets taken out, for a log line.
func without(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "…")
		}
	}
	return s
}

// CheckHandshake has a node try a handshake target and says what it found:
// that it answered with TLS 1.3 and an X25519 key exchange, how large its side
// of the handshake was, and how long it took. Only the node can say, since a
// site may answer from one server and not from another.
func (m *Manager) CheckHandshake(ctx context.Context, id uint, target string) (api.HandshakeCheck, error) {
	n, err := m.nodeFor(id)
	if err != nil {
		return api.HandshakeCheck{}, err
	}
	if n.State != model.NodeRegistered {
		return api.HandshakeCheck{}, neverInstalled()
	}
	target, err = cleanTarget(target)
	if err != nil {
		return api.HandshakeCheck{}, err
	}
	ops, err := m.operate()
	if err != nil {
		return api.HandshakeCheck{}, err
	}
	c, err := m.link(n)
	if err != nil {
		return api.HandshakeCheck{}, err
	}
	got, err := ops.CheckTarget(ctx, c, target)
	if err != nil {
		return api.HandshakeCheck{}, opFailure(err)
	}
	return api.HandshakeCheck{
		OK: got.OK, TLS13: got.TLS13, X25519: got.X25519, HandshakeBytes: got.HandshakeBytes, LatencyMs: got.LatencyMs, Error: got.Error,
	}, nil
}

// RotateKeys gives a node's entry a new REALITY key pair and short id, which
// changes every link it has, since both are in them. It starts a job, which
// gives the id. The node is pushed the config with the new values, and only
// when it runs them are they saved, and the bridge, which logs in with them
// too, is told. A push that fails after the node may have taken the config is
// followed by one that puts the old one back.
func (m *Manager) RotateKeys(_ context.Context, id uint) (string, error) {
	n, err := m.settled(id)
	if err != nil {
		return "", err
	}
	ops, err := m.operate()
	if err != nil {
		return "", err
	}
	if _, err := m.link(n); err != nil {
		return "", err
	}
	keys, err := agentops.NewRealityKeys()
	if err != nil {
		return "", err
	}
	return m.startJob(jobs.Spec{
		Kind: jobs.KindRotateKeys, NodeID: id, Target: n.Name,
		Steps:   []string{agentops.StepCreateKeys, agentops.StepApplyConfig, StepSaveNode},
		Secrets: []string{n.RealityPrivateKey, keys.PrivateKey},
	}, func(ctx context.Context, r *jobs.Run) error { return m.runRotate(ctx, r, ops, id, keys) })
}

func (m *Manager) runRotate(ctx context.Context, r *jobs.Run, ops Ops, id uint, keys agentops.RealityKeys) error {
	// they are made before the job starts, which is how they come to be hidden from it
	r.Begin(agentops.StepCreateKeys)
	if err := keys.Valid(); err != nil {
		r.Fail(agentops.StepCreateKeys, err)
		return err
	}
	r.Done(agentops.StepCreateKeys)

	p, err := m.beginPush(ctx, id)
	if err != nil {
		return err
	}
	defer p.unlock()
	next := p.old
	next.PrivateKey, next.ShortIDs = keys.PrivateKey, []string{keys.ShortID}
	r.Begin(agentops.StepApplyConfig)
	if err := m.pushOrUndo(ctx, ops, p.link, p.old, next); err != nil {
		r.Fail(agentops.StepApplyConfig, err)
		return err
	}
	r.Done(agentops.StepApplyConfig)

	r.Begin(StepSaveNode)
	err = m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil {
			return errNodeGone
		}
		n.RealityPrivateKey, n.RealityPublicKey, n.RealityShortID = keys.PrivateKey, keys.PublicKey, keys.ShortID
		return nil
	})
	if err != nil {
		err = m.restore(ctx, ops, p.link, p.old, err)
		r.Fail(StepSaveNode, err)
		return err
	}
	m.Sync()
	m.Event(id, EventKeysRotated, "")
	r.Done(StepSaveNode)
	m.log.Info("a node's keys were rotated", "node", id)
	return nil
}
