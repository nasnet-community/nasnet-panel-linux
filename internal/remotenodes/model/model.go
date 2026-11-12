// SPDX-License-Identifier: AGPL-3.0-or-later

// Package model is what the plugin keeps about its nodes and subscriptions.
//
// These types are the state file's form, so they carry secrets (the REALITY
// private key, client UUIDs, the pinned SSH host key). They never go to a
// browser as they are: the api package maps them to view types that leave the
// secrets out.
package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

const (
	// SchemaVersion is the state file's format. Bump it whenever a change
	// would make an older build misread the file.
	//
	// 2 added Local and LocalClients, the entry on the router itself. A file
	// of version 1 is read as one with that entry off.
	SchemaVersion = 2

	// The caps stay hard-coded until the plugin has been measured on a real
	// router.
	MaxNodes         = 3
	MaxSubscriptions = 10
	// MaxUnfinished is how many installs that stopped and couldn't be cleaned
	// up are kept beside the nodes. They don't count as nodes, and a new install
	// waits until one is cleaned up or forgotten.
	MaxUnfinished = 3

	// CertRenewBefore is how long before a node certificate runs out the hub
	// renews it.
	CertRenewBefore = pki.RenewBefore
)

var (
	ErrNodeLimit         = errors.New("node limit reached")
	ErrSubscriptionLimit = errors.New("subscription limit reached")
	ErrNameTaken         = errors.New("name already in use")
	ErrNotFound          = errors.New("not found")

	// ErrNewer means the file was written by a newer release. Reading it
	// would drop whatever that release added, so nothing is loaded.
	ErrNewer = errors.New("state was written by a newer version")
)

// NodeState is where a node is in its life. Whether it is up right now is
// live data and isn't kept here.
type NodeState string

const (
	// NodeInstalling is a node whose install has not finished. While its job
	// runs it is a node like the others; once the install has stopped (Failure
	// says why) it is an unfinished install, which only a cleanup or forgetting
	// it takes out.
	NodeInstalling NodeState = "installing"
	NodeRegistered NodeState = "registered"
)

// InstallFailure is why an install stopped before it was done, kept on the
// record it left so that the page can say so after the job is forgotten. The
// texts are the job's, with no secret in them.
type InstallFailure struct {
	// Step is the step it stopped at, by its English name and its key; empty
	// when that isn't known.
	Step    string `json:"step,omitempty"`
	StepKey string `json:"step_key,omitempty"`
	Error   string `json:"error"`
	// Cleanup is why taking it off the server failed too, empty when no
	// cleanup ran.
	Cleanup string    `json:"cleanup,omitempty"`
	At      time.Time `json:"at,omitzero"`
}

// unknownFailure is what a record says of an install that stopped with no
// job left to say why: one a restart cut short, or one from before installs
// cleaned up after themselves.
const unknownFailure = "The install did not finish, and the job that ran it is gone, so the hub can't say why."

// Node is one Linux server running nasnet-agent.
//
// Fields marked "hub only" are never shown in the web page.
type Node struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"` // an IP address or host name, in the form CleanHost returns
	SSHPort   int       `json:"ssh_port"`
	SSHUser   string    `json:"ssh_user,omitempty"` // only to prefill the login form; the login itself is never kept
	AgentPort int       `json:"agent_port"`
	EntryPort int       `json:"entry_port"`
	Arch      string    `json:"arch,omitempty"`
	State     NodeState `json:"state"`
	CreatedAt time.Time `json:"created_at"`

	// The REALITY side of the entry inbound.
	RealityTarget     string `json:"reality_target,omitempty"` // host:port the handshake is forwarded to
	RealitySNI        string `json:"reality_sni,omitempty"`
	RealityPublicKey  string `json:"reality_public_key,omitempty"`
	RealityShortID    string `json:"reality_short_id,omitempty"`
	RealityPrivateKey string `json:"reality_private_key,omitempty"` // hub only

	// What the node last reported. Written when it changes, not on every
	// heartbeat.
	AgentVersion string `json:"agent_version,omitempty"`
	XrayVersion  string `json:"xray_version,omitempty"`

	CertNotBefore time.Time `json:"cert_not_before,omitzero"`
	CertNotAfter  time.Time `json:"cert_not_after,omitzero"`

	// SSHHostKey is the SHA256 fingerprint pinned on the first connect. A
	// changed key stops and asks. Hub only.
	SSHHostKey string `json:"ssh_host_key,omitempty"`

	// BridgeUUID is the id the router's bridge logs in with on this node: the
	// one client of the entry inbound that carries the reverse tag. The
	// install makes it, and until then the node has no way into the bridge.
	// Hub only.
	BridgeUUID string `json:"bridge_uuid,omitempty"`

	// Failure is set on an install that stopped and could not be cleaned up,
	// which makes it an unfinished install and not a node. A value, so that
	// a copy of the state shares nothing.
	Failure InstallFailure `json:"install_failure,omitzero"`
}

