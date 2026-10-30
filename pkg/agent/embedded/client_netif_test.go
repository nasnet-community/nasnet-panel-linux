package embedded

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// The panel must be able to learn a second NIC exists through the same seam it
// uses for everything else. Compile-time assertion plus a shape check.
func TestNetInterface_CarriesEverythingTheUINeeds(t *testing.T) {
	var _ agent.NodeClient = (*Client)(nil)

	ni := agent.NetInterface{
		IfName: "enp2s0", PermMAC: "aa:bb:cc:dd:ee:02", KeyKind: "permaddr",
		Key: "aa:bb:cc:dd:ee:02", Source: "eth_pci", Confidence: 100,
		Carrier: true, OperState: "up", SpeedMbit: 1000, MTU: 1500,
		Assignable: true, Addrs: []string{"100.64.1.9/10"},
	}
	if ni.Key == "" || ni.Source == "" || len(ni.Addrs) == 0 {
		t.Fatal("NetInterface lost a field")
	}
}
