package nft

import (
	"strings"
	"testing"
)

func countersRuleset() Ruleset {
	return Ruleset{
		Counters: true,
		Connmark: true,
		KillSwitch: &KillSwitch{
			SecondaryIfName: "eth1", GatewayIP: "100.64.0.1",
			DishSubnet: "192.168.100.0/24",
			MarkMask:   0xf000000, MarkValue: 0x2000000,
			BootstrapIPs: []string{"1.1.1.1"},
		},
	}
}

func TestCountersAreDeclaredAndReferenced(t *testing.T) {
	out := countersRuleset().Render()
	for _, want := range []string{
		"counter cnt_domestic {",
		"counter cnt_foreign {",
		"counter cnt_killswitch {",
		"meta mark and 0xff0000 == 0x10000 counter name cnt_domestic",
		"meta mark and 0xff0000 == 0x20000 counter name cnt_foreign",
		"counter name cnt_killswitch drop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Both kill-switch chains must count their drops.
	if strings.Count(out, "counter name cnt_killswitch drop") != 2 {
		t.Error("expected the drop counter in killswitch_out and killswitch_fwd")
	}
}

func TestCountersOffLeavesRenderUntouched(t *testing.T) {
	rs := countersRuleset()
	rs.Counters = false
	out := rs.Render()
	if strings.Contains(out, "cnt_") {
		t.Error("counters rendered while disabled")
	}
	if !strings.Contains(out, "\t\tdrop\n") {
		t.Error("plain drop must remain when counters are off")
	}
}

func TestIntrospectionMatchesRender(t *testing.T) {
	rs := countersRuleset()
	rs.Sets = []Set{{Name: "ir_v4", Family: "ipv4_addr", Interval: true}}
	rs.LANClassify = &LANClassify{BridgeName: "lan0", DomesticV4Set: "ir_v4"}
	rs.Masquerade = []string{"eth0"}
	out := rs.Render()

	for _, c := range rs.ChainNames() {
		if !strings.Contains(out, "chain "+c+" {") {
			t.Errorf("ChainNames says %q but render lacks it", c)
		}
	}
	for _, s := range rs.SetNames() {
		if !strings.Contains(out, "set "+s+" {") {
			t.Errorf("SetNames says %q but render lacks it", s)
		}
	}
	for _, c := range rs.CounterNames() {
		if !strings.Contains(out, "counter "+c+" {") {
			t.Errorf("CounterNames says %q but render lacks it", c)
		}
	}
	// And nothing the render emits is missing from the introspection.
	if len(rs.ChainNames()) != strings.Count(out, "\tchain ") {
		t.Errorf("chain count mismatch: introspection %d render %d",
			len(rs.ChainNames()), strings.Count(out, "\tchain "))
	}
}

// IsZero decides whether a rollback restores the table or tears it down, so a
// ruleset that renders anything can never report zero.
func TestCountersOnlyRulesetIsNotZero(t *testing.T) {
	rs := Ruleset{Counters: true}
	if rs.IsZero() {
		t.Error("a counters-only ruleset reports zero but renders a table")
	}
	if rs.Render() == "" {
		t.Error("nothing rendered, so the premise is wrong")
	}
}
