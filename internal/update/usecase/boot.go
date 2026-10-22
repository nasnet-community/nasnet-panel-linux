package usecase

import (
	"context"
	"path/filepath"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// Resume settles the last job at boot. Call it once the notification
// dispatcher runs: the bus drops events published before.
func (u *Usecase) Resume(ctx context.Context) {
	job, err := u.store.Load()
	if err != nil || job == nil {
		return
	}
	switch {
	case job.Finished():
		u.record(ctx, job)
	default:
		go u.watchRunner(ctx)
	}
}

// watchRunner follows a job someone else runs until it ends. The runner
// holds the update lock while it lives, so a free lock and a stale
// heartbeat mean it died.
func (u *Usecase) watchRunner(ctx context.Context) {
	if !u.watching.CompareAndSwap(false, true) {
		return
	}
	defer u.watching.Store(false)
	since := u.now()
	for {
		job, err := u.store.Load()
		if err != nil || job == nil {
			return
		}
		if job.Finished() {
			u.record(ctx, job)
			return
		}
		seen := since
		if job.RunnerSeen != nil && job.RunnerSeen.After(seen) {
			seen = *job.RunnerSeen
		}
		if u.now().Sub(seen) > u.runnerGone {
			if unlock, err := u.store.TryLock(); err == nil {
				u.interrupted(ctx, job)
				unlock()
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(u.pollEvery):
		}
	}
}

// interrupted ends a job whose runner died: power cut, OOM, a kill. What
// runs now decides how it went.
func (u *Usecase) interrupted(ctx context.Context, job *domain.Job) {
	r := &run{u: u, job: job}
	log := logger.GetLogger().WithField("job", job.ID)
	switch {
	case job.Phase == domain.PhasePanel:
		r.finish(domain.ResultFailed, "The update stopped before anything changed.")
	case u.deps.Version == job.To:
		// the new version is up and running this code
		r.finish(domain.ResultOK, "")
	case job.Kind == domain.KindUpdate:
		set, err := readFileSet(u.setFile())
		if err != nil || set.ReplacedBy != job.To {
			r.finish(domain.ResultFailed, "The update stopped before anything changed.")
			break
		}
		if err := u.putBack(ctx, set); err != nil {
			log.WithError(err).Error("[update] could not put the old files back")
			r.finish(domain.ResultFailed, "The update stopped halfway and some files couldn't be put back: "+err.Error())
			break
		}
		u.dropPrev()
		r.finish(domain.ResultRolledBack, "The update stopped halfway, so "+job.From+"'s files were put back.")
	default:
		if fwd, err := readFileSet(filepath.Join(u.deps.Paths.Prev(), "forward.json")); err == nil {
			if err := u.putBack(ctx, fwd); err != nil {
				log.WithError(err).Warn("[update] rollback stopped halfway")
			}
		}
		r.finish(domain.ResultFailed, "The rollback stopped halfway; "+job.From+" is still running.")
	}
	u.cleanup(job)
	u.record(ctx, job)
}

// record copies a finished job into history and announces it, once. Only
// the panel does this: the CLI has no notifications to send.
func (u *Usecase) record(ctx context.Context, job *domain.Job) {
	if job.Recorded || u.deps.Events == nil {
		return
	}
	// a second caller (boot, the tick, a watcher) finds this one at it
	if !u.recordMu.TryLock() {
		return
	}
	defer u.recordMu.Unlock()
	if cur, err := u.store.Load(); err != nil || cur == nil || cur.ID != job.ID || cur.Recorded {
		return
	}
	if u.deps.NotifyReady != nil {
		u.waitReady(ctx)
	}
	if u.deps.DB != nil {
		row := domain.PanelUpdate{
			JobID: job.ID, Kind: job.Kind, FromVersion: job.From, ToVersion: job.To,
			Result: job.Result, Reason: job.Error, StartedAt: job.StartedAt,
		}
		if job.FinishedAt != nil {
			row.FinishedAt = *job.FinishedAt
		}
		if err := u.deps.DB.WithContext(ctx).Where("job_id = ?", job.ID).FirstOrCreate(&row).Error; err != nil {
			logger.GetLogger().WithError(err).Warn("[update] could not record the job")
			return
		}
	}
	p := events.PanelUpdatePayload{Version: job.To, From: job.From, Reason: job.Error, Kind: job.Kind}
	if job.Result == domain.ResultOK {
		u.publish(events.EventPanelUpdated, p)
	} else {
		u.publish(events.EventPanelUpdateFailed, p)
	}
	if _, err := u.store.Update(func(j *domain.Job) {
		if j.ID == job.ID {
			j.Recorded = true
		}
	}); err == nil {
		job.Recorded = true
	}
}

// waitReady gives the bot a while to come up so the result reaches Telegram.
func (u *Usecase) waitReady(ctx context.Context) {
	deadline := u.now().Add(3 * time.Minute)
	for !u.deps.NotifyReady() && u.now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(u.pollEvery):
		}
	}
}

// History is the newest finished jobs.
func (u *Usecase) History(ctx context.Context, limit int) ([]domain.PanelUpdate, error) {
	var rows []domain.PanelUpdate
	if u.deps.DB == nil {
		return rows, nil
	}
	err := u.deps.DB.WithContext(ctx).Order("finished_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
