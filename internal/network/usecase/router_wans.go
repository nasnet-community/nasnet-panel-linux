package usecase

import (
	"context"
	"sort"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// One secondary, as the xray config builder needs it.
type RouterWANView struct {
	Slot        string
	UplinkIndex uint32
	Label       string
}

// Slot order, named the way the operator named them. A read failure answers
// "none": the caller only loses its per-WAN outbounds, and the kernel rules
// fail closed either way.
func (u *networkUsecase) RouterWANs(ctx context.Context) []RouterWANView {
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil
	}
	return routerWANViews(secondariesOf(uplinks), u.uplinkLabels(ctx))
}

func routerWANViews(secs []Uplink, labels map[string]string) []RouterWANView {
	ordered := append([]Uplink(nil), secs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })

	out := make([]RouterWANView, 0, len(ordered))
	for _, up := range ordered {
		label := labels[up.IfName]
		if label == "" || label == up.IfName {
			label = slotDisplay(up.Slot)
		}
		out = append(out, RouterWANView{
			Slot: string(up.Slot), UplinkIndex: up.UplinkIndex, Label: label,
		})
	}
	return out
}

// slotDisplay mirrors the web panel's names (network-labels.ts).
func slotDisplay(s domain.UplinkSlot) string {
	switch s {
	case domain.SlotSecondary:
		return "Secondary 1"
	case domain.SlotSecondary2:
		return "Secondary 2"
	case domain.SlotSecondary3:
		return "Secondary 3"
	case domain.SlotSecondary4:
		return "Secondary 4"
	}
	return string(s)
}
