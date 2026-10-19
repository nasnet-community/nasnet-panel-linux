package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	productXray "github.com/nasnet-community/nasnet-panel-linux/pkg/product/xray"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/sharelink"
)

// ReverseTunnelView is a tunnel with the names the panel shows next to it.
type ReverseTunnelView struct {
	*domain.ReverseTunnel
	Kind             string `json:"kind"`
	PortalNodeName   string `json:"portal_node_name,omitempty"`
	BridgeNodeName   string `json:"bridge_node_name,omitempty"`
	PortalInboundTag string `json:"portal_inbound_tag,omitempty"`
}

// ReverseTunnelPeerConfig is what to set up on the end the panel doesn't run.
type ReverseTunnelPeerConfig struct {
	Kind string `json:"kind"`
	// External bridge: the link it dials and a complete Xray config for it.
	Link   string `json:"link,omitempty"`
	Config string `json:"config,omitempty"`
	// External portal: the client to add to the inbound the bridge dials.
	Client string `json:"client,omitempty"`
}

func (u *nodeUsecase) ListReverseTunnels(ctx context.Context, nodeID uint) ([]*ReverseTunnelView, error) {
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	names := map[uint]string{}
	views := make([]*ReverseTunnelView, 0, len(tunnels))
	for _, t := range tunnels {
		views = append(views, u.reverseTunnelView(ctx, t, names))
	}
	return views, nil
}

func (u *nodeUsecase) GetReverseTunnel(ctx context.Context, id uint) (*ReverseTunnelView, error) {
	t, err := u.nodeRepo.GetReverseTunnel(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reverse tunnel not found: %w", err)
	}
	return u.reverseTunnelView(ctx, t, map[uint]string{}), nil
}

func (u *nodeUsecase) CreateReverseTunnel(ctx context.Context, in *domain.ReverseTunnel) (*ReverseTunnelView, error) {
	t := &domain.ReverseTunnel{
		Name:            in.Name,
		PortalNodeID:    in.PortalNodeID,
		PortalInboundID: in.PortalInboundID,
		PortalHostID:    in.PortalHostID,
		InboundTags:     in.InboundTags,
		BridgeNodeID:    in.BridgeNodeID,
		ExitOutboundTag: in.ExitOutboundTag,
		ExitBalancerTag: in.ExitBalancerTag,
		PortalLink:      strings.TrimSpace(in.PortalLink),
	}
	if err := u.prepareReverseTunnel(ctx, t, true); err != nil {
		return nil, err
	}
	if err := u.nodeRepo.Transaction(ctx, func(tx repository.NodeRepository) error {
		if err := tx.CreateReverseTunnel(ctx, t); err != nil {
			return fmt.Errorf("failed to create reverse tunnel: %w", err)
		}
		return u.saveReverseTunnelRule(ctx, tx, t)
	}); err != nil {
		return nil, err
	}
	u.pushReverseTunnelNodes(t.PortalNodeID, t.BridgeNodeID)
	return u.reverseTunnelView(ctx, t, map[uint]string{}), nil
}

// UpdateReverseTunnel changes a tunnel's settings. Its ends are fixed: moving a
// tunnel means deleting it and creating another.
func (u *nodeUsecase) UpdateReverseTunnel(ctx context.Context, id uint, in *domain.ReverseTunnel) (*ReverseTunnelView, error) {
	existing, err := u.nodeRepo.GetReverseTunnel(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reverse tunnel not found: %w", err)
	}
	t := *existing
	t.Name = in.Name
	t.PortalInboundID = in.PortalInboundID
	t.PortalHostID = in.PortalHostID
	t.InboundTags = in.InboundTags
	t.ExitOutboundTag = in.ExitOutboundTag
	t.ExitBalancerTag = in.ExitBalancerTag
	if existing.PortalNodeID == nil {
		t.PortalLink = strings.TrimSpace(in.PortalLink)
	}
	if err := u.prepareReverseTunnel(ctx, &t, false); err != nil {
		return nil, err
	}
	if err := u.nodeRepo.Transaction(ctx, func(tx repository.NodeRepository) error {
		if err := tx.UpdateReverseTunnel(ctx, &t); err != nil {
			return fmt.Errorf("failed to update reverse tunnel: %w", err)
		}
		return u.saveReverseTunnelRule(ctx, tx, &t)
	}); err != nil {
		return nil, err
	}
	u.pushReverseTunnelNodes(t.PortalNodeID, t.BridgeNodeID)
	return u.reverseTunnelView(ctx, &t, map[uint]string{}), nil
}

