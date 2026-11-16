// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// probeReq is a request the bridge would take: a VLESS outbound over REALITY.
func probeReq() bridgespec.ProbeRequest {
	return bridgespec.ProbeRequest{
		Outbound: bridgespec.Outbound{
			Tag: "out-3.", Protocol: bridgespec.ProtoVLESS, Addr: "198.51.100.7", Port: 443,
			ID: "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f", Flow: bridgespec.VisionFlow,
			Security: bridgespec.SecReality, ServerName: "www.example.com",
			PublicKey: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), ShortID: "ab12",
		},
		From: "192.168.50.16", Connect: true, URL: "https://example.com/204", TimeoutSeconds: 4,
	}
}

// probeLog is a file the fake notes its runs in, and the lines it holds.
func probeLog(t *testing.T) (path string, lines func() [][]string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "probes")
	return path, func() [][]string {
		b, _ := os.ReadFile(path)
		var out [][]string
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l != "" {
				out = append(out, strings.Fields(l))
			}
		}
		return out
	}
}

func TestAProbeRunsTheProgramWithTheRequestAndReadsItsResult(t *testing.T) {
	for name, c := range map[string]struct {
		mode    string
		connect bool
		want    bridgespec.ProbeResult
	}{
		"a web request through it":       {"probe-ok", true, bridgespec.ProbeResult{OK: true, DelayMS: 42, Status: 204}},
		"only built":                     {"probe-ok", false, bridgespec.ProbeResult{OK: true}},
		"refused, with a log line first": {"probe-refuse", true, bridgespec.ProbeResult{Error: "Xray refuses the outbound: invalid user id"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := supervise(t, c.mode)
			req := probeReq()
			req.Connect = c.connect
			got, err := s.Probe(t.Context(), req)
			if err != nil || got != c.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, c.want)
			}
		})
	}
}

func TestTheProgramGetsTheWholeRequestAndTheBridgesSettings(t *testing.T) {
	s := supervise(t, "probe-echo")
	got, err := s.Probe(t.Context(), probeReq())
	if err != nil {
		t.Fatal(err)
	}
	want := `tag="out-3." addr=198.51.100.7 port=443 from="192.168.50.16" connect=true url="https://example.com/204" timeout=4 memlimit=64MiB`
	if got.OK || got.Error != want {
		t.Fatalf("the program got\n%s\nwant\n%s", got.Error, want)
	}
	// and a probe leaves the supervised bridge alone
	if st := s.Status(); st.State != StateStopped || st.Restarts != 0 {
		t.Errorf("the supervisor after a probe: %+v", st)
	}
}

func TestAProbeWithoutAResultIsAnError(t *testing.T) {
	for mode, says := range map[string]string{
		"probe-garbage": "it printed no result",
		"probe-crash":   "exit status 3: panic: boom",
	} {
		t.Run(mode, func(t *testing.T) {
			s := supervise(t, mode)
			_, err := s.Probe(t.Context(), probeReq())
			if err == nil || !strings.Contains(err.Error(), says) || errors.Is(err, ErrNoProgram) {
				t.Fatalf("err = %v, want one saying %q", err, says)
			}
		})
	}
}

// A machine without the program says so in a way of its own, which the hub
// takes as "testing can't be done here" and not as a failed test.
func TestNoProgramIsADistinctError(t *testing.T) {
	s := supervise(t, "probe-ok", func(c *Config) { c.Binary = filepath.Join(t.TempDir(), "remote-nodes-bridge") })
	if _, err := s.Probe(t.Context(), probeReq()); !errors.Is(err, ErrNoProgram) {
		t.Fatalf("err = %v, want ErrNoProgram", err)
	}
	notExec := filepath.Join(t.TempDir(), "bridge")
	if err := os.WriteFile(notExec, []byte("not a program"), 0o600); err != nil {
		t.Fatal(err)
	}
	s = supervise(t, "probe-ok", func(c *Config) { c.Binary = notExec })
	if _, err := s.Probe(t.Context(), probeReq()); !errors.Is(err, ErrNoProgram) {
		t.Fatalf("a file that can't be run: %v", err)
	}
}

