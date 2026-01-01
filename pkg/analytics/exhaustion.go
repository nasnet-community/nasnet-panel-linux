package analytics

import (
	"math"
	"time"

	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
)

// ExhaustionPrediction holds the computed exhaustion forecast for a subscription.
type ExhaustionPrediction struct {
	DataLimit        int64   `json:"data_limit"`
	DataUsed         int64   `json:"data_used"`
	DataRemaining    int64   `json:"data_remaining"`
	DailyAvgBytes    int64   `json:"daily_avg_bytes"`
	DaysRemaining    int     `json:"days_remaining"`
	EndDate          *string `json:"end_date"`
	DaysUntilExpiry  int     `json:"days_until_expiry"`
	ExhaustionDate   *string `json:"exhaustion_date"`
	WillExhaustFirst bool    `json:"will_exhaust_first"`
	UsageTrend       string  `json:"usage_trend"`
	Confidence       float64 `json:"confidence"`
	Unlimited        bool    `json:"unlimited"`
}

const (
	ewmaAlpha     = 0.3
	ewmaWindow    = 14
	trendMinCount = 10
	trendLastN    = 3
	trendPrevN    = 7
)

// ComputeExhaustion calculates a data exhaustion forecast from a subscription
// and its daily usage records. Records are per-day deltas (bytes used on that
// UTC calendar day), ordered by date ASC.
func ComputeExhaustion(sub *subDomain.Subscription, records []*subDomain.SubscriptionDailyUsage) *ExhaustionPrediction {
	effectiveLimit := sub.GetEffectiveDataLimit()
	nowUTC := time.Now().UTC()

	result := &ExhaustionPrediction{
		DataLimit: effectiveLimit,
		DataUsed:  sub.DataUsed,
	}

	if effectiveLimit == 0 {
		result.Unlimited = true
		result.UsageTrend = "stable"
		if ed := sub.GetEffectiveEndDate(); ed != nil {
			s := ed.UTC().Format("2006-01-02")
			result.EndDate = &s
			d := int(math.Ceil(time.Until(*ed).Hours() / 24))
			if d < 0 {
				d = 0
			}
			result.DaysUntilExpiry = d
		}
		return result
	}

	result.DataRemaining = effectiveLimit - sub.DataUsed
	if result.DataRemaining < 0 {
		result.DataRemaining = 0
	}

	if ed := sub.GetEffectiveEndDate(); ed != nil {
		s := ed.UTC().Format("2006-01-02")
		result.EndDate = &s
		d := int(math.Ceil(time.Until(*ed).Hours() / 24))
		if d < 0 {
			d = 0
		}
		result.DaysUntilExpiry = d
	}

	if len(records) == 0 {
		result.UsageTrend = "stable"
		result.Confidence = 0
		return result
	}

	// Records are per-day deltas. Missing dates = 0 usage that day.
	deltas := make([]int64, len(records))
	for i, r := range records {
		if r.DataUsed < 0 {
			deltas[i] = 0
		} else {
			deltas[i] = r.DataUsed
		}
	}

	result.DailyAvgBytes = ewmaAverage(deltas)
	result.UsageTrend = ComputeUsageTrend(deltas)
	result.Confidence = confidenceFor(len(deltas))

	if result.DataRemaining > 0 && result.DailyAvgBytes > 0 {
		daysFloat := float64(result.DataRemaining) / float64(result.DailyAvgBytes)
		result.DaysRemaining = int(math.Ceil(daysFloat))
		exhaust := nowUTC.Add(time.Duration(daysFloat * float64(24*time.Hour))).Format("2006-01-02")
		result.ExhaustionDate = &exhaust
		if sub.GetEffectiveEndDate() != nil {
			result.WillExhaustFirst = result.DaysRemaining < result.DaysUntilExpiry
		}
	}

	return result
}

// ewmaAverage returns exponentially-weighted mean over the last ewmaWindow
// deltas. Zero-usage days count as real signal. Older days decay by alpha.
func ewmaAverage(deltas []int64) int64 {
	n := len(deltas)
	if n == 0 {
		return 0
	}
	start := n - ewmaWindow
	if start < 0 {
		start = 0
	}
	avg := float64(deltas[start])
	for i := start + 1; i < n; i++ {
		avg = ewmaAlpha*float64(deltas[i]) + (1-ewmaAlpha)*avg
	}
	if avg < 0 {
		return 0
	}
	return int64(avg)
}

func confidenceFor(n int) float64 {
	switch {
	case n >= 8:
		return 0.9
	case n >= 4:
		return 0.6
	case n >= 1:
		return 0.3
	}
	return 0
}

// ComputeUsageTrend compares last 3 days vs the preceding 7 days and returns
// "increasing", "decreasing", or "stable". Requires at least 10 deltas to
// avoid drawing conclusions from noisy short windows.
func ComputeUsageTrend(deltas []int64) string {
	if len(deltas) < trendMinCount {
		return "stable"
	}

	n := len(deltas)
	last3 := deltas[n-trendLastN:]
	prev := deltas[n-trendLastN-trendPrevN : n-trendLastN]

	var last3Sum, prevSum int64
	for _, d := range last3 {
		last3Sum += d
	}
	for _, d := range prev {
		prevSum += d
	}
	last3Avg := float64(last3Sum) / float64(trendLastN)
	prevAvg := float64(prevSum) / float64(trendPrevN)

	if prevAvg == 0 {
		if last3Avg > 0 {
			return "increasing"
		}
		return "stable"
	}

	ratio := last3Avg / prevAvg
	switch {
	case ratio > 1.2:
		return "increasing"
	case ratio < 0.8:
		return "decreasing"
	default:
		return "stable"
	}
}
