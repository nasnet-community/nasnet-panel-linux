package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

const (
	machineAMD64 = 0x3E
	machineARM64 = 0xB7
)

// xrayNode is a Server whose xray is a script in a directory of its own, so a
// test can see what an update leaves next to the installed build.
type xrayNode struct {
	s   *Server
	dir string
	bin string
}

// newXrayNode installs a fake xray that reports version v. The fakes are shell
// scripts and can't pass the ELF check, so it is swapped for a shebang check;
// a test that wants the real one sets s.xrayCheck back to nil.
func newXrayNode(t *testing.T, v string) *xrayNode {
	t.Helper()
	dir := t.TempDir()
	bin := fakeXray(t, dir, "node-xray", v)
	s := switchServer(t, bin)
	s.xrayCheck = scriptOnly
	return &xrayNode{s: s, dir: dir, bin: bin}
}

func scriptOnly(head []byte) error {
	if !bytes.HasPrefix(head, []byte("#!")) {
		return errors.New("not a script")
	}
	return nil
}

func (n *xrayNode) start(t *testing.T) {
	t.Helper()
	if err := n.s.xrayMgr.Start(); err != nil {
		t.Fatal(err)
	}
}

func (n *xrayNode) installed(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(n.bin)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// files lists what sits next to the installed build.
func (n *xrayNode) files(t *testing.T) []string {
	t.Helper()
	return listNames(t, n.dir)
}

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// build is the bytes of a fake xray that reports version v; an empty v gives
// one whose version probe fails.
func build(t *testing.T, v string) []byte {
	t.Helper()
	b, err := os.ReadFile(fakeXray(t, t.TempDir(), "build", v))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// elfBytes is a bare ELF header for the given e_machine, padded to size.
func elfBytes(machine byte, size int) []byte {
	b := make([]byte, size)
	copy(b, "\x7fELF")
	b[0x12] = machine
	return b
}

// machines gives the e_machine of this host and of the other one the agent
// knows; there is nothing to refuse on any other CPU, so the test skips.
func machines(t *testing.T) (host, other byte) {
	t.Helper()
	switch elfArchForHost() {
	case "amd64":
		return machineAMD64, machineARM64
	case "arm64":
		return machineARM64, machineAMD64
	}
	t.Skip("the ELF check has no opinion on " + runtime.GOARCH)
	return 0, 0
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hubServing is a download endpoint that wants the bearer token "tok".
func hubServing(t *testing.T, body []byte, hit func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hit != nil {
			hit()
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hubRequest(url string, body []byte, restart bool) *pb.UpdateXrayRequest {
	return &pb.UpdateXrayRequest{
		Version:       "26.11.1",
		RestartAfter:  restart,
		DownloadUrl:   url,
		DownloadToken: "tok",
		Checksum:      sum(body),
	}
}

func TestHubUpdateInstallsTheBuildAndRestartsXray(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	oldPID := n.s.xrayMgr.PID()
	next := build(t, "26.11.1")
	hub := hubServing(t, next, nil)

	resp, err := n.s.UpdateXrayBinary(context.Background(), hubRequest(hub.URL, next, true))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !strings.Contains(resp.Message, "26.11.1") {
		t.Fatalf("response = %+v", resp)
	}
	if !bytes.Equal(n.installed(t), next) {
		t.Fatal("the new build is not what is installed")
	}
	if !n.s.xrayMgr.IsRunning() || n.s.xrayMgr.PID() == oldPID {
		t.Fatalf("xray was not restarted (pid %d, was %d)", n.s.xrayMgr.PID(), oldPID)
	}
	if got := n.files(t); !slices.Equal(got, []string{"node-xray"}) {
		t.Fatalf("files left behind: %v", got)
	}
}

func TestHubUpdateStopsXrayBeforeTheDownload(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	next := build(t, "26.11.1")
	var runningDuringDownload bool
	hub := hubServing(t, next, func() { runningDuringDownload = n.s.xrayMgr.IsRunning() })

	resp, err := n.s.UpdateXrayBinary(context.Background(), hubRequest(hub.URL, next, true))
	if err != nil || !resp.Success {
		t.Fatalf("update: %+v, %v", resp, err)
	}
	if runningDuringDownload {
		t.Fatal("xray was still running while the build downloaded")
	}
}

func TestHubUpdateLeavesAStoppedXrayStoppedUnlessAskedToStartIt(t *testing.T) {
	for _, restart := range []bool{false, true} {
		n := newXrayNode(t, "26.10.10")
		next := build(t, "26.11.1")
		hub := hubServing(t, next, nil)

		resp, err := n.s.UpdateXrayBinary(context.Background(), hubRequest(hub.URL, next, restart))
		if err != nil || !resp.Success {
			t.Fatalf("restart=%v: %+v, %v", restart, resp, err)
		}
		if !bytes.Equal(n.installed(t), next) {
			t.Fatalf("restart=%v: the new build is not installed", restart)
		}
		if got := n.s.xrayMgr.IsRunning(); got != restart {
			t.Fatalf("restart=%v: running = %v", restart, got)
		}
	}
}

func TestHubUpdateKeepsTheOldBuildOnAChecksumMismatch(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old := n.installed(t)
	next := build(t, "26.11.1")
	hub := hubServing(t, next, nil)
	req := hubRequest(hub.URL, next, true)
	req.Checksum = sum([]byte("something else"))

	resp, err := n.s.UpdateXrayBinary(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.HasPrefix(resp.Message, "Checksum mismatch") {
		t.Fatalf("response = %+v", resp)
	}
	if !bytes.Equal(n.installed(t), old) || !n.s.xrayMgr.IsRunning() {
		t.Fatal("the old build is not back and running")
	}
}

func TestHubUpdateKeepsTheOldBuildWhenTheDownloadFails(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old := n.installed(t)
	next := build(t, "26.11.1")
	hub := hubServing(t, next, nil)
	req := hubRequest(hub.URL, next, true)
	req.DownloadToken = "wrong"

	resp, err := n.s.UpdateXrayBinary(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Success || resp.Message != "Download failed: HTTP 401" {
		t.Fatalf("response = %+v", resp)
	}
	if !bytes.Equal(n.installed(t), old) || !n.s.xrayMgr.IsRunning() {
		t.Fatal("the old build is not back and running")
	}
}

func TestHubUpdateRefusesABuildThatIsNotForThisHost(t *testing.T) {
	_, other := machines(t)
	for name, next := range map[string][]byte{
		"another architecture": elfBytes(other, 256),
		"not an ELF":           bytes.Repeat([]byte("x"), 64),
	} {
		n := newXrayNode(t, "26.10.10")
		n.s.xrayCheck = nil // the real check
		n.start(t)
		old := n.installed(t)
		hub := hubServing(t, next, nil)

		resp, err := n.s.UpdateXrayBinary(context.Background(), hubRequest(hub.URL, next, true))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Success || !strings.HasPrefix(resp.Message, "Refusing to install: ") {
			t.Fatalf("%s: response = %+v", name, resp)
		}
		if !bytes.Equal(n.installed(t), old) || !n.s.xrayMgr.IsRunning() {
			t.Fatalf("%s: the old build is not back and running", name)
		}
	}
}

func TestHubUpdateRollsBackWhenTheNewBuildWillNotStart(t *testing.T) {
	host, _ := machines(t)
	n := newXrayNode(t, "26.10.10")
	n.s.xrayCheck = nil
	n.start(t)
	old := n.installed(t)
	// A header the check accepts and the kernel doesn't.
	next := elfBytes(host, 256)
	hub := hubServing(t, next, nil)

	resp, err := n.s.UpdateXrayBinary(context.Background(), hubRequest(hub.URL, next, true))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Success || resp.ErrorCode != "RESTART_FAILED" || !strings.Contains(resp.Message, "rolled back") {
		t.Fatalf("response = %+v", resp)
	}
	if !bytes.Equal(n.installed(t), old) || !n.s.xrayMgr.IsRunning() {
		t.Fatal("the old build is not back and running")
	}
}

func TestHubUpdateRollsBackWhenTheNewBuildReportsNoVersion(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old := n.installed(t)
	next := build(t, "")
	hub := hubServing(t, next, nil)

	resp, err := n.s.UpdateXrayBinary(context.Background(), hubRequest(hub.URL, next, true))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Success || resp.ErrorCode != "POST_INSTALL_VERIFY_FAILED" {
		t.Fatalf("response = %+v", resp)
	}
	if !bytes.Equal(n.installed(t), old) || !n.s.xrayMgr.IsRunning() {
		t.Fatal("the old build is not back and running")
	}
}

func TestUpdateXrayBinaryRefusesBadRequests(t *testing.T) {
	cases := []struct {
		name string
		req  *pb.UpdateXrayRequest
		code string
		msg  string
	}{
		{"no version", &pb.UpdateXrayRequest{}, "INVALID_ARGUMENT", "Version cannot be empty"},
		{"a slash in the version", &pb.UpdateXrayRequest{Version: "26/1"}, "INVALID_ARGUMENT", "Invalid version format: 26/1"},
		{"a hub URL without a checksum", &pb.UpdateXrayRequest{Version: "26.11.1", DownloadUrl: "http://hub"}, "MISSING_CHECKSUM", "requires a Checksum"},
	}
	n := newXrayNode(t, "26.10.10")
	old := n.installed(t)
	for _, c := range cases {
		resp, err := n.s.UpdateXrayBinary(context.Background(), c.req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if resp.Success || resp.ErrorCode != c.code || !strings.Contains(resp.Message, c.msg) {
			t.Fatalf("%s: response = %+v", c.name, resp)
		}
	}
	if !bytes.Equal(n.installed(t), old) {
		t.Fatal("a refused request touched the installed build")
	}
}
