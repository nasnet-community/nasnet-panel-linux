package nft

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestManager_UpdateAppliesRenderedRuleset(t *testing.T) {
	fa := &FakeApplier{}
	m := NewManager(fa)

	if err := m.Update(context.Background(), func(rs *Ruleset) { rs.Connmark = true }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(fa.Applied) != 1 {
		t.Fatalf("applied %d times, want 1", len(fa.Applied))
	}
	if !strings.Contains(fa.Applied[0], "chain mangle_post") {
		t.Errorf("applied ruleset lacks mangle_post:\n%s", fa.Applied[0])
	}
	if !m.Snapshot().Connmark {
		t.Error("Snapshot did not retain the mutation")
	}
}

// Two subsystems mutate the same table. Each Update must build on the last
// state, not replace it — otherwise enabling pins would silently drop
// bandwidth shaping's connmark rules.
func TestManager_UpdateAccumulates(t *testing.T) {
	fa := &FakeApplier{}
	m := NewManager(fa)
	ctx := context.Background()

	if err := m.Update(ctx, func(rs *Ruleset) { rs.Connmark = true }); err != nil {
		t.Fatal(err)
	}
	if err := m.Update(ctx, func(rs *Ruleset) {
		rs.IngressPins = []Pin{{IfName: "enp1s0", Index: 1}}
	}); err != nil {
		t.Fatal(err)
	}

	last := fa.Applied[len(fa.Applied)-1]
	if !strings.Contains(last, "mangle_post") {
		t.Error("second Update dropped the connmark chain")
	}
	if !strings.Contains(last, `iifname "enp1s0"`) {
		t.Error("second Update did not add the pin")
	}
}

// A failed apply must not leave the in-memory state claiming success, or the
// next Update would build on a ruleset the kernel never accepted.
func TestManager_UpdateRollsBackStateOnApplyError(t *testing.T) {
	fa := &FakeApplier{Err: errors.New("nft: syntax error")}
	m := NewManager(fa)

	err := m.Update(context.Background(), func(rs *Ruleset) { rs.Connmark = true })
	if err == nil {
		t.Fatal("Update returned nil on an applier error")
	}
	if m.Snapshot().Connmark {
		t.Error("state kept a mutation whose apply failed")
	}
}

func TestManager_TeardownDeletesTableAndClearsState(t *testing.T) {
	fa := &FakeApplier{}
	m := NewManager(fa)
	ctx := context.Background()
	if err := m.Update(ctx, func(rs *Ruleset) { rs.Connmark = true }); err != nil {
		t.Fatal(err)
	}
	if err := m.Teardown(ctx); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if fa.Deletes != 1 {
		t.Errorf("Deletes = %d, want 1", fa.Deletes)
	}
	if m.Snapshot().Connmark {
		t.Error("Teardown left stale state")
	}
}

func TestCmdApplier_UsesFileStdinForm(t *testing.T) {
	a := NewCmdApplier("")
	if a.Bin != "nft" {
		t.Errorf("default bin = %q, want nft", a.Bin)
	}
	if got := a.applyArgs(); strings.Join(got, " ") != "-f -" {
		t.Errorf("applyArgs = %v, want [-f -]", got)
	}
	if got := a.deleteArgs(); strings.Join(got, " ") != "delete table inet nasnet" {
		t.Errorf("deleteArgs = %v", got)
	}
}
