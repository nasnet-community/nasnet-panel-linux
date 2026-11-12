// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

func eventually(t testing.TB, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", within, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// specFor is a valid spec whose first node is called tag. Its status page is
// on a free loopback port, which the fake bridge serves.
func specFor(t testing.TB, tag string) *bridgespec.Spec {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return &bridgespec.Spec{
		Version: bridgespec.Version,
		Nodes: []bridgespec.Node{{
			Tag:        tag,
			Addr:       "127.0.0.1",
			Port:       4443,
			UUID:       "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f",
			PublicKey:  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
			ShortID:    "0123456789abcdef",
			ServerName: "localhost",
			ReverseTag: "rvs-from-" + tag,
		}},
		DialAddr:   "127.0.0.1",
		Exit:       bridgespec.Exit{Addr: "127.0.0.1"},
		DNS:        bridgespec.DNS{Servers: []string{"9.9.9.9"}},
		StatusAddr: addr,
	}
}

// supervise makes a supervisor that runs the fake bridge in the given mode,
// with timings short enough for tests. It stops it when the test ends.
func supervise(t *testing.T, mode string, edit ...func(*Config)) *Supervisor {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Binary:     self,
		Env:        []string{"BRIDGECTL_FAKE=" + mode},
		MemLimit:   "64MiB",
		Backoff:    Backoff{Min: 20 * time.Millisecond, Max: 80 * time.Millisecond, Healthy: time.Hour},
		StopGrace:  3 * time.Second,
		StatusWait: 300 * time.Millisecond,
		Logf:       t.Logf,
	}
	for _, e := range edit {
		e(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestStartRunsTheBridgeWithTheSpec(t *testing.T) {
	s := supervise(t, "serve")
	if st := s.Status(); st.State != StateStopped || st.PID != 0 || st.Bridge != nil {
		t.Fatalf("before Start: %+v", st)
	}
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.State != StateRunning || st.PID == 0 || st.Started.IsZero() || st.Restarts != 0 || st.LastExit != "" {
		t.Fatalf("after Start: %+v", st)
	}
	pid := st.PID
	eventually(t, 10*time.Second, "the bridge to read the spec", func() bool {
		return strings.Contains(s.Output(), "fake bridge up: n1")
	})
	if !alive(pid) {
		t.Fatal("no such process")
	}

	s.Stop()
	st = s.Status()
	if st.State != StateStopped || st.PID != 0 || !st.Started.IsZero() || st.Bridge != nil {
		t.Fatalf("after Stop: %+v", st)
	}
	if alive(pid) {
		t.Error("the child outlived Stop")
	}
	if !strings.Contains(s.Output(), "fake bridge stopping") {
		t.Errorf("the child was not asked to stop gracefully:\n%s", s.Output())
	}
	if st.LastExit != "exited" {
		t.Errorf("last exit %q after a clean stop", st.LastExit)
	}
}

func TestStatusMergesTheBridgesPage(t *testing.T) {
	s := supervise(t, "serve")
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	var st Status
	eventually(t, 10*time.Second, "the bridge's status page", func() bool {
		st = s.Status()
		return st.Bridge != nil
	})
	if st.State != StateRunning || st.BridgeError != "" {
		t.Fatalf("status %+v", st)
	}
	n := st.Bridge.Nodes[0]
	if !st.Bridge.Running || st.Bridge.Xray != "fake" || n.Tag != "n1" || !n.Up || n.BytesUp != 7 || n.BytesDown != 9 || n.Connections != 2 {
		t.Fatalf("the page came through as %+v", st.Bridge)
	}
}

func TestStatusWhenThePageIsNotThere(t *testing.T) {
	s := supervise(t, "noserve")
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.State != StateRunning || st.Bridge != nil || st.BridgeError == "" {
		t.Fatalf("status %+v: want a running child, no page, and the reason", st)
	}
}

func TestStatusJSON(t *testing.T) {
	data, err := json.Marshal(Status{State: StateStopped})
	if err != nil {
		t.Fatal(err)
	}
	// What isn't known is left out, not written as a time at year 1.
	if got := string(data); got != `{"state":"stopped","restarts":0}` {
		t.Fatalf("a stopped supervisor marshals as %s", got)
	}
}

// The child gets a memory limit and the environment the caller listed, and
// none of the hub's own.
func TestChildEnvironment(t *testing.T) {
	t.Setenv("BRIDGECTL_HUB_SECRET", "hunter2")
	s := supervise(t, "env")
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the environment to be printed", func() bool {
		return strings.Contains(s.Output(), "env: GOMEMLIMIT=64MiB")
	})
	var seen []string
	for _, line := range strings.Split(s.Output(), "\n") {
		if v, ok := strings.CutPrefix(line, "env: "); ok {
			name, _, _ := strings.Cut(v, "=")
			seen = append(seen, name)
		}
	}
	// PWD is the one the standard library adds when it sets a directory.
	for _, name := range seen {
		switch name {
		case "GOMEMLIMIT", "BRIDGECTL_FAKE", "PWD":
		default:
			t.Errorf("the child has %s in its environment", name)
		}
	}
	if strings.Contains(s.Output(), "hunter2") {
		t.Error("the hub's secret reached the child")
	}
}

func TestDefaultMemLimit(t *testing.T) {
	s := supervise(t, "env", func(c *Config) { c.MemLimit = "" })
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	want := "env: GOMEMLIMIT=" + DefaultMemLimit
	eventually(t, 10*time.Second, "the default limit", func() bool { return strings.Contains(s.Output(), want) })
}

func TestRestartsAfterACrash(t *testing.T) {
	s := supervise(t, "crash")
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "three restarts", func() bool { return s.Status().Restarts >= 3 })
	if st := s.Status(); st.LastExit != "exit status 3" || st.LastExitAt.IsZero() {
		t.Errorf("last exit %q at %v", st.LastExit, st.LastExitAt)
	}
	if n := strings.Count(s.Output(), "boom"); n < 3 {
		t.Errorf("the output has %d crashes in it:\n%s", n, s.Output())
	}
	s.Stop()
	after := s.Status().Restarts
	time.Sleep(200 * time.Millisecond)
	if st := s.Status(); st.Restarts != after || st.State != StateStopped {
		t.Errorf("a stopped supervisor went on restarting: %+v", st)
	}
}

