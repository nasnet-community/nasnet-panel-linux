// Package core defines the agent-side abstraction for non-Xray tunnel
// "cores" (SSH and MTProto).
//
// The hub drives each core declaratively: it pushes a DesiredState and the
// core reconciles reality to match (write config files, (re)load the daemon,
// refresh the in-memory credential set). This mirrors the whole-config push +
// reconcile model already used for Xray, and keeps zero new gRPC RPCs per
// protocol.
package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
)

// CoreType is the registry key identifying a protocol core.
type CoreType string

const (
	CoreTypeSSH     CoreType = "ssh"
	CoreTypeMTProto CoreType = "mtproto"
)

// EgressMode selects how a core's traffic leaves the node.
type EgressMode string

const (
	// EgressDirect: the core dials out itself (SSH through the filtered
	// egress dialer, MTProto through its loopback relay in router mode).
	EgressDirect EgressMode = "direct"
	// EgressXray: the core dials out through a loopback SOCKS inbound in the
	// node's Xray config, so Xray's routing and outbounds apply.
	EgressXray EgressMode = "xray"
)

// Either way the core counts its own users' traffic (in-process bytes /
// Prometheus scrape): Xray's SOCKS inbound doesn't know the user.

// UserSpec is one pushed credential for a core. The pushed set is the sole
// source of truth on the agent (which has no database).
type UserSpec struct {
	Username string
	Secret   string
	Email    string // routing + accounting identity
	KLimit   int    // max concurrent devices (0 => treat as 1)
	Enabled  bool   // false => deny new dials + sweep-kill live sessions

	// ExpiresAt is the subscription's expiry, pushed so a core that can enforce
	// it natively keeps holding the line while the node is out of hub contact.
	// Zero => no expiry. Cores without native expiry ignore it.
	ExpiresAt time.Time
	// RateLimitUpBps / RateLimitDownBps are the subscription's throughput cap.
	// 0 => uncapped (or the inbound-wide default, per core). Cores without
	// native shaping ignore them.
	RateLimitUpBps   int64
	RateLimitDownBps int64
}

// FileSpec is an auxiliary file (cert/key/aux config) the core must place on
// disk before (re)loading its daemon. RelPath is resolved under the core's data
// directory; absolute paths and traversal are rejected by the core.
type FileSpec struct {
	RelPath string
	Content []byte
	Mode    uint32
}

// DesiredState is the full declarative state for one core_inbound, pushed by
// the hub. Apply must make reality match this exactly and idempotently.
type DesiredState struct {
	CoreType   CoreType
	InboundID  string
	Enabled    bool
	Listen     string
	Port       int
	EgressMode EgressMode
	Settings   json.RawMessage // per-protocol JSON (mirrors core_inbounds.settings)
	Users      []UserSpec
	Files      []FileSpec
}

// CoreStatus is a point-in-time report for GetCoreStatus.
type CoreStatus struct {
	Running         bool
	Version         string
	LiveSessions    int
	DistinctDevices int
	Detail          string
	// InboundIDs are the inbounds this core currently runs.
	InboundIDs []string
}

// Core is one protocol implementation on the agent.
type Core interface {
	// Type returns the registry key for this core.
	Type() CoreType
	// Apply reconciles reality to the desired state. Idempotent.
	Apply(ctx context.Context, d DesiredState) error
	// Status reports the current runtime state.
	Status(ctx context.Context) (CoreStatus, error)
	// CollectTraffic returns a snapshot of per-email traffic since the last
	// call. May return nil when there is nothing to report.
	CollectTraffic(ctx context.Context) (*traffic.XrayStatsSnapshot, error)
	// Sweep enforces device-limit K and kills sessions whose pushed user is
	// disabled. Runs each traffic tick.
	Sweep(ctx context.Context, d DesiredState) error
}
