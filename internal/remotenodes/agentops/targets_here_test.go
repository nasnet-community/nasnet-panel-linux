// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// hereProbe stands in for the check run on the router: it answers by target,
// and remembers what it was asked.
type hereProbe struct {
	mu      sync.Mutex
	asked   []*pb.RealityDestProbeRequest
	answers map[string]*pb.RealityDestCheck // by dest
	err     error
}

func (h *hereProbe) probe(_ context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, req)
	if h.err != nil {
		return nil, h.err
	}
	c, ok := h.answers[req.Dest]
	if !ok {
		return &pb.RealityDestProbeResponse{}, nil
	}
	return &pb.RealityDestProbeResponse{Checks: []*pb.RealityDestCheck{c}}, nil
}

func goodCheck(bytes, ms int) *pb.RealityDestCheck {
	return &pb.RealityDestCheck{Ok: true, TlsVersion: "TLS 1.3", KeyShare: "X25519", FlightBytes: int32(bytes), HandshakeMs: int64(ms)}
}

// The router's check is the node's check: asked the same way and judged by
// the same rules, wherever it ran.
func TestCheckTargetHereJudgesLikeANodesCheck(t *testing.T) {
	h := &hereProbe{answers: map[string]*pb.RealityDestCheck{
		"www.example.com:443": goodCheck(4000, 30),
		"big.example.com:443": goodCheck(9000, 30),
		"old.example.com:443": {Reason: "tls_version", TlsVersion: "TLS 1.2"},
		"203.0.113.9:8443":    goodCheck(5000, 12),
	}}
	ctx := context.Background()

	got, err := CheckTargetHere(ctx, h.probe, "www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.TLS13 || !got.X25519 || got.HandshakeBytes != 4000 || got.LatencyMs != 30 || got.Error != "" {
		t.Fatalf("a good target: %+v", got)
	}
	req := h.asked[0]
	if req.Dest != "www.example.com:443" || len(req.ServerNames) != 1 || req.ServerNames[0] != "www.example.com" || req.TimeoutMs <= 0 {
		t.Fatalf("the check was asked for as %+v", req)
	}

	if got, _ = CheckTargetHere(ctx, h.probe, "big.example.com:443"); got.OK || !strings.Contains(got.Error, "8 KB") {
		t.Errorf("a handshake over 8 KB: %+v", got)
	}
	if got, _ = CheckTargetHere(ctx, h.probe, "old.example.com:443"); got.OK || got.Error != "It doesn't use TLS 1.3." {
		t.Errorf("a TLS 1.2 site: %+v", got)
	}
	// an address has no name to put in the hello
	if _, err = CheckTargetHere(ctx, h.probe, "203.0.113.9:8443"); err != nil {
		t.Fatal(err)
	}
	if last := h.asked[len(h.asked)-1]; last.ServerNames[0] != "" {
		t.Errorf("an address was sent as the server name %q", last.ServerNames[0])
	}

	// what isn't a target is refused before anything is asked
	before := len(h.asked)
	if _, err = CheckTargetHere(ctx, h.probe, "https://www.example.com/"); err == nil {
		t.Error("a URL was taken for a target")
	}
	if len(h.asked) != before {
		t.Error("a bad target was checked anyway")
	}
}

func TestCheckTargetHereSaysWhatStoppedIt(t *testing.T) {
	ctx := context.Background()
	h := &hereProbe{err: errors.New("no route to host")}
	_, err := CheckTargetHere(ctx, h.probe, "www.example.com")
	if err == nil || !strings.Contains(err.Error(), "The router couldn't check the target") || !strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("a check that couldn't run: %v", err)
	}
	// no answer at all for the target
	_, err = CheckTargetHere(ctx, (&hereProbe{}).probe, "www.example.com")
	if err == nil || !strings.Contains(err.Error(), "router's check") {
		t.Fatalf("a check with no answer: %v", err)
	}
	// a request that was given up on comes back as that
	gone, cancel := context.WithCancel(ctx)
	cancel()
	h = &hereProbe{err: context.Canceled}
	if _, err = CheckTargetHere(gone, h.probe, "www.example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled check: %v", err)
	}
}

func TestProbeTargetsHereChoosesTheBest(t *testing.T) {
	h := &hereProbe{answers: map[string]*pb.RealityDestCheck{
		"a.example.com:443": goodCheck(6000, 10),
		"b.example.com:443": goodCheck(4000, 90),
		"c.example.com:443": {Reason: "no_response"},
	}}
	choice, err := ProbeTargetsHere(context.Background(), h.probe, []string{"a.example.com", "b.example.com:443", "c.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if choice.Best != "b.example.com:443" || len(choice.Results) != 3 || choice.Results[2].Check.OK {
		t.Fatalf("choice %+v", choice)
	}

	// none given: the same well-known sites a node is offered
	h = &hereProbe{answers: map[string]*pb.RealityDestCheck{}}
	for _, target := range DefaultTargets() {
		h.answers[target] = &pb.RealityDestCheck{Reason: "no_response"}
	}
	choice, err = ProbeTargetsHere(context.Background(), h.probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Best != "" || len(choice.Results) != len(DefaultTargets()) {
		t.Fatalf("with none working: %+v", choice)
	}
}
