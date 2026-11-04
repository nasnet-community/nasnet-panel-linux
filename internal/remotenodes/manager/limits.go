// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/subscriptions"
)

// A subscription may have a traffic limit and an end date. When either is
// reached the hub marks it stopped in the state and takes its client off every
// node; the switches on the nodes stay as they were, so lifting the reason
// (a higher limit, a later date, a fresh count) puts the client back with the
// same link.
//
// The mark is sticky on purpose. The periodic check only ever sets it. The
// bytes a subscription is measured by include the bucket an agent is still
// filling, which is gone from the hub's memory for a moment after a restart,
// so a check that also cleared the mark would let a stopped subscription
// through until the next poll. Only a change a person makes (a new limit or
// date, or a reset) clears it.

// DefaultLimitsEvery is how often the limits are looked at. A subscription can
// go over by what it moves in that time and in a poll's, which is seconds.
const DefaultLimitsEvery = 10 * time.Second

// trafficUsed is what a subscription has moved since its count was last reset.
func (m *Manager) trafficUsed(sub model.Subscription) uint64 {
	t := m.cfg.Usage.SubscriptionLifetime(sub.ID)
	return api.TrafficUsed(sub, sat(t.Up, t.Down))
}

func sat(a, b uint64) uint64 {
	if s := a + b; s >= a {
		return s
	}
	return ^uint64(0)
}

// watchLimits checks the limits every LimitsEvery until ctx is done.
func (m *Manager) watchLimits(ctx context.Context) {
	t := time.NewTicker(m.cfg.LimitsEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.enforceLimits(ctx)
		}
	}
}

// enforceLimits stops each running subscription that has used its limit or
// reached its end date.
func (m *Manager) enforceLimits(ctx context.Context) {
	now := m.now()
	for _, sub := range m.cfg.Store.Read().Subscriptions {
		if !sub.Running() {
			continue
		}
		if reason := sub.StopReason(m.trafficUsed(sub), now); reason != "" {
			m.stop(ctx, sub.ID, reason)
		}
	}
}

// stop marks a subscription stopped, if it is still running, and takes its
// client off the nodes.
func (m *Manager) stop(ctx context.Context, subID uint, reason string) {
	changed := false
	err := m.cfg.Store.Update(func(s *model.State) error {
		sub := s.FindSubscription(subID)
		if sub == nil || sub.Stopped == reason {
			return nil
		}
		// a change made since the check read it may have lifted the reason
		if sub.StopReason(m.trafficUsed(*sub), m.now()) == "" {
			return nil
		}
		sub.Stopped, changed = reason, true
		return nil
	})
	if err != nil {
		m.log.Error("a subscription that reached its limit could not be marked stopped; it is tried again", "subscription", subID, "err", err)
		return
	}
	if changed {
		m.log.Info("subscription stopped", "subscription", subID, "reason", reason)
		m.syncClients(ctx, subID)
	}
}

// syncClients puts a subscription's client on, or takes it off, each node it
// is switched on at, according to whether the subscription is running now. It
// tells a node again even when nothing changed, which puts right one that
// never got the last change; a node that can't be reached is put right by its
// reconcile when it is back, which reads the same state.
func (m *Manager) syncClients(ctx context.Context, subID uint) {
	st := m.cfg.Store.Read()
	var nodes []uint
	for _, e := range st.Enablements {
		if e.SubscriptionID == subID && e.Enabled {
			nodes = append(nodes, e.NodeID)
		}
	}
	if len(nodes) == 0 {
		return
	}
	unlock, err := m.lockNodes(ctx, nodes...)
	if err != nil {
		return
	}
	defer unlock()
	// what is wanted is read once the nodes are held
	st = m.cfg.Store.Read()
	sub := st.FindSubscription(subID)
	if sub == nil {
		return
	}
	for _, e := range st.Enablements {
		if e.SubscriptionID != subID || !e.Enabled {
			continue
		}
		c := clientOf(subID, e.ClientUUID)
		if sub.Running() {
			m.applyTo(ctx, e.NodeID, "resume", func(ctx context.Context, a Client) error { return subscriptions.Enable(ctx, a, c) })
			m.Event(e.NodeID, EventSubscriptionResumed, sub.Name)
			continue
		}
		m.applyTo(ctx, e.NodeID, "stop", func(ctx context.Context, a Client) error { return subscriptions.Disable(ctx, a, c.Email) })
		kind := EventSubscriptionLimit
		if sub.Stopped == model.StopExpired {
			kind = EventSubscriptionExpired
		}
		m.Event(e.NodeID, kind, sub.Name)
	}
}

