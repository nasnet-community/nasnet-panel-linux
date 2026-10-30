package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// pushAgent is an agent that only knows PushXrayBinary. It notes what the call
// carried; read it after done closes.
type pushAgent struct {
	pb.UnimplementedNodeAgentServer
	done     chan struct{}
	header   *pb.XrayBinaryHeader
	sizes    []int
	body     bytes.Buffer
	deadline time.Time
	endErr   error
	// early, when set, runs once the header is in; an error ends the call.
	early func() error
	// reply is the answer once the data has ended.
	reply func() (*pb.CommandResponse, error)
}

func newPushAgent() *pushAgent {
	return &pushAgent{
		done:  make(chan struct{}),
		reply: func() (*pb.CommandResponse, error) { return &pb.CommandResponse{Success: true}, nil },
	}
}

func (a *pushAgent) PushXrayBinary(stream pb.NodeAgent_PushXrayBinaryServer) error {
	defer close(a.done)
	a.deadline, _ = stream.Context().Deadline()
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			a.endErr = err
			return err
		}
		if h := m.GetHeader(); h != nil {
			a.header = h
			if a.early != nil {
				if err := a.early(); err != nil {
					return err
				}
			}
			continue
		}
		a.sizes = append(a.sizes, len(m.GetData()))
		a.body.Write(m.GetData())
	}
	resp, err := a.reply()
	if err != nil {
		return err
	}
	return stream.SendAndClose(resp)
}

func (a *pushAgent) wait(t *testing.T) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the agent's handler never returned")
	}
}

// clientFor serves srv in process and dials it the way NewClient does, minus TLS.
func clientFor(t *testing.T, srv pb.NodeAgentServer) *Client {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	pb.RegisterNodeAgentServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///push-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100*1024*1024), grpc.MaxCallSendMsgSize(100*1024*1024)),
		grpc.WithUnaryInterceptor(DeadlineUnaryInterceptor()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Client{conn: conn, client: pb.NewNodeAgentClient(conn), target: "push-test"}
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i>>8)
	}
	return b
}

func TestPushXrayBinaryCutsTheDataIntoChunksOfAtMostOneMiB(t *testing.T) {
	const mib = XrayPushChunkSize
	sizes := []int{0, 1, mib - 1, mib, mib + 1, 2 * mib, 3*mib + 1234}
	wraps := map[string]func(io.Reader) io.Reader{
		"as it comes":       func(r io.Reader) io.Reader { return r },
		"a byte at a time":  iotest.OneByteReader,
		"data with the EOF": iotest.DataErrReader,
	}
	for wrapName, wrap := range wraps {
		for _, gzipped := range []bool{false, true} {
			for _, size := range sizes {
				name := fmt.Sprintf("%s, %d bytes, gzip=%v", wrapName, size, gzipped)
				if wrapName == "a byte at a time" && size > mib+1 {
					continue // the same code path again, only slower
				}
				a := newPushAgent()
				body := payload(size)

				err := clientFor(t, a).PushXrayBinary(context.Background(), "26.11.1", "abc123", wrap(bytes.NewReader(body)), true, gzipped)

				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				a.wait(t)
				want := &pb.XrayBinaryHeader{Version: "26.11.1", Sha256: "abc123", RestartAfter: true, Gzip: gzipped}
				if a.header.GetVersion() != want.Version || a.header.GetSha256() != want.Sha256 ||
					a.header.GetRestartAfter() != want.RestartAfter || a.header.GetGzip() != want.Gzip {
					t.Fatalf("%s: header = %v", name, a.header)
				}
				if !bytes.Equal(a.body.Bytes(), body) {
					t.Fatalf("%s: the agent got different bytes", name)
				}
				for i, n := range a.sizes {
					last := i == len(a.sizes)-1
					if n > mib || n == 0 || (!last && n != mib) {
						t.Fatalf("%s: chunk %d is %d bytes (all of them: %v)", name, i, n, a.sizes)
					}
				}
				if want := (size + mib - 1) / mib; len(a.sizes) != want {
					t.Fatalf("%s: %d chunks, want %d", name, len(a.sizes), want)
				}
			}
		}
	}
}

func TestPushXrayBinaryGivesTheCallFiveMinutes(t *testing.T) {
	a := newPushAgent()
	before := time.Now()

	if err := clientFor(t, a).PushXrayBinary(context.Background(), "26.11.1", "abc", bytes.NewReader(payload(10)), false, false); err != nil {
		t.Fatal(err)
	}

	a.wait(t)
	if got := a.deadline.Sub(before); got > 5*time.Minute+5*time.Second || got < 5*time.Minute-5*time.Second {
		t.Fatalf("the agent saw a deadline %v out", got)
	}
}

func TestPushXrayBinaryKeepsTheCallersOwnDeadline(t *testing.T) {
	a := newPushAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	before := time.Now()

	if err := clientFor(t, a).PushXrayBinary(ctx, "26.11.1", "abc", bytes.NewReader(payload(10)), false, false); err != nil {
		t.Fatal(err)
	}

	a.wait(t)
	if got := a.deadline.Sub(before); got > 45*time.Second || got < 35*time.Second {
		t.Fatalf("the agent saw a deadline %v out", got)
	}
}

