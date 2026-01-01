package usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// ErrInvalidRoutingRule is returned by routing/balancing usecases for
// validation failures (bad input) so the HTTP layer can surface a 4xx
// instead of 500.
var ErrInvalidRoutingRule = errors.New("invalid routing rule")

type ctxKeySkipPush struct{}

// ContextWithSkipPush returns a context that tells routing CRUD methods to skip pushing config to the agent.
// Useful for batch operations where a single push is done at the end.
func ContextWithSkipPush(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeySkipPush{}, true)
}

func shouldSkipPush(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeySkipPush{}).(bool)
	return v
}

// SyncPresetRoutingRules is a no-op. Preset rule creation is now handled
// entirely by the frontend, which calls individual CRUD endpoints.
func (u *nodeUsecase) SyncPresetRoutingRules(ctx context.Context, nodeID uint, rs *domain.RoutingSettings) error {
	return nil
}

// === Routing Rule Validation ===

var validDomainTypes = map[string]bool{
	"plain": true, "regex": true, "domain": true, "full": true,
}

var validNetworks = map[string]bool{
	"tcp": true, "udp": true, "tcp,udp": true,
}

var validProtocols = map[string]bool{
	"http": true, "tls": true, "bittorrent": true, "quic": true,
}

func validateRoutingRule(rule *domain.RoutingRule) error {
	// rule_tag is required and must be reasonable
	tag := strings.TrimSpace(rule.RuleTag)
	if tag == "" {
		return fmt.Errorf("%w: rule_tag is required", ErrInvalidRoutingRule)
	}
	if len(tag) > 100 {
		return fmt.Errorf("%w: rule_tag must be at most 100 characters", ErrInvalidRoutingRule)
	}
	rule.RuleTag = tag

	// Must have a target — exactly one of outbound_tag or balancing_tag.
	if rule.OutboundTag == "" && rule.BalancingTag == "" {
		return fmt.Errorf("%w: outbound_tag or balancing_tag is required", ErrInvalidRoutingRule)
	}
	if rule.OutboundTag != "" && rule.BalancingTag != "" {
		return fmt.Errorf("%w: outbound_tag and balancing_tag are mutually exclusive", ErrInvalidRoutingRule)
	}

	// xray-core rejects rules with no effective fields (BuildCondition returns
	// "this rule has no effective fields"), failing the entire config push.
	if !ruleHasMatcher(rule) {
		return fmt.Errorf("%w: at least one matcher (domain/ip/port/network/protocol/inbound/user/source/process/local) is required", ErrInvalidRoutingRule)
	}

	// Validate domain rule types
	for i, d := range rule.DomainRules {
		if !validDomainTypes[d.Type] {
			return fmt.Errorf("%w: invalid domain rule type %q at index %d (must be plain, regex, domain, or full)", ErrInvalidRoutingRule, d.Type, i)
		}
		if strings.TrimSpace(d.Value) == "" {
			return fmt.Errorf("%w: domain rule value is empty at index %d", ErrInvalidRoutingRule, i)
		}
	}

	// Validate network rules
	for _, n := range rule.NetworkRules {
		if !validNetworks[n] {
			return fmt.Errorf("%w: invalid network %q (must be tcp, udp, or tcp,udp)", ErrInvalidRoutingRule, n)
		}
	}

	// Validate protocol rules
	for _, p := range rule.ProtocolRules {
		if !validProtocols[p] {
			return fmt.Errorf("%w: invalid protocol %q (must be http, tls, bittorrent, or quic)", ErrInvalidRoutingRule, p)
		}
	}

	// Validate port rules format
	for _, p := range rule.PortRules {
		if err := validatePortString(p); err != nil {
			return fmt.Errorf("%w: invalid port rule %q: %v", ErrInvalidRoutingRule, p, err)
		}
	}

	// Validate source port rules format
	for _, p := range rule.SourcePorts {
		if err := validatePortString(p); err != nil {
			return fmt.Errorf("%w: invalid source port %q: %v", ErrInvalidRoutingRule, p, err)
		}
	}

	for _, p := range rule.LocalPorts {
		if err := validatePortString(p); err != nil {
			return fmt.Errorf("%w: invalid local port %q: %v", ErrInvalidRoutingRule, p, err)
		}
	}

	return nil
}

