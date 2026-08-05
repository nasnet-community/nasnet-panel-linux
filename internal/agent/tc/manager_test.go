package tc

import (
	"context"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// A tc fw filter handle must carry the tier mask, so a packet also carrying a
// group and an ingress pin still matches its tier class.
func TestFilterHandle_IsMaskedToTheTierField(t *testing.T) {
	m, err := NewManager("eth0", 1000, nft.NewManager(&nft.FakeApplier{}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	got := m.FilterHandle(50)
	want := "0x32/" + netmark.Hex(netmark.MaskTier)
	if got != want {
		t.Errorf("FilterHandle(50) = %q, want %q", got, want)
	}
	if !strings.Contains(got, "/") {
		t.Error("filter handle has no mask — a group-marked packet would miss its class")
	}
}

// Every rate-limited tier must produce a distinct handle, and none may collide
// with another field.
func TestFilterHandle_CoversEveryTierWithoutTouchingOtherFields(t *testing.T) {
	m, err := NewManager("eth0", 1000, nft.NewManager(&nft.FakeApplier{}))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tier := range bandwidth.RateLimitedTiers() {
		h := m.FilterHandle(tier.Mark)
		if seen[h] {
			t.Errorf("duplicate handle %q", h)
		}
		seen[h] = true
		if tier.Mark&^netmark.MaskTier != 0 {
			t.Errorf("tier mark %d does not fit the tier field", tier.Mark)
		}
	}
	if len(seen) != len(bandwidth.RateLimitedTiers()) {
		t.Errorf("got %d handles for %d tiers", len(seen), len(bandwidth.RateLimitedTiers()))
	}
}

// The connmark rules must land in the owned nft table, not in iptables, so
// teardown is a table replace and the mark is masked.
func TestSetupConnmark_EnablesTheOwnedTableChains(t *testing.T) {
	fa := &nft.FakeApplier{}
	nm := nft.NewManager(fa)
	m, err := NewManager("eth0", 1000, nm)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.setupConnmark(context.Background()); err != nil {
		t.Fatalf("setupConnmark: %v", err)
	}
	if !nm.Snapshot().Connmark {
		t.Fatal("connmark not enabled on the owned ruleset")
	}
	if len(fa.Applied) == 0 {
		t.Fatal("nothing applied")
	}
	last := fa.Applied[len(fa.Applied)-1]
	if !strings.Contains(last, netmark.Hex(netmark.MaskAll)) {
		t.Errorf("connmark rules are not masked to MaskAll:\n%s", last)
	}
}

func TestNewManager_RejectsMissingNftManager(t *testing.T) {
	if _, err := NewManager("eth0", 1000, nil); err == nil {
		t.Fatal("NewManager accepted a nil nft.Manager")
	}
}
