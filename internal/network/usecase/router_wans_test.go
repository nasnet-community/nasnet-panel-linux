package usecase

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// The deal, the tables and the outbound tags all key on slot order, so this
// list has to hold it whatever order the repo answered in.
func TestRouterWANViews_SlotOrderAndLabels(t *testing.T) {
	secs := []Uplink{
		{IfName: "enp4s0", UplinkIndex: 4, Slot: domain.SlotSecondary3},
		{IfName: "enp2s0", UplinkIndex: 2, Slot: domain.SlotSecondary},
	}
	labels := map[string]string{"enp2s0": "Starlink"}

	got := routerWANViews(secs, labels)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Slot != "secondary" || got[0].UplinkIndex != 2 || got[0].Label != "Starlink" {
		t.Errorf("got[0] = %+v", got[0])
	}
	// No operator label falls back to the slot's name, not the interface — the
	// remark ends up in the routing dialog, where "enp4s0" says nothing.
	if got[1].Slot != "secondary3" || got[1].Label != "Secondary 3" {
		t.Errorf("got[1] = %+v", got[1])
	}
}
