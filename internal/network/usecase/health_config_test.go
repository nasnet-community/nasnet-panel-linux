package usecase

import "testing"

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
