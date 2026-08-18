package usecase

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// CI stubs the geoip database out. A real parse failure still fails.
func skipWithoutGeoIP(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, geoip.ErrNoEmbeddedGeoIP) {
		t.Skip("built without the embedded geoip database")
	}
}

func testLAN() domain.LANConfig {
	return domain.LANConfig{
		BridgeName: "lan0", CIDR: "10.77.0.1/24",
		DHCPRangeLow: "10.77.0.100", DHCPRangeHigh: "10.77.0.200",
		LeaseHours: 12, Enabled: true,
	}
}

func TestBuildDomesticSets_TypedIntervalSetsFromTheEmbeddedGeoIP(t *testing.T) {
	// true: probe P3 found --nftset available, so the domain sets are declared too.
	sets, err := BuildDomesticSets(true)
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatalf("BuildDomesticSets: %v", err)
	}
	byName := map[string]nft.Set{}
	for _, s := range sets {
		byName[s.Name] = s
	}
	v4, ok := byName[DomesticSetV4]
	if !ok {
		t.Fatalf("no %s set; got %v", DomesticSetV4, sets)
	}
	if v4.Family != "ipv4_addr" || !v4.Interval {
		t.Errorf("%s = family %q interval %v; a CIDR set must be an interval set",
			DomesticSetV4, v4.Family, v4.Interval)
	}
	// The dnsmasq-populated layer is plain with a timeout, not interval.
	dom, ok := byName[DomainSetV4]
	if !ok {
		t.Fatalf("no %s set with --nftset support enabled", DomainSetV4)
	}
	if dom.Interval || dom.Timeout != DomainSetTimeout || len(dom.Elements) != 0 {
		t.Errorf("%s = %+v; want plain, timeout %s, populated at runtime by dnsmasq",
			DomainSetV4, dom, DomainSetTimeout)
	}
	// With no support detected, only the geoip layer is declared.
	noNft, err := BuildDomesticSets(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, sset := range noNft {
		if sset.Name == DomainSetV4 || sset.Name == DomainSetV6 {
			t.Errorf("declared %s without --nftset support", sset.Name)
		}
	}
	if len(v4.Elements) < 100 {
		t.Errorf("%s has only %d elements", DomesticSetV4, len(v4.Elements))
	}
	for _, e := range v4.Elements[:20] {
		if _, _, err := net.ParseCIDR(e); err != nil {
			t.Errorf("element %q is not a CIDR: %v", e, err)
		}
	}
}

func TestApplyLANNftState_EnablesClassificationMasqueradeAndForwardDrop(t *testing.T) {
	fa := &nft.FakeApplier{}
	m := nft.NewManager(fa)
	lan := testLAN()

	sets, err := BuildDomesticSets(true)
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyLANNftState(context.Background(), m, &lan, twoUplinks(), sets, VPNRouteState{}); err != nil {
		t.Fatal(err)
	}

	rs := m.Snapshot()
	if rs.LANClassify == nil || rs.LANClassify.BridgeName != "lan0" {
		t.Fatalf("LANClassify = %+v", rs.LANClassify)
	}
	if rs.LANClassify.DomesticV4Set != DomesticSetV4 {
		t.Errorf("v4 set = %q", rs.LANClassify.DomesticV4Set)
	}
	// The secondary is deliberately absent — LAN exits by the tunnel or nowhere.
	if len(rs.Masquerade) != 1 || rs.Masquerade[0] != "enp1s0" {
		t.Errorf("masquerade = %v, want the domestic uplink only", rs.Masquerade)
	}
	if rs.FilterForward == nil {
		t.Fatal("filter_fwd not enabled; any host on the domestic segment could route into the LAN")
	}
	// Connmark and the pins must survive: the same conntrack mark word serves
	// download shaping, forwarded reply-path pinning and the DNAT ingress pin.
	if !rs.Connmark {
		t.Error("enabling the LAN disabled connmark")
	}

	rendered := fa.Applied[len(fa.Applied)-1]
	if !strings.Contains(rendered, "policy drop") {
		t.Error("the applied ruleset has no drop policy")
	}
}

