package domain

import "time"

// UsageTrend is the 7d / 30d per-day upload/download series served to the user panel.
type UsageTrend struct {
	Range    string            // "7d" | "30d"
	Points   []UsageTrendPoint // only days with a row; frontend fills gaps
	UnitHint string            // "KB" | "MB" | "GB" — picked from max Total in range
}

// UsageTrendPoint is one day of traffic. Upload/Download are nil on legacy
// rows (pre-migration). Total always reflects the combined bytes.
type UsageTrendPoint struct {
	Date     time.Time // midnight UTC
	Upload   *int64    // nil on legacy rows
	Download *int64    // nil on legacy rows
	Total    int64
}
