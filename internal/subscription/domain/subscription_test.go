package domain

import (
	"testing"
	"time"
)

func int64Ptr(v int64) *int64 { return &v }
func intPtr(v int) *int       { return &v }

// fromNow returns a pointer to a time offset from now; helpers keep the
// expiry-math tests readable.
func fromNow(d time.Duration) *time.Time {
	t := time.Now().Add(d)
	return &t
}

func TestSubscription_GetEffectiveDataLimit(t *testing.T) {
	// Custom override wins, including an explicit 0 meaning "unlimited".
	if got := (&Subscription{DataLimit: 100, CustomDataLimit: int64Ptr(0)}).GetEffectiveDataLimit(); got != 0 {
		t.Errorf("custom 0 override = %d, want 0", got)
	}
	if got := (&Subscription{DataLimit: 100, CustomDataLimit: int64Ptr(500)}).GetEffectiveDataLimit(); got != 500 {
		t.Errorf("custom override = %d, want 500", got)
	}
	if got := (&Subscription{DataLimit: 100}).GetEffectiveDataLimit(); got != 100 {
		t.Errorf("plan default = %d, want 100", got)
	}
}

func TestSubscription_GetEffectiveEndDate(t *testing.T) {
	end := fromNow(24 * time.Hour)
	// Non-custom uses EndDate.
	if got := (&Subscription{EndDate: end}).GetEffectiveEndDate(); got != end {
		t.Error("non-custom should return EndDate")
	}
	// Custom flag with nil date means unlimited (no expiry).
	if got := (&Subscription{IsEndDateCustom: true, EndDate: end}).GetEffectiveEndDate(); got != nil {
		t.Error("custom nil end date should be unlimited (nil)")
	}
}

func TestSubscription_IsExpired(t *testing.T) {
	if (&Subscription{}).IsExpired() {
		t.Error("no end date should never be expired")
	}
	if !(&Subscription{EndDate: fromNow(-time.Hour)}).IsExpired() {
		t.Error("past end date should be expired")
	}
	if (&Subscription{EndDate: fromNow(time.Hour)}).IsExpired() {
		t.Error("future end date should not be expired")
	}
}

func TestSubscription_IsDataExhausted(t *testing.T) {
	if (&Subscription{DataLimit: 0, DataUsed: 999}).IsDataExhausted() {
		t.Error("unlimited plan can't be exhausted")
	}
	if !(&Subscription{DataLimit: 100, DataUsed: 100}).IsDataExhausted() {
		t.Error("usage at limit should be exhausted")
	}
	if (&Subscription{DataLimit: 100, DataUsed: 99}).IsDataExhausted() {
		t.Error("usage below limit should not be exhausted")
	}
}

func TestSubscription_RemainingData(t *testing.T) {
	if got := (&Subscription{DataLimit: 0}).RemainingData(); got != -1 {
		t.Errorf("unlimited remaining = %d, want -1", got)
	}
	if got := (&Subscription{DataLimit: 100, DataUsed: 30}).RemainingData(); got != 70 {
		t.Errorf("remaining = %d, want 70", got)
	}
	if got := (&Subscription{DataLimit: 100, DataUsed: 150}).RemainingData(); got != 0 {
		t.Errorf("over-limit remaining = %d, want 0", got)
	}
}

func TestSubscription_DaysRemaining(t *testing.T) {
	if got := (&Subscription{}).DaysRemaining(); got != -1 {
		t.Errorf("no end date = %d, want -1", got)
	}
	if got := (&Subscription{EndDate: fromNow(-time.Hour)}).DaysRemaining(); got != 0 {
		t.Errorf("expired = %d, want 0", got)
	}
	// Ceiling: any partial day counts as a full day.
	if got := (&Subscription{EndDate: fromNow(time.Hour)}).DaysRemaining(); got != 1 {
		t.Errorf("1h left = %d, want 1", got)
	}
	if got := (&Subscription{EndDate: fromNow(36 * time.Hour)}).DaysRemaining(); got != 2 {
		t.Errorf("36h left = %d, want 2", got)
	}
}

