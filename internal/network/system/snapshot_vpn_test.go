package system

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

func TestSnapshot_CapturesTheActiveProfile(t *testing.T) {
	ctx := context.Background()
	want := &domain.VPNProfile{ID: 7, Name: "frankfurt", Active: true, Config: `{"private_key":"k"}`}

	s := &Snapshotter{
		Backend:    NewFakeBackend(),
		Paths:      Paths{StateDir: t.TempDir()},
		CaptureVPN: func(context.Context) (*domain.VPNProfile, error) { return want, nil },
	}
	snap, err := s.Capture(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.VPNCaptured || snap.VPNProfile == nil || snap.VPNProfile.ID != 7 {
		t.Fatalf("captured = %v / %+v", snap.VPNCaptured, snap.VPNProfile)
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
	if !back.VPNCaptured || back.VPNProfile == nil || back.VPNProfile.Name != "frankfurt" {
		t.Errorf("after a round trip: %v / %+v", back.VPNCaptured, back.VPNProfile)
	}
}

// Config is json:"-", so losing it here erases the row on rollback.
func TestSnapshot_KeepsTheConfigThroughTheFile(t *testing.T) {
	const cfg = `{"private_key":"k","peer":{"endpoint":"1.2.3.4:51820"}}`
	path := filepath.Join(t.TempDir(), "snap-1.json")

	s := &Snapshotter{
		Backend: NewFakeBackend(),
		Paths:   Paths{StateDir: t.TempDir()},
		CaptureVPN: func(context.Context) (*domain.VPNProfile, error) {
			return &domain.VPNProfile{ID: 7, Name: "frankfurt", Active: true, Config: cfg}, nil
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
	if back.VPNProfile == nil {
		t.Fatal("no profile in the reloaded snapshot")
	}
	if back.VPNProfile.Config != cfg {
		t.Errorf("config after the round trip = %q, want the stored one", back.VPNProfile.Config)
	}
}

// A snapshot that skipped the tunnel still looks complete to a rollback.
func TestSnapshot_FailsWhenTheProfileCannotBeRead(t *testing.T) {
	s := &Snapshotter{
		Backend: NewFakeBackend(),
		Paths:   Paths{StateDir: t.TempDir()},
		CaptureVPN: func(context.Context) (*domain.VPNProfile, error) {
			return nil, errors.New("database is locked")
		},
	}
	if _, err := s.Capture(context.Background(), nil); err == nil {
		t.Fatal("a failed VPN capture produced a snapshot anyway")
	}
}

// "No profile was active" is a real state to restore, not an absence.
func TestSnapshot_CapturesTheAbsenceOfAProfile(t *testing.T) {
	s := &Snapshotter{
		Backend:    NewFakeBackend(),
		Paths:      Paths{StateDir: t.TempDir()},
		CaptureVPN: func(context.Context) (*domain.VPNProfile, error) { return nil, nil },
	}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.VPNCaptured || snap.VPNProfile != nil {
		t.Errorf("captured = %v / %+v", snap.VPNCaptured, snap.VPNProfile)
	}
}

func TestRestore_TearsTheTunnelDownWhenNoneWasActive(t *testing.T) {
	var got *domain.VPNProfile
	var called int
	s := &Snapshotter{
		Backend:    NewFakeBackend(),
		Paths:      Paths{StateDir: t.TempDir()},
		Restart:    func(context.Context) error { return nil },
		RestoreVPN: func(_ context.Context, p *domain.VPNProfile) error { called++; got = p; return nil },
	}
	if err := s.Restore(context.Background(), &Snapshot{VPNCaptured: true}); err != nil {
		t.Fatal(err)
	}
	if called != 1 || got != nil {
		t.Errorf("restore called %d times with %+v; want once with nil", called, got)
	}
}

func TestRestore_PutsTheOldProfileBack(t *testing.T) {
	var got *domain.VPNProfile
	s := &Snapshotter{
		Backend:    NewFakeBackend(),
		Paths:      Paths{StateDir: t.TempDir()},
		Restart:    func(context.Context) error { return nil },
		RestoreVPN: func(_ context.Context, p *domain.VPNProfile) error { got = p; return nil },
	}
	want := &domain.VPNProfile{ID: 3, Name: "old"}
	if err := s.Restore(context.Background(), &Snapshot{VPNCaptured: true, VPNProfile: want}); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != 3 {
		t.Errorf("got %+v, want the previously active profile", got)
	}
}

// A snapshot taken by a build that predates the tunnel must not be read as
// "there was no tunnel" and tear a working one down mid-upgrade.
func TestRestore_OlderSnapshotLeavesTheTunnelAlone(t *testing.T) {
	called := 0
	s := &Snapshotter{
		Backend:    NewFakeBackend(),
		Paths:      Paths{StateDir: t.TempDir()},
		Restart:    func(context.Context) error { return nil },
		RestoreVPN: func(context.Context, *domain.VPNProfile) error { called++; return nil },
	}
	if err := s.Restore(context.Background(), &Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Errorf("restore ran %d times on a snapshot that never recorded the tunnel", called)
	}
}

// A revert has to put the tunnel's own table back too, or the restored rules
// point into a table still holding the wrong default.
func TestTablesToSnapshot_IncludesTheTunnel(t *testing.T) {
	var found bool
	for _, tbl := range tablesToSnapshot {
		if tbl == WGTable {
			found = true
		}
	}
	if !found {
		t.Errorf("tablesToSnapshot = %v, missing the tunnel's table %d", tablesToSnapshot, WGTable)
	}
}
