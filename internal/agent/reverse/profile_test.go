package reverse

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/middleware"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// stubAgent stands in for the agent's server and notes what reached it. The
// calls that would run a command or touch a file only answer.
type stubAgent struct {
	pb.UnimplementedNodeAgentServer
	mu    sync.Mutex
	calls []string
}

func (s *stubAgent) note(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, name)
}

func (s *stubAgent) reached() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *stubAgent) GetStatus(context.Context, *pb.Empty) (*pb.NodeStatus, error) {
	return &pb.NodeStatus{XrayRunning: true, XrayVersion: "stub"}, nil
}

func (s *stubAgent) ExecuteCommand(context.Context, *pb.ExecuteCommandRequest) (*pb.ExecuteCommandResponse, error) {
	s.note("ExecuteCommand")
	return &pb.ExecuteCommandResponse{Success: true, Stdout: "ran"}, nil
}

func (s *stubAgent) WriteFile(context.Context, *pb.FilePayload) (*pb.CommandResponse, error) {
	s.note("WriteFile")
	return &pb.CommandResponse{Success: true, Message: "written"}, nil
}

func (s *stubAgent) Uninstall(context.Context, *pb.Empty) (*pb.CommandResponse, error) {
	s.note("Uninstall")
	return &pb.CommandResponse{Success: true}, nil
}

func (s *stubAgent) SwitchConnectMode(context.Context, *pb.SwitchConnectModeRequest) (*pb.CommandResponse, error) {
	s.note("SwitchConnectMode")
	return &pb.CommandResponse{Success: true}, nil
}

func serviceMethods(t *testing.T) []string {
	t.Helper()
	svc := pb.File_node_agent_proto.Services().ByName("NodeAgent")
	var names []string
	for i := 0; i < svc.Methods().Len(); i++ {
		names = append(names, string(svc.Methods().Get(i).Name()))
	}
	if len(names) == 0 {
		t.Fatal("no NodeAgent methods in the proto descriptor")
	}
	return names
}

// Every call the proto has, by every name the hub might use for it: the
// dispatcher refuses exactly what the direct listener's interceptors would,
// before it reads the payload and before the server is touched.
func TestHandleCommandAppliesTheProfile(t *testing.T) {
	oracle := middleware.NewProfileMiddleware(config.ProfileManaged)
	ctx := context.Background()
	garbage := []byte{0xff, 0xff, 0xff, 0xff} // not a valid message for any request

	for _, profile := range []config.Profile{config.ProfileManaged, config.ProfileFull, ""} {
		for _, name := range serviceMethods(t) {
			for _, spelling := range []string{name, "/nodeagent.NodeAgent/" + name, "/other.Service/" + name} {
				stub := &stubAgent{}
				h := NewCommandHandler(stub, profile)
				_, err := h.HandleCommand(ctx, spelling, garbage)

				refused := status.Code(err) == codes.PermissionDenied
				want := profile.Managed() && oracle.CheckName(name) != nil
				if refused != want {
					t.Errorf("profile %q, %s: PermissionDenied = %v (err %v), want %v", profile, spelling, refused, err, want)
				}
				if refused && !strings.Contains(err.Error(), "managed profile") {
					t.Errorf("profile %q, %s: the refusal doesn't say why: %v", profile, spelling, err)
				}
				if refused && len(stub.reached()) != 0 {
					t.Errorf("profile %q, %s: a refused call reached the server: %v", profile, spelling, stub.reached())
				}
				if refused && strings.Contains(err.Error(), "unmarshal") {
					t.Errorf("profile %q, %s: the payload was read before the refusal", profile, spelling)
				}
			}
		}
	}
}

func TestHandleCommandRefusesTheDangerousCallsOnManaged(t *testing.T) {
	stub := &stubAgent{}
	h := NewCommandHandler(stub, config.ProfileManaged)
	execPayload, _ := proto.Marshal(&pb.ExecuteCommandRequest{Command: "id"})
	filePayload, _ := proto.Marshal(&pb.FilePayload{Path: "x", Content: []byte("y")})

	for _, c := range []struct {
		method  string
		payload []byte
	}{
		{"ExecuteCommand", execPayload},
		{"/nodeagent.NodeAgent/ExecuteCommand", execPayload},
		{"WriteFile", filePayload},
		{"Uninstall", nil},
		{"SwitchConnectMode", nil},
		{"Wipe", nil},
		{"UpdateXrayBinary", nil},
		// the name is cut at the last slash, and the check sees what is left
		{"GetStatus/../ExecuteCommand", execPayload},
		{"nodeagent.NodeAgent/GetStatus/ExecuteCommand", execPayload},
		{"ExecuteCommand/", nil},
		{"", nil},
		{"/", nil},
		// and a name that only looks like an allowed one
		{"getstatus", nil},
		{"GetStatus ", nil},
		{"ExecuteCommand\x00", execPayload},
	} {
		resp, err := h.HandleCommand(context.Background(), c.method, c.payload)
		if status.Code(err) != codes.PermissionDenied || resp != nil {
			t.Errorf("%q: resp %v, err %v", c.method, resp, err)
		}
	}
	if got := stub.reached(); len(got) != 0 {
		t.Fatalf("the server was reached: %v", got)
	}

	// what is allowed still gets through
	payload, err := h.HandleCommand(context.Background(), "/nodeagent.NodeAgent/GetStatus", nil)
	if err != nil {
		t.Fatal(err)
	}
	var st pb.NodeStatus
	if err := proto.Unmarshal(payload, &st); err != nil || !st.XrayRunning {
		t.Fatalf("GetStatus: %v, %+v", err, &st)
	}
}

