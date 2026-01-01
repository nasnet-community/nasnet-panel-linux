package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

// === Routing Rule Handlers ===

// HandleRoutingRules displays the list of routing rules for a node
func (h *Handler) HandleRoutingRules(c telebot.Context) error {
	utils.AnswerCallback(c, "📋 Loading...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	rules, err := h.nodeUsecase.ListRoutingRules(ctx, nodeID)
	if err != nil {
		return c.Edit("❌ Failed to load routing rules")
	}

	node, _ := h.nodeUsecase.GetNode(ctx, nodeID)
	nodeName := "Unknown"
	if node != nil {
		nodeName = node.Name
	}

	var msg string
	if len(rules) == 0 {
		msg = fmt.Sprintf("📋 *Routing Rules for %s*\n━━━━━━━━━━━━━━━━\n_No rules configured_", nodeName)
	} else {
		msg = fmt.Sprintf("📋 *Routing Rules for %s* `(%d)`\n━━━━━━━━━━━━━━━━\n", nodeName, len(rules))
		for _, rule := range rules {
			status := "✅"
			if !rule.Enabled {
				status = "⏸"
			}

			target := rule.OutboundTag
			targetType := "🔀"
			if target == "" {
				target = rule.BalancingTag
				targetType = "⚖️"
			}

			// Build mini summary
			matchers := []string{}
			if d := rule.DomainRules; len(d) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 🌐", len(d)))
			}
			if g := rule.GeoIPRules; len(g) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 🗺", len(g)))
			}
			if p := rule.PortRules; len(p) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 🚪", len(p)))
			}
			if ip := rule.IPCIDRRules; len(ip) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 🔢", len(ip)))
			}
			if n := rule.NetworkRules; len(n) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 📡", len(n)))
			}
			if pr := rule.ProtocolRules; len(pr) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 📝", len(pr)))
			}
			if ib := rule.InboundTags; len(ib) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 🔌", len(ib)))
			}
			if u := rule.UserEmails; len(u) > 0 {
				matchers = append(matchers, fmt.Sprintf("%d 👤", len(u)))
			}

			matcherSummary := "(no matchers)"
			if len(matchers) > 0 {
				matcherSummary = strings.Join(matchers, " ")
			}

			msg += fmt.Sprintf("%s `%s` (⚡%d)\n   %s *%s* │ %s\n",
				status, rule.RuleTag, rule.Priority,
				targetType, target, matcherSummary)

			if rule.Remark != "" {
				msg += fmt.Sprintf("   📝 _%s_\n", rule.Remark)
			}
			msg += "\n"
		}
	}

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	// Add rule buttons
	for _, rule := range rules {
		rows = append(rows, kb.Row(
			kb.Data(fmt.Sprintf("📋 %s", rule.RuleTag), "admin_rule_view", fmt.Sprintf("%d", rule.ID)),
		))
	}

	rows = append(rows,
		kb.Row(
			kb.Data("➕ Add Rule", "admin_rule_add", fmt.Sprintf("%d", nodeID)),
			kb.Data("🔄 Sync Rules", "admin_rule_sync", fmt.Sprintf("%d", nodeID)),
		),
		kb.Row(kb.Data("🔙 Back to Node", "admin_node_view", fmt.Sprintf("%d", nodeID))),
	)

	kb.Inline(rows...)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleRoutingRuleView displays details of a single routing rule