func (u *nodeUsecase) DeleteReverseTunnel(ctx context.Context, id uint) error {
	t, err := u.nodeRepo.GetReverseTunnel(ctx, id)
	if err != nil {
		return fmt.Errorf("reverse tunnel not found: %w", err)
	}
	if t.PortalNodeID != nil {
		if err := u.reverseTagInUse(ctx, *t.PortalNodeID, t.PortalTag, t.RuleID); err != nil {
			return err
		}
	}
	if err := u.nodeRepo.Transaction(ctx, func(tx repository.NodeRepository) error {
		if t.RuleID != nil {
			if err := tx.DeleteRoutingRule(ctx, *t.RuleID); err != nil {
				return fmt.Errorf("failed to delete the tunnel's routing rule: %w", err)
			}
		}
		return tx.DeleteReverseTunnel(ctx, id)
	}); err != nil {
		return err
	}
	u.forgetReverseTunnel(t)
	u.pushReverseTunnelNodes(t.PortalNodeID, t.BridgeNodeID)
	return nil
}

// RotateReverseTunnel gives the tunnel a new id. An external bridge needs its
// new config afterwards.
func (u *nodeUsecase) RotateReverseTunnel(ctx context.Context, id uint) error {
	t, err := u.nodeRepo.GetReverseTunnel(ctx, id)
	if err != nil {
		return fmt.Errorf("reverse tunnel not found: %w", err)
	}
	if t.PortalNodeID == nil {
		return fmt.Errorf("the external portal's link sets this tunnel's id; paste its new link instead")
	}
	t.UUID = uuid.NewString()
	if err := u.nodeRepo.UpdateReverseTunnel(ctx, t); err != nil {
		return fmt.Errorf("failed to rotate reverse tunnel: %w", err)
	}
	u.pushReverseTunnelNodes(t.PortalNodeID, t.BridgeNodeID)
	return nil
}

// ReverseTunnelPeerConfig returns what to set up on the end the panel doesn't
// manage.
func (u *nodeUsecase) ReverseTunnelPeerConfig(ctx context.Context, id uint) (*ReverseTunnelPeerConfig, error) {
	t, err := u.nodeRepo.GetReverseTunnel(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reverse tunnel not found: %w", err)
	}
	peer := &ReverseTunnelPeerConfig{Kind: t.Kind()}
	switch t.Kind() {
	case domain.ReverseTunnelExternalBridge:
		raw, err := u.reverseTunnelLink(ctx, t)
		if err != nil {
			return nil, err
		}
		out, err := sharelink.ReverseBridgeOutbound(raw, "rvs-link", t.BridgeTag)
		if err != nil {
			return nil, fmt.Errorf("the bridge can't dial the portal: %w", err)
		}
		// The exit comes first: Xray's first outbound is the default.
		config := map[string]interface{}{
			"log":       map[string]interface{}{"loglevel": "warning"},
			"outbounds": []interface{}{xray.ReverseExitOutbound(), out},
			"routing": map[string]interface{}{"rules": []interface{}{map[string]interface{}{
				"type": "field", "inboundTag": []string{t.BridgeTag}, "outboundTag": domain.ReverseExitTag,
			}}},
		}
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return nil, err
		}
		peer.Link, peer.Config = raw, string(data)
	case domain.ReverseTunnelExternalPortal:
		client := map[string]interface{}{
			"id":      t.UUID,
			"email":   t.Email(),
			"reverse": map[string]interface{}{"tag": t.PortalTag},
		}
		data, err := json.MarshalIndent(client, "", "  ")
		if err != nil {
			return nil, err
		}
		peer.Client = string(data)
	default:
		return nil, fmt.Errorf("the panel runs both ends of this tunnel")
	}
	return peer, nil
}

