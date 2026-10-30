// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/xtls/xray-core/app/dns"
	xlog "github.com/xtls/xray-core/app/log"
	"github.com/xtls/xray-core/app/policy"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/geodata"
	clog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/proxy/freedom"
	vlessout "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

func TestBuildRejectsWhatItCannotRun(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*bridgespec.Spec)
		want   string
	}{
		{"invalid spec", func(s *bridgespec.Spec) { s.Nodes[0].UUID = "nope" }, "UUID"},
		{"unsafe fingerprint", func(s *bridgespec.Spec) { s.Nodes[1].Fingerprint = "unsafe" }, "fingerprint"},
		{"go's own fingerprint", func(s *bridgespec.Spec) { s.Nodes[1].Fingerprint = "hellogolang" }, "fingerprint"},
		{"unknown fingerprint", func(s *bridgespec.Spec) { s.Nodes[1].Fingerprint = "netscape" }, "fingerprint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testSpec()
			tt.mutate(s)
			_, err := buildConfig(s)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error about %q", err, tt.want)
			}
		})
	}
}

func TestBuildStructure(t *testing.T) {
	s := testSpec()
	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbound) != 0 {
		t.Errorf("%d inbounds: everything arrives through the tunnels", len(cfg.Inbound))
	}

	// The exit is first, so what no rule claims lands behind the LAN block.
	var tags []string
	for _, o := range cfg.Outbound {
		tags = append(tags, o.Tag)
	}
	if got := strings.Join(tags, ","); got != "exit,rvs-link-1,rvs-link-2" {
		t.Errorf("outbounds %s", got)
	}

	exit := senderOf(t, cfg.Outbound[0])
	if got := exit.Via.GetIp(); !net.IP(got).Equal(net.ParseIP(s.Exit.Addr)) {
		t.Errorf("exit sends from %v, want %s", net.IP(got), s.Exit.Addr)
	}
	for i, n := range s.Nodes {
		sender := senderOf(t, cfg.Outbound[i+1])
		if got := sender.Via.GetIp(); !net.IP(got).Equal(net.ParseIP(s.DialAddr)) {
			t.Errorf("%s dials from %v, want %s", n.Tag, net.IP(got), s.DialAddr)
		}
		if sender.StreamSettings.ProtocolName != "tcp" || !strings.HasSuffix(sender.StreamSettings.SecurityType, "reality.Config") {
			t.Errorf("%s is not REALITY over tcp: %+v", n.Tag, sender.StreamSettings)
		}
		out := instance(t, cfg.Outbound[i+1].ProxySettings).(*vlessout.Config)
		if out.Vnext.Port != uint32(n.Port) || !net.IP(out.Vnext.Address.GetIp()).Equal(net.ParseIP(n.Addr)) {
			t.Errorf("%s dials %v:%d, want %s:%d", n.Tag, net.IP(out.Vnext.Address.GetIp()), out.Vnext.Port, n.Addr, n.Port)
		}
	}

	// The apps, in the order the loader would put them.
	var apps []string
	for _, a := range cfg.App {
		apps = append(apps, strings.TrimPrefix(a.Type, "xray.app."))
	}
	want := "log.Config,dispatcher.Config,proxyman.InboundConfig,proxyman.OutboundConfig,stats.Config,router.Config,dns.Config,policy.Config"
	if got := strings.Join(apps, ","); got != want {
		t.Errorf("apps %s\nwant %s", got, want)
	}
}

// Everything that can arrive goes to the exit: the dns tag first, then each
// node's reverse tag.
func TestRoutesSendEverythingToTheExit(t *testing.T) {
	s := testSpec()
	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	rc := instance(t, cfg.App[5]).(*router.Config)
	var inbound []string
	for _, r := range rc.Rule {
		if r.GetTag() != bridgespec.ExitTag {
			t.Errorf("rule for %v goes to %q, not the exit", r.InboundTag, r.GetTag())
		}
		if len(r.InboundTag) != 1 || r.Domain != nil || r.Ip != nil || r.PortList != nil || r.Networks != nil || r.UserEmail != nil {
			t.Errorf("rule %v matches on more than its inbound tag: %+v", r.InboundTag, r)
		}
		inbound = append(inbound, r.InboundTag...)
	}
	if got, want := strings.Join(inbound, ","), "dns,rvs-from-n1,rvs-from-n2"; got != want {
		t.Errorf("rules for %s, want %s", got, want)
	}
	if len(rc.BalancingRule) != 0 || rc.DomainStrategy != router.Config_AsIs {
		t.Errorf("router has more than rules: %+v", rc)
	}
}