func (h *Handler) HandleRoutingRuleView(c telebot.Context) error {
	utils.AnswerCallback(c, "📋 Loading...")
	ruleID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, ruleID)
	if err != nil {
		return c.Edit("❌ Routing rule not found")
	}

	// Matchers
	domains := rule.DomainRules
	geoip := rule.GeoIPRules
	ipcidr := rule.IPCIDRRules
	ports := rule.PortRules
	networks := rule.NetworkRules
	protocols := rule.ProtocolRules
	inbounds := rule.InboundTags
	users := rule.UserEmails

	var sb strings.Builder
	sb.WriteString("📜 *Routing Rule Details*\n━━━━━━━━━━━━━━━━━━━━━━\n")

	// Basic Info
	sb.WriteString("📋 *Basic Info*\n")
	sb.WriteString(fmt.Sprintf("┣ 🏷 Tag: `%s`\n", rule.RuleTag))
	sb.WriteString(fmt.Sprintf("┣ 📝 Remark: %s\n", rule.Remark))
	sb.WriteString(fmt.Sprintf("┣ ⚡ Priority: %d\n", rule.Priority))
	if rule.Enabled {
		sb.WriteString("┗ ✅ Status: Enabled\n")
	} else {
		sb.WriteString("┗ ⏸ Status: Disabled\n")
	}

	// Target
	sb.WriteString("\n🎯 *Target*\n")
	if rule.OutboundTag != "" {
		sb.WriteString(fmt.Sprintf("┗ 🔀 Outbound: `%s`\n", rule.OutboundTag))
	} else {
		sb.WriteString(fmt.Sprintf("┗ ⚖️ Balancer: `%s`\n", rule.BalancingTag))
	}

	// Matchers Section
	sb.WriteString("\n📑 *Matchers*\n")
	hasMatchers := false

	if len(domains) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 🌐 Domains: `%d patterns`\n", len(domains)))
		// Preview first 3 domains
		limit := 3
		if len(domains) < 3 {
			limit = len(domains)
		}
		for i := 0; i < limit; i++ {
			sb.WriteString(fmt.Sprintf("┃  • %s\n", domains[i].Value))
		}
		if len(domains) > limit {
			sb.WriteString("┃  • ...\n")
		}
	}

	if len(geoip) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 🗺 GeoIP: `%s`\n", strings.Join(geoip, ", ")))
	}
	if len(ipcidr) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 🔢 IPs: `%d CIDRs`\n", len(ipcidr)))
	}
	if len(ports) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 🚪 Ports: `%s`\n", strings.Join(ports, ", ")))
	}
	if len(networks) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 📡 Networks: `%s`\n", strings.Join(networks, ", ")))
	}
	if len(protocols) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 📝 Protocols: `%s`\n", strings.Join(protocols, ", ")))
	}
	if len(inbounds) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 🔌 Inbounds: `%s`\n", strings.Join(inbounds, ", ")))
	}
	if len(users) > 0 {
		hasMatchers = true
		sb.WriteString(fmt.Sprintf("┣ 👤 Users: `%s`\n", strings.Join(users, ", ")))
	}

	if !hasMatchers {
		sb.WriteString("┗ (No matchers configured)\n")
	} else {
		sb.WriteString("┗ ─\n")
	}

	msg := sb.String()

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("✏️ Edit", "admin_rule_edit", c.Data()),
			kb.Data("🗑 Delete", "admin_rule_delete", c.Data()),
		),
		keyboards.BackRowID(kb, "admin_node_routing", rule.NodeID),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleDeleteRoutingRule deletes a routing rule
func (h *Handler) HandleDeleteRoutingRule(c telebot.Context) error {
	utils.AnswerCallback(c, "🗑 Deleting...")
	ruleID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, ruleID)
	if err != nil {
		return c.Edit("❌ Routing rule not found")
	}
	nodeID := rule.NodeID

	if err := h.nodeUsecase.DeleteRoutingRule(ctx, ruleID); err != nil {
		return c.Edit("❌ Failed to delete: " + err.Error())
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rules", "admin_node_routing", fmt.Sprintf("%d", nodeID))))
	return c.Edit("✅ Routing rule deleted successfully", kb)
}

