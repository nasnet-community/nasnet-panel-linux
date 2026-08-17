package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// ConfirmWindow is how long an applied change has to be confirmed before the dead-man reverts it
const ConfirmWindow = 90 * time.Second

// Marker is the arm state. Panel only writes/deletes it. (systemd timer acts, panel goes down)
type Marker struct {
	PlanID       uint   `json:"plan_id"`
	Snapshot     string `json:"snapshot"`
	DeadlineUnix int64  `json:"deadline_unix"`
}

func (m Marker) Expired(now time.Time) bool { return now.Unix() > m.DeadlineUnix }

func MarkerPath(p Paths) string { return filepath.Join(p.StateDir, "net-pending.json") }

func WriteMarker(p Paths, m Marker) error {
	if err := os.MkdirAll(p.StateDir, 0o750); err != nil {
		return fmt.Errorf("state dir: %w", err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal marker: %w", err)
	}
	tmp := MarkerPath(p) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}
	return os.Rename(tmp, MarkerPath(p))
}

// ReadMarker returns (nil, nil) when absent. Timer hits this ~8,640x/day.
func ReadMarker(p Paths) (*Marker, error) {
	b, err := os.ReadFile(MarkerPath(p))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read marker: %w", err)
	}
	var m Marker
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse marker: %w", err)
	}
	return &m, nil
}

func DeleteMarker(p Paths) error {
	if err := os.Remove(MarkerPath(p)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete marker: %w", err)
	}
	return nil
}

// Op is one network operation. Undo is only for effects a snapshot can't hold.
type Op struct {
	Desc string
	Do   func(context.Context) error
	Undo func(context.Context) error
}

// Plan is a dry run; the UI shows Descriptions() first.
type Plan struct {
	Ops      []Op
	Verdicts []domain.Verdict
}

func (p Plan) Descriptions() []string {
	out := make([]string, 0, len(p.Ops))
	for _, o := range p.Ops {
		out = append(out, o.Desc)
	}
	return out
}

// ApplyRecorder is the slice of ApplyRepository this needs.
type ApplyRecorder interface {
	Create(ctx context.Context, r *domain.ApplyRecord) error
	Latest(ctx context.Context) (*domain.ApplyRecord, error)
	SetPhase(ctx context.Context, id uint, phase domain.ApplyPhase, errMsg string) error
}

// Applier runs the two-phase commit: snapshot, apply, arm, confirm or revert.
type Applier struct {
	Snap  *Snapshotter
	Repo  ApplyRecorder
	Paths Paths
	// Reload makes networkd re-read its config.
	Reload func(context.Context) error
	// Now is injectable so tests skip the 90s wait.
	Now func() time.Time
	// OnRollback lets the caller emit wan.apply_rolled_back.
	OnRollback func(planID uint)
}

// Routing tables this feature owns.
var tablesToSnapshot = []int{201, 202}

func (a *Applier) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

