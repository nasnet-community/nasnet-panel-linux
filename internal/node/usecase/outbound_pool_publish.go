package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/sharelink"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
)

type poolOverrideKey struct{}
type poolOverride struct {
	poolID   uint
	members  []*d.Outbound
	balancer *d.BalancingRule
}

func poolPrefix(id uint) string { return fmt.Sprintf("pool-%d-", id) }
func poolTag(id uint, fingerprint string) string {
	sum := sha256.Sum256([]byte(fingerprint))
	return poolPrefix(id) + hex.EncodeToString(sum[:])
}
func overridePool(ctx context.Context, outbounds []*d.Outbound, balancers []*d.BalancingRule) ([]*d.Outbound, []*d.BalancingRule) {
	override, _ := ctx.Value(poolOverrideKey{}).(*poolOverride)
	if override == nil {
		return outbounds, balancers
	}
	filtered := make([]*d.Outbound, 0, len(outbounds)+len(override.members))
	for _, out := range outbounds {
		if out.ManagedPoolID == nil || *out.ManagedPoolID != override.poolID {
			filtered = append(filtered, out)
		}
	}
	filtered = append(filtered, override.members...)
	for i, b := range balancers {
		if b.ID == override.balancer.ID {
			balancers[i] = override.balancer
		}
	}
	return filtered, balancers
}

func (s *OutboundPoolService) publish(ctx context.Context, p *d.OutboundPool, r *d.OutboundPoolRun) error {
	lock := s.nodes.getOrCreatePushState(p.NodeID)
	lock.applyMu.Lock()
	defer lock.applyMu.Unlock()
	var current d.OutboundPool
	if err := s.db.WithContext(ctx).First(&current, p.ID).Error; err != nil {
		return err
	}
	if current.Revision != r.Revision || current.ActiveRunID != r.ID {
		return fmt.Errorf("pool changed during scan; results were not published")
	}
	node, err := s.nodes.nodeRepo.GetNode(ctx, p.NodeID)
	if err != nil {
		return err
	}
	if node.XrayStopped {
		return fmt.Errorf("Xray is stopped; results saved without publishing")
	}
	bal, err := s.nodes.nodeRepo.GetBalancingRule(ctx, p.BalancerID)
	if err != nil {
		return err
	}
	if !bal.Enabled {
		return fmt.Errorf("balancer is disabled; results saved without publishing")
	}
	if err = poolFallback(s.db.WithContext(ctx), p.NodeID, bal.FallbackTag); err != nil {
		return err
	}
	if err = s.validateSelectors(ctx, p.ID, p.NodeID, p.BalancerID); err != nil {
		return err
	}
	var old []d.Outbound
	if err = s.db.WithContext(ctx).Where("managed_pool_id = ?", p.ID).Find(&old).Error; err != nil {
		return err
	}
	byTag := map[string]d.Outbound{}
	for _, x := range old {
		byTag[x.Tag] = x
	}
	var candidates []d.OutboundPoolCandidate
	if err = s.db.WithContext(ctx).Where("pool_id = ? AND present = ? AND excluded = ? AND outbound_json <> ? AND failures < ?", p.ID, true, false, "", p.FailureThreshold).Find(&candidates).Error; err != nil {
		return err
	}
	eligible := make([]d.OutboundPoolCandidate, 0, len(candidates))
	for _, x := range candidates {
		prior, exists := byTag[poolTag(p.ID, x.Fingerprint)]
		if (exists && !prior.IsDisabled) || (x.TestedAt != nil && !x.TestedAt.Before(r.StartedAt) && x.Status == "passed") {
			eligible = append(eligible, x)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		a, ae := byTag[poolTag(p.ID, eligible[i].Fingerprint)]
		b, be := byTag[poolTag(p.ID, eligible[j].Fingerprint)]
		ai, bi := ae && !a.IsDisabled, be && !b.IsDisabled
		if ai != bi {
			return ai
		}
		if eligible[i].LatencyMs != eligible[j].LatencyMs {
			return eligible[i].LatencyMs < eligible[j].LatencyMs
		}
		return eligible[i].Fingerprint < eligible[j].Fingerprint
	})
	if len(eligible) > p.MaxActive {
		eligible = eligible[:p.MaxActive]
	}
	members := make([]*d.Outbound, 0, len(eligible))
	for _, x := range eligible {
		tag := poolTag(p.ID, x.Fingerprint)
		var config map[string]any
		if err = json.Unmarshal([]byte(x.OutboundJSON), &config); err != nil {
			return fmt.Errorf("invalid saved outbound")
		}
		sharelink.DropNulls(config)
		config["tag"] = tag
		encoded, e := json.Marshal(config)
		if e != nil {
			return e
		}
		out := byTag[tag]
		out.NodeID = p.NodeID
		out.ManagedPoolID = &p.ID
		out.Tag = tag
		out.ManagedConfig = string(encoded)
		out.IsDisabled = false
		out.Protocol = x.Protocol
		out.Address = x.Address
		out.Remark = x.Remark
		out.LastTestedAt = x.TestedAt
		members = append(members, &out)
	}
	prefix := poolPrefix(p.ID)
	found := false
	for _, selector := range bal.OutboundSelectors {
		if selector == prefix {
			found = true
		}
	}
	if !found {
		bal.OutboundSelectors = append(bal.OutboundSelectors, prefix)
	}
	// When the node already runs exactly these members, a push would only
	// restart xray — on the router that drops every LAN client — for nothing.
	// The run is still recorded and acknowledged below.
	if found && current.AppliedRevision > 0 && samePoolMembers(old, members) {
		logger.GetLogger().WithField("pool_id", p.ID).Info("[pools] members unchanged (no change); config push skipped")
	} else {
		if err = s.runState(ctx, r, "applying"); err != nil {
			return err
		}
		override := &poolOverride{poolID: p.ID, members: members, balancer: bal}
		applyCtx := context.WithValue(ctx, poolOverrideKey{}, override)
		if err = s.apply(applyCtx, node); err != nil {
			return s.restorePool(node.ID, fmt.Errorf("pool apply failed: %w", err))
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fresh d.OutboundPool
		if e := tx.First(&fresh, p.ID).Error; e != nil {
			return e
		}
		if fresh.ActiveRunID != r.ID || fresh.Revision != r.Revision {
			return fmt.Errorf("pool changed before acknowledgement")
		}
		if e := tx.Model(&d.Outbound{}).Where("managed_pool_id = ?", p.ID).Update("is_disabled", true).Error; e != nil {
			return e
		}
		for _, out := range members {
			if out.ID == 0 {
				if e := tx.Create(out).Error; e != nil {
					return e
				}
			} else {
				fields := map[string]any{"managed_config": out.ManagedConfig, "is_disabled": false, "remark": out.Remark, "protocol": out.Protocol, "address": out.Address, "last_tested_at": out.LastTestedAt}
				if e := tx.Model(&d.Outbound{}).Where("id = ? AND managed_pool_id = ?", out.ID, p.ID).Updates(fields).Error; e != nil {
					return e
				}
			}
		}
		if e := tx.Model(&d.BalancingRule{}).Where("id = ?", bal.ID).Update("outbound_selectors", bal.OutboundSelectors).Error; e != nil {
			return e
		}
		if e := tx.Model(&fresh).Update("applied_revision", fresh.Revision).Error; e != nil {
			return e
		}
		return tx.Model(&d.OutboundPoolRun{}).Where("id = ?", r.ID).Update("active", len(members)).Error
	})
	if err != nil {
		return s.restorePool(node.ID, fmt.Errorf("pool acknowledgement failed: %w", err))
	}
	r.Active = len(members)
	return nil
}