// HandleSyncRoutingRules syncs all routing rules to Xray
func (h *Handler) HandleSyncRoutingRules(c telebot.Context) error {
	utils.AnswerCallback(c, "🔄 Syncing...")
	nodeID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	result, err := h.nodeUsecase.SyncRoutingRules(ctx, nodeID)
	if err != nil {
		return c.Edit("❌ Sync failed: " + err.Error())
	}

	msg := fmt.Sprintf("✅ *Routing Rules Synced*\n\n📤 Synced: %d\n❌ Errors: %d", result.Restored, result.Errors)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rules", "admin_node_routing", fmt.Sprintf("%d", nodeID))))
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleAddRuleStart starts the add rule wizard
func (h *Handler) HandleAddRuleStart(c telebot.Context) error {
	utils.AnswerCallback(c, "➕ Adding...")
	nodeID := utils.CallbackID(c)
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateAddRuleTag)
	h.stateManager.SetData(userID, "node_id", nodeID)

	return c.Edit("📋 *Add Routing Rule*\n\nStep 1/5: Enter a unique *rule tag* (e.g., `block-ads`, `direct-cn`):", telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleAddRuleTag handles rule tag input
func (h *Handler) HandleAddRuleTag(c telebot.Context) error {
	userID := c.Sender().ID
	tag := strings.TrimSpace(c.Text())

	if len(tag) < 2 {
		return c.Send("❌ Tag too short. Enter at least 2 characters.")
	}

	h.stateManager.SetData(userID, "rule_tag", tag)
	h.stateManager.SetState(userID, conversation.StateAddRuleTarget)

	// Get outbounds for this node to show as options
	nodeID := uint(h.stateManager.GetIntData(userID, "node_id"))
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	outbounds, _ := h.nodeUsecase.ListOutbounds(ctx, nodeID)

	msg := "Step 2/5: Enter the *target outbound tag*\n\nAvailable outbounds:"
	for _, o := range outbounds {
		msg += fmt.Sprintf("\n  • `%s` (%s)", o.Tag, o.Protocol)
	}
	msg += "\n\nType the outbound tag:"

	return c.Send(msg, telebot.ModeMarkdown)
}

// HandleAddRuleTarget handles target outbound input
func (h *Handler) HandleAddRuleTarget(c telebot.Context) error {
	userID := c.Sender().ID
	target := strings.TrimSpace(c.Text())

	h.stateManager.SetData(userID, "outbound_tag", target)
	h.stateManager.SetState(userID, conversation.StateAddRuleDomains)

	return c.Send("Step 3/5: Enter *domain patterns* to match (one per line, or comma-separated)\n\nExamples:\n  • `google.com` (domain and subdomains)\n  • `geosite:category-ads-all` (ad domains)\n  • `geosite:cn` (China domains)\n\nType `-` to skip:", telebot.ModeMarkdown)
}

// HandleAddRuleDomains handles domain patterns input
func (h *Handler) HandleAddRuleDomains(c telebot.Context) error {
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Text())

	var domains []domain.DomainMatcher
	if input != "-" && input != "" {
		// Parse domains (comma or newline separated)
		parts := strings.FieldsFunc(input, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			// Detect type based on prefix
			dtype := domain.DomainTypeDomain
			if strings.HasPrefix(p, "regexp:") || strings.HasPrefix(p, "regex:") {
				dtype = domain.DomainTypeRegex
				p = strings.TrimPrefix(strings.TrimPrefix(p, "regexp:"), "regex:")
			} else if strings.HasPrefix(p, "full:") {
				dtype = domain.DomainTypeFull
				p = strings.TrimPrefix(p, "full:")
			} else if strings.HasPrefix(p, "keyword:") || strings.HasPrefix(p, "plain:") {
				dtype = domain.DomainTypePlain
				p = strings.TrimPrefix(strings.TrimPrefix(p, "keyword:"), "plain:")
			}
			domains = append(domains, domain.DomainMatcher{Type: dtype, Value: p})
		}
	}

	h.stateManager.SetData(userID, "domains", domains)
	h.stateManager.SetState(userID, conversation.StateAddRuleGeoIP)

	return c.Send("Step 4/5: Enter *GeoIP country codes* (comma-separated)\n\nExamples: `cn,ir,ru`\n\nType `-` to skip:", telebot.ModeMarkdown)
}

// HandleAddRuleGeoIP handles GeoIP input
func (h *Handler) HandleAddRuleGeoIP(c telebot.Context) error {
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Text())

	var geoip []string
	if input != "-" && input != "" {
		parts := strings.Split(input, ",")
		for _, p := range parts {
			p = strings.TrimSpace(strings.ToUpper(p))
			if p != "" {
				geoip = append(geoip, p)
			}
		}
	}

	h.stateManager.SetData(userID, "geoip", geoip)
	h.stateManager.SetState(userID, conversation.StateAddRuleRemark)

	return c.Send("Step 5/5: Enter a *remark/description* for this rule (or `-` to skip):", telebot.ModeMarkdown)
}