func TestBackoffState(t *testing.T) {
	s := supervise(t, "crash", func(c *Config) { c.Backoff = Backoff{Min: time.Hour, Max: time.Hour, Healthy: time.Hour} })
	before := time.Now()
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the child to die", func() bool { return s.Status().State == StateBackoff })
	st := s.Status()
	if st.PID != 0 || st.Bridge != nil || st.LastExit != "exit status 3" {
		t.Errorf("status %+v", st)
	}
	if !st.RetryAt.After(before.Add(59 * time.Minute)) {
		t.Errorf("retry at %v, want about an hour after %v", st.RetryAt, before)
	}
	// Stopping out of a backoff is immediate.
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop waited for the backoff")
	}
	if st := s.Status(); st.State != StateStopped || !st.RetryAt.IsZero() {
		t.Errorf("after Stop: %+v", st)
	}
}

// A child that has died is in backoff, and Status says when the next is due from
// the first moment it says backoff at all. The message "the bridge ended" is
// written between the two steps of going into backoff, so a reader that looks
// then sees what a reader at any other moment could.
func TestBackoffComesWithItsRetryTime(t *testing.T) {
	var (
		s   *Supervisor
		mu  sync.Mutex
		bad []string
	)
	s = supervise(t, "crash", func(c *Config) {
		c.Backoff = Backoff{Min: time.Hour, Max: time.Hour, Healthy: time.Hour}
		c.Logf = func(format string, args ...any) {
			t.Logf(format, args...)
			if strings.HasPrefix(format, "the bridge ended") {
				if st := s.Status(); st.State == StateBackoff && st.RetryAt.IsZero() {
					mu.Lock()
					bad = append(bad, fmt.Sprintf("%+v", st))
					mu.Unlock()
				}
			}
		}
	})
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the child to die", func() bool { return s.Status().State == StateBackoff })
	mu.Lock()
	defer mu.Unlock()
	if len(bad) > 0 {
		t.Errorf("backoff with no retry time: %v", bad)
	}
}

