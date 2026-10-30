// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"slices"
	"testing"

	"github.com/xtls/xray-core/common/geodata"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	rs "github.com/xtls/xray-core/features/routing/session"
	"github.com/xtls/xray-core/infra/conf"

	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/jsontype"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// The hub can't import the panel's config builder (it drags in Xray, gorm and
// geoip), so nodecfg rewrites the part of it a portal node needs. These tests
// build the same portal with the panel's FullConfigBuilder and its reverse
// tunnel support, and hold the two together.
//
// What matches: every top-level section; the API inbound, its rule and the
// API section; stats; level 0 and system policy; the entry inbound whole,
// clients, flow and the bridge client's reverse tag included, apart from the
// transport block below; and all four routing rules, in order, with the same
// targets and ranges.
//
// What differs on purpose:
//   - routing.domainStrategy is IPOnDemand where the panel defaults to
//     IPIfNonMatch. The rules here are all ranges and the last one matches the
//     whole inbound, so names must be resolved before they run (see
//     TestIPIfNonMatchWouldSendDomesticNamesIntoTheTunnel).
//   - The block-private rule lists Xray's private ranges outright; the panel
//     would write geoip:private, which needs geoip.dat on the node. A second
//     test holds the list to Xray's own private set.
//   - log.access is "none": the panel leaves it on the console.
//   - policy has level 0 only; the panel declares one entry per user level, and
//     every client here is level 0.
//   - blocked is the first outbound, so it is the fallback; the panel puts
//     direct first. Both outbounds are otherwise identical.
//   - The entry's streamSettings has no empty tcpSettings block; network,
//     security and realitySettings are the same.
func TestParityWithThePanelBuilder(t *testing.T) {
	in := sample()
	ours, theirs := decode(t, in), panelPortal(t, in)

	keys := func(m map[string]any) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(keys(ours), keys(theirs)) {
		t.Errorf("top-level sections: nodecfg has %v, the panel builds %v", keys(ours), keys(theirs))
	}
	if _, ok := theirs["xcoreHub"]; ok {
		t.Error("the panel hands the reverse tag to every user through xcoreHub; nodecfg must not follow")
	}

	same := func(what string, a, b any) {
		t.Helper()
		if !reflect.DeepEqual(a, b) {
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			t.Errorf("%s differs\n nodecfg: %s\n  panel: %s", what, ja, jb)
		}
	}

	same("api section", at(t, ours, "api"), at(t, theirs, "api"))
	same("stats", at(t, ours, "stats"), at(t, theirs, "stats"))
	same("policy level 0", at(t, ours, "policy", "levels", "0"), at(t, theirs, "policy", "levels", "0"))
	same("system policy", at(t, ours, "policy", "system"), at(t, theirs, "policy", "system"))
	same("log level", at(t, ours, "log", "loglevel"), at(t, theirs, "log", "loglevel"))
	if n := len(at(t, ours, "policy", "levels").(map[string]any)); n != 1 {
		t.Errorf("nodecfg declares %d policy levels, want only level 0", n)
	}

	// inbounds: the API first, then the entry
	same("api inbound", at(t, ours, "inbounds", 0), at(t, theirs, "inbounds", 0))
	for _, key := range []string{"tag", "protocol", "port", "listen", "sniffing"} {
		same("entry "+key, at(t, ours, "inbounds", 1, key), at(t, theirs, "inbounds", 1, key))
	}
	same("entry decryption", at(t, ours, "inbounds", 1, "settings", "decryption"), at(t, theirs, "inbounds", 1, "settings", "decryption"))
	same("entry clients", at(t, ours, "inbounds", 1, "settings", "clients"), at(t, theirs, "inbounds", 1, "settings", "clients"))
	for _, key := range []string{"network", "security", "realitySettings"} {
		same("entry stream "+key, at(t, ours, "inbounds", 1, "streamSettings", key), at(t, theirs, "inbounds", 1, "streamSettings", key))
	}

	// the bridge client, spelled out: it is the one client with a reverse tag
	clients := at(t, ours, "inbounds", 1, "settings", "clients").([]any)
	for i, c := range clients {
		tag, has := c.(map[string]any)["reverse"]
		if want := i == len(clients)-1; has != want {
			t.Errorf("client %d has a reverse tag: %v, want %v", i, has, want)
		}
		if has {
			same("reverse tag", tag, map[string]any{"tag": in.Bridge.ReverseTag})
		}
		same("client flow", c.(map[string]any)["flow"], nodecfg.Flow)
	}

	// routing: four rules, same order, same targets, same ranges
	same("routing rules", at(t, ours, "routing", "rules"), at(t, theirs, "routing", "rules"))
	same("routing domain strategy of the panel", at(t, theirs, "routing", "domainStrategy"), "IPIfNonMatch")
	same("routing domain strategy of nodecfg", at(t, ours, "routing", "domainStrategy"), "IPOnDemand")

	// outbounds: the same two, in the other order
	byTag := func(cfg map[string]any) map[string]any {
		out := map[string]any{}
		for _, o := range at(t, cfg, "outbounds").([]any) {
			out[o.(map[string]any)["tag"].(string)] = o
		}
		return out
	}
	same("outbounds", byTag(ours), byTag(theirs))
	same("first outbound of nodecfg", at(t, ours, "outbounds", 0, "tag"), "blocked")
	same("first outbound of the panel", at(t, theirs, "outbounds", 0, "tag"), "direct")
}

