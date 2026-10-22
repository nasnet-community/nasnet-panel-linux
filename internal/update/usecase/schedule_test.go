package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

func TestAScheduledUpdateStartsOnTime(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	at := time.Now().Add(time.Hour)
	if _, err := h.panel.ScheduleUpdate(context.Background(), Request{UploadID: up.ID}, at); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.panel.Status(context.Background()); st.Schedule == nil || st.Schedule.Version != "v1.1.0" {
		t.Fatalf("schedule = %+v", st.Schedule)
	}

	h.panel.MaybeRun(context.Background())
	if j, _ := h.panel.Store().Load(); j != nil {
		t.Fatal("started early")
	}
	h.panel.now = func() time.Time { return at.Add(time.Minute) }
	h.panel.MaybeRun(context.Background())
	if j := h.wait(h.panel); j.Result != domain.ResultOK {
		t.Fatalf("job = %+v", j)
	}
	if h.panel.schedule(context.Background()) != nil {
		t.Fatal("the schedule stayed after it ran")
	}
}

func TestAMissedScheduleIsSkippedAndAnnounced(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	at := time.Now().Add(time.Hour)
	if _, err := h.panel.ScheduleUpdate(context.Background(), Request{UploadID: up.ID}, at); err != nil {
		t.Fatal(err)
	}
	h.panel.now = func() time.Time { return at.Add(lateBy + time.Minute) }
	h.panel.MaybeRun(context.Background())

	if j, _ := h.panel.Store().Load(); j != nil {
		t.Fatal("a missed update ran anyway")
	}
	if h.panel.schedule(context.Background()) != nil {
		t.Fatal("the missed schedule stayed")
	}
	got := drain(h.sub)
	if len(got) != 1 || got[0].Type != events.EventPanelUpdateSkipped ||
		!strings.Contains(got[0].Payload.(events.PanelUpdatePayload).Reason, "wasn't running") {
		t.Fatalf("events = %+v", got)
	}
}

func TestAScheduleWhoseChecksFailIsSkipped(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	at := time.Now().Add(time.Hour)
	if _, err := h.panel.ScheduleUpdate(context.Background(), Request{UploadID: up.ID}, at); err != nil {
		t.Fatal(err)
	}
	h.netPending = true
	h.panel.now = func() time.Time { return at }
	h.panel.MaybeRun(context.Background())
	if j, _ := h.panel.Store().Load(); j != nil {
		t.Fatal("started with a network change waiting")
	}
	if got := drain(h.sub); len(got) != 1 || got[0].Type != events.EventPanelUpdateSkipped {
		t.Fatalf("events = %+v", got)
	}
}

func TestSchedulesAreChecked(t *testing.T) {
	h := newHarness(t)
	up := h.upload("v1.1.0")
	ctx := context.Background()
	for name, at := range map[string]time.Time{
		"past": time.Now().Add(-time.Minute), "too far": time.Now().Add(31 * 24 * time.Hour),
	} {
		if _, err := h.panel.ScheduleUpdate(ctx, Request{UploadID: up.ID}, at); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := h.panel.ScheduleUpdate(ctx, Request{Version: "v9.0.0"}, time.Now().Add(time.Hour)); err == nil {
		t.Error("a version that isn't on offer was scheduled")
	}
	if _, err := h.panel.CancelSchedule(ctx); err == nil {
		t.Error("cancelled nothing")
	}
	if _, err := h.panel.ScheduleUpdate(ctx, Request{UploadID: up.ID}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s, err := h.panel.CancelSchedule(ctx); err != nil || s.Version != "v1.1.0" || h.panel.schedule(ctx) != nil {
		t.Fatalf("cancel = %+v, %v", s, err)
	}
}

func TestUnusedUploadsGoAfterADay(t *testing.T) {
	h := newHarness(t)
	old, kept, fresh := h.upload("v1.1.0"), h.upload("v1.1.0"), h.upload("v1.1.0")
	stale := time.Now().Add(-25 * time.Hour)
	for _, id := range []string{old.ID, kept.ID} {
		for _, ext := range []string{".tar.gz", ".json"} {
			_ = os.Chtimes(filepath.Join(h.panel.deps.Paths.Uploads(), id+ext), stale, stale)
		}
	}
	h.panel.pruneUploads(kept.ID)
	for id, want := range map[string]bool{old.ID: false, kept.ID: true, fresh.ID: true} {
		if _, err := h.panel.upload(id); (err == nil) != want {
			t.Errorf("upload %s: kept %v, want %v", id, err == nil, want)
		}
	}
}

func TestSentenceLeavesAVersionAlone(t *testing.T) {
	for in, want := range map[string]string{
		"v1.0.5 isn't newer": "v1.0.5 isn't newer", "pick a time": "Pick a time", "": "", "Already": "Already",
	} {
		if got := Sentence(in); got != want {
			t.Errorf("Sentence(%q) = %q", in, got)
		}
	}
}
