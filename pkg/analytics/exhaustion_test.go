package analytics

import (
	"testing"
	"time"

	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
)

func ptrTime(t time.Time) *time.Time { return &t }

func makeSub(limit, used int64, endDate *time.Time) *subDomain.Subscription {
	return &subDomain.Subscription{
		DataLimit: limit,
		DataUsed:  used,
		EndDate:   endDate,
	}
}

func makeRecords(deltas []int64) []*subDomain.SubscriptionDailyUsage {
	base := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -len(deltas))
	rec := make([]*subDomain.SubscriptionDailyUsage, len(deltas))
	for i, d := range deltas {
		rec[i] = &subDomain.SubscriptionDailyUsage{
			SubscriptionID: 1,
			Date:           base.AddDate(0, 0, i),
			DataUsed:       d,
		}
	}
	return rec
}

func TestComputeExhaustion_Unlimited(t *testing.T) {
	sub := makeSub(0, 0, ptrTime(time.Now().Add(72*time.Hour)))
	p := ComputeExhaustion(sub, nil)
	if !p.Unlimited {
		t.Fatal("expected unlimited=true")
	}
	if p.UsageTrend != "stable" {
		t.Errorf("unlimited trend should be stable, got %q", p.UsageTrend)
	}
	if p.DaysUntilExpiry < 2 || p.DaysUntilExpiry > 4 {
		t.Errorf("unexpected DaysUntilExpiry=%d", p.DaysUntilExpiry)
	}
}

func TestComputeExhaustion_UnlimitedExpiredClamped(t *testing.T) {
	sub := makeSub(0, 0, ptrTime(time.Now().Add(-48*time.Hour)))
	p := ComputeExhaustion(sub, nil)
	if p.DaysUntilExpiry != 0 {
		t.Errorf("expired unlimited DaysUntilExpiry should clamp to 0, got %d", p.DaysUntilExpiry)
	}
}

func TestComputeExhaustion_EmptyRecords(t *testing.T) {
	sub := makeSub(1<<30, 0, nil)
	p := ComputeExhaustion(sub, nil)
	if p.Confidence != 0 {
		t.Errorf("empty records confidence should be 0, got %v", p.Confidence)
	}
	if p.ExhaustionDate != nil {
		t.Error("empty records should not predict exhaustion date")
	}
	if p.DaysRemaining != 0 {
		t.Errorf("DaysRemaining should be 0, got %d", p.DaysRemaining)
	}
}

func TestComputeExhaustion_SingleDay(t *testing.T) {
	sub := makeSub(1<<30, 0, nil) // 1 GiB limit
	recs := makeRecords([]int64{100 * 1024 * 1024})
	p := ComputeExhaustion(sub, recs)
	if p.Confidence != 0.3 {
		t.Errorf("single-day confidence should be 0.3, got %v", p.Confidence)
	}
	if p.DailyAvgBytes == 0 {
		t.Error("single-day avg should equal that day's delta")
	}
	if p.ExhaustionDate == nil {
		t.Error("should predict exhaustion date with nonzero avg")
	}
}

func TestComputeExhaustion_DataAlreadyExhausted(t *testing.T) {
	sub := makeSub(1000, 1500, nil) // used > limit
	recs := makeRecords([]int64{100, 200, 300})
	p := ComputeExhaustion(sub, recs)
	if p.DataRemaining != 0 {
		t.Errorf("DataRemaining should clamp to 0, got %d", p.DataRemaining)
	}
	if p.ExhaustionDate != nil {
		t.Error("should not set exhaustion date when DataRemaining=0")
	}
	if p.WillExhaustFirst {
		t.Error("WillExhaustFirst should be false when already exhausted")
	}
}

