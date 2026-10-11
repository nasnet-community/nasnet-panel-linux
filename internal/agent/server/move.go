package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
)

const defaultModeGrace = 2 * time.Minute

// restartAgent restarts under the config just saved; tests replace it.
var restartAgent = func(s *Server) { s.restartSelf(executable()) }

func ok(msg string) *pb.CommandResponse { return &pb.CommandResponse{Success: true, Message: msg} }

func refuse(code, msg string) *pb.CommandResponse {
	return &pb.CommandResponse{Success: false, ErrorCode: code, Message: msg}
}

// portOf is the port in an address like ":9090" (0 when there is none).
func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// withPort keeps addr's host and swaps its port.
func withPort(addr string, port int) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// serveLocked serves the gRPC API on ln. A port move closing ln on purpose
// isn't a failure; anything else ends Start.
func (s *Server) serveLocked(ln net.Listener) {
	go func() {
		err := s.grpcServer.Serve(ln)
		s.portsMu.Lock()
		dropped := s.dropped[ln]
		delete(s.dropped, ln)
		s.portsMu.Unlock()
		if dropped {
			return
		}
		select {
		case s.serveErr <- err:
		default:
		}
	}()
}

func (s *Server) dropLocked(addr string) {
	if ln := s.ports[addr]; ln != nil {
		s.dropped[ln] = true
		delete(s.ports, addr)
		ln.Close()
	}
}

// reopenPendingPortLocked opens the port of a move that a restart interrupted.
func (s *Server) reopenPendingPortLocked() {
	next := s.cfg.NextListenAddr
	if next == "" || next == s.cfg.ListenAddr {
		return
	}
	ln, err := net.Listen("tcp", next)
	if err != nil {
		logrus.WithError(err).WithField("addr", next).Warn("Couldn't reopen the port of an unfinished move")
		return
	}
	s.ports[next] = ln
	s.serveLocked(ln)
	logrus.WithField("addr", next).Info("Reopened the port of an unfinished move")
}

// MoveListenPort moves the direct listener in steps, so the hub can always
// get back in: open the new port beside the old, then finish or abort.
func (s *Server) MoveListenPort(ctx context.Context, req *pb.MoveListenPortRequest) (*pb.CommandResponse, error) {
	s.portsMu.Lock()
	defer s.portsMu.Unlock()
	if s.grpcServer == nil || s.ports == nil {
		return refuse("NOT_DIRECT", "the agent isn't listening for the panel: it isn't in direct mode"), nil
	}
	cur, next := s.cfg.ListenAddr, s.cfg.NextListenAddr

	switch {
	case req.Finish:
		ln := s.ports[next]
		if next == "" || ln == nil {
			return refuse("NO_MOVE", "no port move is under way"), nil
		}
		s.cfg.ListenAddr, s.cfg.NextListenAddr = next, ""
		if err := s.cfg.Save(); err != nil {
			s.cfg.ListenAddr, s.cfg.NextListenAddr = cur, next
			return refuse("SAVE_FAILED", fmt.Sprintf("couldn't save the agent's config: %v", err)), nil
		}
		s.listener = ln
		s.dropLocked(cur)
		logrus.WithField("addr", next).Info("Moved the agent to its new port")
		return ok(fmt.Sprintf("the agent listens on port %d only", portOf(next))), nil

	case req.Abort:
		if next != "" {
			s.dropLocked(next)
			s.cfg.NextListenAddr = ""
			if err := s.cfg.Save(); err != nil {
				return refuse("SAVE_FAILED", fmt.Sprintf("couldn't save the agent's config: %v", err)), nil
			}
		}
		return ok(fmt.Sprintf("the agent stays on port %d", portOf(cur))), nil
	}

	port := int(req.Port)
	switch {
	case port < 1 || port > 65535:
		return refuse("BAD_PORT", "use a port from 1 to 65535"), nil
	case port == portOf(cur):
		return refuse("SAME_PORT", fmt.Sprintf("the agent already listens on port %d", port)), nil
	case port == portOf(s.cfg.Xray.APIAddr):
		return refuse("PORT_TAKEN", fmt.Sprintf("port %d is Xray's API port", port)), nil
	}
	addr := withPort(cur, port)
	if addr == next && s.ports[addr] != nil {
		return ok(fmt.Sprintf("port %d is open", port)), nil // a repeat
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return refuse("PORT_BUSY", fmt.Sprintf("port %d can't be opened on the node: %s", port, bindError(err))), nil
	}
	if next != "" {
		s.dropLocked(next)
	}
	s.cfg.NextListenAddr = addr
	if err := s.cfg.Save(); err != nil {
		ln.Close()
		s.cfg.NextListenAddr = ""
		return refuse("SAVE_FAILED", fmt.Sprintf("couldn't save the agent's config: %v", err)), nil
	}
	s.ports[addr] = ln
	s.serveLocked(ln)
	openFirewallPort(port)
	return ok(fmt.Sprintf("port %d is open beside port %d", port, portOf(cur))), nil
}

func bindError(err error) string {
	if strings.Contains(err.Error(), "address already in use") {
		return "something else is listening on it"
	}
	return err.Error()
}

