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
// router's entry gate) come from Config.Live, field by field in the same way.
// With none they stay empty, and where a field takes one of a few words they
// carry the word for "nothing running".

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
	// Local is the router's own entry, in brief.
	Local LocalBrief `json:"local"`
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
	Registered        bool        `json:"registered"` // false: installing, or an install that didn't finish
	CreatedAt         time.Time   `json:"createdAt"`
	EntryPort         int         `json:"entryPort"`
	HandshakeTarget   string      `json:"handshakeTarget"` // host:port the REALITY handshake borrows
	SNI               string      `json:"sni"`
	SNICustom         bool        `json:"sniCustom"` // false: the same as the Reality target's host
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
	// Events is only in the view of one node. A pointer, so that a node with
	// none says "events": [] and a list of nodes leaves the key out.
	Events     *[]NodeEvent `json:"events,omitempty"`
	Installing *Installing  `json:"installing,omitempty"` // present while the install job runs
	// Unfinished is present for an install that stopped and was not cleaned
	// up: it is no node, and only a cleanup or forgetting it works.
	Unfinished *Unfinished `json:"unfinished,omitempty"`
}

// Unfinished is why an install stopped: the step, by the key the page words
// it by and by its English name, the error, and why the cleanup after it
// failed too ("" when none ran).
type Unfinished struct {
	StepKey string     `json:"stepKey"`
	Step    string     `json:"step"`
	Error   string     `json:"error"`
	Cleanup string     `json:"cleanup"`
	At      *time.Time `json:"at"`
}

// stoppedInstall is what a view says of an install that has no job and no
// failure on record, which only a job that ended without saying why leaves.
const stoppedInstall = "The install stopped before it finished, and the hub can't say why."

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

// Installing is how far an install has got: the step it is on, by the key the
// page words it by and by its English name, which is the label.
type Installing struct {
	JobID   string `json:"jobId"`
	Step    int    `json:"step"`
	Steps   int    `json:"steps"`
	Key     string `json:"key"`
	Label   string `json:"label"`
	Percent *int   `json:"percent"`
}

type Subscription struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	// TrafficLimit is how many bytes it may move before it stops, both ways on
	// every node; 0 is no limit. TrafficUsed is what it has moved since its
	// count was last reset.
	TrafficLimit uint64 `json:"trafficLimit"`
	TrafficUsed  uint64 `json:"trafficUsed"`
	// ExpiresAt is when it stops; null is never.
	ExpiresAt *time.Time `json:"expiresAt"`
	// Stopped is why the hub has taken it off the nodes: "limit" or "expired".
	// Empty means it is running. Its switches on the nodes stay as they were.
	Stopped    string             `json:"stopped"`
	Online     *bool              `json:"online"` // null: unknown
	LastSeenAt *time.Time         `json:"lastSeenAt"`
	TodayUp    uint64             `json:"todayUp"`
	TodayDown  uint64             `json:"todayDown"`
	MonthUp    uint64             `json:"monthUp"`
	MonthDown  uint64             `json:"monthDown"`
	Nodes      []SubscriptionNode `json:"nodes"` // one per registered node
	// Local is the subscription on the router's own entry, null while that
	// entry is switched off.
	Local *SubscriptionLocal `json:"local"`
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
	v := AppConfig{
		Version:           cfg.Version,
		Commit:            cfg.Commit,
		MaxNodes:          model.MaxNodes,
		NodeCount:         st.NodeCount(), // an unfinished install is no node
		MaxSubscriptions:  model.MaxSubscriptions,
		SubscriptionCount: len(st.Subscriptions),
		Entry:             Entry{Domestic: linkMissing, VPN: VPN{State: linkMissing}, Bridge: bridgeStopped, Routes: []Route{}},
		Backup:            Backup{LastExportAt: utc(st.LastBackupAt)},
		// Xray stays empty without a manager to say which version it installs
	}
	if cfg.Live != nil {
		v.Entry = cfg.Live.Entry()
		if v.Entry.Routes == nil {
			v.Entry.Routes = []Route{} // JSON gets [], never null
		}
		v.Xray = cfg.Live.Xray()
	}
	v.Local = localBrief(st, cfg.Live)
	return v
}

func nodeViews(st model.State, now time.Time, live Live) []Node {
	on := enabledCounts(st)
	out := make([]Node, 0, len(st.Nodes))
	for _, n := range st.Nodes {
		out = append(out, nodeView(n, on[n.ID], now, live))
	}
	return out
}

// enabledCounts is how many subscriptions are switched on at each node.
func enabledCounts(st model.State) map[uint]int {
	on := map[uint]int{}
	for _, e := range st.Enablements {
		if e.Enabled {
			on[e.NodeID]++
		}
	}
	return on
}