// HandleAddRuleRemark handles remark input and creates the rule
func (h *Handler) HandleAddRuleRemark(c telebot.Context) error {
	userID := c.Sender().ID
	remark := strings.TrimSpace(c.Text())
	if remark == "-" {
		remark = ""
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Get stored data
	nodeID := uint(h.stateManager.GetIntData(userID, "node_id"))
	ruleTag := h.stateManager.GetStringData(userID, "rule_tag")
	outboundTag := h.stateManager.GetStringData(userID, "outbound_tag")
	domainsRaw, _ := h.stateManager.GetData(userID, "domains")
	geoipRaw, _ := h.stateManager.GetData(userID, "geoip")

	h.stateManager.ResetSession(userID)

	// Create the rule
	rule := &domain.RoutingRule{
		NodeID:      nodeID,
		RuleTag:     ruleTag,
		OutboundTag: outboundTag,
		Remark:      remark,
		Enabled:     true,
		Priority:    0,
	}

	// Set domains
	if domains, ok := domainsRaw.([]domain.DomainMatcher); ok && len(domains) > 0 {
		rule.DomainRules = domains
	}

	// Set GeoIP
	if geoip, ok := geoipRaw.([]string); ok && len(geoip) > 0 {
		rule.GeoIPRules = geoip
	}

	if err := h.nodeUsecase.AddRoutingRule(ctx, rule); err != nil {
		return c.Send("❌ Failed to create rule: " + err.Error())
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rules", "admin_node_routing", fmt.Sprintf("%d", nodeID))))

	return c.Send("✅ Routing rule created successfully!", kb)
}

// HandleRuleEdit shows enhanced edit menu for a rule
func (h *Handler) HandleRuleEdit(c telebot.Context) error {
	utils.AnswerCallback(c, "✏️")
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	// Get matcher counts for display
	domains := rule.DomainRules
	geoip := rule.GeoIPRules
	ips := rule.IPCIDRRules
	ports := rule.PortRules
	networks := rule.NetworkRules
	protocols := rule.ProtocolRules
	inbounds := rule.InboundTags
	users := rule.UserEmails

	// Status toggle text
	toggleText := "⏸ Disable"
	if !rule.Enabled {
		toggleText = "✅ Enable"
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		// Basic info row
		kb.Row(
			kb.Data("📝 Remark", "edit_rule_remark", ruleID),
			kb.Data("🏷 Tag", "edit_rule_tag", ruleID),
		),
		kb.Row(
			kb.Data("🎯 Target", "edit_rule_target", ruleID),
			kb.Data("⚡ Priority", "edit_rule_priority", ruleID),
		),
		// Toggle and status
		kb.Row(
			kb.Data(toggleText, "edit_rule_toggle", ruleID),
		),
		// Matchers section
		kb.Row(
			kb.Data(fmt.Sprintf("🌐 Domains (%d)", len(domains)), "edit_rule_domains", ruleID),
			kb.Data(fmt.Sprintf("🗺 GeoIP (%d)", len(geoip)), "edit_rule_geoip", ruleID),
		),
		kb.Row(
			kb.Data(fmt.Sprintf("🔢 IPs (%d)", len(ips)), "edit_rule_ips", ruleID),
			kb.Data(fmt.Sprintf("🚪 Ports (%d)", len(ports)), "edit_rule_ports", ruleID),
		),
		kb.Row(
			kb.Data(fmt.Sprintf("📡 Networks (%d)", len(networks)), "edit_rule_networks", ruleID),
			kb.Data(fmt.Sprintf("📝 Protocols (%d)", len(protocols)), "edit_rule_protocol", ruleID),
		),
		kb.Row(
			kb.Data(fmt.Sprintf("🔌 Inbounds (%d)", len(inbounds)), "edit_rule_inbounds", ruleID),
			kb.Data(fmt.Sprintf("👤 Users (%d)", len(users)), "edit_rule_users", ruleID),
		),
		// Navigation
		kb.Row(
			kb.Data("🗑 Delete", "admin_rule_delete", ruleID),
			kb.Data("🔙 Back", "admin_rule_view", ruleID),
		),
	)

	status := "✅ Enabled"
	if !rule.Enabled {
		status = "⏸ Disabled"
	}

	msg := fmt.Sprintf("✏️ *Edit: %s*\n━━━━━━━━━━━━━━━━━━\n%s │ Priority: %d\n\nSelect what to edit:",
		rule.RuleTag, status, rule.Priority)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleToggleRule toggles rule enabled/disabled status
func (h *Handler) HandleToggleRule(c telebot.Context) error {
	utils.AnswerCallback(c, "🔄")
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	// Toggle status
	rule.Enabled = !rule.Enabled
	if err := h.nodeUsecase.UpdateRoutingRule(ctx, rule); err != nil {
		return c.Edit("❌ Failed to update: " + err.Error())
	}

	status := "enabled"
	if !rule.Enabled {
		status = "disabled"
	}
	utils.AnswerCallback(c, fmt.Sprintf("Rule %s!", status))

	// Refresh edit menu
	c.Callback().Data = ruleID
	return h.HandleRuleEdit(c)
}

// HandleEditRulePriority shows priority edit options
func (h *Handler) HandleEditRulePriority(c telebot.Context) error {
	utils.AnswerCallback(c, "⚡")
	ruleID := c.Data()

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("🔺 High (0)", "edit_rule_priority_set", ruleID, "0"),
			kb.Data("▪️ Normal (50)", "edit_rule_priority_set", ruleID, "50"),
		),
		kb.Row(
			kb.Data("🔻 Low (100)", "edit_rule_priority_set", ruleID, "100"),
			kb.Data("✏️ Custom", "edit_rule_priority_custom", ruleID),
		),
		keyboards.BackRow(kb, "admin_rule_edit", ruleID),
	)

	return c.Edit("⚡ *Edit Priority*\n\nLower number = higher priority (processed first)\n\nSelect preset or enter custom value:", telebot.ModeMarkdown, kb)
}

// HandleEditRulePrioritySet sets priority to a preset value
func (h *Handler) HandleEditRulePrioritySet(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	ruleID := data[0]
	priority, _ := strconv.Atoi(data[1])

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	rule.Priority = priority
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	utils.AnswerCallback(c, "Priority updated!")
	c.Callback().Data = ruleID
	return h.HandleRuleEdit(c)
}

// HandleEditRuleGeoIP starts editing GeoIP rules
func (h *Handler) HandleEditRuleGeoIP(c telebot.Context) error {
	utils.AnswerCallback(c, "🗺")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditRuleGeoIP)
	h.stateManager.SetData(userID, "rule_id", ruleID)

	return c.Edit("🗺 *Edit GeoIP Rules*\n\nEnter country codes (comma-separated)\n\nExamples: `cn,ir,ru,private`\n\nType `-` to clear all GeoIP rules:", telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleEditRuleGeoIPInput processes GeoIP edit
func (h *Handler) HandleEditRuleGeoIPInput(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	idStr := h.stateManager.GetStringData(userID, "rule_id")
	ruleID, _ := strconv.ParseUint(idStr, 10, 32)

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(ruleID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Rule not found")
	}

	input := strings.TrimSpace(c.Text())
	var geoip []string
	if input != "-" && input != "" {
		parts := strings.Split(input, ",")
		for _, p := range parts {
			p = strings.TrimSpace(strings.ToUpper(p))
			if p != "" {
				geoip = append(geoip, p)
			}
		}
	}

	rule.GeoIPRules = geoip
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rule", "admin_rule_edit", idStr)))

	return c.Send(fmt.Sprintf("✅ GeoIP updated! (%d countries)", len(geoip)), kb)
}

// HandleEditRulePorts starts editing port rules
func (h *Handler) HandleEditRulePorts(c telebot.Context) error {
	utils.AnswerCallback(c, "🚪")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditRulePorts)
	h.stateManager.SetData(userID, "rule_id", ruleID)

	return c.Edit("🚪 *Edit Port Rules*\n\nEnter ports (comma-separated, supports ranges)\n\nExamples: `80,443,1000-2000`\n\nType `-` to clear all port rules:", telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleEditRulePortsInput processes port edit
func (h *Handler) HandleEditRulePortsInput(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	idStr := h.stateManager.GetStringData(userID, "rule_id")
	ruleID, _ := strconv.ParseUint(idStr, 10, 32)

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(ruleID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Rule not found")
	}

	input := strings.TrimSpace(c.Text())
	var ports []string
	if input != "-" && input != "" {
		parts := strings.Split(input, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				ports = append(ports, p)
			}
		}
	}

	rule.PortRules = ports
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rule", "admin_rule_edit", idStr)))

	return c.Send(fmt.Sprintf("✅ Ports updated! (%d entries)", len(ports)), kb)
}

// HandleEditRuleNetworks shows network selection
func (h *Handler) HandleEditRuleNetworks(c telebot.Context) error {
	utils.AnswerCallback(c, "📡")
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, _ := h.nodeUsecase.GetRoutingRule(ctx, uint(id))

	networks := rule.NetworkRules
	hasTCP := contains(networks, "tcp")
	hasUDP := contains(networks, "udp")

	tcpBtn := "☐ TCP"
	if hasTCP {
		tcpBtn = "☑ TCP"
	}
	udpBtn := "☐ UDP"
	if hasUDP {
		udpBtn = "☑ UDP"
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(tcpBtn, "edit_rule_net_toggle", ruleID, "tcp"),
			kb.Data(udpBtn, "edit_rule_net_toggle", ruleID, "udp"),
		),
		kb.Row(
			kb.Data("☑ Both", "edit_rule_net_set", ruleID, "both"),
			kb.Data("☐ Clear", "edit_rule_net_set", ruleID, "none"),
		),
		keyboards.BackRow(kb, "admin_rule_edit", ruleID),
	)

	return c.Edit("📡 *Edit Networks*\n\nSelect which networks this rule matches:", telebot.ModeMarkdown, kb)
}

// HandleEditRuleNetworkToggle toggles a network
func (h *Handler) HandleEditRuleNetworkToggle(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	ruleID := data[0]
	network := data[1]

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	networks := rule.NetworkRules
	if contains(networks, network) {
		networks = removeElement(networks, network)
	} else {
		networks = append(networks, network)
	}

	rule.NetworkRules = networks
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	c.Callback().Data = ruleID
	return h.HandleEditRuleNetworks(c)
}

// HandleEditRuleNetworkSet sets networks to preset
func (h *Handler) HandleEditRuleNetworkSet(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	ruleID := data[0]
	preset := data[1]

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	var networks []string
	switch preset {
	case "both":
		networks = []string{"tcp", "udp"}
	case "none":
		networks = nil
	}

	rule.NetworkRules = networks
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	c.Callback().Data = ruleID
	return h.HandleEditRuleNetworks(c)
}

// HandleEditRuleProtocol shows protocol selection
func (h *Handler) HandleEditRuleProtocol(c telebot.Context) error {
	utils.AnswerCallback(c, "📝")
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, _ := h.nodeUsecase.GetRoutingRule(ctx, uint(id))

	protocols := rule.ProtocolRules
	hasHTTP := contains(protocols, "http")
	hasTLS := contains(protocols, "tls")
	hasBT := contains(protocols, "bittorrent")
	hasQUIC := contains(protocols, "quic")

	httpBtn := "☐ HTTP"
	if hasHTTP {
		httpBtn = "☑ HTTP"
	}
	tlsBtn := "☐ TLS"
	if hasTLS {
		tlsBtn = "☑ TLS"
	}
	btBtn := "☐ BitTorrent"
	if hasBT {
		btBtn = "☑ BitTorrent"
	}
	quicBtn := "☐ QUIC"
	if hasQUIC {
		quicBtn = "☑ QUIC"
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(httpBtn, "edit_rule_proto_toggle", ruleID, "http"),
			kb.Data(tlsBtn, "edit_rule_proto_toggle", ruleID, "tls"),
		),
		kb.Row(
			kb.Data(btBtn, "edit_rule_proto_toggle", ruleID, "bittorrent"),
			kb.Data(quicBtn, "edit_rule_proto_toggle", ruleID, "quic"),
		),
		kb.Row(
			kb.Data("☐ Clear All", "edit_rule_proto_clear", ruleID),
		),
		keyboards.BackRow(kb, "admin_rule_edit", ruleID),
	)

	return c.Edit("📝 *Edit Protocols*\n\nSelect sniffed protocols to match:", telebot.ModeMarkdown, kb)
}

