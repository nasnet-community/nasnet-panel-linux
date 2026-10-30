package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"math/rand"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const mib = 1 << 20

// bigBuild is a fake xray that reports version v, padded after its last
// command to size bytes so it spans several chunks. The padding is base64 of
// random bytes: it packs to about three quarters, so gzip chunks stay big too.
func bigBuild(t *testing.T, v string, size int) []byte {
	t.Helper()
	b := append(build(t, v), "exit 0\n"...)
	rnd := rand.New(rand.NewSource(7))
	line := make([]byte, 57)
	for len(b) < size {
		rnd.Read(line)
		b = append(b, "# "+base64.StdEncoding.EncodeToString(line)+"\n"...)
	}
	return b[:size]
}

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// zeros is a gzip of n zero bytes, built without holding them all in memory.
func zeros(t *testing.T, n int) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	block := make([]byte, mib)
	for left := n; left > 0; left -= len(block) {
		if _, err := w.Write(block[:min(left, len(block))]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func chunksOf(b []byte, n int) [][]byte {
	var out [][]byte
	for len(b) > n {
		out = append(out, b[:n])
		b = b[n:]
	}
	return append(out, b)
}

func header(version, sha string, restart, gzipped bool) *pb.XrayBinaryChunk {
	return &pb.XrayBinaryChunk{Payload: &pb.XrayBinaryChunk_Header{Header: &pb.XrayBinaryHeader{
		Version: version, Sha256: sha, RestartAfter: restart, Gzip: gzipped,
	}}}
}

func data(b []byte) *pb.XrayBinaryChunk {
	return &pb.XrayBinaryChunk{Payload: &pb.XrayBinaryChunk_Data{Data: b}}
}

// push is a whole push: the header, then the data in chunks of the given size.
func push(t *testing.T, c pb.NodeAgentClient, hdr *pb.XrayBinaryChunk, body []byte, chunk int) (*pb.CommandResponse, error) {
	t.Helper()
	msgs := []*pb.XrayBinaryChunk{hdr}
	if len(body) > 0 {
		for _, part := range chunksOf(body, chunk) {
			msgs = append(msgs, data(part))
		}
	}
	return pushMessages(t, c, msgs...)
}

// pushMessages sends messages as they are and returns what the call answered.
func pushMessages(t *testing.T, c pb.NodeAgentClient, msgs ...*pb.XrayBinaryChunk) (*pb.CommandResponse, error) {
	t.Helper()
	return pushRaw(c, msgs...)
}

// pushRaw is pushMessages for a goroutine that can't fail the test itself.
func pushRaw(c pb.NodeAgentClient, msgs ...*pb.XrayBinaryChunk) (*pb.CommandResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := c.PushXrayBinary(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if err := stream.Send(m); err != nil {
			if err == io.EOF { // the server has answered already; CloseAndRecv has it
				break
			}
			return nil, err
		}
	}
	return stream.CloseAndRecv()
}

// pushClient serves the node's agent over an in-process connection.
func (n *xrayNode) pushClient(t *testing.T) pb.NodeAgentClient {
	t.Helper()
	lis := bufconn.Listen(mib)
	gs := grpc.NewServer(grpc.MaxRecvMsgSize(100 * mib))
	pb.RegisterNodeAgentServer(gs, n.s)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///push-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewNodeAgentClient(conn)
}

// untouched checks a refused push left xray and the xray directory as they were.
func (n *xrayNode) untouched(t *testing.T, old []byte, pid int) {
	t.Helper()
	if !bytes.Equal(n.installed(t), old) {
		t.Fatal("the installed build changed")
	}
	if got := n.s.xrayMgr.PID(); got != pid || !n.s.xrayMgr.IsRunning() {
		t.Fatalf("xray was disturbed: pid %d (was %d), running %v", got, pid, n.s.xrayMgr.IsRunning())
	}
	if got := n.files(t); !slices.Equal(got, []string{"node-xray"}) {
		t.Fatalf("files in the xray directory: %v", got)
	}
}

func wantCode(t *testing.T, err error, code codes.Code, msg string) {
	t.Helper()
	if status.Code(err) != code || !strings.Contains(err.Error(), msg) {
		t.Fatalf("error = %v, want %v mentioning %q", err, code, msg)
	}
}

func TestPushInstallsABuildSentInChunks(t *testing.T) {
	cases := []struct {
		name  string
		size  int
		gzip  bool
		chunk int
	}{
		{"plain, full chunks and a remainder", 4*mib + mib/2 + 7, false, mib},
		{"plain, an exact multiple of the chunk", 2 * mib, false, mib},
		{"plain, a build under one chunk", 100 << 10, false, mib},
		{"plain, small chunks", 3*mib + 5, false, 4099},
		{"gzip, full chunks and a remainder", 4*mib + mib/2 + 7, true, mib},
		{"gzip, small chunks", 3*mib + 5, true, 4099},
	}
	for _, c := range cases {
		n := newXrayNode(t, "26.10.10")
		n.start(t)
		oldPID := n.s.xrayMgr.PID()
		next := bigBuild(t, "26.11.1", c.size)
		body := next
		if c.gzip {
			body = gz(t, next)
		}

		resp, err := push(t, n.pushClient(t), header("26.11.1", sum(next), false, c.gzip), body, c.chunk)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !resp.Success || !strings.Contains(resp.Message, "26.11.1") {
			t.Fatalf("%s: response = %+v", c.name, resp)
		}
		if !bytes.Equal(n.installed(t), next) {
			t.Fatalf("%s: the installed build is not what was pushed", c.name)
		}
		if !n.s.xrayMgr.IsRunning() || n.s.xrayMgr.PID() == oldPID {
			t.Fatalf("%s: xray was not restarted", c.name)
		}
		if got := n.files(t); !slices.Equal(got, []string{"node-xray"}) {
			t.Fatalf("%s: files left behind: %v", c.name, got)
		}
	}
}

func TestPushInstallsOnANodeThatHasNoXrayYet(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	if err := os.Remove(n.bin); err != nil {
		t.Fatal(err)
	}
	next := build(t, "26.11.1")

	resp, err := push(t, n.pushClient(t), header("26.11.1", sum(next), true, false), next, mib)

	if err != nil || !resp.Success {
		t.Fatalf("push: %+v, %v", resp, err)
	}
	if info, err := os.Stat(n.bin); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("the installed build: %v, %v", info, err)
	}
	if !bytes.Equal(n.installed(t), next) || !n.s.xrayMgr.IsRunning() {
		t.Fatal("the build is not installed and running")
	}
	if got := n.files(t); !slices.Equal(got, []string{"node-xray"}) {
		t.Fatalf("files in the xray directory: %v", got)
	}
}

func TestPushesAtTheSameTimeTakeTurns(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	c := n.pushClient(t)
	versions := []string{"26.11.1", "26.11.2", "26.11.3"}
	builds := map[string][]byte{}
	type result struct {
		resp *pb.CommandResponse
		err  error
	}
	results := make(chan result, len(versions))
	for i, v := range versions {
		next := bigBuild(t, v, (i+1)*mib+i)
		builds[v] = next
		go func() {
			msgs := []*pb.XrayBinaryChunk{header(v, sum(next), true, false)}
			for _, part := range chunksOf(next, mib) {
				msgs = append(msgs, data(part))
			}
			resp, err := pushRaw(c, msgs...)
			results <- result{resp, err}
		}()
	}

	for range versions {
		if r := <-results; r.err != nil || !r.resp.Success {
			t.Fatalf("push: %+v, %v", r.resp, r.err)
		}
	}

	// Whichever went last is what is installed, whole, and nothing else is left.
	got := n.installed(t)
	if !bytes.Equal(got, builds["26.11.1"]) && !bytes.Equal(got, builds["26.11.2"]) && !bytes.Equal(got, builds["26.11.3"]) {
		t.Fatal("the installed build is none of the three that were pushed")
	}
	if !n.s.xrayMgr.IsRunning() {
		t.Fatal("xray is not running")
	}
	if names := n.files(t); !slices.Equal(names, []string{"node-xray"}) {
		t.Fatalf("files in the xray directory: %v", names)
	}
}

func TestPushTakesAnUppercaseChecksum(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	next := build(t, "26.11.1")

	resp, err := push(t, n.pushClient(t), header("26.11.1", strings.ToUpper(sum(next)), false, false), next, mib)
	if err != nil || !resp.Success {
		t.Fatalf("push: %+v, %v", resp, err)
	}
}

func TestPushKeepsTheInstalledBuildOnAChecksumMismatch(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()
	next := build(t, "26.11.1")

	_, err := push(t, n.pushClient(t), header("26.11.1", sum([]byte("something else")), true, false), next, mib)

	wantCode(t, err, codes.InvalidArgument, "checksum mismatch")
	n.untouched(t, old, pid)
}

func TestPushNeedsTheHeaderFirst(t *testing.T) {
	next := build(t, "26.11.1")
	cases := []struct {
		name string
		msgs []*pb.XrayBinaryChunk
		want string
	}{
		{"data first", []*pb.XrayBinaryChunk{data(next)}, "first message must be the header"},
		{"nothing at all", nil, "ended before the header"},
		{"a second header", []*pb.XrayBinaryChunk{header("26.11.1", sum(next), true, false), header("26.11.1", sum(next), true, false)}, "must carry data"},
		{"an empty message", []*pb.XrayBinaryChunk{header("26.11.1", sum(next), true, false), {}}, "must carry data"},
	}
	for _, c := range cases {
		n := newXrayNode(t, "26.10.10")
		n.start(t)
		old, pid := n.installed(t), n.s.xrayMgr.PID()

		_, err := pushMessages(t, n.pushClient(t), c.msgs...)

		wantCode(t, err, codes.InvalidArgument, c.want)
		n.untouched(t, old, pid)
	}
}

func TestPushNeedsAUsableVersionAndChecksum(t *testing.T) {
	next := build(t, "26.11.1")
	cases := []struct {
		name string
		hdr  *pb.XrayBinaryChunk
		want string
	}{
		{"no checksum", header("26.11.1", "", true, false), "sha256 is required"},
		{"a checksum that is not hex", header("26.11.1", strings.Repeat("zz", 32), true, false), "64 hex characters"},
		{"a checksum that is too short", header("26.11.1", sum(next)[:62], true, false), "64 hex characters"},
		{"no version", header("", sum(next), true, false), "version cannot be empty"},
		{"a slash in the version", header("26/1", sum(next), true, false), "invalid version format"},
	}
	for _, c := range cases {
		n := newXrayNode(t, "26.10.10")
		n.start(t)
		old, pid := n.installed(t), n.s.xrayMgr.PID()

		_, err := push(t, n.pushClient(t), c.hdr, next, mib)

		wantCode(t, err, codes.InvalidArgument, c.want)
		n.untouched(t, old, pid)
	}
}

func TestPushRefusesAMessageOverOneMiB(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()
	next := bigBuild(t, "26.11.1", 2*mib)

	_, err := push(t, n.pushClient(t), header("26.11.1", sum(next), true, false), next, mib+1)

	wantCode(t, err, codes.InvalidArgument, "over")
	n.untouched(t, old, pid)
}

func TestPushRefusesABuildThatIsNotForThisHost(t *testing.T) {
	_, other := machines(t)
	cases := map[string][]byte{
		"another architecture": elfBytes(other, 256),
		"not an ELF":           bytes.Repeat([]byte("x"), 64),
		"far too small":        []byte("x"),
		"empty":                nil,
	}
	for name, next := range cases {
		n := newXrayNode(t, "26.10.10")
		n.s.xrayCheck = nil // the real check
		n.start(t)
		old, pid := n.installed(t), n.s.xrayMgr.PID()

		_, err := push(t, n.pushClient(t), header("26.11.1", sum(next), true, false), next, mib)

		if status.Code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), "refusing to install") {
			t.Fatalf("%s: error = %v", name, err)
		}
		// xray was never stopped for it, so it keeps its process.
		n.untouched(t, old, pid)
	}
}