func finalRules(t *testing.T, s *bridgespec.Spec) []*freedom.FinalRuleConfig {
	t.Helper()
	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	return instance(t, cfg.Outbound[0].ProxySettings).(*freedom.Config).FinalRules
}

// The exit's rules, run through the matcher Xray itself builds from them:
// private space is refused, public addresses are not.
func TestExitBlocksPrivateSpace(t *testing.T) {
	rules := finalRules(t, testSpec())
	if len(rules) != 2 {
		t.Fatalf("%d rules, want a block and an allow", len(rules))
	}
	if rules[0].Action != freedom.RuleAction_Block || len(rules[0].Networks) != 0 || rules[0].PortList != nil {
		t.Fatalf("first rule is not a plain block: %+v", rules[0])
	}
	// Xray blocks all that a reverse tunnel carries unless a rule allows it.
	if rules[1].Action != freedom.RuleAction_Allow || len(rules[1].Ip) != 0 || len(rules[1].Networks) != 0 || rules[1].PortList != nil {
		t.Fatalf("last rule is not an allow of everything: %+v", rules[1])
	}
	matcher, err := geodata.IPReg.BuildIPMatcher(rules[0].Ip)
	if err != nil {
		t.Fatal(err)
	}
	blocked := func(s string) bool { return matcher.Match(xnet.ParseAddress(s).IP()) }
	for _, ip := range []string{
		"10.0.0.1", "10.255.255.255", "172.16.0.1", "172.31.255.255", "192.168.0.1", "192.168.50.17", "192.168.88.1",
		"100.64.0.1", "127.0.0.1", "127.255.255.255", "169.254.169.254", "0.0.0.0", "224.0.0.1", "239.255.255.250",
		"255.255.255.255", "192.0.2.1", "198.51.100.7", "203.0.113.9", "198.18.0.1",
		"::", "::1", "fe80::1", "fd00::1", "fc00::1", "ff02::1",
		// Xray folds a mapped address into IPv4 before it matches.
		"::ffff:10.0.0.1", "::ffff:192.168.1.1", "::ffff:127.0.0.1",
	} {
		if !blocked(ip) {
			t.Errorf("%s is not blocked", ip)
		}
	}
	for _, ip := range []string{
		"1.1.1.1", "8.8.8.8", "9.9.9.9", "93.184.216.34", "172.15.255.255", "172.32.0.1", "192.167.255.255",
		"192.169.0.1", "100.63.255.255", "100.128.0.1", "223.255.255.255", "198.17.255.255", "198.20.0.1",
		"2606:4700:4700::1111", "2001:4860:4860::8888", "::ffff:8.8.8.8",
	} {
		if blocked(ip) {
			t.Errorf("%s is blocked", ip)
		}
	}
}

func TestExitBlocksWhatTheSpecSays(t *testing.T) {
	s := testSpec()
	s.Exit.Blocked = []string{"10.0.0.0/8", "fc00::/7"}
	s.DNS.Servers = []string{"1.1.1.1"}
	matcher, err := geodata.IPReg.BuildIPMatcher(finalRules(t, s)[0].Ip)
	if err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{"10.1.1.1": true, "fd00::1": true, "192.168.1.1": false, "127.0.0.1": false, "8.8.8.8": false} {
		if got := matcher.Match(xnet.ParseAddress(ip).IP()); got != want {
			t.Errorf("%s blocked = %v, want %v", ip, got, want)
		}
	}
}