// HandleEditRuleProtocolToggle toggles a protocol
func (h *Handler) HandleEditRuleProtocolToggle(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	ruleID := data[0]
	protocol := data[1]

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	protocols := rule.ProtocolRules
	if contains(protocols, protocol) {
		protocols = removeElement(protocols, protocol)
	} else {
		protocols = append(protocols, protocol)
	}

	rule.ProtocolRules = protocols
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	c.Callback().Data = ruleID
	return h.HandleEditRuleProtocol(c)
}

// HandleEditRuleProtocolClear clears all protocols
func (h *Handler) HandleEditRuleProtocolClear(c telebot.Context) error {
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	rule.ProtocolRules = nil
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	c.Callback().Data = ruleID
	return h.HandleEditRuleProtocol(c)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}

func removeElement(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if !strings.EqualFold(s, item) {
			result = append(result, s)
		}
	}
	return result
}

// HandleEditRuleRemark starts editing rule remark
func (h *Handler) HandleEditRuleRemark(c telebot.Context) error {
	utils.AnswerCallback(c, "✏️")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditRuleRemark)
	h.stateManager.SetData(userID, "rule_id", ruleID)

	return c.Edit("Enter new remark for this rule:", keyboards.CancelInline())
}

// HandleEditRuleRemarkInput processes remark edit
func (h *Handler) HandleEditRuleRemarkInput(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	idStr := h.stateManager.GetStringData(userID, "rule_id")
	ruleID, _ := strconv.ParseUint(idStr, 10, 32)

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(ruleID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Rule not found")
	}

	rule.Remark = strings.TrimSpace(c.Text())
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rule", "admin_rule_view", idStr)))

	return c.Send("✅ Remark updated!", kb)
}

