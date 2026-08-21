package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

type stubApplyRepo struct {
	records []*domain.ApplyRecord
	nextID  uint
}

func (s *stubApplyRepo) Create(_ context.Context, r *domain.ApplyRecord) error {
	s.nextID++
	r.ID = s.nextID
	cp := *r
	s.records = append(s.records, &cp)
	return nil
}

func (s *stubApplyRepo) Latest(context.Context) (*domain.ApplyRecord, error) {
	if len(s.records) == 0 {
		return nil, errors.New("none")
	}
	return s.records[len(s.records)-1], nil
}

func (s *stubApplyRepo) LatestConfirmed(context.Context) (*domain.ApplyRecord, error) {
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].Phase == domain.PhaseConfirmed {
			return s.records[i], nil
		}
	}
	return nil, errors.New("none")
}

func (s *stubApplyRepo) SetPhase(_ context.Context, id uint, phase domain.ApplyPhase, errMsg string) error {
	for _, r := range s.records {
		if r.ID == id {
			r.Phase, r.Error = phase, errMsg
			return nil
		}
	}
	return errors.New("not found")
}

func newApplier(t *testing.T) (*system.Applier, *stubApplyRepo, system.Paths) {
	t.Helper()
	p := testPaths(t)
	repo := &stubApplyRepo{}
	a := &system.Applier{
		Snap:   newTestSnapshotter(t, p),
		Repo:   repo,
		Paths:  p,
		Reload: func(context.Context) error { return nil },
		Now:    time.Now,
	}
	return a, repo, p
}

// A second apply used to overwrite the marker and orphan the first snapshot.
func TestApply_RefusesWhileAnotherChangeIsArmed(t *testing.T) {
	a, _, p := newApplier(t)
	noop := system.Plan{Ops: []system.Op{{Desc: "noop", Do: func(context.Context) error { return nil }}}}

	first, err := a.Apply(context.Background(), noop, false)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	ran := false
	second := system.Plan{Ops: []system.Op{{Desc: "second", Do: func(context.Context) error {
		ran = true
		return nil
	}}}}
	if _, err := a.Apply(context.Background(), second, false); err == nil {
		t.Fatal("second apply was allowed while the first was still armed")
	}
	if ran {
		t.Error("the second plan's ops ran anyway")
	}

	m, err := system.ReadMarker(p)
	if err != nil || m == nil {
		t.Fatalf("marker gone: %v", err)
	}
	if m.PlanID != first.ID {
		t.Errorf("marker plan = %d, want the armed one (%d)", m.PlanID, first.ID)
	}
}