// The same with the embedded domestic list and a custom API port: more input,
// same parity.
func TestParityWithTheEmbeddedListAndAnotherAPIPort(t *testing.T) {
	in := sample()
	in.DomesticCIDRs = nil
	in.APIPort = 20085
	in.EntryPort = 443
	ours, theirs := decode(t, in), panelPortal(t, in)
	for _, path := range [][]any{
		{"api"}, {"inbounds"}, {"routing", "rules"}, {"policy", "system"},
	} {
		a, b := at(t, ours, path...), at(t, theirs, path...)
		if path[0] == "inbounds" {
			// only the entry's transport block differs
			for _, x := range []any{a, b} {
				delete(at(t, x, 1, "streamSettings").(map[string]any), "tcpSettings")
			}
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%v differs between nodecfg and the panel", path)
		}
	}
}

// The panel names the hidden login of a tunnel rvs-t<id>@reverse and skips
// those everywhere it counts customers. A bridge named the same way keeps its
// traffic out of the subscriptions, and a subscription is never mistaken for
// one.
func TestEmailsFollowThePanelsConvention(t *testing.T) {
	for _, id := range []int{1, 7, 250} {
		bridge := nodecfg.BridgeEmail(id)
		if !d.IsReverseEmail(bridge) {
			t.Errorf("%q isn't a hidden reverse login to the panel", bridge)
		}
		if got, ok := d.ReverseTunnelIDFromEmail(bridge); !ok || got != uint(id) {
			t.Errorf("the panel reads %q as tunnel %d, %v", bridge, got, ok)
		}
		if want := (&d.ReverseTunnel{ID: uint(id)}).Email(); bridge != want {
			t.Errorf("BridgeEmail(%d) = %q, the panel's tunnel %d is %q", id, bridge, id, want)
		}
		if sub := nodecfg.SubscriptionEmail(id); d.IsReverseEmail(sub) {
			t.Errorf("%q would be hidden as a reverse login", sub)
		}
	}
}

// block-private lists what Xray itself counts as private (the set its Freedom
// outbound refuses by default), no more and no fewer: checked at the edge of
// each range and just outside it. Without a geoip.dat on the node, the ranges
// have to be spelled out, and this keeps them honest.
func TestPrivateRangesMatchXraysPrivateSet(t *testing.T) {
	// the panel's reverse exit spells out the same set
	if got := panelPrivateRanges(); !slices.Equal(got, nodecfg.PrivateCIDRs()) {
		t.Errorf("the panel's reverse exit blocks %v, nodecfg %v", got, nodecfg.PrivateCIDRs())
	}

	ours := ruleCondition(t, mustJSON(t, map[string]any{"type": "field", "ip": nodecfg.PrivateCIDRs(), "outboundTag": "x"}))
	xrays := geodata.GetPrivateIPMatcher()

	var probes []netip.Addr
	var private []netip.Prefix
	for _, s := range nodecfg.PrivateCIDRs() {
		private = append(private, netip.MustParsePrefix(s))
	}
	for _, p := range private {
		first, last := p.Addr(), lastAddr(p)
		probes = append(probes, first, last)
		if before := first.Prev(); before.IsValid() {
			probes = append(probes, before)
		}
		if after := last.Next(); after.IsValid() {
			probes = append(probes, after)
		}
	}
	for _, s := range []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34", "5.22.9.9", "198.51.101.1", "2001:4860:4860::8888",
		"2606:4700:4700::1111", "::2", "::ffff:8.8.8.8", "::ffff:10.1.1.1", "2a01:5ec0::1",
	} {
		probes = append(probes, netip.MustParseAddr(s))
	}

	matched := 0
	for _, ip := range probes {
		a, b := ours(ip), xrays.Match(ip.Unmap().AsSlice())
		if a != b {
			t.Errorf("%s: nodecfg's private rule says %v, Xray's private set says %v", ip, a, b)
		}
		if a {
			matched++
		}
	}
	if matched < len(private) {
		t.Fatalf("only %d probes matched; the check isn't reaching the ranges", matched)
	}
}