func TestComputeExhaustion_FloatCeil(t *testing.T) {
	// 99 bytes remaining, 100 bytes/day avg → ceil(0.99) = 1 day
	sub := makeSub(1099, 1000, nil)
	recs := makeRecords([]int64{100, 100, 100, 100, 100, 100, 100, 100})
	p := ComputeExhaustion(sub, recs)
	if p.DataRemaining != 99 {
		t.Fatalf("DataRemaining=%d want 99", p.DataRemaining)
	}
	if p.DaysRemaining != 1 {
		t.Errorf("DaysRemaining=%d want 1 (ceil)", p.DaysRemaining)
	}
}

func TestComputeExhaustion_WillExhaustFirst(t *testing.T) {
	// plenty of days until expiry, fast burn rate → exhausts first
	end := time.Now().Add(30 * 24 * time.Hour)
	sub := makeSub(10_000, 0, &end)
	recs := makeRecords([]int64{1000, 1000, 1000, 1000, 1000, 1000, 1000, 1000})
	p := ComputeExhaustion(sub, recs)
	if !p.WillExhaustFirst {
		t.Error("should flag WillExhaustFirst when burn rate outpaces expiry")
	}
	if p.DaysRemaining >= p.DaysUntilExpiry {
		t.Errorf("DaysRemaining=%d should be < DaysUntilExpiry=%d", p.DaysRemaining, p.DaysUntilExpiry)
	}
}

func TestComputeUsageTrend_InsufficientSamples(t *testing.T) {
	// fewer than trendMinCount samples → stable regardless of shape
	if got := ComputeUsageTrend([]int64{1, 2, 3, 4, 5, 6, 7, 8, 9}); got != "stable" {
		t.Errorf("<10 samples should be stable, got %q", got)
	}
}

func TestComputeUsageTrend_Increasing(t *testing.T) {
	// last 3 (avg 200) vs prev 7 (avg 100): ratio 2.0 → increasing
	deltas := []int64{100, 100, 100, 100, 100, 100, 100, 200, 200, 200}
	if got := ComputeUsageTrend(deltas); got != "increasing" {
		t.Errorf("want increasing, got %q", got)
	}
}

func TestComputeUsageTrend_Decreasing(t *testing.T) {
	deltas := []int64{200, 200, 200, 200, 200, 200, 200, 50, 50, 50}
	if got := ComputeUsageTrend(deltas); got != "decreasing" {
		t.Errorf("want decreasing, got %q", got)
	}
}

func TestComputeUsageTrend_Stable(t *testing.T) {
	deltas := []int64{100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	if got := ComputeUsageTrend(deltas); got != "stable" {
		t.Errorf("want stable, got %q", got)
	}
}

func TestComputeUsageTrend_PrevZeroLastNonzero(t *testing.T) {
	deltas := []int64{0, 0, 0, 0, 0, 0, 0, 50, 50, 50}
	if got := ComputeUsageTrend(deltas); got != "increasing" {
		t.Errorf("want increasing when prev=0 last>0, got %q", got)
	}
}

func TestEwmaAverage_IncludesZeros(t *testing.T) {
	// Last sample is 0; EWMA should drag avg down, not skip it.
	deltas := []int64{100, 100, 100, 100, 100, 100, 100, 0}
	avg := ewmaAverage(deltas)
	if avg == 100 {
		t.Error("EWMA must account for trailing zero, not drop it")
	}
	if avg == 0 {
		t.Error("EWMA must retain prior signal, not collapse to latest value")
	}
}

func TestEwmaAverage_Empty(t *testing.T) {
	if avg := ewmaAverage(nil); avg != 0 {
		t.Errorf("empty input avg should be 0, got %d", avg)
	}
}

func TestComputeExhaustion_NegativeDeltaClamped(t *testing.T) {
	sub := makeSub(1<<30, 0, nil)
	recs := makeRecords([]int64{100, -50, 100})
	p := ComputeExhaustion(sub, recs)
	if p.DailyAvgBytes == 0 {
		t.Error("should still compute avg after clamping negative delta")
	}
}
