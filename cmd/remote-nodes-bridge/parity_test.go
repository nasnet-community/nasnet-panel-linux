// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The bridge builds its Xray config by hand, so these tests hold it to what
// xray-core's own JSON loader builds. The reference is the Linux panel's
// bridge end, the config ReverseTunnelPeerConfig hands out for an external
// bridge (internal/node/usecase/reverse_tunnels.go:182): an exit outbound
// from xray.ReverseExitOutbound and, per tunnel, the outbound
// sharelink.ReverseBridgeOutbound makes from the portal's link, plus one
// routing rule. The test makes that JSON with the same functions and loads it
// through infra/conf.
//
// The bridge's config differs from the panel's in the ways listed in
// bridgeDeltas, and only those. TestParityWithDeltas applies them to the
// panel's JSON and demands the whole config come out equal, byte for byte
// (proto.Equal on core.Config). TestParityOutboundsUnchanged is the other
// half: it leaves the panel's JSON alone and shows that what the bridge
// changes in each outbound is exactly the delta and nothing else.

import (
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	vlessout "github.com/xtls/xray-core/proxy/vless/outbound"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/sharelink"
)

// portalLink is the vless:// link the panel would give the bridge for n.
func portalLink(n bridgespec.Node) string {
	q := url.Values{}
	q.Set("encryption", "none")
	if n.Flow != "" {
		q.Set("flow", n.Flow)
	}
	q.Set("security", "reality")
	q.Set("sni", n.ServerName)
	q.Set("fp", n.FingerprintOrDefault())
	q.Set("pbk", n.PublicKey)
	q.Set("sid", n.ShortID)
	q.Set("type", "tcp")
	return "vless://" + n.UUID + "@" + net.JoinHostPort(n.Addr, strconv.Itoa(int(n.Port))) + "?" + q.Encode() + "#" + url.PathEscape(n.Tag)
}

// panelBridgeConfig is the Linux panel's bridge config, as generic JSON, for
// the nodes of s: what ReverseTunnelPeerConfig builds for one tunnel,
// repeated for each.
func panelBridgeConfig(t *testing.T, s *bridgespec.Spec) map[string]any {
	t.Helper()
	outbounds := []any{xray.ReverseExitOutbound()}
	var rules []any
	for _, n := range s.Nodes {
		out, err := sharelink.ReverseBridgeOutbound(portalLink(n), n.Tag, n.ReverseTag)
		if err != nil {
			t.Fatal(err)
		}
		outbounds = append(outbounds, out)
		rules = append(rules, map[string]any{
			"type": "field", "inboundTag": []string{n.ReverseTag}, "outboundTag": domain.ReverseExitTag,
		})
	}
	return generic(t, map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"outbounds": outbounds,
		"routing":   map[string]any{"rules": rules},
	})
}

// generic round-trips v through JSON so every level is map[string]any.
func generic(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// bridgeDeltas edits the panel's config into what the bridge means to run.
// This list is the documentation of every difference.
//
//  1. The exit is tagged "exit", not "rvs-exit".
//  2. The exit sends from the exit address, and resolves names itself
//     (targetStrategy forceipv4): the panel's exit hands them to the system
//     resolver, whose lookups would leave the router by the wrong route.
//  3. Each tunnel outbound sends from the dial address.
//  4. Each tunnel outbound's user has an email, the node's tag, which names
//     its byte counters. The panel leaves it off because there the portal's
//     count is the tunnel's.
//  5. The tunnel outbounds have no explicit tcpSettings. The loader writes
//     header "none" for the link's type=tcp, which is the default anyway.
//  6. The log has no access log, so no destination a subscription visits
//     reaches the hub's output.
//  7. A tagged "dns" section names the resolvers. Without one Xray resolves
//     through the system.
//  8. A first routing rule sends the dns tag's lookups to the exit.
//  9. stats and a level-0 policy count each user's bytes up and down.
func bridgeDeltas(t *testing.T, cfg map[string]any, s *bridgespec.Spec) {
	t.Helper()
	outbounds := cfg["outbounds"].([]any)
	exit := outbounds[0].(map[string]any)
	exit["tag"] = bridgespec.ExitTag     // 1
	exit["sendThrough"] = s.Exit.Addr    // 2
	exit["targetStrategy"] = "forceipv4" // 2
	for _, o := range outbounds[1:] {
		out := o.(map[string]any)
		out["sendThrough"] = s.DialAddr                               // 3
		out["settings"].(map[string]any)["email"] = out["tag"]        // 4
		delete(out["streamSettings"].(map[string]any), "tcpSettings") // 5
	}

	cfg["log"] = map[string]any{"loglevel": "warning", "access": "none"} // 6

	servers := make([]any, 0, len(s.DNS.Servers))
	for _, raw := range s.DNS.Servers {
		ap, err := bridgespec.ParseServer(raw)
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, map[string]any{"address": ap.Addr().String(), "port": ap.Port()})
	}
	cfg["dns"] = map[string]any{"tag": bridgespec.DNSTag, "queryStrategy": "UseIPv4", "servers": servers} // 7

	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	for _, r := range rules {
		r.(map[string]any)["outboundTag"] = bridgespec.ExitTag // 1
	}
	dnsRule := map[string]any{"type": "field", "inboundTag": []string{bridgespec.DNSTag}, "outboundTag": bridgespec.ExitTag}
	cfg["routing"].(map[string]any)["rules"] = append([]any{dnsRule}, rules...) // 8

	cfg["stats"] = map[string]any{} // 9
	cfg["policy"] = map[string]any{"levels": map[string]any{"0": map[string]any{
		"statsUserUplink": true, "statsUserDownlink": true,
	}}} // 9
}

