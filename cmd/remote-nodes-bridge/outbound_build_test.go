// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/app/policy"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/geodata"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"google.golang.org/protobuf/proto"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

func routerOf(t *testing.T, cfg *core.Config) *router.Config {
	t.Helper()
	for _, app := range cfg.App {
		if rc, ok := instance(t, app).(*router.Config); ok {
			return rc
		}
	}
	t.Fatal("no router in the config")
	return nil
}

func TestBuildRefusesOutboundsItCannotRun(t *testing.T) {
	all := testOutbounds()
	tests := []struct {
		name string
		base string
		edit func(*bridgespec.Outbound)
		want string
	}{
		{"an invalid outbound", "trojan", func(o *bridgespec.Outbound) { o.Password = "" }, "password"},
		{"a fingerprint Xray doesn't know", "trojan", func(o *bridgespec.Outbound) { o.Fingerprint = "netscape" }, "fingerprint"},
		{"go's own fingerprint over reality", "vless over reality", func(o *bridgespec.Outbound) { o.Fingerprint = "hellogolang" }, "fingerprint"},
		{"no fingerprint at all over reality", "vless over reality", func(o *bridgespec.Outbound) { o.Fingerprint = "unsafe" }, "fingerprint"},
		{"a spider path that is no path", "vless over reality", func(o *bridgespec.Outbound) { o.SpiderX = "/%zz" }, "spider_x"},
		{"plain vless to a public address", "vless over httpupgrade, plain", func(o *bridgespec.Outbound) { o.Addr = "198.51.100.7" }, "private address"},
		{"plain trojan to a public address", "trojan", func(o *bridgespec.Outbound) {
			o.Security, o.ServerName = "", ""
		}, "private address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := all[tt.base]
			tt.edit(&o)
			_, err := buildConfig(outboundSpec(o), "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error about %q", err, tt.want)
			}
		})
	}
}

// What the bridge refuses over a plain connection, the loader refuses too,
// and only that: to a private, loopback or link-local address it is allowed.
func TestPlainLoginsGoOnlyToPrivateAddressesLikeTheLoader(t *testing.T) {
	plain := map[string]bridgespec.Outbound{
		"vless":  {Protocol: bridgespec.ProtoVLESS, Port: 80, ID: outboundID},
		"trojan": {Protocol: bridgespec.ProtoTrojan, Port: 80, Password: "secret"},
		// nothing for the loader to refuse
		"vmess": {Protocol: bridgespec.ProtoVMess, Port: 80, ID: outboundID},
	}
	for name, o := range plain {
		for addr, private := range map[string]bool{
			"198.51.100.7": false, "8.8.8.8": false, "2001:db8::7": false,
			"192.168.88.7": true, "10.1.2.3": true, "172.16.0.9": true, "127.0.0.1": true, "169.254.1.1": true, "fd00::7": true,
		} {
			t.Run(name+" to "+addr, func(t *testing.T) {
				o.Tag, o.Addr = "out-1.", addr
				_, mine := outboundHandler(o, netip.MustParseAddr("192.168.150.80"))
				data, err := json.Marshal(outboundJSON(o, ""))
				if err != nil {
					t.Fatal(err)
				}
				var detour conf.OutboundDetourConfig
				if err := json.Unmarshal(data, &detour); err != nil {
					t.Fatal(err)
				}
				_, theirs := detour.Build()
				refused := !private && o.Protocol != bridgespec.ProtoVMess
				if (mine != nil) != refused || (theirs != nil) != refused {
					t.Errorf("refused by the bridge: %v, by the loader: %v; want refused = %v", mine, theirs, refused)
				}
			})
		}
	}
}

