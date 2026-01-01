package domain

import (
	"encoding/json"
	"net"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Node represents a physical server (VPS/Dedicated) running Xray-core
type Node struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	UUID        string `gorm:"type:varchar(36);not null;default:''" json:"uuid"`
	Name        string `gorm:"size:100;not null" json:"name"`          // e.g. "Frankfurt 1"
	Datacenter  string `gorm:"size:100" json:"datacenter"`             // e.g. "Hetzner", "DigitalOcean"
	IP          string `gorm:"size:50;not null" json:"ip"`             // Public IP or Domain
	APIPort     int    `gorm:"not null;default:10085" json:"api_port"` // Xray gRPC API Port
	CountryCode string `gorm:"size:5" json:"country_code"`             // e.g. "DE", "US"
	IsActive    bool   `gorm:"default:true" json:"is_active"`

	// Agent Configuration
	AgentPort           int    `gorm:"default:9090" json:"agent_port"`               // Node Agent gRPC port
	ConnectMode         string `gorm:"size:20;default:'direct'" json:"connect_mode"` // "direct" or "reverse"
	IsStealth           bool   `gorm:"default:false" json:"is_stealth"`              // Whether to run agent in Stealth Mode
	IsPersistentStealth bool   `gorm:"default:false" json:"is_persistent_stealth"`   // Whether stealth agent should be persistent

	// Monitoring Status (transient or persistent)
	IsOnline    bool      `gorm:"default:false" json:"is_online"`
	XrayStopped bool      `gorm:"default:false" json:"xray_stopped"` // User explicitly stopped Xray; suppresses auto-restart
	LastCheck   time.Time `json:"last_check"`

	// LastAccessLogSyncedAt is stamped after each successful hourly
	// summary Ack from this node. Nil = never synced. Surfaced as a
	// freshness pill in the access-history UI so operators can spot
	// stale data immediately.
	LastAccessLogSyncedAt *time.Time `gorm:"column:last_access_log_synced_at" json:"last_access_log_synced_at,omitempty"`

	// Maintenance mode fields (per-node layer of maintenance feature)
	MaintenanceMode    bool       `gorm:"default:false" json:"maintenance_mode"`
	MaintenanceMessage string     `gorm:"type:text;default:''" json:"maintenance_message"`
	MaintenanceSince   *time.Time `gorm:"default:null" json:"maintenance_since,omitempty"`

	// Xray Log Configuration
	LogLevel        string `gorm:"size:20;default:'warning'" json:"log_level"` // debug, info, warning, error, none
	LogAccess       string `gorm:"size:255" json:"log_access"`                 // Path to access log
	LogError        string `gorm:"size:255" json:"log_error"`                  // Path to error log
	LogDNS          bool   `gorm:"default:false" json:"log_dns"`               // Whether to enable DNS logging
	EnableAccessLog bool   `gorm:"default:false" json:"enable_access_log"`     // Enable access log capture for per-subscription DNS logs

	// System Stats (transient, populated from agent - not stored in DB)
	CPUUsage      float64 `gorm:"-" json:"cpu_usage,omitempty"`
	MemoryUsed    uint64  `gorm:"-" json:"memory_used,omitempty"`
	MemoryTotal   uint64  `gorm:"-" json:"memory_total,omitempty"`
	MemoryPercent float64 `gorm:"-" json:"memory_percent,omitempty"`
	DiskUsed      uint64  `gorm:"-" json:"disk_used,omitempty"`
	DiskTotal     uint64  `gorm:"-" json:"disk_total,omitempty"`
	DiskPercent   float64 `gorm:"-" json:"disk_percent,omitempty"`
	XrayUptime    int64   `gorm:"-" json:"xray_uptime,omitempty"`
	XrayVersion   string  `gorm:"-" json:"xray_version,omitempty"`
	AgentVersion  string  `gorm:"-" json:"agent_version,omitempty"`
	TcpCount      uint64  `gorm:"-" json:"tcp_count,omitempty"`
	UdpCount      uint64  `gorm:"-" json:"udp_count,omitempty"`
	FdCount       uint64  `gorm:"-" json:"fd_count,omitempty"`

	// Cumulative traffic counters (persisted, incremented each sync cycle)
	TotalUplink   int64 `gorm:"default:0" json:"total_uplink"`
	TotalDownlink int64 `gorm:"default:0" json:"total_downlink"`

	// One Node has many Inbounds
	Inbounds []Inbound `gorm:"foreignKey:NodeID" json:"inbounds,omitempty"`

	// One Node has many Outbounds
	Outbounds []Outbound `gorm:"foreignKey:NodeID" json:"outbounds,omitempty"`

	// Per-node routing configuration presets (Basic Routing settings)
	RoutingSettings *RoutingSettings `gorm:"serializer:json;type:jsonb" json:"routing_settings"`

	// Per-node DNS configuration (omitted from xray config when nil)
	DNSSettings *DNSSettings `gorm:"serializer:json;type:jsonb" json:"dns_settings"`

	// FakeDNS pool configuration (stored as JSONB, omitted from xray config when nil)
	FakeDNSSettings []FakeDNSPool `gorm:"serializer:json;type:jsonb" json:"fake_dns_settings"`

	// Per-node bandwidth shaping (TC) configuration
	BandwidthSettings *BandwidthSettings `gorm:"serializer:json;type:jsonb" json:"bandwidth_settings"`

	// Per-node Starlink monitoring configuration
	StarlinkSettings *StarlinkSettings `gorm:"serializer:json;type:jsonb" json:"starlink_settings"`

	// Per-node crash recovery command configuration
	CrashRecoverySettings *CrashRecoverySettings `gorm:"serializer:json;type:jsonb" json:"crash_recovery_settings"`

	// Last crash recovery attempt result (for panel display)
	LastCrashRecovery *LastCrashRecovery `gorm:"serializer:json;type:jsonb" json:"last_crash_recovery"`

	// One Node has many RoutingRules
	RoutingRules   []RoutingRule   `gorm:"foreignKey:NodeID" json:"routing_rules,omitempty"`
	BalancingRules []BalancingRule `gorm:"foreignKey:NodeID" json:"balancing_rules,omitempty"`

	// One Node has many ReverseProxies
	ReverseProxies []ReverseProxy `gorm:"foreignKey:NodeID" json:"reverse_proxies,omitempty"`

	// Nuke state (populated when this node has been Wiped or Nuked)
	NukedAt  *time.Time `gorm:"column:nuked_at;index" json:"nuked_at,omitempty"`
	NukeMode string     `gorm:"column:nuke_mode;size:16" json:"nuke_mode,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (n *Node) BeforeCreate(tx *gorm.DB) error {
	if n.UUID == "" {
		n.UUID = uuid.New().String()
	}
	return nil
}

// BandwidthSettings holds per-node TC bandwidth shaping configuration.
// When Enabled, the hub calls SetupBandwidth on the agent after each config push.
type BandwidthSettings struct {
	Enabled   bool   `json:"enabled"`             // Enable TC bandwidth shaping on this node
	Interface string `json:"interface,omitempty"` // Egress network interface (e.g. "eth0")
	TotalBW   int    `json:"total_bw,omitempty"`  // Total link bandwidth in Mbps (default 1000)
}

// GetBandwidthSettingsOrDefault returns the node's bandwidth settings, or defaults when nil.
func (n *Node) GetBandwidthSettingsOrDefault() *BandwidthSettings {
	if n.BandwidthSettings == nil {
		return &BandwidthSettings{Enabled: false, Interface: "eth0", TotalBW: 1000}
	}
	return n.BandwidthSettings
}

// StarlinkSettings holds per-node Starlink monitoring configuration.
type StarlinkSettings struct {
	Enabled     bool   `json:"enabled"`                // Enable Starlink monitoring on this node
	DishAddress string `json:"dish_address,omitempty"` // Starlink dish gRPC address (default: "192.168.100.1:9200")
}

// GetStarlinkSettingsOrDefault returns the node's Starlink settings, or defaults when nil.
func (n *Node) GetStarlinkSettingsOrDefault() *StarlinkSettings {
	if n.StarlinkSettings == nil {
		return &StarlinkSettings{Enabled: false, DishAddress: "192.168.100.1:9200"}
	}
	s := *n.StarlinkSettings
	if s.DishAddress == "" {
		s.DishAddress = "192.168.100.1:9200"
	}
	return &s
}

// LastCrashRecovery stores the result of the most recent crash recovery command execution.
// Persisted as JSONB on the Node so the panel can display recovery status after the fact.
type LastCrashRecovery struct {
	Timestamp   time.Time `json:"timestamp"`
	ExitCode    int       `json:"exit_code"`
	Stdout      string    `json:"stdout,omitempty"`
	Stderr      string    `json:"stderr,omitempty"`
	Success     bool      `json:"success"`
	AttemptNum  int       `json:"attempt_num"`
	MaxAttempts int       `json:"max_attempts"`
	Exhausted   bool      `json:"exhausted"`
	Error       string    `json:"error,omitempty"` // RPC error message if command failed to execute
}

// CrashRecoverySettings holds per-node crash recovery command configuration.
// When Enabled and auto-disable fires, the hub executes Command on the node
// via the agent, then starts xray one more time.
type CrashRecoverySettings struct {
	Enabled        bool   `json:"enabled"`                   // Master toggle
	Command        string `json:"command,omitempty"`         // Shell command to execute on the node
	CommandTimeout int    `json:"command_timeout,omitempty"` // Max seconds for command execution (default: 60)
	Cooldown       int    `json:"cooldown,omitempty"`        // Min minutes between command executions (default: 30)
	MaxAttempts    int    `json:"max_attempts,omitempty"`    // Max times command can run, 0=unlimited (default: 3)
}

// GetCrashRecoverySettingsOrDefault returns the node's crash recovery settings, or defaults when nil.
func (n *Node) GetCrashRecoverySettingsOrDefault() *CrashRecoverySettings {
	if n.CrashRecoverySettings == nil {
		return &CrashRecoverySettings{Enabled: false, CommandTimeout: 60, Cooldown: 30, MaxAttempts: 3}
	}
	s := *n.CrashRecoverySettings
	if s.CommandTimeout <= 0 {
		s.CommandTimeout = 60
	}
	if s.Cooldown <= 0 {
		s.Cooldown = 30
	}
	return &s
}

// XrayUser represents a live user on the Xray node
type XrayUser struct {
	Email    string `json:"email"`
	UUID     string `json:"uuid"`
	Level    uint32 `json:"level"`
	AlterId  uint32 `json:"alter_id"`
	Traffic  int64  `json:"traffic"`  // Aggregated Traffic (Up+Down) - Optional
	Uplink   int64  `json:"uplink"`   // Live Uplink
	Downlink int64  `json:"downlink"` // Live Downlink
}

// InboundUsers represents users grouped by inbound
type InboundUsers struct {
	InboundTag string      `json:"inbound_tag"`
	Protocol   string      `json:"protocol"`
	Port       int         `json:"port"`
	Users      []*XrayUser `json:"users"`
}

// SSHStatus represents the status of the SSH service on a node
type SSHStatus struct {
	Enabled  bool `json:"enabled"`
	Port     int  `json:"port"`
	IsActive bool `json:"is_active"`
}

// NodeHealth holds health check result
type NodeHealth struct {
	Healthy bool   `json:"healthy"`
	Message string `json:"message"`
	Latency int64  `json:"latency"`
}

// HostInfo holds static system information
type HostInfo struct {
	Hostname             string `json:"hostname"`
	OS                   string `json:"os"`
	Platform             string `json:"platform"`
	PlatformFamily       string `json:"platform_family"`
	PlatformVersion      string `json:"platform_version"`
	KernelVersion        string `json:"kernel_version"`
	Arch                 string `json:"arch"`
	VirtualizationSystem string `json:"virtualization_system"`
	VirtualizationRole   string `json:"virtualization_role"`
	CPUModelName         string `json:"cpu_model_name"`
	CPUCores             int32  `json:"cpu_cores"`
	TotalMemory          uint64 `json:"total_memory"` // bytes
	TotalSwap            uint64 `json:"total_swap"`   // bytes
	BootTime             uint64 `json:"boot_time"`
}

// Inbound represents a specific protocol/port entry on a Node
type Inbound struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	NodeID uint `gorm:"not null;index" json:"node_id"`

	// Belongs To Relation (Critical for accessing Node IP from Inbound)
	Node *Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`

	// Tag must match the "tag" in Xray config.json 'inbounds'
	Tag string `gorm:"size:100;not null" json:"tag"`

	Protocol string `gorm:"size:20;not null" json:"protocol"` // "vmess", "vless", "trojan"
	Port     int    `gorm:"not null" json:"port"`             // Public connecting port (e.g. 443); also the port advertised in client links/stats
	// Optional PortList ("1000-2000" or "80,443,8080") to bind a range/list
	// instead of Port. Port is still used for links and stats.
	PortRange string `gorm:"size:100;default:''" json:"port_range"`
	Listen    string `gorm:"size:50;default:''" json:"listen"` // Listen address, e.g., "0.0.0.0"

	// Custom address override (if different from Node.IP)
	Address string `gorm:"size:100" json:"address"` // Custom IP/Domain override

	// Remark is the user-visible label for this specific connection
	Remark string `gorm:"size:100" json:"remark"` // e.g. "🇩🇪 High Speed"

	// --- Stream Settings ---
	Network  string `gorm:"size:20" json:"network"`  // tcp, ws, grpc, xhttp, httpupgrade
	Security string `gorm:"size:20" json:"security"` // tls, reality, none

	// Detailed Settings (Stored as JSONB for type safety and query capabilities)
	TLSSettings       *TLSSettings       `gorm:"serializer:json;type:jsonb" json:"tls_settings"`
	RealitySettings   *RealitySettings   `gorm:"serializer:json;type:jsonb" json:"reality_settings"`
	TransportSettings *TransportSettings `gorm:"serializer:json;type:jsonb" json:"transport_settings"`
	SniffingSettings  *SniffingSettings  `gorm:"serializer:json;type:jsonb" json:"sniffing_settings"`

	// Protocol-specific settings
	ShadowsocksSettings *ShadowsocksSettings `gorm:"serializer:json;type:jsonb" json:"shadowsocks_settings"`
	WireGuardSettings   *WireGuardSettings   `gorm:"serializer:json;type:jsonb" json:"wireguard_settings"`
	HTTPSettings        *HTTPSettings        `gorm:"serializer:json;type:jsonb" json:"http_settings"`
	SOCKSSettings       *SOCKSSettings       `gorm:"serializer:json;type:jsonb" json:"socks_settings"`
	SockoptSettings     *SockoptSettings     `gorm:"serializer:json;type:jsonb" json:"sockopt_settings"`
	VLESSSettings       *VLESSSettings       `gorm:"serializer:json;type:jsonb" json:"vless_settings"`
	VMessSettings       *VMessSettings       `gorm:"serializer:json;type:jsonb" json:"vmess_settings"`
	TrojanSettings      *TrojanSettings      `gorm:"serializer:json;type:jsonb" json:"trojan_settings"`
	DokodemoSettings    *DokodemoSettings    `gorm:"serializer:json;type:jsonb" json:"dokodemo_settings"`
	HysteriaSettings    *HysteriaSettings    `gorm:"serializer:json;type:jsonb" json:"hysteria_settings"`
	FinalMask           *FinalMask           `gorm:"serializer:json;type:jsonb" json:"finalmask"`

	// LinkFormat is the template for generating the client link.
	// Placeholders: {uuid}, {email}, {host}, {port}, {name}
	// Example: vless://{uuid}@{host}:{port}?security=reality&sni=google.com&fp=chrome&pbk=...&type=grpc&serviceName=grpc# {name}
	LinkFormat string `gorm:"type:text" json:"link_format"`

	// IsDisabled allows temporarily disabling an inbound without deleting it.
	// When disabled: excluded from Xray config push and subscription links.
	IsDisabled bool `gorm:"default:false" json:"is_disabled"`

	// One Inbound has many Hosts (presentation-layer templates)
	Hosts []Host `gorm:"foreignKey:InboundID" json:"hosts,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// --- JSON Structs for Detailed Settings ---

type TLSSettings struct {
	ServerName              string          `json:"serverName,omitempty"`
	ALPN                    []string        `json:"alpn,omitempty"`
	Fingerprint             string          `json:"fingerprint,omitempty"`
	Certificates            []Certificate   `json:"certificates,omitempty"`
	CipherSuites            string          `json:"cipherSuites,omitempty"`
	MinVersion              string          `json:"minVersion,omitempty"`
	MaxVersion              string          `json:"maxVersion,omitempty"`
	AllowInsecure           bool            `json:"allowInsecure,omitempty"`
	DisableSystemRoot       bool            `json:"disableSystemRoot,omitempty"`
	RejectUnknownSNI        bool            `json:"rejectUnknownSni,omitempty"`
	EnableSessionResumption bool            `json:"enableSessionResumption,omitempty"`
	PinnedPeerCertSha256    string          `json:"pinnedPeerCertSha256,omitempty"`
	MasterKeyLog            string          `json:"masterKeyLog,omitempty"`
	ECH                     json.RawMessage `json:"ech,omitempty"`
	// CurvePreferences limits the TLS key-exchange curves. VerifyPeerCertByName
	// replaces allowInsecure (verify against a name, not the SNI).
	CurvePreferences     []string `json:"curvePreferences,omitempty"`
	VerifyPeerCertByName string   `json:"verifyPeerCertByName,omitempty"`
}

type Certificate struct {
	CertificateFile string `json:"certificateFile,omitempty"`
	KeyFile         string `json:"keyFile,omitempty"`
	// ID references an AgentCertificate (for managed certs)
	ID             uint   `json:"id,omitempty"`
	SNIId          uint   `json:"sniId,omitempty"` // References an SNI domain certificate
	Usage          string `json:"usage,omitempty"`
	BuildChain     bool   `json:"buildChain,omitempty"`
	OneTimeLoading bool   `json:"oneTimeLoading,omitempty"`
	OCSPStapling   uint64 `json:"ocspStapling,omitempty"` // seconds between OCSP refreshes
}

type RealitySettings struct {
	Show          bool     `json:"show,omitempty"`
	Dest          string   `json:"dest,omitempty"` // e.g., "www.google.com:443"
	Xver          uint64   `json:"xver,omitempty"`
	ServerNames   []string `json:"serverNames,omitempty"`
	ServerName    string   `json:"serverName,omitempty"` // For Outbound compatibility
	PrivateKey    string   `json:"privateKey,omitempty"` // Stored here, not sent to client
	PublicKey     string   `json:"publicKey,omitempty"`
	ShortID       string   `json:"shortId,omitempty"`
	SpiderX       string   `json:"spiderX,omitempty"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	ALPN          []string `json:"alpn,omitempty"`
	MinClientVer  string   `json:"minClientVer,omitempty"`
	MaxClientVer  string   `json:"maxClientVer,omitempty"`
	MaxTimeDiff   uint64   `json:"maxTimeDiff,omitempty"`
	Mldsa65Verify string   `json:"mldsa65Verify,omitempty"`
	// Server-side extras. ShortIDs holds multiple shortIds (ShortID kept for
	// compat, merged in). Mldsa65Seed is the server PQ seed (Mldsa65Verify the
	// client half). LimitFallback* pass through to xray verbatim.
	ShortIDs              []string        `json:"shortIds,omitempty"`
	Mldsa65Seed           string          `json:"mldsa65Seed,omitempty"`
	MasterKeyLog          string          `json:"masterKeyLog,omitempty"`
	LimitFallbackUpload   json.RawMessage `json:"limitFallbackUpload,omitempty"`
	LimitFallbackDownload json.RawMessage `json:"limitFallbackDownload,omitempty"`
}