func TestStopKillsAChildThatWontGo(t *testing.T) {
	s := supervise(t, "stubborn", func(c *Config) { c.StopGrace = 500 * time.Millisecond })
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the child to start", func() bool { return strings.Contains(s.Output(), "not leaving") })
	pid := s.Status().PID

	began := time.Now()
	s.Stop()
	took := time.Since(began)
	if took < 500*time.Millisecond || took > 5*time.Second {
		t.Errorf("Stop took %s, want the 500ms grace and then the kill", took)
	}
	if alive(pid) {
		t.Error("the child survived the kill")
	}
	if st := s.Status(); !strings.Contains(st.LastExit, "killed") {
		t.Errorf("last exit %q, want a kill", st.LastExit)
	}
	if !strings.Contains(s.Output(), "killing it") {
		t.Errorf("no word of the kill in the output:\n%s", s.Output())
	}
}

func TestUpdateRestartsWithTheNewSpec(t *testing.T) {
	s := supervise(t, "serve")
	one := specFor(t, "n1")
	if err := s.Start(one); err != nil {
		t.Fatal(err)
	}
	var first Status
	eventually(t, 10*time.Second, "the first page", func() bool {
		first = s.Status()
		return first.Bridge != nil && first.Bridge.Nodes[0].Tag == "n1"
	})

	// The new spec has the same status address, so the old child has to be
	// gone before the new one can serve.
	two := specFor(t, "n2")
	two.StatusAddr = one.StatusAddr
	if err := s.Update(two); err != nil {
		t.Fatal(err)
	}
	var st Status
	eventually(t, 10*time.Second, "the page of the new spec", func() bool {
		st = s.Status()
		return st.Bridge != nil && st.Bridge.Nodes[0].Tag == "n2"
	})
	if st.PID == first.PID || st.Restarts != 1 || st.State != StateRunning {
		t.Errorf("status %+v after Update; the first child was pid %d", st, first.PID)
	}
	if alive(first.PID) {
		t.Error("the old child is still running")
	}
	if out := s.Output(); !strings.Contains(out, "fake bridge up: n2") {
		t.Errorf("the new child didn't get the new spec:\n%s", out)
	}
}

// Update skips the wait a crash would bring: the supervisor has been told
// something changed, so it tries at once.
func TestUpdateDoesNotWaitOutABackoff(t *testing.T) {
	s := supervise(t, "crash", func(c *Config) { c.Backoff = Backoff{Min: time.Hour, Max: time.Hour, Healthy: time.Hour} })
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the backoff", func() bool { return s.Status().State == StateBackoff })
	if err := s.Update(specFor(t, "n2")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the new child to start and die", func() bool {
		return s.Status().Restarts == 1 && strings.Count(s.Output(), "boom") == 2
	})
}

func TestUpdateBeforeStartStarts(t *testing.T) {
	s := supervise(t, "serve")
	if err := s.Update(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.State != StateRunning || st.Restarts != 0 {
		t.Fatalf("status %+v", st)
	}
}

func TestStartRefusesWhatCantRun(t *testing.T) {
	t.Run("twice", func(t *testing.T) {
		s := supervise(t, "serve")
		if err := s.Start(specFor(t, "n1")); err != nil {
			t.Fatal(err)
		}
		pid := s.Status().PID
		if err := s.Start(specFor(t, "n2")); !errors.Is(err, ErrRunning) {
			t.Fatalf("got %v", err)
		}
		if st := s.Status(); st.PID != pid {
			t.Errorf("the second Start replaced the child: %+v", st)
		}
	})
	t.Run("an invalid spec", func(t *testing.T) {
		s := supervise(t, "serve")
		spec := specFor(t, "n1")
		spec.Nodes[0].UUID = "nope"
		if err := s.Start(spec); err == nil || !strings.Contains(err.Error(), "UUID") {
			t.Fatalf("got %v", err)
		}
		if err := s.Update(spec); err == nil {
			t.Fatal("Update took an invalid spec")
		}
		if st := s.Status(); st.State != StateStopped || st.PID != 0 {
			t.Errorf("something started: %+v", st)
		}
	})
	t.Run("a missing binary", func(t *testing.T) {
		s := supervise(t, "serve", func(c *Config) { c.Binary = "/nonexistent/remote-nodes-bridge" })
		if err := s.Start(specFor(t, "n1")); err == nil {
			t.Fatal("started a program that isn't there")
		}
		if st := s.Status(); st.State != StateStopped {
			t.Errorf("status %+v", st)
		}
		// And a later Start isn't blocked by the failed one.
		if err := s.Start(specFor(t, "n1")); err == nil || errors.Is(err, ErrRunning) {
			t.Fatalf("second try: %v", err)
		}
	})
}

func TestStopIsHarmless(t *testing.T) {
	s := supervise(t, "serve")
	s.Stop() // before Start
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s.Stop()
	}
	// It can be started again afterwards, from a clean slate.
	if err := s.Start(specFor(t, "n2")); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.State != StateRunning || st.Restarts != 0 || st.LastExit != "" {
		t.Fatalf("after a second Start: %+v", st)
	}
}

