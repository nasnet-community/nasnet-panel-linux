package usecase

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func (u *nodeUsecase) ensureNoReverseTagCollision(ctx context.Context, nodeID uint, tag string, inbound *domain.Inbound, outbound *domain.Outbound) error {
	proxies, err := u.nodeRepo.ListReverseProxiesByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check reverse proxy tags: %w", err)
	}
	for _, rp := range proxies {
		if rp.Tag == tag {
			return fmt.Errorf("tag %q is already used by reverse proxy %q", tag, rp.Tag)
		}
	}
	if len(proxies) == 0 {
		return nil
	}
	ins, err := u.nodeRepo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return err
	}
	outs, err := u.nodeRepo.ListOutboundsByNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if inbound != nil {
		ins = slices.DeleteFunc(slices.Clone(ins), func(in *domain.Inbound) bool { return in.ID == inbound.ID })
		ins = append(ins, inbound)
	}
	if outbound != nil {
		outs = slices.DeleteFunc(slices.Clone(outs), func(out *domain.Outbound) bool { return out.ID == outbound.ID })
		outs = append(outs, outbound)
	}
	return domain.ValidateVLESSReverse(ins, outs, proxies)
}

// Renaming a handler must not leave stale matchers, routes, or dialer targets.
// Reverse edits regenerate their own managed rules, so exclude only those IDs.
func (u *nodeUsecase) rejectReferencedTagRename(ctx context.Context, nodeID uint, tag, kind string, managed *domain.ReverseProxy) error {
	referenced := func(description string) error {
		return fmt.Errorf("cannot rename %s %q: referenced by %s; update or remove that reference first", kind, tag, description)
	}
	proxies, err := u.nodeRepo.ListReverseProxiesByReferencedTag(ctx, nodeID, tag)
	if err != nil {
		return fmt.Errorf("failed to check reverse proxy references: %w", err)
	}
	if len(proxies) > 0 {
		return referenced(fmt.Sprintf("reverse proxy %q", proxies[0].Tag))
	}
	rules, err := u.nodeRepo.ListRoutingRulesByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check routing references: %w", err)
	}
	for _, rule := range rules {
		if managed != nil && managed.Rule2ID != nil && rule.ID == *managed.Rule2ID {
			continue
		}
		if (kind == "outbound" && rule.OutboundTag == tag) || (kind == "inbound" && slices.Contains(rule.InboundTags, tag)) {
			return referenced(fmt.Sprintf("routing rule %q", rule.RuleTag))
		}
	}
	outbounds, err := u.nodeRepo.ListOutboundsByNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to check outbound references: %w", err)
	}
	for _, out := range outbounds {
		if kind == "inbound" {
			if out.LoopbackSettings != nil && out.LoopbackSettings.InboundTag == tag {
				return referenced(fmt.Sprintf("loopback outbound %q", out.Tag))
			}
		} else if (out.ProxySettings != nil && out.ProxySettings.Tag == tag) || (out.SockoptSettings != nil && out.SockoptSettings.DialerProxy == tag) {
			return referenced(fmt.Sprintf("outbound %q", out.Tag))
		}
	}
	if kind == "outbound" {
		inbounds, err := u.nodeRepo.ListInboundsByNode(ctx, nodeID)
		if err != nil {
			return fmt.Errorf("failed to check inbound socket references: %w", err)
		}
		for _, in := range inbounds {
			if in.SockoptSettings != nil && in.SockoptSettings.DialerProxy == tag {
				return referenced(fmt.Sprintf("inbound %q socket dialer", in.Tag))
			}
		}
		balancers, err := u.nodeRepo.ListBalancingRulesByNode(ctx, nodeID)
		if err != nil {
			return fmt.Errorf("failed to check balancer references: %w", err)
		}
		for _, bal := range balancers {
			if bal.FallbackTag == tag {
				return referenced(fmt.Sprintf("balancer %q fallback", bal.Tag))
			}
			for _, selector := range bal.OutboundSelectors {
				if strings.HasPrefix(tag, selector) {
					return referenced(fmt.Sprintf("balancer %q selector %q", bal.Tag, selector))
				}
			}
		}
	}
	return nil
}