// RangeConfig represents a {from, to} range used by XHTTP/SplitHTTP settings
type RangeConfig struct {
	From int32 `json:"from"`
	To   int32 `json:"to"`
}

// XmuxConfig represents XHTTP multiplexing configuration
type XmuxConfig struct {
	MaxConcurrency   *RangeConfig `json:"maxConcurrency,omitempty"`
	MaxConnections   *RangeConfig `json:"maxConnections,omitempty"`
	CMaxReuseTimes   *RangeConfig `json:"cMaxReuseTimes,omitempty"`
	HMaxRequestTimes *RangeConfig `json:"hMaxRequestTimes,omitempty"`
	HMaxReusableSecs *RangeConfig `json:"hMaxReusableSecs,omitempty"`
	HKeepAlivePeriod int64        `json:"hKeepAlivePeriod,omitempty"`
}

type TransportSettings struct {
	// Common
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`

	// gRPC
	ServiceName             string `json:"serviceName,omitempty"`
	GRPCAuthority           string `json:"authority,omitempty"`
	GRPCMultiMode           bool   `json:"multiMode,omitempty"`
	GRPCIdleTimeout         int    `json:"idle_timeout,omitempty"`
	GRPCHealthCheckTimeout  int    `json:"health_check_timeout,omitempty"`
	GRPCPermitWithoutStream bool   `json:"permit_without_stream,omitempty"`
	GRPCInitialWindowsSize  int    `json:"initial_windows_size,omitempty"`

	// TCP/HTTP Header
	HeaderType string `json:"headerType,omitempty"`

	// WebSocket
	MaxEarlyData        int    `json:"maxEarlyData,omitempty"`
	EarlyDataHeaderName string `json:"earlyDataHeaderName,omitempty"`
	HeartbeatPeriod     int    `json:"heartbeatPeriod,omitempty"`

	// XHTTP (SplitHTTP)
	Mode                 string            `json:"mode,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	NoSSEHeader          bool              `json:"noSSEHeader,omitempty"`
	ScMaxBufferedPosts   int               `json:"scMaxBufferedPosts,omitempty"`
	ScMaxEachPostBytes   *RangeConfig      `json:"scMaxEachPostBytes,omitempty"`
	ScMinPostsIntervalMs *RangeConfig      `json:"scMinPostsIntervalMs,omitempty"`
	ScStreamUpServerSecs *RangeConfig      `json:"scStreamUpServerSecs,omitempty"`
	XPaddingBytes        *RangeConfig      `json:"xPaddingBytes,omitempty"`
	Xmux                 *XmuxConfig       `json:"xmux,omitempty"`
	NoGRPCHeader         bool              `json:"noGRPCHeader,omitempty"`
	Extra                string            `json:"extra,omitempty"`
	// Enable PROXY protocol on the tcp/ws/httpupgrade listener (per-transport,
	// not just via sockopt).
	AcceptProxyProtocol bool `json:"acceptProxyProtocol,omitempty"`

	// gRPC
	UserAgent string `json:"userAgent,omitempty"`

	// XHTTP advanced placement/padding
	XPaddingObfsMode     bool         `json:"xPaddingObfsMode,omitempty"`
	XPaddingKey          string       `json:"xPaddingKey,omitempty"`
	XPaddingHeader       string       `json:"xPaddingHeader,omitempty"`
	XPaddingPlacement    string       `json:"xPaddingPlacement,omitempty"`
	XPaddingMethod       string       `json:"xPaddingMethod,omitempty"`
	UplinkHTTPMethod     string       `json:"uplinkHTTPMethod,omitempty"`
	SessionPlacement     string       `json:"sessionPlacement,omitempty"`
	SessionKey           string       `json:"sessionKey,omitempty"`
	SeqPlacement         string       `json:"seqPlacement,omitempty"`
	SeqKey               string       `json:"seqKey,omitempty"`
	UplinkDataPlacement  string       `json:"uplinkDataPlacement,omitempty"`
	UplinkDataKey        string       `json:"uplinkDataKey,omitempty"`
	UplinkChunkSize      *RangeConfig `json:"uplinkChunkSize,omitempty"`
	ServerMaxHeaderBytes int32        `json:"serverMaxHeaderBytes,omitempty"`

	// mKCP
	KCPMtu              uint32 `json:"mtu,omitempty"`
	KCPTti              uint32 `json:"tti,omitempty"`
	KCPUplinkCapacity   uint32 `json:"uplinkCapacity,omitempty"`
	KCPDownlinkCapacity uint32 `json:"downlinkCapacity,omitempty"`
	KCPCwndMultiplier   uint32 `json:"cwndMultiplier,omitempty"`
	KCPMaxSendingWindow uint32 `json:"maxSendingWindow,omitempty"`
}