// prepareReverseTunnel validates t and fills in what the panel decides: its
// id and tags on create, and the default name.
func (u *nodeUsecase) prepareReverseTunnel(ctx context.Context, t *domain.ReverseTunnel, create bool) error {
	t.Name = strings.TrimSpace(t.Name)
	if len(t.Name) > 100 {
		return fmt.Errorf("name must be at most 100 characters")
	}
	if t.PortalNodeID == nil && t.BridgeNodeID == nil {
		return fmt.Errorf("a reverse tunnel needs a node of this panel at one end")
	}
	if t.PortalNodeID != nil && t.BridgeNodeID != nil && *t.PortalNodeID == *t.BridgeNodeID {
		return fmt.Errorf("the portal and the bridge must be different nodes")
	}

	var portal, bridge *domain.Node
	if t.PortalNodeID != nil {
		node, err := u.nodeRepo.GetNode(ctx, *t.PortalNodeID)
		if err != nil {
			return fmt.Errorf("portal node not found")
		}
		if node.IsLite() {
			return fmt.Errorf("an MTProto node runs no Xray, so it can't be a tunnel's portal")
		}
		portal = node
		if err := u.checkReversePortal(ctx, t); err != nil {
			return err
		}
	} else {
		t.PortalInboundID, t.PortalHostID, t.InboundTags = nil, nil, nil
		id, err := reverseLinkID(t.PortalLink)
		if err != nil {
			return err
		}
		t.UUID = id
	}
	if t.BridgeNodeID != nil {
		node, err := u.nodeRepo.GetNode(ctx, *t.BridgeNodeID)
		if err != nil {
			return fmt.Errorf("bridge node not found")
		}
		if node.IsLite() {
			return fmt.Errorf("an MTProto node runs no Xray, so it can't be a tunnel's bridge")
		}
		bridge = node
		if err := u.checkReverseExit(ctx, t); err != nil {
			return err
		}
	} else {
		t.ExitOutboundTag, t.ExitBalancerTag = "", ""
	}

	if create {
		if t.PortalNodeID != nil {
			t.UUID = uuid.NewString()
		}
		var err error
		if t.PortalTag, err = u.newReverseTag(ctx, portal, "rvs-to-", endName(bridge, "external")); err != nil {
			return err
		}
		if t.BridgeTag, err = u.newReverseTag(ctx, bridge, "rvs-from-", endName(portal, "external")); err != nil {
			return err
		}
	}
	if t.Name == "" {
		t.Name = fmt.Sprintf("%s → %s", endName(portal, "External portal"), endName(bridge, "External bridge"))
	}
	// The bridge must be able to dial what the portal serves, e.g. no
	// plaintext VLESS to a public address.
	if _, err := u.reverseBridgeOutbound(ctx, t); err != nil {
		return err
	}
	return nil
}

func endName(node *domain.Node, external string) string {
	if node == nil {
		return external
	}
	return node.Name
}

