// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// installSteps are the steps of an install, in order: the agent over SSH, the
// link to it, the Xray and the entry over that link, and the node saved. An
// install that is to take over the server's own Xray has the step for it.
func installSteps(opts agentops.InstallOptions) []string {
	return slices.Concat(agentops.InstallPlanFor(opts), []string{StepConnect}, agentops.ProvisionPlan(), []string{StepSaveNode})
}

var errRemovedMidInstall = errors.New("the node was removed while it was being installed")

// AddNode saves a node whose install has not finished and starts the job that
// finishes it, which gives the job's id.
//
// The node is saved first, as installing, with the key the person agreed to
// pinned and the id its bridge logs in with, so that it has a page and a job
// from the first step on. A job that fails takes off the server what the
// install put there, with the login it still holds, and then forgets the
// node, so a failed install leaves no node behind (see afterFailure). Only
// when that cleanup fails too is the record kept, as an unfinished install.
//
// A node is added only when the router has room, when no other node or
// unfinished install is on that server, when no other node has the name (the
// way a person would compare names, so "Berlin" and "berlin" are one), and
// while there are fewer than model.MaxUnfinished unfinished installs. The
// login is used by the job and never kept, and every secret in it, and the
// REALITY key the job makes, is taken out of whatever the job shows. The job
// carries the rest of what was asked, so that the page can offer the same
// install again, but for the one thing that is asked again each time: that the
// server's own Xray may be taken over (req.TakeOverXray) is not in the form it
// carries.
//
// A Reality target that is asked for (req.HandshakeTarget) is read the way the
// settings call reads one, before anything is saved, and the install uses it as
// it is: the node checks it at "Choose a Reality target" and the install fails
// there if the node can't use it. Left out, the node chooses one.
func (m *Manager) AddNode(ctx context.Context, req api.InstallRequest) (string, error) {
	name, err := model.CleanName(req.Name)
	if err != nil {
		return "", err
	}
	host, err := model.CleanHost(req.Host)
	if err != nil {
		return "", err
	}
	lg, err := cleanLogin(req.Login)
	if err != nil {
		return "", err
	}
	target, err := cleanInstallTarget(req.HandshakeTarget)
	if err != nil {
		return "", err
	}
	ops, err := m.operate()
	if err != nil {
		return "", err
	}
	if m.cfg.Jobs == nil {
		return "", &api.Error{Code: api.CodeInternal, Msg: "This hub isn't set up to run jobs."}
	}
	keys, err := agentops.NewRealityKeys()
	if err != nil {
		return "", err
	}
	bridgeID, err := newClientID() // the bridge is a client of the entry like any other
	if err != nil {
		return "", err
	}

	var node model.Node
	err = m.cfg.Store.Update(func(s *model.State) error {
		// the limit is the answer when there is no room, whatever else is wrong
		if s.NodeCount() < model.MaxNodes {
			if err := m.checkNew(*s, name, host); err != nil {
				return err
			}
			if s.UnfinishedCount() >= model.MaxUnfinished {
				return badRequest(fmt.Sprintf("There are %d installs that didn't finish. Clean one up or forget it first.", s.UnfinishedCount()))
			}
		}
		entry := req.EntryPort
		if entry == 0 {
			entry = s.Settings.EntryPort
		}
		n, err := s.AddNode(model.Node{
			Name: name, Host: host, SSHPort: lg.port, SSHUser: lg.user, AgentPort: s.Settings.AgentPort, EntryPort: entry,
			State: model.NodeInstalling, CreatedAt: m.now(), SSHHostKey: lg.hostKey, BridgeUUID: bridgeID,
		})
		node = n
		return err
	})
	if err != nil {
		return "", err
	}

	opts := agentops.InstallOptions{TakeOverXray: req.TakeOverXray}
	spec := jobs.Spec{
		Kind: jobs.KindInstall, NodeID: node.ID, Target: node.Name, Steps: installSteps(opts),
		Secrets: lg.secrets(keys.PrivateKey),
		Install: &jobs.InstallForm{
			Name: node.Name, Host: node.Host, SSHPort: node.SSHPort, SSHUser: node.SSHUser, EntryPort: node.EntryPort,
			HandshakeTarget: target,
		},
	}
	id, err := m.startJob(spec, m.usesDownloads(func(ctx context.Context, r *jobs.Run) error {
		return m.runInstall(ctx, r, ops, node, lg, keys, opts, target)
	}))
	if err != nil {
		// no job will ever finish this one, so it isn't left for a person to find
		if rerr := m.cfg.Store.Update(func(s *model.State) error { return s.RemoveNode(node.ID) }); rerr != nil {
			m.log.Error("a node whose install could not be started is still in the state", "node", node.ID, "err", rerr)
		}
		return "", err
	}
	m.log.Info("adding a node", "node", node.ID, "name", node.Name, "host", node.Host, "job", id)
	return id, nil
}

