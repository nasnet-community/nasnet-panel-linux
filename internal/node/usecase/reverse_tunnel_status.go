package usecase

import (
	"context"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// reverseTunnelWatch remembers which tunnels had their state announced since
// the hub started, so a restart doesn't lose an alert that was pending.
type reverseTunnelWatch struct {
	mu        sync.Mutex
	announced map[uint]bool
}

// observeReverseTunnels takes the hidden reverse users out of a portal's
// online-IP snapshot, so they never count as customers online, and records
// whether each tunnel's bridge is connected: the portal sees the bridge as the
// hidden user's online IP for as long as the tunnel is up.
func (u *nodeUsecase) observeReverseTunnels(ctx context.Context, node *domain.Node, ips map[string]map[string]int64, at time.Time) {
	hidden := map[string]map[string]int64{}
	for email, set := range ips {
		if domain.IsReverseEmail(email) {
			hidden[email] = set
			delete(ips, email)
		}
	}
	tunnels, err := u.nodeRepo.ListReverseTunnelsByNode(ctx, node.ID)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[ReverseTunnel] list tunnels for status failed")
		return
	}
	for _, t := range tunnels {
		if t.PortalNodeID != nil && *t.PortalNodeID == node.ID {
			u.observeReverseTunnel(ctx, t, hidden[t.Email()], at)
		}
	}
}

func (u *nodeUsecase) observeReverseTunnel(ctx context.Context, t *domain.ReverseTunnel, ips map[string]int64, at time.Time) {
	state, bridgeIP := domain.ReverseTunnelDown, ""
	var newest int64
	for ip, seen := range ips {
		if bridgeIP == "" || seen > newest {
			bridgeIP, newest = ip, seen
		}
	}
	if bridgeIP != "" {
		state = domain.ReverseTunnelConnected
	}

	changed := state != t.State
	write := changed
	if changed {
		t.State, t.StateSince = state, &at
	}
	if bridgeIP != "" && bridgeIP != t.BridgeIP {
		t.BridgeIP, write = bridgeIP, true
	}
	if state == domain.ReverseTunnelConnected {
		if t.LastSeenAt == nil || at.Sub(*t.LastSeenAt) >= time.Minute {
			write = true
		}
		t.LastSeenAt = &at
	}
	if write {
		if err := u.nodeRepo.UpdateReverseTunnelState(ctx, t); err != nil {
			logger.GetLogger().WithError(err).Warnf("[ReverseTunnel] %q: saving status failed", t.Name)
		}
	}

	u.reverseWatch.mu.Lock()
	if u.reverseWatch.announced == nil {
		u.reverseWatch.announced = map[uint]bool{}
	}
	announce := changed || !u.reverseWatch.announced[t.ID]
	u.reverseWatch.announced[t.ID] = true
	u.reverseWatch.mu.Unlock()
	if !announce || u.eventBus == nil {
		return
	}
	since := at
	if t.StateSince != nil {
		since = *t.StateSince
	}
	kind := events.EventReverseTunnelDown
	if state == domain.ReverseTunnelConnected {
		kind = events.EventReverseTunnelUp
	}
	u.eventBus.Publish(events.Event{Type: kind, Payload: events.ReverseTunnelPayload{
		TunnelID: t.ID, Name: t.Name, PortalNodeID: *t.PortalNodeID, BridgeIP: t.BridgeIP, Since: since,
	}})
}

// forgetReverseTunnel drops a deleted tunnel's alert state without a recovery
// notice.
func (u *nodeUsecase) forgetReverseTunnel(t *domain.ReverseTunnel) {
	u.reverseWatch.mu.Lock()
	delete(u.reverseWatch.announced, t.ID)
	u.reverseWatch.mu.Unlock()
	if u.eventBus == nil || t.PortalNodeID == nil {
		return
	}
	u.eventBus.Publish(events.Event{Type: events.EventReverseTunnelUp, Payload: events.ReverseTunnelPayload{
		TunnelID: t.ID, Name: t.Name, PortalNodeID: *t.PortalNodeID, Since: time.Now(), Removed: true,
	}})
}