// A rule naming a set that was never declared aborts the whole nft transaction,
// taking the working rules with it.
func TestApplyLANNftState_ReferencesOnlyDeclaredSets(t *testing.T) {
	m := nft.NewManager(&nft.FakeApplier{})
	lan := testLAN()

	sets, err := BuildDomesticSets(false)
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyLANNftState(context.Background(), m, &lan, twoUplinks(), sets, VPNRouteState{}); err != nil {
		t.Fatal(err)
	}
	c := m.Snapshot().LANClassify
	if c.DomainV4Set != "" || c.DomainV6Set != "" {
		t.Errorf("referenced the dnsmasq sets with no --nftset support: %+v", c)
	}
}

func TestApplyLANNftState_DisableRemovesLANRulesOnly(t *testing.T) {
	ctx := context.Background()
	m := nft.NewManager(&nft.FakeApplier{})
	lan := testLAN()
	sets, _ := BuildDomesticSets(true)

	if err := ApplyNftState(ctx, m, twoUplinks()); err != nil {
		t.Fatal(err)
	}
	if err := ApplyLANNftState(ctx, m, &lan, twoUplinks(), sets, VPNRouteState{}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyLANNftState(ctx, m, nil, twoUplinks(), nil, VPNRouteState{}); err != nil {
		t.Fatal(err)
	}

	rs := m.Snapshot()
	if rs.LANClassify != nil || rs.FilterForward != nil || len(rs.Sets) != 0 {
		t.Errorf("LAN state survived disabling: %+v", rs)
	}
	if len(rs.Masquerade) != 0 {
		t.Errorf("masquerade survived disabling: %v", rs.Masquerade)
	}
	if !rs.Connmark || len(rs.IngressPins) != 2 {
		t.Errorf("disabling the LAN damaged stage-1 state: %+v", rs)
	}
}

// Domestic suffix on the domestic uplink, the default on the tunnel.
func TestLANDNSConfig_MapsServersToTheRightUplinks(t *testing.T) {
	c := LANDNSConfig(testLAN(), twoUplinks(), "217.218.127.127", ForeignDNS{IfName: system.WGLinkName, Server: "1.1.1.1"}, "ir", true)
	if c.DomesticIfName != "enp1s0" {
		t.Errorf("DomesticIfName = %q, want the domestic uplink", c.DomesticIfName)
	}
	if c.ForeignIfName != system.WGLinkName {
		t.Errorf("ForeignIfName = %q, want the tunnel", c.ForeignIfName)
	}
	if c.ListenAddr != "10.77.0.1" {
		t.Errorf("ListenAddr = %q, want the bridge address without its prefix", c.ListenAddr)
	}
}

// Querying a domestic suffix out the secondary uplink returns the wrong CDN
// edge, so with no domestic uplink the suffix server is dropped entirely.
func TestLANDNSConfig_NoDomesticUplinkDropsTheSuffixServer(t *testing.T) {
	c := LANDNSConfig(testLAN(), twoUplinks()[1:], "217.218.127.127", ForeignDNS{IfName: system.WGLinkName, Server: "1.1.1.1"}, "ir", true)
	if c.DomesticServer != "" || c.DomesticSuffix != "" {
		t.Errorf("kept the domestic server with no domestic uplink: %+v", c)
	}
	if c.ForeignIfName != system.WGLinkName {
		t.Errorf("ForeignIfName = %q, want the tunnel", c.ForeignIfName)
	}
}

// Forwarding must be on once the LAN exists, and the bridge needs loose
// reverse-path filtering like every other interface in the path.
func TestRenderSysctl_WithLAN(t *testing.T) {
	got := system.RenderSysctl([]string{"enp1s0", "enp2s0"}, true)
	if !strings.Contains(got, "net.ipv4.ip_forward = 1") {
		t.Error("forwarding not enabled")
	}
	withBridge := system.RenderSysctlWithLAN([]string{"enp1s0", "enp2s0"}, "lan0")
	if !strings.Contains(withBridge, "net.ipv4.conf.lan0.rp_filter = 2") {
		t.Errorf("the bridge has no rp_filter entry:\n%s", withBridge)
	}
	if !strings.Contains(withBridge, "net.ipv4.ip_forward = 1") {
		t.Error("RenderSysctlWithLAN did not enable forwarding")
	}
}

// The bridge is in the forwarded path too, so a runtime apply must set its
// rp_filter — the drop-in file only takes effect at boot.
func TestApplySysctls_SetsTheBridgeRPFilter(t *testing.T) {
	be := system.NewFakeBackend()
	if err := ApplySysctls(context.Background(), be, twoUplinks(), true, "lan0"); err != nil {
		t.Fatal(err)
	}
	if got := be.Sysctls["net.ipv4.conf.lan0.rp_filter"]; got != "2" {
		t.Errorf("lan0 rp_filter = %q, want 2", got)
	}
	if got := be.Sysctls["net.ipv4.ip_forward"]; got != "1" {
		t.Errorf("ip_forward = %q, want 1", got)
	}
}

// networkd creates the bridge asynchronously and dnsmasq cannot bind an address
// that is not there yet: it exits and the LAN has no DHCP and no resolver.
func TestWaitForBridgeAddr_ReturnsOnceTheAddressAppears(t *testing.T) {
	be := system.NewFakeBackend()
	be.AddrList = []system.Addr{{IfName: "lan0", CIDR: "10.77.0.1/24"}}
	if err := waitForBridgeAddr(context.Background(), be, "lan0", 100*time.Millisecond); err != nil {
		t.Errorf("address was present but the wait failed: %v", err)
	}
}

func TestWaitForBridgeAddr_FailsLoudlyWhenTheBridgeNeverAppears(t *testing.T) {
	err := waitForBridgeAddr(context.Background(), system.NewFakeBackend(), "lan0",
		100*time.Millisecond)
	if err == nil {
		t.Fatal("a missing bridge must fail the apply, not start dnsmasq against nothing")
	}
	if !strings.Contains(err.Error(), "lan0") {
		t.Errorf("error should name the bridge: %v", err)
	}
}

func TestRefreshDomesticRanges_ReplacesTheV4Set(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		for i := 0; i < 2105; i++ {
			fmt.Fprintf(&b, "10.%d.%d.0/24\n", i/256, i%256)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	u := newRangesUsecase(t, srv)
	err := u.RefreshDomesticRanges(context.Background())
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	sets, _, err := u.domesticSets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v4 := setNamed(sets, DomesticSetV4)
	if v4 == nil || len(v4.Elements) != 2105 {
		t.Fatalf("v4 set = %d elements, want the 2105 fetched", len(v4.Elements))
	}
	// The box is IPv4-only, so a refresh must not resurrect the v6 sets.
	if setNamed(sets, DomesticSetV6) != nil {
		t.Error("a refresh declared an IPv6 set on an IPv4-only box")
	}
}

// A truncated response must leave the working list alone. Routing a country's
// traffic off a half-list is worse than routing it off a stale one.
func TestRefreshDomesticRanges_KeepsTheOldListOnATruncatedFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("10.0.0.0/24\n10.0.1.0/24\n"))
	}))
	defer srv.Close()

	u := newRangesUsecase(t, srv)
	before, _, berr := u.domesticSets(context.Background())
	skipWithoutGeoIP(t, berr)
	beforeCount := len(setNamed(before, DomesticSetV4).Elements)

	err := u.RefreshDomesticRanges(context.Background())
	if err == nil {
		t.Fatal("a two-prefix list was accepted")
	}

	after, _, _ := u.domesticSets(context.Background())
	if got := len(setNamed(after, DomesticSetV4).Elements); got != beforeCount {
		t.Errorf("v4 set = %d elements, want the previous %d kept", got, beforeCount)
	}
}