func nodeView(n model.Node, enabled int, now time.Time, live Live) Node {
	v := Node{
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
		SubscriptionCount: enabled,
		Tunnel:            TunnelState{State: tunnelOff},
		Cert:              certView(n, now),
	}
	if live != nil {
		l := live.NodeLive(n.ID)
		v.LastSeenAt, v.Agent, v.Updates, v.Usage, v.Installing = l.LastSeenAt, l.Agent, l.Updates, l.Usage, l.Installing
		if l.Tunnel.State != "" {
			v.Tunnel = l.Tunnel
		}
	}
	switch {
	case n.Unfinished():
		f := n.Failure
		v.Unfinished = &Unfinished{StepKey: f.StepKey, Step: f.Step, Error: f.Error, Cleanup: f.Cleanup, At: utc(f.At)}
		v.Installing = nil
	case n.State == model.NodeInstalling && v.Installing == nil && live != nil:
		// installing with no job running: the job ended without settling it
		v.Unfinished = &Unfinished{Error: stoppedInstall}
	}
	return v
}

// expiry is a subscription's end date for a view: null when it has none.
func expiry(sub model.Subscription) *time.Time {
	if sub.ExpiresAt.IsZero() {
		return nil
	}
	at := sub.ExpiresAt.UTC()
	return &at
}

// TrafficUsed is what a subscription has moved since its count was last reset,
// given everything it has moved in all on this install. What it brought from
// a backup counts as moved here.
func TrafficUsed(sub model.Subscription, lifetime uint64) uint64 {
	total := lifetime + sub.TrafficCarried
	if total < lifetime {
		total = ^uint64(0)
	}
	if total <= sub.TrafficBase {
		return 0
	}
	return total - sub.TrafficBase
}

func subscriptionViews(st model.State, live Live) []Subscription {
	out := make([]Subscription, 0, len(st.Subscriptions))
	for _, sub := range st.Subscriptions {
		out = append(out, subscriptionView(st, sub, live))
	}
	return out
}

// ViewSubscription is one subscription as the list shows it, live parts and
// all, for the code that changes one and has to answer with the result. It
// reports false for an id the state doesn't hold.
func ViewSubscription(st model.State, id uint, live Live) (Subscription, bool) {
	sub := st.FindSubscription(id)
	if sub == nil {
		return Subscription{}, false
	}
	return subscriptionView(st, *sub, live), true
}

func subscriptionView(st model.State, sub model.Subscription, live Live) Subscription {
	v := Subscription{
		ID: sub.ID, Name: sub.Name, CreatedAt: sub.CreatedAt.UTC(), Nodes: []SubscriptionNode{},
		TrafficLimit: sub.TrafficLimit, Stopped: sub.Stopped,
	}
	v.ExpiresAt = expiry(sub)
	// Where it is switched on, a node that answers says whether it is
	// connected; one that doesn't leaves it unknown.
	connected, unknown := false, false
	for _, n := range st.Nodes {
		if n.State != model.NodeRegistered {
			continue
		}
		e := st.FindEnablement(sub.ID, n.ID)
		row := SubscriptionNode{NodeID: n.ID, Enabled: e != nil && e.Enabled}
		if live != nil {
			u := live.SubscriptionOn(sub.ID, n.ID)
			row.LastSeenAt = u.LastSeenAt
			row.TodayUp, row.TodayDown, row.MonthUp, row.MonthDown = u.TodayUp, u.TodayDown, u.MonthUp, u.MonthDown
			if row.Enabled {
				if u.Live {
					row.Online, row.Connections = &u.Online, &u.Connections
					connected = connected || u.Online
				} else {
					unknown = true
				}
			}
		}
		v.Nodes = append(v.Nodes, row)
	}
	if st.Local.Enabled {
		row, on, unsure := subscriptionLocalView(st, sub, live)
		v.Local = &row
		connected, unknown = connected || on, unknown || unsure
	}
	if live != nil {
		v.TrafficUsed = TrafficUsed(sub, live.SubscriptionLifetime(sub.ID))
		t := live.SubscriptionTotal(sub.ID)
		v.LastSeenAt = t.LastSeenAt
		v.TodayUp, v.TodayDown, v.MonthUp, v.MonthDown = t.TodayUp, t.TodayDown, t.MonthUp, t.MonthDown
		if connected || !unknown {
			v.Online = &connected
		}
	}
	return v
}

// nodeSubscriptionViews is a node's list: every subscription, switched on
// there or not.
func nodeSubscriptionViews(st model.State, nodeID uint, live Live) []NodeSubscription {
	out := make([]NodeSubscription, 0, len(st.Subscriptions))
	for _, sub := range st.Subscriptions {
		e := st.FindEnablement(sub.ID, nodeID)
		row := NodeSubscription{ID: sub.ID, Name: sub.Name, Enabled: e != nil && e.Enabled, Stopped: sub.Stopped, TrafficLimit: sub.TrafficLimit, ExpiresAt: expiry(sub)}
		if live != nil {
			row.TrafficUsed = TrafficUsed(sub, live.SubscriptionLifetime(sub.ID))
			u := live.SubscriptionOn(sub.ID, nodeID)
			row.LastSeenAt = u.LastSeenAt
			row.TodayUp, row.TodayDown, row.MonthUp, row.MonthDown = u.TodayUp, u.TodayDown, u.MonthUp, u.MonthDown
			if row.Enabled && u.Live {
				row.Online, row.Connections = &u.Online, &u.Connections
			}
		}
		out = append(out, row)
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