// HandleEditRuleTarget shows outbound selection for changing rule target
func (h *Handler) HandleEditRuleTarget(c telebot.Context) error {
	utils.AnswerCallback(c, "🎯")
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	// Get outbounds for this node
	outbounds, _ := h.nodeUsecase.ListOutbounds(ctx, rule.NodeID)

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, o := range outbounds {
		emoji := "🔀"
		if o.Protocol == "freedom" {
			emoji = "🌐"
		} else if o.Protocol == "blackhole" {
			emoji = "🕳"
		}
		rows = append(rows, kb.Row(
			kb.Data(fmt.Sprintf("%s %s (%s)", emoji, o.Tag, o.Protocol), "edit_rule_target_save", ruleID, o.Tag),
		))
	}

	rows = append(rows, keyboards.BackRow(kb, "admin_rule_edit", ruleID))
	kb.Inline(rows...)

	currentTarget := rule.OutboundTag
	if currentTarget == "" {
		currentTarget = rule.BalancingTag + " (balancer)"
	}

	return c.Edit(fmt.Sprintf("🎯 *Edit Target*\n\nCurrent: `%s`\n\nSelect new target outbound:", currentTarget), telebot.ModeMarkdown, kb)
}

// HandleEditRuleTargetSave saves the new target
func (h *Handler) HandleEditRuleTargetSave(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	ruleID := data[0]
	newTarget := data[1]

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	rule.OutboundTag = newTarget
	rule.BalancingTag = "" // Clear balancing if setting outbound
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	utils.AnswerCallback(c, "Target updated!")
	c.Callback().Data = ruleID
	return h.HandleRuleEdit(c)
}

