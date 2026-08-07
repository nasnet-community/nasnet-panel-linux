package system

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// netplan refuses to load a world-readable file, so 0644 everywhere is wrong.
func TestRestore_KeepsFileModes(t *testing.T) {
	p := tmpPaths(t)
	yaml := filepath.Join(p.NetplanDir, "50-cloud-init.yaml")
	if err := os.WriteFile(yaml, []byte("network: {version: 2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Snapshotter{Backend: NewFakeBackend(), Paths: p}
	snap, err := s.Capture(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.NetplanModes["50-cloud-init.yaml"]; got != 0o600 {
		t.Fatalf("captured mode = %#o, want 0600", got)
	}

	// Someone loosens it, then we revert.
	if err := os.Chmod(yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	info, err := os.Stat(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("restored mode = %#o, want 0600", got)
	}
}

func TestUnitsToUnmask(t *testing.T) {
	nm := "NetworkManager.service"
	wait := "NetworkManager-wait-online.service"

	// The takeover masked both; neither was masked before.
	got := unitsToUnmask([]string{nm, wait}, nil)
	if !slices.Equal(got, []string{nm, wait}) {
		t.Errorf("unmask = %v, want both units back", got)
	}

	// Already masked by the operator: leave it alone.
	if got := unitsToUnmask([]string{nm}, []string{nm}); len(got) != 0 {
		t.Errorf("unmask = %v, want nothing — it was masked before the apply", got)
	}
}
