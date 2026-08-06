package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTakeoverOps_MovesNetplanAsideAndReportsDone(t *testing.T) {
	p := tmpPaths(t)
	yaml := filepath.Join(p.NetplanDir, "50-cloud-init.yaml")
	if err := os.WriteFile(yaml, []byte("network:\n  version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if TakeoverDone(p) {
		t.Fatal("TakeoverDone true before the takeover ran")
	}

	ops := TakeoverOps(p)
	if len(ops) == 0 {
		t.Fatal("no takeover ops")
	}
	for _, op := range ops {
		if op.Desc == "" {
			t.Error("an op has no description; the UI shows these before applying")
		}
		if err := op.Do(context.Background()); err != nil {
			t.Fatalf("op %q: %v", op.Desc, err)
		}
	}

	if _, err := os.Stat(yaml); !os.IsNotExist(err) {
		t.Error("netplan YAML was not moved aside")
	}
	moved := filepath.Join(p.NetplanDisabledDir, "50-cloud-init.yaml")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("netplan YAML is not in netplan.disabled: %v", err)
	}
	if !TakeoverDone(p) {
		t.Error("TakeoverDone false after the takeover ran")
	}
}

// The reconciler runs at every boot, so running twice must be safe.
func TestTakeoverOps_Idempotent(t *testing.T) {
	p := tmpPaths(t)
	if err := os.WriteFile(filepath.Join(p.NetplanDir, "a.yaml"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		for _, op := range TakeoverOps(p) {
			if err := op.Do(context.Background()); err != nil {
				t.Fatalf("pass %d, op %q: %v", i, op.Desc, err)
			}
		}
	}
}

// The revert path is the exact inverse.
func TestTakeoverOps_UndoPutsNetplanBack(t *testing.T) {
	p := tmpPaths(t)
	if err := os.WriteFile(filepath.Join(p.NetplanDir, "a.yaml"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := TakeoverOps(p)
	for _, op := range ops {
		if err := op.Do(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].Undo == nil {
			continue
		}
		if err := ops[i].Undo(context.Background()); err != nil {
			t.Fatalf("undo %q: %v", ops[i].Desc, err)
		}
	}
	if _, err := os.Stat(filepath.Join(p.NetplanDir, "a.yaml")); err != nil {
		t.Errorf("undo did not restore the netplan YAML: %v", err)
	}
}

// Every path an op writes must come from Paths — op.Do runs for real in tests.
func TestTakeoverOps_StayInsideTheGivenPaths(t *testing.T) {
	p := tmpPaths(t)
	stale := filepath.Join(p.RunNetworkdDir, "10-netplan-eth0.network")
	if err := os.MkdirAll(p.RunNetworkdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, op := range TakeoverOps(p) {
		if err := op.Do(context.Background()); err != nil {
			t.Fatalf("op %q: %v", op.Desc, err)
		}
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("netplan's generated unit was not removed from RunNetworkdDir")
	}
	marker := filepath.Join(p.CloudInitDir, cloudInitDropIn)
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("cloud-init drop-in not written under CloudInitDir: %v", err)
	}
}