type SniffingSettings struct {
	Enabled         bool     `json:"enabled"`
	DestOverride    []string `json:"destOverride,omitempty"`
	MetadataOnly    bool     `json:"metadataOnly,omitempty"`
	RouteOnly       bool     `json:"routeOnly,omitempty"`
	DomainsExcluded []string `json:"domainsExcluded,omitempty"`
	IPsExcluded     []string `json:"ipsExcluded,omitempty"`
}

// ShadowsocksSettings for Shadowsocks inbound
type ShadowsocksSettings struct {
	Method     string `json:"method,omitempty"`   // 2022-blake3-aes-128-gcm, chacha20-ietf-poly1305, etc.
	Password   string `json:"password,omitempty"` // Server password
	Network    string `json:"network,omitempty"`  // tcp, udp, or tcp,udp
	IVCheck    bool   `json:"ivCheck,omitempty"`
	Level      int    `json:"level,omitempty"`
	Email      string `json:"email,omitempty"`
	UoT        bool   `json:"uot,omitempty"`
	UoTVersion int    `json:"uotVersion,omitempty"`
}

// WireGuardSettings for WireGuard inbound/outbound
type WireGuardSettings struct {
	SecretKey      string          `json:"secretKey,omitempty"`      // Server private key
	MTU            int             `json:"mtu,omitempty"`            // Default 1420
	Endpoint       []string        `json:"endpoint,omitempty"`       // Local addresses (CIDR)
	NumWorkers     int             `json:"numWorkers,omitempty"`     // Worker threads
	Reserved       []int           `json:"reserved,omitempty"`       // Reserved bytes (3 bytes)
	DomainStrategy string          `json:"domainStrategy,omitempty"` // FORCE_IP, FORCE_IP4, etc.
	NoKernelTun    bool            `json:"noKernelTun,omitempty"`
	Peers          []WireGuardPeer `json:"peers,omitempty"`

	// Per-device selling (not emitted into xray config).
	PeerPoolCIDR string `json:"peerPoolCidr,omitempty"` // IPAM pool for device /32s, e.g. 10.8.0.0/16
	ClientDNS    string `json:"clientDns,omitempty"`    // DNS line for generated client .conf
}