// load builds a JSON config through xray-core's loader.
func load(t *testing.T, cfg map[string]any) *core.Config {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var c conf.Config
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("loader: %v", err)
	}
	built, err := c.Build()
	if err != nil {
		t.Fatalf("loader: %v", err)
	}
	return built
}

func TestParityWithDeltas(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*bridgespec.Spec)
	}{
		{"two IPv4 nodes", func(*bridgespec.Spec) {}},
		{"one node", func(s *bridgespec.Spec) { s.Nodes = s.Nodes[:1] }},
		{"no flow, other fingerprint", func(s *bridgespec.Spec) {
			s.Nodes[0].Flow = ""
			s.Nodes[1].Fingerprint = "firefox"
		}},
		{"short id of one byte", func(s *bridgespec.Spec) { s.Nodes[0].ShortID = "ab" }},
		{"capitals in the id", func(s *bridgespec.Spec) { s.Nodes[0].UUID = "7F8B1A9E-3C2D-4B5A-9E6F-0A1B2C3D4E51" }},
		{"another resolver port", func(s *bridgespec.Spec) { s.DNS.Servers = []string{"9.9.9.9:5353"} }},
		{"debug log", func(s *bridgespec.Spec) { s.LogLevel = "debug" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testSpec()
			tt.mutate(s)
			cfg := panelBridgeConfig(t, s)
			bridgeDeltas(t, cfg, s)
			if s.LogLevel != "" {
				cfg["log"] = map[string]any{"loglevel": s.LogLevel, "access": "none"}
			}
			want := load(t, cfg)

			got, err := buildConfig(s)
			if err != nil {
				t.Fatal(err)
			}
			sameConfig(t, got, want)
		})
	}
}

// With the log off the loader and the bridge agree on that too.
func TestParityLogNone(t *testing.T) {
	s := testSpec()
	s.LogLevel = "none"
	cfg := panelBridgeConfig(t, s)
	bridgeDeltas(t, cfg, s)
	cfg["log"] = map[string]any{"loglevel": "none"}
	got, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	sameConfig(t, got, load(t, cfg))
}

// Beyond the first resolver and node, nothing about a second family changes
// the shape either.
func TestParityIPv6(t *testing.T) {
	s := testSpec()
	s.DialAddr, s.Exit.Addr = "fd00::18", "fd00::17"
	s.Nodes[0].Addr, s.Nodes[1].Addr = "2001:db8::10", "2001:db8::20"
	s.DNS.Servers = []string{"2606:4700:4700::1111", "[2001:4860:4860::8888]:53"}
	cfg := panelBridgeConfig(t, s)
	bridgeDeltas(t, cfg, s)
	cfg["outbounds"].([]any)[0].(map[string]any)["targetStrategy"] = "forceipv6"
	cfg["dns"].(map[string]any)["queryStrategy"] = "UseIPv6"
	got, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	sameConfig(t, got, load(t, cfg))
}

// The panel's JSON, untouched, and the bridge's outbounds side by side:
// what the bridge changes is deltas 1 to 5 and nothing else. Anything the
// loader fills in that the bridge doesn't, or the other way, shows here.
func TestParityOutboundsUnchanged(t *testing.T) {
	s := testSpec()
	panel := load(t, panelBridgeConfig(t, s))
	got, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(panel.Outbound) != len(got.Outbound) {
		t.Fatalf("%d outbounds, the panel has %d", len(got.Outbound), len(panel.Outbound))
	}

	// The exit: the panel's rules are the bridge's rules.
	if !proto.Equal(instance(t, panel.Outbound[0].ProxySettings), instance(t, got.Outbound[0].ProxySettings)) {
		t.Errorf("the exit's rules differ from the panel's:\n%s\nvs panel's\n%s",
			text(t, got.Outbound[0].ProxySettings), text(t, panel.Outbound[0].ProxySettings))
	}

	// The tunnels: the same outbound once the bridge's additions are taken
	// off it. That is the source address, the user's email and the loader's
	// explicit tcp header.
	for i := 1; i < len(got.Outbound); i++ {
		mine, theirs := got.Outbound[i], panel.Outbound[i]
		if mine.Tag != theirs.Tag {
			t.Errorf("outbound %d: tag %q, the panel's %q", i, mine.Tag, theirs.Tag)
		}
		mineSender, theirSender := senderOf(t, mine), senderOf(t, theirs)
		if mineSender.Via == nil {
			t.Errorf("%s has no source address", mine.Tag)
		}
		mineSender.Via = nil
		theirSender.StreamSettings.TransportSettings = nil
		if !proto.Equal(mineSender, theirSender) {
			t.Errorf("%s: stream settings differ from the panel's:\n%s\nvs panel's\n%s", mine.Tag, prototext.Format(mineSender), prototext.Format(theirSender))
		}

		mineProxy, theirProxy := instance(t, mine.ProxySettings), instance(t, theirs.ProxySettings)
		if got, want := userEmail(t, mineProxy), mine.Tag; got != want {
			t.Errorf("%s: email %q, want the tag", mine.Tag, got)
		}
		setEmail(t, mineProxy, "")
		if !proto.Equal(mineProxy, theirProxy) {
			t.Errorf("%s: VLESS settings differ from the panel's:\n%s\nvs panel's\n%s", mine.Tag, prototext.Format(mineProxy), prototext.Format(theirProxy))
		}
	}
}