// The full profile dispatches as it always did.
func TestHandleCommandOnFullStillRunsEverything(t *testing.T) {
	for _, profile := range []config.Profile{config.ProfileFull, ""} {
		stub := &stubAgent{}
		h := NewCommandHandler(stub, profile)
		execPayload, _ := proto.Marshal(&pb.ExecuteCommandRequest{Command: "id"})

		payload, err := h.HandleCommand(context.Background(), "/nodeagent.NodeAgent/ExecuteCommand", execPayload)
		if err != nil {
			t.Fatalf("profile %q: %v", profile, err)
		}
		var resp pb.ExecuteCommandResponse
		if err := proto.Unmarshal(payload, &resp); err != nil || resp.Stdout != "ran" {
			t.Fatalf("profile %q: %v, %+v", profile, err, &resp)
		}
		for _, name := range []string{"WriteFile", "Uninstall", "SwitchConnectMode"} {
			if _, err := h.HandleCommand(context.Background(), name, nil); err != nil {
				t.Errorf("profile %q, %s: %v", profile, name, err)
			}
		}
		if got := strings.Join(stub.reached(), ","); got != "ExecuteCommand,WriteFile,Uninstall,SwitchConnectMode" {
			t.Errorf("profile %q: the server saw %q", profile, got)
		}
	}
}

func TestHandlerCheck(t *testing.T) {
	managed := NewCommandHandler(&stubAgent{}, config.ProfileManaged)
	full := NewCommandHandler(&stubAgent{}, config.ProfileFull)
	for _, name := range []string{"OpenTerminal", "ExecuteCommand", "SomethingNew"} {
		if status.Code(managed.Check(name)) != codes.PermissionDenied {
			t.Errorf("managed.Check(%q) = %v", name, managed.Check(name))
		}
		if err := full.Check(name); err != nil {
			t.Errorf("full.Check(%q) = %v", name, err)
		}
	}
	if err := managed.Check("StreamLogs"); err != nil {
		t.Errorf("managed.Check(StreamLogs) = %v", err)
	}
}

// fakeHub is the other end of a reverse connection: it takes the agent's
// registration, sends what the test tells it to, and hands back what comes in.
type fakeHub struct {
	hubpb.UnimplementedAgentHubServer
	registered chan *hubpb.AgentRegistration
	toAgent    chan *hubpb.HubEnvelope
	fromAgent  chan *hubpb.AgentEnvelope
	terminals  chan []*hubpb.TerminalFrame // one entry per terminal stream, when it ends
}

func newFakeHub() *fakeHub {
	return &fakeHub{
		registered: make(chan *hubpb.AgentRegistration, 4),
		toAgent:    make(chan *hubpb.HubEnvelope, 16),
		fromAgent:  make(chan *hubpb.AgentEnvelope, 16),
		terminals:  make(chan []*hubpb.TerminalFrame, 4),
	}
}