// WireGuardPeer represents a WireGuard peer configuration
type WireGuardPeer struct {
	PublicKey    string   `json:"publicKey,omitempty"`
	PreSharedKey string   `json:"preSharedKey,omitempty"`
	Endpoint     string   `json:"endpoint,omitempty"`  // Remote endpoint (host:port)
	KeepAlive    int      `json:"keepAlive,omitempty"` // Seconds
	AllowedIPs   []string `json:"allowedIps,omitempty"`
}

// HTTPSettings for HTTP inbound proxy
type HTTPSettings struct {
	AllowTransparent bool              `json:"allowTransparent,omitempty"`
	Timeout          int               `json:"timeout,omitempty"` // Seconds
	UserLevel        int               `json:"userLevel,omitempty"`
	Accounts         []HTTPAccount     `json:"accounts,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"` // Custom headers for outbound
}

// HTTPAccount for HTTP proxy authentication
type HTTPAccount struct {
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
}

// SOCKSSettings for SOCKS inbound proxy
type SOCKSSettings struct {
	Auth      string         `json:"auth,omitempty"` // noauth, password
	UDP       bool           `json:"udp,omitempty"`
	IP        string         `json:"ip,omitempty"` // UDP relay IP
	UserLevel int            `json:"userLevel,omitempty"`
	Accounts  []SOCKSAccount `json:"accounts,omitempty"`
}

