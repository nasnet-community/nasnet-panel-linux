package usecase

// Live conntrack, decoded. The tracer says where traffic would go; this says
// where it actually went, and under which mark.

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// A full table can hold tens of thousands of flows; the page shows the ones
// carrying data.
const maxFlowConns = 200

type FlowConn struct {
	Proto   string `json:"proto"`
	Src     string `json:"src"`
	Dst     string `json:"dst"`
	Mark    uint32 `json:"mark"`
	Group   string `json:"group"`
	Pin     uint32 `json:"pin"`
	Device  string `json:"device,omitempty"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type FlowConnsView struct {
	Flows       []FlowConn `json:"flows"`
	Total       int        `json:"total"`
	AcctEnabled bool       `json:"acct_enabled"`
}

func (u *networkUsecase) FlowConns(ctx context.Context) (*FlowConnsView, error) {
	raw, err := u.flowSource().Conntrack(ctx)
	if err != nil {
		return nil, err
	}
	// Unknown counts as off: the key is missing exactly when nothing counts.
	acct := "0"
	if v, serr := u.Backend.SysctlGet(ctx, "net.netfilter.nf_conntrack_acct"); serr == nil {
		acct = strings.TrimSpace(v)
	}
	deviceOf := u.deviceNamesByIP(ctx)

	view := &FlowConnsView{Total: len(raw), AcctEnabled: acct == "1", Flows: []FlowConn{}}
	sort.SliceStable(raw, func(i, j int) bool {
		return raw[i].OrigBytes+raw[i].ReplyBytes > raw[j].OrigBytes+raw[j].ReplyBytes
	})
	if len(raw) > maxFlowConns {
		raw = raw[:maxFlowConns]
	}
	for _, f := range raw {
		c := FlowConn{
			Proto: f.Proto,
			Src:   net.JoinHostPort(f.SrcIP, strconv.Itoa(int(f.SrcPort))),
			Dst:   net.JoinHostPort(f.DstIP, strconv.Itoa(int(f.DstPort))),
			Mark:  f.Mark, Pin: netmark.Pin(f.Mark),
			Device:  deviceOf[f.SrcIP],
			TxBytes: f.OrigBytes, RxBytes: f.ReplyBytes,
		}
		switch netmark.Group(f.Mark) {
		case netmark.GroupDomestic:
			c.Group = "domestic"
		case netmark.GroupForeign:
			c.Group = "foreign"
		}
		view.Flows = append(view.Flows, c)
	}
	return view, nil
}

// deviceNamesByIP is best-effort sugar; the rows stay useful without it.
func (u *networkUsecase) deviceNamesByIP(ctx context.Context) map[string]string {
	out := map[string]string{}
	list, err := u.ListDevices(ctx)
	if err != nil || list == nil {
		return out
	}
	for _, d := range list.Devices {
		name := d.Label
		if name == "" {
			name = d.Hostname
		}
		if name == "" {
			continue
		}
		for _, ip := range d.IPs {
			out[ip] = name
		}
	}
	return out
}

func (u *networkUsecase) RecentNetworkEvents(context.Context) ([]events.Event, error) {
	if u.Events == nil {
		return []events.Event{}, nil
	}
	return u.Events.Recent(), nil
}