func TestBuildStructureWithOutbounds(t *testing.T) {
	all := testOutbounds()
	s := outboundSpec(all["socks, login"])
	o := all["trojan"]
	o.Tag, o.Fallback = "out-2.", true
	s.Outbounds = append(s.Outbounds, o)
	s.Nodes[1].Via = o.Tag
	s.Probe = bridgespec.Probe{URL: "http://203.0.113.9/ok", Seconds: 45}
	cfg, err := buildConfig(s, "")
	if err != nil {
		t.Fatal(err)
	}

	// After the tunnels, so the exit stays first and takes what no rule claims.
	var tags []string
	for _, o := range cfg.Outbound {
		tags = append(tags, o.Tag)
	}
	if got := strings.Join(tags, ","); got != "exit,rvs-link-1,rvs-link-2,out-1.,out-2." {
		t.Errorf("outbounds %s", got)
	}
	for _, out := range cfg.Outbound[3:] {
		if got := senderOf(t, out).Via.GetIp(); !net.IP(got).Equal(net.ParseIP(s.OutboundAddr)) {
			t.Errorf("%s sends from %v, want the outbound address %s", out.Tag, net.IP(got), s.OutboundAddr)
		}
	}

	// The lookups, then each node: the blocked ranges to the exit, the rest to
	// its outbound, or to the group of one with a fallback.
	rc := routerOf(t, cfg)
	type rule struct{ from, to string }
	var got []rule
	for _, r := range rc.Rule {
		to := r.GetTag()
		if r.GetBalancingTag() != "" {
			to = "group " + r.GetBalancingTag()
		}
		if len(r.Ip) > 0 {
			to += " (blocked ranges)"
		}
		got = append(got, rule{r.InboundTag[0], to})
	}
	want := []rule{
		{"dns", "exit"},
		{"rvs-from-n1", "exit (blocked ranges)"}, {"rvs-from-n1", "out-1."},
		{"rvs-from-n2", "exit (blocked ranges)"}, {"rvs-from-n2", "group group-out-2."},
	}
	if len(got) != len(want) {
		t.Fatalf("rules %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d: %v, want %v", i, got[i], want[i])
		}
	}
	if len(rc.BalancingRule) != 1 {
		t.Fatalf("groups %v", rc.BalancingRule)
	}
	g := rc.BalancingRule[0]
	if g.Tag != "group-out-2." || len(g.OutboundSelector) != 1 || g.OutboundSelector[0] != "out-2." || g.Strategy != "random" || g.FallbackTag != bridgespec.ExitTag {
		t.Errorf("group %v", g)
	}
	if rc.DomainStrategy != router.Config_AsIs {
		t.Errorf("the router resolves names (%v): the blocked ranges are matched by literal address only", rc.DomainStrategy)
	}

	var apps []string
	for _, a := range cfg.App {
		apps = append(apps, strings.TrimPrefix(a.Type, "xray.app."))
	}
	if got, want := strings.Join(apps, ","), "log.Config,dispatcher.Config,proxyman.OutboundConfig,stats.Config,router.Config,dns.Config,policy.Config,xray.core.app.observatory.Config,proxyman.InboundConfig"; got != want {
		t.Errorf("apps %s\nwant %s", got, want)
	}
	obs := instance(t, cfg.App[7]).(*observatory.Config)
	if strings.Join(obs.SubjectSelector, ",") != "out-1.,out-2." || obs.ProbeUrl != s.Probe.URL ||
		time.Duration(obs.ProbeInterval) != 45*time.Second || !obs.EnableConcurrency {
		t.Errorf("observatory %v", obs)
	}
	p := instance(t, cfg.App[6]).(*policy.Config)
	if st := p.GetSystem().GetStats(); st == nil || !st.OutboundUplink || !st.OutboundDownlink || st.InboundUplink || st.InboundDownlink {
		t.Errorf("system policy %v", p.System)
	}
}

