package preflight

import (
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