// checkNew refuses a name that a node has already, and a server that a node
// or an unfinished install is on. An unfinished install's name is free: it is
// no node, and the person may well want the name for the next try.
func (m *Manager) checkNew(s model.State, name, host string) error {
	for _, o := range s.Nodes {
		switch {
		case strings.EqualFold(o.Name, name) && !o.Unfinished():
			return fmt.Errorf("%w: %q", model.ErrNameTaken, name)
		case o.Host == host:
			if err := m.idle(o.ID); err != nil {
				return err // an install to it is still going
			}
			if o.State != model.NodeRegistered {
				return badRequest("An install on that server didn't finish. Clean it up or forget it on the Nodes tab first.")
			}
			return badRequest("That server is already a node here.")
		}
	}
	return nil
}

// runInstall is the install job: the agent over SSH, then everything else over
// the agent's own link, and only when all of it has worked, the node saved as
// set up. One that fails is cleaned up after (afterFailure), so the job ends
// with no node or with an unfinished install, never with a node that only
// looks like one.
func (m *Manager) runInstall(ctx context.Context, r *jobs.Run, ops Ops, node model.Node, lg login, keys agentops.RealityKeys, opts agentops.InstallOptions, target string) (err error) {
	settled := false
	defer func() {
		// a panic ends the job, and the record mustn't go on looking like a running install
		if !settled {
			m.keepUnfinished(node.ID, model.InstallFailure{Error: "The install stopped on an internal error.", At: m.now().UTC().Truncate(time.Second)})
		}
	}()
	did, err := m.install(ctx, r, ops, node, lg.creds, keys, opts, target)
	if err != nil {
		err = m.afterFailure(ctx, r, ops, node, lg.creds, did, err, lg.secrets(keys.PrivateKey))
	}
	settled = true
	return err
}

// installed is what an install had done to the server by the time it ended.
type installed struct {
	// changed: the server may have been changed. The agent's install says so of its
	// own steps, and after it everything has.
	changed bool
	// tookOver: the server's own Xray was stopped and taken away, which nothing puts back.
	tookOver bool
}

// install is runInstall's work, and says how far it got on the server. A
// target that is given is the node's Reality target as it is, or the install
// fails where the node checks it; empty, the node chooses.
func (m *Manager) install(ctx context.Context, r *jobs.Run, ops Ops, node model.Node, creds agentops.Login, keys agentops.RealityKeys, opts agentops.InstallOptions, target string) (did installed, err error) {
	progress := progressTo(r)
	res, err := ops.InstallAgent(ctx, node, creds, opts, progress)
	did = installed{changed: res.Changed, tookOver: res.TookOver}
	if err != nil {
		return did, err
	}
	did.changed = true
	node.Arch = res.Arch

	r.Begin(StepConnect)
	c, err := ops.Connect(ctx, node)
	if err != nil {
		r.Fail(StepConnect, err)
		return did, err
	}
	defer c.Close()
	r.Done(StepConnect)

	st := m.cfg.Store.Read()
	prov, err := ops.Provision(ctx, c, agentops.ProvisionInput{
		Node: node, Target: target, Keys: keys, Clients: clientsOf(st, node.ID), Bridge: bridgeOf(node), XrayKept: res.XrayKept,
	}, progress)
	if err != nil {
		return did, err
	}

	r.Begin(StepSaveNode)
	if err := m.register(node.ID, res, prov); err != nil {
		r.Fail(StepSaveNode, err)
		return did, err
	}
	// the loop and the bridge take the node up from here
	m.Sync()
	r.Done(StepSaveNode)
	m.log.Info("a node is installed", "node", node.ID, "name", node.Name, "agent", res.AgentVersion, "xray", prov.XrayVersion, "tookOverXray", res.TookOver)
	return did, nil
}

// How a failed install ended, which its job's error says after the cause.
const (
	leftAlone = iota // nothing was changed on the server, and the node was forgotten
	cleanedUp        // the server was cleaned up, and the node was forgotten
	keptAsIs         // the cleanup failed too, and the install is kept as unfinished
)

// tookOverNote is what a failed install says when it had taken over the server's
// own Xray: that is not undone by anything, and the person is told so plainly.
const tookOverNote = "The server's own Xray was stopped and removed, as you asked, and is not restored."

// failedInstall is the error of an install job that failed: the cause, and
// what was done about it. The cause stays reachable, for the job's code.
type failedInstall struct {
	cause    error
	outcome  int
	tookOver bool // the server's own Xray was taken over before it failed
}

func (e *failedInstall) Unwrap() error { return e.cause }

