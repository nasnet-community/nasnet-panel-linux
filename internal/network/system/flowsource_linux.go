//go:build linux

package system

import (
	"context"
	"fmt"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func (s *LiveFlowSource) Conntrack(context.Context) ([]CTFlow, error) {
	flows, err := netlink.ConntrackTableList(netlink.ConntrackTable, unix.AF_INET)
	if err != nil {
		return nil, fmt.Errorf("conntrack dump: %w", err)
	}
	out := make([]CTFlow, 0, len(flows))
	for _, f := range flows {
		out = append(out, CTFlow{
			Proto:   ctProto(f.Forward.Protocol),
			SrcIP:   f.Forward.SrcIP.String(),
			DstIP:   f.Forward.DstIP.String(),
			SrcPort: f.Forward.SrcPort, DstPort: f.Forward.DstPort,
			Mark:      f.Mark,
			OrigBytes: f.Forward.Bytes, ReplyBytes: f.Reverse.Bytes,
			OrigPackets: f.Forward.Packets, ReplyPackets: f.Reverse.Packets,
		})
	}
	return out, nil
}