// ruleHasMatcher reports whether the rule has at least one active matcher
// condition. Mirrors xray-core's app/router/config.go BuildCondition logic.
func ruleHasMatcher(rule *domain.RoutingRule) bool {
	switch {
	case len(rule.DomainRules) > 0,
		len(rule.GeoIPRules) > 0,
		len(rule.IPCIDRRules) > 0,
		len(rule.PortRules) > 0,
		len(rule.NetworkRules) > 0,
		len(rule.ProtocolRules) > 0,
		len(rule.InboundTags) > 0,
		len(rule.UserEmails) > 0,
		len(rule.SourceIPs) > 0,
		len(rule.SourcePorts) > 0,
		len(rule.Attributes) > 0,
		len(rule.ProcessNames) > 0,
		len(rule.LocalIPs) > 0,
		len(rule.LocalPorts) > 0:
		return true
	}
	return false
}

func validatePortString(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("empty port value")
	}
	if strings.Contains(p, "-") {
		parts := strings.SplitN(p, "-", 2)
		from, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 16)
		if err != nil || from == 0 {
			return fmt.Errorf("invalid start port")
		}
		to, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 16)
		if err != nil || to == 0 {
			return fmt.Errorf("invalid end port")
		}
		if from > to {
			return fmt.Errorf("start port %d is greater than end port %d", from, to)
		}
	} else {
		port, err := strconv.ParseUint(p, 10, 16)
		if err != nil || port == 0 {
			return fmt.Errorf("invalid port number")
		}
	}
	return nil
}

// === Routing Rule Management ===

func (u *nodeUsecase) AddRoutingRule(ctx context.Context, rule *domain.RoutingRule) error {
	log := logger.GetLogger()

	if err := validateRoutingRule(rule); err != nil {
		return err
	}

	// Check rule_tag uniqueness per node
	if existing, _ := u.nodeRepo.GetRoutingRuleByTagAndNode(ctx, rule.NodeID, rule.RuleTag); existing != nil {
		return fmt.Errorf("%w: rule_tag %q already exists on this node", ErrInvalidRoutingRule, rule.RuleTag)
	}

	// Create in database
	if err := u.nodeRepo.CreateRoutingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to create routing rule: %w", err)
	}

	if shouldSkipPush(ctx) || !rule.Enabled {
		return nil
	}

	// Get node for xray sync
	node, err := u.nodeRepo.GetNode(ctx, rule.NodeID)
	if err != nil {
		log.Warnf("Failed to get node %d for routing rule sync: %v", rule.NodeID, err)
		return nil // Rule saved in DB, sync can happen later
	}

	// Build xray config and push to Xray
	if err := u.pushConfigToAgent(ctx, node); err != nil {
		log.Warnf("Failed to push config to agent for routing rule: %v", err)
	}

	return nil
}

func (u *nodeUsecase) ListRoutingRules(ctx context.Context, nodeID uint) ([]*domain.RoutingRule, error) {
	return u.nodeRepo.ListRoutingRulesByNode(ctx, nodeID)
}

func (u *nodeUsecase) GetRoutingRule(ctx context.Context, id uint) (*domain.RoutingRule, error) {
	return u.nodeRepo.GetRoutingRuleWithNode(ctx, id)
}

