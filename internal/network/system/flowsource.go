package system

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CTFlow is one conntrack entry, already reduced to what the flow page shows.
type CTFlow struct {
	Proto                     string
	SrcIP, DstIP              string
	SrcPort, DstPort          uint16
	Mark                      uint32
	OrigBytes, ReplyBytes     uint64
	OrigPackets, ReplyPackets uint64
}

// LinkStat is one interface's cumulative byte counters.
type LinkStat struct{ RxBytes, TxBytes uint64 }

// FlowSource reads live traffic accounting: what flowed, marked how.
type FlowSource interface {
	Conntrack(ctx context.Context) ([]CTFlow, error)
	LinkStats(ctx context.Context) (map[string]LinkStat, error)
}

type LiveFlowSource struct{ ProcNetDev string }

func NewFlowSource() *LiveFlowSource { return &LiveFlowSource{ProcNetDev: "/proc/net/dev"} }

func (s *LiveFlowSource) LinkStats(context.Context) (map[string]LinkStat, error) {
	b, err := os.ReadFile(s.ProcNetDev)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.ProcNetDev, err)
	}
	return parseProcNetDev(string(b)), nil
}

func parseProcNetDev(text string) map[string]LinkStat {
	out := map[string]LinkStat{}
	for _, line := range strings.Split(text, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue // the two header lines
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[strings.TrimSpace(name)] = LinkStat{RxBytes: rx, TxBytes: tx}
	}
	return out
}

var ctProtoNames = map[uint8]string{1: "icmp", 6: "tcp", 17: "udp"}

func ctProto(p uint8) string {
	if n, ok := ctProtoNames[p]; ok {
		return n
	}
	return strconv.Itoa(int(p))
}
