// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/subscriptions"
)

// The API changes subscriptions and hands out links through the manager.
var _ api.SubscriptionOps = (*Manager)(nil)

// A change to a subscription goes into the state first and then to each node
// the manager holds a connection to. A node it can't reach keeps the change as
// a wish: its loop puts the node's clients right with the state after every
// reconnect, and that is what applies it. So a failed send is logged and the
// call still succeeds; the state is what the hub means, and the nodes catch up.
//
// The one change that can't wait that way is a new link (see ReplaceLink).

// applyTimeout is how long one node gets to take one change.
const applyTimeout = 10 * time.Second

// applyLocks let one thing at a time change a node's clients. A reconcile
// reads what is wanted and then works on it for as long as it takes, and a
// switch that lands in the middle would be undone by what it read before. So
// both take the node's lock. A channel and not a mutex, so a request that
// goes away stops waiting.
//
// Id 0 is nobody's node, and holds the lock for backups: scrypt wants 32 MiB,
// which the router can spare once and not three times.
type applyLocks struct {
	mu    sync.Mutex
	locks map[uint]chan struct{}
}

func (a *applyLocks) of(id uint) chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locks == nil {
		a.locks = map[uint]chan struct{}{}
	}
	l := a.locks[id]
	if l == nil {
		l = make(chan struct{}, 1)
		a.locks[id] = l
	}
	return l
}

// lockNodes takes the locks of the nodes, the lowest id first so that two
// callers can't wait on each other, and returns what lets them go. If ctx ends
// first it holds nothing and says so.
func (m *Manager) lockNodes(ctx context.Context, ids ...uint) (unlock func(), err error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	var held []chan struct{}
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			<-held[i]
		}
	}
	for _, id := range slices.Compact(ids) {
		l := m.apply.of(id)
		select {
		case l <- struct{}{}:
			held = append(held, l)
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	return release, nil
}

// subscribable is the node a subscription may be switched on for: one that is
// installed, with no install still running on it.
func (m *Manager) subscribable(st model.State, id uint) (model.Node, error) {
	n := st.FindNode(id)
	if n == nil {
		return model.Node{}, notFound("node", id)
	}
	if in := m.installing(id); in != nil {
		return model.Node{}, &jobs.BusyError{NodeID: id, JobID: in.JobID}
	}
	if n.State != model.NodeRegistered {
		// the install died before the node was saved: only removal works
		return model.Node{}, fmt.Errorf("%w: node %d was never fully installed", model.ErrNotFound, id)
	}
	return *n, nil
}

func notFound(what string, id uint) error {
	return fmt.Errorf("%w: %s %d", model.ErrNotFound, what, id)
}

// What a refusal says, for the page and for whoever reads the API.
var (
	errSwitchedOff = &api.Error{Code: api.CodeNotFound, Msg: "This subscription is switched off on that node, so it has no link there."}
	errCantReach   = &api.Error{Code: api.CodeNodeUnreachable, Msg: "The node can't be reached, so its link can't be replaced now."}
	errNotTaken    = &api.Error{Code: api.CodeNodeUnreachable, Msg: "The node did not take the change, so the link was not replaced."}
)

func clientOf(subID uint, clientID string) subscriptions.Client {
	return subscriptions.Client{Email: nodecfg.SubscriptionEmail(int(subID)), UUID: clientID}
}

// newClientID is a subscription's login on one node, which is what its link
// carries.
func newClientID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("making a client id: %w", err)
	}
	return id.String(), nil
}

// callNode runs a change on the node's agent over the connection its loop
// holds. With no connection it does nothing and says so: the node isn't
// reachable now.
//
// The change runs on a context of its own, since a browser that closes the page
// would otherwise leave a node with half of a swap done.
func (m *Manager) callNode(ctx context.Context, nodeID uint, do func(context.Context, Client) error) (held bool, err error) {
	c, ok := m.Client(nodeID)
	if !ok {
		return false, nil
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), applyTimeout)
	defer cancel()
	return true, do(actx, c)
}

