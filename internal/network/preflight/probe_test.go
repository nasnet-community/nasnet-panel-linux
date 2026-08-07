package preflight

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/netif"
)

// Checked on 24.04: absent unit prints not-found (exit 4), `mask --runtime`
// prints masked-runtime (exit 1). Both mean NM won't touch a link.
func TestUnitDisarmed(t *testing.T) {
	for _, s := range []string{"masked", "masked-runtime", "not-found", " masked\n"} {
		if !unitDisarmed(s) {
			t.Errorf("%q should count as disarmed", s)
		}
	}
	for _, s := range []string{"enabled", "disabled", "static", "", "enabled-runtime"} {
		if unitDisarmed(s) {
			t.Errorf("%q must not count as disarmed", s)
		}
	}
}

// A docker bridge is assignable (stage 2 LAN role) but is not an uplink.
func TestUplinkCandidate_ExcludesVirtualAndLoopback(t *testing.T) {
	for _, s := range []netif.Source{
		netif.SourceEthOnboard, netif.SourceEthPCI, netif.SourceEthUSB, netif.SourceEthPlatform,
		netif.SourceWifiPCI, netif.SourceWifiUSB,
		netif.SourceTetherAndroid, netif.SourceTetherIPhone,
		netif.SourceWWANUSB, netif.SourceWWANPCIe,
	} {
		if !uplinkCandidate(s) {
			t.Errorf("%q must count as an uplink candidate", s)
		}
	}
	for _, s := range []netif.Source{
		netif.SourceVirtBridge, netif.SourceVirtVLAN, netif.SourceVirtBond,
		netif.SourceVirtOther, netif.SourceLoopback, netif.SourceUnknown,
	} {
		if uplinkCandidate(s) {
			t.Errorf("%q must not count as an uplink", s)
		}
	}
}

// On a non-target platform the operator must be told the platform is wrong, not
// shown a missing sysfs path.
func TestProbe_WrongPlatformSkipsEnumeration(t *testing.T) {
	e, err := Probe(false)
	if err != nil && (e.OSID != "ubuntu" || e.OSVersionID != "24.04") {
		t.Fatalf("Probe errored on a non-target platform instead of letting Check speak: %v", err)
	}
	if e.OSID == "ubuntu" && e.OSVersionID == "24.04" {
		t.Skip("running on the target platform")
	}
	r := Check(e)
	if r.OK() {
		t.Fatal("a non-target platform passed preflight")
	}
	if !strings.Contains(strings.Join(r.Fatal, " | "), "Ubuntu 24.04") {
		t.Errorf("first fatal does not name the platform: %v", r.Fatal)
	}
}
