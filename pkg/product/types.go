package product

import "time"

// ProductType represents different VPN/proxy product types
type ProductType string

const (
	ProductTypeXray      ProductType = "xray"
	ProductTypeOpenVPN   ProductType = "openvpn"
	ProductTypeWireGuard ProductType = "wireguard"
)

// IsValid checks if the product type is valid
func (p ProductType) IsValid() bool {
	switch p {
	case ProductTypeXray, ProductTypeOpenVPN, ProductTypeWireGuard:
		return true
	}
	return false
}

// String returns the string representation
func (p ProductType) String() string {
	return string(p)
}

// UsageStats represents usage statistics for a subscription
type UsageStats struct {
	Uplink   int64 `json:"uplink"`    // bytes uploaded
	Downlink int64 `json:"downlink"`  // bytes downloaded
	Total    int64 `json:"total"`     // total bytes
	IsOnline bool  `json:"is_online"` // currently connected
	LastSeen int64 `json:"last_seen"` // unix timestamp
}

// ProvisionResult tracks the outcome of provisioning on a single inbound
type ProvisionResult struct {
	InboundTag string
	NodeID     uint
	Success    bool
	Error      string
}

// ConfigResult represents generated config output
type ConfigResult struct {
	ConfigData     string            `json:"config_data"`  // the actual config content (or list of links)
	ConfigID       string            `json:"config_id"`    // unique identifier (UUID, etc)
	ConfigEmail    string            `json:"config_email"` // email/identifier for the user
	SubLink        string            `json:"sub_link"`     // subscription link for import
	FileExtension  string            `json:"file_ext"`     // .json, .ovpn, .conf, .txt
	FailedInbounds []ProvisionResult `json:"-"`            // inbounds that failed provisioning
}

// InboundDetail contains connection info for a specific server entry
// Used for multi-server provisioning
type InboundDetail struct {
	NodeID          uint   // Node Database ID
	Tag             string // Xray Inbound Tag
	Protocol        string // vmess, vless, etc
	LinkFormat      string // Template for generating link (if empty, generate from settings)
	NodeIP          string // Public-facing IP/address (may be CDN, used for link generation)
	ProvisionIP     string // Real server IP for gRPC/agent provisioning (never overridden by host address)
	APIPort         int    // Xray API Port
	AgentPort       int    // Agent API Port
	AgentCACert     string // CA Certificate for agent mTLS
	AgentClientCert string // Client Certificate for agent mTLS
	AgentClientKey  string // Client Key for agent mTLS
	PublicPort      int    // Connect Port
	Remark          string // Node name/Label
	NodeName        string // Node display name (for remark templates)
	CountryCode     string // ISO Country Code (e.g., "DE", "US")
	Network         string // tcp, ws, grpc, xhttp
	Security        string // tls, reality, none

	// TLS Settings
	TLSSni         string   // TLS SNI
	TLSALPN        []string // TLS ALPN
	TLSFingerprint string   // TLS Fingerprint

	// Reality Settings
	RealityPublicKey   string // Reality public key (pbk)
	RealityShortID     string // Reality short ID (sid)
	RealitySNI         string // Reality server name (sni)
	RealityFingerprint string // Reality fingerprint (fp)
	RealitySpiderX     string // Reality spiderX

	// Transport Settings
	TransportPath        string // Path for ws, xhttp, httpupgrade
	TransportHost        string // Host for ws, xhttp, httpupgrade
	TransportServiceName string // gRPC service name
	TransportHeaderType  string // TCP header type
	TransportMode        string // XHTTP mode

	// VLESS Settings
	VLESSFlow       string // XTLS flow (xtls-rprx-vision)
	VLESSEncryption string
	VLESSDecryption string

	// VMess Settings
	VMessAlterId  uint32
	VMessSecurity string // auto, aes-128-gcm, chacha20-poly1305, none, zero

	// Hysteria2
	HysteriaObfsPassword string // salamander obfs password (from inbound finalmask)
	PortRange            string // listener port range -> hysteria2 mport

	// WireGuard (per-subscription peer; empty when no device provisioned)
	WGPrivateKey      string // client private key (stored on the peer)
	WGServerPublicKey string // derived from the inbound's server secret key
	WGAddress         string // client tunnel IP (bare; rendered /32)
	WGPresharedKey    string // peer PSK
	WGMTU             int    // interface MTU
	WGReserved        []int  // 3 reserved bytes (optional)

	// Host overrides (presentation-layer)
	AllowInsecure    *bool         // Allow insecure TLS (from host override)
	Fragment         *FragmentInfo // Anti-censorship fragment settings (from host override)
	Priority         int           // From Host.Priority, for ordering
	RemarkIsTemplate bool          // If true, Remark has {variables} to render at output time
	HostID           uint          // 0 = no host (backward compat)
	IsInfoOnly       bool          // If true, generates a placeholder link (no real server)
}

// FragmentInfo carries Xray fragment outbound settings into the URI generator.
type FragmentInfo struct {
	Packets  string // e.g. "tlshello", "1-3"
	Length   string // e.g. "100-200"
	Interval string // e.g. "10-20"
}

// SubscriptionInfo contains info needed by providers
type SubscriptionInfo struct {
	ID             uint
	UserID         uint
	TelegramID     int64
	ConfigID       string
	Email          string
	DataLimit      int64
	DataUsed       int64     // Current data usage in bytes
	ExpiresAt      time.Time // Subscription expiry date (for {days_left} in remarks)
	PlanName       string
	Status         string // Subscription status (active, paused, expired, etc.)
	BandwidthLimit int    // Mbps, 0 = unlimited (from plan)

	// Deprecated: Legacy single-server template
	TemplateLink string

	// Multi-Server Support: List of inbounds this subscription has access to
	Inbounds []InboundDetail
}
