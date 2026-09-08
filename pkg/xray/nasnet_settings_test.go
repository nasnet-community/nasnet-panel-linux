package xray

import (
	"encoding/json"
	"strings"
	"testing"

	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/xtls/xray-core/infra/conf"
)

func TestPanelSettingsRouterBandwidthPreservesWANSelection(t *testing.T) {
	via := TagDirectForeignVia("secondary2")
	pinned := netmark.GroupMark(netmark.GroupForeignVia(3)) | netmark.PinMark(2)
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).WithRouterMode(true).WithRouterWANs(twoVias()).
		WithUsers(map[string][]*User{"in": {{Email: "limited@test", Level: 1}}}).
		WithOutbounds([]*d.Outbound{
			{Tag: "pinned", Protocol: "freedom", Network: "tcp", SockoptSettings: &d.SockoptSettings{Mark: pinned | 200}},
			{Tag: "chain", Protocol: "socks", Address: "192.0.2.1", Port: 1080, Network: "tcp", SockoptSettings: &d.SockoptSettings{DialerProxy: "pinned"}},
		}).WithRoutingRules([]*d.RoutingRule{
		{Enabled: true, OutboundTag: "blocked", DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "blocked.test"}}},
		{Enabled: true, OutboundTag: TagDirectDomestic, DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "domestic.test"}}},
		{Enabled: true, OutboundTag: via, DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "via.test"}}},
		{Enabled: true, OutboundTag: "chain", DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "chain.test"}}},
		{Enabled: true, BalancingTag: "foreign-pool", DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "balanced.test"}}},
	}).WithBalancingRules([]*d.BalancingRule{{Tag: "foreign-pool", Enabled: true, Strategy: "roundrobin", OutboundSelectors: []string{TagDirectForeign}}})
	c := b.buildOrderedConfig()
	for _, tc := range []struct {
		host, original string
		group          uint32
	}{
		{"domestic.test", TagDirectDomestic, netmark.GroupDomestic},
		{"via.test", via, netmark.GroupForeignVia(3)},
	} {
		tag, _ := firstPanelRoute(t, c, "in", "limited@test", tc.host)
		assertPanelMark(t, c, tag, netmark.GroupMark(tc.group)|10)
		tag, _ = firstPanelRoute(t, c, "in", "unlimited@test", tc.host)
		if tag != tc.original {
			t.Fatalf("unlimited %s selected %s", tc.host, tag)
		}
		assertPanelMark(t, c, tag, netmark.GroupMark(tc.group))
	}
	for _, email := range []string{"limited@test", "unlimited@test"} {
		tag, _ := firstPanelRoute(t, c, "in", email, "blocked.test")
		if tag != "blocked" {
			t.Fatalf("block bypassed by %s", email)
		}
		// On a router miss, the generated loopback must retain the foreign
		// default for both limited and unlimited users.
		tag, _ = firstPanelRoute(t, c, "in", email, "fallback.test")
		if tag != "" {
			t.Fatalf("unexpected first-pass fallback %s", tag)
		}
		inbound := c.Outbounds[0]["settings"].(map[string]interface{})["inboundTag"].(string)
		tag, _ = firstPanelRoute(t, c, inbound, email, "fallback.test")
		mark := netmark.GroupMark(netmark.GroupForeign)
		if email == "limited@test" {
			mark |= 10
		}
		assertPanelMark(t, c, tag, mark)
	}
	tag, _ := firstPanelRoute(t, c, "in", "limited@test", "chain.test")
	dialer := panelOutbound(t, c, tag)["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})["dialerProxy"].(string)
	assertPanelMark(t, c, dialer, pinned|10)
	assertPanelMark(t, c, "pinned", pinned|200)
	_, balTag := firstPanelRoute(t, c, "in", "limited@test", "balanced.test")
	found := false
	for _, bal := range c.Routing["balancers"].([]map[string]interface{}) {
		if bal["tag"] != balTag {
			continue
		}
		found = true
		selectors := bal["selector"].([]string)
		if len(selectors) != 3 {
			t.Fatalf("lost foreign WAN candidates: %v", selectors)
		}
		for _, selector := range selectors {
			mark := sockoptMark(t, panelOutbound(t, c, selector))
			group := netmark.Group(mark)
			if netmark.Tier(mark) != 10 || (group != netmark.GroupForeign && !netmark.IsGroupForeignVia(group)) {
				t.Fatalf("balancer changed WAN or tier: 0x%x", mark)
			}
		}
	}
	if !found {
		t.Fatal("missing limited foreign balancer")
	}
	raw, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	var parsed conf.Config
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Build(); err != nil {
		t.Fatal(err)
	}
}

func TestPanelSettingsRouterReverseNamespace(t *testing.T) {
	for _, tag := range []string{TagDirectForeign, TagDirectDomestic, TagDirectForeignVia("secondary4")} {
		b := NewFullConfigBuilder(&d.Node{}).WithRouterMode(true).
			WithReverseProxies([]*d.ReverseProxy{{Type: "portal", Tag: tag}})
		if _, err := b.Build(); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("accepted managed reverse tag %q: %v", tag, err)
		}
	}
}
