package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNetCmd_Shape(t *testing.T) {
	c := newNetCmd()
	if c.Use != "net" {
		t.Errorf("Use = %q", c.Use)
	}
	var rollback *cobra.Command
	for _, sub := range c.Commands() {
		if strings.HasPrefix(sub.Use, "rollback") {
			rollback = sub
		}
	}
	if rollback == nil {
		t.Fatal("no rollback subcommand")
	}
	f := rollback.Flags().Lookup("if-expired")
	if f == nil {
		t.Fatal("no --if-expired flag")
	}
	if f.Value.String() != "false" {
		t.Errorf("--if-expired default = %q, want false", f.Value.String())
	}
	// The timer's only documentation for the next operator.
	if rollback.Long == "" {
		t.Error("rollback has no Long description")
	}
}

// Timer runs this ~8,640x/day with nothing pending; must exit 0 without a DB.
func TestRunNetRollback_NoMarkerExitsZeroWithoutADatabase(t *testing.T) {
	t.Setenv("NASNET_STATE_DIR", t.TempDir())
	if err := runNetRollback(true); err != nil {
		t.Fatalf("no-marker fast path returned an error: %v", err)
	}
}