// The ranges that stay with the exit are the exit's own, matched the way Xray
// matches them.
func TestBlockedRangesStayWithTheExit(t *testing.T) {
	s := outboundSpec(testOutbounds()["shadowsocks"])
	cfg, err := buildConfig(s, "")
	if err != nil {
		t.Fatal(err)
	}
	rule := routerOf(t, cfg).Rule[1]
	if rule.GetTag() != bridgespec.ExitTag || rule.InboundTag[0] != "rvs-from-n1" {
		t.Fatalf("rule %v", rule)
	}
	matcher, err := geodata.IPReg.BuildIPMatcher(rule.Ip)
	if err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{
		"10.0.0.1": true, "192.168.88.1": true, "127.0.0.1": true, "169.254.169.254": true, "fd00::1": true, "::ffff:192.168.1.1": true,
		"1.1.1.1": false, "93.184.216.34": false, "2606:4700:4700::1111": false,
	} {
		if got := matcher.Match(xnet.ParseAddress(ip).IP()); got != want {
			t.Errorf("%s stays with the exit = %v, want %v", ip, got, want)
		}
	}
	exit := instance(t, cfg.Outbound[0].ProxySettings)
	if !proto.Equal(exit, instance(t, mustBuild(t, testSpec()).Outbound[0].ProxySettings)) {
		t.Error("the exit changed with an outbound in the spec")
	}
}

// Without outbounds the config is the one the bridge built before there were
// any: a check's settings alone change nothing, and nothing of the check, the
// groups or the outbounds' counters is in it.
func TestNoOutboundsBuildsWhatItAlwaysHas(t *testing.T) {
	for name, s := range map[string]*bridgespec.Spec{"two nodes": testSpec(), "the entry": localSpec()} {
		t.Run(name, func(t *testing.T) {
			plain := mustBuildFor(t, s)
			s.Probe = bridgespec.Probe{URL: "http://203.0.113.9/ok", Seconds: 60}
			if !proto.Equal(plain, mustBuildFor(t, s)) {
				t.Error("a check's settings changed a spec with no outbound")
			}
			for _, app := range plain.App {
				if _, ok := instance(t, app).(*observatory.Config); ok {
					t.Error("a check with nothing to check")
				}
			}
			if rc := routerOf(t, plain); len(rc.BalancingRule) != 0 {
				t.Errorf("groups %v", rc.BalancingRule)
			}
			for _, app := range plain.App {
				if p, ok := instance(t, app).(*policy.Config); ok && p.System != nil {
					t.Errorf("system policy %v", p.System)
				}
			}
		})
	}
}