// The exit resolves names with Xray's resolver and never falls back to the
// system's, whose lookups would take the container's own route.
func TestExitResolvesWithXray(t *testing.T) {
	for _, tt := range []struct {
		name     string
		exit     string
		dial     string
		portal   string
		servers  []string
		strategy internet.DomainStrategy
		query    dns.QueryStrategy
	}{
		{"IPv4", "192.168.50.17", "192.168.50.18", "203.0.113.10", []string{"1.1.1.1"}, internet.DomainStrategy_FORCE_IP4, dns.QueryStrategy_USE_IP4},
		{"IPv6", "fd00::17", "fd00::18", "2001:db8::1", []string{"2606:4700:4700::1111"}, internet.DomainStrategy_FORCE_IP6, dns.QueryStrategy_USE_IP6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testSpec()
			s.Exit.Addr, s.DialAddr = tt.exit, tt.dial
			s.Nodes = s.Nodes[:1]
			s.Nodes[0].Addr = tt.portal
			s.DNS.Servers = tt.servers
			cfg, err := buildConfig(s)
			if err != nil {
				t.Fatal(err)
			}
			if got := senderOf(t, cfg.Outbound[0]).StreamSettings.SocketSettings.DomainStrategy; got != tt.strategy {
				t.Errorf("exit domain strategy %v, want %v", got, tt.strategy)
			}
			d := instance(t, cfg.App[6]).(*dns.Config)
			if d.QueryStrategy != tt.query || d.Tag != bridgespec.DNSTag || len(d.NameServer) != 1 {
				t.Errorf("dns %+v", d)
			}
			if got := d.NameServer[0].Address; got.Network != xnet.Network_UDP || got.Port != 53 {
				t.Errorf("name server %+v", got)
			}
		})
	}
}

func TestDNSServers(t *testing.T) {
	s := testSpec()
	s.DNS.Servers = []string{"1.1.1.1", "9.9.9.9:5353", "8.8.8.8:53"}
	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	d := instance(t, cfg.App[6]).(*dns.Config)
	var got []string
	for _, ns := range d.NameServer {
		got = append(got, xnet.IPAddress(ns.Address.Address.GetIp()).String()+":"+strconv.Itoa(int(ns.Address.Port)))
		if ns.PolicyID != 1 {
			t.Errorf("policy id %d", ns.PolicyID)
		}
	}
	if want := "1.1.1.1:53 9.9.9.9:5353 8.8.8.8:53"; strings.Join(got, " ") != want {
		t.Errorf("servers %v, want %s", got, want)
	}
}

// Each tunnel's counters are the node's: the outbound's user is the tag, and
// level 0, where the reverse flows run, counts both directions.
func TestCountersByNode(t *testing.T) {
	s := testSpec()
	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range s.Nodes {
		out := instance(t, cfg.Outbound[i+1].ProxySettings).(*vlessout.Config)
		if out.Vnext.User.Email != n.Tag || out.Vnext.User.Level != 0 {
			t.Errorf("%s: user %q at level %d", n.Tag, out.Vnext.User.Email, out.Vnext.User.Level)
		}
	}
	p := instance(t, cfg.App[7]).(*policy.Config)
	if len(p.Level) != 1 || !p.Level[0].Stats.UserUplink || !p.Level[0].Stats.UserDownlink || p.Level[0].Stats.UserOnline {
		// UserOnline reads a client address the reverse flows don't carry.
		t.Errorf("policy %+v", p)
	}
}

func TestLogLevels(t *testing.T) {
	for level, want := range map[string]struct {
		severity clog.Severity
		errors   xlog.LogType
	}{
		"debug":   {clog.Severity_Debug, xlog.LogType_Console},
		"info":    {clog.Severity_Info, xlog.LogType_Console},
		"warning": {clog.Severity_Warning, xlog.LogType_Console},
		"":        {clog.Severity_Warning, xlog.LogType_Console},
		"error":   {clog.Severity_Error, xlog.LogType_Console},
		"none":    {0, xlog.LogType_None},
	} {
		s := testSpec()
		s.LogLevel = level
		cfg, err := buildConfig(s)
		if err != nil {
			t.Fatal(err)
		}
		l := instance(t, cfg.App[0]).(*xlog.Config)
		if l.ErrorLogLevel != want.severity || l.ErrorLogType != want.errors {
			t.Errorf("level %q: %v / %v, want %v / %v", level, l.ErrorLogLevel, l.ErrorLogType, want.severity, want.errors)
		}
		// Whatever the level, no access log: it would list every destination.
		if l.AccessLogType != xlog.LogType_None {
			t.Errorf("level %q keeps an access log", level)
		}
	}
}

func TestSendersAreBoundToTheirAddresses(t *testing.T) {
	s := testSpec()
	cfg, err := buildConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range cfg.Outbound {
		sender := senderOf(t, o)
		if sender.Via == nil || sender.ViaCidr != "" {
			t.Errorf("%s has no plain source address: %+v", o.Tag, sender.Via)
		}
	}
}
