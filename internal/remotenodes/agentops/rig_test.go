// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

func sum256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func gz(t testing.TB, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// fakeFiles hands out builds of its own: an "agent" that is a script, so the
// sandbox can run it, and an "Xray" of random bytes.
type fakeFiles struct {
	t       *testing.T
	version string // of the agent
	padding int    // bytes of the agent beyond its script, to make an upload take a while
	xrayPad int    // the same for the Xray build, which is 20 KB without it

	mu       sync.Mutex
	agentErr error
	xrayErr  error
	asked    []string
	built    map[string]any // a build is made once, so asking again gives the same bytes
}

func newFakeFiles(t *testing.T) *fakeFiles { return &fakeFiles{t: t, version: "v1.3.0"} }

func (f *fakeFiles) Agent(ctx context.Context, arch string) (*artifacts.Agent, error) {
	f.mu.Lock()
	f.asked = append(f.asked, "agent "+arch)
	err, built := f.agentErr, f.built["agent "+arch]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if built != nil {
		return built.(*artifacts.Agent), nil
	}
	// what `nasnet-agent version` prints on a real one
	script := fmt.Sprintf("#!/bin/sh\n# a test agent for %s\nif [ \"$1\" = version ]; then echo \"nasnet-agent %s\"; fi\nexit 0\n", arch, f.version)
	plain := append([]byte(script), randomBytes(f.padding)...)
	packed := gz(f.t, plain)
	a := &artifacts.Agent{
		Version: f.version, Arch: arch, Gz: packed, GzSHA256: sum256(packed),
		PlainSize: int64(len(plain)), PlainSHA256: sum256(plain), Sig: make([]byte, 64),
	}
	f.keep("agent "+arch, a)
	return a, nil
}

func (f *fakeFiles) keep(key string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.built == nil {
		f.built = map[string]any{}
	}
	f.built[key] = v
}

func (f *fakeFiles) Xray(ctx context.Context, arch string) (*artifacts.Xray, error) {
	f.mu.Lock()
	f.asked = append(f.asked, "xray "+arch)
	err, built := f.xrayErr, f.built["xray "+arch]
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if built != nil {
		return built.(*artifacts.Xray), nil
	}
	plain := append([]byte("fake xray build\n"), randomBytes(20_000+f.xrayPad)...)
	x := &artifacts.Xray{Version: pin.Version, Arch: arch, Gz: gz(f.t, plain), PlainSize: int64(len(plain)), PlainSHA256: sum256(plain)}
	f.keep("xray "+arch, x)
	return x, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

const (
	pinnedKey = "SHA256:pinnedpinnedpinnedpinnedpinnedpinnedpinned0"
	rigHost   = "203.0.113.7" // documentation range: nothing dials it, the SSH connection is a stand-in
)

// installRig is an Env, a server in a sandbox, and what a test needs to see of
// the install: its steps, and what it asked the SSH library for.
type installRig struct {
	t     *testing.T
	sb    *sandbox
	env   *Env
	ca    *pki.CA
	files *fakeFiles
	node  model.Node
	login Login
	shell *fakeShell

	mu        sync.Mutex
	steps     []Step
	targets   []installer.Target
	verifyErr error
	verified  []*artifacts.Agent
	shows     string // the host key the server shows
	connectFn func(ctx context.Context, t installer.Target) (Shell, error)
}

func newInstallRig(t *testing.T) *installRig {
	t.Helper()
	sb := newSandbox(t)
	ca := testCA(t)
	r := &installRig{
		t: t, sb: sb, ca: ca, files: newFakeFiles(t),
		shell: &fakeShell{sb: sb, root: true},
		shows: pinnedKey,
		node: model.Node{
			ID: 3, Name: "Frankfurt", Host: rigHost, SSHPort: 22, SSHUser: "root",
			AgentPort: 9090, EntryPort: 443, SSHHostKey: pinnedKey,
		},
		login: Login{Password: "hunter2"},
	}
	r.env = &Env{CA: ca, Files: r.files, startWait: 600 * time.Millisecond, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r.env.connectSSH = func(ctx context.Context, tg installer.Target) (Shell, error) {
		r.mu.Lock()
		r.targets = append(r.targets, tg)
		fn, shows := r.connectFn, r.shows
		r.mu.Unlock()
		if fn != nil {
			return fn(ctx, tg)
		}
		if tg.HostKey != shows {
			return nil, &installer.HostKeyError{Got: shows, Want: tg.HostKey}
		}
		return r.shell, nil
	}
	r.env.verify = func(ctx context.Context, node model.Node, ag *artifacts.Agent) (Versions, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.verified = append(r.verified, ag)
		if r.verifyErr != nil {
			return Versions{}, r.verifyErr
		}
		return Versions{Agent: ag.Version}, nil
	}
	return r
}

func (r *installRig) progress(s Step) {
	r.mu.Lock()
	r.steps = append(r.steps, s)
	r.mu.Unlock()
}

func (r *installRig) install() (InstallResult, error) {
	r.t.Helper()
	return r.env.InstallAgent(context.Background(), r.node, r.login, r.progress)
}

// stepNames is the steps that began, in order, once each.
func (r *installRig) stepNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var names []string
	for _, s := range r.steps {
		if s.State == StateRunning && (len(names) == 0 || names[len(names)-1] != s.Name) {
			names = append(names, s.Name)
		}
	}
	return names
}

// lastStep is the last thing said about a step.
func (r *installRig) lastStep(name string) Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	var last Step
	for _, s := range r.steps {
		if s.Name == name {
			last = s
		}
	}
	return last
}

// ran is the commands sent to the server, as the lines of a script.
func (r *installRig) ran() []string {
	var out []string
	for _, c := range r.sb.commands() {
		out = append(out, c.Cmd)
	}
	return out
}

// newCaptureLog is a logger that writes to w, so a test can read what was logged.
func newCaptureLog(w *strings.Builder) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func fmtSscan(s string, v any) (int, error) { return fmt.Sscan(s, v) }

// statMode is a file's permission bits as octal text, such as "0600".
func statMode(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04o", fi.Mode().Perm()), nil
}

func itoa(n int) string { return fmt.Sprint(n) }

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

func gunzipBytes(t testing.TB, b []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
