// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// expiring is a certificate with that many days left.
func expiring(days int) func(*model.Node) {
	return func(n *model.Node) {
		n.CertNotBefore, n.CertNotAfter = epoch.AddDate(0, 0, days-365), epoch.AddDate(0, 0, days)
	}
}

func TestRenewingACertificateSavesTheDatesTheNodeNowHas(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t, expiring(20))
		s.start()
		defer s.stop()
		made, _ := s.net.opened(s.node.ID)

		id, err := s.m.RenewCert(context.Background(), s.node.ID)
		must(t, err)
		j := s.ok(id)
		if j.Kind != jobs.KindRenewCert || j.NodeID != s.node.ID || j.Target != s.node.Name || !slices.Equal(stepNames(j), []string{StepRenewCert, StepSaveNode}) {
			t.Errorf("job = %+v", j)
		}
		if d := stepDetail(j, StepRenewCert); d != "Valid until 2027-10-30" {
			t.Errorf("the renewing step says %q", d)
		}
		if len(s.ops.renewals) != 1 || s.ops.renewals[0].ID != s.node.ID || s.ops.renewals[0].Host != s.node.Host || s.ops.renewals[0].AgentPort != s.node.AgentPort {
			t.Errorf("renewals = %+v", s.ops.renewals)
		}
		n := s.current()
		if !n.CertNotBefore.Equal(epoch) || !n.CertNotAfter.Equal(epoch.AddDate(1, 0, 0)) {
			t.Errorf("the state has %v to %v", n.CertNotBefore, n.CertNotAfter)
		}
		ev := s.m.NodeEvents(s.node.ID)
		if len(ev) != 1 || ev[0].Kind != EventCertRenewed || ev[0].Detail != "valid until 2027-10-30" {
			t.Errorf("events = %+v", ev)
		}
		// the hub's own link to the node carries on, as a renewal must leave it
		if now, open := s.net.opened(s.node.ID); now != made || open != 1 {
			t.Errorf("the link was remade: %d made (was %d), %d open", now, made, open)
		}
		// nothing was downloaded, so nothing is let go of
		if s.dl.releases() != 0 {
			t.Errorf("downloads let go of %d times", s.dl.releases())
		}
	})
}

func TestACertificateThatCantBeRenewedIsLeftAsItWas(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t, expiring(20))
		s.start()
		defer s.stop()
		before := s.current()

		s.ops.failAt("RenewCert", &agentops.UnreachableError{What: "agent", Host: "203.0.113.1", Port: 9090})
		id, err := s.m.RenewCert(context.Background(), s.node.ID)
		must(t, err)
		j := s.failed(id)
		if !strings.Contains(j.Error, "TCP port 9090") || stepStates(j)[StepRenewCert] != jobs.StepFailed || stepStates(j)[StepSaveNode] != jobs.StepPending {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}

		// the node took it, and the state can't say so: nothing is claimed
		s.ops.failAt("RenewCert", nil)
		s.disk.failNext(1, errors.New("the disk is full"))
		id, err = s.m.RenewCert(context.Background(), s.node.ID)
		must(t, err)
		j = s.failed(id)
		if j.Error != "the disk is full" || stepStates(j)[StepRenewCert] != jobs.StepOK || stepStates(j)[StepSaveNode] != jobs.StepFailed {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}
		if s.current() != before || len(s.m.NodeEvents(s.node.ID)) != 0 {
			t.Errorf("the node changed: %+v, events %v", s.current(), kinds(s.m.NodeEvents(s.node.ID)))
		}
	})
}

func TestACertificateIsRenewedOnlyOnANodeThatIsReadyAndFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t, expiring(20))
		down := addNode(t, s.cfg.Store, expiring(20))
		s.net.agent(down.ID).setDown(true)
		unfinished := addNode(t, s.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		s.start()
		defer s.stop()
		ctx := context.Background()
		var e *api.Error

		if _, err := s.m.RenewCert(ctx, 99); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a node nobody has: %v", err)
		}
		if _, err := s.m.RenewCert(ctx, unfinished.ID); !errors.As(err, &e) || e.Code != api.CodeBadRequest {
			t.Errorf("a node that never finished: %v", err)
		}
		if _, err := s.m.RenewCert(ctx, down.ID); !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable {
			t.Errorf("a node that doesn't answer: %v", err)
		}
		hold := make(chan struct{})
		_, err := s.m.startJob(jobs.Spec{Kind: jobs.KindUpdateAgent, NodeID: s.node.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		if _, err := s.m.RenewCert(ctx, s.node.ID); !errors.Is(err, jobs.ErrBusy) {
			t.Errorf("a node with a job: %v", err)
		}
		close(hold)
		synctest.Wait()
		if s.ops.called("RenewCert") != 0 {
			t.Errorf("calls %v", s.ops.allCalls())
		}
	})
}

