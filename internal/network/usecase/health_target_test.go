package usecase

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeProber struct {
	mu    sync.Mutex
	calls []string
	up    map[string]bool
}

func (f *fakeProber) ProbeTarget(_ context.Context, ifName string, mark uint32, t ProbeTarget) ProbeResult {
	f.mu.Lock()
	f.calls = append(f.calls, ifName+"/"+t.Address)
	f.mu.Unlock()
	return ProbeResult{Target: t, OK: f.up[t.Address], RTT: 10 * time.Millisecond}
}

func TestProbeAllHitsEveryTargetConcurrently(t *testing.T) {
	f := &fakeProber{up: map[string]bool{"1.1.1.1:443": true, "8.8.8.8:443": false}}
	targets := []ProbeTarget{
		{Address: "1.1.1.1:443", Proto: "tcp"},
		{Address: "8.8.8.8:443", Proto: "tcp"},
	}
	rs := probeAll(context.Background(), f, "eth1", 0, targets)
	if len(rs) != 2 {
		t.Fatalf("want 2 results, got %d", len(rs))
	}
	// Order must match input order regardless of goroutine finish order.
	if rs[0].Target.Address != "1.1.1.1:443" || !rs[0].OK || rs[1].OK {
		t.Fatalf("results out of order or wrong: %+v", rs)
	}
	if !anyUp(rs) {
		t.Fatal("one target answered; anyUp must be true")
	}
	if anyUp(nil) {
		t.Fatal("no targets means no proof; anyUp must be false")
	}
}

func TestDNSQueryWireFormat(t *testing.T) {
	id, q := dnsQuery("example.com")
	// Header: ID, RD flag, one question.
	if len(q) < 12+len("example.com")+2+4 {
		t.Fatalf("query too short: %d bytes", len(q))
	}
	if uint16(q[0])<<8|uint16(q[1]) != id {
		t.Fatal("ID not at offset 0")
	}
	if q[2] != 0x01 || q[3] != 0x00 {
		t.Fatalf("flags: want RD only, got %#x %#x", q[2], q[3])
	}
	if !dnsAnswers(id, append(q[:2:2], 0x81, 0x80)) {
		t.Fatal("a matching-ID response with QR set must count as an answer")
	}
	if dnsAnswers(id, []byte{0x00}) {
		t.Fatal("a runt must not count")
	}
	if dnsAnswers(id+1, append(q[:2:2], 0x81, 0x80)) {
		t.Fatal("an ID mismatch must not count")
	}
}