// checkReversePortal checks the portal end: an enabled VLESS inbound for the
// bridge to dial, an optional host of it, and the inbounds sent into the tunnel.
func (u *nodeUsecase) checkReversePortal(ctx context.Context, t *domain.ReverseTunnel) error {
	if t.PortalInboundID == nil {
		return fmt.Errorf("choose the portal inbound the bridge connects to")
	}
	in, err := u.nodeRepo.GetInboundWithNodeAndHosts(ctx, *t.PortalInboundID)
	if err != nil || in.NodeID != *t.PortalNodeID {
		return fmt.Errorf("portal inbound not found on the portal node")
	}
	if !strings.EqualFold(in.Protocol, "vless") {
		return fmt.Errorf("the portal inbound must be a VLESS inbound")
	}
	if in.IsDisabled {
		return fmt.Errorf("the portal inbound %q is disabled", in.Tag)
	}
	if t.PortalHostID != nil {
		found := false
		for _, h := range in.Hosts {
			if h.ID == *t.PortalHostID && !h.IsDisabled {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("the chosen host isn't an enabled host of %q", in.Tag)
		}
	}

	seen := map[string]bool{}
	tags := t.InboundTags[:0]
	for _, tag := range t.InboundTags {
		if tag = strings.TrimSpace(tag); tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	t.InboundTags = tags
	if len(tags) == 0 {
		return nil
	}
	inbounds, err := u.nodeRepo.ListInboundsByNode(ctx, *t.PortalNodeID)
	if err != nil {
		return fmt.Errorf("failed to check inbound tags: %w", err)
	}
	exists := map[string]bool{}
	for _, in := range inbounds {
		exists[in.Tag] = true
	}
	for _, tag := range tags {
		if !exists[tag] {
			return fmt.Errorf("inbound %q isn't on the portal node", tag)
		}
	}
	return nil
}

// checkReverseExit checks where tunnel traffic leaves the bridge: the managed
// exit, an enabled outbound, or an enabled balancer.
func (u *nodeUsecase) checkReverseExit(ctx context.Context, t *domain.ReverseTunnel) error {
	t.ExitOutboundTag = strings.TrimSpace(t.ExitOutboundTag)
	t.ExitBalancerTag = strings.TrimSpace(t.ExitBalancerTag)
	if t.ExitOutboundTag != "" && t.ExitBalancerTag != "" {
		return fmt.Errorf("choose either an exit outbound or a balancer")
	}
	if tag := t.ExitOutboundTag; tag != "" {
		outbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, *t.BridgeNodeID)
		if err != nil {
			return fmt.Errorf("failed to check the exit outbound: %w", err)
		}
		for _, out := range outbounds {
			if out.Tag == tag && !out.IsDisabled && out.ManagedPoolID == nil {
				return nil
			}
		}
		return fmt.Errorf("exit outbound %q isn't an enabled outbound on the bridge node", tag)
	}
	if tag := t.ExitBalancerTag; tag != "" {
		balancers, err := u.nodeRepo.ListBalancingRulesByNode(ctx, *t.BridgeNodeID)
		if err != nil {
			return fmt.Errorf("failed to check the exit balancer: %w", err)
		}
		for _, bal := range balancers {
			if bal.Tag == tag && bal.Enabled {
				return nil
			}
		}
		return fmt.Errorf("exit balancer %q isn't an enabled balancer on the bridge node", tag)
	}
	return nil
}

// newReverseTag picks prefix+peer, numbered when that node already has it.
// Tags never change afterwards: routing rules refer to them.
func (u *nodeUsecase) newReverseTag(ctx context.Context, node *domain.Node, prefix, peer string) (string, error) {
	base := prefix + domain.ReverseTagSlug(peer)
	if node == nil {
		return base, nil
	}
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, node.ID)
	if err != nil {
		return "", fmt.Errorf("failed to check tunnel tags: %w", err)
	}
	used := map[string]bool{}
	for _, t := range tunnels {
		used[t.PortalTag], used[t.BridgeTag] = true, true
	}
	tag := base
	for i := 2; used[tag]; i++ {
		tag = fmt.Sprintf("%s-%d", base, i)
	}
	return tag, nil
}

// reverseLinkID checks an external portal's link and returns the id in it.
func reverseLinkID(raw string) (string, error) {
	uri, err := url.Parse(strings.TrimSpace(raw))
	if raw == "" || err != nil || uri.Scheme != "vless" || uri.User == nil {
		return "", fmt.Errorf("paste the external portal's vless:// link")
	}
	id, err := uuid.Parse(uri.User.Username())
	if err != nil {
		return "", fmt.Errorf("the link's id isn't a UUID")
	}
	return id.String(), nil
}

