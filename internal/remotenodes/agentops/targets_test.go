// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops/fakeagent"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// what the agent says of a target that works: TLS 1.3, X25519, a flight of
// this many bytes
func good(name string, bytes, ms int) *pb.RealityDestCheck {
	return &pb.RealityDestCheck{ServerName: name, Ok: true, TlsVersion: "TLS 1.3", KeyShare: "X25519", FlightBytes: int32(bytes), HandshakeMs: int64(ms), Alpn: "h2"}
}

func TestAHandshakeCheckReadsWhatTheAgentFound(t *testing.T) {
	for _, c := range []struct {
		name string
		in   *pb.RealityDestCheck
		want HandshakeCheck
		say  string // what the error says, when there is one
	}{
		{"one that works", good("a", 3860, 42), HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 3860, LatencyMs: 42}, ""},
		{"with the post-quantum exchange", &pb.RealityDestCheck{Ok: true, TlsVersion: "TLS 1.3", KeyShare: "X25519MLKEM768", FlightBytes: 5000}, HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 5000}, ""},
		{"a handshake that adds up to over 8 KB, each record small", good("a", 9895, 80), HandshakeCheck{TLS13: true, X25519: true, HandshakeBytes: 9895, LatencyMs: 80}, "9895 bytes"},
		{"exactly 8 KB", good("a", 8192, 1), HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 8192, LatencyMs: 1}, ""},
		{"no TLS 1.3", &pb.RealityDestCheck{Reason: "tls_version", Message: "x negotiated TLS 1.2. REALITY needs TLS 1.3.", TlsVersion: "TLS 1.2", FlightBytes: 3000}, HandshakeCheck{HandshakeBytes: 3000}, "It doesn't use TLS 1.3."},
		{"another key exchange", &pb.RealityDestCheck{Reason: "key_share", TlsVersion: "TLS 1.3", KeyShare: "P-256"}, HandshakeCheck{TLS13: true}, "It doesn't use the X25519 key exchange."},
		{"a retry request", &pb.RealityDestCheck{Reason: "hello_retry_request", TlsVersion: "TLS 1.3", KeyShare: "X25519"}, HandshakeCheck{TLS13: true, X25519: true}, "start over"},
		{"a record that is too large", &pb.RealityDestCheck{Reason: "record_too_large", TlsVersion: "TLS 1.3", KeyShare: "X25519", FlightBytes: 12000}, HandshakeCheck{TLS13: true, X25519: true, HandshakeBytes: 12000}, "over 8 KB"},
		{"no connection", &pb.RealityDestCheck{Reason: "dial_failed", Message: "Could not connect to www.example.com:443: connection refused"}, HandshakeCheck{}, "Could not connect to www.example.com:443: connection refused."},
		{"a reason it doesn't know", &pb.RealityDestCheck{Reason: "something_new", Message: "it went wrong"}, HandshakeCheck{}, "It went wrong."},
		{"no reason at all", &pb.RealityDestCheck{}, HandshakeCheck{}, "The node couldn't use it."},
	} {
		got := handshakeCheck(c.in)
		say := got.Error
		got.Error = ""
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
		if (c.say == "") != (say == "") || !strings.Contains(say, c.say) {
			t.Errorf("%s: the error says %q, want it to say %q", c.name, say, c.say)
		}
		// the page shows it as it is
		for _, bad := range []string{"REALITY", "reality", "dest", "agent"} {
			if strings.Contains(say, bad) && c.in.Reason != "dial_failed" && c.in.Reason != "something_new" {
				t.Errorf("%s: %q has %q in it", c.name, say, bad)
			}
		}
	}
}

func TestParseTarget(t *testing.T) {
	for in, want := range map[string]string{
		"www.google.com": "www.google.com:443", "www.google.com:443": "www.google.com:443", "  WWW.Google.Com:8443  ": "www.google.com:8443",
		"192.0.2.7": "192.0.2.7:443", "192.0.2.7:8443": "192.0.2.7:8443", "[2001:db8::7]:443": "[2001:db8::7]:443", "2001:db8::7": "[2001:db8::7]:443",
	} {
		host, port, err := parseTarget(in)
		if err != nil {
			t.Errorf("parseTarget(%q): %v", in, err)
			continue
		}
		if got := net.JoinHostPort(host, strconv.Itoa(port)); got != want {
			t.Errorf("parseTarget(%q) = %s, want %s", in, got, want)
		}
	}
	for _, in := range []string{"", "https://www.google.com", "www.google.com/path", "a b", "www.google.com:0", "www.google.com:70000", "www.google.com:https", "not a host:443", ":443", "1.2.3", "www.google.com."} {
		var inv *model.InvalidError
		if _, _, err := parseTarget(in); !errors.As(err, &inv) {
			t.Errorf("parseTarget(%q) = %v, want an InvalidError", in, err)
		}
	}
}

