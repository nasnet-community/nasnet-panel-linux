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
