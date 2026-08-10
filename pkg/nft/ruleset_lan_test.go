package nft

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// Every chain, so one `nft -c` on the golden file validates the lot.
func lanRuleset() Ruleset {
	return Ruleset{
		Connmark:    true,
		IngressPins: []Pin{{IfName: "enp1s0", Index: 1}, {IfName: "enp2s0", Index: 2}},
		Sets: []Set{
			{Name: "ir_v4", Family: "ipv4_addr", Interval: true,
				Elements: []string{"2.144.0.0/13", "5.22.0.0/17"}},
			{Name: "ir_v6", Family: "ipv6_addr", Interval: true,
				Elements: []string{"2001:db8::/32"}},
			// dnsmasq-populated, so plain with a timeout rather than interval.
			{Name: "ir_dom_v4", Family: "ipv4_addr", Timeout: "24h"},
			{Name: "ir_dom_v6", Family: "ipv6_addr", Timeout: "24h"},
		},
		LANClassify: &LANClassify{
			BridgeName: "lan0", DomesticV4Set: "ir_v4", DomesticV6Set: "ir_v6",
			DomainV4Set: "ir_dom_v4", DomainV6Set: "ir_dom_v6",
		},
		Masquerade:    []string{"enp1s0", "enp2s0"},
		FilterForward: &FilterForward{BridgeName: "lan0", UplinkNames: []string{"enp1s0", "enp2s0"}},
		FilterInput: &FilterInput{
			LocalIfNames: []string{"lo", "lan0", "enp3s0"},
			Accepts: []InputAccept{
				{IfNames: []string{"enp1s0"}, Proto: "tcp", Port: 9761, Comment: "panel"},
				{IfNames: []string{"enp1s0", "enp2s0"}, Proto: "tcp", Port: 443, Comment: "vless-tcp"},
			},
		},
		PortForwards: []PortForward{
			{IfName: "enp1s0", Proto: "tcp", DPort: 443, ToAddr: "10.77.0.5", ToPort: 443},
			{IfName: "enp2s0", Proto: "tcp", DPort: 443, ToAddr: "10.77.0.5", ToPort: 443},
		},
	}
}

// Classified by destination in the kernel: domestic first, foreign catch-all.
func TestRender_LANClassificationIsDestinationBasedAndDefaultsForeign(t *testing.T) {
	got := lanRuleset().Render()

	domestic := netmark.Hex(netmark.GroupMark(netmark.GroupDomestic))
	foreign := netmark.Hex(netmark.GroupMark(netmark.GroupForeign))
	keep := netmark.Hex(^netmark.MaskGroup)

	for _, want := range []string{
		`iifname "lan0" ip daddr @ir_v4 meta mark set mark and ` + keep + ` or ` + domestic,
		`iifname "lan0" ip6 daddr @ir_v6 meta mark set mark and ` + keep + ` or ` + domestic,
		`iifname "lan0" meta mark and ` + netmark.Hex(netmark.MaskGroup) + ` == 0x0 meta mark set mark and ` + keep + ` or ` + foreign,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	// Domestic must precede the catch-all, or everything becomes foreign.
	if strings.Index(got, "@ir_v4") > strings.Index(got, "== 0x0") {
		t.Error("the foreign catch-all precedes the domestic match")
	}
}

// No uplink address: interface plus "is this box" survives a DHCP renewal.
func TestRender_PortForwardsNameNoAddresses(t *testing.T) {
	got := lanRuleset().Render()
	for _, want := range []string{
		`fib daddr type local iifname "enp1s0" tcp dport 443 dnat ip to 10.77.0.5:443`,
		`fib daddr type local iifname "enp2s0" tcp dport 443 dnat ip to 10.77.0.5:443`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "192.168.1.") || strings.Contains(got, "100.64.") {
		t.Error("an uplink address leaked into the nat chain")
	}
}

// Without a drop policy any host on the domestic segment can route into the LAN.
func TestRender_ForwardChainIsPolicyDrop(t *testing.T) {
	got := lanRuleset().Render()
	if !strings.Contains(got, "type filter hook forward priority filter; policy drop;") {
		t.Errorf("forward chain is not policy drop:\n%s", got)
	}
	for _, want := range []string{
		"ct state established,related accept",
		`iifname "lan0" oifname { "enp1s0", "enp2s0" } accept`,
		"ct status dnat accept",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in the forward chain:\n%s", want, got)
		}
	}
}

