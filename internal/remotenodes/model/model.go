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
	SchemaVersion = 1

	// The caps stay hard-coded until the plugin has been measured on a real
	// router.
	MaxNodes         = 3
	MaxSubscriptions = 10

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
	// NodeInstalling is a node whose install has not finished. With no job
	// running for it the install died, and only removal works.
	NodeInstalling NodeState = "installing"
	NodeRegistered NodeState = "registered"
)

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
}

// Subscription is a named entry with one link per node it is switched on for.
type Subscription struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
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
// when the router already has MaxNodes.
func (s *State) AddNode(n Node) (Node, error) {
	if len(s.Nodes) >= MaxNodes {
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
	if len(s.Nodes) > MaxNodes && len(s.Nodes) > len(before.Nodes) {
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
