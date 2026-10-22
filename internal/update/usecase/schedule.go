package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

const (
	keySchedule = "update_schedule"
	// lateBy is how late a scheduled update may still start; past that the
	// panel was down at the time and the admins pick again.
	lateBy       = 10 * time.Minute
	scheduleSpan = 30 * 24 * time.Hour
)

// Schedule is an update set for a later time.
type Schedule struct {
	Version  string    `json:"version"`
	UploadID string    `json:"upload_id,omitempty"`
	Size     int64     `json:"size"`
	At       time.Time `json:"at"`
}

// ScheduleUpdate sets req to install at at. It is checked again then.
func (u *Usecase) ScheduleUpdate(ctx context.Context, req Request, at time.Time) (*Schedule, error) {
	if u.kind.Name != KindSystemd {
		return nil, errors.New(u.kind.Why)
	}
	now := u.now()
	if !at.After(now) {
		return nil, errors.New("pick a time in the future")
	}
	if at.After(now.Add(scheduleSpan)) {
		return nil, errors.New("pick a time within 30 days")
	}
	s := &Schedule{At: at.UTC()}
	switch {
	case req.UploadID != "":
		up, err := u.upload(req.UploadID)
		if err != nil {
			return nil, err
		}
		s.Version, s.UploadID, s.Size = up.Version, up.ID, up.Size
	case req.Version != "":
		c := u.lastCheck(ctx)
		if c == nil || !c.Available || c.Latest == nil || c.Latest.Version != req.Version {
			return nil, fmt.Errorf("%s isn't on offer; check for updates first", req.Version)
		}
		s.Version, s.Size = req.Version, c.Latest.Size
	default:
		return nil, errors.New("pick a version or an upload")
	}
	if err := u.saveState(ctx, keySchedule, s); err != nil {
		return nil, err
	}
	return s, nil
}

// CancelSchedule drops the scheduled update and returns it.
func (u *Usecase) CancelSchedule(ctx context.Context) (*Schedule, error) {
	s := u.schedule(ctx)
	if s == nil {
		return nil, errors.New("no update is scheduled")
	}
	return s, u.saveRaw(ctx, keySchedule, "")
}

func (u *Usecase) schedule(ctx context.Context) *Schedule {
	raw := u.setting(ctx, keySchedule)
	if raw == "" {
		return nil
	}
	var s Schedule
	if json.Unmarshal([]byte(raw), &s) != nil || s.Version == "" {
		return nil
	}
	return &s
}

// runSchedule starts a due update. The schedule is cleared first, so a
// failure never loops; a skip tells the admins why.
func (u *Usecase) runSchedule(ctx context.Context) {
	s := u.schedule(ctx)
	if s == nil || u.now().Before(s.At) {
		return
	}
	if err := u.saveRaw(ctx, keySchedule, ""); err != nil {
		return
	}
	if late := u.now().Sub(s.At); late > lateBy {
		u.skip(s, fmt.Sprintf("The panel wasn't running at %s.", s.At.Format("15:04 MST")))
		return
	}
	if err := u.guard(ctx, s.Size); err != nil {
		u.skip(s, Sentence(err.Error())+".")
		return
	}
	if _, err := u.Apply(ctx, Request{Version: s.Version, UploadID: s.UploadID}); err != nil {
		u.skip(s, Sentence(err.Error())+".")
	}
}

func (u *Usecase) skip(s *Schedule, reason string) {
	u.publish(events.EventPanelUpdateSkipped, events.PanelUpdatePayload{
		Version: s.Version, From: u.deps.Version, Reason: reason,
	})
}

// Sentence starts s with a capital, for errors shown as messages. A version
// leads as it is: "v1.0.5 isn't newer", never "V1.0.5".
func Sentence(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' || (s[0] == 'v' && len(s) > 1 && s[1] >= '0' && s[1] <= '9') {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