// SOCKSAccount for SOCKS proxy authentication
type SOCKSAccount struct {
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
}

// DokodemoSettings for Dokodemo-door transparent proxy inbound
type DokodemoSettings struct {
	Address        string                 `json:"address,omitempty"`  // Destination address
	Port           int                    `json:"port,omitempty"`     // Destination port
	Networks       string                 `json:"networks,omitempty"` // tcp, udp, or tcp,udp
	UserLevel      int                    `json:"userLevel,omitempty"`
	FollowRedirect bool                   `json:"followRedirect,omitempty"` // Follow system redirect (tproxy)
	PortMap        map[string]interface{} `json:"portMap,omitempty"`
}

// HysteriaSettings for Hysteria2 inbound/outbound
type HysteriaSettings struct {
	Auth           string `json:"auth,omitempty"`
	Congestion     string `json:"congestion,omitempty"`
	Up             string `json:"up,omitempty"`
	Down           string `json:"down,omitempty"`
	UdpIdleTimeout int    `json:"udpIdleTimeout,omitempty"`
}

// MuxSettings for outbound multiplexing
type MuxSettings struct {
	Enabled         bool   `json:"enabled"`
	Concurrency     int32  `json:"concurrency,omitempty"`
	XudpConcurrency int32  `json:"xudpConcurrency,omitempty"`
	XudpProxyUDP443 string `json:"xudpProxyUDP443,omitempty"`
}

// ProxySettings for outbound proxy chaining
type ProxySettings struct {
	Tag            string `json:"tag,omitempty"`
	TransportLayer bool   `json:"transportLayer,omitempty"`
}

// GetTLSSettingsOrDefault returns TLSSettings, creating empty struct if nil
func (i *Inbound) GetTLSSettingsOrDefault() *TLSSettings {
	if i.TLSSettings == nil {
		return &TLSSettings{}
	}
	return i.TLSSettings
}

// GetRealitySettingsOrDefault returns RealitySettings, creating empty struct if nil
func (i *Inbound) GetRealitySettingsOrDefault() *RealitySettings {
	if i.RealitySettings == nil {
		return &RealitySettings{}
	}
	return i.RealitySettings
}

// GetTransportSettingsOrDefault returns TransportSettings, creating empty struct if nil
func (i *Inbound) GetTransportSettingsOrDefault() *TransportSettings {
	if i.TransportSettings == nil {
		return &TransportSettings{}
	}
	return i.TransportSettings
}

// GetSniffingSettingsOrDefault returns SniffingSettings with sensible defaults if nil
func (i *Inbound) GetSniffingSettingsOrDefault() *SniffingSettings {
	if i.SniffingSettings == nil {
		return &SniffingSettings{Enabled: true, DestOverride: []string{"http", "tls"}}
	}
	return i.SniffingSettings
}

