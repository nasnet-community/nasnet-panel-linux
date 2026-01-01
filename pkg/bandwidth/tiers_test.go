package bandwidth

import "testing"

func TestTier_OutboundTag(t *testing.T) {
	if got := (Tier{RateMbit: 0}).OutboundTag(); got != "direct" {
		t.Errorf("unlimited tag = %q, want direct", got)
	}
	if got := (Tier{RateMbit: 30}).OutboundTag(); got != "direct-bw30" {
		t.Errorf("30Mbps tag = %q, want direct-bw30", got)
	}
}

func TestTier_TCClassID(t *testing.T) {
	// Mark==0 falls back to the default class 1:99.
	if got := (Tier{Mark: 0}).TCClassID(); got != "1:99" {
		t.Errorf("default class = %q", got)
	}
	if got := (Tier{Mark: 50}).TCClassID(); got != "1:50" {
		t.Errorf("mark=50 class = %q", got)
	}
}

func TestAllTiers_IncludesUnlimited(t *testing.T) {
	all := AllTiers()
	if len(all) == 0 {
		t.Fatal("AllTiers returned empty")
	}
	if all[0].RateMbit != 0 {
		t.Errorf("first tier should be unlimited (RateMbit=0), got %d", all[0].RateMbit)
	}
}

// RateLimitedTiers drops the unlimited tier so callers can iterate "real" caps.
func TestRateLimitedTiers_ExcludesUnlimited(t *testing.T) {
	for _, t2 := range RateLimitedTiers() {
		if t2.RateMbit == 0 {
			t.Errorf("unlimited tier leaked into RateLimitedTiers: %+v", t2)
		}
	}
	if len(RateLimitedTiers()) != len(AllTiers())-1 {
		t.Errorf("RateLimitedTiers should be one shorter than AllTiers")
	}
}

func TestGetTier(t *testing.T) {
	tests := []struct {
		name     string
		mbps     int
		wantRate int
	}{
		{"zero is unlimited", 0, 0},
		{"negative is unlimited", -1, 0},
		{"exact match returns same tier", 10, 10},
		{"slightly above rounds up to next tier", 11, 30},
		{"in-between rounds up", 25, 30},
		{"above all tiers caps at the highest", 9999, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetTier(tt.mbps); got.RateMbit != tt.wantRate {
				t.Errorf("GetTier(%d).RateMbit = %d, want %d", tt.mbps, got.RateMbit, tt.wantRate)
			}
		})
	}
}