func mustBuild(t *testing.T, s *bridgespec.Spec) *core.Config {
	t.Helper()
	cfg, err := buildConfig(s, "")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func mustBuildFor(t *testing.T, s *bridgespec.Spec) *core.Config {
	t.Helper()
	target := ""
	if s.Local != nil {
		target = testForwarder
	}
	cfg, err := buildConfig(s, target)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Every outbound the parity tests cover makes an instance Xray accepts, so
// none of them only looks right on paper.
func TestEveryKindOfOutboundMakesAnInstance(t *testing.T) {
	for name, o := range testOutbounds() {
		t.Run(name, func(t *testing.T) {
			cfg, err := probeConfig(o, netip.MustParseAddr("192.168.150.80"))
			if err != nil {
				t.Fatal(err)
			}
			inst, err := core.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			inst.Close()
		})
	}
}

// refusedSpec is a spec with one outbound Xray takes and two it won't, one
// with a fallback and one without, a node through each, and the entry through
// the one without.
func refusedSpec() *bridgespec.Spec {
	all := testOutbounds()
	s := localSpec()
	good, fallback, none := all["socks, login"], all["trojan"], all["vless over reality"]
	good.Tag, fallback.Tag, none.Tag = "out-1.", "out-2.", "out-3."
	fallback.Fallback, fallback.Fingerprint = true, "netscape" // a fingerprint Xray doesn't know
	none.Fingerprint = "netscape"
	s.Outbounds, s.OutboundAddr = []bridgespec.Outbound{good, fallback, none}, "192.168.150.80"
	n3 := s.Nodes[1]
	n3.Tag, n3.ReverseTag, n3.Addr = "rvs-link-3", "rvs-from-n3", "198.51.100.30"
	s.Nodes = append(s.Nodes, n3)
	s.Nodes[0].Via, s.Nodes[1].Via, s.Nodes[2].Via, s.Local.Via = good.Tag, fallback.Tag, none.Tag, none.Tag
	return s
}

// Each outbound is tried on its own, and only the ones Xray won't take are
// named, with a reason an admin can read and no secret in it.
func TestRefusedOutbounds(t *testing.T) {
	s := refusedSpec()
	refused := refusedOutbounds(s)
	if len(refused) != 2 || refused["out-1."] != "" {
		t.Fatalf("refused %v, want out-2. and out-3.", refused)
	}
	for i, tag := range []string{"out-2.", "out-3."} {
		why := refused[tag]
		if !strings.HasPrefix(why, "Xray does not accept these settings: fingerprint") || !strings.HasSuffix(why, ".") {
			t.Errorf("%s: %q", tag, why)
		}
		sentence(t, why, s.Outbounds[i+1])
	}
	for name, o := range testOutbounds() {
		o.Tag = "out-1."
		if refused := refusedOutbounds(outboundSpec(o)); len(refused) != 0 {
			t.Errorf("%s: refused %v", name, refused)
		}
	}
}

// When every outbound builds, leaving none out is the config the parity tests
// hold to the loader.
func TestNothingLeftOutIsTheSameConfig(t *testing.T) {
	for name, o := range testOutbounds() {
		s := outboundSpec(o)
		s.Outbounds[0].Fallback = true
		if !proto.Equal(mustBuild(t, s), mustBuildWithout(t, s, refusedOutbounds(s))) {
			t.Errorf("%s: the config changed", name)
		}
	}
}

func mustBuildWithout(t *testing.T, s *bridgespec.Spec, refused map[string]string) *core.Config {
	t.Helper()
	target := ""
	if s.Local != nil {
		target = testForwarder
	}
	cfg, err := buildConfigWithout(s, target, refused)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// An outbound left out is in no part of the config but the rules: what was
// sent to it goes to the exit with a fallback, and to its tag without one,
// which no outbound has any more, so Xray refuses it. The blocked ranges go to
// the exit first, either way.
func TestRefusedOutboundsAreLeftOut(t *testing.T) {
	s := refusedSpec()
	if _, err := buildConfig(s, testForwarder); err == nil {
		t.Fatal("built a config with outbounds Xray won't take")
	}
	cfg := mustBuildWithout(t, s, refusedOutbounds(s))

	var tags []string
	for _, o := range cfg.Outbound {
		tags = append(tags, o.Tag)
	}
	if got := strings.Join(tags, ","); got != "exit,rvs-link-1,rvs-link-2,rvs-link-3,out-1." {
		t.Errorf("outbounds %s", got)
	}

	rc := routerOf(t, cfg)
	var got []string
	for _, r := range rc.Rule {
		to := r.GetTag()
		if len(r.Ip) > 0 {
			to += " (blocked ranges)"
		}
		got = append(got, r.InboundTag[0]+" to "+to)
	}
	want := []string{
		"dns to exit",
		"local to exit (blocked ranges)", "local to out-3.",
		"rvs-from-n1 to exit (blocked ranges)", "rvs-from-n1 to out-1.",
		"rvs-from-n2 to exit (blocked ranges)", "rvs-from-n2 to exit",
		"rvs-from-n3 to exit (blocked ranges)", "rvs-from-n3 to out-3.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rules\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(rc.BalancingRule) != 0 {
		t.Errorf("groups %v", rc.BalancingRule)
	}
	for _, app := range cfg.App {
		if obs, ok := instance(t, app).(*observatory.Config); ok && strings.Join(obs.SubjectSelector, ",") != "out-1." {
			t.Errorf("the check watches %v", obs.SubjectSelector)
		}
	}

	// With none left to check there is no check at all. No node: a tunnel's
	// outbound starts dialing as soon as it is made, which would outlive the test.
	s.Outbounds, s.Nodes = s.Outbounds[1:], nil
	cfg = mustBuildWithout(t, s, refusedOutbounds(s))
	for _, app := range cfg.App {
		if _, ok := instance(t, app).(*observatory.Config); ok {
			t.Error("a check with nothing to check")
		}
	}
	inst, err := core.New(cfg)
	if err != nil {
		t.Fatalf("Xray won't take the config with every outbound left out: %v", err)
	}
	inst.Close()
}
