package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gorm.io/gorm"
)

// VPNTypeWireGuard is the only protocol implemented. The column exists so the
// second one does not need a second table.
const VPNTypeWireGuard = "wireguard"

// VPNProfile is one saved tunnel. At most one row is Active, enforced by a
// partial unique index in cmd/root.go.
//
// Config holds the protocol-shaped payload as JSON. Only what the panel queries
// or enforces gets a real column, so a second protocol costs no migration —
// the schema here is additive only.
type VPNProfile struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	Name string `gorm:"not null" json:"name"`
	Type string `gorm:"not null;default:'wireguard'" json:"type"`
	// Active is the retired single-tunnel flag; the migration drains it.
	Active bool `gorm:"not null;default:false" json:"-"`
	// Enabled puts the profile in the pool. Priority 0 is the best tier;
	// weight splits flows inside a tier.
	Enabled  bool `gorm:"not null;default:false" json:"enabled"`
	Priority int  `gorm:"not null;default:0" json:"priority"`
	Weight   int  `gorm:"not null;default:1" json:"weight"`
	// WGSlot names the interface (nasnet-wg{slot}). Nil while disabled.
	WGSlot *int `json:"wg_slot"`
	// TransportUplink is reserved for multi-secondary; empty and unused today.
	TransportUplink string `json:"transport_uplink,omitempty"`

	// Config is a marshalled WireGuardConfig. Served decoded, never raw.
	Config string `gorm:"type:text;not null" json:"-"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName pins the table: GORM's snake_case of an acronym-led name is not
// worth guessing, and a raw partial index has to name it.
func (VPNProfile) TableName() string { return "vpn_profiles" }

// WireGuardConfig is the payload of a wireguard profile.
//
// The private key is stored and served as written. That is the operator's
// call: anyone holding an admin session already holds the panel.
type WireGuardConfig struct {
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"` // this end's tunnel address, IPv4 CIDR
	DNS        string `json:"dns,omitempty"`
	MTU        int    `json:"mtu,omitempty"`         // 0 means DefaultWGMTU at apply
	ListenPort int    `json:"listen_port,omitempty"` // 0 means the kernel picks

	Peer WGPeerConfig `json:"peer"`

	// PinnedEndpointIP is what the endpoint hostname resolved to, kept so the
	// tunnel can come up before any resolver does.
	PinnedEndpointIP string `json:"pinned_endpoint_ip,omitempty"`

	// Notices records what the parser dropped or filled in, so the UI can say
	// so instead of the operator finding out from behaviour.
	Notices []string `json:"notices,omitempty"`

	// SuggestedName comes from a URI fragment. Never persisted as the name.
	SuggestedName string `json:"suggested_name,omitempty"`
}