func (u *nodeUsecase) ToggleRoutingRule(ctx context.Context, id uint) (*domain.RoutingRule, error) {
	rule, err := u.nodeRepo.GetRoutingRuleWithNode(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("routing rule not found: %w", err)
	}

	rule.Enabled = !rule.Enabled
	if err := u.nodeRepo.UpdateRoutingRule(ctx, rule); err != nil {
		return nil, fmt.Errorf("failed to toggle routing rule: %w", err)
	}

	if rule.Node != nil {
		if err := u.pushConfigToAgent(ctx, rule.Node); err != nil {
			logger.GetLogger().Warnf("Failed to push config to agent after toggle: %v", err)
		}
	}

	return rule, nil
}

func (u *nodeUsecase) DeleteRoutingRule(ctx context.Context, id uint) error {
	// Get rule with node for xray removal
	rule, err := u.nodeRepo.GetRoutingRuleWithNode(ctx, id)
	if err != nil {
		return fmt.Errorf("routing rule not found: %w", err)
	}

	// Check if this rule is managed by a reverse proxy
	if strings.HasPrefix(rule.Remark, "[reverse]") {
		return fmt.Errorf("cannot delete: this rule is managed by a reverse proxy — delete the reverse proxy instead")
	}

	// Delete from database
	if err := u.nodeRepo.DeleteRoutingRule(ctx, id); err != nil {
		return err
	}

	if shouldSkipPush(ctx) {
		return nil
	}

	// Push config now that the rule is gone from DB
	if rule.Node != nil {
		if pushErr := u.pushConfigToAgent(ctx, rule.Node); pushErr != nil {
			logger.GetLogger().WithError(pushErr).WithField("node_id", rule.Node.ID).Warn("[DeleteRoutingRule] Failed to push config to agent after rule deletion")
		}
	}

	return nil
}

func (u *nodeUsecase) UpdateRoutingRule(ctx context.Context, rule *domain.RoutingRule) error {
	if err := validateRoutingRule(rule); err != nil {
		return err
	}

	// Get existing rule with node
	existing, err := u.nodeRepo.GetRoutingRuleWithNode(ctx, rule.ID)
	if err != nil {
		return fmt.Errorf("routing rule not found: %w", err)
	}
	// Pin NodeID — JSON body cannot relocate the rule to another node.
	rule.NodeID = existing.NodeID

	if strings.HasPrefix(existing.Remark, "[reverse]") {
		return fmt.Errorf("%w: cannot edit: this rule is managed by a reverse proxy — edit from the Reverse tab instead", ErrInvalidRoutingRule)
	}

	// Check rule_tag uniqueness if tag changed
	if rule.RuleTag != existing.RuleTag {
		if dup, _ := u.nodeRepo.GetRoutingRuleByTagAndNode(ctx, existing.NodeID, rule.RuleTag); dup != nil {
			return fmt.Errorf("%w: rule_tag %q already exists on this node", ErrInvalidRoutingRule, rule.RuleTag)
		}
	}

	// Update in database
	if err := u.nodeRepo.UpdateRoutingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to update routing rule: %w", err)
	}

	if shouldSkipPush(ctx) {
		return nil
	}

	// Reload with node
	rule, err = u.nodeRepo.GetRoutingRuleWithNode(ctx, rule.ID)
	if err != nil {
		return nil // Saved to DB, sync later
	}

	if rule.Node != nil {
		if err := u.pushConfigToAgent(ctx, rule.Node); err != nil {
			logger.GetLogger().Warnf("Failed to push config to agent: %v", err)
		}
	}

	return nil
}