// GetShadowsocksSettingsOrDefault returns ShadowsocksSettings, creating empty struct if nil
func (i *Inbound) GetShadowsocksSettingsOrDefault() *ShadowsocksSettings {
	if i.ShadowsocksSettings == nil {
		return &ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm", Network: "tcp,udp"}
	}
	return i.ShadowsocksSettings
}

// GetWireGuardSettingsOrDefault returns WireGuardSettings, creating empty struct if nil
func (i *Inbound) GetWireGuardSettingsOrDefault() *WireGuardSettings {
	if i.WireGuardSettings == nil {
		return &WireGuardSettings{MTU: 1420}
	}
	return i.WireGuardSettings
}

// WGServerIP returns the bare server tunnel IP from Endpoint[0] (drops the /mask).
func (w *WireGuardSettings) WGServerIP() string {
	if len(w.Endpoint) == 0 {
		return ""
	}
	if host, _, err := net.ParseCIDR(w.Endpoint[0]); err == nil {
		return host.String()
	}
	return w.Endpoint[0]
}

// GetHTTPSettingsOrDefault returns HTTPSettings, creating empty struct if nil
func (i *Inbound) GetHTTPSettingsOrDefault() *HTTPSettings {
	if i.HTTPSettings == nil {
		return &HTTPSettings{}
	}
	return i.HTTPSettings
}

// GetSOCKSSettingsOrDefault returns SOCKSSettings, creating empty struct if nil
func (i *Inbound) GetSOCKSSettingsOrDefault() *SOCKSSettings {
	if i.SOCKSSettings == nil {
		return &SOCKSSettings{Auth: "noauth"}
	}
	return i.SOCKSSettings
}

// GetSockoptSettingsOrDefault returns SockoptSettings, creating empty struct if nil
func (i *Inbound) GetSockoptSettingsOrDefault() *SockoptSettings {
	if i.SockoptSettings == nil {
		return &SockoptSettings{}
	}
	return i.SockoptSettings
}

// GetVLESSSettingsOrDefault returns VLESSSettings, creating empty struct if nil
func (i *Inbound) GetVLESSSettingsOrDefault() *VLESSSettings {
	if i.VLESSSettings == nil {
		return &VLESSSettings{Encryption: "none"}
	}
	return i.VLESSSettings
}

// GetVMessSettingsOrDefault returns VMessSettings, creating empty struct if nil
func (i *Inbound) GetVMessSettingsOrDefault() *VMessSettings {
	if i.VMessSettings == nil {
		return &VMessSettings{Security: "auto"}
	}
	return i.VMessSettings
}

// GetTrojanSettingsOrDefault returns TrojanSettings, creating empty struct if nil (Inbound)
func (i *Inbound) GetTrojanSettingsOrDefault() *TrojanSettings {
	if i.TrojanSettings == nil {
		return &TrojanSettings{}
	}
	return i.TrojanSettings
}

// GetDokodemoSettingsOrDefault returns DokodemoSettings, creating empty struct if nil
func (i *Inbound) GetDokodemoSettingsOrDefault() *DokodemoSettings {
	if i.DokodemoSettings == nil {
		return &DokodemoSettings{Networks: "tcp,udp"}
	}
	return i.DokodemoSettings
}

func (i *Inbound) GetHysteriaSettingsOrDefault() *HysteriaSettings {
	if i.HysteriaSettings == nil {
		return &HysteriaSettings{}
	}
	return i.HysteriaSettings
}

// GetRoutingSettingsOrDefault returns the node's routing settings, or sensible defaults when nil.
func (n *Node) GetRoutingSettingsOrDefault() *RoutingSettings {
	if n.RoutingSettings == nil {
		return GetDefaultRoutingSettings()
	}
	return n.RoutingSettings
}

// DefaultAccessLogPath is the default file path for xray access logging on the agent.
const DefaultAccessLogPath = "/var/log/xray/access.log"

// GetAccessLogPath returns the effective access log path.
// If EnableAccessLog is true but LogAccess is empty, returns the default path.
func (n *Node) GetAccessLogPath() string {
	if n.LogAccess != "" {
		return n.LogAccess
	}
	if n.EnableAccessLog {
		return DefaultAccessLogPath
	}
	return ""
}

// GetDNSSettingsOrDefault returns the node's DNS settings, or nil when not configured.
func (n *Node) GetDNSSettingsOrDefault() *DNSSettings {
	return n.DNSSettings
}

// GetFakeDNSSettingsOrDefault returns the node's FakeDNS pools, or nil when not configured.
func (n *Node) GetFakeDNSSettingsOrDefault() []FakeDNSPool {
	return n.FakeDNSSettings
}

