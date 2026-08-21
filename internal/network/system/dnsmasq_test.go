package system

import (
	"context"
	"strings"
	"testing"
)

func lanDNSConfig() DNSMasqConfig {
	return DNSMasqConfig{
		BridgeName: "lan0", ListenAddr: "10.77.0.1",
		RangeLow: "10.77.0.100", RangeHigh: "10.77.0.200", LeaseHours: 12,
		DomesticServer: "217.218.127.127", DomesticSuffix: "ir", DomesticIfName: "enp1s0",
		Foreign: []ForeignServer{{Server: "1.1.1.1", IfName: "enp2s0"}},
	}
}

// bind-interfaces binds 10.77.0.1:53 and nothing else, leaving resolved's stub
// alone — which is why DNSStubListener=no must never be set.
func TestRenderDNSMasq_BindsOnlyTheLANAddress(t *testing.T) {
	got := RenderDNSMasq(lanDNSConfig())
	for _, want := range []string{
		"interface=lan0",
		"bind-interfaces",
		"except-interface=lo",
		"listen-address=10.77.0.1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "DNSStubListener") {
		t.Error("dnsmasq config mentions DNSStubListener; that setting must never be used")
	}
}

func TestRenderDNSMasq_DHCPRange(t *testing.T) {
	got := RenderDNSMasq(lanDNSConfig())
	for _, want := range []string{
		"dhcp-range=10.77.0.100,10.77.0.200,12h",
		"dhcp-option=option:dns-server,10.77.0.1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// @interface binds the query to that link: the socket hits the oif rules at pref
// 20/21 and leaves by that uplink. That is what makes the split real.
func TestRenderDNSMasq_SplitDNSIsPerInterface(t *testing.T) {
	got := RenderDNSMasq(lanDNSConfig())
	for _, want := range []string{
		"server=/ir/217.218.127.127@enp1s0",
		"server=1.1.1.1@enp2s0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The suffix-scoped server must precede the default, or the default wins.
	if strings.Index(got, "server=/ir/") > strings.Index(got, "server=1.1.1.1") {
		t.Error("the default server precedes the suffix-scoped one")
	}
}

// Without no-resolv, foreign names leak to the domestic resolver.
func TestRenderDNSMasq_NeverFallsBackToResolvConf(t *testing.T) {
	withTunnel := RenderDNSMasq(lanDNSConfig())
	if !strings.Contains(withTunnel, "no-resolv") {
		t.Errorf("missing no-resolv in:\n%s", withTunnel)
	}

	c := lanDNSConfig()
	c.Foreign = nil
	noTunnel := RenderDNSMasq(c)
	if !strings.Contains(noTunnel, "no-resolv") {
		t.Errorf("with no tunnel, foreign lookups fall back to the system resolver:\n%s", noTunnel)
	}
	if strings.Contains(noTunnel, "\nserver=1.1.1.1") {
		t.Errorf("a foreign server was rendered with no tunnel to carry it:\n%s", noTunnel)
	}
}

// Emitted only when the caller feature-detected support, so the config still
// degrades cleanly on a build without it.
func TestRenderDNSMasq_NftsetOnlyWhenEnabled(t *testing.T) {
	off := RenderDNSMasq(lanDNSConfig())
	if strings.Contains(off, "nftset") {
		t.Errorf("emitted nftset without support being detected:\n%s", off)
	}

	c := lanDNSConfig()
	c.NftSetSupported = true
	c.DomainSets = []DomainSet{
		{Suffix: "ir", V4Set: "ir_dom_v4", V6Set: "ir_dom_v6"},
	}
	on := RenderDNSMasq(c)
	for _, want := range []string{
		"nftset=/ir/inet#nasnet#ir_dom_v4",
		"nftset=/ir/6#inet#nasnet#ir_dom_v6",
	} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q in:\n%s", want, on)
		}
	}
}

// Dropped silently, not an error: the geoip sets still cover the traffic.
func TestRenderDNSMasq_DomainSetsIgnoredWithoutSupport(t *testing.T) {
	c := lanDNSConfig()
	c.NftSetSupported = false
	c.DomainSets = []DomainSet{{Suffix: "ir", V4Set: "ir_dom_v4"}}
	if strings.Contains(RenderDNSMasq(c), "nftset") {
		t.Error("domain sets leaked into a config for a build with no nftset support")
	}
}

// Feature-detect, never version-sniff.
func TestNftSetSupported_FeatureDetects(t *testing.T) {
	if NftSetSupported(context.Background(), "/nonexistent/dnsmasq") {
		t.Error("NftSetSupported returned true for a missing binary")
	}
}

func TestRenderDNSMasq_MissingUplinkOmitsItsServer(t *testing.T) {
	c := lanDNSConfig()
	c.DomesticIfName = ""
	c.DomesticServer = ""
	got := RenderDNSMasq(c)
	if strings.Contains(got, "server=/ir/") {
		t.Errorf("emitted a domestic server with no domestic uplink:\n%s", got)
	}
	if !strings.Contains(got, "server=1.1.1.1@enp2s0") {
		t.Error("the foreign server disappeared")
	}
}

// A unit that exists but is not running serves nothing, so the two states have
// to be reported apart: one is "install the package", the other is "it died".
func TestDNSMasqStatus_SeparatesInstalledFromRunning(t *testing.T) {
	d := &DNSMasq{Bin: "/nonexistent/dnsmasq"}
	st := d.Status(context.Background())
	// With no systemctl (tests, containers) we cannot tell, and must not claim
	// the LAN is broken.
	if !st.Installed {
		t.Errorf("Status = %+v; an undetectable unit must not read as missing", st)
	}
}

// The device list reads the lease file, so the writer must name the same path
// the reader opens rather than relying on a distro default.
// One resolver line per pool member, each bound to its own tunnel, so a dead
// member's resolver just times out and dnsmasq shifts to a sibling.
func TestRenderDNSMasq_OneForeignServerPerTunnel(t *testing.T) {
	c := lanDNSConfig()
	c.Foreign = []ForeignServer{
		{Server: "10.64.0.1", IfName: "nasnet-wg0"},
		{Server: "1.1.1.1", IfName: "nasnet-wg1"},
	}
	out := RenderDNSMasq(c)
	for _, want := range []string{
		"server=10.64.0.1@nasnet-wg0\n",
		"server=1.1.1.1@nasnet-wg1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderDNSMasq_PinsTheLeaseFile(t *testing.T) {
	out := RenderDNSMasq(DNSMasqConfig{
		BridgeName: "lan0", ListenAddr: "10.77.0.1",
		RangeLow: "10.77.0.100", RangeHigh: "10.77.0.200",
	})
	if !strings.Contains(out, "dhcp-leasefile="+LeasePath) {
		t.Errorf("lease file not pinned:\n%s", out)
	}
}
