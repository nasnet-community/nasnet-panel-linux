package domain

import "time"

// SubscriptionIP tracks IP addresses seen for a subscription per node.
// The (subscription_id, ip, node_id) unique index preserves per-node
// visibility: the same IP appearing on nodes A and B yields two rows,
// so audit reports can show "this IP used both A and B".
type SubscriptionIP struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	SubscriptionID uint      `gorm:"uniqueIndex:idx_sub_ip_node,priority:1;not null" json:"subscription_id"`
	IP             string    `gorm:"size:45;uniqueIndex:idx_sub_ip_node,priority:2;not null" json:"ip"`
	NodeID         uint      `gorm:"uniqueIndex:idx_sub_ip_node,priority:3;index" json:"node_id"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (SubscriptionIP) TableName() string {
	return "subscription_ips"
}
