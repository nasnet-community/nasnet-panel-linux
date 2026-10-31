// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The steps that the manager adds to the ones agentops reports. They are
// words for the page like the rest: the page shows a name it doesn't know as
// it is.
const (
	StepConnect    = "Connect to the agent"
	StepSaveNode   = "Save the node"
	StepRenewCert  = "Renew the certificate"
	StepForgetNode = "Forget the node"
)

type jobFunc = func(ctx context.Context, r *jobs.Run) error

// startJob runs fn as a job and gives its id. A node has one job at a time,
// and for a second Start's error says so (jobs.ErrBusy, which the API words).
func (m *Manager) startJob(spec jobs.Spec, fn jobFunc) (string, error) {
	if m.cfg.Jobs == nil {
		return "", &api.Error{Code: api.CodeInternal, Msg: "This hub isn't set up to run jobs."}
	}
	job, err := m.cfg.Jobs.Start(spec, fn)
	switch {
	case errors.Is(err, jobs.ErrClosed):
		return "", &api.Error{Code: api.CodeInternal, Msg: "The hub is shutting down."}
	case err != nil:
		return "", err
	}
	return job.ID, nil
}

// progressTo passes what agentops reports of its steps on to the job. The
// states are spelled the same on both sides.
func progressTo(r *jobs.Run) agentops.Progress {
	return func(s agentops.Step) { r.Set(s.Name, jobs.StepState(s.State), s.Detail) }
}

// usesDownloads is fn, counted as a job that may hold downloads: when the last
// such job ends, what was downloaded is let go. A router has little memory,
// and the next job is not soon.
func (m *Manager) usesDownloads(fn jobFunc) jobFunc {
	return func(ctx context.Context, r *jobs.Run) error {
		m.downloading.Add(1)
		defer func() {
			if m.downloading.Add(-1) == 0 && m.cfg.Downloads != nil {
				m.cfg.Downloads.Release()
			}
		}()
		return fn(ctx, r)
	}
}

// nodeFor is the node with this id, or the error that says there is none.
func (m *Manager) nodeFor(id uint) (model.Node, error) {
	st := m.cfg.Store.Read()
	if n := st.FindNode(id); n != nil {
		return *n, nil
	}
	return model.Node{}, notFound("node", id)
}

// idle is nil when no job runs on the node, and the error that Start would
// have given when one does. An operation asks it first so that the page gets
// "busy" and not a complaint about something the running job is changing.
func (m *Manager) idle(id uint) error {
	if m.cfg.Jobs == nil {
		return nil
	}
	if j, running := m.cfg.Jobs.Running(id); running {
		return &jobs.BusyError{NodeID: id, JobID: j.ID}
	}
	return nil
}

// settled is the node an operation on a set-up node is for: it exists, no job
// runs on it, and its install has finished.
func (m *Manager) settled(id uint) (model.Node, error) {
	n, err := m.nodeFor(id)
	if err != nil {
		return n, err
	}
	if err := m.idle(id); err != nil {
		return n, err
	}
	if n.State != model.NodeRegistered {
		return n, neverInstalled()
	}
	return n, nil
}

// link is the connection to the node that its loop holds, for an operation
// that talks to it. There is none while the node can't be reached, which is
// the answer for the page, and it is only good until the loop gives it up, so
// each use of it asks again.
func (m *Manager) link(n model.Node) (Client, error) {
	if c, ok := m.Client(n.ID); ok {
		return c, nil
	}
	return nil, unreachable(n.Name + "'s agent")
}
