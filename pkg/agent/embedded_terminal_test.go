package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func TestEmbeddedTerminalReadinessExitAndIndependentSessions(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := NewEmbeddedClient(&server.Server{})
	firstCtx, stopFirst := context.WithCancel(ctx)
	first, err := client.OpenTerminal(firstCtx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.OpenTerminal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []pb.NodeAgent_OpenTerminalClient{first, second} {
		ready, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ready.Payload.(*pb.TerminalOutput_Data); !ok {
			t.Fatalf("expected startup data, got %T", ready.Payload)
		}
	}
	stopFirst()
	for {
		_, err := first.Recv()
		if err != nil {
			break
		}
	}
	// Drain the second PTY concurrently: the embedded pipe is unbuffered.
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- second.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("printf 'NASNET_%s\\n' OK; exit 7\n")}})
	}()
	var output strings.Builder
	for {
		frame, err := second.Recv()
		if err != nil {
			t.Fatalf("missing exit status: %v, output %q", err, output.String())
		}
		switch p := frame.Payload.(type) {
		case *pb.TerminalOutput_Data:
			output.Write(p.Data)
		case *pb.TerminalOutput_ExitCode:
			if p.ExitCode != 7 {
				t.Fatalf("exit = %d", p.ExitCode)
			}
			if !strings.Contains(output.String(), "NASNET_OK") {
				t.Fatalf("missing final output: %q", output.String())
			}
			if err := <-sendDone; err != nil {
				t.Fatal(err)
			}
			if _, err := second.Recv(); !errors.Is(err, io.EOF) {
				t.Fatalf("after exit: %v", err)
			}
			return
		}
	}
}

func TestEmbeddedTerminalCloseSendStopsSilentShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := NewEmbeddedClient(&server.Server{}).OpenTerminal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("close did not finish cleanly: %v", err)
		}
	}
}

func TestBidiHandlerReturnReleasesServerPumps(t *testing.T) {
	pipe := newBidiStreamPipe[pb.TerminalInput, pb.TerminalOutput](context.Background())
	srv := bidiServerStream[pb.TerminalInput, pb.TerminalOutput]{p: pipe}
	done := make(chan error, 2)
	go func() { done <- srv.Send(&pb.TerminalOutput{}) }()
	go func() { _, err := srv.Recv(); done <- err }()
	pipe.finish(nil)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, io.EOF) {
				t.Fatalf("pump error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("handler return left a blocked pump")
		}
	}
}
