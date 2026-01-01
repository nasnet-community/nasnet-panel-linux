package stats

import "testing"

func TestComputeDeltaWraparound(t *testing.T) {
	// current < last → treat as reset, return current.
	got := computeNetDelta(100, 1000)
	if got != 100 {
		t.Fatalf("wraparound: want 100, got %d", got)
	}
	// normal forward case
	got = computeNetDelta(1100, 1000)
	if got != 100 {
		t.Fatalf("normal: want 100, got %d", got)
	}
}
