package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

type terminalResult struct {
	output *pb.TerminalOutput
	err    error
}

type terminalTestStream struct {
	pb.NodeAgent_OpenTerminalClient
	ctx       context.Context
	outputs   chan terminalResult
	inputs    chan *pb.TerminalInput
	closeSent atomic.Int32
}

func (s *terminalTestStream) Recv() (*pb.TerminalOutput, error) {
	select {
	case result := <-s.outputs:
		return result.output, result.err
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *terminalTestStream) Send(in *pb.TerminalInput) error {
	select {
	case s.inputs <- in:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *terminalTestStream) CloseSend() error {
	s.closeSent.Add(1)
	return nil
}

type terminalTestUsecase struct {
	usecase.NodeUsecase
	stream  *terminalTestStream
	opened  chan struct{}
	cleaned atomic.Int32
}

func (u *terminalTestUsecase) OpenTerminal(ctx context.Context, _ uint) (pb.NodeAgent_OpenTerminalClient, func(), error) {
	u.stream.ctx = ctx
	close(u.opened)
	return u.stream, func() { u.cleaned.Add(1) }, nil
}

func terminalWebSocketFixture(t *testing.T) (*websocket.Conn, *terminalTestUsecase, <-chan struct{}) {
	t.Helper()
	uc := &terminalTestUsecase{
		stream: &terminalTestStream{outputs: make(chan terminalResult, 64), inputs: make(chan *pb.TerminalInput, 8)},
		opened: make(chan struct{}),
	}
	h := NewHandler(uc)
	done := make(chan struct{})
	router := gin.New()
	router.GET("/nodes/:id/terminal", func(c *gin.Context) {
		defer close(done)
		h.TerminalWebSocket(c)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/nodes/6/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	select {
	case <-uc.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not open")
	}
	return conn, uc, done
}

func readTerminalFrame(t *testing.T, conn *websocket.Conn, wantType int, want string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	kind, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != wantType {
		t.Fatalf("frame type = %d, want %d", kind, wantType)
	}
	if wantType == websocket.TextMessage {
		var gotJSON, wantJSON any
		if err := json.Unmarshal(data, &gotJSON); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(gotJSON)
		expected, _ := json.Marshal(wantJSON)
		if string(got) != string(expected) {
			t.Fatalf("control = %s, want %s", got, expected)
		}
	} else if string(data) != want {
		t.Fatalf("output = %q, want %q", data, want)
	}
}

func assertTerminalStopped(t *testing.T, uc *terminalTestUsecase, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("terminal handler leaked after a pump completed")
	}
	if uc.stream.ctx.Err() == nil || uc.cleaned.Load() != 1 || uc.stream.closeSent.Load() != 1 {
		t.Fatalf("incomplete cleanup: context=%v cleanup=%d CloseSend=%d", uc.stream.ctx.Err(), uc.cleaned.Load(), uc.stream.closeSent.Load())
	}
}

func TestTerminalWebSocketReadyFollowsAgentOutputAndPingPong(t *testing.T) {
	conn, uc, done := terminalWebSocketFixture(t)
	if err := conn.WriteJSON(map[string]bool{"ping": true}); err != nil {
		t.Fatal(err)
	}
	// A pong can arrive while the shell is still starting, with no ready yet.
	readTerminalFrame(t, conn, websocket.TextMessage, `{"pong":true}`)
	uc.stream.outputs <- terminalResult{output: &pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("$ ")}}}
	readTerminalFrame(t, conn, websocket.TextMessage, `{"ready":true}`)
	readTerminalFrame(t, conn, websocket.BinaryMessage, "$ ")
	uc.stream.outputs <- terminalResult{output: &pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("second output")}}}
	readTerminalFrame(t, conn, websocket.BinaryMessage, "second output")
	_ = conn.Close()
	assertTerminalStopped(t, uc, done)
}

