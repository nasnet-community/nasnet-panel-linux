// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// A Reality target is a site the node borrows the start of every TLS
// handshake from, as Xray's REALITY does. It has to be reachable from the node
// itself, answer with TLS 1.3 and an X25519 key exchange, and keep its handshake
// small: REALITY reads at most 8 KB of it. Only the node can say, so the
// checks run there, through the agent.

// maxHandshakeBytes is what REALITY reads of the target's side of the
// handshake. The agent refuses a single record over it and only warns about
// a handshake that adds up to more, which is the one that breaks a link when
// it reconnects, so over it counts as failing here.
const maxHandshakeBytes = 8192

// probeTimeout is how long the node may take to connect to and shake hands
// with one target; the call is given a little more.
const probeTimeout = 6 * time.Second

// HandshakeCheck is what the node found out about one target. The fields are
// the API's, which the page reads.
type HandshakeCheck struct {
	OK             bool   `json:"ok"`
	TLS13          bool   `json:"tls13"`
	X25519         bool   `json:"x25519"`
	HandshakeBytes int    `json:"handshakeBytes"` // the target's side of the handshake
	LatencyMs      int    `json:"latencyMs"`
	Error          string `json:"error"` // why it won't do, in a sentence; empty when it will
}

// TargetResult is the check of one candidate.
type TargetResult struct {
	Target string // host:port
	Check  HandshakeCheck
}

// TargetChoice is the candidates checked, in the order they were given, and the
// one that is best: working, then the smallest handshake, then the quickest.
// Best is empty when none works.
type TargetChoice struct {
	Best    string
	Results []TargetResult
}

// NoTargetError is every target tried and none that works from the node.
type NoTargetError struct{ Results []TargetResult }

func (e *NoTargetError) Error() string {
	if len(e.Results) == 1 {
		return sentence(fmt.Sprintf("The site %s doesn't work as a Reality target from this server: %s", e.Results[0].Target, lowerFirst(e.Results[0].Check.Error)))
	}
	return fmt.Sprintf("None of the %d sites tried works as a Reality target from this server.", len(e.Results))
}
func (e *NoTargetError) Is(target error) bool { return target == ErrNoTarget }

func lowerFirst(s string) string {
	if s != "" && s[0] >= 'A' && s[0] <= 'Z' {
		return string(s[0]+'a'-'A') + s[1:]
	}
	return s
}

// DefaultTargets are well-known sites to choose from when the user names none.
// Tried from a development machine, each answered with TLS 1.3 and X25519 and
// a handshake of 3,860 to 6,322 bytes, under the 8 KB Xray reads. The Linux
// panel's own default, www.google.com, comes first. They are candidates, not a
// promise: whether one works is for the node to say, and it varies from one
// server to the next.
func DefaultTargets() []string {
	return []string{
		"www.google.com:443", "www.cloudflare.com:443", "dl.google.com:443", "www.wikipedia.org:443",
		"www.debian.org:443", "addons.mozilla.org:443", "www.python.org:443", "www.samsung.com:443",
	}
}

// maxCandidates is how many targets one choice looks at.
const maxCandidates = 16

// parseTarget reads host or host:port, with 443 for the port, and returns the
// target as host:port.
func parseTarget(s string) (host string, port int, err error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") || strings.ContainsAny(s, "/ \t") {
		return "", 0, &model.InvalidError{Msg: fmt.Sprintf("%q isn't a host and port: write it like www.example.com:443", s)}
	}
	hostPart, portPart, err := net.SplitHostPort(s)
	if err != nil {
		hostPart, portPart = strings.Trim(s, "[]"), "443" // no port given
	}
	if hostPart, err = model.CleanHost(hostPart); err != nil {
		return "", 0, err
	}
	if port, err = strconv.Atoi(portPart); err != nil {
		return "", 0, &model.InvalidError{Msg: fmt.Sprintf("%q has no valid port", s)}
	}
	if err := model.ValidPort(port); err != nil {
		return "", 0, err
	}
	return hostPart, port, nil
}

// CheckTarget has the node try one target: connect to it, shake hands the way
// a client would, and judge what comes back. What is wrong with the target is
// in the result; an error is for what stopped the check, such as a target that
// isn't written as host:port, or an agent too old to run it.
func (e *Env) CheckTarget(ctx context.Context, c *agent.Client, target string) (_ HandshakeCheck, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	host, port, err := parseTarget(target)
	if err != nil {
		return HandshakeCheck{}, err
	}
	return checkTarget(ctx, c, host, port)
}

func checkTarget(ctx context.Context, c *agent.Client, host string, port int) (HandshakeCheck, error) {
	// the name in the hello is the host's, which an address can't be
	sni := host
	if net.ParseIP(host) != nil {
		sni = ""
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout+6*time.Second)
	defer cancel()
	resp, err := c.ProbeRealityDest(ctx, &pb.RealityDestProbeRequest{
		Dest:        net.JoinHostPort(host, strconv.Itoa(port)),
		ServerNames: []string{sni},
		TimeoutMs:   int32(probeTimeout / time.Millisecond),
	})
	if err != nil {
		return HandshakeCheck{}, probeFailure(err, c)
	}
	if len(resp.Checks) != 1 {
		return HandshakeCheck{}, &opError{msg: "The node's check of the target didn't come back with an answer.", cause: nil}
	}
	return handshakeCheck(resp.Checks[0]), nil
}