// checkLimits checks what a request asks for. A date has to be in the future, so
// that a subscription is never made only to stop at once; the one it already
// has is let through, so that saving another change to an expired
// subscription doesn't trip over it.
func (m *Manager) checkLimits(l api.SubscriptionLimits, current time.Time) (api.SubscriptionLimits, error) {
	if l.TrafficLimit > model.MaxTrafficLimit {
		return l, &model.InvalidError{Msg: "The traffic limit is too large."}
	}
	if !l.ExpiresAt.IsZero() {
		l.ExpiresAt = l.ExpiresAt.UTC().Truncate(time.Second)
		if !l.ExpiresAt.After(m.now()) && !l.ExpiresAt.Equal(current) {
			return l, &model.InvalidError{Msg: "The expiry date has to be in the future."}
		}
	}
	return l, nil
}

// UpdateSubscription changes a subscription's name, traffic limit or end date,
// all or none. Changing a limit or the date works out again whether the
// subscription should run: raising the limit or moving the date on starts a
// stopped one, and a limit below what it has used stops a running one at once.
func (m *Manager) UpdateSubscription(ctx context.Context, id uint, patch api.SubscriptionPatch) (api.Subscription, error) {
	var name string
	if patch.Name != nil {
		n, err := model.CleanName(*patch.Name)
		if err != nil {
			return api.Subscription{}, err
		}
		name = n
	}
	if patch.TrafficLimit.Set && !patch.TrafficLimit.Null && patch.TrafficLimit.Value == 0 {
		return api.Subscription{}, &model.InvalidError{Msg: "The traffic limit has to be more than zero. Leave it empty for no limit."}
	}
	limitsAsked := patch.TrafficLimit.Set || patch.ExpiresAt.Set

	was := ""
	err := m.cfg.Store.Update(func(s *model.State) error {
		sub := s.FindSubscription(id)
		if sub == nil {
			return notFound("subscription", id)
		}
		was = sub.Stopped
		next := *sub
		if patch.Name != nil {
			next.Name = name
		}
		if patch.TrafficLimit.Set {
			next.TrafficLimit = 0
			if !patch.TrafficLimit.Null {
				next.TrafficLimit = patch.TrafficLimit.Value
			}
		}
		if patch.ExpiresAt.Set {
			next.ExpiresAt = time.Time{}
			if !patch.ExpiresAt.Null {
				l, err := m.checkLimits(api.SubscriptionLimits{ExpiresAt: patch.ExpiresAt.Value}, sub.ExpiresAt)
				if err != nil {
					return err
				}
				next.ExpiresAt = l.ExpiresAt
			}
		}
		if limitsAsked {
			next.Stopped = next.StopReason(m.trafficUsed(next), m.now())
		}
		if err := next.Validate(); err != nil {
			return err
		}
		// the store refuses a name another subscription has, whatever its case
		*sub = next
		return nil
	})
	if err != nil {
		return api.Subscription{}, err
	}
	m.afterLimitChange(ctx, id, was)
	return m.subscriptionView(id)
}

// ResetUsage starts a subscription's traffic count again from zero. A
// subscription that was stopped for its limit runs again; one that has
// expired stays stopped until its date moves.
func (m *Manager) ResetUsage(ctx context.Context, id uint) (api.Subscription, error) {
	was := ""
	err := m.cfg.Store.Update(func(s *model.State) error {
		sub := s.FindSubscription(id)
		if sub == nil {
			return notFound("subscription", id)
		}
		was = sub.Stopped
		t := m.cfg.Usage.SubscriptionLifetime(id)
		sub.TrafficBase = sat(t.Up, t.Down)
		sub.Stopped = sub.StopReason(0, m.now())
		return nil
	})
	if err != nil {
		return api.Subscription{}, err
	}
	m.log.Info("subscription usage reset", "subscription", id)
	m.afterLimitChange(ctx, id, was)
	return m.subscriptionView(id)
}

// afterLimitChange tells the nodes when a change made a subscription stop or
// run again.
func (m *Manager) afterLimitChange(ctx context.Context, id uint, was string) {
	st := m.cfg.Store.Read()
	sub := st.FindSubscription(id)
	if sub == nil || sub.Stopped == was {
		return
	}
	m.log.Info("subscription limits changed whether it runs", "subscription", id, "stopped", sub.Stopped)
	m.syncClients(ctx, id)
}

// stoppedSet is the subscriptions whose clients don't belong on any node.
func stoppedSet(st model.State) map[uint]bool {
	out := map[uint]bool{}
	for _, sub := range st.Subscriptions {
		if !sub.Running() {
			out[sub.ID] = true
		}
	}
	return out
}
