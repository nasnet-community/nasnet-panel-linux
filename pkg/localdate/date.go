// Package localdate formats customer-facing dates without changing stored instants.
package localdate

import (
	"time"
	_ "time/tzdata" // Keep Tehran's historical zone rules available in minimal images.

	ptime "github.com/yaa110/go-persian-calendar"
)

var tehran = ptime.Iran()

// Date uses Jalali for Persian and preserves the existing English presentation.
func Date(t time.Time, lang string) string {
	return Format(t, lang, "Jan 02, 2006", false)
}

func DateTime(t time.Time, lang string) string {
	return Format(t, lang, "Jan 02, 2006 15:04", true)
}

// Format allows existing English layouts to stay unchanged. Persian dates
// always use the Jalali calendar, ASCII digits and Tehran local time, with an isolated digit
// run so dates are not reordered inside RTL Telegram messages.
func Format(t time.Time, lang, englishLayout string, withTime bool) string {
	if lang != "fa" {
		return t.Format(englishLayout)
	}
	format := "yyyy/MM/dd"
	if withTime {
		format += " HH:mm"
	}
	return "\u2066" + ptime.New(t.In(tehran)).Format(format) + "\u2069"
}