// HandleEditRuleTag starts editing rule tag
func (h *Handler) HandleEditRuleTag(c telebot.Context) error {
	utils.AnswerCallback(c, "🏷")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateAddRuleTag) // Reuse add state
	h.stateManager.SetData(userID, "rule_id", ruleID)
	h.stateManager.SetData(userID, "edit_mode", true)

	return c.Edit("🏷 *Edit Rule Tag*\n\nEnter new tag for this rule:", telebot.ModeMarkdown)
}

// HandleEditRuleDomains starts editing domain rules
func (h *Handler) HandleEditRuleDomains(c telebot.Context) error {
	utils.AnswerCallback(c, "🌐")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditRuleDomains)
	h.stateManager.SetData(userID, "rule_id", ruleID)

	return c.Edit("🌐 *Edit Domain Rules*\n\nEnter domain patterns (one per line or comma-separated)\n\nExamples:\n• `google.com` - domain and subdomains\n• `geosite:category-ads-all` - ad domains\n• `geosite:cn` - China domains\n• `full:exact.domain.com` - exact match\n• `regexp:.*\\.gov$` - regex pattern\n\nType `-` to clear all domain rules:", telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleEditRuleDomainsInput processes domain edit
func (h *Handler) HandleEditRuleDomainsInput(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	idStr := h.stateManager.GetStringData(userID, "rule_id")
	ruleID, _ := strconv.ParseUint(idStr, 10, 32)

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(ruleID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Rule not found")
	}

	input := strings.TrimSpace(c.Text())
	var domains []domain.DomainMatcher

	if input != "-" && input != "" {
		// Parse domains (comma or newline separated)
		parts := strings.FieldsFunc(input, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			// Detect type based on prefix
			dtype := domain.DomainTypeDomain
			if strings.HasPrefix(p, "regexp:") || strings.HasPrefix(p, "regex:") {
				dtype = domain.DomainTypeRegex
				p = strings.TrimPrefix(strings.TrimPrefix(p, "regexp:"), "regex:")
			} else if strings.HasPrefix(p, "full:") {
				dtype = domain.DomainTypeFull
				p = strings.TrimPrefix(p, "full:")
			} else if strings.HasPrefix(p, "keyword:") || strings.HasPrefix(p, "plain:") {
				dtype = domain.DomainTypePlain
				p = strings.TrimPrefix(strings.TrimPrefix(p, "keyword:"), "plain:")
			}
			domains = append(domains, domain.DomainMatcher{Type: dtype, Value: p})
		}
	}

	rule.DomainRules = domains
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rule", "admin_rule_edit", idStr)))

	return c.Send(fmt.Sprintf("✅ Domains updated! (%d patterns)", len(domains)), kb)
}

// HandleEditRuleIPs starts editing IP CIDR rules
func (h *Handler) HandleEditRuleIPs(c telebot.Context) error {
	utils.AnswerCallback(c, "🔢")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditRuleIPs)
	h.stateManager.SetData(userID, "rule_id", ruleID)

	return c.Edit("🔢 *Edit IP/CIDR Rules*\n\nEnter IP addresses or CIDR blocks (comma-separated)\n\nExamples: `10.0.0.0/8,192.168.0.0/16,1.2.3.4`\n\nType `-` to clear all IP rules:", telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleEditRuleIPsInput processes IP edit
func (h *Handler) HandleEditRuleIPsInput(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	idStr := h.stateManager.GetStringData(userID, "rule_id")
	ruleID, _ := strconv.ParseUint(idStr, 10, 32)

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(ruleID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Rule not found")
	}

	input := strings.TrimSpace(c.Text())
	var ips []string
	if input != "-" && input != "" {
		parts := strings.Split(input, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				ips = append(ips, p)
			}
		}
	}

	rule.IPCIDRRules = ips
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rule", "admin_rule_edit", idStr)))

	return c.Send(fmt.Sprintf("✅ IPs updated! (%d entries)", len(ips)), kb)
}

// HandleEditRuleInbounds shows inbound tag selection for the rule
func (h *Handler) HandleEditRuleInbounds(c telebot.Context) error {
	utils.AnswerCallback(c, "🔌")
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	// Get inbounds for this node
	inbounds, _ := h.nodeUsecase.ListInbounds(ctx, rule.NodeID)
	selectedTags := rule.InboundTags

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, inb := range inbounds {
		isSelected := contains(selectedTags, inb.Tag)
		checkbox := "☐"
		if isSelected {
			checkbox = "☑"
		}
		rows = append(rows, kb.Row(
			kb.Data(fmt.Sprintf("%s %s (%s)", checkbox, inb.Tag, inb.Protocol), "edit_rule_inbound_toggle", ruleID, inb.Tag),
		))
	}

	rows = append(rows,
		kb.Row(kb.Data("☐ Clear All", "edit_rule_inbound_clear", ruleID)),
		keyboards.BackRow(kb, "admin_rule_edit", ruleID),
	)
	kb.Inline(rows...)

	currentText := "None (matches all)"
	if len(selectedTags) > 0 {
		currentText = strings.Join(selectedTags, ", ")
	}

	return c.Edit(fmt.Sprintf("🔌 *Edit Inbound Tags*\n\nCurrent: %s\n\nSelect inbounds this rule applies to:", currentText), telebot.ModeMarkdown, kb)
}

// HandleEditRuleInboundToggle toggles an inbound tag
func (h *Handler) HandleEditRuleInboundToggle(c telebot.Context) error {
	data, ok := utils.SplitCallback(c, "|", 2)
	if !ok {
		return nil
	}
	ruleID := data[0]
	inboundTag := data[1]

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	tags := rule.InboundTags
	if contains(tags, inboundTag) {
		tags = removeElement(tags, inboundTag)
	} else {
		tags = append(tags, inboundTag)
	}

	rule.InboundTags = tags
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	c.Callback().Data = ruleID
	return h.HandleEditRuleInbounds(c)
}

// HandleEditRuleInboundClear clears all inbound tags
func (h *Handler) HandleEditRuleInboundClear(c telebot.Context) error {
	ruleID := c.Data()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, _ := strconv.ParseUint(ruleID, 10, 32)
	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Rule not found")
	}

	rule.InboundTags = nil
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)

	c.Callback().Data = ruleID
	return h.HandleEditRuleInbounds(c)
}