// panelPrivateRanges is the private set the panel's reverse exit blocks.
func panelPrivateRanges() []string {
	settings := xray.ReverseExitOutbound()["settings"].(map[string]interface{})
	rules := settings["finalRules"].([]map[string]interface{})
	return rules[0]["ip"].([]string)
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ruleCondition compiles one routing rule the way Xray does and returns a
// function that asks whether a connection to an IP matches it.
func ruleCondition(t *testing.T, rule string) func(netip.Addr) bool {
	t.Helper()
	var parsed conf.RouterConfig
	if err := json.Unmarshal([]byte(`{"rules":[`+rule+`]}`), &parsed); err != nil {
		t.Fatal(err)
	}
	built, err := parsed.Build()
	if err != nil {
		t.Fatal(err)
	}
	cond, err := built.Rule[0].BuildCondition()
	if err != nil {
		t.Fatal(err)
	}
	return func(ip netip.Addr) bool {
		return cond.Apply(&rs.Context{
			Inbound:  &session.Inbound{Tag: nodecfg.EntryTag},
			Outbound: &session.Outbound{Target: xnet.TCPDestination(xnet.ParseAddress(ip.String()), 443)},
		})
	}
}

// panelPortal builds in's node the way the panel would: a VLESS + REALITY
// inbound with the clients, a reverse tunnel portal on it, and the same four
// routing rules as managed rules.
func panelPortal(t *testing.T, in nodecfg.Input) map[string]any {
	t.Helper()
	domestic := in.DomesticCIDRs
	if len(domestic) == 0 {
		domestic = nodecfg.DomesticCIDRs()
	}
	apiPort := in.APIPort
	if apiPort == 0 {
		apiPort = nodecfg.DefaultAPIPort
	}

	users := make([]*xray.User, 0, len(in.Clients))
	for _, c := range in.Clients {
		users = append(users, &xray.User{UUID: c.UUID, Email: c.Email})
	}
	entry := &d.Inbound{
		Tag: nodecfg.EntryTag, Protocol: "vless", Listen: "0.0.0.0", Port: in.EntryPort,
		Network: "tcp", Security: "reality",
		VLESSSettings: &d.VLESSSettings{Flow: nodecfg.Flow, Encryption: "none"},
		RealitySettings: &d.RealitySettings{
			Dest: in.RealityDest, ServerNames: in.ServerNames, PrivateKey: in.PrivateKey, ShortIDs: in.ShortIDs,
		},
	}
	private := panelPrivateRanges() // as the first rule, ahead of the domestic one
	tunnelRule := uint(3)
	rules := []*d.RoutingRule{
		{ID: 1, Enabled: true, RuleTag: "block-private", IPCIDRRules: jsontype.StringSlice(private), OutboundTag: "blocked"},
		{ID: 2, Enabled: true, RuleTag: "domestic-direct", IPCIDRRules: jsontype.StringSlice(domestic), OutboundTag: "direct"},
		{ID: tunnelRule, Enabled: true, RuleTag: "to-tunnel", InboundTags: jsontype.StringSlice{nodecfg.EntryTag}, OutboundTag: in.Bridge.ReverseTag},
	}

	raw, err := xray.NewFullConfigBuilder(&d.Node{LogLevel: "warning"}).
		WithInbounds([]*d.Inbound{entry}).
		WithUsers(map[string][]*xray.User{nodecfg.EntryTag: users}).
		WithAPI(true, apiPort).
		WithRoutingRules(rules).
		WithReverseTunnels([]xray.ReversePortal{{
			InboundTag: entry.Tag, UUID: in.Bridge.UUID, Email: in.Bridge.Email, Tag: in.Bridge.ReverseTag, RuleID: &tunnelRule,
		}}, nil).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
