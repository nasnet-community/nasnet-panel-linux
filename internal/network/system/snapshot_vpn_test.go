package system

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

func slotOf(n int) *int { return &n }

func TestSnapshot_CapturesTheEnabledPool(t *testing.T) {
	ctx := context.Background()
	want := []domain.VPNProfile{
		{ID: 7, Name: "frankfurt", Enabled: true, WGSlot: slotOf(0), Weight: 3, Config: `{"private_key":"k"}`},
		{ID: 9, Name: "vienna", Enabled: true, WGSlot: slotOf(1), Priority: 1, Weight: 1, Config: `{"private_key":"j"}`},
	}

	s := &Snapshotter{
		Backend:     NewFakeBackend(),
		Paths:       Paths{StateDir: t.TempDir()},
		CapturePool: func(context.Context) ([]domain.VPNProfile, error) { return want, nil },
	}
	snap, err := s.Capture(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.VPNPoolCaptured || len(snap.VPNProfiles) != 2 || snap.VPNProfiles[0].ID != 7 {
		t.Fatalf("captured = %v / %+v", snap.VPNPoolCaptured, snap.VPNProfiles)
	}

	// It has to survive the round trip to disk, which is where a rollback
	// reads it from.
	blob, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var back Snapshot
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatal(err)
	}
	if !back.VPNPoolCaptured || len(back.VPNProfiles) != 2 || back.VPNProfiles[1].Name != "vienna" {
		t.Errorf("after a round trip: %v / %+v", back.VPNPoolCaptured, back.VPNProfiles)
	}
	if back.VPNProfiles[1].WGSlot == nil || *back.VPNProfiles[1].WGSlot != 1 ||
		back.VPNProfiles[1].Priority != 1 {
		t.Errorf("role fields lost in the round trip: %+v", back.VPNProfiles[1])
	}
}

// Config is json:"-", so losing it here erases the rows on rollback.
func TestSnapshot_KeepsTheConfigsThroughTheFile(t *testing.T) {
	cfgs := []string{
		`{"private_key":"k","peer":{"endpoint":"1.2.3.4:51820"}}`,
		`{"private_key":"j","peer":{"endpoint":"5.6.7.8:51820"}}`,
	}
	path := filepath.Join(t.TempDir(), "snap-1.json")

	s := &Snapshotter{
		Backend: NewFakeBackend(),
		Paths:   Paths{StateDir: t.TempDir()},
		CapturePool: func(context.Context) ([]domain.VPNProfile, error) {
			return []domain.VPNProfile{
				{ID: 7, Name: "a", Enabled: true, WGSlot: slotOf(0), Config: cfgs[0]},
				{ID: 8, Name: "b", Enabled: true, WGSlot: slotOf(1), Config: cfgs[1]},
			}, nil
		},
	}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(snap, path); err != nil {
		t.Fatal(err)
	}

	back, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.VPNProfiles) != 2 {
		t.Fatal("profiles missing from the reloaded snapshot")
	}
	for i := range back.VPNProfiles {
		if back.VPNProfiles[i].Config != cfgs[i] {
			t.Errorf("config %d after the round trip = %q, want the stored one", i, back.VPNProfiles[i].Config)
		}
	}
}

// A snapshot that skipped the pool still looks complete to a rollback.
func TestSnapshot_FailsWhenThePoolCannotBeRead(t *testing.T) {
	s := &Snapshotter{
		Backend: NewFakeBackend(),
		Paths:   Paths{StateDir: t.TempDir()},
		CapturePool: func(context.Context) ([]domain.VPNProfile, error) {
			return nil, errors.New("database is locked")
		},
	}
	if _, err := s.Capture(context.Background(), nil); err == nil {
		t.Fatal("a failed pool capture produced a snapshot anyway")
	}
}

// "Nothing was enabled" is a real state to restore, not an absence.
func TestSnapshot_CapturesTheAbsenceOfAPool(t *testing.T) {
	s := &Snapshotter{
		Backend:     NewFakeBackend(),
		Paths:       Paths{StateDir: t.TempDir()},
		CapturePool: func(context.Context) ([]domain.VPNProfile, error) { return nil, nil },
	}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.VPNPoolCaptured || len(snap.VPNProfiles) != 0 {
		t.Errorf("captured = %v / %+v", snap.VPNPoolCaptured, snap.VPNProfiles)
	}
}

func TestRestore_TearsThePoolDownWhenNothingWasEnabled(t *testing.T) {
	var got []domain.VPNProfile
	var called int
	s := &Snapshotter{
		Backend:     NewFakeBackend(),
		Paths:       Paths{StateDir: t.TempDir()},
		Restart:     func(context.Context) error { return nil },
		RestorePool: func(_ context.Context, p []domain.VPNProfile) error { called++; got = p; return nil },
	}
	if err := s.Restore(context.Background(), &Snapshot{VPNPoolCaptured: true}); err != nil {
		t.Fatal(err)
	}
	if called != 1 || len(got) != 0 {
		t.Errorf("restore called %d times with %+v; want once with an empty set", called, got)
	}
}

func TestRestore_PutsTheOldPoolBack(t *testing.T) {
	var got []domain.VPNProfile
	s := &Snapshotter{
		Backend:     NewFakeBackend(),
		Paths:       Paths{StateDir: t.TempDir()},
		Restart:     func(context.Context) error { return nil },
		RestorePool: func(_ context.Context, p []domain.VPNProfile) error { got = p; return nil },
	}
	want := []domain.VPNProfile{{ID: 3, Name: "old", Enabled: true, WGSlot: slotOf(0)}}
	if err := s.Restore(context.Background(), &Snapshot{VPNPoolCaptured: true, VPNProfiles: want}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 3 {
		t.Errorf("got %+v, want the previously enabled set", got)
	}
}

// A snapshot taken by a build that predates the pool must not be read as
// "there was no pool" and tear a working one down mid-upgrade.
func TestRestore_OlderSnapshotLeavesThePoolAlone(t *testing.T) {
	called := 0
	s := &Snapshotter{
		Backend:     NewFakeBackend(),
		Paths:       Paths{StateDir: t.TempDir()},
		Restart:     func(context.Context) error { return nil },
		RestorePool: func(context.Context, []domain.VPNProfile) error { called++; return nil },
	}
	if err := s.Restore(context.Background(), &Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Errorf("restore ran %d times on a snapshot that never recorded the pool", called)
	}
}

// A revert has to put the pool's own table back too, or the restored rules
// point into a table still holding the wrong default.
func TestTablesToSnapshot_IncludesThePool(t *testing.T) {
	var found bool
	for _, tbl := range tablesToSnapshot {
		if tbl == WGTable {
			found = true
		}
	}
	if !found {
		t.Errorf("tablesToSnapshot = %v, missing the pool's table %d", tablesToSnapshot, WGTable)
	}
}
