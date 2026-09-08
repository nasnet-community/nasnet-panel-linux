package usecase

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

func TestParseHealthConfigFallsBackPerKey(t *testing.T) {
	vals := map[string]string{
		"router_probe_targets_domestic":   `[{"address":"10.0.0.1:53","proto":"dns"}]`,
		"router_probe_targets_foreign":    `not json`,
		"router_degraded_loss_pct":        "40",
		"router_failover_domestic_to_vpn": "false",
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
	if len(cfg.TargetsDomestic) != 1 || cfg.TargetsDomestic[0].Address != "10.0.0.1:53" {
		t.Fatalf("domestic not parsed: %+v", cfg.TargetsDomestic)
	}
	if len(cfg.TargetsForeign) != 2 {
		t.Fatal("corrupt foreign blob must fall back to defaults, not empty")
	}
	if cfg.DegradedLossPct != 40 || cfg.FailoverToVPN {
		t.Fatalf("scalars wrong: %+v", cfg)
	}
}

func TestLossThresholdsInheritByGroupThenByLine(t *testing.T) {
	vals := map[string]string{"router_degraded_loss_pct": "40"}
	read := func(k string) (string, error) { return vals[k], nil }
	cfg := ParseHealthConfig(read)
	for _, slot := range allUplinkSlots() {
		if got := cfg.degradedLossFor(slot); got != 40 {
			t.Fatalf("legacy loss for %s = %d", slot, got)
		}
	}
	vals["router_degraded_loss_pct_domestic"] = "60"
	cfg = ParseHealthConfig(read)
	for _, slot := range domain.DomesticSlots() {
		if got := cfg.degradedLossFor(slot); got != 60 {
			t.Fatalf("domestic loss for %s = %d", slot, got)
		}
	}
	for _, slot := range domain.SecondarySlots() {
		if got := cfg.degradedLossFor(slot); got != 40 {
			t.Fatalf("domestic edit changed foreign loss for %s to %d", slot, got)
		}
	}
	vals["router_degraded_loss_pct_foreign"] = "70"
	vals[DegradedLossSlotKey(domain.SlotDomestic2)] = "80"
	vals[DegradedLossSlotKey(domain.SlotSecondary)] = "90"
	cfg = ParseHealthConfig(read)
	if cfg.degradedLossFor(domain.SlotDomestic2) != 80 || cfg.degradedLossFor(domain.SlotDomestic) != 60 ||
		cfg.degradedLossFor(domain.SlotSecondary) != 90 || cfg.degradedLossFor(domain.SlotSecondary2) != 70 {
		t.Fatalf("line/group precedence incorrect: %+v", cfg)
	}
	if cfg.degradedGroupLossFor(domain.SlotSecondary) != 70 {
		t.Fatal("VPN pool inherited an individual secondary's override")
	}
	delete(vals, DegradedLossSlotKey(domain.SlotDomestic2))
	if got := ParseHealthConfig(read).degradedLossFor(domain.SlotDomestic2); got != 60 {
		t.Fatalf("clearing override did not restore group loss: %d", got)
	}
}

func TestInvalidGroupLossFallsBackToLegacyDefault(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "101", "bad"} {
		t.Run(value, func(t *testing.T) {
			vals := map[string]string{"router_degraded_loss_pct": "40", "router_degraded_loss_pct_domestic": value,
				"router_degraded_loss_pct_foreign": value}
			cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
			for _, slot := range allUplinkSlots() {
				if got := cfg.degradedLossFor(slot); got != 40 {
					t.Fatalf("invalid group loss for %s = %d", slot, got)
				}
			}
		})
	}
}

func TestParseHealthConfigEmptyIsAllDefaults(t *testing.T) {
	cfg := ParseHealthConfig(func(string) (string, error) { return "", nil })
	def := DefaultHealthConfig()
	if len(cfg.TargetsDomestic) != len(def.TargetsDomestic) || !cfg.FailoverToVPN ||
		cfg.DegradedLossPct != def.DegradedLossPct {
		t.Fatalf("empty store must yield defaults: %+v", cfg)
	}
}

