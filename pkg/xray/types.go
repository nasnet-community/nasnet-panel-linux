package xray

// InboundConfig holds all necessary parameters to build an Xray InboundHandlerConfig.
type InboundConfig struct {
	Tag      string
	Protocol string // vmess, vless, trojan, shadowsocks
	Port     uint32
	Listen   string // "0.0.0.0" or specific IP

	// Stream Settings
	Network  string // tcp, ws, grpc, xhttp, httpupgrade
	Security string // tls, reality, none

	// Detailed Configuration Objects
	TLS      *TLSConfig
	Reality  *RealityConfig
	WS       *WSConfig
	GRPC     *GRPCConfig
	XHTTP    *XHTTPConfig
	VLESS    *VLESSConfig
	Sniffing *SniffingConfig
	Sockopt  *SockoptConfig
}

type VLESSConfig struct {
	Flow       string
	Decryption string
	Encryption string
}

type TLSConfig struct {
	ServerName  string
	ALPN        []string
	MinVersion  string // e.g. "1.2"
	MaxVersion  string // e.g. "1.3"
	CertPath    string // Path to certificate file on server
	KeyPath     string // Path to key file on server
	CertContent string // Raw certificate content (PEM)
	KeyContent  string // Raw key content (PEM)
	EnableSelf  bool   // Generate self-signed (if applicable)
	Fingerprint string // uTLS fingerprint
}

type RealityConfig struct {
	Show        bool
	Dest        string // Fallback destination (e.g., google.com:443)
	Xver        uint64
	ServerNames []string
	PrivateKey  string
	PublicKey   string // Useful for link generation
	ShortIDs    []string
	Fingerprint string // chrome, firefox, etc.
	SpiderX     string
}

type WSConfig struct {
	Path   string
	Host   string
	Header map[string]string
}

type GRPCConfig struct {
	ServiceName string
	MultiMode   bool // true for "multi", false for "gun"
}

// XHTTPConfig for XHTTP/SplitHTTP transport
type XHTTPConfig struct {
	Host    string            // Host header
	Path    string            // URL path (e.g., "/download")
	Mode    string            // "auto", "packet-up", "stream-up", "stream-one"
	Headers map[string]string // Custom headers
	Extra   string            // Extra JSON settings

	// Advanced settings (optional)
	NoGRPCHeader       bool  // Disable gRPC header
	NoSSEHeader        bool  // Disable SSE header
	ScMaxBufferedPosts int64 // Max buffered posts
}

type SniffingConfig struct {
	Enabled      bool
	DestOverride []string // "http", "tls", "quic"
	MetadataOnly bool
}

// User represents an xray user
type User struct {
	Email      string
	UUID       string
	Level      uint32
	Protocol   Protocol
	AlterId    uint32 // for VMess
	Flow       string // for VLESS XTLS
	Encryption string // for VLESS
	Decryption string // for VLESS

	// WireGuard peers (xray-core >= v26.6.27 exposes a UserManager on the WG
	// inbound, so a peer can be added/removed without rewriting the config).
	// PublicKey/PreSharedKey take the usual base64 WireGuard form; AllowedIPs
	// must be prefixes ("10.8.0.2/32"), since the core parses them as such.
	PublicKey    string
	PreSharedKey string
	AllowedIPs   []string
	KeepAlive    int
}

type Protocol string

const (
	ProtocolVMess       Protocol = "vmess"
	ProtocolVLESS       Protocol = "vless"
	ProtocolTrojan      Protocol = "trojan"
	ProtocolShadowsocks Protocol = "shadowsocks"
	ProtocolHysteria2   Protocol = "hysteria2"
	ProtocolWireGuard   Protocol = "wireguard"
)

// UserStats represents traffic statistics for a user
type UserStats struct {
	Email    string
	Uplink   int64 // bytes uploaded
	Downlink int64 // bytes downloaded
}

// Stat represents a single statistic entry
type Stat struct {
	Name  string
	Value int64
}

// SysStats represents system-level statistics
type SysStats struct {
	NumGoroutine uint32
	NumGC        uint32
	Alloc        uint64
	TotalAlloc   uint64
	Sys          uint64
	Mallocs      uint64
	Frees        uint64
	LiveObjects  uint64
	PauseTotalNs uint64
	Uptime       uint32
}

