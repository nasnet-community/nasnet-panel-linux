package xray

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// applyBandwidthRouting specializes each destination without changing which
// policy matches first. Blackholes, loopbacks and reverse portals retain their
// identity: they don't open an ordinary upstream socket to mark here.
func (b *FullConfigBuilder) applyBandwidthRouting(c *OrderedConfig) {
	var tiers []bandwidth.Tier
	for _, tier := range bandwidth.RateLimitedTiers() {
		if len(b.getEmailsForLevel(tier.Level)) > 0 {
			tiers = append(tiers, tier)
		}
	}
	if len(tiers) == 0 || len(c.Outbounds) == 0 {
		return
	}

	base := make(map[string]map[string]interface{}, len(c.Outbounds))
	var tags []string
	for _, out := range c.Outbounds {
		tag, _ := out["tag"].(string)
		base[tag] = out
		tags = append(tags, tag)
	}
	for _, rp := range b.reverseProxies {
		if rp.Type == "portal" {
			tags = append(tags, rp.Tag)
		}
	}
	// Selectors in Xray are prefixes. Generated handlers must never become
	// candidates of an original selector once it is expanded to existing tags.
	prefix := bandwidthNamespace(tags, b)
	name := func(kind, tag string, mark uint32) string {
		return fmt.Sprintf("%s%s-%d-%x", prefix, kind, mark, sha256.Sum256([]byte(tag)))
	}
	clones := make(map[string]string)
	var internalRules []map[string]interface{}
	var clone func(string, uint32) string
	clone = func(tag string, mark uint32) string {
		src := base[tag]
		if src == nil || src["protocol"] == "blackhole" || src["protocol"] == "loopback" {
			return tag
		}
		key := name("out", tag, mark)
		if existing := clones[key]; existing != "" {
			return existing
		}
		clones[key] = key
		// NASNET shares the mark word between shaping, WAN selection and
		// ingress pins. Replace only the tier bits on each physical dialer.
		socketMark := mark & netmark.MaskTier
		if stream, ok := src["streamSettings"].(map[string]interface{}); ok {
			if sock, ok := stream["sockopt"].(map[string]interface{}); ok {
				if existing, ok := sock["mark"].(uint32); ok {
					socketMark |= existing &^ netmark.MaskTier
				}
			}
		}
		out := b.cloneOutboundWithMark(src, key, socketMark)
		// Only the original VLESS handler owns the reverse control channel.
		if settings, ok := out["settings"].(map[string]interface{}); ok {
			delete(settings, "reverse")
		}
		// The last dialer in a chain owns the socket, so mark that handler too.
		if ps, ok := out["proxySettings"].(map[string]interface{}); ok {
			if next, ok := ps["tag"].(string); ok && next != "" {
				ps["tag"] = clone(next, mark)
			}
		}
		ss := out["streamSettings"].(map[string]interface{})
		sock := ss["sockopt"].(map[string]interface{})
		if next, ok := sock["dialerProxy"].(string); ok && next != "" {
			sock["dialerProxy"] = clone(next, mark)
		}
		c.Outbounds = append(c.Outbounds, out)
		return key
	}
	// Balancers need a unique selector for every candidate, including virtual
	// destinations. Leaving a portal's original prefix would also select any
	// unmarked ordinary outbounds sharing that prefix.
	cloneSelection := func(tag string, mark uint32) string {
		if target := clone(tag, mark); target != tag {
			return target
		}
		key := name("virtual", tag, mark)
		if clones[key] != "" {
			return key
		}
		clones[key] = key
		if src := base[tag]; src != nil {
			out := deepCopyMap(src)
			out["tag"] = key
			c.Outbounds = append(c.Outbounds, out)
		} else {
			c.Outbounds = append(c.Outbounds, map[string]interface{}{
				"tag": key, "protocol": "loopback",
				"settings": map[string]interface{}{"inboundTag": key},
			})
			internalRules = append(internalRules, map[string]interface{}{
				"type": "field", "inboundTag": []string{key}, "outboundTag": tag,
			})
		}
		return key
	}

	balancers, _ := c.Routing["balancers"].([]map[string]interface{})
	byBalancer := make(map[string]map[string]interface{})
	for _, bal := range balancers {
		selectors, _ := bal["selector"].([]string)
		resolved := matchingTags(tags, selectors)
		// An empty list selects nothing, including the generated default.
		bal["selector"] = resolved
		byBalancer[bal["tag"].(string)] = bal
	}
	balancerClones := make(map[string]string)
	cloneBalancer := func(tag string, mark uint32) string {
		src := byBalancer[tag]
		if src == nil {
			return tag
		}
		key := name("bal", tag, mark)
		if existing := balancerClones[key]; existing != "" {
			return existing
		}
		bal := deepCopyMap(src)
		bal["tag"] = key
		var selectors []string
		for _, target := range src["selector"].([]string) {
			selectors = append(selectors, cloneSelection(target, mark))
		}
		bal["selector"] = selectors
		if fallback, ok := bal["fallbackTag"].(string); ok {
			bal["fallbackTag"] = clone(fallback, mark)
		}
		balancers = append(balancers, bal)
		balancerClones[key] = key
		if c.Observatory != nil {
			observed, _ := c.Observatory["subjectSelector"].([]string)
			c.Observatory["subjectSelector"] = append(observed, selectors...)
		}
		return key
	}
	if c.Observatory != nil {
		selectors, _ := c.Observatory["subjectSelector"].([]string)
		c.Observatory["subjectSelector"] = matchingTags(tags, selectors)
	}

	var rules []map[string]interface{}
	for _, rule := range c.Routing["rules"].([]map[string]interface{}) {
		for _, tier := range tiers {
			emails := matchingTierEmails(b.getEmailsForLevel(tier.Level), rule)
			if len(emails) == 0 {
				continue
			}
			variant := deepCopyMap(rule)
			changed := false
			if target, ok := rule["outboundTag"].(string); ok {
				variant["outboundTag"] = clone(target, tier.Mark)
				changed = variant["outboundTag"] != target
			} else if target, ok := rule["balancerTag"].(string); ok {
				variant["balancerTag"] = cloneBalancer(target, tier.Mark)
				changed = variant["balancerTag"] != target
			}
			if changed {
				variant["user"] = emails
				rules = append(rules, variant)
			}
		}
		rules = append(rules, rule)
	}

	// A user-only fallback RULE would suppress IPIfNonMatch's DNS retry. Use
	// the implicit default OUTBOUND to re-dispatch only after routing misses.
	// Loopback preserves the user; the private inbound tag selects the original
	// first outbound (or its marked equivalent) immediately on the second pass.
	defaultTag := c.Outbounds[0]["tag"].(string)
	internalTag := prefix + "fallback"
	var fallbackRules []map[string]interface{}
	for _, tier := range tiers {
		if target := clone(defaultTag, tier.Mark); target != defaultTag {
			fallbackRules = append(fallbackRules, map[string]interface{}{
				"type": "field", "inboundTag": []string{internalTag},
				"user": b.getEmailsForLevel(tier.Level), "outboundTag": target,
			})
		}
	}
	if len(fallbackRules) > 0 {
		fallbackRules = append(fallbackRules, map[string]interface{}{
			"type": "field", "inboundTag": []string{internalTag}, "outboundTag": defaultTag,
		})
		rules = append(fallbackRules, rules...)
		c.Outbounds = append([]map[string]interface{}{{
			"tag": internalTag, "protocol": "loopback",
			"settings": map[string]interface{}{"inboundTag": internalTag},
		}}, c.Outbounds...)
	}
	c.Routing["rules"] = append(internalRules, rules...)
	if len(balancers) > 0 {
		c.Routing["balancers"] = balancers
	}
}