// What the bridge prints is kept, but only the last OutputBytes of it.
func TestOutputIsBounded(t *testing.T) {
	const size = 4096
	s := supervise(t, "spam", func(c *Config) { c.OutputBytes = size })
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 20*time.Second, "the spam to finish", func() bool { return strings.Contains(s.Output(), "spam done") })
	out := s.Output()
	if len(out) > size {
		t.Fatalf("kept %d bytes, the limit is %d", len(out), size)
	}
	if len(out) < size/2 {
		t.Errorf("kept only %d bytes of a %d byte ring", len(out), size)
	}
	if !strings.Contains(out, "spam line 49999") || strings.Contains(out, "spam line 0\n") {
		t.Errorf("not the tail of the output:\n%.200s", out)
	}
	if !strings.HasPrefix(out, "spam line ") {
		t.Errorf("starts with a cut line: %.40q", out)
	}
}

// The credential is skipped, with a word about it, when the hub can't change
// user: the tests and a dev run have to work as themselves.
func TestCredentialSkippedWithoutRoot(t *testing.T) {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		t.Skip("the hub would really switch user here; TestCredentialAsRoot covers that")
	}
	s := supervise(t, "serve", func(c *Config) { c.Credential = &Credential{UID: 65534, GID: 65534} })
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatalf("a credential must not stop a non-root start: %v", err)
	}
	st := s.Status()
	if st.State != StateRunning {
		t.Fatalf("status %+v", st)
	}
	want := "not root"
	if runtime.GOOS != "linux" {
		want = "Linux only"
	}
	if !strings.Contains(st.Credential, want) {
		t.Errorf("Status.Credential %q, want a mention of %q", st.Credential, want)
	}
	if !strings.Contains(s.Output(), want) {
		t.Errorf("the output has no word about it:\n%s", s.Output())
	}
}

// As root on Linux the child really runs as the credential, with no groups.
func TestCredentialAsRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("changing user is supported on Linux only")
	}
	if os.Geteuid() != 0 {
		t.Skipf("changing user needs root, and the tests run as uid %d", os.Geteuid())
	}
	// go test builds into a 0700 directory, which the unprivileged user can't
	// enter to run the binary, so run a copy from one it can.
	dir, err := os.MkdirTemp("", "bridgectl-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	copied := dir + "/bridge"
	if err := os.WriteFile(copied, data, 0o755); err != nil {
		t.Fatal(err)
	}

	s := supervise(t, "whoami", func(c *Config) {
		c.Binary = copied
		c.Credential = &Credential{UID: 65534, GID: 65534}
	})
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the child to say who it is", func() bool { return strings.Contains(s.Output(), "whoami:") })
	if out := s.Output(); !strings.Contains(out, "uid=65534 gid=65534 groups=[]") {
		t.Fatalf("the child ran as:\n%s", out)
	}
	if st := s.Status(); !strings.Contains(st.Credential, "uid 65534") {
		t.Errorf("Status.Credential %q", st.Credential)
	}
}

func TestNewChecksItsConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("no binary")
	}
	for _, b := range []Backoff{
		{Min: 0, Max: time.Second, Healthy: time.Second},
		{Min: 2 * time.Second, Max: time.Second, Healthy: time.Second},
		{Min: time.Second, Max: time.Second, Healthy: -1},
	} {
		if _, err := New(Config{Binary: "x", Backoff: b}); err == nil {
			t.Errorf("backoff %+v accepted", b)
		}
	}
	s, err := New(Config{Binary: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if s.cfg.Backoff != DefaultBackoff || s.cfg.MemLimit != DefaultMemLimit || s.cfg.OutputBytes != DefaultOutputBytes ||
		s.cfg.StopGrace != DefaultStopGrace || s.cfg.StatusWait != DefaultStatusWait {
		t.Errorf("defaults not applied: %+v", s.cfg)
	}
	if DefaultBackoff != (Backoff{Min: time.Second, Max: 30 * time.Second, Healthy: time.Minute}) {
		t.Errorf("DefaultBackoff is %+v, want a second doubling to 30 seconds, reset after a minute", DefaultBackoff)
	}
}

// All the entry points at once, for the race detector.
func TestConcurrentUse(t *testing.T) {
	s := supervise(t, "serve")
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.Status()
					s.Output()
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		if err := s.Update(specFor(t, fmt.Sprintf("n%d", i+2))); err != nil {
			t.Error(err)
		}
	}
	s.Stop()
	close(stop)
	wg.Wait()
}

