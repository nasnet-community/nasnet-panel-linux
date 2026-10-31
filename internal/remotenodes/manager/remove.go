// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"slices"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

func needsLogin() error {
	return &api.Error{
		Code: api.CodeNeedsLogin,
		Msg:  "The agent can't be taken off the server without an SSH login. Send one, or forget the node and leave the server as it is.",
	}
}

// removeSteps are the steps of a removal: with a login the agent is taken off
// the server first, and then the node is forgotten.
func removeSteps(force bool) []string {
	if force {
		return []string{StepForgetNode}
	}
	return slices.Concat(agentops.UninstallPlan(), []string{StepForgetNode})
}

// RemoveNode takes a node out, as a job, which gives the id.
//
// A managed agent can't take itself off the server, whatever is asked of it,
// and the hub keeps no login, so without one the answer is needs_login, for a
// node that can be reached as much as for one that can't. With a login the job
// takes the agent off the server over SSH (Uninstall, which puts the server
// back as it found it, and leaves alone a server's own Xray) and then forgets
// the node. With force it only forgets the node and leaves the server as it is,
// which is the way out for a server that is gone or can't be signed in to.
//
// An unfinished install, an install that failed and whose cleanup failed too,
// is taken out the same way: with a login is the page's Clean up, which takes
// off what the marker says the install put there, and force is its Forget.
// The job's target is the node's name, since the node is gone by the end, and
// the page needs it to say what was removed.
func (m *Manager) RemoveNode(_ context.Context, id uint, req api.RemoveRequest) (string, error) {
	n, err := m.nodeFor(id)
	if err != nil {
		return "", err
	}
	if err := m.idle(id); err != nil {
		return "", err
	}
	switch {
	case req.Force && req.Login != nil:
		return "", invalidf("send a login or force the removal, not both")
	case !req.Force && req.Login == nil:
		return "", needsLogin()
	}

	var ops Ops
	var lg login
	if !req.Force {
		if lg, err = cleanLogin(*req.Login); err != nil {
			return "", err
		}
		// what the person left blank is what the node was set up with
		if req.Login.SSHPort == 0 && n.SSHPort != 0 {
			lg.port = n.SSHPort
		}
		if req.Login.SSHUser == "" && n.SSHUser != "" {
			lg.user = n.SSHUser
		}
		if ops, err = m.operate(); err != nil {
			return "", err
		}
	}
	return m.startJob(jobs.Spec{
		Kind: jobs.KindRemove, NodeID: id, Target: n.Name, Steps: removeSteps(req.Force), Secrets: lg.secrets(),
	}, func(ctx context.Context, r *jobs.Run) error { return m.runRemove(ctx, r, ops, id, lg, req.Force) })
}

func (m *Manager) runRemove(ctx context.Context, r *jobs.Run, ops Ops, id uint, lg login, force bool) error {
	n, err := m.nodeFor(id)
	if err != nil {
		return err
	}
	if !force {
		// the node's own address, signed in to the way the person said, with the
		// key they agreed to: it may be a rebuilt server's, which they were shown
		n.SSHPort, n.SSHUser, n.SSHHostKey = lg.port, lg.user, lg.hostKey
		if err := ops.Uninstall(ctx, n, lg.creds, progressTo(r)); err != nil {
			return err
		}
	}
	r.Begin(StepForgetNode)
	if err := m.forgetNode(id); err != nil {
		r.Fail(StepForgetNode, err)
		return err
	}
	r.Done(StepForgetNode)
	m.log.Info("a node was removed", "node", id, "name", n.Name, "force", force)
	return nil
}

// forgetNode takes the node out of the state, with the clients it held, and
// lets go of everything the manager has of it: Sync ends its loop, which closes
// its link and then drops its numbers, and the bridge stops carrying it. A node
// that is gone already is as forgotten as one can be.
func (m *Manager) forgetNode(id uint) error {
	err := m.cfg.Store.Update(func(s *model.State) error { return s.RemoveNode(id) })
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	// the loop does this too, once it has ended; this is for a manager that isn't running
	m.cfg.Usage.Forget(id)
	m.Sync()
	return nil
}
