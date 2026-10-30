// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// loneSpec is a spec with one node whose portal isn't there, everything on
// loopback: enough for the process to start, serve and stop.
func loneSpec(t *testing.T) *bridgespec.Spec {
	t.Helper()
	return &bridgespec.Spec{
		Version: bridgespec.Version,
		Nodes: []bridgespec.Node{{
			Tag:        "n1",
			Addr:       "127.0.0.1",
			Port:       uint16(freePort(t)),
			UUID:       newUUID(),
			PublicKey:  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
			ShortID:    "0123456789abcdef",
			ServerName: "localhost",
			ReverseTag: "rvs-from-n1",
		}},
		DialAddr:   "127.0.0.1",
		Exit:       bridgespec.Exit{Addr: "127.0.0.1"},
		DNS:        bridgespec.DNS{Servers: []string{"9.9.9.9"}},
		StatusAddr: "127.0.0.1:18080",
	}
}

// waitExit waits for the process to end and returns its exit code, or -1 if
// a signal ended it.
func (rb *runningBridge) waitExit(t *testing.T, within time.Duration) int {
	t.Helper()
	select {
	case <-rb.exited:
	case <-time.After(within):
		t.Fatalf("the bridge was still running %s later\n%s", within, rb.out)
	}
	var ee *exec.ExitError
	switch {
	case rb.err == nil:
		return 0
	case errors.As(rb.err, &ee):
		return ee.ExitCode()
	default:
		t.Fatalf("waiting for the bridge: %v", rb.err)
		return -2
	}
}

// The hub closing its end of stdin is how it says stop when no signal can
// reach the bridge: it has to shut down, cleanly.
func TestProcessStopsWhenStdinCloses(t *testing.T) {
	rb := startBridge(t, loneSpec(t))
	if st := rb.status(); !st.Running || len(st.Nodes) != 1 {
		t.Fatalf("status %+v", st)
	}
	rb.stdin.Close()
	if code := rb.waitExit(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d\n%s", code, rb.out)
	}
	if _, err := rb.fetch(); err == nil {
		t.Error("the status page still answers after the bridge exited")
	}
}

func TestProcessStopsOnSIGTERM(t *testing.T) {
	rb := startBridge(t, loneSpec(t))
	if err := rb.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := rb.waitExit(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d\n%s", code, rb.out)
	}
}

// SIGINT takes the same way out, for a bridge started by hand.
func TestProcessStopsOnInterrupt(t *testing.T) {
	rb := startBridge(t, loneSpec(t))
	if err := rb.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if code := rb.waitExit(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d\n%s", code, rb.out)
	}
}

// Whatever else arrives on stdin after the spec is of no interest: only its
// closing is.
func TestProcessIgnoresWhatFollowsTheSpec(t *testing.T) {
	rb := startProcess(t, loneSpec(t))
	rb.stdin.Write([]byte(strings.Repeat("noise", 1000)))
	eventually(t, 20*time.Second, "the status page", func() bool { _, err := rb.fetch(); return err == nil })
	time.Sleep(500 * time.Millisecond)
	select {
	case <-rb.exited:
		t.Fatalf("the bridge stopped on its own: %v\n%s", rb.err, rb.out)
	default:
	}
	rb.stdin.Close()
	if code := rb.waitExit(t, 10*time.Second); code != 0 {
		t.Fatalf("exit code %d", code)
	}
}

func TestProcessRefusesBadInput(t *testing.T) {
	frame := func(payload string) []byte {
		b := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
		return append(b, payload...)
	}
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{"nothing", nil, "no spec"},
		{"half a length", []byte{0, 0}, "length"},
		{"empty frame", frame(""), "empty"},
		{"oversize frame", binary.BigEndian.AppendUint32(nil, bridgespec.MaxSize+1), "too large"},
		{"not JSON", frame("hello"), "invalid"},
		{"a spec that isn't one", frame(`{"version":1}`), "dial_addr: missing"},
		{"a bad address", frame(`{"version":1,"nodes":[],"dial_addr":"router.lan"}`), "dial_addr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := launch(t)
			rb.stdin.Write(tt.input)
			rb.stdin.Close()
			if code := rb.waitExit(t, 10*time.Second); code != 1 {
				t.Fatalf("exit code %d, want 1\n%s", code, rb.out)
			}
			if out := rb.out.String(); !strings.Contains(out, tt.want) {
				t.Fatalf("output %q does not mention %q", out, tt.want)
			}
		})
	}
}

// A taken status port must stop the bridge before its tunnels dial anything.
func TestProcessStatusPortInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	spec := loneSpec(t)
	spec.StatusAddr = l.Addr().String()
	rb := launch(t)
	if err := bridgespec.Write(rb.stdin, spec); err != nil {
		t.Fatal(err)
	}
	if code := rb.waitExit(t, 10*time.Second); code != 1 {
		t.Fatalf("exit code %d, want 1\n%s", code, rb.out)
	}
	out := rb.out.String()
	if !strings.Contains(out, "status page") || strings.Contains(out, "started") {
		t.Fatalf("output %q: want the status page error and no Xray start", out)
	}
}