// Unfinished says whether the record is an install that stopped before it
// was done and was kept because something of it may still be on the server.
// It is no node: it doesn't count towards MaxNodes, and nothing but a cleanup
// or forgetting it is done with it.
func (n Node) Unfinished() bool { return n.State == NodeInstalling && n.Failure.Error != "" }

// NodeCount is how many nodes there are against MaxNodes: the set-up ones and
// those whose install is running. Unfinished installs are left out.
func (s State) NodeCount() int {
	c := 0
	for _, n := range s.Nodes {
		if !n.Unfinished() {
			c++
		}
	}
	return c
}

// UnfinishedCount is how many unfinished installs there are.
func (s State) UnfinishedCount() int { return len(s.Nodes) - s.NodeCount() }

// settleInstalls makes every install that has no failure on record an
// unfinished one. It is for a state read from a file: no job outlives the hub
// that ran it, so an install the file holds as running stopped with that hub.
func (s *State) settleInstalls() {
	for i := range s.Nodes {
		if n := &s.Nodes[i]; n.State == NodeInstalling && n.Failure.Error == "" {
			n.Failure = InstallFailure{Error: unknownFailure}
		}
	}
}

// Subscription is a named entry with one link per node it is switched on for.
type Subscription struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`

	// TrafficLimit is how many bytes the subscription may move, both ways and
	// on every node, since the count was last reset. Zero means no limit.
	TrafficLimit uint64 `json:"traffic_limit,omitempty"`
	// TrafficBase is what the subscription had moved in all when its count was
	// last reset. What it has used is the total now, plus TrafficCarried, minus
	// this.
	TrafficBase uint64 `json:"traffic_base,omitempty"`
	// TrafficCarried is what it had used when the backup it was restored from
	// was made. A restored install counts from zero, so without it a limit
	// would start again from nothing.
	TrafficCarried uint64 `json:"traffic_carried,omitempty"`
	// ExpiresAt is when the subscription stops working. Zero means never.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// Stopped is why the hub took the subscription off the nodes: StopLimit or
	// StopExpired. Empty means it is running. The switches on the nodes are
	// left as they were, so lifting the reason puts the clients back.
	Stopped string `json:"stopped,omitempty"`
}

// Why a subscription was stopped.
const (
	StopLimit   = "limit"
	StopExpired = "expired"
)

// MaxTrafficLimit is the largest limit that can be set, 1 PiB. Far above what
// a router can carry, and low enough that sums of it can't wrap.
const MaxTrafficLimit = 1 << 50

// Running is whether the subscription's clients belong on the nodes.
func (s Subscription) Running() bool { return s.Stopped == "" }

// StopReason says why a subscription that has used `used` bytes since its last
// reset should not run at `now`, or "" if it should. The date wins when both
// are true, since it can't be undone by a bigger limit.
func (s Subscription) StopReason(used uint64, now time.Time) string {
	switch {
	case !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt):
		return StopExpired
	case s.TrafficLimit > 0 && used >= s.TrafficLimit:
		return StopLimit
	}
	return ""
}

// Enablement is a subscription's client on one node. Switching it off keeps
// the record, so switching it on again hands out the same UUID and the link
// doesn't change.
type Enablement struct {
	SubscriptionID uint   `json:"subscription_id"`
	NodeID         uint   `json:"node_id"`
	ClientUUID     string `json:"client_uuid"` // the VLESS credential. Hub only
	Enabled        bool   `json:"enabled"`
}

// Local is the entry on the router itself, "This router" on the page: an Xray
// inbound in the bridge that subscriptions connect to straight, with no node
// in between. It is not a node: nothing is installed for it and it has no
// agent, so it has a record of its own.
type Local struct {
	// Enabled is its switch. Off leaves the rest as it was, so switching it
	// on again gives every subscription the link it had.
	Enabled bool `json:"enabled"`
	// Host is the name or address its links carry when one was set by hand,
	// in the form CleanHost returns. Empty means the router's own name.
	Host string `json:"host,omitempty"`

	// The REALITY side, as on a node. The keys are made the first time it is
	// switched on; the target may come a little later, once one is chosen.
	RealityTarget     string `json:"reality_target,omitempty"` // host:port the handshake is forwarded to
	RealitySNI        string `json:"reality_sni,omitempty"`
	RealityPublicKey  string `json:"reality_public_key,omitempty"`
	RealityShortID    string `json:"reality_short_id,omitempty"`
	RealityPrivateKey string `json:"reality_private_key,omitempty"` // hub only
}

// HasKeys says whether the entry's REALITY keys have been made.
func (l Local) HasKeys() bool { return l.RealityPrivateKey != "" }

// LocalClient is a subscription's client on the router's own entry, what an
// Enablement is on a node. Switching it off keeps the record, so switching it
// on again hands out the same UUID and the link doesn't change.
type LocalClient struct {
	SubscriptionID uint   `json:"subscription_id"`
	ClientUUID     string `json:"client_uuid"` // the VLESS credential. Hub only
	Enabled        bool   `json:"enabled"`
}

// Settings are the defaults a new node starts from.
type Settings struct {
	AgentPort int `json:"agent_port"`
	EntryPort int `json:"entry_port"`
}

// DefaultSettings uses the agent's own listen port and the usual HTTPS port.
func DefaultSettings() Settings { return Settings{AgentPort: 9090, EntryPort: 443} }

// State is everything the plugin keeps across a restart.
//
// The id counters only go up, so a removed node's id (which is in its
// certificate) never comes back for another node.
type State struct {
	Version            int       `json:"version"`
	NextNodeID         uint      `json:"next_node_id"`
	NextSubscriptionID uint      `json:"next_subscription_id"`
	Settings           Settings  `json:"settings"`
	LastBackupAt       time.Time `json:"last_backup_at,omitzero"` // when a backup was last exported

	// The CA every node trusts, as PEM. The hub makes it the first time it
	// needs one, so a fresh state has neither half. Hub only: the key never
	// reaches the page, and losing it means enrolling every node again.
	CACert string `json:"ca_cert,omitempty"`
	CAKey  string `json:"ca_key,omitempty"`

	Nodes         []Node         `json:"nodes"`
	Subscriptions []Subscription `json:"subscriptions"`
	Enablements   []Enablement   `json:"enablements"`

	// The entry on the router itself, and the subscriptions' clients on it.
	Local        Local         `json:"local"`
	LocalClients []LocalClient `json:"local_clients"`
}

// NewState is what a fresh install starts with.
func NewState() State {
	return State{Version: SchemaVersion, NextNodeID: 1, NextSubscriptionID: 1, Settings: DefaultSettings()}
}

// Clone copies the slices. The records hold only values, so that is a deep
// copy (a test keeps it that way).
func (s State) Clone() State {
	s.Nodes = slices.Clone(s.Nodes)
	s.Subscriptions = slices.Clone(s.Subscriptions)
	s.Enablements = slices.Clone(s.Enablements)
	s.LocalClients = slices.Clone(s.LocalClients)
	return s
}

// FindNode points into s.Nodes, so it is good only until the slice changes.
func (s *State) FindNode(id uint) *Node {
	if i := slices.IndexFunc(s.Nodes, func(n Node) bool { return n.ID == id }); i >= 0 {
		return &s.Nodes[i]
	}
	return nil
}

func (s *State) FindSubscription(id uint) *Subscription {
	if i := slices.IndexFunc(s.Subscriptions, func(x Subscription) bool { return x.ID == id }); i >= 0 {
		return &s.Subscriptions[i]
	}
	return nil
}

// FindLocalClient is the subscription's client on the router's own entry.
func (s *State) FindLocalClient(subscriptionID uint) *LocalClient {
	if i := slices.IndexFunc(s.LocalClients, func(c LocalClient) bool { return c.SubscriptionID == subscriptionID }); i >= 0 {
		return &s.LocalClients[i]
	}
	return nil
}

func (s *State) FindEnablement(subscriptionID, nodeID uint) *Enablement {
	i := slices.IndexFunc(s.Enablements, func(e Enablement) bool {
		return e.SubscriptionID == subscriptionID && e.NodeID == nodeID
	})
	if i < 0 {
		return nil
	}
	return &s.Enablements[i]
}

// AddNode gives n the next id and appends it, or fails with ErrNodeLimit
// when the router already has MaxNodes. Unfinished installs don't count.
func (s *State) AddNode(n Node) (Node, error) {
	if s.NodeCount() >= MaxNodes {
		return Node{}, fmt.Errorf("%w: at most %d", ErrNodeLimit, MaxNodes)
	}
	n.ID = s.NextNodeID
	n.CreatedAt = n.CreatedAt.UTC().Truncate(time.Second)
	if err := n.Validate(); err != nil {
		return Node{}, err
	}
	s.NextNodeID++
	s.Nodes = append(s.Nodes, n)
	return n, nil
}

// AddSubscription gives sub the next id and appends it, or fails with
// ErrSubscriptionLimit or ErrNameTaken.
func (s *State) AddSubscription(sub Subscription) (Subscription, error) {
	if len(s.Subscriptions) >= MaxSubscriptions {
		return Subscription{}, fmt.Errorf("%w: at most %d", ErrSubscriptionLimit, MaxSubscriptions)
	}
	sub.ID = s.NextSubscriptionID
	sub.CreatedAt = sub.CreatedAt.UTC().Truncate(time.Second)
	if err := sub.Validate(); err != nil {
		return Subscription{}, err
	}
	if s.nameTaken(sub.Name, 0) {
		return Subscription{}, fmt.Errorf("%w: %q", ErrNameTaken, sub.Name)
	}
	s.NextSubscriptionID++
	s.Subscriptions = append(s.Subscriptions, sub)
	return sub, nil
}

// RemoveNode drops the node and the clients it held.
func (s *State) RemoveNode(id uint) error {
	i := slices.IndexFunc(s.Nodes, func(n Node) bool { return n.ID == id })
	if i < 0 {
		return fmt.Errorf("%w: node %d", ErrNotFound, id)
	}
	s.Nodes = slices.Delete(s.Nodes, i, i+1)
	s.Enablements = slices.DeleteFunc(s.Enablements, func(e Enablement) bool { return e.NodeID == id })
	return nil
}

// RemoveSubscription drops the subscription and its clients.
func (s *State) RemoveSubscription(id uint) error {
	i := slices.IndexFunc(s.Subscriptions, func(x Subscription) bool { return x.ID == id })
	if i < 0 {
		return fmt.Errorf("%w: subscription %d", ErrNotFound, id)
	}
	s.Subscriptions = slices.Delete(s.Subscriptions, i, i+1)
	s.Enablements = slices.DeleteFunc(s.Enablements, func(e Enablement) bool { return e.SubscriptionID == id })
	s.LocalClients = slices.DeleteFunc(s.LocalClients, func(c LocalClient) bool { return c.SubscriptionID == id })
	return nil
}

// nameTaken compares the way a person would: "Ali" and "ali" are one name.
// except is a subscription to leave out, so a rename can keep its own name.
func (s State) nameTaken(name string, except uint) bool {
	return slices.ContainsFunc(s.Subscriptions, func(x Subscription) bool {
		return x.ID != except && strings.EqualFold(x.Name, name)
	})
}

// CheckEdit is what the store runs on a state after an edit. The result must
// be valid on its own and, compared with the state before, must not grow past
// a cap, turn an id counter back or lose the CA.
//
// Growth is what the caps refuse, so a state that already holds more (a past
// release allowed it, or the file was edited) can still shrink and be
// renamed.
func (s State) CheckEdit(before State) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if c := s.NodeCount(); c > MaxNodes && c > before.NodeCount() {
		return fmt.Errorf("%w: at most %d", ErrNodeLimit, MaxNodes)
	}
	if len(s.Subscriptions) > MaxSubscriptions && len(s.Subscriptions) > len(before.Subscriptions) {
		return fmt.Errorf("%w: at most %d", ErrSubscriptionLimit, MaxSubscriptions)
	}
	if s.NextNodeID < before.NextNodeID || s.NextSubscriptionID < before.NextSubscriptionID {
		return invalid("an id counter went backwards, which would reuse an id")
	}
	// another CA can take its place (a backup import does), but none at all
	// would leave every node trusting a CA the hub can no longer sign with
	if before.CACert != "" && s.CACert == "" {
		return invalid("an edit dropped the CA, which every node trusts")
	}
	return nil
}