// HandleEditRuleUsers starts editing user email rules
func (h *Handler) HandleEditRuleUsers(c telebot.Context) error {
	utils.AnswerCallback(c, "👤")
	ruleID := c.Data()
	userID := c.Sender().ID

	h.stateManager.StartConversation(userID, conversation.StateEditRuleUsers)
	h.stateManager.SetData(userID, "rule_id", ruleID)

	return c.Edit("👤 *Edit User Emails*\n\nEnter user emails (comma-separated)\n\nThis rule will only apply to traffic from these users.\n\nType `-` to clear (matches all users):", telebot.ModeMarkdown, keyboards.CancelInline())
}

// HandleEditRuleUsersInput processes user email edit
func (h *Handler) HandleEditRuleUsersInput(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	idStr := h.stateManager.GetStringData(userID, "rule_id")
	ruleID, _ := strconv.ParseUint(idStr, 10, 32)

	rule, err := h.nodeUsecase.GetRoutingRule(ctx, uint(ruleID))
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send("❌ Rule not found")
	}

	input := strings.TrimSpace(c.Text())
	var emails []string
	if input != "-" && input != "" {
		parts := strings.Split(input, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				emails = append(emails, p)
			}
		}
	}

	rule.UserEmails = emails
	h.nodeUsecase.UpdateRoutingRule(ctx, rule)
	h.stateManager.ResetSession(userID)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data("🔙 Back to Rule", "admin_rule_edit", idStr)))

	return c.Send(fmt.Sprintf("✅ Users updated! (%d emails)", len(emails)), kb)
}