// A hostname or v6 literal in the set text aborts the whole nft table load.
func TestParseHealthConfigDropsNonIPv4Targets(t *testing.T) {
	vals := map[string]string{
		"router_probe_targets_foreign": `[
			{"address":"dns.google:443","proto":"tcp"},
			{"address":"[2606:4700::1111]:443","proto":"tcp"},
			{"address":"9.9.9.9:443","proto":"tcp"},
			{"address":"1.1.1.1:0","proto":"tcp"},
			{"address":"8.8.8.8:53","proto":"icmp"}
		]`,
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
	if len(cfg.TargetsForeign) != 1 || cfg.TargetsForeign[0].Address != "9.9.9.9:443" {
		t.Fatalf("want only the clean v4 target, got %+v", cfg.TargetsForeign)
	}
}

func TestParseHealthConfigAllInvalidFallsBackToDefaults(t *testing.T) {
	vals := map[string]string{
		"router_probe_targets_foreign": `[{"address":"dns.google:443","proto":"tcp"}]`,
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
	if len(cfg.TargetsForeign) != 2 {
		t.Fatalf("all-invalid list must fall back to defaults, got %+v", cfg.TargetsForeign)
	}
}

// The settings keys stay per-kind, so every slot of a kind must read them.
func TestHealthConfigCoversEveryDomesticSlot(t *testing.T) {
	def := DefaultHealthConfig()
	for _, s := range domain.DomesticSlots() {
		if def.DegradedRTTms[s] != 300 {
			t.Errorf("default rtt for %s = %d, want 300", s, def.DegradedRTTms[s])
		}
		if got := def.targetsFor(s); len(got) != len(def.TargetsDomestic) || got[0] != def.TargetsDomestic[0] {
			t.Errorf("%s probes %+v, want the domestic targets", s, got)
		}
	}
	cfg := ParseHealthConfig(func(k string) (string, error) {
		if k == "router_degraded_rtt_ms_domestic" {
			return "450", nil
		}
		return "", nil
	})
	for _, s := range domain.DomesticSlots() {
		if cfg.DegradedRTTms[s] != 450 {
			t.Errorf("rtt for %s = %d after the domestic key, want 450", s, cfg.DegradedRTTms[s])
		}
	}
	for _, s := range domain.SecondarySlots() {
		if cfg.DegradedRTTms[s] != 800 {
			t.Errorf("the domestic key leaked into %s: %d", s, cfg.DegradedRTTms[s])
		}
	}
}

// A per-line list wins over its group's, and the group still covers the rest.
func TestParseHealthConfigPerSlotTargetsOverrideTheGroup(t *testing.T) {
	vals := map[string]string{
		"router_probe_targets_domestic":       `[{"address":"10.0.0.1:53","proto":"dns"}]`,
		"router_probe_targets_slot_domestic2": `[{"address":"10.0.0.9:53","proto":"dns","label":"Modem"}]`,
		"router_probe_targets_slot_secondary": `[{"address":"9.9.9.9:443","proto":"tcp"}]`,
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })

	own := cfg.targetsFor(domain.SlotDomestic2)
	if len(own) != 1 || own[0].Address != "10.0.0.9:53" || own[0].Label != "Modem" {
		t.Fatalf("domestic2 must use its own list: %+v", own)
	}
	shared := cfg.targetsFor(domain.SlotDomestic)
	if len(shared) != 1 || shared[0].Address != "10.0.0.1:53" {
		t.Fatalf("domestic must still use the group list: %+v", shared)
	}
	if got := cfg.targetsFor(domain.SlotDomestic3); len(got) != 1 || got[0].Address != "10.0.0.1:53" {
		t.Fatalf("an unset slot falls back to its group: %+v", got)
	}
	sec := cfg.targetsFor(domain.SlotSecondary)
	if len(sec) != 1 || sec[0].Address != "9.9.9.9:443" {
		t.Fatalf("secondary must use its own list: %+v", sec)
	}
	if got := cfg.targetsFor(domain.SlotSecondary2); len(got) != 2 {
		t.Fatalf("an unset secondary falls back to the foreign defaults: %+v", got)
	}
}

// A corrupt or empty override must not blank out a line's checks.
func TestParseHealthConfigIgnoresUnusableSlotOverride(t *testing.T) {
	vals := map[string]string{
		"router_probe_targets_slot_domestic2": `not json`,
		"router_probe_targets_slot_domestic3": `[]`,
		"router_probe_targets_slot_domestic4": `[{"address":"dns.google:53","proto":"dns"}]`,
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
	for _, s := range []domain.UplinkSlot{domain.SlotDomestic2, domain.SlotDomestic3, domain.SlotDomestic4} {
		got := cfg.targetsFor(s)
		if len(got) != 2 || got[0].Address != "217.218.155.155:53" {
			t.Errorf("%s must fall back to the group list, got %+v", s, got)
		}
	}
}

func TestParseHealthConfigPerSlotDegradedThresholds(t *testing.T) {
	vals := map[string]string{
		"router_degraded_loss_pct":                "25",
		"router_degraded_rtt_ms_domestic":         "300",
		"router_degraded_rtt_ms_slot_domestic2":   "900",
		"router_degraded_loss_pct_slot_domestic2": "60",
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
	if cfg.DegradedRTTms[domain.SlotDomestic2] != 900 {
		t.Errorf("domestic2 rtt = %d, want its own 900", cfg.DegradedRTTms[domain.SlotDomestic2])
	}
	if cfg.DegradedRTTms[domain.SlotDomestic] != 300 {
		t.Errorf("domestic rtt = %d, want the group 300", cfg.DegradedRTTms[domain.SlotDomestic])
	}
	if got := cfg.degradedLossFor(domain.SlotDomestic2); got != 60 {
		t.Errorf("domestic2 loss = %d, want its own 60", got)
	}
	if got := cfg.degradedLossFor(domain.SlotDomestic); got != 25 {
		t.Errorf("domestic loss = %d, want the global 25", got)
	}
}

// The kill switch only ever meets probes on a secondary, and each leg may now
// carry a different list, so the exemption has to be keyed by slot.
func TestProbeExemptIPsBySlotFollowsEachSecondary(t *testing.T) {
	vals := map[string]string{
		"router_probe_targets_slot_secondary2": `[{"address":"9.9.9.9:443","proto":"tcp"}]`,
	}
	cfg := ParseHealthConfig(func(k string) (string, error) { return vals[k], nil })
	got := cfg.probeExemptIPsBySlot()

	if ips := got[domain.SlotSecondary2]; len(ips) != 1 || ips[0] != "9.9.9.9" {
		t.Errorf("secondary2 exemption = %v, want its own target", ips)
	}
	if ips := got[domain.SlotSecondary]; len(ips) != 2 || ips[0] != "1.1.1.1" {
		t.Errorf("secondary exemption = %v, want the foreign defaults", ips)
	}
	for _, s := range domain.DomesticSlots() {
		if ips, ok := got[s]; ok {
			t.Errorf("%s must not get an exemption, got %v", s, ips)
		}
	}
}