func (h *fakeHub) CommandChannel(stream grpc.BidiStreamingServer[hubpb.AgentEnvelope, hubpb.HubEnvelope]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	h.registered <- first.GetRegistration()
	go func() {
		for {
			env, err := stream.Recv()
			if err != nil {
				return
			}
			h.fromAgent <- env
		}
	}()
	for {
		select {
		case env := <-h.toAgent:
			if err := stream.Send(env); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (h *fakeHub) TerminalChannel(stream grpc.BidiStreamingServer[hubpb.TerminalFrame, hubpb.TerminalFrame]) error {
	var frames []*hubpb.TerminalFrame
	for {
		f, err := stream.Recv()
		if err != nil { // the agent half-closes when it has nothing more to say
			break
		}
		frames = append(frames, f)
	}
	h.terminals <- frames
	return nil
}

func within[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

// connect runs a connector with the given profile against a fake hub and
// returns the hub once the agent has registered.
func connect(t *testing.T, profile config.Profile, srv pb.NodeAgentServer) *fakeHub {
	t.Helper()
	hub := newFakeHub()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	hubpb.RegisterAgentHubServer(gs, hub)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	cfg := config.DefaultConfig()
	cfg.ConnectMode = "reverse"
	cfg.HubURL = lis.Addr().String()
	cfg.NodeUUID = "node-under-test"
	cfg.Profile = profile
	cfg.TLS.Enabled = false

	connector := NewConnector(cfg, NewCommandHandler(srv, cfg.Profile))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = connector.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		connector.Stop()
		<-done
	})

	reg := within(t, hub.registered, "the agent to register")
	if reg.GetNodeUuid() != "node-under-test" {
		t.Fatalf("registered as %q", reg.GetNodeUuid())
	}
	return hub
}

// command sends one command the way the hub does and returns the agent's answer.
func (h *fakeHub) command(t *testing.T, id, method string, req proto.Message) *hubpb.AgentEnvelope {
	t.Helper()
	var payload []byte
	if req != nil {
		var err error
		if payload, err = proto.Marshal(req); err != nil {
			t.Fatal(err)
		}
	}
	h.toAgent <- &hubpb.HubEnvelope{RequestId: id, Method: method, Payload: payload, DeadlineMs: 10000}
	for {
		env := within(t, h.fromAgent, "the answer to "+method)
		if env.GetRequestId() == id {
			return env
		}
	}
}

func (h *fakeHub) openTerminal(t *testing.T, session string) []*hubpb.TerminalFrame {
	t.Helper()
	h.toAgent <- &hubpb.HubEnvelope{
		Control: &hubpb.HubEnvelope_OpenTerminal{OpenTerminal: &hubpb.OpenTerminalSignal{SessionId: session}},
	}
	return within(t, h.terminals, "the terminal stream to end")
}

// markerShell is a "shell" that proves it ran by creating a file.
func markerShell(t *testing.T) (shell, marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "shell-started")
	shell = filepath.Join(dir, "shell.sh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	return shell, marker
}

func TestManagedAgentOverTheReverseChannel(t *testing.T) {
	_, marker := markerShell(t)
	stub := &stubAgent{}
	hub := connect(t, config.ProfileManaged, stub)

	// a command that would run a shell command, one that would write a file,
	// and one that would flip the node's mode
	for i, c := range []struct {
		method string
		req    proto.Message
	}{
		{"/nodeagent.NodeAgent/ExecuteCommand", &pb.ExecuteCommandRequest{Command: "touch " + marker}},
		{"/nodeagent.NodeAgent/WriteFile", &pb.FilePayload{Path: marker, Content: []byte("x")}},
		{"SwitchConnectMode", &pb.SwitchConnectModeRequest{Mode: "direct", ListenPort: 9090}},
		{"Uninstall", &pb.Empty{}},
	} {
		env := hub.command(t, "refused-"+string(rune('a'+i)), c.method, c.req)
		if env.GetPayload() != nil || !strings.Contains(env.GetError(), "PermissionDenied") || !strings.Contains(env.GetError(), "managed profile") {
			t.Errorf("%s: answer = payload %v, error %q", c.method, env.GetPayload(), env.GetError())
		}
	}

	// a call on the list is answered
	env := hub.command(t, "allowed", "/nodeagent.NodeAgent/GetStatus", &pb.Empty{})
	var st pb.NodeStatus
	if env.GetError() != "" || proto.Unmarshal(env.GetPayload(), &st) != nil || !st.XrayRunning {
		t.Fatalf("GetStatus: error %q, %+v", env.GetError(), &st)
	}

	// and the terminal the hub asks for is refused on its own stream, with
	// the reason, and no shell starts
	frames := hub.openTerminal(t, "session-1")
	if len(frames) != 2 {
		t.Fatalf("%d frames on the terminal stream: %v", len(frames), frames)
	}
	if frames[0].GetSessionId() != "session-1" || frames[0].GetNodeUuid() != "node-under-test" {
		t.Errorf("identifying frame = %v", frames[0])
	}
	if msg := frames[1].GetError(); !strings.Contains(msg, "managed profile") {
		t.Errorf("second frame = %v, want the refusal", frames[1])
	}

	if got := stub.reached(); len(got) != 0 {
		t.Fatalf("the server was reached: %v", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("something ran a command or a shell: %v", err)
	}
}

func TestFullAgentOverTheReverseChannelIsUnchanged(t *testing.T) {
	for _, profile := range []config.Profile{config.ProfileFull, ""} {
		_, marker := markerShell(t)
		stub := &stubAgent{}
		hub := connect(t, profile, stub)

		env := hub.command(t, "exec", "/nodeagent.NodeAgent/ExecuteCommand", &pb.ExecuteCommandRequest{Command: "id"})
		var resp pb.ExecuteCommandResponse
		if env.GetError() != "" || proto.Unmarshal(env.GetPayload(), &resp) != nil || resp.Stdout != "ran" {
			t.Fatalf("profile %q: ExecuteCommand: error %q, %+v", profile, env.GetError(), &resp)
		}

		frames := hub.openTerminal(t, "session-2")
		var sawExit bool
		for _, f := range frames {
			if f.GetError() != "" {
				t.Errorf("profile %q: terminal error frame: %q", profile, f.GetError())
			}
			if _, ok := f.GetPayload().(*hubpb.TerminalFrame_ExitCode); ok {
				sawExit = true
			}
		}
		if !sawExit {
			t.Errorf("profile %q: the shell never reported an exit: %v", profile, frames)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("profile %q: the shell did not start: %v", profile, err)
		}
		if got := stub.reached(); len(got) != 1 || got[0] != "ExecuteCommand" {
			t.Errorf("profile %q: the server saw %v", profile, got)
		}
	}
}
