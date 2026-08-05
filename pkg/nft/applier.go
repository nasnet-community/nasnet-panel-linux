package nft

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Applier -> privileged edge of this package
// Production -> CmdApplier
// unprivileged CI -> FakeApplier
type Applier interface {
	Apply(ctx context.Context, ruleset string) error // Apply feeds a complete `nft -f -` script to nftables
	Delete(ctx context.Context) error
}

// CmdApplier shells out to the nft binary.
type CmdApplier struct {
	Bin string
}

// NewCmdApplier returns an applier using bin, or "nft" from $PATH when empty.
func NewCmdApplier(bin string) *CmdApplier {
	if bin == "" {
		bin = "nft"
	}
	return &CmdApplier{Bin: bin}
}

func (a *CmdApplier) applyArgs() []string { return []string{"-f", "-"} }

func (a *CmdApplier) deleteArgs() []string {
	return []string{"delete", "table", TableFamily, TableName}
}

func (a *CmdApplier) Apply(ctx context.Context, ruleset string) error {
	cmd := exec.CommandContext(ctx, a.Bin, a.applyArgs()...)
	cmd.Stdin = strings.NewReader(ruleset)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nft -f -: %w (output: %s)", err, strings.TrimSpace(out.String()))
	}
	return nil
}

func (a *CmdApplier) Delete(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.Bin, a.deleteArgs()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// "No such file or directory" means the table is already gone, which
		// is the state Delete promises to reach.
		if strings.Contains(string(out), "No such file or directory") {
			return nil
		}
		return fmt.Errorf("nft delete table: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// FakeApplier records what would have been applied. Safe for concurrent use.
type FakeApplier struct {
	mu      sync.Mutex
	Applied []string
	Deletes int
	Err     error
}

func (f *FakeApplier) Apply(_ context.Context, ruleset string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Applied = append(f.Applied, ruleset)
	return nil
}

func (f *FakeApplier) Delete(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Deletes++
	return nil
}