// An unreachable upstream is the normal case on a censored link, so it must
// degrade to the embedded list rather than fail the box.
func TestDomesticSets_FallsBackToTheEmbeddedListWithNoCache(t *testing.T) {
	u := newRangesUsecase(t, nil)
	sets, _, err := u.domesticSets(context.Background())
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if v4 := setNamed(sets, DomesticSetV4); v4 == nil || len(v4.Elements) < 100 {
		t.Error("no usable domestic prefixes without a successful fetch")
	}
}

// The cache survives a restart, so a box that fetched once keeps the fresh list
// without refetching.
func TestDomesticSets_PrefersTheCacheOverTheEmbeddedList(t *testing.T) {
	u := newRangesUsecase(t, nil)
	if err := geoip.SaveCachedRanges(u.rangesCachePath(), &geoip.CachedRanges{
		FetchedAt: time.Now(), Source: "test",
		V4: []string{"10.0.0.0/24", "10.0.1.0/24"},
	}); err != nil {
		t.Fatal(err)
	}
	sets, _, err := u.domesticSets(context.Background())
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(setNamed(sets, DomesticSetV4).Elements); got != 2 {
		t.Errorf("v4 set = %d elements, want the 2 cached", got)
	}
}

func newRangesUsecase(t *testing.T, srv *httptest.Server) *networkUsecase {
	t.Helper()
	u := &networkUsecase{Deps: Deps{
		Paths: testPaths(t),
		Nft:   nft.NewManager(&nft.FakeApplier{}),
	}}
	if srv != nil {
		u.RangesURL = srv.URL
		u.RangesClient = srv.Client()
	}
	return u
}

