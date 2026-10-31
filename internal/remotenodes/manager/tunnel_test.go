// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// Every row is a situation of one node, and what the page should read.
func TestTunnelStateTable(t *testing.T) {
	since := epoch.Add(-time.Hour)
	carried := func(up bool) *bridgespec.NodeStatus {
		return &bridgespec.NodeStatus{Tag: "node-1", Up: up, Since: since, Connections: 4, BytesUp: 100, BytesDown: 900}
	}
	// a node that is carried by a running bridge, as every row starts from
	base := tunnelIn{registered: true, entryOn: true, xray: xrayOK, ready: true, applied: true, bridgeUp: true, node: carried(true)}
	edit := func(f func(*tunnelIn)) tunnelIn { in := base; f(&in); return in }

	for name, c := range map[string]struct {
		in     tunnelIn
		state  string
		reason string
		carry  bool // the tunnel's own numbers are given
	}{
		"up": {base, "up", "", true},
		"up, and its agent has stopped answering": {edit(func(in *tunnelIn) { in.agentDown = true }), "up", "", true},
		"the bridge says it is down":              {edit(func(in *tunnelIn) { in.node = carried(false) }), "down", "", true},
		"down, and the agent is out of reach":     {edit(func(in *tunnelIn) { in.node = carried(false); in.agentDown = true }), "down", "agent_down", true},

		"the install has not finished":           {edit(func(in *tunnelIn) { in.registered = false }), "off", "", false},
		"not installed, and the entry is off":    {edit(func(in *tunnelIn) { in.registered = false; in.entryOn = false }), "off", "", false},
		"the entry is off":                       {edit(func(in *tunnelIn) { in.entryOn = false; in.applied = false; in.bridgeUp = false; in.node = nil }), "off", "entry_off", false},
		"the entry is off while it still runs":   {edit(func(in *tunnelIn) { in.entryOn = false }), "off", "entry_off", false},
		"the entry is off, the node unreachable": {edit(func(in *tunnelIn) { in.entryOn = false; in.agentDown = true }), "off", "entry_off", false},
		"the entry is off and its Xray differs":  {edit(func(in *tunnelIn) { in.entryOn = false; in.xray = xrayMismatch }), "off", "entry_off", false},
		"its Xray differs":                       {edit(func(in *tunnelIn) { in.xray = xrayMismatch; in.ready = false; in.applied = false; in.node = nil }), "off", "xray_mismatch", false},
		"its Xray differs and it is unreachable": {edit(func(in *tunnelIn) { in.xray = xrayMismatch; in.ready = false; in.agentDown = true }), "off", "xray_mismatch", false},

		"ready, but the bridge is not running":  {edit(func(in *tunnelIn) { in.bridgeUp = false; in.node = nil }), "down", "bridge_down", false},
		"ready, but the bridge doesn't have it": {edit(func(in *tunnelIn) { in.applied = false; in.node = nil }), "down", "bridge_down", false},
		"ready, but its page doesn't list it":   {edit(func(in *tunnelIn) { in.node = nil }), "down", "bridge_down", false},
		"the bridge is down and the node too":   {edit(func(in *tunnelIn) { in.bridgeUp = false; in.node = nil; in.agentDown = true }), "down", "agent_down", false},
		"not ready (nothing to say why)":        {edit(func(in *tunnelIn) { in.ready = false; in.applied = false; in.node = nil }), "down", "", false},
		"not ready, and the agent is out":       {edit(func(in *tunnelIn) { in.ready = false; in.applied = false; in.node = nil; in.agentDown = true }), "down", "agent_down", false},
		"its Xray is not known yet":             {edit(func(in *tunnelIn) { in.xray = xrayUnknown; in.ready = false; in.applied = false; in.node = nil }), "down", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := deriveTunnel(c.in)
			if got.State != c.state || got.Reason != c.reason {
				t.Fatalf("tunnel = %s %q, want %s %q", got.State, got.Reason, c.state, c.reason)
			}
			if c.carry {
				if got.Streams != 4 || got.BytesToSubs != 900 || got.BytesFromSubs != 100 || got.Since == nil || !got.Since.Equal(since) {
					t.Errorf("tunnel numbers = %+v", got)
				}
			} else if got.Streams != 0 || got.BytesToSubs != 0 || got.BytesFromSubs != 0 || got.Since != nil {
				t.Errorf("a tunnel that isn't carried by the bridge has numbers: %+v", got)
			}
			// what the bridge's page has no word for stays zero
			if got.RTTMs != 0 || got.Carried != 0 || got.DialFailures != 0 {
				t.Errorf("fields with no source are not zero: %+v", got)
			}
		})
	}
}

func TestATunnelThatWentDownSaysSinceWhen(t *testing.T) {
	st := &bridgespec.NodeStatus{Up: false, Since: epoch}
	got := deriveTunnel(tunnelIn{registered: true, entryOn: true, xray: xrayOK, ready: true, applied: true, bridgeUp: true, node: st})
	if got.State != "down" || got.Since == nil || !got.Since.Equal(epoch) {
		t.Errorf("tunnel = %+v", got)
	}
	// a bridge that has not been up yet has no moment to give
	got = deriveTunnel(tunnelIn{registered: true, entryOn: true, xray: xrayOK, ready: true, applied: true, bridgeUp: true, node: &bridgespec.NodeStatus{}})
	if got.Since != nil {
		t.Errorf("a zero time was given: %v", got.Since)
	}
}
