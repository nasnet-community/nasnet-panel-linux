// SPDX-License-Identifier: AGPL-3.0-or-later

package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// What every subscription's client looks like on the entry inbound.
const (
	clientProtocol   = "vless"
	clientEncryption = "none"
	clientLevel      = 0
)

// Agent is the part of a node's agent that manages its clients. The hub's
// *agent.Client satisfies it, and so do the reverse and embedded clients; a
// test stands in with three methods.
type Agent interface {
	AddUser(ctx context.Context, inboundTag, email, uuid, protocol, flow, encryption string, level int32) error
	RemoveUser(ctx context.Context, inboundTag, email string) error
	ListUsers(ctx context.Context, inboundTag string) ([]*pb.UserInfo, error)
}

// Client is a subscription's login on one node.
type Client struct {
	Email string // nodecfg.SubscriptionEmail(id), the same on every node
	UUID  string // the credential in the link; never goes into an error or a log
}

// Only subscription clients are handled here. The bridge's client carries the
// tunnel's reverse tag, which the agent takes from the node's config and AddUser
// can't set, so it travels with a config push; taking it off, or putting it
// back without the tag, would cut the tunnel.
func checkEmail(email string) error {
	if _, ok := nodecfg.ParseSubscriptionEmail(email); !ok {
		return fmt.Errorf("subscriptions: %q is not a subscription's email, and only those are managed here", email)
	}
	return nil
}

func (c Client) check() error {
	if err := checkEmail(c.Email); err != nil {
		return err
	}
	if !uuidRe.MatchString(c.UUID) {
		return fmt.Errorf("subscriptions: the client id of %s is not a UUID", c.Email)
	}
	return nil
}

// Xray answers an add of an email the inbound already has with "User <email>
// already exists." and a remove of one it doesn't with "User <email> not
// found.". The agent hands that text back inside its own (a prefix, the gRPC
// status, the client's prefix) without changing it, and every client does the
// same, so the check looks for the whole sentence with the email in it. "not
// found" alone would also pass "handler not found: entry", which says the node
// has no such inbound and is a real fault.
func said(err error, email, what string) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower("user "+email+" "+what))
}

func alreadyThere(err error, email string) bool { return said(err, email, "already exists") }
func alreadyGone(err error, email string) bool  { return said(err, email, "not found") }

// Enable gives the subscription its client on the node, without a restart. A
// node that has the client already is not an error, which makes it safe to
// repeat.
//
// The agent can't say what credential the existing client has, so Enable
// leaves it alone. To change the UUID, which is what replacing a link does,
// use Replace.
func Enable(ctx context.Context, a Agent, c Client) error {
	if err := c.check(); err != nil {
		return err
	}
	err := a.AddUser(ctx, nodecfg.EntryTag, c.Email, c.UUID, clientProtocol, DefaultFlow, clientEncryption, clientLevel)
	if err != nil && !alreadyThere(err, c.Email) {
		return fmt.Errorf("subscriptions: add %s: %w", c.Email, err)
	}
	return nil
}

// Disable takes the client off the node. One the node doesn't have is not an
// error. Xray stops taking the id for new connections; a connection that is
// open already is not cut, and there is no call that would.
func Disable(ctx context.Context, a Agent, email string) error {
	if err := checkEmail(email); err != nil {
		return err
	}
	err := a.RemoveUser(ctx, nodecfg.EntryTag, email)
	if err != nil && !alreadyGone(err, email) {
		return fmt.Errorf("subscriptions: remove %s: %w", email, err)
	}
	return nil
}

// Replace gives the subscription a new credential on the node: the old client
// is removed and the new one added, so the old link stops working for new
// connections. If the add fails the node is left without the client, and
// calling Enable puts it back.
func Replace(ctx context.Context, a Agent, c Client) error {
	if err := c.check(); err != nil {
		return err
	}
	if err := Disable(ctx, a, c.Email); err != nil {
		return err
	}
	return Enable(ctx, a, c)
}

