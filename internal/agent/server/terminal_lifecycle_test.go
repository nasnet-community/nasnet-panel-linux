package server

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type lifecycleTerminalServer struct {
	pb.UnimplementedNodeAgentServer
	server Server
	done   chan struct{}
}

func (s *lifecycleTerminalServer) OpenTerminal(stream pb.NodeAgent_OpenTerminalServer) error {
	defer close(s.done)
	return s.server.OpenTerminal(stream)
}

func openDirectTerminalFixture(t *testing.T, shell string) (pb.NodeAgent_OpenTerminalClient, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	t.Setenv("SHELL", shell)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	handler := &lifecycleTerminalServer{done: make(chan struct{})}
	pb.RegisterNodeAgentServer(server, handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///terminal-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	stream, err := pb.NewNodeAgentClient(conn).OpenTerminal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return stream, cancel, handler.done
}

func directTerminalFixture(t *testing.T) (pb.NodeAgent_OpenTerminalClient, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	stream, cancel, done := openDirectTerminalFixture(t, "/bin/sh")
	// Wait for a real interactive shell prompt before exercising its lifecycle.
	for {
		out, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if len(out.GetData()) > 0 {
			break
		}
	}
	return stream, cancel, done
}

func TestTerminalAcknowledgesSilentShellBeforeInput(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "silent-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nread line\nprintf 'silent-shell:%s\\n' \"$line\"\nread line\n"), 0700); err != nil {
		t.Fatal(err)
	}
	stream, cancel, done := openDirectTerminalFixture(t, shell)
	out, err := stream.Recv()
	if err != nil {
		t.Fatalf("silent shell did not acknowledge startup: %v", err)
	}
	ack, ok := out.Payload.(*pb.TerminalOutput_Data)
	if !ok || len(ack.Data) != 0 {
		t.Fatalf("expected empty startup acknowledgment, got %v", out)
	}
	if err := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("probe\n")}}); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for !strings.Contains(output.String(), "silent-shell:probe\r\n") {
		out, err := stream.Recv()
		if err != nil {
			t.Fatalf("silent shell did not accept input: %v, output=%q", err, output.String())
		}
		output.Write(out.GetData())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("silent shell did not stop after cancellation")
	}
}

func TestTerminalPreservesFinalOutputAndExitCode(t *testing.T) {
	stream, _, done := directTerminalFixture(t)
	if err := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("printf 'terminal-final-output\\n'; exit 7\n")}}); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	var sawExit bool
	for {
		out, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch p := out.Payload.(type) {
		case *pb.TerminalOutput_Data:
			if sawExit {
				t.Fatal("output followed exit status")
			}
			output.Write(p.Data)
		case *pb.TerminalOutput_ExitCode:
			if sawExit || p.ExitCode != 7 {
				t.Fatalf("exit status = %d, duplicate = %v", p.ExitCode, sawExit)
			}
			sawExit = true
		}
	}
	if !sawExit || !strings.Contains(output.String(), "terminal-final-output\r\n") {
		t.Fatalf("missing output/status: exit=%v output=%q", sawExit, output.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not finish after shell exit")
	}
}

func TestTerminalClosesOnExplicitCloseAndCancellation(t *testing.T) {
	for _, mode := range []string{"close", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			stream, cancel, done := directTerminalFixture(t)
			if mode == "close" {
				if err := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Close{Close: true}}); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("server kept the shell alive after session closure")
			}
		})
	}
}