// If the Linux panel's exit rules change, the bridge's default must follow:
// subscriptions through the router get the same protection as through a node.
func TestDefaultBlockedMatchesThePanelsExit(t *testing.T) {
	settings := xray.ReverseExitOutbound()["settings"].(map[string]interface{})
	rules := settings["finalRules"].([]map[string]interface{})
	panel := rules[0]["ip"].([]string)
	mine := bridgespec.DefaultBlocked()
	if len(panel) != len(mine) {
		t.Fatalf("the panel blocks %d ranges, the bridge %d:\n%v\n%v", len(panel), len(mine), panel, mine)
	}
	for i := range panel {
		if panel[i] != mine[i] {
			t.Errorf("range %d: the panel blocks %s, the bridge %s", i, panel[i], mine[i])
		}
	}
	if len(rules) != 2 || rules[1]["action"] != "allow" {
		t.Fatalf("the panel's exit no longer ends in allow: %v", rules)
	}
}

// sameConfig reports each part that differs, in a form a person can read,
// then fails if the whole isn't equal.
func sameConfig(t *testing.T, got, want *core.Config) {
	t.Helper()
	if proto.Equal(got, want) {
		return
	}
	if len(got.Inbound) != len(want.Inbound) {
		t.Errorf("%d inbounds, want %d", len(got.Inbound), len(want.Inbound))
	}
	if len(got.Outbound) != len(want.Outbound) {
		t.Errorf("%d outbounds, want %d", len(got.Outbound), len(want.Outbound))
	}
	for i := 0; i < len(got.Outbound) && i < len(want.Outbound); i++ {
		g, w := got.Outbound[i], want.Outbound[i]
		if g.Tag != w.Tag {
			t.Errorf("outbound %d: tag %q, want %q", i, g.Tag, w.Tag)
		}
		if !proto.Equal(g.SenderSettings, w.SenderSettings) {
			t.Errorf("outbound %s: sender settings differ:\n%s\nwant\n%s", g.Tag, text(t, g.SenderSettings), text(t, w.SenderSettings))
		}
		if !proto.Equal(g.ProxySettings, w.ProxySettings) {
			t.Errorf("outbound %s: proxy settings differ:\n%s\nwant\n%s", g.Tag, text(t, g.ProxySettings), text(t, w.ProxySettings))
		}
	}
	if len(got.App) != len(want.App) {
		t.Errorf("%d apps, want %d", len(got.App), len(want.App))
	}
	for i := 0; i < len(got.App) && i < len(want.App); i++ {
		g, w := got.App[i], want.App[i]
		if g.Type != w.Type || !proto.Equal(g, w) {
			t.Errorf("app %d (%s vs %s) differs:\n%s\nwant\n%s", i, g.Type, w.Type, text(t, g), text(t, w))
		}
	}
	t.Fatal("the bridge's config is not what the loader builds for the equivalent JSON")
}

func instance(t *testing.T, m *serial.TypedMessage) proto.Message {
	t.Helper()
	msg, err := m.GetInstance()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func text(t *testing.T, m *serial.TypedMessage) string {
	t.Helper()
	if m == nil {
		return "<nil>"
	}
	return prototext.MarshalOptions{Multiline: true}.Format(instance(t, m))
}

func senderOf(t *testing.T, o *core.OutboundHandlerConfig) *proxyman.SenderConfig {
	t.Helper()
	return instance(t, o.SenderSettings).(*proxyman.SenderConfig)
}

func userEmail(t *testing.T, p proto.Message) string {
	t.Helper()
	return p.(*vlessout.Config).Vnext.User.Email
}

func setEmail(t *testing.T, p proto.Message, email string) {
	t.Helper()
	p.(*vlessout.Config).Vnext.User.Email = email
}