// WGPeerConfig is the single remote peer. One only: the tunnel is the default
// route, and a second peer has no meaning under that model.
type WGPeerConfig struct {
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key,omitempty"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"` // host:port, as written
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
}

// Applied defaults. Starlink is behind CGNAT, so without a keepalive the
// mapping expires and the far side can never reach us again.
const (
	DefaultWGMTU       = 1420
	DefaultWGKeepalive = 25
	MinWGMTU           = 576
	MaxWGMTU           = 9000
)

// MaxEnabledProfiles matches the oif-rule window: 10 slots, two for uplinks.
const MaxEnabledProfiles = 8

var ErrPoolFull = errors.New("all 8 tunnel slots are in use")

func ValidatePoolRole(priority, weight int) error {
	if priority < 0 || priority > 7 {
		return errors.New("priority must be between 0 and 7")
	}
	if weight < 1 || weight > 100 {
		return errors.New("weight must be between 1 and 100")
	}
	return nil
}

var (
	ErrScriptKey     = errors.New("this config runs shell commands, which is refused")
	ErrReservedParam = errors.New("this config needs a userspace WireGuard client")
	ErrMultiplePeers = errors.New("more than one peer, which this router cannot route")
	ErrNoIPv4Address = errors.New("no IPv4 address, and this router is IPv4 only")
	ErrNotWireGuard  = errors.New("not a WireGuard URI or config file")
	ErrProfileActive = errors.New("this VPN is in use — turn it off first")
	// A stale list is not a server fault.
	ErrProfileNotFound = errors.New("no such VPN profile")
	ErrBadKey          = errors.New("not a WireGuard key")
	ErrBadEndpoint     = errors.New("not a host:port endpoint")
)

// ValidateWireGuardConfig checks everything the kernel would reject later, plus
// the IPv4-only constraint this router imposes.
func ValidateWireGuardConfig(c *WireGuardConfig) error {
	if _, err := wgtypes.ParseKey(c.PrivateKey); err != nil {
		return fmt.Errorf("private key: %w", ErrBadKey)
	}
	if _, err := wgtypes.ParseKey(c.Peer.PublicKey); err != nil {
		return fmt.Errorf("peer public key: %w", ErrBadKey)
	}
	if c.Peer.PresharedKey != "" {
		if _, err := wgtypes.ParseKey(c.Peer.PresharedKey); err != nil {
			return fmt.Errorf("preshared key: %w", ErrBadKey)
		}
	}

	addr, err := netip.ParsePrefix(c.Address)
	if err != nil || !addr.Addr().Is4() {
		return ErrNoIPv4Address
	}

	if _, err := ParseWGEndpoint(c.Peer.Endpoint); err != nil {
		return err
	}

	if len(c.Peer.AllowedIPs) == 0 {
		return errors.New("no allowed IPs, so the tunnel would carry nothing")
	}
	for _, s := range c.Peer.AllowedIPs {
		p, err := netip.ParsePrefix(s)
		if err != nil || !p.Addr().Is4() {
			return fmt.Errorf("allowed IP %q is not an IPv4 range", s)
		}
	}

	if c.DNS != "" {
		ip, err := netip.ParseAddr(c.DNS)
		if err != nil || !ip.Is4() {
			return fmt.Errorf("DNS %q is not an IPv4 address", c.DNS)
		}
	}
	if c.MTU != 0 && (c.MTU < MinWGMTU || c.MTU > MaxWGMTU) {
		return fmt.Errorf("MTU must be between %d and %d", MinWGMTU, MaxWGMTU)
	}
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return errors.New("listen port is out of range")
	}
	if c.Peer.PersistentKeepalive < 0 || c.Peer.PersistentKeepalive > 65535 {
		return errors.New("keepalive is out of range")
	}
	return nil
}

// ParseWGEndpoint splits host:port without resolving anything. The host may be
// a name; resolution is a separate step that has to happen over the bootstrap.
func ParseWGEndpoint(s string) (host string, err error) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", ErrBadEndpoint
	}
	host, portStr := s[:i], s[i+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", ErrBadEndpoint
	}
	if strings.ContainsAny(host, " \t/") {
		return "", ErrBadEndpoint
	}
	return host, nil
}

// CoversDefaultRoute reports whether the peer would carry general traffic.
// Anything narrower still applies, but everything it misses is dropped by the
// kill switch rather than falling back to the raw uplink.
func CoversDefaultRoute(allowedIPs []string) bool {
	for _, s := range allowedIPs {
		if p, err := netip.ParsePrefix(s); err == nil && p.Bits() == 0 && p.Addr().Is4() {
			return true
		}
	}
	return false
}

// GenerateWGKeypair makes a key for an operator standing up their own server.
func GenerateWGKeypair() (priv, pub string, err error) {
	k, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", err
	}
	return k.String(), k.PublicKey().String(), nil
}

// WGPublicKeyOf derives the public half, so the UI never has to store it.
func WGPublicKeyOf(privateKey string) (string, error) {
	k, err := wgtypes.ParseKey(privateKey)
	if err != nil {
		return "", ErrBadKey
	}
	return k.PublicKey().String(), nil
}
