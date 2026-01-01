package agent

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// runWithTimeout fails the test if fn does not return within d (catches the
// channel-deadlock failure mode these pipes are most at risk of).
func runWithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("timed out — likely a stream-pipe deadlock")
	}
}

func TestServerStreamPipe_SendThenEOF(t *testing.T) {
	runWithTimeout(t, 2*time.Second, func() {
		pipe := newServerStreamPipe[pb.LogEntry](context.Background())
		go func() {
			// Fake server handler: emit 3 frames then return OK.
			for i := 0; i < 3; i++ {
				if err := pipe.Send(&pb.LogEntry{Message: "line"}); err != nil {
					return
				}
			}
			pipe.finish(nil)
		}()

		got := 0
		for {
			_, err := pipe.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("unexpected Recv error: %v", err)
				return
			}
			got++
		}
		if got != 3 {
			t.Errorf("got %d frames, want 3", got)
		}
	})
}

func TestServerStreamPipe_HandlerError(t *testing.T) {
	runWithTimeout(t, 2*time.Second, func() {
		pipe := newServerStreamPipe[pb.LogEntry](context.Background())
		sentinel := errors.New("boom")
		go func() { pipe.finish(sentinel) }()

		if _, err := pipe.Recv(); !errors.Is(err, sentinel) {
			t.Errorf("Recv err = %v, want %v", err, sentinel)
		}
	})
}

func TestServerStreamPipe_ContextCancel(t *testing.T) {
	runWithTimeout(t, 2*time.Second, func() {
		ctx, cancel := context.WithCancel(context.Background())
		pipe := newServerStreamPipe[pb.LogEntry](ctx)
		cancel()
		if _, err := pipe.Recv(); !errors.Is(err, context.Canceled) {
			t.Errorf("Recv err = %v, want context.Canceled", err)
		}
		// Handler Send should also observe cancellation rather than block.
		if err := pipe.Send(&pb.LogEntry{}); !errors.Is(err, context.Canceled) {
			t.Errorf("Send err = %v, want context.Canceled", err)
		}
	})
}

func TestBidiStreamPipe_EchoAndClose(t *testing.T) {
	runWithTimeout(t, 2*time.Second, func() {
		ctx := context.Background()
		pipe := newBidiStreamPipe[pb.TerminalInput, pb.TerminalOutput](ctx)
		srv := bidiServerStream[pb.TerminalInput, pb.TerminalOutput]{p: pipe}
		cli := bidiClientStream[pb.TerminalInput, pb.TerminalOutput]{p: pipe}

		// Fake bidi handler: echo each input's data back as output until the
		// client closes its send side (Recv returns io.EOF), then return OK.
		go func() {
			for {
				in, err := srv.Recv()
				if err == io.EOF {
					pipe.finish(nil)
					return
				}
				if err != nil {
					pipe.finish(err)
					return
				}
				data := in.GetData()
				if sendErr := srv.Send(&pb.TerminalOutput{
					Payload: &pb.TerminalOutput_Data{Data: data},
				}); sendErr != nil {
					pipe.finish(sendErr)
					return
				}
			}
		}()

		for i := 0; i < 3; i++ {
			if err := cli.Send(&pb.TerminalInput{
				Payload: &pb.TerminalInput_Data{Data: []byte("hi")},
			}); err != nil {
				t.Errorf("client Send: %v", err)
				return
			}
			out, err := cli.Recv()
			if err != nil {
				t.Errorf("client Recv: %v", err)
				return
			}
			if got := string(out.GetData()); got != "hi" {
				t.Errorf("echo = %q, want %q", got, "hi")
			}
		}

		if err := cli.CloseSend(); err != nil {
			t.Errorf("CloseSend: %v", err)
		}
		// After the handler returns OK, the next Recv is io.EOF.
		if _, err := cli.Recv(); err != io.EOF {
			t.Errorf("final Recv = %v, want io.EOF", err)
		}
	})
}
