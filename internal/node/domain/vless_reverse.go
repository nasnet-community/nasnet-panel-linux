package domain

import (
	"fmt"
	"strings"
)

// ValidateVLESSReverse prevents legacy or ambiguous tunnel definitions from
// producing a config that the current core cannot run.
func ValidateVLESSReverse(inbounds []*Inbound, outbounds []*Outbound, proxies []*ReverseProxy) error {
	ins := make(map[string]*Inbound)
	outs := make(map[string]*Outbound)
	for _, in := range inbounds {
		ins[in.Tag] = in
	}
	for _, out := range outbounds {
		outs[out.Tag] = out
	}
	inOwners, outOwners, tags := map[string]string{}, map[string]string{}, map[string]bool{}
	for _, rp := range proxies {
		if rp.Tag == "" || IsGeneratedXrayTag(rp.Tag) || ins[rp.Tag] != nil || outs[rp.Tag] != nil || tags[rp.Tag] {
			return fmt.Errorf("reverse proxy tag %q is empty, reserved, or already used", rp.Tag)
		}
		tags[rp.Tag] = true
		switch rp.Type {
		case "bridge":
			if IsRouterXrayTag(rp.OutboundTag) {
				return fmt.Errorf("reverse bridge %q: managed router outbound %q cannot configure reverse permissions; use an editable outbound with the intended WAN socket mark and Allow Final Rule", rp.Tag, rp.OutboundTag)
			}
			out := outs[rp.InterconnectionTag]
			if out == nil || out.IsDisabled || out.Protocol != "vless" {
				return fmt.Errorf("reverse bridge %q requires an enabled VLESS interconnection outbound; legacy reverse is no longer supported", rp.Tag)
			}
			if owner := outOwners[out.Tag]; owner != "" {
				return fmt.Errorf("VLESS outbound %q already belongs to reverse bridge %q", out.Tag, owner)
			}
			outOwners[out.Tag] = rp.Tag
			target := outs[rp.OutboundTag]
			if (target == nil && rp.OutboundTag != "direct" && rp.OutboundTag != "blocked") || (target != nil && target.IsDisabled) {
				return fmt.Errorf("reverse bridge %q requires an enabled traffic outbound", rp.Tag)
			}
			// Current Xray blocks all VLESS Reverse traffic at Freedom unless
			// the operator explicitly permits its destination in finalRules.
			// Do not silently weaken that default while migrating legacy entries.
			if (target == nil && rp.OutboundTag == "direct") || (target != nil && target.Protocol == "freedom" && !hasReverseAllowRule(target)) {
				return fmt.Errorf("reverse bridge %q: Freedom outbound %q needs an explicit Allow Final Rule for the intended destination", rp.Tag, rp.OutboundTag)
			}
		case "portal":
			if len(rp.InterconnectionTags) == 0 || len(rp.InboundTags) == 0 {
				return fmt.Errorf("reverse portal %q requires interconnection and traffic inbounds", rp.Tag)
			}
			for _, tag := range rp.InterconnectionTags {
				in := ins[tag]
				if in == nil || in.IsDisabled || in.Protocol != "vless" {
					return fmt.Errorf("reverse portal %q requires enabled VLESS interconnection inbounds; legacy reverse is no longer supported", rp.Tag)
				}
				if owner := inOwners[tag]; owner != "" && owner != rp.Tag {
					return fmt.Errorf("VLESS inbound %q already belongs to reverse portal %q", tag, owner)
				}
				inOwners[tag] = rp.Tag
			}
			for _, tag := range rp.InboundTags {
				in := ins[tag]
				if in == nil || in.IsDisabled {
					return fmt.Errorf("reverse portal %q traffic inbound %q is missing or disabled", rp.Tag, tag)
				}
			}
		default:
			return fmt.Errorf("invalid reverse proxy type %q", rp.Type)
		}
	}
	return nil
}

func hasReverseAllowRule(out *Outbound) bool {
	for _, rule := range out.GetFreedomSettingsOrDefault().FinalRules {
		if strings.EqualFold(strings.TrimSpace(rule.Action), "allow") {
			return true
		}
	}
	return false
}
