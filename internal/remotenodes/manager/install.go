// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// installSteps are the steps of an install, in order: the agent over SSH, the
// link to it, the Xray and the entry over that link, and the node saved.
func installSteps() []string {
	return slices.Concat(agentops.InstallPlan(), []string{StepConnect}, agentops.ProvisionPlan(), []string{StepSaveNode})
}

var errRemovedMidInstall = errors.New("the node was removed while it was being installed")

// AddNode saves a node whose install has not finished and starts the job that
// finishes it, which gives the job's id.
//
// The node is saved first, as installing, with the key the person agreed to
// pinned and the id its bridge logs in with, so that it has a page and a job
// from the first step on. A job that fails leaves it so, with no job running:
// the page shows it as one that never finished, and only removing it works.
// What the install leaves on the server is taken off by that removal.
//
// A node is added only when the router has room, when no other node is on that
// server, and when no other has the name (the way a person would compare
// names, so "Berlin" and "berlin" are one). The login is used by the job and
// never kept, and every secret in it, and the REALITY key the job makes, is
// taken out of whatever the job shows.
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
	bridgeID, err := newUUID()
	if err != nil {
		return "", err
	}

	var node model.Node
	err = m.cfg.Store.Update(func(s *model.State) error {
		// the limit is the answer when there is no room, whatever else is wrong
		if len(s.Nodes) < model.MaxNodes {
			if err := m.checkNew(*s, name, host); err != nil {
				return err
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

	spec := jobs.Spec{
		Kind: jobs.KindInstall, NodeID: node.ID, Target: node.Name, Steps: installSteps(),
		Secrets: lg.secrets(keys.PrivateKey),
	}
	id, err := m.startJob(spec, m.usesDownloads(func(ctx context.Context, r *jobs.Run) error {
		return m.runInstall(ctx, r, ops, node, lg.creds, keys)
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

// checkNew refuses a name or a server that a node has already.
func (m *Manager) checkNew(s model.State, name, host string) error {
	for _, o := range s.Nodes {
		switch {
		case strings.EqualFold(o.Name, name):
			return fmt.Errorf("%w: %q", model.ErrNameTaken, name)
		case o.Host == host:
			if err := m.idle(o.ID); err != nil {
				return err // an install to it is still going
			}
			if o.State != model.NodeRegistered {
				return badRequest("An install on that server was started and never finished. Remove that node first.")
			}
			return badRequest("That server is already a node here.")
		}
	}
	return nil
}

// runInstall is the install job: the agent over SSH, then everything else over
// the agent's own link, and only when all of it has worked, the node saved as
// set up. The node's clients are the subscriptions switched on there, which
// for a new node is none.
func (m *Manager) runInstall(ctx context.Context, r *jobs.Run, ops Ops, node model.Node, creds agentops.Login, keys agentops.RealityKeys) error {
	progress := progressTo(r)
	res, err := ops.InstallAgent(ctx, node, creds, progress)
	if err != nil {
		return err
	}
	node.Arch = res.Arch

	r.Begin(StepConnect)
	c, err := ops.Connect(ctx, node)
	if err != nil {
		r.Fail(StepConnect, err)
		return err
	}
	defer c.Close()
	r.Done(StepConnect)

	st := m.cfg.Store.Read()
	prov, err := ops.Provision(ctx, c, agentops.ProvisionInput{
		Node: node, Keys: keys, Clients: clientsOf(st, node.ID), Bridge: bridgeOf(node),
	}, progress)
	if err != nil {
		return err
	}

	r.Begin(StepSaveNode)
	if err := m.register(node.ID, res, prov); err != nil {
		r.Fail(StepSaveNode, err)
		return err
	}
	// the loop and the bridge take the node up from here
	m.Sync()
	r.Done(StepSaveNode)
	m.log.Info("a node is installed", "node", node.ID, "name", node.Name, "agent", res.AgentVersion, "xray", prov.XrayVersion)
	return nil
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
