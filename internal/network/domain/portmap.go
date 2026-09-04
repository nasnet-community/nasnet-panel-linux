package domain

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PortMapRule asks the router in front of this box to forward a port here.
// The inbound rows are mapped automatically; these are the operator's extras.
type PortMapRule struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	// UplinkKey names the WAN to map on (empty means every WAN)
	UplinkKey string `gorm:"not null;default:''" json:"uplink_key"`

	Proto string `gorm:"not null" json:"proto"` // "tcp" | "udp"
	Port  int    `gorm:"not null" json:"port"`
	// ExternalHint is the external port to ask for; zero asks for Port.
	ExternalHint int    `gorm:"not null;default:0" json:"external_hint"`
	Comment      string `json:"comment"`
	Enabled      bool   `gorm:"not null;default:true" json:"enabled"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// PortMapRuleInput is everything a rule validation needs.
type PortMapRuleInput struct {
	Existing  []PortMapRule
	New       PortMapRule
	PanelPort int
	SSHPort   int
	// InboundPorts is proto -> ports of the enabled inbounds, which map
	// automatically — a manual duplicate would fight the reconciler.
	InboundPorts map[string][]int
	// Forwards are the DNAT rows. A port that is forwarded onward does not
	// terminate on this box, so a mapping for it opens somebody else's device.
	Forwards   []PortForward
	UplinkKeys []string
}

// ValidatePortMapRule runs V43-V46. Pure.
func ValidatePortMapRule(in PortMapRuleInput) []Verdict {
	var vs []Verdict
	reject := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelReject, Message: fmt.Sprintf(format, args...)})
	}
	confirm := func(rule, format string, args ...any) {
		vs = append(vs, Verdict{Rule: rule, Level: LevelConfirm, Message: fmt.Sprintf(format, args...)})
	}
	r := in.New

	// V43 — fields first; a malformed row cannot be collision-checked.
	switch r.Proto {
	case "tcp", "udp":
	default:
		reject("V43", "protocol %q must be tcp or udp", r.Proto)
		return vs
	}
	if r.Port < 1 || r.Port > 65535 {
		reject("V43", "port %d is out of range", r.Port)
		return vs
	}
	if r.ExternalHint < 0 || r.ExternalHint > 65535 {
		reject("V43", "external port %d is out of range", r.ExternalHint)
		return vs
	}
	if r.UplinkKey != "" {
		known := false
		for _, k := range in.UplinkKeys {
			if k == r.UplinkKey {
				known = true
			}
		}
		if !known {
			reject("V43", "uplink %q is not an assigned uplink", r.UplinkKey)
			return vs
		}
	}

	// A disabled row asks for nothing, so nothing can collide with it and
	// nothing is exposed. Turning one off must never need a confirmation.
	if !r.Enabled {
		return vs
	}

	// V44 — collisions. Empty UplinkKey means every WAN, so it collides with
	// every per-uplink row on the same (proto, port).
	sameUplink := func(a, b string) bool { return a == "" || b == "" || a == b }
	for _, port := range in.InboundPorts[r.Proto] {
		if port == r.Port {
			reject("V44", "%s/%d is an enabled xray inbound — it is mapped automatically", r.Proto, r.Port)
			return vs
		}
	}
	for _, ex := range in.Existing {
		if ex.ID == r.ID || !ex.Enabled {
			continue
		}
		if ex.Proto == r.Proto && ex.Port == r.Port && sameUplink(ex.UplinkKey, r.UplinkKey) {
			reject("V44", "%s/%d already has a mapping rule on this uplink", r.Proto, r.Port)
			return vs
		}
	}

	// V45 — legal and dangerous: handing an admin plane to the internet.
	if r.Proto == "tcp" && in.PanelPort > 0 && r.Port == in.PanelPort {
		confirm("V45", "this maps the panel (port %d) onto the internet. Anyone who reaches "+
			"the upstream router's address can attempt to log in. Type CONFIRM to continue.", r.Port)
	}
	if r.Proto == "tcp" && in.SSHPort > 0 && r.Port == in.SSHPort {
		confirm("V45", "this maps SSH (port %d) onto the internet. Anyone who reaches "+
			"the upstream router's address can attempt to log in. Type CONFIRM to continue.", r.Port)
	}

	// V46 — the port is forwarded onward, so the mapping reaches that device
	// and not this box. Legal, occasionally the whole point behind two NATs,
	// never something to discover by accident.
	for _, f := range in.Forwards {
		if !f.Enabled || f.Proto != r.Proto || f.DPort != r.Port {
			continue
		}
		if !sameUplink(f.UplinkKey, r.UplinkKey) {
			continue
		}
		confirm("V46", "%s/%d is forwarded to %s:%d, so this mapping opens that device to the "+
			"internet, not this box. Type CONFIRM to continue.", r.Proto, r.Port, f.ToAddr, f.ToPort)
		break
	}

	return vs
}