func TestCheckTargetAsksTheNodeAboutOneTarget(t *testing.T) {
	r := newLinkRig(t)
	var got *pb.RealityDestProbeRequest
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnProbe = func(req *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse {
			got = req
			return &pb.RealityDestProbeResponse{Ok: true, Dest: req.Dest, Checks: []*pb.RealityDestCheck{good(req.ServerNames[0], 4200, 35)}}
		}
	})
	chk, err := r.env.CheckTarget(context.Background(), r.connect(), "www.cloudflare.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := (HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 4200, LatencyMs: 35}); chk != want {
		t.Errorf("check %+v", chk)
	}
	if got.Dest != "www.cloudflare.com:443" || !slices.Equal(got.ServerNames, []string{"www.cloudflare.com"}) || got.TimeoutMs != 6000 {
		t.Errorf("the node was asked %+v", got)
	}

	// an address has no name to put in the hello, so none goes
	if _, err := r.env.CheckTarget(context.Background(), r.connect(), "192.0.2.7:8443"); err != nil {
		t.Fatal(err)
	}
	if got.Dest != "192.0.2.7:8443" || !slices.Equal(got.ServerNames, []string{""}) {
		t.Errorf("the node was asked %+v", got)
	}

	// a target that doesn't work is a result, and one that isn't a target is an error
	r.ag.Configure(func(a *fakeagent.Agent) { a.OnProbe = nil })
	chk, err = r.env.CheckTarget(context.Background(), r.connect(), "www.example.com:443")
	if err != nil || chk.OK || !strings.Contains(chk.Error, "connection refused") {
		t.Errorf("a target that can't be reached: %+v, %v", chk, err)
	}
	var inv *model.InvalidError
	if _, err := r.env.CheckTarget(context.Background(), r.connect(), "https://nope"); !errors.As(err, &inv) {
		t.Errorf("a URL: %v", err)
	}
}

func TestProbeTargetsChoosesTheBestAndReportsThemAll(t *testing.T) {
	r := newLinkRig(t)
	answers := map[string]*pb.RealityDestCheck{
		"a.example": {Reason: "key_share", TlsVersion: "TLS 1.3", KeyShare: "P-256", FlightBytes: 3000}, // doesn't work
		"b.example": good("b.example", 6200, 20),
		"c.example": good("c.example", 3900, 90),
		"d.example": good("d.example", 3900, 30), // as small as c, and quicker
		"e.example": good("e.example", 9100, 5),  // quick, but over 8 KB
		"f.example": {Reason: "dial_failed", Message: "Could not connect to f.example:443: i/o timeout"},
	}
	var mu sync.Mutex
	var dests []string
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnProbe = func(req *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse {
			name := req.ServerNames[0]
			mu.Lock()
			dests = append(dests, req.Dest) // the agent serves calls in goroutines
			mu.Unlock()
			chk := proto.Clone(answers[name]).(*pb.RealityDestCheck)
			chk.ServerName = name
			return &pb.RealityDestProbeResponse{Dest: req.Dest, Checks: []*pb.RealityDestCheck{chk}}
		}
	})
	choice, err := r.env.ProbeTargets(context.Background(), r.connect(),
		[]string{"a.example", "b.example:443", "c.example", "d.example", "e.example", "f.example", "A.example:443"}) // the last is a again
	if err != nil {
		t.Fatal(err)
	}
	if choice.Best != "d.example:443" {
		t.Errorf("best %q: the smallest working handshake is c's and d's, and d is quicker", choice.Best)
	}
	var targets []string
	var oks []bool
	for _, res := range choice.Results {
		targets = append(targets, res.Target)
		oks = append(oks, res.Check.OK)
	}
	if want := []string{"a.example:443", "b.example:443", "c.example:443", "d.example:443", "e.example:443", "f.example:443"}; !slices.Equal(targets, want) {
		t.Errorf("results are for %v, want %v (in the order given, each once)", targets, want)
	}
	if want := []bool{false, true, true, true, false, false}; !slices.Equal(oks, want) {
		t.Errorf("working: %v, want %v", oks, want)
	}
	if e := choice.Results[4].Check.Error; !strings.Contains(e, "9100 bytes") {
		t.Errorf("the large one says %q", e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dests) != 6 {
		t.Errorf("the node was asked %d times: %v", len(dests), dests)
	}
}

func TestProbeTargetsWithNoneWorkingHasNoBest(t *testing.T) {
	r := newLinkRig(t) // the stand-in fails every name by default
	choice, err := r.env.ProbeTargets(context.Background(), r.connect(), []string{"a.example", "b.example"})
	if err != nil || choice.Best != "" || len(choice.Results) != 2 {
		t.Fatalf("%+v, %v", choice, err)
	}
	e := &NoTargetError{Results: choice.Results}
	if !errors.Is(e, ErrNoTarget) || !strings.Contains(e.Error(), "None of the 2 sites") {
		t.Errorf("%v", e)
	}
	one := &NoTargetError{Results: choice.Results[:1]}
	if !strings.Contains(one.Error(), "a.example:443") || !strings.Contains(one.Error(), "doesn't work as a Reality target") {
		t.Errorf("%v", one)
	}
}