func TestPushXrayBinaryKeepsTheAgentsStatusCode(t *testing.T) {
	for _, code := range []codes.Code{codes.InvalidArgument, codes.ResourceExhausted, codes.FailedPrecondition} {
		a := newPushAgent()
		a.reply = func() (*pb.CommandResponse, error) { return nil, status.Error(code, "no thanks") }

		err := clientFor(t, a).PushXrayBinary(context.Background(), "26.11.1", "abc", bytes.NewReader(payload(10)), false, false)

		if status.Code(err) != code || IsUnimplemented(err) {
			t.Fatalf("error = %v, want code %v", err, code)
		}
		var st interface{ GRPCStatus() *status.Status }
		if !errors.As(err, &st) || st.GRPCStatus().Message() != "no thanks" {
			t.Fatalf("the agent's message is lost: %v", err)
		}
	}
}

func TestPushXrayBinaryReportsAnAnswerThatSaysNo(t *testing.T) {
	a := newPushAgent()
	a.reply = func() (*pb.CommandResponse, error) {
		return &pb.CommandResponse{Success: false, Message: "Xray restart failed after update: boom (rolled back)", ErrorCode: "RESTART_FAILED"}, nil
	}

	err := clientFor(t, a).PushXrayBinary(context.Background(), "26.11.1", "abc", bytes.NewReader(payload(10)), true, false)

	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("rolled back")) {
		t.Fatalf("error = %v", err)
	}
	if IsUnimplemented(err) {
		t.Fatal("a refusal read as an old agent")
	}
}

func TestPushXrayBinaryToAnAgentThatPredatesIt(t *testing.T) {
	// Big enough that the client is still sending when the answer comes.
	body := payload(8 * XrayPushChunkSize)

	err := clientFor(t, pb.UnimplementedNodeAgentServer{}).PushXrayBinary(context.Background(), "26.11.1", "abc", bytes.NewReader(body), true, false)

	if !IsUnimplemented(err) {
		t.Fatalf("error = %v, want Unimplemented", err)
	}
}

func TestPushXrayBinaryStopsReadingOnceTheAgentRefuses(t *testing.T) {
	a := newPushAgent()
	a.early = func() error { return status.Error(codes.ResourceExhausted, "no room for it") }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	src := &endless{}

	err := clientFor(t, a).PushXrayBinary(ctx, "26.11.1", "abc", src, true, false)

	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("error = %v", err)
	}
	if got := src.read.Load(); got > 64*XrayPushChunkSize {
		t.Fatalf("read %d bytes of a build the agent had already refused", got)
	}
}

func TestPushXrayBinaryStopsWhenTheContextIsCancelled(t *testing.T) {
	a := newPushAgent()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &cancelAfter{reads: 3, cancel: cancel}

	err := clientFor(t, a).PushXrayBinary(ctx, "26.11.1", "abc", src, true, false)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	a.wait(t)
	if status.Code(a.endErr) != codes.Canceled {
		t.Fatalf("the agent saw the call end with %v, want Canceled", a.endErr)
	}
}

func TestPushXrayBinaryStopsWhenTheDeadlineRunsOut(t *testing.T) {
	a := newPushAgent()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := clientFor(t, a).PushXrayBinary(ctx, "26.11.1", "abc", &endless{slow: true}, true, false)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	a.wait(t)
}

func TestPushXrayBinaryGivesUpWhenTheBuildCantBeRead(t *testing.T) {
	a := newPushAgent()
	disk := errors.New("disk gone")
	src := io.MultiReader(bytes.NewReader(payload(XrayPushChunkSize+10)), iotest.ErrReader(disk))

	err := clientFor(t, a).PushXrayBinary(context.Background(), "26.11.1", "abc", src, true, false)

	if !errors.Is(err, disk) {
		t.Fatalf("error = %v, want the read error", err)
	}
	a.wait(t)
	if a.endErr == nil {
		t.Fatal("the agent was left to think the build was complete")
	}
}

func TestReverseClientCannotPushAnXrayBuild(t *testing.T) {
	rc := NewReverseClient("node-1", nil, nil, nil)

	err := rc.PushXrayBinary(context.Background(), "26.11.1", "abc", bytes.NewReader(payload(10)), true, false)

	if !IsUnimplemented(err) {
		t.Fatalf("error = %v, want Unimplemented", err)
	}
}

func TestIsUnimplemented(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("unimplemented"), false},
		{status.Error(codes.NotFound, "x"), false},
		{status.Error(codes.Unimplemented, "x"), true},
		{fmt.Errorf("push xray binary: %w", status.Error(codes.Unimplemented, "x")), true},
	}
	for _, c := range cases {
		if got := IsUnimplemented(c.err); got != c.want {
			t.Errorf("IsUnimplemented(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// endless is a reader with no end that counts what was taken from it.
type endless struct {
	read atomic.Int64
	slow bool
}

func (e *endless) Read(p []byte) (int, error) {
	if e.slow {
		time.Sleep(20 * time.Millisecond)
		p = p[:min(len(p), 1000)]
	}
	e.read.Add(int64(len(p)))
	return len(p), nil
}

// cancelAfter cancels its context on the nth read and carries on regardless.
type cancelAfter struct {
	reads  int
	cancel context.CancelFunc
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	if c.reads--; c.reads == 0 {
		c.cancel()
	}
	return len(p), nil
}