func TestTerminalWebSocketForwardsBinaryResizeAndCloses(t *testing.T) {
	conn, uc, done := terminalWebSocketFixture(t)
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(TerminalMessage{Resize: &TerminalResize{Cols: 132, Rows: 40}}); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		select {
		case in := <-uc.stream.inputs:
			if i == 0 && string(in.GetData()) != "ls\r" {
				t.Fatalf("input = %q", in.GetData())
			}
			if i == 1 && (in.GetResize().GetCols() != 132 || in.GetResize().GetRows() != 40) {
				t.Fatalf("resize = %v", in.GetResize())
			}
		case <-time.After(3 * time.Second):
			t.Fatal("input was not forwarded")
		}
	}
	if err := conn.WriteJSON(TerminalMessage{Close: true}); err != nil {
		t.Fatal(err)
	}
	assertTerminalStopped(t, uc, done)
}

func TestTerminalWebSocketEmptyStartupAcknowledgesReady(t *testing.T) {
	conn, uc, done := terminalWebSocketFixture(t)
	uc.stream.outputs <- terminalResult{output: &pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte{}}}}
	readTerminalFrame(t, conn, websocket.TextMessage, `{"ready":true}`)
	readTerminalFrame(t, conn, websocket.BinaryMessage, "")
	// Readiness must precede the first user input even if no prompt is printed.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("probe\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case in := <-uc.stream.inputs:
		if string(in.GetData()) != "probe\r" {
			t.Fatalf("input = %q", in.GetData())
		}
	case <-time.After(time.Second):
		t.Fatal("silent shell input was not forwarded")
	}
	_ = conn.Close()
	assertTerminalStopped(t, uc, done)
}

func TestTerminalWebSocketCompletesOnExitErrorAndEOF(t *testing.T) {
	cases := []struct {
		name   string
		result terminalResult
		want   string
	}{
		{"exit", terminalResult{output: &pb.TerminalOutput{Payload: &pb.TerminalOutput_ExitCode{ExitCode: 7}}}, `{"exit_code":7}`},
		{"error", terminalResult{output: &pb.TerminalOutput{Payload: &pb.TerminalOutput_Error{Error: "PTY unavailable"}}}, `{"error":"PTY unavailable"}`},
		{"eof", terminalResult{err: io.EOF}, `{"error":"Terminal stream ended without an exit status."}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			conn, uc, done := terminalWebSocketFixture(t)
			uc.stream.outputs <- tt.result
			readTerminalFrame(t, conn, websocket.TextMessage, tt.want)
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _, err := conn.ReadMessage()
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				t.Fatalf("WebSocket did not close normally: %v", err)
			}
			assertTerminalStopped(t, uc, done)
		})
	}
}

func TestTerminalWebSocketSerializesPongWithConcurrentOutput(t *testing.T) {
	conn, uc, done := terminalWebSocketFixture(t)
	const count = 32
	written := make(chan error, 1)
	go func() {
		for range count {
			if err := conn.WriteJSON(TerminalMessage{Ping: true}); err != nil {
				written <- err
				return
			}
		}
		written <- nil
	}()
	for range count {
		uc.stream.outputs <- terminalResult{output: &pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("output")}}}
	}
	var ready, pongs, outputs int
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for range count*2 + 1 {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.BinaryMessage {
			if ready != 1 || string(data) != "output" {
				t.Fatalf("invalid output ordering: ready=%d, output=%q", ready, data)
			}
			outputs++
			continue
		}
		var control struct {
			Ready bool `json:"ready"`
			Pong  bool `json:"pong"`
		}
		if err := json.Unmarshal(data, &control); err != nil {
			t.Fatal(err)
		}
		if control.Ready {
			ready++
		}
		if control.Pong {
			pongs++
		}
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if ready != 1 || pongs != count || outputs != count {
		t.Fatalf("lost or duplicate frames: ready=%d, pongs=%d, outputs=%d", ready, pongs, outputs)
	}
	_ = conn.Close()
	assertTerminalStopped(t, uc, done)
}