func TestARequestTheBridgeWouldRefuseIsNotRun(t *testing.T) {
	path, lines := probeLog(t)
	s := supervise(t, "probe-ok", func(c *Config) { c.Env = append(c.Env, "BRIDGECTL_PROBE_LOG="+path) })
	req := probeReq()
	req.Outbound.Addr = "de.example.com"
	if _, err := s.Probe(t.Context(), req); err == nil || !strings.Contains(err.Error(), "outbound.addr") {
		t.Fatalf("err = %v", err)
	}
	if got := lines(); len(got) != 0 {
		t.Fatalf("the program ran: %v", got)
	}
}

func TestAProbeThatHangsIsStoppedAfterItsTime(t *testing.T) {
	path, lines := probeLog(t)
	s := supervise(t, "probe-hang", func(c *Config) { c.Env = append(c.Env, "BRIDGECTL_PROBE_LOG="+path) })
	s.probeMargin = 200 * time.Millisecond
	req := probeReq()
	req.TimeoutSeconds = 1
	began := time.Now()
	_, err := s.Probe(t.Context(), req)
	took := time.Since(began)
	if err == nil || !strings.Contains(err.Error(), "took longer than 1.2s") {
		t.Fatalf("err = %v", err)
	}
	if took > 5*time.Second {
		t.Fatalf("it took %s", took)
	}
	pidGone(t, lines)
}

func TestAProbeEndsWithItsContext(t *testing.T) {
	path, lines := probeLog(t)
	s := supervise(t, "probe-hang", func(c *Config) { c.Env = append(c.Env, "BRIDGECTL_PROBE_LOG="+path) })
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		eventually(t, 10*time.Second, "the probe to start", func() bool { return len(lines()) > 0 })
		cancel()
	}()
	if _, err := s.Probe(ctx, probeReq()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	pidGone(t, lines)
}

// pidGone checks that the process the log names has gone.
func pidGone(t *testing.T, lines func() [][]string) {
	t.Helper()
	got := lines()
	if len(got) == 0 {
		t.Fatal("the program never ran")
	}
	pid, _ := strconv.Atoi(got[0][1])
	eventually(t, 5*time.Second, "the program to be gone", func() bool { return !alive(pid) })
}

func TestProbesRunOneAtATime(t *testing.T) {
	path, lines := probeLog(t)
	s := supervise(t, "probe-slow", func(c *Config) { c.Env = append(c.Env, "BRIDGECTL_PROBE_LOG="+path) })
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if got, err := s.Probe(t.Context(), probeReq()); err != nil || !got.OK {
				t.Errorf("%+v, %v", got, err)
			}
		})
	}
	wg.Wait()
	got := lines()
	if len(got) != 6 {
		t.Fatalf("log: %v", got)
	}
	for i := 0; i < len(got); i += 2 {
		if got[i][0] != "start" || got[i+1][0] != "end" || got[i][1] != got[i+1][1] {
			t.Fatalf("two probes overlapped: %v", got)
		}
	}

	// and one that waits gives up with its context
	hold := make(chan struct{})
	s.probeGate <- struct{}{}
	go func() { <-hold; <-s.probeGate }()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := s.Probe(ctx, probeReq()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting past its context: %v", err)
	}
	close(hold)
}

func TestWhatIsKeptOfTheOutputIsCapped(t *testing.T) {
	c := &capped{max: 8}
	for _, p := range []string{"12345", "67890", "abc"} {
		if n, err := c.Write([]byte(p)); n != len(p) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", p, n, err)
		}
	}
	if got := c.b.String(); got != "12345678" {
		t.Fatalf("kept %q", got)
	}
	if got := lastLine("a\nb\n  " + strings.Repeat("x", 400) + "  \n"); len(got) != 300+len("…") {
		t.Fatalf("last line is %d bytes", len(got))
	}
	if res, ok := lastResult([]byte("{\"ok\":true}\nlog\n{\"ok\":false,\"error\":\"last\"}\n")); !ok || res.Error != "last" {
		t.Fatalf("lastResult = %+v, %v", res, ok)
	}
}