func TestSubscription_TimeRemainingFormatted(t *testing.T) {
	tests := []struct {
		name string
		end  *time.Time
		want string
	}{
		{"unlimited", nil, "Unlimited"},
		{"expired", fromNow(-time.Hour), "Expired"},
		{"days and hours", fromNow(50*time.Hour + 30*time.Minute), "2 days 2 hours"},
		{"whole days", fromNow(48*time.Hour + 30*time.Minute), "2 days"},
		{"hours only", fromNow(5*time.Hour + 30*time.Minute), "5 hours"},
		{"minutes", fromNow(40*time.Minute + 30*time.Second), "40 min"},
		{"sub-minute", fromNow(30 * time.Second), "Less than 1 min"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Subscription{EndDate: tt.end}
			if got := s.TimeRemainingFormatted(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSubscription_GetDisplayName(t *testing.T) {
	if got := (&Subscription{Label: "My VPN"}).GetDisplayName(); got != "My VPN" {
		t.Errorf("label = %q", got)
	}
	if got := (&Subscription{}).GetDisplayName(); got != "Subscription" {
		t.Errorf("fallback = %q", got)
	}
}

func TestSubscription_GetLinkKey(t *testing.T) {
	if got := (&Subscription{LinkKey: "lk", ConfigID: "cfg"}).GetLinkKey(); got != "lk" {
		t.Errorf("link key = %q, want lk", got)
	}
	// Older subscriptions without LinkKey fall back to ConfigID.
	if got := (&Subscription{ConfigID: "cfg"}).GetLinkKey(); got != "cfg" {
		t.Errorf("fallback = %q, want cfg", got)
	}
}

func TestSubscription_GetUserID(t *testing.T) {
	if got := (&Subscription{}).GetUserID(); got != 0 {
		t.Errorf("nil user = %d, want 0", got)
	}
	if got := (&Subscription{UserID: uintPtr(7)}).GetUserID(); got != 7 {
		t.Errorf("user = %d, want 7", got)
	}
}

func TestSubscription_GetEffectiveBandwidthLimit(t *testing.T) {
	if got := (&Subscription{CustomBandwidthLimit: intPtr(50)}).GetEffectiveBandwidthLimit(); got != 50 {
		t.Errorf("custom = %d, want 50", got)
	}
	// No custom override means unlimited (0) — there is no plan-level default
	// to fall back to any more.
	if got := (&Subscription{}).GetEffectiveBandwidthLimit(); got != 0 {
		t.Errorf("none = %d, want 0", got)
	}
}

func TestSubscription_GetDataUsagePercentage(t *testing.T) {
	if got := (&Subscription{DataLimit: 0}).GetDataUsagePercentage(); got != 0 {
		t.Errorf("unlimited = %v, want 0", got)
	}
	if got := (&Subscription{DataLimit: 100, DataUsed: 50}).GetDataUsagePercentage(); got != 50 {
		t.Errorf("half = %v, want 50", got)
	}
	// Over-usage is capped at 100.
	if got := (&Subscription{DataLimit: 100, DataUsed: 150}).GetDataUsagePercentage(); got != 100 {
		t.Errorf("capped = %v, want 100", got)
	}
}

func TestSubscription_IsApproachingDataLimit(t *testing.T) {
	if (&Subscription{DataLimit: 0}).IsApproachingDataLimit(75) {
		t.Error("unlimited never approaches")
	}
	if !(&Subscription{DataLimit: 100, DataUsed: 80}).IsApproachingDataLimit(75) {
		t.Error("80% should cross a 75% threshold")
	}
	if (&Subscription{DataLimit: 100, DataUsed: 80}).IsApproachingDataLimit(90) {
		t.Error("80% should not cross a 90% threshold")
	}
}

func TestSubscription_GetDataWarningLevelString(t *testing.T) {
	tests := []struct {
		used int64
		want string
	}{
		{100, "exhausted"},
		{95, "critical"},
		{80, "warning"},
		{60, "notice"},
		{10, "none"},
	}
	for _, tt := range tests {
		s := &Subscription{DataLimit: 100, DataUsed: tt.used}
		if got := s.GetDataWarningLevelString(); got != tt.want {
			t.Errorf("used %d: got %q, want %q", tt.used, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{-1, "Unlimited"},
		{1024, "1 KB"},
		{1572864, "1.50 MB"}, // 1.5 MB
		{1073741824, "1 GB"},
		{5368709120, "5 GB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.bytes); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	if got := formatFloat(2.0); got != "2" {
		t.Errorf("whole number = %q, want 2", got)
	}
	if got := formatFloat(2.5); got != "2.50" {
		t.Errorf("fraction = %q, want 2.50", got)
	}
}

func TestSubscription_FormattedDataStrings(t *testing.T) {
	unlimited := &Subscription{DataLimit: 0, DataUsed: 1073741824}
	if got := unlimited.GetFormattedDataUsage(); got != "1 GB / Unlimited" {
		t.Errorf("unlimited usage = %q", got)
	}
	if got := unlimited.GetRemainingDataFormatted(); got != "Unlimited" {
		t.Errorf("unlimited remaining = %q", got)
	}

	limited := &Subscription{DataLimit: 2147483648, DataUsed: 1073741824} // 2 GB limit, 1 GB used
	if got := limited.GetFormattedDataUsage(); got != "1 GB / 2 GB" {
		t.Errorf("usage = %q", got)
	}
	if got := limited.GetRemainingDataFormatted(); got != "1 GB" {
		t.Errorf("remaining = %q", got)
	}

	over := &Subscription{DataLimit: 100, DataUsed: 150}
	if got := over.GetRemainingDataFormatted(); got != "0 B" {
		t.Errorf("over-limit remaining = %q, want 0 B", got)
	}
}

func TestSubscription_ToSubscriptionInfo(t *testing.T) {
	s := &Subscription{
		ID:          9,
		UserID:      uintPtr(4),
		ConfigID:    "uuid-1",
		ConfigEmail: "user@x",
		DataLimit:   500,
		// BandwidthLimit now comes solely from the per-subscription override.
		CustomBandwidthLimit: intPtr(25),
	}
	info := s.ToSubscriptionInfo()
	if info.ID != 9 || info.UserID != 4 || info.ConfigID != "uuid-1" || info.Email != "user@x" {
		t.Errorf("mapped info = %+v", info)
	}
	if info.DataLimit != 500 || info.BandwidthLimit != 25 {
		t.Errorf("limits = %+v", info)
	}
	// Nil user id maps to 0 rather than panicking.
	if got := (&Subscription{}).ToSubscriptionInfo().UserID; got != 0 {
		t.Errorf("nil user id = %d, want 0", got)
	}
}

func uintPtr(v uint) *uint { return &v }