// InboundInfo represents information about an inbound handler (for discovery/listing)
type InboundInfo struct {
	Tag      string
	Protocol string
	Port     uint32
	Listen   string
	Flow     string // VLESS XTLS Flow
	// Stream Settings
	Network       string // tcp, ws, grpc, http, xhttp, httpupgrade, kcp, quic
	Security      string // tls, reality, none
	TLSConfig     *TLSInfoConfig
	RealityConfig *RealityInfoConfig
	// WebSocket settings
	WSPath string
	WSHost string
	// gRPC settings
	GRPCServiceName string
	GRPCAuthority   string
	GRPCMode        string
	// HTTP/2 settings
	HTTPPath string
	// TCP settings
	HeaderType string
	// XHTTP (splithttp) settings
	XHTTPPath  string
	XHTTPHost  string
	XHTTPMode  string
	XHTTPExtra string
	// HTTPUpgrade settings
	HTTPUpgradePath string
	HTTPUpgradeHost string
	// VLESS settings
	VLESSEncryption string // MLKEM encryption key
	VLESSDecryption string // MLKEM decryption key
	// VMess settings
	VMessAlterId  uint32
	VMessSecurity string // auto, aes-128-gcm, chacha20-poly1305, none, zero
	// Anti-censorship fragment settings (rendered into URI as fragment=...)
	Fragment *FragmentInfoConfig
	// Sniffing settings
	SniffingEnabled      bool
	SniffingDestOverride []string
	SniffingMetadataOnly bool
	SniffingRouteOnly    bool
	// Hysteria2
	HysteriaObfsPassword string // salamander obfs password (from finalmask); empty = no obfs
	PortRange            string // listener port range -> hysteria2 "mport" (port hopping)
	// WireGuard
	WGPrivateKey      string // client private key
	WGServerPublicKey string // server public key
	WGAddress         string // client tunnel IP (bare; rendered /32)
	WGPresharedKey    string // peer PSK
	WGMTU             int    // interface MTU
	WGReserved        []int  // 3 reserved bytes (optional)
}

// FragmentInfoConfig holds Xray fragment outbound settings for URI generation.
type FragmentInfoConfig struct {
	Packets  string
	Length   string
	Interval string
}

// TLSInfoConfig holds TLS settings for an inbound (read-only discovery)
type TLSInfoConfig struct {
	SNI           string
	ALPN          []string
	Fingerprint   string
	AllowInsecure bool
	CertPath      string // Certificate file path (if using path mode)
	KeyPath       string // Key file path (if using path mode)
	CertContent   string // Certificate content (if embedded)
	KeyContent    string // Key content (if embedded)
}

// RealityInfoConfig holds Reality settings for an inbound (read-only discovery)
type RealityInfoConfig struct {
	PublicKey   string
	ShortID     string
	ServerName  string
	Fingerprint string
	SpiderX     string
}

// OnlineUser represents an online user with their IPs
type OnlineUser struct {
	Email string
	IPs   map[string]int64 // IP -> connection count
}

// === Outbound Types ===

// OutboundConfig holds all necessary parameters to build an Xray OutboundHandlerConfig.
type OutboundConfig struct {
	Tag      string
	Protocol string // freedom, blackhole, vless, vmess, trojan, socks, http, shadowsocks

	// Destination (for proxy protocols)
	Address string // Server address
	Port    uint32 // Server port

	// Protocol-specific settings
	// Protocol-specific settings
	UUID     string // VLESS/VMess
	Flow     string // VLESS xtls-rprx-vision
	AlterId  uint32 // VMess legacy
	Password string // Trojan/Shadowsocks
	Method   string // Shadowsocks encryption method

	// Expanded Protocol Settings
	Level       uint32 // VLESS/VMess/Trojan/Shadowsocks
	Email       string // VLESS/VMess/Trojan/Shadowsocks
	Encryption  string // VLESS
	Experiments string // VMess
	IVCheck     bool   // Shadowsocks
	UoT         bool   // Shadowsocks UDP over TCP
	UoTVersion  int    // Shadowsocks UoT Version

	// WireGuard
	WireGuard *WireGuardOutboundConfig

	// SOCKS/HTTP auth
	Username string
	Pass     string

	// Freedom protocol
	DomainStrategy string // AsIs, UseIP, UseIPv4, UseIPv6
	Redirect       string // Force destination

	// Stream Settings
	Network  string // tcp, ws, grpc, xhttp, httpupgrade
	Security string // tls, reality, none

	// Detailed Configuration Objects
	TLS     *OutboundTLSClientConfig
	Reality *OutboundRealityClientConfig
	WS      *WSConfig
	GRPC    *GRPCConfig
	XHTTP   *XHTTPConfig
	Sockopt *SockoptConfig
}

type SockoptConfig struct {
	Mark                 uint32
	TcpFastOpen          bool
	Tproxy               string // "off", "redirect", "tproxy"
	DomainStrategy       string
	DialerProxy          string
	TcpKeepAliveInterval int32
	TcpMptcp             bool
	TcpNoDelay           bool
	Interface            string
	V6Only               bool
}

// OutboundTLSClientConfig holds client-side TLS settings for outbound
type OutboundTLSClientConfig struct {
	ServerName    string
	ALPN          []string
	Fingerprint   string // uTLS fingerprint
	AllowInsecure bool
}