// Diff is what Reconcile found and did. Added, Removed and Replaced hold only
// what was done, in the order it was done: a change that failed is in the
// error, not here, except that a client whose re-add failed after its removal
// is listed under Removed, since the node has lost it.
type Diff struct {
	Added    []string // wanted, and the node had no such client
	Removed  []string // subscription clients nobody wants any more
	Replaced []string // the node's client had another UUID: removed and added again
	// Foreign are the emails on the entry that are not a subscription's: the
	// bridge's client, and anything else someone put there. They are never
	// touched, and the list is for the caller's log.
	Foreign []string
}

// Reconcile makes the entry's clients the ones in want. It lists what the node
// has, adds what is missing and removes the subscription clients that nobody
// wants; the clients that are right are not touched.
//
// It removes only emails that nodecfg.ParseSubscriptionEmail accepts. The
// bridge's client and anything else the node holds are reported in
// Diff.Foreign and left as they are, so a sweep can't cut the tunnel or take
// away something that was put there for another reason. For the same reason
// every email in want must be a subscription's.
//
// A client whose UUID differs from the wanted one is removed and added again,
// but only when the agent reports UUIDs. The real agent doesn't: its ListUsers
// sends the email and the level and nothing else, so with it a changed UUID
// goes unseen and a link is replaced by calling Replace. No UUID is guessed.
//
// One failure doesn't stop the rest. The error joins every failure, each one
// saying which email it was; a cancelled context stops the work and is
// reported once.
func Reconcile(ctx context.Context, a Agent, want []Client) (Diff, error) {
	wanted := make(map[string]bool, len(want))
	for _, c := range want {
		if err := c.check(); err != nil {
			return Diff{}, err
		}
		if wanted[c.Email] {
			return Diff{}, fmt.Errorf("subscriptions: %s is wanted twice", c.Email)
		}
		wanted[c.Email] = true
	}

	users, err := a.ListUsers(ctx, nodecfg.EntryTag)
	if err != nil {
		return Diff{}, fmt.Errorf("subscriptions: list the clients: %w", err)
	}
	have := make(map[string]*pb.UserInfo, len(users))
	for _, u := range users {
		if u != nil {
			have[u.GetEmail()] = u
		}
	}

	// Foreign and extra are known before anything is changed.
	var d Diff
	var extra []string
	for email := range have {
		if wanted[email] {
			continue
		}
		if _, ours := nodecfg.ParseSubscriptionEmail(email); ours {
			extra = append(extra, email)
		} else {
			d.Foreign = append(d.Foreign, email)
		}
	}
	slices.Sort(extra)
	slices.Sort(d.Foreign)

	var errs []error
	// A done context fails every call after it the same way: the first call it
	// breaks reports it, and if none has yet, it is reported here.
	cancelled := func() bool {
		err := ctx.Err()
		if err == nil {
			return false
		}
		if !errors.Is(errors.Join(errs...), err) {
			errs = append(errs, err)
		}
		return true
	}

	// Put back what is missing before taking away what is extra: the people
	// who should get through come first.
	for _, c := range want {
		u, there := have[c.Email]
		changed := there && u.GetUuid() != "" && !strings.EqualFold(u.GetUuid(), c.UUID)
		if there && !changed {
			continue
		}
		if cancelled() {
			return d, errors.Join(errs...)
		}
		if !there {
			if err := Enable(ctx, a, c); err != nil {
				errs = append(errs, err)
				continue
			}
			d.Added = append(d.Added, c.Email)
			continue
		}
		if err := Disable(ctx, a, c.Email); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := Enable(ctx, a, c); err != nil {
			errs = append(errs, err)
			d.Removed = append(d.Removed, c.Email) // the node has lost the client
			continue
		}
		d.Replaced = append(d.Replaced, c.Email)
	}

	for _, email := range extra {
		if cancelled() {
			break
		}
		if err := Disable(ctx, a, email); err != nil {
			errs = append(errs, err)
			continue
		}
		d.Removed = append(d.Removed, email)
	}
	return d, errors.Join(errs...)
}
