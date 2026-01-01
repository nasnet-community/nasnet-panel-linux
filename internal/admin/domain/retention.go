package domain

import "time"

// RetentionStat describes one retention-tracked table: how many rows it holds
// right now and when the oldest row was written. Used by the admin settings
// panel so admins can see the impact of a retention change before saving.
type RetentionStat struct {
	// SettingKey is the settings key governing this table (e.g.
	// "retention_node_stats_days"). Lets the frontend co-locate a stat
	// with its corresponding input field.
	SettingKey string `json:"setting_key"`
	// Table is the SQL table name, shown to admins for transparency.
	Table string `json:"table"`
	// Rows is the current row count.
	Rows int64 `json:"rows"`
	// OldestAt is the timestamp of the oldest row (by the table's age
	// field). Nil when the table is empty.
	OldestAt *time.Time `json:"oldest_at,omitempty"`
}
