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

func clientOf(subID uint, uuid string) subscriptions.Client {
	return subscriptions.Client{Email: nodecfg.SubscriptionEmail(int(subID)), UUID: uuid}
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