// OutboundRealityClientConfig holds client-side Reality settings for outbound
type OutboundRealityClientConfig struct {
	ServerName  string
	Fingerprint string // chrome, firefox, etc.
	PublicKey   string
	ShortID     string
	SpiderX     string
}

// OutboundInfo represents information about an outbound handler (for discovery/listing)
type OutboundInfo struct {
	// Basic
	Tag      string
	Protocol string
	Address  string
	Port     uint32

	// Stream Settings
	Network  string // tcp, ws, grpc, xhttp, httpupgrade
	Security string // tls, reality, none

	// User info (from protocol.User)
	Level uint32
	Email string

	// Protocol-specific credentials
	UUID       string // VLESS, VMess
	Flow       string // VLESS
	Encryption string // VLESS, VMess security
	AlterID    uint32 // VMess
	Password   string // Trojan, Shadowsocks, SOCKS/HTTP
	Method     string // Shadowsocks cipher
	Username   string // SOCKS, HTTP

	// Shadowsocks extras
	IVCheck    bool
	UoT        bool
	UoTVersion int

	// Freedom
	DomainStrategy string
	Redirect       string

	// TLS Settings
	TLSServerName  string
	TLSFingerprint string
	TLSALPN        []string
	AllowInsecure  bool

	// Reality Settings
	RealityServerName  string
	RealityFingerprint string
	RealityPublicKey   string
	RealityShortID     string
	RealitySpiderX     string

	// Transport settings
	WSPath          string
	WSHost          string
	GRPCServiceName string
	XHTTPPath       string
	XHTTPHost       string
	XHTTPMode       string
	HTTPUpgradePath string
	HTTPUpgradeHost string

	// Sockopt settings
	SockoptMark           uint32
	SockoptTcpFastOpen    bool
	SockoptTproxy         string
	SockoptDomainStrategy string
	SockoptDialerProxy    string
	SockoptTcpMptcp       bool
	SockoptInterface      string
}

// WireGuardOutboundConfig holds WireGuard outbound settings for the protobuf builder.
type WireGuardOutboundConfig struct {
	SecretKey      string
	Endpoint       []string // Local tunnel addresses (CIDR)
	MTU            int
	NumWorkers     int
	Reserved       []byte
	DomainStrategy string
	NoKernelTun    bool
	Peers          []WireGuardPeerConfig
}

// WireGuardPeerConfig represents a WireGuard peer for the protobuf builder.
type WireGuardPeerConfig struct {
	PublicKey    string
	PreSharedKey string
	Endpoint     string
	KeepAlive    int
	AllowedIPs   []string
}

// === Routing Rule Types ===

// RoutingRuleConfig for building xray routing rules
type RoutingRuleConfig struct {
	RuleTag      string // Unique identifier for this rule
	OutboundTag  string // Target outbound tag (or empty if using balancing)
	BalancingTag string // Target balancer tag (or empty if using outbound)

	// Destination Matchers
	Domains     []DomainConfig // Domain matching patterns
	GeoIP       []string       // Country codes: ["cn", "ir", "ru"]
	IPCIDR      []string       // CIDR blocks: ["10.0.0.0/8", "192.168.0.0/16"]
	Ports       []string       // Port ranges: ["80", "443", "1000-2000"]
	Networks    []string       // Networks: ["tcp", "udp"]
	Protocols   []string       // Sniffed protocols: ["http", "tls", "bittorrent"]
	InboundTags []string       // Match traffic from specific inbounds
	UserEmails  []string       // Match specific user emails

	// Source Matchers
	SourceIPs   []string // Source client IPs/CIDRs
	SourcePorts []string // Source port ranges

	// Advanced Matchers
	Attributes   map[string]string // HTTP header attrs: key -> regex pattern
	ProcessNames []string          // Process name matching
	LocalIPs     []string          // Local/bind IP matching
	LocalPorts   []string          // Local/bind port matching
	VlessRoutes  []string          // VLESS Reverse Proxy route ports (also Hysteria)

	// Webhook fires on match. Empty URL disables it.
	WebhookURL           string
	WebhookDeduplication uint32
	WebhookHeaders       map[string]string

	ShouldAppend bool // If true, append to rule list (vs prepend)
}

// DomainConfig for domain matching
type DomainConfig struct {
	Type  string // "plain", "regex", "domain", "full"
	Value string // e.g., "google.com", "geosite:cn"
}

// RoutingRuleInfo for discovered/listed rules
type RoutingRuleInfo struct {
	RuleTag      string
	OutboundTag  string
	BalancingTag string

	// Matcher counts (quick summary)
	DomainCount int
	GeoIPCount  int
	IPCIDRCount int
	PortCount   int

	// Full matcher data
	Domains     []DomainConfig
	GeoIP       []string
	IPCIDR      []string
	Ports       []string
	Networks    []string
	Protocols   []string
	InboundTags []string
	UserEmails  []string
}