func (e *failedInstall) Error() string {
	var then string
	switch e.outcome {
	case leftAlone:
		then = "Nothing was changed on the server, and nothing was kept."
	case cleanedUp:
		then = "The server was cleaned up and nothing was kept."
	default:
		then = "Cleaning up the server failed too, so the install is kept as unfinished: clean it up or forget it on the Nodes tab."
	}
	if e.tookOver {
		then += " " + tookOverNote
	}
	// the cause is cut to fit, so that what was done is never what the job's error loses
	cause := strings.TrimSpace(e.cause.Error())
	if room := jobs.MaxError - utf8.RuneCountInString(then) - 2; utf8.RuneCountInString(cause) > room {
		cause = string([]rune(cause)[:room-1]) + "…"
	}
	if !strings.HasSuffix(cause, ".") && !strings.HasSuffix(cause, "!") && !strings.HasSuffix(cause, "?") && !strings.HasSuffix(cause, "…") {
		cause += "."
	}
	return cause + " " + then
}

// afterFailure is what an install job does once it has failed with cause. An
// install that changed the server is taken off it first (UndoInstall, with the
// login the job still holds, which puts the server back as the marker says,
// but for the server's own Xray when the install took it over, which stays
// gone and is said to); one that changed nothing has nothing to take off. When
// that worked the node is forgotten, as a forced removal forgets one. When it
// didn't, the record is kept as an unfinished install, with what failed, and is
// no node from then on.
//
// The record keeps the texts of the errors, with the secrets of the login and
// the REALITY key taken out as the job's own texts have them: a library may
// repeat what it was given.
func (m *Manager) afterFailure(ctx context.Context, r *jobs.Run, ops Ops, node model.Node, creds agentops.Login, did installed, cause error, secrets []string) error {
	said := func(err error) string { return clipText(jobs.Hide(err.Error(), secrets)) }
	failure := model.InstallFailure{Error: said(cause), At: m.now().UTC().Truncate(time.Second)}
	if did.tookOver {
		failure.Error = clipText(said(cause) + " " + tookOverNote)
	}
	if step := r.FailedStep(); step != "" {
		failure.Step, failure.StepKey = step, StepKey(step)
	}
	outcome := leftAlone
	if did.changed {
		outcome = cleanedUp
		if err := ops.UndoInstall(ctx, node, creds, progressTo(r)); err != nil {
			failure.Cleanup = said(err)
			m.keepUnfinished(node.ID, failure)
			m.log.Warn("an install failed and its server could not be cleaned up", "node", node.ID, "host", node.Host, "err", failure.Cleanup)
			return &failedInstall{cause, keptAsIs, did.tookOver}
		}
	}
	r.Begin(StepDiscardNode)
	if err := m.forgetNode(node.ID); err != nil {
		r.Fail(StepDiscardNode, err)
		failure.Cleanup = "The server was cleaned up, but the record could not be deleted: " + said(err)
		m.keepUnfinished(node.ID, failure)
		return &failedInstall{cause, keptAsIs, did.tookOver}
	}
	r.Done(StepDiscardNode)
	m.log.Info("an install failed and left nothing behind", "node", node.ID, "host", node.Host, "cleaned", did.changed, "tookOverXray", did.tookOver)
	return &failedInstall{cause, outcome, did.tookOver}
}

// keepUnfinished marks the node an unfinished install that failed so. A node
// that is gone, or has been set up after all, is left as it is.
func (m *Manager) keepUnfinished(id uint, f model.InstallFailure) {
	err := m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil || n.State != model.NodeInstalling {
			return nil
		}
		n.Failure = f
		return nil
	})
	if err != nil {
		m.log.Error("an unfinished install could not be marked so", "node", id, "err", err)
	}
}

// register marks the node as set up and keeps what the install found out in
// one write: the server's architecture, the versions its agent reports, the
// REALITY values that make the links, and the certificate's dates.
func (m *Manager) register(id uint, res agentops.InstallResult, prov agentops.ProvisionResult) error {
	return m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil {
			return errRemovedMidInstall
		}
		n.State = model.NodeRegistered
		n.Arch = res.Arch
		n.AgentVersion = firstOf(agentVersionOf(prov.AgentVersion), agentVersionOf(res.AgentVersion))
		n.XrayVersion = firstOf(xrayVersionOf(prov.XrayVersion), xrayVersionOf(res.XrayVersion))
		n.RealityTarget, n.RealitySNI = prov.HandshakeTarget, prov.SNI
		n.RealityPublicKey, n.RealityShortID, n.RealityPrivateKey = prov.PublicKey, prov.ShortID, prov.PrivateKey
		n.CertNotBefore, n.CertNotAfter = res.CertNotBefore.UTC(), res.CertNotAfter.UTC()
		return nil
	})
}
