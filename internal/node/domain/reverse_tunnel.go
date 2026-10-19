package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"gorm.io/gorm"
)

// ReverseTagPrefix starts every tag and email a reverse tunnel generates.
// Operator-made inbounds, outbounds and balancers may not use it.
const ReverseTagPrefix = "rvs-"

// ReverseExitTag is the Freedom exit tunnels leave the bridge through unless
// they name another one: public addresses allowed, private ones blocked.
const ReverseExitTag = "rvs-exit"

// Tunnel kinds, derived from which ends the panel manages.
const (
	ReverseTunnelNodes          = "nodes"
	ReverseTunnelExternalBridge = "external_bridge" // panel node is the portal
	ReverseTunnelExternalPortal = "external_portal" // panel node is the bridge
)

// Tunnel states. An empty state means the portal hasn't been observed yet.
const (
	ReverseTunnelConnected = "connected"
	ReverseTunnelDown      = "down"
)

// ReverseTunnel is a VLESS Reverse tunnel. The bridge dials the portal, and the
// portal sends whatever is routed to PortalTag out through the bridge.
type ReverseTunnel struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:100;not null" json:"name"`

	// Portal end. A nil node is a portal the panel doesn't manage.
	PortalNodeID    *uint  `gorm:"index" json:"portal_node_id"`
	PortalInboundID *uint  `json:"portal_inbound_id"`
	PortalHostID    *uint  `json:"portal_host_id"`
	PortalTag       string `gorm:"size:100;not null" json:"portal_tag"`
	// InboundTags sends whole portal inbounds into the tunnel through the
	// managed routing rule RuleID.
	InboundTags jsontype.StringSlice `gorm:"serializer:json;type:jsonb" json:"inbound_tags"`
	RuleID      *uint                `json:"rule_id"`

	// Bridge end. A nil node is a bridge the panel doesn't manage.
	BridgeNodeID    *uint  `gorm:"index" json:"bridge_node_id"`
	BridgeTag       string `gorm:"size:100;not null" json:"bridge_tag"`
	ExitOutboundTag string `gorm:"size:100" json:"exit_outbound_tag"`
	ExitBalancerTag string `gorm:"size:100" json:"exit_balancer_tag"`

	UUID string `gorm:"size:64;not null" json:"uuid"`
	// PortalLink is the external portal's vless:// link the bridge dials.
	PortalLink string `gorm:"type:text" json:"portal_link,omitempty"`

	State      string     `gorm:"size:20" json:"state"`
	StateSince *time.Time `json:"state_since,omitempty"`
	BridgeIP   string     `gorm:"size:100" json:"bridge_ip,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	ToBridge   int64      `gorm:"default:0" json:"to_bridge"`
	FromBridge int64      `gorm:"default:0" json:"from_bridge"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ReverseTunnel) TableName() string { return "reverse_tunnels" }

// Kind reports which ends the panel manages.
func (t *ReverseTunnel) Kind() string {
	switch {
	case t.PortalNodeID != nil && t.BridgeNodeID != nil:
		return ReverseTunnelNodes
	case t.PortalNodeID != nil:
		return ReverseTunnelExternalBridge
	default:
		return ReverseTunnelExternalPortal
	}
}

// Email is the hidden portal user the bridge authenticates as.
func (t *ReverseTunnel) Email() string { return fmt.Sprintf("%st%d@reverse", ReverseTagPrefix, t.ID) }

// LinkTag is the bridge's outbound that dials the portal.
func (t *ReverseTunnel) LinkTag() string { return fmt.Sprintf("%slink-%d", ReverseTagPrefix, t.ID) }

// Observable reports whether the panel can see the bridge connect: only the
// portal tracks it.
func (t *ReverseTunnel) Observable() bool { return t.PortalNodeID != nil }

// ReverseExitCopyTag names the copy of a Freedom outbound that tunnel traffic
// leaves through, with the LAN-blocking final rules added.
func ReverseExitCopyTag(outboundID uint) string {
	return fmt.Sprintf("%s-%d", ReverseExitTag, outboundID)
}

// IsReverseEmail reports whether email is a hidden reverse credential rather
// than a customer: tunnels use rvs-t<id>@reverse, hand-built portals
// rvs-p<id>@reverse.
func IsReverseEmail(email string) bool {
	return strings.HasPrefix(email, ReverseTagPrefix) && strings.HasSuffix(email, "@reverse")
}

// ReverseTunnelIDFromEmail returns the tunnel a hidden email belongs to.
func ReverseTunnelIDFromEmail(email string) (uint, bool) {
	if !IsReverseEmail(email) {
		return 0, false
	}
	return parseReverseID(strings.TrimSuffix(strings.TrimPrefix(email, ReverseTagPrefix+"t"), "@reverse"))
}

// ReverseTunnelIDFromLinkTag returns the tunnel a bridge link outbound belongs to.
func ReverseTunnelIDFromLinkTag(tag string) (uint, bool) {
	rest, ok := strings.CutPrefix(tag, ReverseTagPrefix+"link-")
	if !ok {
		return 0, false
	}
	return parseReverseID(rest)
}

func parseReverseID(s string) (uint, bool) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

// ReverseTagSlug turns a node name into the part of a tunnel tag after rvs-.
func ReverseTagSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		slug = "tunnel"
	}
	return slug
}