func probeFailure(err error, c *agent.Client) error {
	if down := linkDown(err, c); down != nil && !ctxDone(err) {
		return down
	}
	switch {
	case ctxDone(err):
		return err
	case agent.IsUnimplemented(err):
		return newErr(ErrAgentOutdated, err, "This node's agent is too old to check Reality targets. Update the agent first.")
	}
	return &opError{msg: sentence("The node couldn't check the target: " + statusMessage(err)), cause: err}
}

// handshakeCheck is what the agent found, as the page reads it. What is wrong
// is said in the plain words below, except for what the node couldn't do at
// all (connect, shake hands), where its own words say more.
func handshakeCheck(c *pb.RealityDestCheck) HandshakeCheck {
	h := HandshakeCheck{
		OK:             c.Ok,
		TLS13:          c.TlsVersion == "TLS 1.3",
		X25519:         c.KeyShare == "X25519" || c.KeyShare == "X25519MLKEM768",
		HandshakeBytes: int(c.FlightBytes),
		LatencyMs:      int(c.HandshakeMs),
	}
	if h.OK && h.HandshakeBytes > maxHandshakeBytes {
		h.OK = false
		h.Error = fmt.Sprintf("Its side of the handshake is %d bytes, and Xray reads at most 8 KB of it. A site with a smaller certificate would do.", h.HandshakeBytes)
	}
	if !h.OK && h.Error == "" {
		h.Error = explainCheck(c)
	}
	return h
}

var checkReasons = map[string]string{
	"hello_retry_request": "It asks for the key exchange to start over, which Xray can't follow.",
	"tls_version":         "It doesn't use TLS 1.3.",
	"cipher_suite":        "It chose a cipher suite that Xray can't mirror.",
	"key_share":           "It doesn't use the X25519 key exchange.",
	"no_ccs":              "Its handshake isn't laid out the way Xray needs.",
	"record_version":      "Its handshake isn't laid out the way Xray needs.",
	"unexpected_record":   "Its handshake isn't laid out the way Xray needs.",
	"record_too_large":    "Its handshake has records over 8 KB, which Xray can't read.",
	"no_response":         "It doesn't answer the handshake.",
}

func explainCheck(c *pb.RealityDestCheck) string {
	if text, ok := checkReasons[c.Reason]; ok {
		return text
	}
	if c.Message != "" {
		return sentence(c.Message)
	}
	return "The node couldn't use it."
}

// ProbeTargets has the node check each candidate, a few at a time, and picks
// the best of them. Candidates are host or host:port, with 443 for the port;
// none given means DefaultTargets. A target that doesn't work isn't an error:
// it is a result that says why. An error is for what stopped the checks.
func (e *Env) ProbeTargets(ctx context.Context, c *agent.Client, candidates []string) (_ TargetChoice, err error) {
	defer func() { err = ctxErr(ctx, err) }()
	if len(candidates) == 0 {
		candidates = DefaultTargets()
	}
	type cand struct {
		host   string
		port   int
		target string
	}
	var list []cand
	seen := map[string]bool{}
	for _, s := range candidates {
		host, port, err := parseTarget(s)
		if err != nil {
			return TargetChoice{}, err
		}
		t := net.JoinHostPort(host, strconv.Itoa(port))
		if !seen[t] {
			seen[t] = true
			list = append(list, cand{host, port, t})
		}
	}
	if len(list) > maxCandidates {
		return TargetChoice{}, &model.InvalidError{Msg: fmt.Sprintf("at most %d targets can be checked at once", maxCandidates)}
	}

	results := make([]TargetResult, len(list))
	errs := make([]error, len(list))
	slots := make(chan struct{}, 4) // the agent does the same with the names of one call
	var wg sync.WaitGroup
	for i, cd := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			chk, err := checkTarget(ctx, c, cd.host, cd.port)
			results[i], errs[i] = TargetResult{Target: cd.target, Check: chk}, err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return TargetChoice{}, err
		}
	}

	choice := TargetChoice{Results: results}
	best := -1
	for i, r := range results {
		if r.Check.OK && (best < 0 || betterCheck(r.Check, results[best].Check)) {
			best = i
		}
	}
	if best >= 0 {
		choice.Best = results[best].Target
	}
	return choice, nil
}

// betterCheck is whether a is a better target than b, both working: the
// smaller handshake wins, since it has the most room under what Xray reads,
// then the quicker one. A tie goes to the one that came first.
func betterCheck(a, b HandshakeCheck) bool {
	if a.HandshakeBytes != b.HandshakeBytes {
		return a.HandshakeBytes < b.HandshakeBytes
	}
	return a.LatencyMs < b.LatencyMs
}
