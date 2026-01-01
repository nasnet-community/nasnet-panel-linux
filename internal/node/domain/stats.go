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
	ID        uint      `gorm:"primaryKey" json:"id"`
	NodeID    uint      `gorm:"index;not null" json:"node_id"`
	Status    string    `gorm:"size:10;not null" json:"status"` // "online" or "offline"
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
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