func TestPushStopsAGzipThatUnpacksPastTheCap(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()
	bomb := zeros(t, maxXrayUnpacked+mib)
	if len(bomb) > mib {
		t.Fatalf("the bomb is %d bytes: not small enough to prove the point", len(bomb))
	}

	_, err := push(t, n.pushClient(t), header("26.11.1", sum([]byte("whatever")), true, true), bomb, mib)

	wantCode(t, err, codes.ResourceExhausted, "limit")
	n.untouched(t, old, pid)
}

func TestPushRollsBackWhenTheNewBuildWillNotStart(t *testing.T) {
	host, _ := machines(t)
	n := newXrayNode(t, "26.10.10")
	n.s.xrayCheck = nil
	n.start(t)
	old := n.installed(t)
	// A header the check accepts and the kernel doesn't.
	next := elfBytes(host, 256)

	resp, err := push(t, n.pushClient(t), header("26.11.1", sum(next), true, false), next, mib)

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

func TestPushRollsBackWhenTheNewBuildReportsNoVersion(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old := n.installed(t)
	next := build(t, "")

	resp, err := push(t, n.pushClient(t), header("26.11.1", sum(next), true, false), next, mib)

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

func TestPushOfTheBuildAlreadyRunningLeavesXrayAlone(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()

	resp, err := push(t, n.pushClient(t), header("26.10.10", sum(old), true, false), old, mib)

	if err != nil || !resp.Success || !strings.Contains(resp.Message, "already installed") {
		t.Fatalf("push: %+v, %v", resp, err)
	}
	n.untouched(t, old, pid)
}

func TestPushOfTheSameBytesUnderAnotherVersionIsInstalledAnyway(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()

	resp, err := push(t, n.pushClient(t), header("26.10.11", sum(old), true, false), old, mib)

	if err != nil || !resp.Success || strings.Contains(resp.Message, "already installed") {
		t.Fatalf("push: %+v, %v", resp, err)
	}
	if n.s.xrayMgr.PID() == pid {
		t.Fatal("xray was not restarted")
	}
}

func TestPushLeavesAStoppedXrayStoppedUnlessAskedToStartIt(t *testing.T) {
	for _, restart := range []bool{false, true} {
		n := newXrayNode(t, "26.10.10")
		next := build(t, "26.11.1")

		resp, err := push(t, n.pushClient(t), header("26.11.1", sum(next), restart, false), next, mib)

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

// fakePush is the server end of a push without a network: it hands out its
// messages, then ends the call with end (io.EOF for a clean one).
type fakePush struct {
	grpc.ServerStream
	ctx  context.Context
	msgs []*pb.XrayBinaryChunk
	end  error
	sent *pb.CommandResponse
}

func (f *fakePush) Context() context.Context { return f.ctx }

func (f *fakePush) Recv() (*pb.XrayBinaryChunk, error) {
	if len(f.msgs) == 0 {
		return nil, f.end
	}
	m := f.msgs[0]
	f.msgs = f.msgs[1:]
	return m, nil
}

func (f *fakePush) SendAndClose(r *pb.CommandResponse) error {
	f.sent = r
	return nil
}

func dataMessages(b []byte, chunk int) []*pb.XrayBinaryChunk {
	var msgs []*pb.XrayBinaryChunk
	for _, part := range chunksOf(b, chunk) {
		msgs = append(msgs, data(part))
	}
	return msgs
}

func TestPushForACallTheHubGaveUpOnInstallsNothing(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()
	next := build(t, "26.11.1")
	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakePush{
		ctx:  ctx,
		msgs: append([]*pb.XrayBinaryChunk{header("26.11.1", sum(next), true, false)}, dataMessages(next, mib)...),
		end:  io.EOF,
	}
	cancel()

	err := n.s.PushXrayBinary(stream)

	if status.Code(err) != codes.Canceled || stream.sent != nil {
		t.Fatalf("error = %v, answer sent = %v", err, stream.sent)
	}
	n.untouched(t, old, pid)
}

func TestPushForABrokenCallLeavesNoSpoolFile(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()
	next := bigBuild(t, "26.11.1", 2*mib)
	lost := status.Error(codes.Unavailable, "connection lost")
	stream := &fakePush{
		ctx:  context.Background(),
		msgs: append([]*pb.XrayBinaryChunk{header("26.11.1", sum(next), true, false)}, dataMessages(next[:mib+10], mib)...),
		end:  lost,
	}

	err := n.s.PushXrayBinary(stream)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("error = %v", err)
	}
	n.untouched(t, old, pid)
}

func TestSpoolStopsAtTheCapAndNotBefore(t *testing.T) {
	const limit = 1000
	exact := bytes.Repeat([]byte{1}, limit)
	over := bytes.Repeat([]byte{1}, limit+1)
	cases := []struct {
		name string
		gzip bool
		body []byte
		ok   bool
	}{
		{"plain at the cap", false, exact, true},
		{"plain over the cap", false, over, false},
		{"gzip at the cap", true, gz(t, exact), true},
		{"gzip over the cap", true, gz(t, over), false},
	}
	for _, c := range cases {
		dir := t.TempDir()
		stream := &fakePush{msgs: dataMessages(c.body, 100), end: io.EOF}

		name, size, got, err := spoolXrayPush(stream, c.gzip, dir, limit)

		entries := listNames(t, dir)
		if c.ok {
			if err != nil || size != limit || got != sum(exact) || len(entries) != 1 {
				t.Fatalf("%s: size %d, sum ok %v, err %v, files %v", c.name, size, got == sum(exact), err, entries)
			}
			continue
		}
		if status.Code(err) != codes.ResourceExhausted || name != "" || len(entries) != 0 {
			t.Fatalf("%s: err %v, name %q, files %v", c.name, err, name, entries)
		}
	}
}

func TestSpoolRemovesItsFileOnEveryError(t *testing.T) {
	next := bigBuild(t, "26.11.1", 64<<10)
	packed := gz(t, next)
	cases := []struct {
		name string
		gzip bool
		msgs []*pb.XrayBinaryChunk
		end  error
		code codes.Code
	}{
		{"the call breaks", false, dataMessages(next[:1000], 100), status.Error(codes.Unavailable, "gone"), codes.Unavailable},
		{"the call is cancelled", true, dataMessages(packed[:1000], 100), status.Error(codes.Canceled, "cancelled"), codes.Canceled},
		{"a header after the first", false, []*pb.XrayBinaryChunk{data(next[:10]), header("26.11.1", sum(next), false, false)}, io.EOF, codes.InvalidArgument},
		{"gzip cut short", true, dataMessages(packed[:len(packed)/2], 1000), io.EOF, codes.InvalidArgument},
		{"gzip that is not gzip", true, dataMessages(next, 1000), io.EOF, codes.InvalidArgument},
		{"gzip with nothing in it", true, nil, io.EOF, codes.InvalidArgument},
		{"gzip with a corrupt trailer", true, dataMessages(append(slices.Clone(packed[:len(packed)-8]), make([]byte, 8)...), 1000), io.EOF, codes.InvalidArgument},
		{"gzip followed by junk", true, dataMessages(append(slices.Clone(packed), []byte("junk after the stream")...), 1000), io.EOF, codes.InvalidArgument},
	}
	for _, c := range cases {
		dir := t.TempDir()

		name, _, _, err := spoolXrayPush(&fakePush{msgs: c.msgs, end: c.end}, c.gzip, dir, maxXrayUnpacked)

		entries := listNames(t, dir)
		if status.Code(err) != c.code || name != "" || len(entries) != 0 {
			t.Fatalf("%s: err %v, name %q, files %v", c.name, err, name, entries)
		}
	}
}

func TestSpoolStopsAGzipThatNeverEnds(t *testing.T) {
	// Empty gzip members unpack to nothing however many there are, so only a
	// cap on the input stops a stream of them.
	member := gz(t, nil)
	var msgs []*pb.XrayBinaryChunk
	for i := 0; i < 3*mib/len(member); i++ {
		msgs = append(msgs, data(member))
	}
	dir := t.TempDir()

	_, _, _, err := spoolXrayPush(&fakePush{msgs: msgs, end: io.EOF}, true, dir, 1000)

	wantCode(t, err, codes.ResourceExhausted, "limit")
	if entries := listNames(t, dir); len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}
}
