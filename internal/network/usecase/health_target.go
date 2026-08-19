package usecase

import (
	"context"
	"encoding/binary"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// JSON tags match the router_probe_targets_* settings blobs.
type ProbeTarget struct {
	Address string `json:"address"` // host:port
	Proto   string `json:"proto"`   // "tcp" | "dns"
}

type ProbeResult struct {
	Target ProbeTarget
	OK     bool
	RTT    time.Duration
	Err    string
}

// TargetProber dials past the gateway; mark rides SO_MARK, 0 sends unmarked.
type TargetProber interface {
	ProbeTarget(ctx context.Context, ifName string, mark uint32, t ProbeTarget) ProbeResult
}

// Fan out, so a 2s timeout costs 2s and not targets×2s of the 5s tick.
func probeAll(ctx context.Context, p TargetProber, ifName string, mark uint32, targets []ProbeTarget) []ProbeResult {
	out := make([]ProbeResult, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t ProbeTarget) {
			defer wg.Done()
			out[i] = p.ProbeTarget(ctx, ifName, mark, t)
		}(i, t)
	}
	wg.Wait()
	return out
}

// anyUp: one answering target proves egress; partial failure is degraded's job.
func anyUp(results []ProbeResult) bool {
	for _, r := range results {
		if r.OK {
			return true
		}
	}
	return false
}

// dnsQuery builds a minimal A query; not worth a dependency.
func dnsQuery(name string) (uint16, []byte) {
	id := uint16(rand.Intn(0x10000))
	b := make([]byte, 12, 12+len(name)+6)
	binary.BigEndian.PutUint16(b[0:2], id)
	b[2] = 0x01 // RD
	binary.BigEndian.PutUint16(b[4:6], 1)
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0x00)       // root
	b = append(b, 0x00, 0x01) // QTYPE A
	b = append(b, 0x00, 0x01) // QCLASS IN
	return id, b
}

// dnsAnswers takes any response with our ID — even SERVFAIL proves a resolver.
func dnsAnswers(id uint16, resp []byte) bool {
	if len(resp) < 3 {
		return false
	}
	return binary.BigEndian.Uint16(resp[0:2]) == id && resp[2]&0x80 != 0
}