func TestRender_MasqueradeOnUplinkEgressOnly(t *testing.T) {
	got := lanRuleset().Render()
	if !strings.Contains(got, `oifname { "enp1s0", "enp2s0" } masquerade`) {
		t.Errorf("no masquerade on uplink egress:\n%s", got)
	}
	// Masquerading toward the LAN would hide the real client address.
	if strings.Contains(got, `oifname "lan0" masquerade`) {
		t.Error("masquerade on the LAN bridge would hide the real client IP")
	}
}

func TestRender_InputChainIsPolicyDropWithRenderedAccepts(t *testing.T) {
	got := lanRuleset().Render()

	if !strings.Contains(got, "type filter hook input priority filter; policy drop;") {
		t.Errorf("input chain is not policy drop:\n%s", got)
	}
	for _, want := range []string{
		`iifname { "lo", "lan0", "enp3s0" } accept`,
		"icmp type { echo-request, destination-unreachable, time-exceeded }",
		`iifname "enp1s0" tcp dport 9761 accept`,
		`iifname { "enp1s0", "enp2s0" } tcp dport 443 accept`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in the input chain:\n%s", want, got)
		}
	}
	// established must come first, or the reply to our own outbound dies.
	chain := got[strings.Index(got, "chain filter_in"):]
	if strings.Index(chain, "ct state established,related accept") >
		strings.Index(chain, "dport 9761") {
		t.Error("an accept precedes the established rule")
	}
}

// Stage 1 boxes must render exactly as before: no LAN, no NAT, no filter chains.
func TestRender_StageOneShapeIsUnchanged(t *testing.T) {
	got := Ruleset{Connmark: true, IngressPins: []Pin{{IfName: "enp1s0", Index: 1}}}.Render()
	for _, absent := range []string{"hook forward", "hook input", "masquerade", "@ir_v4", "set ir_v4"} {
		if strings.Contains(got, absent) {
			t.Errorf("stage-1 ruleset emitted %q:\n%s", absent, got)
		}
	}
}

// Still declared when empty, or a rule referencing it aborts the transaction.
func TestRender_EmptySetStillDeclared(t *testing.T) {
	got := Ruleset{Sets: []Set{{Name: "ir_v4", Family: "ipv4_addr", Interval: true}}}.Render()
	if !strings.Contains(got, "set ir_v4 {") {
		t.Errorf("empty set not declared:\n%s", got)
	}
	if !strings.Contains(got, "flags interval") {
		t.Error("a CIDR set must carry `flags interval` or its elements are rejected")
	}
}

// Plain with a timeout: intervals cannot carry per-element timeouts.
func TestRender_TimeoutSetIsNotAnIntervalSet(t *testing.T) {
	got := Ruleset{Sets: []Set{
		{Name: "ir_dom_v4", Family: "ipv4_addr", Timeout: "24h"},
	}}.Render()
	if !strings.Contains(got, "flags timeout") || !strings.Contains(got, "timeout 24h") {
		t.Errorf("timeout set not declared correctly:\n%s", got)
	}
	if strings.Contains(got, "flags interval") {
		t.Error("a timeout set must not also be an interval set — nftables rejects the pair")
	}
}

// Matched too, or --nftset populates a set nothing reads.
func TestRender_LANClassificationMatchesTheDomainSets(t *testing.T) {
	got := lanRuleset().Render()

	for _, want := range []string{
		`iifname "lan0" ip daddr @ir_dom_v4 meta mark set`,
		`iifname "lan0" ip6 daddr @ir_dom_v6 meta mark set`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Both domestic layers must precede the foreign catch-all.
	if strings.Index(got, "@ir_dom_v4") > strings.Index(got, "== 0x0") {
		t.Error("the foreign catch-all precedes the domain-set match")
	}
}

// A rule naming an undeclared set aborts the whole transaction.
func TestRender_NoDomainSetsMeansNoDomainRules(t *testing.T) {
	rs := lanRuleset()
	rs.Sets = rs.Sets[:2]
	rs.LANClassify.DomainV4Set, rs.LANClassify.DomainV6Set = "", ""
	if got := rs.Render(); strings.Contains(got, "@ir_dom_v4") {
		t.Errorf("referenced an undeclared set:\n%s", got)
	}
}

func TestRender_LANFullGolden(t *testing.T) {
	got := lanRuleset().Render()
	if want := golden(t, "lan_full.nft"); got != want {
		t.Errorf("render mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
