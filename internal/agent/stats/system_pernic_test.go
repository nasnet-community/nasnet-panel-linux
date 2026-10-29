package stats

import "testing"

// Router mode needs per-interface throughput, and the aggregate call cannot
// provide it. Summing per-NIC counters also drops loopback, which the old
// aggregate silently included.
func TestSumNICs_ExcludesLoopback(t *testing.T) {
	in := map[string]NICCounters{
		"lo":     {BytesRecv: 1000, BytesSent: 1000},
		"enp1s0": {BytesRecv: 10, BytesSent: 20},
		"enp2s0": {BytesRecv: 3, BytesSent: 4},
	}
	recv, sent := sumNICs(in)
	if recv != 13 || sent != 24 {
		t.Errorf("sum = (%d, %d), want (13, 24) — loopback must be excluded", recv, sent)
	}
}

func TestSumNICs_Empty(t *testing.T) {
	if r, s := sumNICs(nil); r != 0 || s != 0 {
		t.Errorf("sum of nothing = (%d, %d)", r, s)
	}
}

// wg-quick down/up restarts one tunnel's counters at zero. Summed first, the
// total shrank and the reset fallback reported all traffic since boot.
func TestNetDelta_TunnelRecreated(t *testing.T) {
	last := map[string]NICCounters{
		"enp1s0": {BytesRecv: 9_000_000_000, BytesSent: 4_000_000_000},
		"wg0":    {BytesRecv: 50_000_000, BytesSent: 20_000_000},
	}
	cur := map[string]NICCounters{
		"enp1s0": {BytesRecv: 9_000_001_000, BytesSent: 4_000_000_500},
		"wg0":    {BytesRecv: 300, BytesSent: 100},
	}
	recv, sent := netDelta(last, cur)
	if recv != 1300 || sent != 600 {
		t.Errorf("delta = (%d, %d), want (1300, 600)", recv, sent)
	}
}

func TestNetDelta_NewAndGoneInterfaces(t *testing.T) {
	last := map[string]NICCounters{
		"enp1s0": {BytesRecv: 100, BytesSent: 100},
		"wg1":    {BytesRecv: 7_000, BytesSent: 7_000}, // torn down since
		"lo":     {BytesRecv: 1, BytesSent: 1},
	}
	cur := map[string]NICCounters{
		"enp1s0": {BytesRecv: 150, BytesSent: 120},
		"wg2":    {BytesRecv: 40, BytesSent: 30}, // came up since
		"lo":     {BytesRecv: 900, BytesSent: 900},
	}
	recv, sent := netDelta(last, cur)
	if recv != 90 || sent != 50 {
		t.Errorf("delta = (%d, %d), want (90, 50)", recv, sent)
	}
}
