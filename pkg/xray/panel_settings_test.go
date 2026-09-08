package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdnet "net"
	"reflect"
	"strings"
	"testing"
	"time"

	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/policy"
	"github.com/xtls/xray-core/app/proxyman"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	om "github.com/xtls/xray-core/app/proxyman/outbound"
	"github.com/xtls/xray-core/app/router"
	_ "github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/dns"
	fo "github.com/xtls/xray-core/features/outbound"
	rs "github.com/xtls/xray-core/features/routing/session"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/loopback"
)

func parsedPanelRouting(t *testing.T, c *OrderedConfig) *router.Config {
	t.Helper()
	raw, err := json.Marshal(c.Routing)
	if err != nil {
		t.Fatal(err)
	}
	var parsed conf.RouterConfig
	if err = json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	built, err := parsed.Build()
	if err != nil {
		t.Fatal(err)
	}
	return built
}
func firstPanelRoute(t *testing.T, c *OrderedConfig, inbound, email, host string) (string, string) {
	t.Helper()
	ctx := &rs.Context{Inbound: &session.Inbound{Tag: inbound, User: &protocol.MemoryUser{Email: email}}, Outbound: &session.Outbound{Target: net.TCPDestination(net.ParseAddress(host), 443)}}
	for _, rule := range parsedPanelRouting(t, c).Rule {
		condition, err := rule.BuildCondition()
		if err != nil {
			t.Fatal(err)
		}
		if condition.Apply(ctx) {
			return rule.GetTag(), rule.GetBalancingTag()
		}
	}
	return "", ""
}
func panelOutbound(t *testing.T, c *OrderedConfig, tag string) map[string]interface{} {
	t.Helper()
	for _, out := range c.Outbounds {
		if out["tag"] == tag {
			return out
		}
	}
	t.Fatalf("missing outbound %s", tag)
	return nil
}
func assertPanelMark(t *testing.T, c *OrderedConfig, tag string, mark uint32) {
	t.Helper()
	out := panelOutbound(t, c, tag)
	ss, _ := out["streamSettings"].(map[string]interface{})
	sock, _ := ss["sockopt"].(map[string]interface{})
	if sock["mark"] != mark {
		t.Fatalf("outbound %s mark=%v, want %d", tag, sock["mark"], mark)
	}
}
func TestPanelSettingsBandwidthRetainsPolicyOrder(t *testing.T) {
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
		WithUsers(map[string][]*User{"in": {{Email: "limited@example.test", Level: 1}, {Email: "another@example.test", Level: 1}}}).
		WithOutbounds([]*d.Outbound{
			{Tag: "proxy", Protocol: "socks", Address: "192.0.2.1", Port: 1080, ProxySettings: &d.ProxySettings{Tag: "direct"}},
			{Tag: "warp", Protocol: "freedom", Network: "tcp", SockoptSettings: &d.SockoptSettings{Interface: "wg0"}},
		}).WithRoutingRules([]*d.RoutingRule{
		{Enabled: true, RuleTag: "block", OutboundTag: "blocked", DomainRules: d.DomainMatcherSlice{{Type: "regex", Value: `^blocked\.test$`}}},
		{Enabled: true, RuleTag: "private", OutboundTag: "proxy", UserEmails: []string{"other@test"}, DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "private.test"}}},
		{Enabled: true, RuleTag: "warp", OutboundTag: "warp", UserEmails: []string{`regexp:^limited@`}, DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "warp.test"}}},
		{Enabled: true, RuleTag: "balanced", BalancingTag: "pool", DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "balanced.test"}}},
		{Enabled: true, RuleTag: "reverse", OutboundTag: "portal", DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "reverse.test"}}},
		{Enabled: true, RuleTag: "catch", OutboundTag: "direct", NetworkRules: []string{"tcp", "udp"}},
	}).WithBalancingRules([]*d.BalancingRule{{Tag: "pool", Enabled: true, OutboundSelectors: []string{"pro"}, Strategy: "random", FallbackTag: "warp"}}).
		WithReverseProxies([]*d.ReverseProxy{{Type: "portal", Tag: "portal", Domain: "internal.test"}})
	c := b.buildOrderedConfig()
	for _, email := range []string{"limited@example.test", "unlimited@test"} {
		tag, _ := firstPanelRoute(t, c, "in", email, "blocked.test")
		if tag != "blocked" {
			t.Fatalf("block bypassed for %s: %s", email, tag)
		}
		tag, _ = firstPanelRoute(t, c, "in", email, "reverse.test")
		if tag != "portal" {
			t.Fatalf("reverse replaced by %s", tag)
		}
	}
	tag, _ := firstPanelRoute(t, c, "in", "limited@example.test", "warp.test")
	assertPanelMark(t, c, tag, 10)
	if panelOutbound(t, c, tag)["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})["interface"] != "wg0" {
		t.Fatal("lost interface")
	}
	// The specialized rule must intersect the original user matcher.
	tag, _ = firstPanelRoute(t, c, "in", "another@example.test", "warp.test")
	if panelOutbound(t, c, tag)["protocol"] != "freedom" || tag == "warp" {
		t.Fatal("user matcher broadened")
	}
	tag, _ = firstPanelRoute(t, c, "in", "limited@example.test", "private.test")
	assertPanelMark(t, c, tag, 10)
	if panelOutbound(t, c, tag)["protocol"] != "freedom" {
		t.Fatal("private user route broadened")
	}
	_, balTag := firstPanelRoute(t, c, "in", "limited@example.test", "balanced.test")
	if balTag == "pool" || balTag == "" {
		t.Fatal("tier balancer missing")
	}
	for _, bal := range c.Routing["balancers"].([]map[string]interface{}) {
		if bal["tag"] == "pool" {
			if !reflect.DeepEqual(bal["selector"], []string{"proxy"}) {
				t.Fatal("original selectors not isolated")
			}
		}
		if bal["tag"] == balTag {
			for _, outTag := range bal["selector"].([]string) {
				assertPanelMark(t, c, outTag, 10)
				chain := panelOutbound(t, c, outTag)["proxySettings"].(map[string]interface{})["tag"].(string)
				assertPanelMark(t, c, chain, 10)
			}
			assertPanelMark(t, c, bal["fallbackTag"].(string), 10)
		}
	}
	// No map-order nondeterminism or mutation of the model between builds.
	raw, _ := json.Marshal(c)
	again, _ := json.Marshal(b.buildOrderedConfig())
	if string(raw) != string(again) {
		t.Fatal("unstable config")
	}
}