// Once a day the certificates are looked at, and a renewal is started for each
// that has a month left or less, or is over: the same job a person would start.
func TestTheDailyCheckRenewsTheCertificatesThatAreDue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops)
		due := r.addNode(expiring(29))
		fine := r.addNode(expiring(40))
		down := r.addNode(expiring(-3)) // over already, and the node isn't answering
		r.net.agent(down.ID).setDown(true)
		r.start()
		defer r.stop()

		r.advance(time.Minute + 59*time.Second)
		if r.ops.called("RenewCert") != 0 {
			t.Fatalf("a certificate was renewed before the first look at two minutes: %v", r.ops.allCalls())
		}
		r.advance(2 * time.Second)
		if got := r.ops.allCalls(); !slices.Equal(got, []string{"RenewCert 1"}) {
			t.Fatalf("calls %v, want the one that is due and can be reached", got)
		}
		if n := r.log.count("the node can't be reached, so it waits for tomorrow", "node=3"); n != 1 {
			t.Errorf("the node that is down was said %d times:\n%s", n, r.log)
		}
		list := r.jobs.List()
		if len(list) != 1 || list[0].Kind != jobs.KindRenewCert || list[0].NodeID != due.ID || list[0].Target != due.Name || list[0].State != jobs.StateOK {
			t.Fatalf("jobs = %+v", list)
		}
		if got := nodeOf(r.cfg.Store, due.ID).CertNotAfter; !got.Equal(epoch.AddDate(1, 0, 0)) {
			t.Errorf("the renewed node's certificate ends %v", got)
		}
		if got := kinds(r.m.NodeEvents(due.ID)); !slices.Equal(got, []string{EventCertRenewed}) {
			t.Errorf("events = %v", got)
		}
		if got := nodeOf(r.cfg.Store, fine.ID).CertNotAfter; !got.Equal(epoch.AddDate(0, 0, 40)) {
			t.Errorf("a certificate with more than a month left was touched: ends %v", got)
		}

		// the node comes back, and the next day's look renews it; the others are left alone
		r.net.agent(down.ID).setDown(false)
		r.advance(24 * time.Hour)
		if got := r.ops.allCalls(); !slices.Equal(got, []string{"RenewCert 1", "RenewCert 3"}) {
			t.Errorf("calls after the second day: %v", got)
		}
		// and a day on, there is nothing due
		r.advance(24 * time.Hour)
		if r.ops.called("RenewCert") != 2 {
			t.Errorf("calls after the third day: %v", r.ops.allCalls())
		}
		if r.log.count("renewing a node's certificate") != 2 {
			t.Errorf("the log:\n%s", r.log)
		}
	})
}

// A certificate that isn't on record reads as long over, so the node gets one
// that is; and a node that isn't set up has no certificate to renew.
func TestTheDailyCheckRenewsACertificateThatIsNotOnRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		none := addNode(t, r.cfg.Store) // no dates
		unfinished := addNode(t, r.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		r.start()
		defer r.stop()
		r.advance(3 * time.Minute)
		if got := r.ops.allCalls(); !slices.Equal(got, []string{"RenewCert 1"}) {
			t.Fatalf("calls %v", got)
		}
		if got := nodeOf(r.cfg.Store, none.ID); got.CertNotAfter.IsZero() {
			t.Error("the dates weren't saved")
		}
		if got := nodeOf(r.cfg.Store, unfinished.ID); !got.CertNotAfter.IsZero() {
			t.Error("a node that isn't set up got a certificate")
		}
	})
}

// A node with a job running is left for tomorrow, since two jobs on a node
// step on each other, and gets its renewal then.
func TestTheDailyCheckLeavesANodeWithAJobForTomorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops)
		n := r.addNode(expiring(10))
		r.start()
		defer r.stop()
		hold := make(chan struct{})
		busy, err := r.m.startJob(jobs.Spec{Kind: jobs.KindUpdateXray, NodeID: n.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)

		r.advance(3 * time.Minute)
		if r.ops.called("RenewCert") != 0 || r.log.count("a job is running on it", "job="+busy) != 1 {
			t.Fatalf("calls %v\n%s", r.ops.allCalls(), r.log)
		}
		close(hold)
		r.advance(24 * time.Hour)
		if got := r.ops.allCalls(); !slices.Equal(got, []string{"RenewCert 1"}) {
			t.Errorf("calls the next day: %v", got)
		}
	})
}

// A renewal that fails is tried again the next day, for as long as it is due.
func TestTheDailyCheckTriesAgainAFailedRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t, lazyLoops)
		n := r.addNode(expiring(10))
		r.ops.failAt("RenewCert", errors.New("The agent refused the new certificate."))
		r.start()
		defer r.stop()
		r.advance(3 * time.Minute)
		r.advance(24 * time.Hour)
		r.advance(24 * time.Hour)
		if r.ops.called("RenewCert") != 3 {
			t.Fatalf("calls %v, want a try on each of three days", r.ops.allCalls())
		}
		// the engine forgets a job an hour after it ends, so the log is where the three are
		if failed := r.log.count("job finished", "kind=renew-cert", "state=failed"); failed != 3 || len(r.m.NodeEvents(n.ID)) != 0 {
			t.Errorf("%d failed jobs, events %v", failed, kinds(r.m.NodeEvents(n.ID)))
		}
		r.ops.failAt("RenewCert", nil)
		r.advance(24 * time.Hour)
		if got := kinds(r.m.NodeEvents(n.ID)); !slices.Equal(got, []string{EventCertRenewed}) {
			t.Errorf("events after it worked: %v", got)
		}
	})
}

// A manager that has no operations, as one in a test of something else may
// not, has nothing to renew with, and says nothing about it.
func TestTheDailyCheckHasNothingToDoWithoutOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, lazyLoops)
		addNode(t, r.cfg.Store, expiring(5))
		r.start()
		defer r.stop()
		r.advance(72 * time.Hour)
		for _, loud := range []string{"level=WARN", "level=ERROR", "renew"} {
			if r.log.count(loud) != 0 {
				t.Errorf("the log has %q:\n%s", loud, r.log)
			}
		}
	})
}