// applyTo is callNode for a change the state holds already. A node that fails
// it isn't brought into line until its loop reconciles, so it is asked to
// reconnect, which starts one.
func (m *Manager) applyTo(ctx context.Context, nodeID uint, change string, do func(context.Context, Client) error) {
	held, err := m.callNode(ctx, nodeID, do)
	if held && err != nil {
		m.log.Warn("a change to a node's subscriptions did not go through; it is applied when the node connects again",
			"node", nodeID, "change", change, "err", err)
		m.Reconnect(nodeID)
	}
}

func (m *Manager) subscriptionView(id uint) (api.Subscription, error) {
	v, ok := api.ViewSubscription(m.cfg.Store.Read(), id, m)
	if !ok {
		return api.Subscription{}, notFound("subscription", id)
	}
	return v, nil
}

// CreateSubscription adds a subscription, switched on at the nodes named, each
// with a client id of its own. Only installed nodes may be named, and none whose
// install is still running. A node it can't reach takes the client when it is
// back.
func (m *Manager) CreateSubscription(ctx context.Context, name string, nodeIDs []uint) (api.Subscription, error) {
	name, err := model.CleanName(name)
	if err != nil {
		return api.Subscription{}, err
	}
	ids := slices.Clone(nodeIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	st := m.cfg.Store.Read()
	for _, id := range ids {
		if _, err := m.subscribable(st, id); err != nil {
			return api.Subscription{}, err
		}
	}
	unlock, err := m.lockNodes(ctx, ids...)
	if err != nil {
		return api.Subscription{}, err
	}
	defer unlock()

	var made model.Subscription
	clients := map[uint]subscriptions.Client{}
	err = m.cfg.Store.Update(func(s *model.State) error {
		sub, err := s.AddSubscription(model.Subscription{Name: name, CreatedAt: m.now()})
		if err != nil {
			return err
		}
		for _, id := range ids {
			// the node may have been removed since it was looked at
			if n := s.FindNode(id); n == nil || n.State != model.NodeRegistered {
				return notFound("node", id)
			}
			cid, err := newClientID()
			if err != nil {
				return err
			}
			s.Enablements = append(s.Enablements, model.Enablement{SubscriptionID: sub.ID, NodeID: id, ClientUUID: cid, Enabled: true})
			clients[id] = clientOf(sub.ID, cid)
		}
		made = sub
		return nil
	})
	if err != nil {
		return api.Subscription{}, err
	}
	for _, id := range ids {
		c := clients[id]
		m.applyTo(ctx, id, "add", func(ctx context.Context, a Client) error { return subscriptions.Enable(ctx, a, c) })
		m.Event(id, EventSubscriptionOn, made.Name)
	}
	m.log.Info("subscription added", "subscription", made.ID, "name", made.Name, "nodes", ids)
	return m.subscriptionView(made.ID)
}

// RenameSubscription gives a subscription another name. The nodes know it by
// its number, and a link carries the name only as a label, so nothing changes
// on a node and the links already handed out keep working.
func (m *Manager) RenameSubscription(_ context.Context, id uint, name string) (api.Subscription, error) {
	name, err := model.CleanName(name)
	if err != nil {
		return api.Subscription{}, err
	}
	// the store refuses a name another subscription has, whatever its case
	err = m.cfg.Store.Update(func(s *model.State) error {
		sub := s.FindSubscription(id)
		if sub == nil {
			return notFound("subscription", id)
		}
		sub.Name = name
		return nil
	})
	if err != nil {
		return api.Subscription{}, err
	}
	return m.subscriptionView(id)
}

// DeleteSubscription removes a subscription and takes its client off every node
// it had one on. A node that can't be reached keeps the client until it is
// back, when the reconcile finds nobody wants it and removes it. What the
// subscription used is forgotten.
func (m *Manager) DeleteSubscription(ctx context.Context, id uint) error {
	st := m.cfg.Store.Read()
	sub := st.FindSubscription(id)
	if sub == nil {
		return notFound("subscription", id)
	}
	name := sub.Name
	var nodes []uint
	for _, e := range st.Enablements {
		if e.SubscriptionID == id {
			nodes = append(nodes, e.NodeID)
		}
	}
	unlock, err := m.lockNodes(ctx, nodes...)
	if err != nil {
		return err
	}
	defer unlock()

	var removed []model.Enablement
	err = m.cfg.Store.Update(func(s *model.State) error {
		removed = removed[:0]
		for _, e := range s.Enablements {
			if e.SubscriptionID == id {
				removed = append(removed, e)
			}
		}
		return s.RemoveSubscription(id)
	})
	if err != nil {
		return err
	}
	// Off as well as on: a node that was away when the switch went off may
	// still have the client, and taking off one that isn't there is no error.
	email := nodecfg.SubscriptionEmail(int(id))
	for _, e := range removed {
		m.applyTo(ctx, e.NodeID, "remove", func(ctx context.Context, a Client) error { return subscriptions.Disable(ctx, a, email) })
		if e.Enabled {
			m.Event(e.NodeID, EventSubscriptionOff, name)
		}
	}
	m.cfg.Usage.ForgetSubscription(id)
	m.log.Info("subscription removed", "subscription", id, "name", name)
	return nil
}

// SetEnabled switches a subscription on or off at one node. Off keeps the
// client id in the state, so switching on again gives the same link back. The
// choice is saved first and a node that can't be reached takes it when it is
// back.
//
// Asking for what is so already does no harm: no event is made, but the node
// is told again, which puts right a node that never got the last change.
func (m *Manager) SetEnabled(ctx context.Context, subID, nodeID uint, enabled bool) (api.Subscription, error) {
	st := m.cfg.Store.Read()
	if st.FindSubscription(subID) == nil {
		return api.Subscription{}, notFound("subscription", subID)
	}
	if _, err := m.subscribable(st, nodeID); err != nil {
		return api.Subscription{}, err
	}
	unlock, err := m.lockNodes(ctx, nodeID)
	if err != nil {
		return api.Subscription{}, err
	}
	defer unlock()

	var (
		name    string
		client  subscriptions.Client
		known   bool // the state has a client id for the pair
		changed bool
	)
	err = m.cfg.Store.Update(func(s *model.State) error {
		sub := s.FindSubscription(subID)
		if sub == nil {
			return notFound("subscription", subID)
		}
		if n := s.FindNode(nodeID); n == nil || n.State != model.NodeRegistered {
			return notFound("node", nodeID)
		}
		name = sub.Name
		e := s.FindEnablement(subID, nodeID)
		switch {
		case e == nil && !enabled:
			return nil // never on here, so nothing to switch off
		case e == nil:
			cid, err := newClientID()
			if err != nil {
				return err
			}
			s.Enablements = append(s.Enablements, model.Enablement{SubscriptionID: subID, NodeID: nodeID, ClientUUID: cid, Enabled: true})
			e, changed = &s.Enablements[len(s.Enablements)-1], true
		case e.Enabled != enabled:
			e.Enabled, changed = enabled, true
		}
		client, known = clientOf(subID, e.ClientUUID), true
		return nil
	})
	if err != nil {
		return api.Subscription{}, err
	}
	if known && enabled {
		m.applyTo(ctx, nodeID, "on", func(ctx context.Context, a Client) error { return subscriptions.Enable(ctx, a, client) })
	} else if known {
		m.applyTo(ctx, nodeID, "off", func(ctx context.Context, a Client) error { return subscriptions.Disable(ctx, a, client.Email) })
	}
	if changed {
		kind := EventSubscriptionOff
		if enabled {
			kind = EventSubscriptionOn
		}
		m.Event(nodeID, kind, name)
		m.log.Info("subscription switched", "subscription", subID, "node", nodeID, "on", enabled)
	}
	return m.subscriptionView(subID)
}

// Link is the address a subscription's client uses to reach a node: the node's
// address and entry port, its REALITY values and the client id the state keeps
// for the pair, with the subscription's name as the label. It is refused while
// the subscription is switched off there, since the client is not on the node.
func (m *Manager) Link(subID, nodeID uint) (api.Link, error) {
	st := m.cfg.Store.Read()
	n, sub, e, err := linkParts(st, subID, nodeID)
	if err != nil {
		return api.Link{}, err
	}
	return buildLink(n, sub, e.ClientUUID)
}

// linkParts finds what a link is made of, or why there is none.
func linkParts(st model.State, subID, nodeID uint) (model.Node, model.Subscription, model.Enablement, error) {
	sub := st.FindSubscription(subID)
	if sub == nil {
		return model.Node{}, model.Subscription{}, model.Enablement{}, notFound("subscription", subID)
	}
	n := st.FindNode(nodeID)
	if n == nil || n.State != model.NodeRegistered {
		return model.Node{}, model.Subscription{}, model.Enablement{}, notFound("node", nodeID)
	}
	e := st.FindEnablement(subID, nodeID)
	if e == nil || !e.Enabled {
		return model.Node{}, model.Subscription{}, model.Enablement{}, errSwitchedOff
	}
	return *n, *sub, *e, nil
}

func buildLink(n model.Node, sub model.Subscription, clientID string) (api.Link, error) {
	vless, err := subscriptions.BuildLink(subscriptions.LinkParams{
		UUID: clientID, Host: n.Host, Port: n.EntryPort, ServerName: serverName(n),
		PublicKey: n.RealityPublicKey, ShortID: n.RealityShortID, Name: sub.Name,
	})
	if err != nil {
		// a node that is installed has all of this, so something is wrong in the state
		return api.Link{}, fmt.Errorf("the link of subscription %d on node %d: %w", sub.ID, n.ID, err)
	}
	return api.Link{NodeID: n.ID, NodeName: n.Name, Vless: vless}, nil
}

// ReplaceLink gives a subscription a new client id on one node: the link
// handed out before stops working, and the answer is the one that takes its
// place.
//
// A switch can be left for a node to catch up on, but this can't. The
// reconcile tells a client that is missing from one that is there and nothing
// more, since the agent doesn't say what id a client has, so a new id saved for
// later would leave the old link working and the new one dead. So the node goes
// first, and with no connection to it the call is refused. The id is saved once
// the node holds it, and if that fails the node gets its old one back.
func (m *Manager) ReplaceLink(ctx context.Context, subID, nodeID uint) (api.Link, error) {
	if _, _, _, err := linkParts(m.cfg.Store.Read(), subID, nodeID); err != nil {
		return api.Link{}, err
	}
	unlock, err := m.lockNodes(ctx, nodeID)
	if err != nil {
		return api.Link{}, err
	}
	defer unlock()

	// it may have been switched off while this waited
	n, sub, was, err := linkParts(m.cfg.Store.Read(), subID, nodeID)
	if err != nil {
		return api.Link{}, err
	}
	fresh, err := newClientID()
	if err != nil {
		return api.Link{}, err
	}
	held, err := m.callNode(ctx, nodeID, func(ctx context.Context, a Client) error {
		return subscriptions.Replace(ctx, a, clientOf(subID, fresh))
	})
	switch {
	case !held:
		return api.Link{}, errCantReach
	case err != nil:
		// the node may have lost the client; the reconcile after a reconnect puts it back
		m.log.Warn("a link could not be replaced on its node", "subscription", subID, "node", nodeID, "err", err)
		m.Reconnect(nodeID)
		return api.Link{}, errNotTaken
	}

	err = m.cfg.Store.Update(func(s *model.State) error {
		e := s.FindEnablement(subID, nodeID)
		if e == nil || !e.Enabled {
			return errSwitchedOff
		}
		e.ClientUUID = fresh
		return nil
	})
	if err != nil {
		m.applyTo(ctx, nodeID, "restore", func(ctx context.Context, a Client) error {
			return subscriptions.Replace(ctx, a, clientOf(subID, was.ClientUUID))
		})
		return api.Link{}, err
	}
	m.Event(nodeID, EventSubscriptionOn, sub.Name+" (new link)")
	m.log.Info("link replaced", "subscription", subID, "node", nodeID)
	return buildLink(n, sub, fresh)
}