func TestPanelSettingsBandwidthUserMatcherCompatibility(t *testing.T) {
	for _, users := range [][]string{{"regexp:"}, {"regexp:["}, {"regexp:^limited@"}, {"limited@test"}, {""}} {
		b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
			WithUsers(map[string][]*User{"in": {{Email: "limited@test", Level: 1}}}).
			WithOutbounds([]*d.Outbound{{Tag: "private", Protocol: "socks", Address: "192.0.2.1", Port: 1080}}).
			WithRoutingRules([]*d.RoutingRule{{Enabled: true, OutboundTag: "private", UserEmails: users}, {Enabled: true, OutboundTag: "blocked", NetworkRules: []string{"tcp", "udp"}}})
		c := b.buildOrderedConfig()
		tag, _ := firstPanelRoute(t, c, "in", "limited@test", "example.test")
		ctx := &rs.Context{Inbound: &session.Inbound{User: &protocol.MemoryUser{Email: "limited@test"}}}
		wantPrivate := router.NewUserMatcher(users).Apply(ctx)
		if (panelOutbound(t, c, tag)["protocol"] == "socks") != wantPrivate {
			t.Fatalf("user matcher %v changed routing to %s", users, tag)
		}
	}
}

type panelTestDNS struct {
	dns.Client
	calls int
}