func matchingTags(tags, selectors []string) []string {
	result := []string{}
	for _, tag := range tags {
		for _, selector := range selectors {
			if strings.HasPrefix(tag, selector) {
				result = append(result, tag)
				break
			}
		}
	}
	return result
}

func matchingTierEmails(emails []string, rule map[string]interface{}) []string {
	users, _ := rule["user"].([]string)
	if len(users) == 0 {
		return emails
	}
	var matched []string
	for _, email := range emails {
		for _, user := range users {
			matches := user == email
			if len(user) > 7 && strings.HasPrefix(user, "regexp:") {
				matches, _ = regexp.MatchString(strings.TrimPrefix(user, "regexp:"), email)
			}
			if matches {
				matched = append(matched, email)
				break
			}
		}
	}
	return matched
}

func bandwidthNamespace(outboundTags []string, b *FullConfigBuilder) string {
	tags := append([]string{}, outboundTags...)
	for _, in := range b.inbounds {
		tags = append(tags, in.Tag)
	}
	for _, rp := range b.reverseProxies {
		tags = append(tags, rp.Tag)
	}
	for _, bal := range b.balancing {
		tags = append(tags, bal.Tag)
	}
	for _, rule := range b.routing {
		tags = append(tags, rule.InboundTags...)
		tags = append(tags, rule.OutboundTag, rule.BalancingTag)
	}
	prefix := "__panel_bw/"
	for candidate := rune(0xe000); ; candidate++ {
		conflict := false
		for _, tag := range tags {
			if tag != "" && (strings.HasPrefix(prefix, tag) || strings.HasPrefix(tag, prefix)) {
				conflict = true
				break
			}
		}
		if !conflict {
			return prefix
		}
		prefix = string(candidate) + "panel_bw/"
	}
}