func (u *nodeUsecase) MoveRoutingRule(ctx context.Context, id uint, moveUp bool) error {
	rule, err := u.nodeRepo.GetRoutingRuleWithNode(ctx, id)
	if err != nil {
		return fmt.Errorf("routing rule not found: %w", err)
	}

	neighbor, err := u.nodeRepo.FindAdjacentRoutingRule(ctx, rule.NodeID, rule.Priority, rule.ID, moveUp)
	if err != nil {
		return fmt.Errorf("no adjacent rule to swap with: %w", err)
	}

	// Swap priorities; if they're equal (tied by id), offset by 1 instead
	if rule.Priority == neighbor.Priority {
		if moveUp {
			rule.Priority--
		} else {
			rule.Priority++
		}
	} else {
		rule.Priority, neighbor.Priority = neighbor.Priority, rule.Priority
	}

	if err := u.nodeRepo.UpdateRoutingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to update rule priority: %w", err)
	}
	if err := u.nodeRepo.UpdateRoutingRule(ctx, neighbor); err != nil {
		return fmt.Errorf("failed to update neighbor priority: %w", err)
	}

	// Push updated config to agent
	if rule.Node != nil {
		if err := u.pushConfigToAgent(ctx, rule.Node); err != nil {
			logger.GetLogger().Warnf("Failed to push config to agent after routing rule move: %v", err)
		}
	}

	return nil
}

func (u *nodeUsecase) ReorderRoutingRules(ctx context.Context, nodeID uint, ruleIDs []uint) error {
	// Fetch existing rules for this node
	existing, err := u.nodeRepo.ListRoutingRulesByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to list routing rules: %w", err)
	}

	// Validate: ruleIDs must be a complete, non-duplicate permutation of existing rule IDs
	if len(ruleIDs) != len(existing) {
		return fmt.Errorf("expected %d rule IDs, got %d", len(existing), len(ruleIDs))
	}
	existingSet := make(map[uint]bool, len(existing))
	for _, r := range existing {
		existingSet[r.ID] = true
	}
	seen := make(map[uint]bool, len(ruleIDs))
	for _, id := range ruleIDs {
		if !existingSet[id] {
			return fmt.Errorf("rule ID %d does not belong to node %d", id, nodeID)
		}
		if seen[id] {
			return fmt.Errorf("duplicate rule ID %d", id)
		}
		seen[id] = true
	}

	// Batch update priorities
	if err := u.nodeRepo.ReorderRoutingRules(ctx, nodeID, ruleIDs); err != nil {
		return fmt.Errorf("failed to reorder routing rules: %w", err)
	}

	// Push config once
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		logger.GetLogger().Warnf("Failed to get node %d for config push after reorder: %v", nodeID, err)
		return nil
	}
	if err := u.pushConfigToAgent(ctx, node); err != nil {
		logger.GetLogger().Warnf("Failed to push config to agent after routing rule reorder: %v", err)
	}

	return nil
}

func (u *nodeUsecase) SyncRoutingRules(ctx context.Context, nodeID uint) (*SyncResult, error) {
	result := &SyncResult{}

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	// Push the full config which includes all rules
	if err := u.pushConfigToAgent(ctx, node); err != nil {
		return nil, err
	}
	// We don't have detailed stats for push, but we assume success if no error
	return result, nil
}

// === Balancing Rule Management ===

func validateBalancingRule(rule *domain.BalancingRule) error {
	tag := strings.TrimSpace(rule.Tag)
	if tag == "" {
		return fmt.Errorf("%w: balancing rule tag is required", ErrInvalidRoutingRule)
	}
	if len(tag) > 100 {
		return fmt.Errorf("%w: balancing rule tag must be at most 100 characters", ErrInvalidRoutingRule)
	}
	rule.Tag = tag

	if len(rule.OutboundSelectors) == 0 {
		return fmt.Errorf("%w: at least one outbound selector is required", ErrInvalidRoutingRule)
	}
	if rule.Strategy == "" {
		rule.Strategy = "random"
	}
	if !domain.ValidBalancingStrategies[rule.Strategy] {
		return fmt.Errorf("%w: invalid balancing strategy %q", ErrInvalidRoutingRule, rule.Strategy)
	}
	if rule.FallbackTag != "" && len(rule.FallbackTag) > 100 {
		return fmt.Errorf("%w: fallback_tag must be at most 100 characters", ErrInvalidRoutingRule)
	}
	return nil
}