func (d *panelTestDNS) LookupIP(_ string, _ dns.IPOption) ([]net.IP, uint32, error) {
	d.calls++
	return []net.IP{net.ParseIP("203.0.113.7")}, 60, nil
}
func TestPanelSettingsBandwidthPreservesDNSRetryAndImplicitDefault(t *testing.T) {
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).WithUsers(map[string][]*User{"in": {{Email: "limited@test", Level: 1}}}).
		WithOutbounds([]*d.Outbound{{Tag: "first-proxy", Protocol: "socks", Address: "192.0.2.1", Port: 1080}}).
		WithRoutingRules([]*d.RoutingRule{{Enabled: true, OutboundTag: "blocked", IPCIDRRules: []string{"203.0.113.0/24"}}})
	c := b.buildOrderedConfig()
	dnsClient := &panelTestDNS{}
	r := new(router.Router)
	if err := r.Init(context.Background(), parsedPanelRouting(t, c), dnsClient, nil, nil); err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := &rs.Context{Inbound: &session.Inbound{Tag: "in", User: &protocol.MemoryUser{Email: "limited@test"}}, Outbound: &session.Outbound{Target: net.TCPDestination(net.DomainAddress("resolve.test"), 443)}}
	route, err := r.PickRoute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if route.GetOutboundTag() != "blocked" || dnsClient.calls != 1 {
		t.Fatalf("DNS policy bypassed: %s, lookups=%d", route.GetOutboundTag(), dnsClient.calls)
	}
	tag, _ := firstPanelRoute(t, c, "in", "limited@test", "198.51.100.1")
	if tag != "" {
		t.Fatalf("unexpected first-pass fallback %s", tag)
	}
	loop := c.Outbounds[0]
	if loop["protocol"] != "loopback" {
		t.Fatal("implicit fallback isn't deferred")
	}
	internal := loop["settings"].(map[string]interface{})["inboundTag"].(string)
	tag, _ = firstPanelRoute(t, c, internal, "limited@test", "198.51.100.1")
	assertPanelMark(t, c, tag, 10)
	if panelOutbound(t, c, tag)["protocol"] != "socks" {
		t.Fatal("implicit default changed")
	}
	tag, _ = firstPanelRoute(t, c, internal, "unlimited@test", "198.51.100.1")
	if tag != "first-proxy" {
		t.Fatal("unlimited default changed")
	}
	// Existing prefixes must not accidentally select any generated handlers.
	b.WithOutbounds([]*d.Outbound{{Tag: "__", Protocol: "freedom"}})
	c = b.buildOrderedConfig()
	if strings.HasPrefix(c.Outbounds[0]["tag"].(string), "__") {
		t.Fatal("generated namespace collides with original selector")
	}
}

func TestPanelSettingsStreamOptionsAndAuthentication(t *testing.T) {
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0)
	sock := &d.SockoptSettings{AcceptProxyProtocol: true, CustomSockopt: []d.CustomSockopt{{Level: 0, OptName: 1, OptValue: "1"}, {Level: 6, OptName: 2, OptValue: "hello", Type: "str"}}}
	mask := &d.FinalMask{UDP: json.RawMessage(`[{"type":"salamander","settings":{"password":"demo-password"}}]`)}
	for _, p := range []string{"socks", "http", "mixed", "dokodemo-door", "hysteria2"} {
		t.Run(p, func(t *testing.T) {
			in := b.convertInbound(&d.Inbound{Tag: p, Protocol: p, Port: 12345, Network: "tcp", SockoptSettings: sock, FinalMask: mask, SniffingSettings: &d.SniffingSettings{Enabled: true, DestOverride: []string{"http", "tls"}}})
			stream := in["streamSettings"].(map[string]interface{})
			if stream["sockopt"] == nil || stream["finalmask"] == nil {
				t.Fatal("advanced options dropped")
			}
			if in["sniffing"].(map[string]interface{})["enabled"] != true {
				t.Fatal("sniffing dropped")
			}
			raw, _ := json.Marshal(stream)
			var parsed conf.StreamConfig
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatal(err)
			}
			built, err := parsed.Build()
			if err != nil {
				t.Fatal(err)
			}
			options := built.SocketSettings.CustomSockopt
			if options[0].Type != "int" || options[0].Level != "0" || options[1].Type != "str" {
				t.Fatalf("invalid custom options: %v", options)
			}
		})
	}
	hy := b.convertOutbound(&d.Outbound{Tag: "hy", Protocol: "hysteria2", Address: "192.0.2.1", Port: 443,
		HysteriaSettings: &d.HysteriaSettings{Auth: "demo", Up: "10 mbps", Down: "20 mbps"}, TLSSettings: &d.TLSSettings{PinnedPeerCertSha256: strings.Repeat("01", 32)}, SockoptSettings: sock, FinalMask: mask})
	stream := hy["streamSettings"].(map[string]interface{})
	tls := stream["tlsSettings"].(map[string]interface{})
	if tls["pinnedPeerCertSha256"] == nil {
		t.Fatal("pin-only TLS dropped")
	}
	quic := stream["finalmask"].(map[string]interface{})["quicParams"].(map[string]interface{})
	if quic["congestion"] != "brutal" || quic["brutalUp"] != "10 mbps" || quic["brutalDown"] != "20 mbps" {
		t.Fatalf("legacy QUIC knobs lost: %v", quic)
	}
	raw, _ := json.Marshal(stream)
	var parsed conf.StreamConfig
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, err := parsed.Build(); err != nil {
		t.Fatal(err)
	}
	mask.QuicParams = json.RawMessage(`{"congestion":"bbr","brutalUp":"99 mbps"}`)
	overridden := b.convertOutbound(&d.Outbound{Protocol: "hysteria2", FinalMask: mask, HysteriaSettings: &d.HysteriaSettings{Congestion: "brutal", Up: "10 mbps"}})
	quic = overridden["streamSettings"].(map[string]interface{})["finalmask"].(map[string]interface{})["quicParams"].(map[string]interface{})
	if quic["congestion"] != "bbr" || quic["brutalUp"] != "99 mbps" {
		t.Fatal("explicit finalmask overwritten")
	}
	mask.QuicParams = json.RawMessage(`"invalid"`)
	invalid := b.convertOutbound(&d.Outbound{Protocol: "hysteria2", FinalMask: mask, HysteriaSettings: &d.HysteriaSettings{Up: "10 mbps"}})
	raw, _ = json.Marshal(invalid["streamSettings"])
	if err := json.Unmarshal(raw, &conf.StreamConfig{}); err == nil {
		t.Fatal("legacy Hysteria controls hid malformed explicit QUIC parameters")
	}
	socks := &d.Outbound{Protocol: "socks", SOCKSSettings: &d.SOCKSSettings{Auth: "noauth", Accounts: []d.SOCKSAccount{{User: "stale", Pass: "credential"}}}}
	servers := b.buildSOCKSOutboundSettings(socks)["servers"].([]map[string]interface{})
	if servers[0]["users"] != nil {
		t.Fatal("noauth retained credentials")
	}
	socks.SOCKSSettings.Auth = "password"
	if b.buildSOCKSOutboundSettings(socks)["servers"].([]map[string]interface{})[0]["users"] == nil {
		t.Fatal("password auth lost")
	}
}