// Outbound represents a specific protocol/destination entry on a Node
type Outbound struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	NodeID uint `gorm:"not null;index" json:"node_id"`

	// Belongs To Relation
	Node *Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`

	// Tag must match the "tag" in Xray config.json 'outbounds'
	Tag string `gorm:"size:100;not null" json:"tag"`

	Protocol string `gorm:"size:20;not null" json:"protocol"` // "freedom", "blackhole", "socks", "vmess", etc.

	// Basic Settings
	Address string `gorm:"size:100" json:"address"` // Destination address (for SOCKS/VMess/Trojan)
	Port    int    `gorm:"default:0" json:"port"`   // Destination port

	// Stream Settings
	Network  string `gorm:"size:20" json:"network"`  // tcp, ws, grpc, etc.
	Security string `gorm:"size:20" json:"security"` // tls, reality, none

	// Detailed Settings (Stored as JSONB)
	TLSSettings       *TLSSettings       `gorm:"serializer:json;type:jsonb" json:"tls_settings"`
	RealitySettings   *RealitySettings   `gorm:"serializer:json;type:jsonb" json:"reality_settings"`
	TransportSettings *TransportSettings `gorm:"serializer:json;type:jsonb" json:"transport_settings"`

	// Protocol Specific Settings
	FreedomSettings     *FreedomSettings     `gorm:"serializer:json;type:jsonb" json:"freedom_settings"`
	BlackholeSettings   *BlackholeSettings   `gorm:"serializer:json;type:jsonb" json:"blackhole_settings"`
	ShadowsocksSettings *ShadowsocksSettings `gorm:"serializer:json;type:jsonb" json:"shadowsocks_settings"`
	WireGuardSettings   *WireGuardSettings   `gorm:"serializer:json;type:jsonb" json:"wireguard_settings"`
	HTTPSettings        *HTTPSettings        `gorm:"serializer:json;type:jsonb" json:"http_settings"`
	SOCKSSettings       *SOCKSSettings       `gorm:"serializer:json;type:jsonb" json:"socks_settings"`
	VMessSettings       *VMessSettings       `gorm:"serializer:json;type:jsonb" json:"vmess_settings"`
	VLESSSettings       *VLESSSettings       `gorm:"serializer:json;type:jsonb" json:"vless_settings"`
	TrojanSettings      *TrojanSettings      `gorm:"serializer:json;type:jsonb" json:"trojan_settings"`
	DNSOutboundSettings *DNSOutboundSettings `gorm:"serializer:json;type:jsonb" json:"dns_settings"`
	LoopbackSettings    *LoopbackSettings    `gorm:"serializer:json;type:jsonb" json:"loopback_settings"`
	SockoptSettings     *SockoptSettings     `gorm:"serializer:json;type:jsonb" json:"sockopt_settings"`
	HysteriaSettings    *HysteriaSettings    `gorm:"serializer:json;type:jsonb" json:"hysteria_settings"`
	MuxSettings         *MuxSettings         `gorm:"serializer:json;type:jsonb" json:"mux_settings"`
	ProxySettings       *ProxySettings       `gorm:"serializer:json;type:jsonb" json:"proxy_settings"`
	SendThrough         string               `gorm:"size:50" json:"send_through"`
	FinalMask           *FinalMask           `gorm:"serializer:json;type:jsonb" json:"finalmask"`

	Uplink   int64 `gorm:"default:0" json:"uplink"`
	Downlink int64 `gorm:"default:0" json:"downlink"`

	Remark string `gorm:"size:100" json:"remark"`

	// IsDisabled allows temporarily disabling an outbound without deleting it.
	// When disabled: excluded from Xray config push.
	IsDisabled bool `gorm:"default:false" json:"is_disabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// FreedomFragment for anti-censorship fragmentation
type FreedomFragment struct {
	Packets  string `json:"packets,omitempty"`  // "tlshello", "1-3"
	Length   string `json:"length,omitempty"`   // "100-200"
	Interval string `json:"interval,omitempty"` // "10-20"
	MaxSplit string `json:"maxSplit,omitempty"`
}

// FreedomNoise for anti-censorship noise injection
type FreedomNoise struct {
	Type    string `json:"type,omitempty"`   // "rand", "str", "base64"
	Packet  string `json:"packet,omitempty"` // noise content
	Delay   string `json:"delay,omitempty"`  // "10-20"
	ApplyTo string `json:"applyTo,omitempty"`
}

// FreedomSettings (Direct)
type FreedomSettings struct {
	DomainStrategy string           `json:"domainStrategy,omitempty"` // AsIs, UseIP, UseIPv4, UseIPv6
	Redirect       string           `json:"redirect,omitempty"`       // host:port
	UserLevel      int              `json:"userLevel,omitempty"`
	Fragment       *FreedomFragment `json:"fragment,omitempty"`
	Noise          []FreedomNoise   `json:"noise,omitempty"`
	ProxyProtocol  int              `json:"proxyProtocol,omitempty"` // 0=off, 1=v1, 2=v2
}

// BlackholeSettings (Block)
type BlackholeSettings struct {
	ResponseType string `json:"responseType,omitempty"` // none, http
}

// DNSOutboundSettings for DNS outbound protocol
type DNSOutboundSettings struct {
	Network    string  `json:"network,omitempty"` // tcp, udp
	Address    string  `json:"address,omitempty"` // DNS server address
	Port       int     `json:"port,omitempty"`    // DNS server port
	UserLevel  int     `json:"userLevel,omitempty"`
	NonIPQuery string  `json:"nonIPQuery,omitempty"` // drop, skip, reject
	BlockTypes []int32 `json:"blockTypes,omitempty"` // DNS record types to block (e.g. 28 for AAAA)
}

// LoopbackSettings for Loopback outbound protocol
type LoopbackSettings struct {
	InboundTag string `json:"inboundTag,omitempty"`
}

// Helper methods for Outbound

func (o *Outbound) GetTLSSettingsOrDefault() *TLSSettings {
	if o.TLSSettings == nil {
		return &TLSSettings{}
	}
	return o.TLSSettings
}

func (o *Outbound) GetRealitySettingsOrDefault() *RealitySettings {
	if o.RealitySettings == nil {
		return &RealitySettings{}
	}
	return o.RealitySettings
}

func (o *Outbound) GetTransportSettingsOrDefault() *TransportSettings {
	if o.TransportSettings == nil {
		return &TransportSettings{}
	}
	return o.TransportSettings
}

func (o *Outbound) GetFreedomSettingsOrDefault() *FreedomSettings {
	if o.FreedomSettings == nil {
		return &FreedomSettings{DomainStrategy: "AsIs"}
	}
	return o.FreedomSettings
}

func (o *Outbound) GetBlackholeSettingsOrDefault() *BlackholeSettings {
	if o.BlackholeSettings == nil {
		return &BlackholeSettings{ResponseType: "none"}
	}
	return o.BlackholeSettings
}

func (o *Outbound) GetShadowsocksSettingsOrDefault() *ShadowsocksSettings {
	if o.ShadowsocksSettings == nil {
		return &ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm", Network: "tcp,udp"}
	}
	return o.ShadowsocksSettings
}

func (o *Outbound) GetWireGuardSettingsOrDefault() *WireGuardSettings {
	if o.WireGuardSettings == nil {
		return &WireGuardSettings{MTU: 1420}
	}
	return o.WireGuardSettings
}

func (o *Outbound) GetHTTPSettingsOrDefault() *HTTPSettings {
	if o.HTTPSettings == nil {
		return &HTTPSettings{}
	}
	return o.HTTPSettings
}

func (o *Outbound) GetSOCKSSettingsOrDefault() *SOCKSSettings {
	if o.SOCKSSettings == nil {
		return &SOCKSSettings{Auth: "noauth"}
	}
	return o.SOCKSSettings
}

