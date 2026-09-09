package domain

// OrderReverseRoutingRules keeps managed reverse traffic ahead of a general
// fallback. Specific user policies retain their relative order.
func OrderReverseRoutingRules(rules []*RoutingRule, proxies []*ReverseProxy) []*RoutingRule {
	ordered := append([]*RoutingRule{}, rules...)
	for _, rp := range proxies {
		if rp.Rule2ID == nil {
			continue
		}
		var traffic *RoutingRule
		position := len(ordered)
		for i, rule := range ordered {
			if rule == nil {
				continue
			}
			if rule.ID == *rp.Rule2ID {
				traffic = rule
				position = min(position, i)
			}
			if rule.Enabled && rule.isGeneralFallback() {
				position = min(position, i)
			}
		}
		if traffic == nil {
			continue
		}
		next := make([]*RoutingRule, 0, len(ordered))
		for i, rule := range ordered {
			if i == position {
				next = append(next, traffic)
			}
			if rule == nil || rule.ID != traffic.ID {
				next = append(next, rule)
			}
		}
		ordered = next
	}
	return ordered
}

func (r *RoutingRule) isGeneralFallback() bool {
	return len(r.DomainRules)+len(r.GeoIPRules)+len(r.IPCIDRRules)+len(r.PortRules)+
		len(r.ProtocolRules)+len(r.InboundTags)+len(r.UserEmails)+len(r.SourceIPs)+
		len(r.SourcePorts)+len(r.Attributes)+len(r.ProcessNames)+len(r.LocalIPs)+
		len(r.LocalPorts)+len(r.VlessRoutes) == 0
}