// saveReverseTunnelRule keeps the portal's managed rule in step with
// InboundTags: whole inbounds routed into the tunnel, ahead of a general
// fallback. The "[reverse]" remark keeps it out of the routing editor.
func (u *nodeUsecase) saveReverseTunnelRule(ctx context.Context, tx repository.NodeRepository, t *domain.ReverseTunnel) error {
	if t.PortalNodeID == nil || len(t.InboundTags) == 0 {
		if t.RuleID == nil {
			return nil
		}
		if err := tx.DeleteRoutingRule(ctx, *t.RuleID); err != nil {
			return fmt.Errorf("failed to delete the tunnel's routing rule: %w", err)
		}
		t.RuleID = nil
		return tx.UpdateReverseTunnel(ctx, t)
	}
	remark := "[reverse] " + t.Name
	if t.RuleID != nil {
		if rule, err := tx.GetRoutingRule(ctx, *t.RuleID); err == nil {
			rule.Remark, rule.InboundTags, rule.OutboundTag, rule.BalancingTag, rule.Enabled = remark, t.InboundTags, t.PortalTag, "", true
			return tx.UpdateRoutingRule(ctx, rule)
		}
	}
	rule := &domain.RoutingRule{
		NodeID:      *t.PortalNodeID,
		RuleTag:     fmt.Sprintf("%st%d-route", domain.ReverseTagPrefix, t.ID),
		Remark:      remark,
		Priority:    9000,
		Enabled:     true,
		InboundTags: t.InboundTags,
		OutboundTag: t.PortalTag,
	}
	if err := tx.CreateRoutingRule(ctx, rule); err != nil {
		return fmt.Errorf("failed to create the tunnel's routing rule: %w", err)
	}
	t.RuleID = &rule.ID
	if err := tx.UpdateReverseTunnel(ctx, t); err != nil {
		return err
	}
	rules, err := tx.ListRoutingRulesByNode(ctx, rule.NodeID)
	if err != nil {
		return fmt.Errorf("failed to load routing order: %w", err)
	}
	ordered := domain.OrderReverseRoutingRules(rules, []*domain.ReverseProxy{{Type: "portal", Rule2ID: t.RuleID}})
	ids := make([]uint, len(ordered))
	for i, r := range ordered {
		ids[i] = r.ID
	}
	return tx.ReorderRoutingRules(ctx, rule.NodeID, ids)
}

// reverseTagInUse blocks deleting a tunnel the portal's routing still sends
// traffic to: that traffic would be dropped.
func (u *nodeUsecase) reverseTagInUse(ctx context.Context, nodeID uint, tag string, ownRule *uint) error {
	rules, err := u.nodeRepo.ListRoutingRulesByOutboundTag(ctx, nodeID, tag)
	if err != nil {
		return fmt.Errorf("failed to check routing rules: %w", err)
	}
	for _, rule := range rules {
		if ownRule == nil || rule.ID != *ownRule {
			return fmt.Errorf("routing rule %q still sends traffic to %s; change it first", routingRuleLabel(rule), tag)
		}
	}
	balancers, err := u.nodeRepo.ListBalancingRulesByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check balancers: %w", err)
	}
	for _, bal := range balancers {
		if bal.FallbackTag == tag {
			return fmt.Errorf("balancer %q falls back to %s; change it first", bal.Tag, tag)
		}
		for _, sel := range bal.OutboundSelectors {
			if strings.TrimSpace(sel) == tag {
				return fmt.Errorf("balancer %q still selects %s; change it first", bal.Tag, tag)
			}
		}
	}
	return nil
}

func routingRuleLabel(rule *domain.RoutingRule) string {
	if rule.Remark != "" {
		return rule.Remark
	}
	if rule.RuleTag != "" {
		return rule.RuleTag
	}
	return fmt.Sprintf("#%d", rule.ID)
}

// reverseTunnelLink is the link the bridge dials: the external portal's, or the
// one a customer of the portal inbound would get, with the tunnel's id.
func (u *nodeUsecase) reverseTunnelLink(ctx context.Context, t *domain.ReverseTunnel) (string, error) {
	if t.PortalNodeID == nil {
		return t.PortalLink, nil
	}
	if t.PortalInboundID == nil {
		return "", fmt.Errorf("the tunnel has no portal inbound")
	}
	in, err := u.nodeRepo.GetInboundWithNodeAndHosts(ctx, *t.PortalInboundID)
	if err != nil || in.Node == nil {
		return "", fmt.Errorf("portal inbound not found")
	}
	detail := product.LinkDetail(in, in.Node)
	if t.PortalHostID != nil {
		for i := range in.Hosts {
			if in.Hosts[i].ID == *t.PortalHostID {
				product.ApplyHostOverrides(&detail, &in.Hosts[i])
			}
		}
	}
	return productXray.TunnelLink(detail, t.UUID, t.Email(), t.Name)
}

