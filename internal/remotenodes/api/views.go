// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"math"
	"net"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// These are the shapes the page reads. Each is filled field by field from the
// model, never embedded, so a new field in the model (a key, a login) can't
// reach the browser by being added there.
//
// The live parts (tunnel, agent, usage, events, installing, updates and the
// router's entry gate) stay empty until the manager fills them. Where a field
// takes one of a few words, they carry the word for "nothing running".

const (
	linkMissing   = "missing"
	bridgeStopped = "stopped"
	tunnelOff     = "off"
)

type AppConfig struct {
	Version           string `json:"version"`
	Commit            string `json:"commit"`
	MaxNodes          int    `json:"maxNodes"`
	NodeCount         int    `json:"nodeCount"`
	MaxSubscriptions  int    `json:"maxSubscriptions"`
	SubscriptionCount int    `json:"subscriptionCount"`
	Entry             Entry  `json:"entry"`
	Backup            Backup `json:"backup"`
	Xray              string `json:"xray"` // the one Xray version this plugin installs
}

type Backup struct {
	LastExportAt *time.Time `json:"lastExportAt"`
}

type Entry struct {
	On        bool       `json:"on"`
	Domestic  string     `json:"domestic"` // online, offline or missing
	VPN       VPN        `json:"vpn"`
	Bridge    string     `json:"bridge"` // running, waiting or stopped
	RouterOS  RouterOS   `json:"routerOS"`
	Routes    []Route    `json:"routes"`
	CheckedAt *time.Time `json:"checkedAt"`
	Stale     bool       `json:"stale"`
}

type VPN struct {
	State string `json:"state"` // online, offline or missing
	Kind  string `json:"kind"`  // wireguard, l2tp, openvpn, other or ""
	Label string `json:"label"`
}

type RouterOS struct {
	Version string `json:"version"`
	Min     string `json:"min"`
	OK      bool   `json:"ok"`
}

type Route struct {
	Address string `json:"address"`
	UsedFor string `json:"usedFor"` // downloads, entry-exit or nodes
	Via     string `json:"via"`     // foreign, vpn or domestic
	Ready   bool   `json:"ready"`
}

type Node struct {
	ID                uint        `json:"id"`
	Name              string      `json:"name"`
	Host              string      `json:"host"`
	SSHPort           int         `json:"sshPort"`
	SSHUser           string      `json:"sshUser"`
	Registered        bool        `json:"registered"` // false: the install died before it was saved, and only removal works
	CreatedAt         time.Time   `json:"createdAt"`
	EntryPort         int         `json:"entryPort"`
	HandshakeTarget   string      `json:"handshakeTarget"` // host:port the REALITY handshake borrows
	SNI               string      `json:"sni"`
	SNICustom         bool        `json:"sniCustom"` // false: the same as the handshake target's host
	PublicKey         string      `json:"publicKey"`
	ShortID           string      `json:"shortId"`
	Arch              string      `json:"arch"`
	AgentVersion      string      `json:"agentVersion"`
	XrayVersion       string      `json:"xrayVersion"` // as the node reports it
	LastSeenAt        *time.Time  `json:"lastSeenAt"`
	SubscriptionCount int         `json:"subscriptionCount"` // enabled on this node
	Tunnel            TunnelState `json:"tunnel"`
	Agent             *AgentState `json:"agent"`
	Cert              Cert        `json:"cert"`
	Updates           Updates     `json:"updates"`
	Usage             NodeUsage   `json:"usage"`
	Events            []NodeEvent `json:"events,omitempty"`
	Installing        *Installing `json:"installing,omitempty"` // present while the install job runs
}

type TunnelState struct {
	State         string     `json:"state"` // up, down or off
	Since         *time.Time `json:"since"`
	RTTMs         int        `json:"rttMs"`
	Streams       int        `json:"streams"`
	Carried       uint64     `json:"carried"`
	BytesToSubs   uint64     `json:"bytesToSubs"`
	BytesFromSubs uint64     `json:"bytesFromSubs"`
	DialFailures  int        `json:"dialFailures"`
	Reason        string     `json:"reason"` // "", xray_mismatch, entry_off, bridge_down or agent_down
}

type AgentState struct {
	Version         string    `json:"version"`
	XrayVersion     string    `json:"xrayVersion"`
	UptimeSec       int64     `json:"uptimeSec"`
	RSSBytes        uint64    `json:"rssBytes"`
	ReportedAt      time.Time `json:"reportedAt"`
	Xray            XrayState `json:"xray"`
	SettingsApplied bool      `json:"settingsApplied"`
	LastPushError   string    `json:"lastPushError"`
}

type XrayState struct {
	Running   bool   `json:"running"`
	Restarts  int    `json:"restarts"`
	UptimeSec int64  `json:"uptimeSec"`
	LastExit  string `json:"lastExit"`
	CrashLoop bool   `json:"crashLoop"`
}

type Cert struct {
	NotAfter  string `json:"notAfter"`
	ValidDays int    `json:"validDays"` // whole days left, negative once it has run out
	RenewsAt  string `json:"renewsAt"`
}

type Updates struct {
	Agent        bool   `json:"agent"`
	Xray         bool   `json:"xray"`
	AgentVersion string `json:"agentVersion"`
	XrayVersion  string `json:"xrayVersion"`
}