func TestPanelSettingsReverseRulesPrecedeCatchAll(t *testing.T) {
	first, second := uint(2), uint(3)
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).WithReverseProxies([]*d.ReverseProxy{{Tag: "bridge", Type: "bridge", Domain: "internal.test", Rule1ID: &first, Rule2ID: &second}}).
		WithRoutingRules([]*d.RoutingRule{
			{ID: 1, Enabled: true, OutboundTag: "direct", NetworkRules: []string{"tcp", "udp"}},
			{ID: 2, Enabled: true, OutboundTag: "tunnel", InboundTags: []string{"bridge"}, DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "internal.test"}}},
			{ID: 3, Enabled: true, OutboundTag: "user-exit", InboundTags: []string{"bridge"}},
		})
	c := b.buildOrderedConfig()
	for host, want := range map[string]string{"internal.test": "user-exit", "external.test": "user-exit"} {
		tag, _ := firstPanelRoute(t, c, "bridge", "", host)
		if tag != want {
			t.Fatalf("%s routed to %s, want %s", host, tag, want)
		}
	}
	tag, _ := firstPanelRoute(t, c, "ordinary", "", "external.test")
	if tag != "direct" {
		t.Fatal("ordinary fallback changed")
	}
}

// Instantiate real core features so generated loopback handlers, routing,
// defaults, and balancers are checked together for startup compatibility.
func TestPanelSettingsGeneratedConfigStarts(t *testing.T) {
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
		WithUsers(map[string][]*User{"in": {{Email: "limited@test", Level: 1}}}).
		WithBalancingRules([]*d.BalancingRule{{Tag: "pool", Enabled: true, Strategy: "roundrobin", OutboundSelectors: []string{"direct"}}}).
		WithRoutingRules([]*d.RoutingRule{{Enabled: true, BalancingTag: "pool", DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "balanced.test"}}}}).
		WithReverseProxies(nil)
	raw, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	var parsed conf.Config
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	cfg, err := parsed.Build()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := core.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
}

type panelTestHandler struct {
	fo.Handler
	tag string
}