// samePoolMembers reports whether the enabled published members are exactly
// the new set, config for config.
func samePoolMembers(published []d.Outbound, next []*d.Outbound) bool {
	live := map[string]string{}
	for _, out := range published {
		if !out.IsDisabled {
			live[out.Tag] = out.ManagedConfig
		}
	}
	if len(live) != len(next) {
		return false
	}
	for _, out := range next {
		if cfg, ok := live[out.Tag]; !ok || cfg != out.ManagedConfig {
			return false
		}
	}
	return true
}

// Rebuild from current DB state so rollback preserves unrelated manual edits.
func (s *OutboundPoolService) restorePool(nodeID uint, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	node, err := s.nodes.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("%v; restoration is pending", cause)
	}
	if !node.XrayStopped {
		if err = s.apply(ctx, node); err != nil {
			return fmt.Errorf("%v; restoration is pending: %v", cause, err)
		}
	}
	return cause
}
func (s *OutboundPoolService) validateSelectors(ctx context.Context, poolID, nodeID, balancerID uint) error {
	bals, err := s.nodes.nodeRepo.ListBalancingRulesByNode(ctx, nodeID)
	if err != nil {
		return err
	}
	for _, bal := range bals {
		for _, selector := range bal.OutboundSelectors {
			if strings.HasPrefix(poolPrefix(poolID), selector) && !(bal.ID == balancerID && selector == poolPrefix(poolID)) {
				return fmt.Errorf("balancer %q has selector %q overlapping this pool; narrow it first", bal.Tag, selector)
			}
		}
	}
	return nil
}

func (s *OutboundPoolService) Delete(ctx context.Context, nodeID, poolID uint) error {
	lock := s.nodes.getOrCreatePushState(nodeID)
	lock.applyMu.Lock()
	defer lock.applyMu.Unlock()
	var p d.OutboundPool
	if err := s.db.WithContext(ctx).First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; err != nil {
		return err
	}
	if p.ActiveRunID != 0 {
		return ErrPoolBusy
	}
	node, err := s.nodes.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if node.XrayStopped {
		return fmt.Errorf("resume Xray before removing pool members")
	}
	bal, err := s.nodes.nodeRepo.GetBalancingRule(ctx, p.BalancerID)
	if err != nil {
		return err
	}
	selectors := jsontype.StringSlice{}
	for _, x := range bal.OutboundSelectors {
		if x != poolPrefix(poolID) {
			selectors = append(selectors, x)
		}
	}
	if len(selectors) == 0 {
		selectors = append(selectors, poolPrefix(poolID))
	}
	bal.OutboundSelectors = selectors
	override := &poolOverride{poolID: poolID, balancer: bal}
	if err = s.apply(context.WithValue(ctx, poolOverrideKey{}, override), node); err != nil {
		return s.restorePool(nodeID, err)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("managed_pool_id = ?", poolID).Delete(&d.Outbound{}).Error; e != nil {
			return e
		}
		for _, model := range []any{&d.OutboundPoolSource{}, &d.OutboundPoolCandidate{}, &d.OutboundPoolRun{}} {
			if e := tx.Where("pool_id = ?", poolID).Delete(model).Error; e != nil {
				return e
			}
		}
		if e := tx.Model(&d.BalancingRule{}).Where("id = ?", bal.ID).Update("outbound_selectors", selectors).Error; e != nil {
			return e
		}
		return tx.Delete(&d.OutboundPool{}, poolID).Error
	})
	if err != nil {
		return s.restorePool(nodeID, err)
	}
	return nil
}