// openFirewallPort lets a port through ufw when it's on, as the installer does.
func openFirewallPort(port int) {
	if _, err := exec.LookPath("ufw"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ufw", "status").Output()
	if err != nil || !strings.Contains(string(out), "Status: active") {
		return
	}
	if err := exec.CommandContext(ctx, "ufw", "allow", fmt.Sprintf("%d/tcp", port)).Run(); err != nil {
		logrus.WithError(err).WithField("port", port).Warn("Couldn't open the port in ufw")
	}
}

// SwitchConnectMode rewrites the agent's own config for the other mode,
// keeping everything else, and restarts. The old mode comes back by itself
// unless the hub confirms the new one in time.
func (s *Server) SwitchConnectMode(ctx context.Context, req *pb.SwitchConnectModeRequest) (*pb.CommandResponse, error) {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()

	if req.Confirm {
		if s.cfg.ModeFallback == nil {
			return ok("nothing to confirm"), nil
		}
		fb := s.cfg.ModeFallback
		s.cfg.ModeFallback = nil
		if err := s.cfg.Save(); err != nil {
			s.cfg.ModeFallback = fb
			return refuse("SAVE_FAILED", fmt.Sprintf("couldn't save the agent's config: %v", err)), nil
		}
		if s.fallbackTimer != nil {
			s.fallbackTimer.Stop()
			s.fallbackTimer = nil
		}
		logrus.WithField("mode", s.cfg.ConnectMode).Info("The hub confirmed the new connection mode")
		return ok("the new mode stays"), nil
	}

	cur := s.cfg.ConnectMode
	if cur == "" {
		cur = "direct"
	}
	switch {
	case req.Mode != "direct" && req.Mode != "reverse":
		return refuse("BAD_MODE", fmt.Sprintf("unknown connection mode %q", req.Mode)), nil
	case req.Mode == cur:
		return refuse("SAME_MODE", fmt.Sprintf("the agent is already in %s mode", cur)), nil
	case s.cfg.FilePath == "":
		return refuse("NO_CONFIG", "the agent has no config file to change"), nil
	case s.cfg.ModeFallback != nil:
		return refuse("SWITCHING", "a mode switch is still waiting for the hub"), nil
	}

	before := *s.cfg
	grace := time.Duration(req.GraceSeconds) * time.Second
	if grace <= 0 {
		grace = defaultModeGrace
	}
	s.cfg.ModeFallback = &config.ModeFallback{
		ConnectMode: cur,
		ListenAddr:  s.cfg.ListenAddr,
		HubURL:      s.cfg.HubURL,
		Deadline:    time.Now().Add(grace),
	}
	port := 0
	switch req.Mode {
	case "reverse":
		if strings.TrimSpace(req.HubUrl) == "" {
			*s.cfg = before
			return refuse("NO_HUB", "the hub's address is missing"), nil
		}
		s.cfg.HubURL = strings.TrimSpace(req.HubUrl)
		if s.cfg.NodeUUID == "" {
			s.cfg.NodeUUID = req.NodeUuid
		}
		if s.cfg.NodeUUID == "" {
			*s.cfg = before
			return refuse("NO_UUID", "the node's id is missing"), nil
		}
	case "direct":
		port = int(req.ListenPort)
		if port < 1 || port > 65535 {
			*s.cfg = before
			return refuse("BAD_PORT", "use a port from 1 to 65535"), nil
		}
		s.cfg.ListenAddr = withPort(s.cfg.ListenAddr, port)
	}
	s.cfg.ConnectMode = req.Mode
	if err := s.cfg.Save(); err != nil {
		*s.cfg = before
		return refuse("SAVE_FAILED", fmt.Sprintf("couldn't save the agent's config: %v", err)), nil
	}
	if port > 0 {
		openFirewallPort(port)
	}
	logrus.WithField("mode", req.Mode).Info("Switching connection mode")
	// answer first, then restart under the new mode
	go func() {
		time.Sleep(time.Second)
		restartAgent(s)
	}()
	return ok(fmt.Sprintf("restarting in %s mode", req.Mode)), nil
}

// armModeFallback waits out an unconfirmed mode switch, then goes back.
func (s *Server) armModeFallback() {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	if s.cfg == nil || s.cfg.ModeFallback == nil || s.cfg.FilePath == "" {
		return
	}
	fb := s.cfg.ModeFallback
	wait := time.Until(fb.Deadline)
	if wait < 0 {
		wait = 0
	}
	logrus.WithFields(logrus.Fields{"back_to": fb.ConnectMode, "in": wait.Round(time.Second)}).Info("Waiting for the hub to confirm the connection mode")
	s.fallbackTimer = time.AfterFunc(wait, s.revertMode)
}

func (s *Server) revertMode() {
	s.modeMu.Lock()
	fb := s.cfg.ModeFallback
	if fb == nil {
		s.modeMu.Unlock()
		return // confirmed meanwhile
	}
	before := *s.cfg
	s.cfg.ConnectMode, s.cfg.ListenAddr, s.cfg.HubURL = fb.ConnectMode, fb.ListenAddr, fb.HubURL
	s.cfg.ModeFallback = nil
	err := s.cfg.Save()
	if err != nil {
		*s.cfg = before
	}
	s.fallbackTimer = nil
	s.modeMu.Unlock()
	if err != nil {
		logrus.WithError(err).Error("Couldn't go back to the old connection mode")
		return
	}
	logrus.WithField("mode", fb.ConnectMode).Warn("The hub never confirmed the new connection mode: going back")
	restartAgent(s)
}

func executable() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return os.Args[0]
}