// withEntry is specFor with the router's own entry and one client on it.
func withEntry(t testing.TB, tag string) *bridgespec.Spec {
	t.Helper()
	spec := specFor(t, tag)
	spec.Local = &bridgespec.Local{
		ListenAddr:  "127.0.0.1",
		Port:        8443,
		PrivateKey:  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		ShortIDs:    []string{"0123456789abcdef"},
		ServerNames: []string{"localhost"},
		Target:      "127.0.0.1:9443",
		Clients:     []bridgespec.Client{{Email: "sub-1@remote-nodes", UUID: "11111111-2222-4333-8444-555555555551"}},
		Token:       "0123456789abcdef0123456789abcdef",
	}
	return spec
}

func waitForPage(t *testing.T, s *Supervisor) {
	t.Helper()
	eventually(t, 10*time.Second, "the bridge's status page", func() bool { return s.Status().Bridge != nil })
}

// The hub changes the entry's clients on the running bridge: the whole set,
// with the spec's token, and no restart.
func TestSetClientsReachesTheRunningBridge(t *testing.T) {
	s := supervise(t, "serve")
	if err := s.Start(withEntry(t, "n1")); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, s)
	pid := s.Status().PID

	second := bridgespec.Client{Email: "sub-2@remote-nodes", UUID: "11111111-2222-4333-8444-555555555552"}
	res, err := s.SetClients([]bridgespec.Client{second})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || res.Added[0] != second.Email || len(res.Removed) != 1 || res.Removed[0] != "sub-1@remote-nodes" || res.Closed != 1 {
		t.Fatalf("result %+v", res)
	}
	// nobody at all is a set too, and not a null the bridge would have to guess at
	if res, err = s.SetClients(nil); err != nil || len(res.Removed) != 1 {
		t.Fatalf("an empty set: %+v, %v", res, err)
	}
	if st := s.Status(); st.PID != pid || st.Restarts != 0 {
		t.Fatalf("the bridge was restarted for a change of clients: %+v", st)
	}

	// what the bridge refuses comes back as an error with its words, and no id
	_, err = s.SetClients([]bridgespec.Client{{Email: "fail", UUID: second.UUID}})
	if err == nil || !strings.Contains(err.Error(), "refused by the fake") || strings.Contains(err.Error(), second.UUID) {
		t.Fatalf("a refused set: %v", err)
	}
}

// With no bridge, or one that has no entry, there is nothing to send to.
func TestSetClientsNeedsARunningEntry(t *testing.T) {
	s := supervise(t, "serve")
	if _, err := s.SetClients(nil); !errors.Is(err, ErrNoEntry) {
		t.Fatalf("before any start: %v", err)
	}
	if err := s.Start(specFor(t, "n1")); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, s)
	if _, err := s.SetClients(nil); !errors.Is(err, ErrNoEntry) {
		t.Fatalf("a bridge with no entry: %v", err)
	}
	s.Stop()
	if _, err := s.SetClients(nil); !errors.Is(err, ErrNoEntry) {
		t.Fatalf("after a stop: %v", err)
	}
}

// A bridge whose page doesn't answer fails the call rather than hang it.
func TestSetClientsWhenThePageIsNotThere(t *testing.T) {
	s := supervise(t, "noserve")
	if err := s.Start(withEntry(t, "n1")); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if _, err := s.SetClients(nil); err == nil || errors.Is(err, ErrNoEntry) {
		t.Fatalf("got %v", err)
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("the call took %v", took)
	}
}

// A spec with the entry and no node runs like any other.
func TestABridgeWithOnlyTheEntry(t *testing.T) {
	s := supervise(t, "serve")
	spec := withEntry(t, "n1")
	spec.Nodes = nil
	if err := s.Start(spec); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, s)
	if st := s.Status(); st.State != StateRunning || len(st.Bridge.Nodes) != 0 {
		t.Fatalf("status %+v", st)
	}
	if _, err := s.SetClients(nil); err != nil {
		t.Fatal(err)
	}
}
