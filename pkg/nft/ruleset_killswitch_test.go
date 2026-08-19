package nft

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func killSwitchRuleset() Ruleset {
	return Ruleset{
		Connmark:    true,
		IngressPins: []Pin{{IfName: "enp1s0", Index: 1}, {IfName: "enp2s0", Index: 2}},
		Masquerade:  []string{"enp1s0", "enp2s0", "nasnet-wg0"},
		FilterForward: &FilterForward{
			BridgeName: "lan0",
			// The secondary uplink is deliberately absent: the LAN reaches the
			// internet through the tunnel now, not around it.
			UplinkNames: []string{"enp1s0", "nasnet-wg0"},
		},
		KillSwitch: &KillSwitch{
			SecondaryIfName: "enp2s0",
			GatewayIP:       "100.64.0.1",
			DishSubnet:      "192.168.100.0/24",
			MarkMask:        netmark.MaskPin,
			MarkValue:       netmark.PinMark(2),
			BootstrapIPs:    []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"},
		},
	}
}

func TestRender_KillSwitchMatchesTheGoldenFile(t *testing.T) {
	got := killSwitchRuleset().Render()
	if want := golden(t, "killswitch.nft"); got != want {
		t.Errorf("rendered ruleset differs from the golden file:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The drop has to be last in both chains, or an exemption below it never runs
// and the one above it lets everything through.
func TestRender_KillSwitchDropsLast(t *testing.T) {
	got := killSwitchRuleset().Render()

	for _, chain := range []string{"killswitch_out", "killswitch_fwd"} {
		body := chainBody(t, got, chain)
		lines := meaningfulLines(body)
		if len(lines) == 0 {
			t.Fatalf("%s is empty", chain)
		}
		if last := lines[len(lines)-1]; last != "drop" {
			t.Errorf("%s ends with %q, want drop", chain, last)
		}
		for i, l := range lines[:len(lines)-1] {
			if l == "drop" {
				t.Errorf("%s drops at line %d, before its exemptions", chain, i)
			}
		}
	}
}

// Policy drop would take the box off the network the moment a render went wrong.
func TestRender_KillSwitchChainsAreAcceptPolicy(t *testing.T) {
	got := killSwitchRuleset().Render()
	for _, want := range []string{
		"chain killswitch_out {\n\t\ttype filter hook output priority filter + 10; policy accept;",
		"chain killswitch_fwd {\n\t\ttype filter hook forward priority filter + 10; policy accept;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing:\n%s\nin:\n%s", want, got)
		}
	}
}

// The exemption list is the security boundary. Anything extra is a leak.
func TestRender_KillSwitchExemptsOnlyTheAllowlist(t *testing.T) {
	got := killSwitchRuleset().Render()
	body := chainBody(t, got, "killswitch_out")

	mask, value := netmark.Hex(netmark.MaskPin), netmark.Hex(netmark.PinMark(2))
	want := []string{
		`oifname != "enp2s0" accept`,
		`ct state established,related accept`,
		`meta mark and ` + mask + ` == ` + value + ` meta l4proto udp accept`,
		`meta mark and ` + mask + ` == ` + value + ` tcp dport 443 ip daddr @doh_bootstrap accept`,
		`udp sport 68 udp dport 67 accept`,
		`ip daddr 100.64.0.1 accept`,
		`ip daddr 192.168.100.0/24 accept`,
		`drop`,
	}
	if got := meaningfulLines(body); !equalLines(got, want) {
		t.Errorf("killswitch_out =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Before the first lease there is no gateway to name, and a rule naming an
// empty address would abort the whole transaction.
func TestRender_KillSwitchWithoutAGateway(t *testing.T) {
	rs := killSwitchRuleset()
	rs.KillSwitch.GatewayIP = ""
	body := chainBody(t, rs.Render(), "killswitch_out")

	if strings.Contains(body, "ip daddr  accept") {
		t.Error("rendered an empty address")
	}
	for _, want := range []string{"udp sport 68 udp dport 67 accept", "drop"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// With no VPN configured at all the mark exemption has nothing to match, and
// the uplink still has to keep its lease and answer the health probe.
func TestRender_KillSwitchWithoutATunnel(t *testing.T) {
	rs := Ruleset{KillSwitch: &KillSwitch{
		SecondaryIfName: "enp2s0",
		GatewayIP:       "100.64.0.1",
	}}
	body := chainBody(t, rs.Render(), "killswitch_out")

	if strings.Contains(body, "meta mark") {
		t.Error("a mark exemption with no tunnel to exempt")
	}
	if strings.Contains(body, "doh_bootstrap") {
		t.Error("a bootstrap exemption with no bootstrap addresses")
	}
	want := []string{
		`oifname != "enp2s0" accept`,
		`ct state established,related accept`,
		`udp sport 68 udp dport 67 accept`,
		`ip daddr 100.64.0.1 accept`,
		`drop`,
	}
	if got := meaningfulLines(body); !equalLines(got, want) {
		t.Errorf("killswitch_out =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A referenced set that was never declared aborts the transaction, taking every
// other rule in the table with it.
func TestRender_KillSwitchDeclaresItsOwnSet(t *testing.T) {
	got := killSwitchRuleset().Render()
	if !strings.Contains(got, "set doh_bootstrap {") {
		t.Fatal("the bootstrap set is referenced but never declared")
	}
	if strings.Index(got, "set doh_bootstrap {") > strings.Index(got, "@doh_bootstrap") {
		t.Error("the set is declared after the rule that uses it")
	}
	if !strings.Contains(got, "elements = { 1.1.1.1, 1.0.0.1, 8.8.8.8, 8.8.4.4 }") {
		t.Errorf("bootstrap addresses missing from:\n%s", got)
	}
}

// The kill switch reads marks; writing one would fight the classification.
func TestRender_KillSwitchNeverWritesAMark(t *testing.T) {
	for _, chain := range []string{"killswitch_out", "killswitch_fwd"} {
		body := chainBody(t, killSwitchRuleset().Render(), chain)
		if strings.Contains(body, "mark set") {
			t.Errorf("%s writes a mark", chain)
		}
	}
}

func TestRuleset_KillSwitchAloneIsNotZero(t *testing.T) {
	rs := Ruleset{KillSwitch: &KillSwitch{SecondaryIfName: "enp2s0"}}
	if rs.IsZero() {
		t.Error("a ruleset carrying a kill switch reported itself empty, so it would be torn down")
	}
}

// chainBody returns the lines between a chain's braces.
func chainBody(t *testing.T, rendered, chain string) string {
	t.Helper()
	start := strings.Index(rendered, "chain "+chain+" {")
	if start < 0 {
		t.Fatalf("no chain %s in:\n%s", chain, rendered)
	}
	rest := rendered[start:]
	end := strings.Index(rest, "\n\t}")
	if end < 0 {
		t.Fatalf("chain %s is unterminated", chain)
	}
	return rest[:end]
}

// meaningfulLines drops comments, the chain header and blank lines.
func meaningfulLines(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "chain ") ||
			strings.HasPrefix(l, "type filter") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestKillSwitchProbeExemption(t *testing.T) {
	r := Ruleset{KillSwitch: &KillSwitch{
		SecondaryIfName: "enp0s3",
		MarkMask:        netmark.MaskPin,
		MarkValue:       netmark.PinMark(2),
		ProbeMark:       netmark.PinMark(netmark.PinProbe),
		ProbeIPs:        []string{"1.1.1.1", "8.8.8.8"},
	}}
	out := r.Render()
	for _, want := range []string{
		"set probe_v4 {",
		"elements = { 1.1.1.1, 8.8.8.8 }",
		"meta mark and 0xf000000 == 0xf000000 ip daddr @probe_v4 accept",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestKillSwitchNoProbeIPsRendersNoProbeRule(t *testing.T) {
	r := Ruleset{KillSwitch: &KillSwitch{SecondaryIfName: "enp0s3"}}
	if out := r.Render(); strings.Contains(out, "probe_v4") {
		t.Fatalf("probe set rendered with no IPs:\n%s", out)
	}
}
