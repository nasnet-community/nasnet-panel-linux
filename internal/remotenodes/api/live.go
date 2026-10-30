// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "time"

// Live is what the manager knows that the state file doesn't: whether each
// node answers, the tunnel's state, the numbers of the last minutes and the
// router's entry gate. The API reads it for every view that has live parts and
// leaves what the model holds to the model. With none, the live parts read as
// "nothing is running", as they did before there was a manager.
//
// Every answer is a copy the caller may keep. None of them waits for a node or
// the disk.
type Live interface {
	// Entry is the router's entry gate.
	Entry() Entry
	// Xray is the one Xray version this plugin installs on nodes.
	Xray() string
	// NodeLive is what is known of a node right now. A node that nothing is
	// known of has the zero value.
	NodeLive(id uint) NodeLive
	// NodeEvents is the node's recent events, newest first. Never nil.
	NodeEvents(id uint) []NodeEvent
	// SubscriptionOn is what a subscription used on one node, and whether it
	// is connected there.
	SubscriptionOn(subID, nodeID uint) SubscriptionUsage
	// SubscriptionTotal is the same over every node.
	SubscriptionTotal(subID uint) SubscriptionUsage
	// Traffic is the node's chart for the last few minutes. Never nil samples.
	Traffic(nodeID uint) Traffic
}

// NodeLive is the live part of a node's view. The zero value is a node that
// nothing is known of: no agent, no numbers, and a tunnel that is off.
type NodeLive struct {
	LastSeenAt *time.Time
	Tunnel     TunnelState
	Agent      *AgentState
	Updates    Updates
	Usage      NodeUsage
	// Installing is set while the node's install job runs.
	Installing *Installing
}

// SubscriptionUsage is what a subscription used, on one node or over all of
// them, and whether it is connected.
type SubscriptionUsage struct {
	// Live is whether the node (for a total, some node) answered lately. When
	// it is false, Online and Connections are not known and read zero.
	Live        bool
	Online      bool
	Connections int
	LastSeenAt  *time.Time
	TodayUp     uint64
	TodayDown   uint64
	MonthUp     uint64
	MonthDown   uint64
}

// Traffic is a node's chart: the rates in bytes per second, a sample for each
// step the node was polled in. Down is what went to the subscriptions.
type Traffic struct {
	StepSec int             `json:"stepSec"`
	Samples []TrafficSample `json:"samples"`
}

type TrafficSample struct {
	At   time.Time `json:"at"`
	Up   uint64    `json:"up"`
	Down uint64    `json:"down"`
}

// NodeSubscription is one row of a node's list: a subscription, and how it is
// doing on that node. Every subscription has a row, switched on or not.
type NodeSubscription struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Online      *bool      `json:"online"` // null: switched off here, or the node can't be reached
	Connections *int       `json:"connections"`
	LastSeenAt  *time.Time `json:"lastSeenAt"`
	TodayUp     uint64     `json:"todayUp"`
	TodayDown   uint64     `json:"todayDown"`
	MonthUp     uint64     `json:"monthUp"`
	MonthDown   uint64     `json:"monthDown"`
}