// reverseBridgeOutbound is the bridge's outbound that dials the portal.
func (u *nodeUsecase) reverseBridgeOutbound(ctx context.Context, t *domain.ReverseTunnel) (map[string]interface{}, error) {
	raw, err := u.reverseTunnelLink(ctx, t)
	if err != nil {
		return nil, err
	}
	out, err := sharelink.ReverseBridgeOutbound(raw, t.LinkTag(), t.BridgeTag)
	if err != nil {
		return nil, fmt.Errorf("the bridge can't dial the portal: %w", err)
	}
	return out, nil
}

// reverseTunnelEnds returns the tunnel ends node runs, for its config.
func (u *nodeUsecase) reverseTunnelEnds(ctx context.Context, node *domain.Node) ([]xray.ReversePortal, []xray.ReverseBridge, error) {
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, node.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list reverse tunnels: %w", err)
	}
	log := logger.GetLogger()
	var portals []xray.ReversePortal
	var bridges []xray.ReverseBridge
	for _, t := range tunnels {
		if t.PortalNodeID != nil && *t.PortalNodeID == node.ID && t.PortalInboundID != nil {
			in, err := u.nodeRepo.GetInbound(ctx, *t.PortalInboundID)
			if err != nil {
				log.WithError(err).Warnf("[ReverseTunnel] %q: portal inbound missing", t.Name)
				continue
			}
			portals = append(portals, xray.ReversePortal{InboundTag: in.Tag, UUID: t.UUID, Email: t.Email(), Tag: t.PortalTag, RuleID: t.RuleID})
		}
		if t.BridgeNodeID != nil && *t.BridgeNodeID == node.ID {
			out, err := u.reverseBridgeOutbound(ctx, t)
			if err != nil {
				// One broken tunnel must not keep the rest of the config off the node.
				log.WithError(err).Warnf("[ReverseTunnel] %q: bridge end left out", t.Name)
				continue
			}
			bridges = append(bridges, xray.ReverseBridge{Tag: t.BridgeTag, Outbound: out, ExitOutboundTag: t.ExitOutboundTag, ExitBalancerTag: t.ExitBalancerTag})
		}
	}
	return portals, bridges, nil
}

// pushReverseTunnelNodes pushes the managed ends of a tunnel in the background.
func (u *nodeUsecase) pushReverseTunnelNodes(nodeIDs ...*uint) {
	for _, id := range nodeIDs {
		if id == nil {
			continue
		}
		nodeID := *id
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			node, err := u.nodeRepo.GetNode(ctx, nodeID)
			if err != nil {
				logger.GetLogger().Warnf("Failed to get node %d for reverse tunnel config push: %v", nodeID, err)
				return
			}
			if err := u.pushConfigToAgent(ctx, node); err != nil {
				logger.GetLogger().Warnf("Failed to push config to node %d after a reverse tunnel change: %v", nodeID, err)
			}
		}()
	}
}

func (u *nodeUsecase) reverseTunnelView(ctx context.Context, t *domain.ReverseTunnel, names map[uint]string) *ReverseTunnelView {
	name := func(id *uint) string {
		if id == nil {
			return ""
		}
		if n, ok := names[*id]; ok {
			return n
		}
		if node, err := u.nodeRepo.GetNode(ctx, *id); err == nil {
			names[*id] = node.Name
		}
		return names[*id]
	}
	view := &ReverseTunnelView{ReverseTunnel: t, Kind: t.Kind(), PortalNodeName: name(t.PortalNodeID), BridgeNodeName: name(t.BridgeNodeID)}
	if t.PortalInboundID != nil {
		if in, err := u.nodeRepo.GetInbound(ctx, *t.PortalInboundID); err == nil {
			view.PortalInboundTag = in.Tag
		}
	}
	return view
}

