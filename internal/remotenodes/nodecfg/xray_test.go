// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"encoding/json"
	"fmt"
	"testing"

	// What a node's Xray registers for this config. Not main/distro/all: its
	// JSON loader drags in modules the panel doesn't otherwise need.
	_ "github.com/xtls/xray-core/app/commander"
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/dns"
	_ "github.com/xtls/xray-core/app/geodata"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/log/command"
	_ "github.com/xtls/xray-core/app/policy"
	_ "github.com/xtls/xray-core/app/proxyman/command"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/app/router"
	_ "github.com/xtls/xray-core/app/stats"
	_ "github.com/xtls/xray-core/app/stats/command"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/dokodemo"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// load does what `xray -test` does: read the JSON through the config loader
// the binary uses (infra/conf) and build the instance, without binding
// anything. A config Xray would refuse fails here, not when the node restarts.
func load(t *testing.T, raw []byte) *core.Instance {
	t.Helper()
	var parsed conf.Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("Xray can't read the config: %v", err)
	}
	cfg, err := parsed.Build()
	if err != nil {
		t.Fatalf("Xray refuses the config: %v", err)
	}
	inst, err := core.New(cfg)
	if err != nil {
		t.Fatalf("Xray can't build the config: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst
}

func TestXrayAcceptsTheConfig(t *testing.T) {
	cases := map[string]func(*nodecfg.Input){
		"sample": func(*nodecfg.Input) {},
		"embedded domestic list": func(in *nodecfg.Input) {
			in.DomesticCIDRs = nil
		},
		"bridge only": func(in *nodecfg.Input) {
			in.Clients = nil
		},
		"ten subscriptions": func(in *nodecfg.Input) {
			in.Clients = nil
			for i := 1; i <= 10; i++ {
				in.Clients = append(in.Clients, nodecfg.Client{
					UUID:  fmt.Sprintf("2f8e3f0b-3a38-4d27-9b0e-5c1d6a7e%04x", i),
					Email: nodecfg.SubscriptionEmail(i),
				})
			}
		},
		"ipv6 dest and debug log": func(in *nodecfg.Input) {
			in.RealityDest = "[2001:db8::1]:443"
			in.LogLevel = "debug"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := sample()
			mutate(&in)
			raw, err := nodecfg.Build(in)
			if err != nil {
				t.Fatal(err)
			}
			load(t, raw)
		})
	}
}

// What the agent reads off a running node: per-user counters and the online
// list need the level's stats switched on, and the node-wide totals need the
// system ones.
func TestXrayPolicyEnablesTheStatsTheAgentReads(t *testing.T) {
	raw, err := nodecfg.Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	pm := load(t, raw).GetFeature(policy.ManagerType()).(policy.Manager)

	if s := pm.ForLevel(0).Stats; !s.UserUplink || !s.UserDownlink || !s.UserOnline {
		t.Errorf("level 0 stats = %+v, want uplink, downlink and online all on", s)
	}
	if s := pm.ForSystem().Stats; !s.InboundUplink || !s.InboundDownlink || !s.OutboundUplink || !s.OutboundDownlink {
		t.Errorf("system stats = %+v, want all four on", s)
	}
}
