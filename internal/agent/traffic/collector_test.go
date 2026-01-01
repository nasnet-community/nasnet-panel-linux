package traffic

import (
	"testing"
	"time"
)

func TestDiffSnapshot_NoBaseline_ReturnsFullCurrent(t *testing.T) {
	curr := &XrayStatsSnapshot{
		UserUplink:    map[string]int64{"alice": 100},
		UserDownlink:  map[string]int64{"alice": 50},
		TotalUplink:   100,
		TotalDownlink: 50,
	}
	got := diffSnapshot(nil, curr)
	if got.TotalUplink != 100 || got.TotalDownlink != 50 {
		t.Fatalf("totals: got up=%d down=%d", got.TotalUplink, got.TotalDownlink)
	}
	if got.UserUplink["alice"] != 100 {
		t.Fatalf("alice uplink: got %d want 100", got.UserUplink["alice"])
	}
}

func TestDiffSnapshot_NilCurrent_ReturnsEmpty(t *testing.T) {
	got := diffSnapshot(&XrayStatsSnapshot{TotalUplink: 10}, nil)
	if !snapshotIsEmpty(got) {
		t.Fatalf("expected empty snapshot")
	}
}

func TestDiffSnapshot_NormalProgress(t *testing.T) {
	prev := &XrayStatsSnapshot{
		UserUplink:    map[string]int64{"alice": 100, "bob": 200},
		InboundUplink: map[string]int64{"in-1": 300},
		TotalUplink:   600,
		TotalDownlink: 0,
	}
	curr := &XrayStatsSnapshot{
		UserUplink:    map[string]int64{"alice": 150, "bob": 200, "carol": 25},
		InboundUplink: map[string]int64{"in-1": 375},
		TotalUplink:   750,
		TotalDownlink: 0,
	}
	got := diffSnapshot(prev, curr)

	if got.UserUplink["alice"] != 50 {
		t.Fatalf("alice: got %d want 50", got.UserUplink["alice"])
	}
	if _, ok := got.UserUplink["bob"]; ok {
		t.Fatalf("bob should be omitted (zero delta)")
	}
	if got.UserUplink["carol"] != 25 {
		t.Fatalf("carol: got %d want 25", got.UserUplink["carol"])
	}
	if got.InboundUplink["in-1"] != 75 {
		t.Fatalf("in-1: got %d want 75", got.InboundUplink["in-1"])
	}
	if got.TotalUplink != 150 {
		t.Fatalf("total uplink: got %d want 150", got.TotalUplink)
	}
}

func TestDiffSnapshot_XrayRestart_CounterBackwards(t *testing.T) {
	// prev observed 1000 bytes cumulative. Xray restarts; current shows 25
	// (fresh counter). Collector must treat 25 as the delta (not overflow,
	// not negative, not zero).
	prev := &XrayStatsSnapshot{
		UserUplink:  map[string]int64{"alice": 1000},
		TotalUplink: 1000,
	}
	curr := &XrayStatsSnapshot{
		UserUplink:  map[string]int64{"alice": 25},
		TotalUplink: 25,
	}
	got := diffSnapshot(prev, curr)
	if got.UserUplink["alice"] != 25 {
		t.Fatalf("after restart alice: got %d want 25", got.UserUplink["alice"])
	}
	if got.TotalUplink != 25 {
		t.Fatalf("after restart total: got %d want 25", got.TotalUplink)
	}
}

func TestDiffMap_SkipsZeroAndNegative(t *testing.T) {
	got := diffMap(map[string]int64{"a": 10}, map[string]int64{"a": 10, "b": 0, "c": -5})
	if _, ok := got["a"]; ok {
		t.Fatalf("a: unchanged, should be omitted")
	}
	if _, ok := got["b"]; ok {
		t.Fatalf("b: zero current, should be omitted")
	}
	if _, ok := got["c"]; ok {
		t.Fatalf("c: negative current, should be omitted")
	}
}

func TestMemoryStore_BaselineRoundtrip(t *testing.T) {
	s := NewMemoryStore(time.Hour, 7*24*time.Hour)
	if s.GetBaseline() != nil {
		t.Fatalf("expected nil baseline on fresh store")
	}
	snap := &XrayStatsSnapshot{
		UserUplink:  map[string]int64{"alice": 42},
		TotalUplink: 42,
	}
	s.SetBaseline(snap)

	// Mutating original must not affect stored copy.
	snap.UserUplink["alice"] = 999
	snap.TotalUplink = 999

	got := s.GetBaseline()
	if got.UserUplink["alice"] != 42 {
		t.Fatalf("baseline alice: got %d want 42 (deep copy expected)", got.UserUplink["alice"])
	}
	if got.TotalUplink != 42 {
		t.Fatalf("baseline total: got %d want 42", got.TotalUplink)
	}
}
