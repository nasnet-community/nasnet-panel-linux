package traffic

import (
	"testing"
	"time"
)

// A core-tagged snapshot lands in BOTH the merged per-email maps (aggregate) AND
// the per-(protocol,email) CoreUsage map. Two protocols under one email stay
// separate in CoreUsage but sum in UserUplink — this is what makes multi-core
// subs exact per tunnel.
func TestAccumulate_CoreUsageKeptPerProtocol(t *testing.T) {
	s := NewMemoryStore(time.Hour, 7*24*time.Hour)
	now := time.Unix(1_700_000_000, 0)

	s.Accumulate(&XrayStatsSnapshot{
		UserUplink:   map[string]int64{"main": 130}, // mtproto100 + openvpn30
		UserDownlink: map[string]int64{"main": 70},  // mtproto50 + openvpn20
		CoreUsage: map[string]map[string]CoreUsageCounts{
			"mtproto": {"main": {Uplink: 100, Downlink: 50}},
			"openvpn": {"main": {Uplink: 30, Downlink: 20}},
		},
	}, now)

	buckets := s.GetAll()
	if len(buckets) != 1 {
		t.Fatalf("want 1 bucket, got %d", len(buckets))
	}
	b := buckets[0]
	if b.UserUplink["main"] != 130 || b.UserDownlink["main"] != 70 {
		t.Fatalf("aggregate wrong: up=%d down=%d", b.UserUplink["main"], b.UserDownlink["main"])
	}
	if got := b.CoreUsage["mtproto"]["main"]; got.Uplink != 100 || got.Downlink != 50 {
		t.Fatalf("mtproto core usage = %+v, want {100,50}", got)
	}
	if got := b.CoreUsage["openvpn"]["main"]; got.Uplink != 30 || got.Downlink != 20 {
		t.Fatalf("openvpn core usage = %+v, want {30,20}", got)
	}
}

// Accumulate merges same-key CoreUsage across ticks, and deepCopy (via GetAll)
// must not alias the store's inner maps.
func TestAccumulate_CoreUsageMergesAndDeepCopies(t *testing.T) {
	s := NewMemoryStore(time.Hour, 7*24*time.Hour)
	now := time.Unix(1_700_000_000, 0)
	snap := func() *XrayStatsSnapshot {
		return &XrayStatsSnapshot{
			UserUplink: map[string]int64{"main": 10},
			CoreUsage:  map[string]map[string]CoreUsageCounts{"mtproto": {"main": {Uplink: 10}}},
		}
	}
	s.Accumulate(snap(), now)
	s.Accumulate(snap(), now)
	if got := s.GetAll()[0].CoreUsage["mtproto"]["main"].Uplink; got != 20 {
		t.Fatalf("want merged 20, got %d", got)
	}
	// Mutating the returned (copied) bucket must not affect the store.
	b := s.GetAll()[0]
	b.CoreUsage["mtproto"]["main"] = CoreUsageCounts{Uplink: 999}
	if got := s.GetAll()[0].CoreUsage["mtproto"]["main"].Uplink; got != 20 {
		t.Fatalf("deepCopy leaked: store mutated to %d", got)
	}
}

// A plain (Xray) snapshot leaves CoreUsage empty — no phantom core attribution
// for pure-Xray emails.
func TestAccumulate_PlainSnapshotLeavesCoreUsageEmpty(t *testing.T) {
	s := NewMemoryStore(time.Hour, 7*24*time.Hour)
	s.Accumulate(&XrayStatsSnapshot{UserUplink: map[string]int64{"x": 10}}, time.Unix(1_700_000_000, 0))
	if b := s.GetAll()[0]; len(b.CoreUsage) != 0 {
		t.Fatalf("core usage should be empty for a plain snapshot, got %v", b.CoreUsage)
	}
}
