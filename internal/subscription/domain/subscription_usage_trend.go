package domain

import "time"

// UsageTrend is the 7d / 30d per-day upload/download series served to the user panel.
type UsageTrend struct {
	Range    string            // "7d" | "30d"
	Points   []UsageTrendPoint // only days with a row; frontend fills gaps
	UnitHint string            // "KB" | "MB" | "GB" — picked from max Total in range
}

// UsageTrendPoint is one day of traffic, with upload, download and combined bytes.
type UsageTrendPoint struct {
	Date     time.Time // midnight UTC
	Upload   int64
	Download int64
	Total    int64
}
