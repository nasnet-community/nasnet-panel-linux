package system

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const wgLinkPrefix = "nasnet-wg"

// MaxWGSlots matches domain.MaxEnabledProfiles; both count the oif window.
const MaxWGSlots = 8

func WGLinkNameFor(slot int) string { return fmt.Sprintf("%s%d", wgLinkPrefix, slot) }
func IsWGLink(name string) bool     { return strings.HasPrefix(name, wgLinkPrefix) }

// The pool's fixed identities. The table number is pinned like 201/202 are,
// so a snapshot taken by one build restores under another.
const (
	WGLinkName  = "nasnet-wg0"
	WGTable     = 203
	WGTableName = "nasnet-vpn"

	// StaleHandshakeAfter is when a tunnel counts as down. WireGuard rekeys
	// every 120 s, so anything past this has stopped answering.
	StaleHandshakeAfter = 180 * time.Second
)

// ErrNoWGDevice means the link is absent, which is the normal state whenever no
// profile is active.
var ErrNoWGDevice = errors.New("no WireGuard device")

// WGApplyConfig is everything needed to bring the tunnel up. Resolution has
// already happened: Endpoint is an address, never a name.
type WGApplyConfig struct {
	PrivateKey    string
	PeerPublicKey string
	PresharedKey  string
	Endpoint      netip.AddrPort
	AllowedIPs    []netip.Prefix
	Address       netip.Prefix
	MTU           int
	ListenPort    int
	Keepalive     time.Duration
	// DNS is the resolver to register on the link. Zero means none.
	DNS netip.Addr
	// FirewallMark is stamped on the tunnel's own transport sockets, which is
	// what pins them to the secondary uplink and keeps them off the domestic one.
	FirewallMark uint32
}

// WGStatus is what the kernel knows about the running tunnel.
type WGStatus struct {
	// LastHandshake is zero when the peer has never answered.
	LastHandshake time.Time
	RxBytes       int64
	TxBytes       int64
	Endpoint      string
	PublicKey     string
	ListenPort    int
}

// Connected reports the only honest liveness signal WireGuard offers. There is
// no link state to read: the interface is up whether or not anyone answers.
func (s *WGStatus) Connected() bool {
	return s != nil && !s.LastHandshake.IsZero() &&
		time.Since(s.LastHandshake) < StaleHandshakeAfter
}

// WGDevice owns the tunnel interfaces. Separate from Backend for the same
// reason DeviceSource is: Backend is the apply/rollback seam for routes and rules.
type WGDevice interface {
	// Ensure creates or reconfigures one link. Idempotent.
	Ensure(ctx context.Context, ifName string, cfg WGApplyConfig) error
	// UpdateEndpoint moves the peer to a new address without touching anything
	// else, for when a hostname endpoint has been re-resolved.
	UpdateEndpoint(ctx context.Context, ifName string, endpoint netip.AddrPort) error
	// Status returns ErrNoWGDevice when the link is absent.
	Status(ctx context.Context, ifName string) (*WGStatus, error)
	// Delete removes the link, which takes its addresses, its resolver
	// registration and its peer state with it. Idempotent.
	Delete(ctx context.Context, ifName string) error
	// List names the nasnet-wg* links present, so a reconcile can remove
	// devices whose profile left the pool.
	List(ctx context.Context) ([]string, error)
}