func (h *panelTestHandler) Tag() string { return h.tag }
func TestPanelSettingsBalancerPrefixIsolation(t *testing.T) {
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
		WithOutbounds([]*d.Outbound{{Tag: "portal-extra", Protocol: "freedom"}}).
		WithUsers(map[string][]*User{"in": {{Email: "limited@test", Level: 1}}}).
		WithBalancingRules([]*d.BalancingRule{{Tag: "pool", Enabled: true, Strategy: "roundrobin", OutboundSelectors: []string{"portal"}}}).
		WithRoutingRules([]*d.RoutingRule{{Enabled: true, BalancingTag: "pool", NetworkRules: []string{"tcp", "udp"}}}).
		WithReverseProxies([]*d.ReverseProxy{{Type: "portal", Tag: "portal", Domain: "internal.test"}})
	c := b.buildOrderedConfig()
	mgr, err := om.New(context.Background(), &proxyman.OutboundConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range c.Outbounds {
		if err := mgr.AddHandler(context.Background(), &panelTestHandler{tag: out["tag"].(string)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mgr.AddHandler(context.Background(), &panelTestHandler{tag: "portal"}); err != nil {
		t.Fatal(err)
	}
	for _, bal := range c.Routing["balancers"].([]map[string]interface{}) {
		selected := mgr.Select(bal["selector"].([]string))
		if len(selected) != 2 {
			t.Fatalf("balancer %s selects %v, want exactly two candidates", bal["tag"], selected)
		}
		if bal["tag"] != "pool" {
			for _, tag := range selected {
				if strings.HasPrefix(tag, "portal") {
					t.Fatalf("tier selected unmarked original %s", tag)
				}
			}
		}
	}
}

func TestPanelSettingsVLESSReverseMigration(t *testing.T) {
	control, traffic := uint(1), uint(2)
	bridge := &d.ReverseProxy{Type: "bridge", Tag: "bridge", Domain: "legacy.test", InterconnectionTag: "tunnel", OutboundTag: "direct", Rule1ID: &control, Rule2ID: &traffic}
	portal := &d.ReverseProxy{Type: "portal", Tag: "portal", Domain: "legacy.test", InterconnectionTags: []string{"tunnel-in"}, InboundTags: []string{"public-in"}}
	b := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
		WithInbounds([]*d.Inbound{{Tag: "tunnel-in", Protocol: "vless", Listen: "127.0.0.1", Port: 12345}, {Tag: "public-in", Protocol: "socks", Listen: "127.0.0.1", Port: 12346}}).
		WithOutbounds([]*d.Outbound{{Tag: "tunnel", Protocol: "vless", Address: "127.0.0.1", Port: 12345, Network: "tcp", VLESSSettings: &d.VLESSSettings{UUID: "test-user", Encryption: "none"}}, {Tag: "direct", Protocol: "freedom", FreedomSettings: &d.FreedomSettings{FinalRules: []d.FreedomFinalRule{{Action: "allow", IP: []string{"127.0.0.1/32"}}}}}}).
		WithUsers(map[string][]*User{"tunnel-in": {{Email: "bridge@test", UUID: "test-user"}}}).
		WithReverseProxies([]*d.ReverseProxy{bridge, portal}).
		WithRoutingRules([]*d.RoutingRule{{ID: 1, Enabled: true, RuleTag: "legacy-control", OutboundTag: "tunnel", InboundTags: []string{"bridge"}, DomainRules: d.DomainMatcherSlice{{Type: "full", Value: "legacy.test"}}}, {ID: 2, Enabled: true, OutboundTag: "direct", InboundTags: []string{"bridge"}}})
	raw, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	var parsed conf.Config
	if err = json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Reverse != nil {
		t.Fatal("removed top-level reverse still emitted")
	}
	if _, err = parsed.Build(); err != nil {
		t.Fatal(err)
	}
	c := b.buildOrderedConfig()
	out := panelOutbound(t, c, "tunnel")["settings"].(map[string]interface{})
	if out["reverse"].(map[string]interface{})["tag"] != "bridge" || out["vnext"] != nil {
		t.Fatal("bridge didn't use simplified VLESS Reverse settings")
	}
	clients := c.Inbounds[0]["settings"].(map[string]interface{})["clients"].([]interface{})
	if clients[0].(map[string]interface{})["reverse"].(map[string]interface{})["tag"] != "portal" {
		t.Fatal("portal user permission missing")
	}
	if c.XCoreHub["reversePortals"].(map[string]string)["tunnel-in"] != "portal" {
		t.Fatal("API metadata missing")
	}
	for _, rule := range c.Routing["rules"].([]map[string]interface{}) {
		if rule["ruleTag"] == "legacy-control" {
			t.Fatal("legacy control rule retained")
		}
	}
	// Unsupported legacy interconnections are surfaced before a node push.
	b.outbounds[0].Protocol = "socks"
	if _, err = b.Build(); err == nil || !strings.Contains(err.Error(), "VLESS") {
		t.Fatalf("non-VLESS interconnection accepted: %v", err)
	}
}

// The verification stays on loopback: a real VLESS reverse control connection
// carries a TCP request from the portal, through the bridge, to an echo server.
func TestPanelSettingsVLESSReverseTunnel(t *testing.T) {
	reserve, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tunnelPort := reserve.Addr().(*stdnet.TCPAddr).Port
	reserve.Close()
	echo, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		conn, err := echo.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(8 * time.Second))
		io.Copy(conn, conn)
	}()
	routeID := uint(1)
	portalBuilder := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
		WithInbounds([]*d.Inbound{{Tag: "tunnel-in", Protocol: "vless", Listen: "127.0.0.1", Port: tunnelPort, Network: "tcp"}}).
		WithUsers(map[string][]*User{"tunnel-in": {{UUID: "reverse-test-user", Email: "bridge@test"}}}).
		WithReverseProxies([]*d.ReverseProxy{{Type: "portal", Tag: "portal", InterconnectionTags: []string{"tunnel-in"}, InboundTags: []string{"tunnel-in"}, Rule2ID: &routeID}}).
		WithRoutingRules([]*d.RoutingRule{{ID: 1, Enabled: true, OutboundTag: "portal", InboundTags: []string{"tunnel-in"}}})
	bridgeBuilder := NewFullConfigBuilder(&d.Node{}).WithAPI(false, 0).
		WithOutbounds([]*d.Outbound{{Tag: "tunnel-out", Protocol: "vless", Address: "127.0.0.1", Port: tunnelPort, Network: "tcp", VLESSSettings: &d.VLESSSettings{UUID: "reverse-test-user", Encryption: "none"}}, {Tag: "direct", Protocol: "freedom", FreedomSettings: &d.FreedomSettings{FinalRules: []d.FreedomFinalRule{{Action: "allow", IP: []string{"127.0.0.1/32"}}}}}}).
		WithReverseProxies([]*d.ReverseProxy{{Type: "bridge", Tag: "bridge", InterconnectionTag: "tunnel-out", OutboundTag: "direct", Rule2ID: &routeID}}).
		WithRoutingRules([]*d.RoutingRule{{ID: 1, Enabled: true, OutboundTag: "direct", InboundTags: []string{"bridge"}}})
	start := func(b *FullConfigBuilder) *core.Instance {
		t.Helper()
		raw, err := b.Build()
		if err != nil {
			t.Fatal(err)
		}
		var parsed conf.Config
		if err = json.Unmarshal([]byte(raw), &parsed); err != nil {
			t.Fatal(err)
		}
		cfg, err := parsed.Build()
		if err != nil {
			t.Fatal(err)
		}
		instance, err := core.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { instance.Close() })
		if err := instance.Start(); err != nil {
			t.Fatal(err)
		}
		return instance
	}
	portalInstance := start(portalBuilder)
	start(bridgeBuilder)
	manager := portalInstance.GetFeature(fo.ManagerType()).(fo.Manager)
	deadline := time.Now().Add(8 * time.Second)
	for manager.GetHandler("portal") == nil {
		if time.Now().After(deadline) {
			t.Fatal("reverse control tunnel didn't register its portal")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = session.ContextWithInbound(ctx, &session.Inbound{Tag: "tunnel-in"})
	conn, err := core.Dial(ctx, portalInstance, net.TCPDestination(net.ParseAddress("127.0.0.1"), net.Port(echo.Addr().(*stdnet.TCPAddr).Port)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Xray's synthetic net.Conn has no deadline implementation; bound the
	// exchange explicitly so a routing regression cannot hang the test suite.
	result := make(chan error, 1)
	go func() {
		if _, err := conn.Write([]byte("ping")); err != nil {
			result <- err
			return
		}
		reply := make([]byte, 4)
		_, err := io.ReadFull(conn, reply)
		if err == nil && string(reply) != "ping" {
			err = fmt.Errorf("reverse tunnel returned %q", reply)
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		conn.Close()
		t.Fatal("reverse tunnel exchange timed out")
	}
}
