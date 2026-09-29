package domain

import (
	"time"
)

// NodeStat represents a snapshot of node statistics
type NodeStat struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	NodeID      uint      `gorm:"index;not null" json:"node_id"`
	CPU         float64   `json:"cpu"`        // Percent
	Memory      float64   `json:"memory"`     // Percent
	Disk        float64   `json:"disk"`       // Percent
	UpSpeed     uint64    `json:"up_speed"`   // Bytes/sec
	DownSpeed   uint64    `json:"down_speed"` // Bytes/sec
	TcpCount    uint64    `json:"tcp_count"`
	UdpCount    uint64    `json:"udp_count"`
	FdCount     uint64    `json:"fd_count"`
	LoadAvg1    float64   `json:"load_avg_1"`
	OnlineUsers int       `gorm:"default:0" json:"online_users"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type NodeDailyTraffic struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	NodeID    uint      `gorm:"uniqueIndex:idx_node_daily_traffic_date;not null" json:"node_id"`
	Date      time.Time `gorm:"uniqueIndex:idx_node_daily_traffic_date;not null;type:date" json:"date"`
	Uplink    int64     `gorm:"default:0" json:"uplink"`
	Downlink  int64     `gorm:"default:0" json:"downlink"`
	CreatedAt time.Time `json:"created_at"`
}

type NodeUptimeEvent struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	NodeID uint   `gorm:"index;not null" json:"node_id"`
	Status string `gorm:"size:10;not null" json:"status"` // "online" or "offline"
	// Reason is why the transition happened (see the NodeOfflineReason* and
	// NodeOnlineReason* constants). Empty on rows written before this column.
	Reason string `gorm:"size:32;default:''" json:"reason,omitempty"`
	// Detail carries the underlying error or actor, e.g. the health-check
	// message. Truncated by the writer; never parsed, only displayed.
	Detail string `gorm:"size:255;default:''" json:"detail,omitempty"`
	// Timestamp is when the outage actually started, not when the debounce
	// window confirmed it — durations computed from these rows are real.
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
}

// HubLifecycleEvent brackets one run of the hub process. While the hub is
// down nothing observes the nodes, so those windows are rendered as
// "unknown" instead of stretching the last known state across them — and
// they are excluded from observed time in the daily rollup.
// StoppedAt is nil for the current run, or for a run that was killed.
type HubLifecycleEvent struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	StartedAt time.Time  `gorm:"index;not null" json:"started_at"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
	Version   string     `gorm:"size:64;default:''" json:"version,omitempty"`
}

func (HubLifecycleEvent) TableName() string { return "hub_lifecycle_events" }

// NodeDailyUptime is the per-day rollup of a node's connectivity. It exists
// because node_uptime_events is pruned on a retention window — this survives
// so long-range reliability views keep working.
//
// ObservedSec is the part of the day the hub was actually watching (the day
// minus hub-down gaps); DowntimeSec counts only unplanned outages, so
// administrative bands (disable, maintenance) inflate neither figure.
type NodeDailyUptime struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	NodeID      uint      `gorm:"uniqueIndex:idx_node_daily_uptime_date;not null" json:"node_id"`
	Date        time.Time `gorm:"uniqueIndex:idx_node_daily_uptime_date;not null;type:date" json:"date"`
	ObservedSec int       `gorm:"default:0" json:"observed_sec"`
	DowntimeSec int       `gorm:"default:0" json:"downtime_sec"`
	PlannedSec  int       `gorm:"default:0" json:"planned_sec"`
	OutageCount int       `gorm:"default:0" json:"outage_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (NodeDailyUptime) TableName() string { return "node_daily_uptimes" }

// Node connectivity transition reasons. Closed set — the writer validates,
// so call sites can't invent values that break the UI's reason filters.
const (
	NodeStatusOnline  = "online"
	NodeStatusOffline = "offline"

	// Unplanned: these count as downtime.
	NodeOfflineReasonHeartbeatTimeout = "heartbeat_timeout"
	NodeOfflineReasonAgentUnreachable = "agent_unreachable"
	NodeOfflineReasonHealthCheck      = "health_check_failed"
	NodeOfflineReasonHubShutdown      = "hub_shutdown"
	// NodeOfflineReasonPanelDown is never written as an event: it labels a
	// hub-down window on the built-in node, whose xray stops with the panel.
	NodeOfflineReasonPanelDown = "panel_down"

	// Planned: recorded, but excluded from downtime and uptime %.
	NodeOfflineReasonAdminDisabled = "admin_disabled"
	NodeOfflineReasonMaintenance   = "maintenance"

	NodeOnlineReasonHeartbeat      = "heartbeat_ok"
	NodeOnlineReasonAgentReachable = "agent_reachable"
	NodeOnlineReasonAdminEnabled   = "admin_enabled"
	NodeOnlineReasonHubStart       = "hub_start"
)

// TimeWindow is a half-open [From, To) interval, used for hub-down gaps and
// for the outage stretches paired out of the event stream.
type TimeWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// IsPlannedOfflineReason reports whether an offline stretch was an operator
// action rather than a fault. Planned stretches are shown in the timeline but
// kept out of downtime totals — mixing them makes an uptime figure useless.
func IsPlannedOfflineReason(reason string) bool {
	switch reason {
	case NodeOfflineReasonAdminDisabled, NodeOfflineReasonMaintenance:
		return true
	}
	return false
}

// StarlinkStat represents a snapshot of Starlink dish metrics for historical tracking.
type StarlinkStat struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	NodeID                uint      `gorm:"index;not null" json:"node_id"`
	DownlinkThroughputBps float64   `json:"downlink_throughput_bps"`
	UplinkThroughputBps   float64   `json:"uplink_throughput_bps"`
	PopPingLatencyMs      float64   `json:"pop_ping_latency_ms"`
	PopPingDropRate       float64   `json:"pop_ping_drop_rate"`
	ObstructionFraction   float64   `json:"obstruction_fraction"`
	CurrentlyObstructed   bool      `json:"currently_obstructed"`
	TiltAngleDeg          float64   `json:"tilt_angle_deg"`
	BoresightAzimuthDeg   float64   `json:"boresight_azimuth_deg"`
	BoresightElevationDeg float64   `json:"boresight_elevation_deg"`
	GpsValid              bool      `json:"gps_valid"`
	AlertFlags            uint32    `gorm:"default:0" json:"alert_flags"`
	CreatedAt             time.Time `gorm:"index" json:"created_at"`
}

// AlertFlagsFromBooleans packs 11 alert booleans into a uint32 bitmask.
func AlertFlagsFromBooleans(
	thermalShutdown, thermalThrottle, motorsStuck, noEthernetLink,
	isHeating, slowEthernet, powerSaveIdle, mastNotNearVertical,
	roaming, unexpectedLocation, installPending bool,
) uint32 {
	var flags uint32
	bools := []bool{
		thermalShutdown, thermalThrottle, motorsStuck, noEthernetLink,
		isHeating, slowEthernet, powerSaveIdle, mastNotNearVertical,
		roaming, unexpectedLocation, installPending,
	}
	for i, b := range bools {
		if b {
			flags |= 1 << uint(i)
		}
	}
	return flags
}
