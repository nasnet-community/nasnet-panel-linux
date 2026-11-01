package agentops

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"
)

// what the SSH library says when a session is opened on a connection that
// something between the router and the server has cut
var errChannelGone = errors.New("ssh: unexpected packet in response to channel open: <nil>")

// A rig whose first connection can be cut, and whose second is a fresh one to
// the same server: what the hub meets when a long idle wait ends and the
// connection it was holding is gone.
func cuttableRig(t *testing.T) (r *installRig, cut *atomic.Bool, second *fakeShell) {
	r = newInstallRig(t)
	cut = &atomic.Bool{}
	first := r.shell
	first.failOn = func(string) error {
		if cut.Load() {
			return errChannelGone
		}
		return nil
	}
	second = &fakeShell{sb: r.sb, root: true}
	r.connectFn = func(ctx context.Context, tg installer.Target) (Shell, error) {
		if tg.HostKey != pinnedKey {
			return nil, &installer.HostKeyError{Got: tg.HostKey, Want: pinnedKey}
		}
		if len(r.targets) == 1 {
			return first, nil
		}
		return second, nil
	}
	return r, cut, second
}

func TestAConnectionThatDroppedDuringTheDownloadIsMadeAgain(t *testing.T) {
	old := keepAliveEvery
	keepAliveEvery = time.Hour // so that only the next command finds it out
	t.Cleanup(func() { keepAliveEvery = old })

	r, cut, second := cuttableRig(t)
	var ran atomic.Int32
	second.failOn = func(string) error { ran.Add(1); return nil }
	r.files.delay = 20 * time.Millisecond
	r.onStep = func(s Step) {
		if s.Name == StepDownloadAgent && s.State == StateRunning {
			cut.Store(true) // the connection is lost while the hub waits for the agent
		}
	}
	res, err := r.install()
	if err != nil {
		t.Fatalf("the install did not get over a dropped connection: %v", err)
	}
	if res.Arch != "amd64" {
		t.Errorf("result %+v", res)
	}
	if n := len(r.targets); n != 2 {
		t.Fatalf("signed in %d times, want 2", n)
	}
	// the second sign-in is the same as the first: the same user, port, login and the key that was pinned
	a, b := r.targets[0], r.targets[1]
	if a.Host != b.Host || a.Port != b.Port || a.User != b.User || a.Password != b.Password || a.HostKey != b.HostKey || b.HostKey != pinnedKey {
		t.Errorf("the second sign-in differs from the first: %+v and %+v", a, b)
	}
	if !r.sb.exists("/var/lib/nasnet-agent/remote-nodes.json") {
		t.Error("the install did not finish writing its marker")
	}
	// the connection that was cut is let go, and the rest of the install ran on the new one
	if !r.shell.closed {
		t.Error("the connection that was cut was not closed")
	}
	if n := ran.Load(); n < 5 {
		t.Errorf("only %d commands ran on the new connection", n)
	}
}

// What a command that ran says is its own answer, even when its words sound
// like a lost connection (a pipe it wrote to closed). It is never run again,
// and the connection is not remade for it.
func TestACommandThatRanAndFailedIsNotRunAgain(t *testing.T) {
	old := keepAliveEvery
	keepAliveEvery = time.Hour
	t.Cleanup(func() { keepAliveEvery = old })

	r := newInstallRig(t)
	var runs atomic.Int32
	r.shell.failOn = func(cmd string) error {
		if strings.Contains(cmd, "/usr/local/bin/.nasnet-agent.upload") {
			runs.Add(1)
			return &installer.CommandError{Cmd: cmd, Stderr: "tee: write error: broken pipe", Err: errors.New("Process exited with status 1")}
		}
		return nil
	}
	_, err := r.install()
	if err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("err = %v", err)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("the upload ran %d times, want once", n)
	}
	if n := len(r.targets); n != 1 {
		t.Errorf("signed in %d times, want once", n)
	}
}

// The connection is touched while the hub waits with nothing to say to the
// server, and not after.
func TestTheConnectionIsKeptAliveWhileTheAgentDownloads(t *testing.T) {
	old := keepAliveEvery
	keepAliveEvery = 15 * time.Millisecond
	t.Cleanup(func() { keepAliveEvery = old })

	r := newInstallRig(t)
	r.files.delay = 300 * time.Millisecond
	var pings atomic.Int32
	var after atomic.Bool
	var late atomic.Int32
	r.shell.failOn = func(cmd string) error {
		if cmd == "true" {
			pings.Add(1)
			if after.Load() {
				late.Add(1)
			}
		}
		return nil
	}
	r.onStep = func(s Step) {
		if s.Name == StepDownloadAgent && s.State == StateOK {
			after.Store(true)
		}
	}
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if n := pings.Load(); n < 5 {
		t.Errorf("%d keepalives over a 300 ms download at one every 15 ms, want at least 5", n)
	}
	if n := late.Load(); n != 0 {
		t.Errorf("%d keepalives after the download was over", n)
	}
	if n := len(r.targets); n != 1 {
		t.Errorf("signed in %d times, want once", n)
	}
}

// A connection that dies while the hub waits is found by the keepalive, which
// makes it again before the download is over, so the next command meets a live one.
func TestADeadConnectionIsFoundAndRemadeByTheKeepalive(t *testing.T) {
	old := keepAliveEvery
	keepAliveEvery = 15 * time.Millisecond
	t.Cleanup(func() { keepAliveEvery = old })

	r, cut, _ := cuttableRig(t)
	r.files.delay = 300 * time.Millisecond
	r.onStep = func(s Step) {
		if s.Name == StepDownloadAgent && s.State == StateRunning {
			cut.Store(true)
		}
	}
	var remadeBeforeTheEnd atomic.Bool
	r.onStep = func(s Step) {
		switch {
		case s.Name == StepDownloadAgent && s.State == StateRunning:
			cut.Store(true)
		case s.Name == StepDownloadAgent && s.State == StateOK:
			r.mu.Lock()
			remadeBeforeTheEnd.Store(len(r.targets) == 2)
			r.mu.Unlock()
		}
	}
	if _, err := r.install(); err != nil {
		t.Fatal(err)
	}
	if !remadeBeforeTheEnd.Load() {
		t.Error("the connection was not made again until a real command needed it")
	}
}

// A server that can't be signed in to again, such as a changed host key, is
// the answer, said as it always is.
func TestAConnectionThatCannotBeMadeAgainFailsTheStepPlainly(t *testing.T) {
	old := keepAliveEvery
	keepAliveEvery = time.Hour
	t.Cleanup(func() { keepAliveEvery = old })

	r, cut, _ := cuttableRig(t)
	r.files.delay = 10 * time.Millisecond
	r.onStep = func(s Step) {
		if s.Name == StepDownloadAgent && s.State == StateRunning {
			cut.Store(true)
		}
	}
	first := r.connectFn
	r.connectFn = func(ctx context.Context, tg installer.Target) (Shell, error) {
		if len(r.targets) > 1 {
			return nil, &installer.HostKeyError{Got: "SHA256:another", Want: tg.HostKey}
		}
		return first(ctx, tg)
	}
	_, err := r.install()
	if err == nil {
		t.Fatal("an install that lost its connection and could not get it back went on")
	}
	if strings.Contains(err.Error(), "unexpected packet") && !strings.Contains(err.Error(), "Couldn't") {
		t.Errorf("the raw SSH error reached the user: %v", err)
	}
}