type NodeUsage struct {
	Live        bool       `json:"live"`
	Online      int        `json:"online"`
	Connections int        `json:"connections"`
	RateUp      uint64     `json:"rateUp"` // bytes per second
	RateDown    uint64     `json:"rateDown"`
	TodayUp     uint64     `json:"todayUp"`
	TodayDown   uint64     `json:"todayDown"`
	MonthUp     uint64     `json:"monthUp"`
	MonthDown   uint64     `json:"monthDown"`
	UpdatedAt   *time.Time `json:"updatedAt"`
}

type NodeEvent struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail"`
}

type Installing struct {
	JobID   string `json:"jobId"`
	Step    int    `json:"step"`
	Steps   int    `json:"steps"`
	Label   string `json:"label"`
	Percent *int   `json:"percent"`
}

type Subscription struct {
	ID         uint               `json:"id"`
	Name       string             `json:"name"`
	CreatedAt  time.Time          `json:"createdAt"`
	Online     *bool              `json:"online"` // null: unknown
	LastSeenAt *time.Time         `json:"lastSeenAt"`
	TodayUp    uint64             `json:"todayUp"`
	TodayDown  uint64             `json:"todayDown"`
	MonthUp    uint64             `json:"monthUp"`
	MonthDown  uint64             `json:"monthDown"`
	Nodes      []SubscriptionNode `json:"nodes"` // one per registered node
}

type SubscriptionNode struct {
	NodeID      uint       `json:"nodeId"`
	Enabled     bool       `json:"enabled"`
	Online      *bool      `json:"online"` // null: the node can't be reached, so unknown
	Connections *int       `json:"connections"`
	LastSeenAt  *time.Time `json:"lastSeenAt"`
	TodayUp     uint64     `json:"todayUp"`
	TodayDown   uint64     `json:"todayDown"`
	MonthUp     uint64     `json:"monthUp"`
	MonthDown   uint64     `json:"monthDown"`
}

func configView(cfg Config, st model.State) AppConfig {
	return AppConfig{
		Version:           cfg.Version,
		Commit:            cfg.Commit,
		MaxNodes:          model.MaxNodes,
		NodeCount:         len(st.Nodes),
		MaxSubscriptions:  model.MaxSubscriptions,
		SubscriptionCount: len(st.Subscriptions),
		Entry:             Entry{Domestic: linkMissing, VPN: VPN{State: linkMissing}, Bridge: bridgeStopped, Routes: []Route{}},
		Backup:            Backup{LastExportAt: utc(st.LastBackupAt)},
		// Xray stays empty until the build carries the version it installs
	}
}

func nodeViews(st model.State, now time.Time) []Node {
	on := map[uint]int{}
	for _, e := range st.Enablements {
		if e.Enabled {
			on[e.NodeID]++
		}
	}
	out := make([]Node, 0, len(st.Nodes))
	for _, n := range st.Nodes {
		out = append(out, Node{
			ID:                n.ID,
			Name:              n.Name,
			Host:              n.Host,
			SSHPort:           n.SSHPort,
			SSHUser:           n.SSHUser,
			Registered:        n.State == model.NodeRegistered,
			CreatedAt:         n.CreatedAt.UTC(),
			EntryPort:         n.EntryPort,
			HandshakeTarget:   n.RealityTarget,
			SNI:               n.RealitySNI,
			SNICustom:         n.RealitySNI != "" && n.RealitySNI != hostOf(n.RealityTarget),
			PublicKey:         n.RealityPublicKey,
			ShortID:           n.RealityShortID,
			Arch:              n.Arch,
			AgentVersion:      n.AgentVersion,
			XrayVersion:       n.XrayVersion,
			SubscriptionCount: on[n.ID],
			Tunnel:            TunnelState{State: tunnelOff},
			Cert:              certView(n, now),
		})
	}
	return out
}

func subscriptionViews(st model.State) []Subscription {
	out := make([]Subscription, 0, len(st.Subscriptions))
	for _, sub := range st.Subscriptions {
		v := Subscription{ID: sub.ID, Name: sub.Name, CreatedAt: sub.CreatedAt.UTC(), Nodes: []SubscriptionNode{}}
		for _, n := range st.Nodes {
			if n.State != model.NodeRegistered {
				continue
			}
			e := st.FindEnablement(sub.ID, n.ID)
			v.Nodes = append(v.Nodes, SubscriptionNode{NodeID: n.ID, Enabled: e != nil && e.Enabled})
		}
		out = append(out, v)
	}
	return out
}

func certView(n model.Node, now time.Time) Cert {
	if n.CertNotAfter.IsZero() {
		return Cert{}
	}
	return Cert{
		NotAfter:  n.CertNotAfter.UTC().Format(time.RFC3339),
		ValidDays: int(math.Floor(n.CertNotAfter.Sub(now).Hours() / 24)),
		RenewsAt:  n.CertNotAfter.Add(-model.CertRenewBefore).UTC().Format(time.RFC3339),
	}
}

func hostOf(target string) string {
	h, _, err := net.SplitHostPort(target)
	if err != nil {
		return ""
	}
	return h
}
