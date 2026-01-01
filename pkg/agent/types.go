package agent

// SystemStats holds system resource statistics from the agent.
type SystemStats struct {
	CPUUsagePercent     float64
	MemoryTotalBytes    uint64
	MemoryUsedBytes     uint64
	MemoryUsagePercent  float64
	DiskTotalBytes      uint64
	DiskUsedBytes       uint64
	DiskUsagePercent    float64
	NetworkRecvRate     uint64
	NetworkSentRate     uint64
	LoadAvg1            float64
	LoadAvg5            float64
	LoadAvg15           float64
	SystemUptimeSeconds int64
	TcpCount            uint64
	UdpCount            uint64
	FdCount             uint64
}

// XrayStats holds traffic statistics from the agent.
type XrayStats struct {
	UserUplink       map[string]int64
	UserDownlink     map[string]int64
	InboundUplink    map[string]int64
	InboundDownlink  map[string]int64
	OutboundUplink   map[string]int64
	OutboundDownlink map[string]int64
	TotalUplink      int64
	TotalDownlink    int64
}

// HostInfo holds static system information.
type HostInfo struct {
	Hostname             string
	OS                   string
	Platform             string
	PlatformFamily       string
	PlatformVersion      string
	KernelVersion        string
	Arch                 string
	VirtualizationSystem string
	VirtualizationRole   string
	CPUModelName         string
	CPUCores             int32
	TotalMemory          uint64
	TotalSwap            uint64
	BootTime             uint64
}

// HealthStatus represents the health status of an agent.
type HealthStatus int

const (
	HealthUnknown HealthStatus = iota
	HealthHealthy
	HealthDegraded
	HealthUnhealthy
)

// HealthResult holds the result of a health check.
type HealthResult struct {
	Status     HealthStatus
	Message    string
	Components map[string]ComponentHealth
}

// ComponentHealth holds health info for a single component.
type ComponentHealth struct {
	Status  HealthStatus
	Message string
}

// VersionInfo holds version information from the agent.
type VersionInfo struct {
	AgentVersion   string
	AgentCommit    string
	AgentBuildTime string
	XrayVersion    string
	GoVersion      string
	OS             string
	Arch           string
}

// UpdateResult holds the result of an agent update.
type UpdateResult struct {
	Success    bool
	Message    string
	OldVersion string
	NewVersion string
}

// ChecksumResult holds the checksum of the running binary.
type ChecksumResult struct {
	Checksum string
	Path     string
}

// OutboundTestResult holds the result of an outbound connectivity test.
type OutboundTestResult struct {
	Success    bool   `json:"success"`
	LatencyMs  int64  `json:"latency_ms"`
	StatusCode int32  `json:"status_code"`
	IP         string `json:"ip,omitempty"`
	Country    string `json:"country,omitempty"`
	Error      string `json:"error,omitempty"`
	Message    string `json:"message,omitempty"`
}

// TrafficRecord holds traffic data for a single time bucket.
type TrafficRecord struct {
	Timestamp        int64
	UserUplink       map[string]int64
	UserDownlink     map[string]int64
	OutboundUplink   map[string]int64
	OutboundDownlink map[string]int64
	InboundUplink    map[string]int64
	InboundDownlink  map[string]int64
	TotalUplink      int64
	TotalDownlink    int64
}

// BufferedTrafficStats holds time-bucketed traffic records from the agent.
type BufferedTrafficStats struct {
	Records         []*TrafficRecord
	BufferStartTime int64
	BufferEndTime   int64
}

// VLESSKeyPair holds a generated VLESS key pair.
type VLESSKeyPair struct {
	Label      string `json:"label"`
	Decryption string `json:"decryption"`
	Encryption string `json:"encryption"`
}

// StarlinkStatus holds real-time Starlink dish metrics from the agent.
type StarlinkStatus struct {
	Available                        bool    `json:"available"`
	DownlinkThroughputBps            float64 `json:"downlink_throughput_bps"`
	UplinkThroughputBps              float64 `json:"uplink_throughput_bps"`
	PopPingLatencyMs                 float64 `json:"pop_ping_latency_ms"`
	PopPingDropRate                  float64 `json:"pop_ping_drop_rate"`
	EthSpeedMbps                     int32   `json:"eth_speed_mbps"`
	ObstructionFraction              float64 `json:"obstruction_fraction"`
	CurrentlyObstructed              bool    `json:"currently_obstructed"`
	AvgProlongedObstructionDurationS float64 `json:"avg_prolonged_obstruction_duration_s"`
	AvgProlongedObstructionIntervalS float64 `json:"avg_prolonged_obstruction_interval_s"`
	AlertThermalThrottle             bool    `json:"alert_thermal_throttle"`
	AlertThermalShutdown             bool    `json:"alert_thermal_shutdown"`
	AlertIsHeating                   bool    `json:"alert_is_heating"`
	AlertSlowEthernet                bool    `json:"alert_slow_ethernet"`
	AlertPowerSaveIdle               bool    `json:"alert_power_save_idle"`
	AlertMotorsStuck                 bool    `json:"alert_motors_stuck"`
	AlertNoEthernetLink              bool    `json:"alert_no_ethernet_link"`
	AlertUnexpectedLocation          bool    `json:"alert_unexpected_location"`
	AlertRoaming                     bool    `json:"alert_roaming"`
	AlertMastNotNearVert             bool    `json:"alert_mast_not_near_vertical"`
	AlertInstallPending              bool    `json:"alert_install_pending"`
	HardwareVersion                  string  `json:"hardware_version"`
	SoftwareVersion                  string  `json:"software_version"`
	CountryCode                      string  `json:"country_code"`
	UptimeS                          uint64  `json:"uptime_s"`
	BootCount                        int32   `json:"boot_count"`
	GpsValid                         bool    `json:"gps_valid"`
	GpsSats                          uint32  `json:"gps_sats"`
	TiltAngleDeg                     float64 `json:"tilt_angle_deg"`
	BoresightAzimuthDeg              float64 `json:"boresight_azimuth_deg"`
	BoresightElevationDeg            float64 `json:"boresight_elevation_deg"`
	SoftwareUpdateState              string  `json:"software_update_state"`
	SoftwareUpdateProgress           float64 `json:"software_update_progress"`
	OutageCause                      string  `json:"outage_cause"`
	OutageDurationNs                 uint64  `json:"outage_duration_ns"`
	DisablementCode                  string  `json:"disablement_code"`
	MobilityClass                    string  `json:"mobility_class"`
	ClassOfService                   string  `json:"class_of_service"`
	CellID                           uint32  `json:"cell_id"`
	SatelliteID                      uint32  `json:"satellite_id"`
	GatewayID                        uint32  `json:"gateway_id"`
	OnBackupBeam                     bool    `json:"on_backup_beam"`
	Latitude                         float64 `json:"latitude"`
	Longitude                        float64 `json:"longitude"`
	Altitude                         float64 `json:"altitude"`
	IsSnrAboveNoiseFloor             bool    `json:"is_snr_above_noise_floor"`
	IsSnrPersistentlyLow             bool    `json:"is_snr_persistently_low"`
}

// StarlinkObstructionMap holds the dish obstruction map data.
type StarlinkObstructionMap struct {
	NumRows        uint32    `json:"num_rows"`
	NumCols        uint32    `json:"num_cols"`
	SNR            []float32 `json:"snr"`
	MaxThetaDeg    float32   `json:"max_theta_deg"`
	ReferenceFrame string    `json:"reference_frame"`
}