func TestApply_RunsOpsInOrderAndArmsTheDeadMan(t *testing.T) {
	a, repo, p := newApplier(t)
	var order []string
	plan := system.Plan{Ops: []system.Op{
		{Desc: "move netplan aside", Do: func(context.Context) error {
			order = append(order, "netplan")
			return nil
		}},
		{Desc: "render uplink files", Do: func(context.Context) error {
			order = append(order, "render")
			return nil
		}},
	}}

	rec, err := a.Apply(context.Background(), plan, true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(order) != 2 || order[0] != "netplan" || order[1] != "render" {
		t.Errorf("ops ran out of order: %v", order)
	}
	if rec.Phase != domain.PhaseApplied {
		t.Errorf("phase = %q, want applied", rec.Phase)
	}
	if !rec.PerformedTakeover {
		t.Error("takeover flag not recorded")
	}
	if len(rec.Ops) != 2 {
		t.Errorf("op descriptions not persisted for the UI: %v", rec.Ops)
	}

	m, err := system.ReadMarker(p)
	if err != nil || m == nil {
		t.Fatalf("dead-man not armed: %v %v", m, err)
	}
	if m.PlanID != rec.ID {
		t.Errorf("marker plan id = %d, want %d", m.PlanID, rec.ID)
	}
	if m.Snapshot == "" {
		t.Error("marker has no snapshot path; a revert would have nothing to restore")
	}
	_ = repo
}

// A failing op restores now, not after 90s half-applied.
func TestApply_RestoresImmediatelyWhenAnOpFails(t *testing.T) {
	a, repo, p := newApplier(t)
	plan := system.Plan{Ops: []system.Op{
		{Desc: "ok", Do: func(context.Context) error { return nil }},
		{Desc: "boom", Do: func(context.Context) error { return errors.New("nft: syntax error") }},
	}}

	if _, err := a.Apply(context.Background(), plan, false); err == nil {
		t.Fatal("Apply returned nil after an op failed")
	}
	if m, _ := system.ReadMarker(p); m != nil {
		t.Error("a failed apply left the dead-man armed")
	}
	latest, _ := repo.Latest(context.Background())
	if latest.Phase != domain.PhaseFailed {
		t.Errorf("phase = %q, want failed", latest.Phase)
	}
}

func TestConfirm_DeletesTheMarkerAndRecordsThePhase(t *testing.T) {
	a, repo, p := newApplier(t)
	rec, err := a.Apply(context.Background(), system.Plan{Ops: []system.Op{
		{Desc: "noop", Do: func(context.Context) error { return nil }},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	if m, _ := system.ReadMarker(p); m != nil {
		t.Error("confirm did not delete the marker")
	}
	latest, _ := repo.Latest(context.Background())
	if latest.Phase != domain.PhaseConfirmed {
		t.Errorf("phase = %q, want confirmed", latest.Phase)
	}
}

// --if-expired runs ~8,640x/day; must no-op cheaply.
func TestRollback_IfExpiredNoOpsWithNoMarkerOrAFutureDeadline(t *testing.T) {
	a, _, _ := newApplier(t)
	did, err := a.Rollback(context.Background(), true)
	if err != nil || did {
		t.Fatalf("no marker: did=%v err=%v", did, err)
	}

	if _, err := a.Apply(context.Background(), system.Plan{Ops: []system.Op{
		{Desc: "noop", Do: func(context.Context) error { return nil }},
	}}, false); err != nil {
		t.Fatal(err)
	}
	did, err = a.Rollback(context.Background(), true)
	if err != nil || did {
		t.Fatalf("future deadline: did=%v err=%v", did, err)
	}
}

func TestRollback_IfExpiredRevertsAnExpiredMarker(t *testing.T) {
	a, repo, p := newApplier(t)
	rec, err := a.Apply(context.Background(), system.Plan{Ops: []system.Op{
		{Desc: "noop", Do: func(context.Context) error { return nil }},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}

	// Advance the clock instead of sleeping 90s.
	a.Now = func() time.Time { return time.Now().Add(2 * system.ConfirmWindow) }

	did, err := a.Rollback(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("an expired marker was not reverted")
	}
	if m, _ := system.ReadMarker(p); m != nil {
		t.Error("revert left the marker in place")
	}
	latest, _ := repo.Latest(context.Background())
	if latest.ID != rec.ID || latest.Phase != domain.PhaseRolledBack {
		t.Errorf("phase = %q, want rolled_back", latest.Phase)
	}
}

// A snapshot that will not restore used to keep the marker armed forever.
func TestRollback_GivesUpAfterRepeatedFailures(t *testing.T) {
	a, repo, p := newApplier(t)
	a.Snap.CapturePool = func(context.Context) ([]domain.VPNProfile, error) {
		return []domain.VPNProfile{{ID: 13, Name: "berlin", Enabled: true, Config: `{"private_key":"k"}`}}, nil
	}
	a.Snap.RestorePool = func(context.Context, []domain.VPNProfile) error {
		return errors.New("the stored config will not decode")
	}

	rec, err := a.Apply(context.Background(), system.Plan{Ops: []system.Op{
		{Desc: "noop", Do: func(context.Context) error { return nil }},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	a.Now = func() time.Time { return time.Now().Add(2 * system.ConfirmWindow) }

	for i := 1; i < system.MaxRollbackAttempts; i++ {
		if _, err := a.Rollback(context.Background(), true); err == nil {
			t.Fatalf("attempt %d reported success", i)
		}
		m, _ := system.ReadMarker(p)
		if m == nil {
			t.Fatalf("disarmed after %d attempts, want %d", i, system.MaxRollbackAttempts)
		}
		if m.Attempts != i {
			t.Errorf("attempts = %d, want %d", m.Attempts, i)
		}
	}

	if _, err := a.Rollback(context.Background(), true); err == nil {
		t.Fatal("the last attempt reported success")
	}
	if m, _ := system.ReadMarker(p); m != nil {
		t.Error("still armed after the budget ran out, so the timer keeps replaying it")
	}
	latest, _ := repo.Latest(context.Background())
	if latest.ID != rec.ID || latest.Phase != domain.PhaseFailed {
		t.Errorf("phase = %q, want failed", latest.Phase)
	}
}

// An explicit rollback ignores the deadline.
func TestRollback_ExplicitIgnoresTheDeadline(t *testing.T) {
	a, _, _ := newApplier(t)
	if _, err := a.Apply(context.Background(), system.Plan{Ops: []system.Op{
		{Desc: "noop", Do: func(context.Context) error { return nil }},
	}}, false); err != nil {
		t.Fatal(err)
	}
	did, err := a.Rollback(context.Background(), false)
	if err != nil || !did {
		t.Fatalf("explicit rollback: did=%v err=%v", did, err)
	}
}

// The dead-man runs as its own process, so the panel never hears about a revert
// and never re-derives the runtime from the restored intent — dnsmasq stayed
// down after a reverted LAN change on the target until something reconciled.
func TestShouldReconcileAfterRollback(t *testing.T) {
	rolled := &domain.ApplyRecord{ID: 7, Phase: domain.PhaseRolledBack}

	if !shouldReconcileAfterRollback(rolled, 0) {
		t.Error("a revert we have not seen must trigger a reconcile")
	}
	if shouldReconcileAfterRollback(rolled, 7) {
		t.Error("the same revert reconciled twice")
	}
	if shouldReconcileAfterRollback(&domain.ApplyRecord{ID: 8, Phase: domain.PhaseConfirmed}, 0) {
		t.Error("a confirmed apply is not a revert")
	}
	if shouldReconcileAfterRollback(nil, 0) {
		t.Error("no record is not a revert")
	}
}
