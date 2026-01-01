package domain

import "time"

// AccessLogSummary stores hourly aggregated access log statistics per node per user.
type AccessLogSummary struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	NodeID          uint      `gorm:"uniqueIndex:idx_als_uniq,priority:1;index:idx_als_node_time" json:"node_id"`
	Email           string    `gorm:"uniqueIndex:idx_als_uniq,priority:2;size:255;index:idx_als_email_time" json:"email"`
	HourTime        time.Time `gorm:"uniqueIndex:idx_als_uniq,priority:3;index:idx_als_node_time;index:idx_als_email_time;index:idx_als_sub_time,priority:2" json:"hour_time"`
	SubscriptionID  uint      `gorm:"index:idx_als_sub_time,priority:1" json:"subscription_id"`
	AcceptedCount   int64     `json:"accepted_count"`
	RejectedCount   int64     `json:"rejected_count"`
	TcpCount        int64     `json:"tcp_count"`
	UdpCount        int64     `json:"udp_count"`
	TopDomains      string    `gorm:"type:text" json:"top_domains"`      // JSON: {"google.com":145}
	RejectedDomains string    `gorm:"type:text" json:"rejected_domains"` // JSON: {"blocked.com":45}
	SourceIPs       string    `gorm:"type:text" json:"source_ips"`       // JSON: {"1.2.3.4":50}
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
