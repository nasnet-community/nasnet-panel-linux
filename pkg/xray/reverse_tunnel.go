package xray

import (
	"strings"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// ReversePortal is the hidden client a reverse tunnel adds to a portal inbound.
// Only this client may register the bridge, and Xray then routes Tag to it.
type ReversePortal struct {
	InboundTag string
	UUID       string
	Email      string
	Tag        string
	// RuleID is the managed rule sending whole inbounds into the tunnel. It is
	// kept ahead of a general fallback like a hand-built portal's rule.
	RuleID *uint
}

// ReverseBridge is one tunnel's bridge end: the outbound that dials the portal
// and the exit its traffic leaves through.
type ReverseBridge struct {
	Tag             string                 // inbound tag the tunnel's traffic arrives on
	Outbound        map[string]interface{} // dials the portal; tagged, flat settings with reverse
	ExitOutboundTag string                 // empty: the managed exit
	ExitBalancerTag string
}

// WithReverseTunnels sets the tunnel ends this node runs.
func (b *FullConfigBuilder) WithReverseTunnels(portals []ReversePortal, bridges []ReverseBridge) *FullConfigBuilder {
	b.reversePortals = portals
	b.reverseBridges = bridges
	return b
}

// reverseExitRules keep tunnel traffic off the bridge's LAN. Xray blocks all
// vless-reverse traffic at Freedom unless a final rule allows it, and checks
// the resolved and dialed addresses of domains against these too. The ranges
// are Xray's own private set, spelled out: geoip:private needs geoip.dat, and
// without it the bridge's whole config would fail to load.
func reverseExitRules() []map[string]interface{} {
	return []map[string]interface{}{
		{"action": "block", "ip": []string{
			"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
			"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
			"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3",
			"::/127", "fc00::/7", "fe80::/10", "ff00::/8",
		}},
		{"action": "allow"},
	}
}

// ReverseExitOutbound is the managed exit tunnel traffic leaves a bridge
// through.
func ReverseExitOutbound() map[string]interface{} {
	return map[string]interface{}{
		"tag":      nodeDomain.ReverseExitTag,
		"protocol": "freedom",
		"settings": map[string]interface{}{"finalRules": reverseExitRules()},
	}
}

// applyReverseTunnels runs after bandwidth routing: tunnel plumbing is never
// shaped (a tunnel's traffic carries no customer) and must not be cloned.
func (b *FullConfigBuilder) applyReverseTunnels(c *OrderedConfig) {
	for _, p := range b.reversePortals {
		for _, in := range c.Inbounds {
			if in["tag"] != p.InboundTag || in["protocol"] != "vless" {
				continue
			}
			settings, _ := in["settings"].(map[string]interface{})
			if settings == nil {
				continue
			}
			clients, _ := settings["clients"].([]interface{})
			settings["clients"] = append(clients, b.reverseClient(p.InboundTag, p.UUID, p.Email, p.Tag))
		}
	}

	exitUsed := b.handBuiltExitUsed()
	copies := map[string]bool{}
	var rules []map[string]interface{}
	for _, br := range b.reverseBridges {
		c.Outbounds = append(c.Outbounds, br.Outbound)
		rule := map[string]interface{}{"type": "field", "inboundTag": []string{br.Tag}}
		switch {
		case br.ExitBalancerTag != "":
			rule["balancerTag"] = br.ExitBalancerTag
		case br.ExitOutboundTag != "":
			target := br.ExitOutboundTag
			if out := b.freedomOutbound(target); out != nil {
				target = nodeDomain.ReverseExitCopyTag(out.ID)
				if !copies[target] {
					copies[target] = true
					c.Outbounds = append(c.Outbounds, b.reverseExitCopy(out, target))
				}
			}
			rule["outboundTag"] = target
		default:
			exitUsed = true
			rule["outboundTag"] = nodeDomain.ReverseExitTag
		}
		rules = append(rules, rule)
	}
	if exitUsed {
		c.Outbounds = append(c.Outbounds, ReverseExitOutbound())
	}
	if len(rules) == 0 {
		return
	}
	// The inbound tags are the tunnels' own, so the rules can't shadow anything:
	// put them first, after the API rule, where no operator rule catches them.
	existing, _ := c.Routing["rules"].([]map[string]interface{})
	at := 0
	for i, rule := range existing {
		if tags, ok := rule["inboundTag"].([]string); ok && len(tags) == 1 && tags[0] == "api" {
			at = i + 1
			break
		}
	}
	merged := make([]map[string]interface{}, 0, len(existing)+len(rules))
	merged = append(merged, existing[:at]...)
	merged = append(merged, rules...)
	merged = append(merged, existing[at:]...)
	c.Routing["rules"] = merged
}

// reverseClient is a portal's hidden client. It takes the inbound's default
// flow, as the bridge's link does.
func (b *FullConfigBuilder) reverseClient(inboundTag, uuid, email, tag string) map[string]interface{} {
	flow := ""
	for _, in := range b.inbounds {
		if in != nil && in.Tag == inboundTag {
			flow = in.GetVLESSSettingsOrDefault().Flow
		}
	}
	return map[string]interface{}{
		"id":      uuid,
		"flow":    flow,
		"email":   email,
		"level":   0,
		"reverse": map[string]interface{}{"tag": tag},
	}
}

// handBuiltExitUsed reports whether a hand-built bridge sends its traffic to
// the managed exit.
func (b *FullConfigBuilder) handBuiltExitUsed() bool {
	for _, rp := range b.reverseProxies {
		if rp.Type == "bridge" && rp.OutboundTag == nodeDomain.ReverseExitTag {
			return true
		}
	}
	return false
}

// freedomOutbound returns the enabled Freedom outbound tagged tag, if any.
func (b *FullConfigBuilder) freedomOutbound(tag string) *nodeDomain.Outbound {
	for _, out := range b.outbounds {
		if out != nil && out.Tag == tag && out.ManagedPoolID == nil && strings.EqualFold(out.Protocol, "freedom") {
			return out
		}
	}
	return nil
}

// reverseExitCopy is a Freedom exit the operator picked, copied so tunnel
// traffic may leave through it: its own final rules first, then the LAN block.
func (b *FullConfigBuilder) reverseExitCopy(out *nodeDomain.Outbound, tag string) map[string]interface{} {
	exit := deepCopyMap(b.convertOutbound(out))
	exit["tag"] = tag
	settings, _ := exit["settings"].(map[string]interface{})
	if settings == nil {
		settings = map[string]interface{}{}
		exit["settings"] = settings
	}
	rules, _ := settings["finalRules"].([]map[string]interface{})
	settings["finalRules"] = append(rules, reverseExitRules()...)
	return exit
}

// managedReverseRules lists the reverse entries whose managed rules are kept
// ahead of a general fallback: hand-built entries plus tunnel shortcuts.
func (b *FullConfigBuilder) managedReverseRules() []*nodeDomain.ReverseProxy {
	if len(b.reversePortals) == 0 {
		return b.reverseProxies
	}
	out := append([]*nodeDomain.ReverseProxy{}, b.reverseProxies...)
	for _, p := range b.reversePortals {
		if p.RuleID != nil {
			out = append(out, &nodeDomain.ReverseProxy{Type: "portal", Tag: p.Tag, Rule2ID: p.RuleID})
		}
	}
	return out
}
