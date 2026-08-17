package system

import (
	"os"
	"path/filepath"
	"testing"
)

const procNetDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  151013    1888    0    0    0     0          0         0   151013    1888    0    0    0     0       0          0
  eth0: 9182736   12345    0    0    0     0          0         0  4455667    9876    0    0    0     0       0          0
nasnet-wg0:  100  2    0    0    0     0          0         0      200        3    0    0    0     0       0          0
`

func TestParseProcNetDev(t *testing.T) {
	got := parseProcNetDev(procNetDevFixture)
	if s := got["eth0"]; s.RxBytes != 9182736 || s.TxBytes != 4455667 {
		t.Fatalf("eth0: %+v", s)
	}
	if s := got["nasnet-wg0"]; s.RxBytes != 100 || s.TxBytes != 200 {
		t.Fatalf("wg: %+v", s)
	}
	if _, ok := got["lo"]; !ok {
		t.Fatal("lo missing")
	}
}

func TestLinkStatsReadsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dev")
	if err := os.WriteFile(p, []byte(procNetDevFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	src := &LiveFlowSource{ProcNetDev: p}
	got, err := src.LinkStats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got["eth0"].RxBytes != 9182736 {
		t.Fatalf("eth0: %+v", got["eth0"])
	}
}