// Apply snapshots, runs ops in order, reloads, arms. A failing op restores now, not in 90s.
func (a *Applier) Apply(ctx context.Context, p Plan, performedTakeover bool) (*domain.ApplyRecord, error) {
	// One armed change at a time: a second apply orphans the first snapshot.
	m, err := ReadMarker(a.Paths)
	if err != nil {
		return nil, fmt.Errorf("read dead-man: %w", err)
	}
	if m != nil {
		return nil, fmt.Errorf("plan %d is still armed; confirm it or let it revert first", m.PlanID)
	}

	rec := &domain.ApplyRecord{
		NodeID:            1,
		Phase:             domain.PhasePlanned,
		Ops:               p.Descriptions(),
		PerformedTakeover: performedTakeover,
	}
	if err := a.Repo.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("record plan: %w", err)
	}

	snap, err := a.Snap.Capture(ctx, tablesToSnapshot)
	if err != nil {
		_ = a.Repo.SetPhase(ctx, rec.ID, domain.PhaseFailed, err.Error())
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	snapPath := filepath.Join(a.Paths.StateDir, fmt.Sprintf("snap-%d.json", rec.ID))
	if err := a.Snap.Save(snap, snapPath); err != nil {
		_ = a.Repo.SetPhase(ctx, rec.ID, domain.PhaseFailed, err.Error())
		return nil, fmt.Errorf("save snapshot: %w", err)
	}
	rec.SnapshotPath = snapPath

	for _, op := range p.Ops {
		if err := op.Do(ctx); err != nil {
			applyErr := fmt.Errorf("op %q: %w", op.Desc, err)
			if rerr := a.Snap.Restore(ctx, snap); rerr != nil {
				applyErr = fmt.Errorf("%w (restore also failed: %v)", applyErr, rerr)
			}
			// Nothing to disarm: the marker is written after the ops.
			_ = a.Repo.SetPhase(ctx, rec.ID, domain.PhaseFailed, applyErr.Error())
			return nil, applyErr
		}
	}

	if a.Reload != nil {
		if err := a.Reload(ctx); err != nil {
			reloadErr := fmt.Errorf("networkctl reload: %w", err)
			if rerr := a.Snap.Restore(ctx, snap); rerr != nil {
				reloadErr = fmt.Errorf("%w (restore also failed: %v)", reloadErr, rerr)
			}
			_ = a.Repo.SetPhase(ctx, rec.ID, domain.PhaseFailed, reloadErr.Error())
			return nil, reloadErr
		}
	}

	deadline := a.now().Add(ConfirmWindow)
	if err := WriteMarker(a.Paths, Marker{
		PlanID: rec.ID, Snapshot: snapPath, DeadlineUnix: deadline.Unix(),
	}); err != nil {
		// Nothing would undo this now. Revert.
		_ = a.Snap.Restore(ctx, snap)
		_ = a.Repo.SetPhase(ctx, rec.ID, domain.PhaseFailed, err.Error())
		return nil, fmt.Errorf("arm dead-man: %w", err)
	}

	rec.Deadline = &deadline
	rec.Phase = domain.PhaseApplied
	if err := a.Repo.SetPhase(ctx, rec.ID, domain.PhaseApplied, ""); err != nil {
		return nil, fmt.Errorf("record applied: %w", err)
	}
	return rec, nil
}

// Confirm disarms the dead-man.
func (a *Applier) Confirm(ctx context.Context, planID uint) error {
	m, err := ReadMarker(a.Paths)
	if err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("nothing to confirm")
	}
	if planID != 0 && m.PlanID != planID {
		return fmt.Errorf("marker is for plan %d, not %d", m.PlanID, planID)
	}
	if err := DeleteMarker(a.Paths); err != nil {
		return err
	}
	return a.Repo.SetPhase(ctx, m.PlanID, domain.PhaseConfirmed, "")
}

// Rollback restores the armed snapshot. ifExpired no-ops before the deadline.
func (a *Applier) Rollback(ctx context.Context, ifExpired bool) (bool, error) {
	m, err := ReadMarker(a.Paths)
	if err != nil || m == nil {
		return false, err
	}
	if ifExpired && !m.Expired(a.now()) {
		return false, nil
	}

	snap, err := LoadSnapshot(m.Snapshot)
	if err != nil {
		return false, fmt.Errorf("load snapshot %s: %w", m.Snapshot, err)
	}
	if err := a.Snap.Restore(ctx, snap); err != nil {
		return false, fmt.Errorf("restore: %w", err)
	}
	if a.Reload != nil {
		if err := a.Reload(ctx); err != nil {
			return false, fmt.Errorf("networkctl reload after restore: %w", err)
		}
	}
	if err := DeleteMarker(a.Paths); err != nil {
		return false, err
	}
	if err := a.Repo.SetPhase(ctx, m.PlanID, domain.PhaseRolledBack, "confirm window expired"); err != nil {
		return true, fmt.Errorf("record rollback: %w", err)
	}
	if a.OnRollback != nil {
		a.OnRollback(m.PlanID)
	}
	return true, nil
}

// ReloadNetworkd is the production Reload. it won't remove stale files under /run/systemd/network
func ReloadNetworkd(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "networkctl", "reload").CombinedOutput()
	if err != nil {
		return fmt.Errorf("networkctl reload: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