func TestProbeTargetsWithNoCandidatesTriesTheDefaults(t *testing.T) {
	r := newLinkRig(t)
	var asked atomic.Int32
	seen := make(chan string, 32)
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnProbe = func(req *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse {
			asked.Add(1)
			seen <- req.Dest
			return &pb.RealityDestProbeResponse{Dest: req.Dest, Checks: []*pb.RealityDestCheck{good(req.ServerNames[0], 4000, 10)}}
		}
	})
	choice, err := r.env.ProbeTargets(context.Background(), r.connect(), nil)
	if err != nil {
		t.Fatal(err)
	}
	close(seen)
	var dests []string
	for d := range seen {
		dests = append(dests, d)
	}
	slices.Sort(dests)
	want := DefaultTargets()
	slices.Sort(want)
	if !slices.Equal(dests, want) {
		t.Errorf("the node was asked about %v, want %v", dests, want)
	}
	if choice.Best != DefaultTargets()[0] {
		t.Errorf("best %q: with all equal, the first, which is the panel's own default", choice.Best)
	}
}

func TestTheDefaultTargetsAreWellFormedAndFewEnough(t *testing.T) {
	list := DefaultTargets()
	if len(list) < 5 || len(list) > maxCandidates {
		t.Fatalf("%d targets", len(list))
	}
	if list[0] != "www.google.com:443" {
		t.Errorf("the panel's own default isn't first: %q", list[0])
	}
	seen := map[string]bool{}
	for _, s := range list {
		host, port, err := parseTarget(s)
		if err != nil || port != 443 || net.ParseIP(host) != nil || seen[s] {
			t.Errorf("%q: %v", s, err)
		}
		seen[s] = true
		// the pairs that can't make a good one, or the sort of site that draws attention to the node
		for _, bad := range []string{"microsoft", "apple", "icloud", ".ir", ".ru", ".cn"} {
			if strings.Contains(host, bad) {
				t.Errorf("%s is no Reality target to default to", host)
			}
		}
	}
	list[0] = "changed"
	if DefaultTargets()[0] == "changed" {
		t.Error("the list is shared")
	}
}

func TestProbeTargetsChecksTheCandidatesBeforeAskingTheNode(t *testing.T) {
	r := newLinkRig(t)
	c := r.connect()
	var inv *model.InvalidError
	if _, err := r.env.ProbeTargets(context.Background(), c, []string{"fine.example", "https://not.a.target"}); !errors.As(err, &inv) {
		t.Errorf("a URL: %v", err)
	}
	many := make([]string, maxCandidates+1)
	for i := range many {
		many[i] = "site" + strconv.Itoa(i) + ".example"
	}
	if _, err := r.env.ProbeTargets(context.Background(), c, many); !errors.As(err, &inv) {
		t.Errorf("too many: %v", err)
	}
	if slices.Contains(r.ag.Calls(), "ProbeRealityDest") {
		t.Error("the node was asked about candidates that were refused")
	}
}

func TestProbeTargetsRunsSeveralAtOnce(t *testing.T) {
	r := newLinkRig(t)
	var running, peak atomic.Int32
	r.ag.Configure(func(a *fakeagent.Agent) {
		a.OnProbe = func(req *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse {
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(150 * time.Millisecond)
			running.Add(-1)
			return &pb.RealityDestProbeResponse{Dest: req.Dest, Checks: []*pb.RealityDestCheck{good(req.ServerNames[0], 4000, 10)}}
		}
	})
	started := time.Now()
	if _, err := r.env.ProbeTargets(context.Background(), r.connect(), DefaultTargets()); err != nil {
		t.Fatal(err)
	}
	if peak.Load() < 2 || peak.Load() > 4 {
		t.Errorf("%d checks ran at once, want a few (2 to 4)", peak.Load())
	}
	if took := time.Since(started); took > 6*150*time.Millisecond {
		t.Errorf("8 checks of 150 ms took %v: they weren't run together", took)
	}
}

func TestProbeTargetsToAnAgentThatPredatesTheCheckSaysToUpdateIt(t *testing.T) {
	r := newLinkRig(t)
	// the stand-in answers Unimplemented for what it doesn't serve: take the probe away
	r.ag.Configure(func(a *fakeagent.Agent) { a.NoProbe = true })
	_, err := r.env.ProbeTargets(context.Background(), r.connect(), []string{"a.example"})
	if !errors.Is(err, ErrAgentOutdated) || !strings.Contains(err.Error(), "Update the agent first") {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.env.CheckTarget(context.Background(), r.connect(), "a.example"); !errors.Is(err, ErrAgentOutdated) {
		t.Fatalf("CheckTarget: %v", err)
	}
}

func TestProbeTargetsStopsWhenItsContextDoes(t *testing.T) {
	r := newLinkRig(t)
	c := r.connect()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.env.ProbeTargets(ctx, c, []string{"a.example"}); err == nil || !ctxDone(err) {
		t.Fatalf("err = %v", err)
	}
}