func setNamed(sets []nft.Set, name string) *nft.Set {
	for i := range sets {
		if sets[i].Name == name {
			return &sets[i]
		}
	}
	return nil
}

// The drop-in only takes effect at boot, so an apply has to turn IPv6 off in
// the running kernel too — otherwise it stays up until the next reboot.
func TestApplySysctls_DisablesIPv6AtRuntime(t *testing.T) {
	be := system.NewFakeBackend()
	if err := ApplySysctls(context.Background(), be, twoUplinks(), true, "lan0"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"net.ipv6.conf.all.disable_ipv6",
		"net.ipv6.conf.default.disable_ipv6",
		"net.ipv6.conf.enp1s0.disable_ipv6",
		"net.ipv6.conf.enp2s0.disable_ipv6",
		"net.ipv6.conf.lan0.disable_ipv6",
	} {
		if got := be.Sysctls[key]; got != "1" {
			t.Errorf("%s = %q, want 1", key, got)
		}
	}
}

// With IPv6 off the v6 sets can never match, and a rule referencing them is
// dead weight in every ruleset the box applies.
func TestBuildDomesticSets_NoIPv6SetsWhileIPv6IsDisabled(t *testing.T) {
	sets, err := BuildDomesticSets(true)
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sets {
		if s.Name == DomesticSetV6 || s.Name == DomainSetV6 {
			t.Errorf("declared %s on an IPv4-only box", s.Name)
		}
		if s.Family == "ipv6_addr" {
			t.Errorf("set %s is an IPv6 set", s.Name)
		}
	}
}

// dnsmasq must not be told to populate a set the ruleset never declares.
func TestLANDNSConfig_NoIPv6DomainSet(t *testing.T) {
	c := LANDNSConfig(testLAN(), twoUplinks(), "217.218.127.127", ForeignDNS{IfName: system.WGLinkName, Server: "1.1.1.1"}, "ir", true)
	for _, ds := range c.DomainSets {
		if ds.V6Set != "" {
			t.Errorf("domain set %+v names an IPv6 set on an IPv4-only box", ds)
		}
	}
}