// balancingTagExistsOnNode reports whether another rule on this node already
// uses tag. Pass excludeID > 0 to skip the rule being updated.
func (u *nodeUsecase) balancingTagExistsOnNode(ctx context.Context, nodeID uint, tag string, excludeID uint) (bool, error) {
	existing, err := u.nodeRepo.ListBalancingRulesByNode(ctx, nodeID)
	if err != nil {
		return false, err
	}
	for _, r := range existing {
		if r == nil {
			continue
		}
		if r.ID == excludeID {
			continue
		}
		if r.Tag == tag {
			return true, nil
		}
	}
	return false, nil
}

func (u *nodeUsecase) AddBalancingRule(ctx context.Context, rule *domain.BalancingRule) error {
	if err := validateBalancingRule(rule); err != nil {
		return err
	}

	if exists, err := u.balancingTagExistsOnNode(ctx, rule.NodeID, rule.Tag, 0); err != nil {
		return fmt.Errorf("failed to check balancing rule uniqueness: %w", err)
	} else if exists {
		return fmt.Errorf("%w: balancing rule tag %q already exists on this node", ErrInvalidRoutingRule, rule.Tag)
	}

	if err := u.nodeRepo.CreateBalancingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to create balancing rule: %w", err)
	}

	node, err := u.nodeRepo.GetNode(ctx, rule.NodeID)
	if err == nil {
		if pushErr := u.pushConfigToAgent(ctx, node); pushErr != nil {
			logger.GetLogger().Warnf("Failed to push config after adding balancing rule: %v", pushErr)
		}
	}
	return nil
}

func (u *nodeUsecase) ListBalancingRules(ctx context.Context, nodeID uint) ([]*domain.BalancingRule, error) {
	return u.nodeRepo.ListBalancingRulesByNode(ctx, nodeID)
}

func (u *nodeUsecase) UpdateBalancingRule(ctx context.Context, rule *domain.BalancingRule) error {
	if err := validateBalancingRule(rule); err != nil {
		return err
	}

	existing, err := u.nodeRepo.GetBalancingRule(ctx, rule.ID)
	if err != nil {
		return fmt.Errorf("balancing rule not found: %w", err)
	}
	// Pin NodeID — do not let body re-home the rule to another node.
	rule.NodeID = existing.NodeID

	if exists, err := u.balancingTagExistsOnNode(ctx, rule.NodeID, rule.Tag, rule.ID); err != nil {
		return fmt.Errorf("failed to check balancing rule uniqueness: %w", err)
	} else if exists {
		return fmt.Errorf("%w: balancing rule tag %q already exists on this node", ErrInvalidRoutingRule, rule.Tag)
	}

	if err := u.nodeRepo.UpdateBalancingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to update balancing rule: %w", err)
	}

	node, err := u.nodeRepo.GetNode(ctx, rule.NodeID)
	if err == nil {
		if pushErr := u.pushConfigToAgent(ctx, node); pushErr != nil {
			logger.GetLogger().Warnf("Failed to push config after updating balancing rule: %v", pushErr)
		}
	}
	return nil
}

func (u *nodeUsecase) DeleteBalancingRule(ctx context.Context, id uint) error {
	rule, err := u.nodeRepo.GetBalancingRule(ctx, id)
	if err != nil {
		return fmt.Errorf("balancing rule not found: %w", err)
	}

	if err := u.nodeRepo.DeleteBalancingRule(ctx, id); err != nil {
		return fmt.Errorf("failed to delete balancing rule: %w", err)
	}

	node, err := u.nodeRepo.GetNode(ctx, rule.NodeID)
	if err == nil {
		if pushErr := u.pushConfigToAgent(ctx, node); pushErr != nil {
			logger.GetLogger().Warnf("Failed to push config after deleting balancing rule: %v", pushErr)
		}
	}
	return nil
}