// VMessSettings for VMess outbound
type VMessSettings struct {
	UUID        string `json:"uuid,omitempty"`
	AlterId     int    `json:"alterId,omitempty"`
	Security    string `json:"security,omitempty"`    // auto, aes-128-gcm, chacha20-poly1305, none
	Experiments string `json:"experiments,omitempty"` // optional
}

// Fallback represents a VLESS/Trojan fallback destination
type Fallback struct {
	Name string      `json:"name,omitempty"` // Display name
	Alpn string      `json:"alpn,omitempty"` // ALPN match (e.g. "h2")
	Path string      `json:"path,omitempty"` // Path match
	Dest interface{} `json:"dest,omitempty"` // Destination (string or int)
	Xver int         `json:"xver,omitempty"` // Proxy protocol version (0, 1, 2)
	Type string      `json:"type,omitempty"`
}

// VLESSSettings for VLESS outbound
type VLESSSettings struct {
	UUID       string     `json:"uuid,omitempty"`
	Flow       string     `json:"flow"` // xtls-rprx-vision, empty = none
	Decryption string     `json:"decryption,omitempty"`
	Encryption string     `json:"encryption,omitempty"` // none
	Fallbacks  []Fallback `json:"fallbacks,omitempty"`
}

// TrojanSettings for Trojan outbound
type TrojanSettings struct {
	Password  string     `json:"password,omitempty"`
	Level     int        `json:"level,omitempty"` // Outbound only
	Email     string     `json:"email,omitempty"` // Outbound only
	Fallbacks []Fallback `json:"fallbacks,omitempty"`
}

// SockoptSettings for Inbound/Outbound socket options
type SockoptSettings struct {
	Mark                 uint32               `json:"mark,omitempty"`
	TcpFastOpen          bool                 `json:"tcpFastOpen,omitempty"`
	Tproxy               string               `json:"tproxy,omitempty"` // "redirect", "tproxy", "off"
	DomainStrategy       string               `json:"domainStrategy,omitempty"`
	DialerProxy          string               `json:"dialerProxy,omitempty"`
	TcpKeepAliveInterval int                  `json:"tcpKeepAliveInterval,omitempty"`
	TcpKeepAliveIdle     int                  `json:"tcpKeepAliveIdle,omitempty"`
	TcpCongestion        string               `json:"tcpCongestion,omitempty"`
	TcpWindowClamp       int                  `json:"tcpWindowClamp,omitempty"`
	TcpUserTimeout       int                  `json:"tcpUserTimeout,omitempty"`
	TcpMaxSeg            int                  `json:"tcpMaxSeg,omitempty"`
	TcpMptcp             bool                 `json:"tcpMptcp,omitempty"`
	AcceptProxyProtocol  bool                 `json:"acceptProxyProtocol,omitempty"`
	Interface            string               `json:"interface,omitempty"`
	V6Only               bool                 `json:"v6Only,omitempty"`
	Penetrate            bool                 `json:"penetrate,omitempty"`
	AddressPortStrategy  string               `json:"addressPortStrategy,omitempty"`
	TrustedXForwardedFor []string             `json:"trustedXForwardedFor,omitempty"`
	HappyEyeballs        *HappyEyeballsConfig `json:"happyEyeballs,omitempty"`
	CustomSockopt        []CustomSockopt      `json:"customSockopt,omitempty"`
}

// HappyEyeballsConfig for IPv4/IPv6 connection racing
type HappyEyeballsConfig struct {
	TryDelay       int `json:"tryDelay,omitempty"`
	MaxConcurrency int `json:"maxConcurrency,omitempty"`
}

// CustomSockopt for custom socket options
type CustomSockopt struct {
	Level    int         `json:"level,omitempty"`
	OptName  int         `json:"optName,omitempty"`
	OptValue interface{} `json:"optValue,omitempty"`
}

// FinalMask for packet masking
type FinalMask struct {
	TCP        json.RawMessage `json:"tcp,omitempty"`
	UDP        json.RawMessage `json:"udp,omitempty"`
	QuicParams json.RawMessage `json:"quicParams,omitempty"`
}

func (o *Outbound) GetVMessSettingsOrDefault() *VMessSettings {
	if o.VMessSettings == nil {
		return &VMessSettings{Security: "auto"}
	}
	return o.VMessSettings
}

func (o *Outbound) GetVLESSSettingsOrDefault() *VLESSSettings {
	if o.VLESSSettings == nil {
		return &VLESSSettings{Encryption: "none"}
	}
	return o.VLESSSettings
}

func (o *Outbound) GetTrojanSettingsOrDefault() *TrojanSettings {
	if o.TrojanSettings == nil {
		return &TrojanSettings{}
	}
	return o.TrojanSettings
}

func (o *Outbound) GetDNSOutboundSettingsOrDefault() *DNSOutboundSettings {
	if o.DNSOutboundSettings == nil {
		return &DNSOutboundSettings{}
	}
	return o.DNSOutboundSettings
}

func (o *Outbound) GetLoopbackSettingsOrDefault() *LoopbackSettings {
	if o.LoopbackSettings == nil {
		return &LoopbackSettings{}
	}
	return o.LoopbackSettings
}

func (o *Outbound) GetSockoptSettingsOrDefault() *SockoptSettings {
	if o.SockoptSettings == nil {
		return &SockoptSettings{}
	}
	return o.SockoptSettings
}

func (o *Outbound) GetHysteriaSettingsOrDefault() *HysteriaSettings {
	if o.HysteriaSettings == nil {
		return &HysteriaSettings{}
	}
	return o.HysteriaSettings
}

func (o *Outbound) GetMuxSettingsOrDefault() *MuxSettings {
	if o.MuxSettings == nil {
		return &MuxSettings{}
	}
	return o.MuxSettings
}

func (o *Outbound) GetProxySettingsOrDefault() *ProxySettings {
	if o.ProxySettings == nil {
		return &ProxySettings{}
	}
	return o.ProxySettings
}

func (Node) TableName() string {
	return "nodes"
}

func (Inbound) TableName() string {
	return "inbounds"
}

func (Outbound) TableName() string {
	return "outbounds"
}
