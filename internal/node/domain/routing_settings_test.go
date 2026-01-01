package domain

import "testing"

// Defaults must preserve pre-settings behaviour: IPIfNonMatch, nothing blocked.
func TestGetDefaultRoutingSettings(t *testing.T) {
	d := GetDefaultRoutingSettings()
	if d.DomainStrategy != "IPIfNonMatch" {
		t.Errorf("DomainStrategy = %q, want IPIfNonMatch", d.DomainStrategy)
	}
	if d.BlockBitTorrent || d.WARPEnabled {
		t.Error("defaults should not enable blocking or WARP")
	}
	if len(d.BlockIPs) != 0 || len(d.DirectDomains) != 0 {
		t.Error("defaults should have empty rule lists")
	}
}
