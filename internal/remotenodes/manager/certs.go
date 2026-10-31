// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

const (
	// certCheckFirst is how long after the manager starts the certificates
	// are looked at for the first time, which lets the nodes be reached.
	certCheckFirst = 2 * time.Minute
	// certCheckEvery is how often they are looked at after that. A
	// certificate is renewed with a month left, so a node that can't be
	// reached on the day has thirty more tries.
	certCheckEvery = 24 * time.Hour
)

// RenewCert gives a node a new certificate from the CA, as a job, which gives
// the id. The node keeps its id and its links, and the hub's own link to it
// carries on. The new dates are saved when the node shows the new certificate.
func (m *Manager) RenewCert(_ context.Context, id uint) (string, error) {
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
	return m.startRenew(n, ops)
}

func (m *Manager) startRenew(n model.Node, ops Ops) (string, error) {
	return m.startJob(jobs.Spec{
		Kind: jobs.KindRenewCert, NodeID: n.ID, Target: n.Name, Steps: []string{StepRenewCert, StepSaveNode},
	}, func(ctx context.Context, r *jobs.Run) error { return m.runRenew(ctx, r, ops, n.ID) })
}

func (m *Manager) runRenew(ctx context.Context, r *jobs.Run, ops Ops, id uint) error {
	n, err := m.nodeFor(id)
	if err != nil {
		return err
	}
	c, err := m.link(n)
	if err != nil {
		return err
	}
	r.Begin(StepRenewCert)
	info, err := ops.RenewCert(ctx, c, n)
	if err != nil {
		r.Fail(StepRenewCert, err)
		return err
	}
	until := info.NotAfter.UTC().Format(time.DateOnly)
	r.Detail(StepRenewCert, "Valid until "+until)
	r.Done(StepRenewCert)

	r.Begin(StepSaveNode)
	err = m.cfg.Store.Update(func(s *model.State) error {
		n := s.FindNode(id)
		if n == nil {
			return errNodeGone
		}
		n.CertNotBefore, n.CertNotAfter = info.NotBefore.UTC(), info.NotAfter.UTC()
		return nil
	})
	if err != nil {
		r.Fail(StepSaveNode, err)
		return err
	}
	m.Event(id, EventCertRenewed, "valid until "+until)
	r.Done(StepSaveNode)
	m.log.Info("a node's certificate was renewed", "node", id, "until", until)
	return nil
}

// watchCerts looks at every node's certificate once a day. Without operations
// and jobs there is nothing it could do about one.
func (m *Manager) watchCerts(ctx context.Context) {
	if m.cfg.NewOps == nil || m.cfg.Jobs == nil {
		return
	}
	for wait := certCheckFirst; sleep(ctx, wait); wait = certCheckEvery {
		m.checkCerts()
	}
}

// checkCerts starts a renewal for each node whose certificate pki says is due:
// a month from its end or less, or already over, or not on record at all. A
// node with a job running is left for the next day, and so is one that can't be
// reached, which the log says. The jobs are the same as the ones a person
// starts, so the page shows them in its job tray.
func (m *Manager) checkCerts() {
	st := m.cfg.Store.Read()
	now := m.now()
	for _, n := range st.Nodes {
		if n.State != model.NodeRegistered || !pki.NeedsRenewal(n.CertNotAfter, now) {
			continue
		}
		if job, running := m.cfg.Jobs.Running(n.ID); running {
			m.log.Info("a node's certificate is due, but a job is running on it, so it waits for tomorrow", "node", n.ID, "job", job.ID)
			continue
		}
		if _, ok := m.Client(n.ID); !ok {
			m.log.Warn("a node's certificate is due, but the node can't be reached, so it waits for tomorrow", "node", n.ID, "until", n.CertNotAfter)
			continue
		}
		ops, err := m.Ops()
		if err != nil {
			m.log.Warn("no certificate can be renewed", "err", err)
			return
		}
		id, err := m.startRenew(n, ops)
		if err != nil {
			m.log.Warn("a node's certificate could not be renewed", "node", n.ID, "err", err)
			continue
		}
		m.log.Info("renewing a node's certificate", "node", n.ID, "until", n.CertNotAfter, "job", id)
	}
}
