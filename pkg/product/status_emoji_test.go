package product

import "testing"

func TestStatusEmoji(t *testing.T) {
	tests := map[string]string{
		"active":            "🟢",
		"paused":            "⏸️",
		"expired":           "🔴",
		"traffic_exhausted": "⚠️",
		"pending":           "⏳",
		"cancelled":         "❌",
		"":                  "⚪", // unknown / empty → fallback
		"garbage":           "⚪",
	}
	for status, want := range tests {
		if got := StatusEmoji(status); got != want {
			t.Errorf("StatusEmoji(%q) = %q, want %q", status, got, want)
		}
	}
}