// pushReverseBridgesOf re-pushes the bridges of the tunnels portalled on
// nodeID after its config changed: their outbound is derived from the portal
// inbound. A bridge whose config comes out the same skips its push.
func (u *nodeUsecase) pushReverseBridgesOf(nodeID uint) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, nodeID)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[ReverseTunnel] list tunnels for bridge push failed")
		return
	}
	for _, t := range tunnels {
		if t.PortalNodeID != nil && *t.PortalNodeID == nodeID {
			u.pushReverseTunnelNodes(t.BridgeNodeID)
		}
	}
}

// rejectReverseTunnelInbound keeps an inbound a tunnel depends on in place:
// the one its bridge dials, or one it sends through the tunnel when deleting.
func (u *nodeUsecase) rejectReverseTunnelInbound(ctx context.Context, in *domain.Inbound, action string, routed bool) error {
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, in.NodeID)
	if err != nil {
		return fmt.Errorf("failed to check reverse tunnels: %w", err)
	}
	for _, t := range tunnels {
		if t.PortalNodeID == nil || *t.PortalNodeID != in.NodeID {
			continue
		}
		if t.PortalInboundID != nil && *t.PortalInboundID == in.ID {
			return fmt.Errorf("cannot %s: reverse tunnel %q's bridge connects to inbound %q", action, t.Name, in.Tag)
		}
		if routed {
			for _, tag := range t.InboundTags {
				if tag == in.Tag {
					return fmt.Errorf("cannot %s: reverse tunnel %q sends inbound %q through it; remove it there first", action, t.Name, in.Tag)
				}
			}
		}
	}
	return nil
}

// rejectReverseTunnelExit keeps the outbound or balancer a tunnel leaves its
// bridge through in place.
func (u *nodeUsecase) rejectReverseTunnelExit(ctx context.Context, nodeID uint, tag string, balancer bool, action string) error {
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check reverse tunnels: %w", err)
	}
	for _, t := range tunnels {
		if t.BridgeNodeID == nil || *t.BridgeNodeID != nodeID {
			continue
		}
		if (balancer && t.ExitBalancerTag == tag) || (!balancer && t.ExitOutboundTag == tag) {
			return fmt.Errorf("cannot %s: reverse tunnel %q leaves the bridge through %q", action, t.Name, tag)
		}
	}
	return nil
}

// rejectNodeInReverseTunnel keeps a node in a tunnel from being deleted: the
// other end would be left dialing, or waiting for, nothing.
func (u *nodeUsecase) rejectNodeInReverseTunnel(ctx context.Context, nodeID uint) error {
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check reverse tunnels: %w", err)
	}
	if len(tunnels) > 0 {
		return fmt.Errorf("remove the node's reverse tunnels first (%q)", tunnels[0].Name)
	}
	return nil
}

// pushReverseBridgesOfHost re-pushes the bridges of the tunnels that dial the
// portal through host: hosts carry the address and TLS names they use.
func (u *nodeUsecase) pushReverseBridgesOfHost(ctx context.Context, hostID uint) {
	tunnels, err := u.nodeRepo.ListReverseTunnels(ctx)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[ReverseTunnel] list tunnels for host change failed")
		return
	}
	for _, t := range tunnels {
		if t.PortalHostID != nil && *t.PortalHostID == hostID {
			u.pushReverseTunnelNodes(t.BridgeNodeID)
		}
	}
}

// rejectReverseTunnelHost keeps a host a tunnel's bridge dials through: without
// it the bridge would quietly fall back to the inbound's own address.
func (u *nodeUsecase) rejectReverseTunnelHost(ctx context.Context, hostID uint) error {
	tunnels, err := u.nodeRepo.ListReverseTunnels(ctx)
	if err != nil {
		return fmt.Errorf("failed to check reverse tunnels: %w", err)
	}
	for _, t := range tunnels {
		if t.PortalHostID != nil && *t.PortalHostID == hostID {
			return fmt.Errorf("cannot delete: reverse tunnel %q's bridge dials through this host", t.Name)
		}
	}
	return nil
}
