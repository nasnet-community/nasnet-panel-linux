package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func reachable(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// listening serves a bare gRPC server on a real port, as Start does.
func listening(t *testing.T) (*Server, int) {
	t.Helper()
	port := freePort(t)
	cfg := config.DefaultConfig()
	cfg.ListenAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cfg.Xray.APIAddr = "127.0.0.1:10085"
	cfg.FilePath = filepath.Join(t.TempDir(), "config.yaml")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, grpcServer: grpc.NewServer()}
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	s.listener = ln
	s.serveErr = make(chan error, 1)
	s.portsMu.Lock()
	s.ports = map[string]net.Listener{cfg.ListenAddr: ln}
	s.dropped = map[net.Listener]bool{}
	s.serveLocked(ln)
	s.portsMu.Unlock()
	t.Cleanup(s.grpcServer.Stop)
	return s, port
}

func move(t *testing.T, s *Server, req *pb.MoveListenPortRequest) *pb.CommandResponse {
	t.Helper()
	resp, err := s.MoveListenPort(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestTheListenPortMovesInSteps(t *testing.T) {
	s, old := listening(t)
	next := freePort(t)

	if r := move(t, s, &pb.MoveListenPortRequest{Port: int32(next)}); !r.Success {
		t.Fatalf("open: %+v", r)
	}
	if !reachable(old) || !reachable(next) {
		t.Fatal("both ports should be open while the hub moves")
	}
	saved, _ := config.Load(s.cfg.FilePath)
	if saved.NextListenAddr != net.JoinHostPort("127.0.0.1", strconv.Itoa(next)) {
		t.Fatalf("the pending port isn't saved for a restart: %q", saved.NextListenAddr)
	}
	// a repeat changes nothing
	if r := move(t, s, &pb.MoveListenPortRequest{Port: int32(next)}); !r.Success {
		t.Fatalf("repeat: %+v", r)
	}

	if r := move(t, s, &pb.MoveListenPortRequest{Finish: true}); !r.Success {
		t.Fatalf("finish: %+v", r)
	}
	if reachable(old) {
		t.Error("the old port should close once the hub is in on the new one")
	}
	if !reachable(next) {
		t.Error("the new port should stay open")
	}
	saved, _ = config.Load(s.cfg.FilePath)
	if portOf(saved.ListenAddr) != next || saved.NextListenAddr != "" {
		t.Errorf("saved %q / %q", saved.ListenAddr, saved.NextListenAddr)
	}
	select {
	case err := <-s.serveErr:
		t.Fatalf("closing the old port on purpose ended the server: %v", err)
	default:
	}
}

func TestAnAbortedMoveKeepsTheOldPort(t *testing.T) {
	s, old := listening(t)
	next := freePort(t)
	move(t, s, &pb.MoveListenPortRequest{Port: int32(next)})
	if r := move(t, s, &pb.MoveListenPortRequest{Abort: true}); !r.Success {
		t.Fatalf("abort: %+v", r)
	}
	if !reachable(old) || reachable(next) {
		t.Error("abort keeps the old port and closes the new one")
	}
	if saved, _ := config.Load(s.cfg.FilePath); saved.NextListenAddr != "" || portOf(saved.ListenAddr) != old {
		t.Errorf("saved %q / %q", saved.ListenAddr, saved.NextListenAddr)
	}
	if r := move(t, s, &pb.MoveListenPortRequest{Finish: true}); r.Success || r.ErrorCode != "NO_MOVE" {
		t.Errorf("finish with nothing open: %+v", r)
	}
}

func TestPortsTheAgentRefuses(t *testing.T) {
	s, old := listening(t)
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	busy := held.Addr().(*net.TCPAddr).Port
	for _, c := range []struct {
		port int
		code string
	}{{0, "BAD_PORT"}, {70000, "BAD_PORT"}, {old, "SAME_PORT"}, {10085, "PORT_TAKEN"}, {busy, "PORT_BUSY"}} {
		if r := move(t, s, &pb.MoveListenPortRequest{Port: int32(c.port)}); r.Success || r.ErrorCode != c.code {
			t.Errorf("port %d: %+v, want %s", c.port, r, c.code)
		}
	}
	if !reachable(old) {
		t.Error("a refused move must leave the agent where it was")
	}
}

func TestAnUnfinishedMoveIsReopenedAfterARestart(t *testing.T) {
	s, _ := listening(t)
	next := freePort(t)
	s.cfg.NextListenAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(next))
	s.portsMu.Lock()
	s.reopenPendingPortLocked()
	s.portsMu.Unlock()
	if !reachable(next) {
		t.Error("the pending port should open again at start")
	}
}

func TestAModeSwitchGoesBackUnlessConfirmed(t *testing.T) {
	restarts := make(chan struct{}, 4)
	defer func(old func(*Server)) { restartAgent = old }(restartAgent)
	restartAgent = func(*Server) { restarts <- struct{}{} }

	s, port := listening(t)
	s.cfg.NodeUUID = "n-1"
	resp, err := s.SwitchConnectMode(context.Background(), &pb.SwitchConnectModeRequest{Mode: "reverse", HubUrl: "hub.example:9091", GraceSeconds: 60})
	if err != nil || !resp.Success {
		t.Fatalf("switch: %+v %v", resp, err)
	}
	select {
	case <-restarts:
	case <-time.After(3 * time.Second):
		t.Fatal("the agent should restart into the new mode")
	}
	saved, _ := config.Load(s.cfg.FilePath)
	if saved.ConnectMode != "reverse" || saved.HubURL != "hub.example:9091" || saved.ModeFallback == nil || saved.ModeFallback.ConnectMode != "direct" {
		t.Fatalf("saved %+v / %+v", saved, saved.ModeFallback)
	}
	if r, _ := s.SwitchConnectMode(context.Background(), &pb.SwitchConnectModeRequest{Mode: "direct", ListenPort: 9090}); r.Success || r.ErrorCode != "SWITCHING" {
		t.Errorf("a second switch while one waits: %+v", r)
	}

	// the deadline passes with no word from the hub: back to direct
	s.cfg.ModeFallback.Deadline = time.Now().Add(-time.Second)
	s.armModeFallback()
	select {
	case <-restarts:
	case <-time.After(3 * time.Second):
		t.Fatal("the agent should restart back into the old mode")
	}
	saved, _ = config.Load(s.cfg.FilePath)
	if saved.ConnectMode != "direct" || portOf(saved.ListenAddr) != port || saved.ModeFallback != nil {
		t.Errorf("after the fallback: %+v", saved)
	}
}

func TestAConfirmedModeStays(t *testing.T) {
	restarts := make(chan struct{}, 4)
	defer func(old func(*Server)) { restartAgent = old }(restartAgent)
	restartAgent = func(*Server) { restarts <- struct{}{} }

	s, _ := listening(t)
	s.cfg.ConnectMode = "reverse"
	s.cfg.HubURL = "hub.example:9091"
	if r, _ := s.SwitchConnectMode(context.Background(), &pb.SwitchConnectModeRequest{Mode: "direct", ListenPort: 9443, GraceSeconds: 1}); !r.Success {
		t.Fatalf("switch: %+v", r)
	}
	<-restarts
	s.armModeFallback()
	if r, _ := s.SwitchConnectMode(context.Background(), &pb.SwitchConnectModeRequest{Confirm: true}); !r.Success {
		t.Fatalf("confirm: %+v", r)
	}
	time.Sleep(1500 * time.Millisecond)
	select {
	case <-restarts:
		t.Fatal("a confirmed mode must not go back")
	default:
	}
	saved, _ := config.Load(s.cfg.FilePath)
	if saved.ConnectMode != "direct" || portOf(saved.ListenAddr) != 9443 || saved.ModeFallback != nil {
		t.Errorf("saved %+v", saved)
	}
	if _, err := os.Stat(s.cfg.FilePath); err != nil {
		t.Fatal(err)
	}
}
