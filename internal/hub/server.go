package hub

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"sync"
	"time"

	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const maxMsgSize = 100 * 1024 * 1024 // 100 MB

// NodeLookupFunc resolves a node UUID to its database ID and connect mode.
// Returns (nodeID, connectMode, found).
type NodeLookupFunc func(ctx context.Context, uuid string) (uint, string, bool)

// HeartbeatCallback is called when a heartbeat pong is received from an agent.
type HeartbeatCallback func(nodeID uint, pong *hubpb.HeartbeatPong)

// HubServer is the gRPC server that accepts reverse agent connections.
type HubServer struct {
	hubpb.UnimplementedAgentHubServer

	registry    *ConnectedAgentRegistry
	nodeLookup  NodeLookupFunc
	heartbeatCB HeartbeatCallback
	certGate    func(nodeID uint, leaf *x509.Certificate) error
	grpcServer  *grpc.Server
	listener    net.Listener

	terminalMu      sync.Mutex
	terminalWaiters map[string]chan hubpb.AgentHub_TerminalChannelServer

	logStreamMu      sync.Mutex
	logStreamWaiters map[string]chan hubpb.AgentHub_LogChannelServer
}

// NewHubServer creates a new HubServer.
func NewHubServer(registry *ConnectedAgentRegistry, nodeLookup NodeLookupFunc) *HubServer {
	return &HubServer{
		registry:         registry,
		nodeLookup:       nodeLookup,
		terminalWaiters:  make(map[string]chan hubpb.AgentHub_TerminalChannelServer),
		logStreamWaiters: make(map[string]chan hubpb.AgentHub_LogChannelServer),
	}
}

// SetHeartbeatCallback registers a callback invoked on every heartbeat pong.
func (s *HubServer) SetHeartbeatCallback(cb HeartbeatCallback) {
	s.heartbeatCB = cb
}

// SetCertGate registers the check run on each agent registration with the
// client certificate the agent presented.
func (s *HubServer) SetCertGate(fn func(nodeID uint, leaf *x509.Certificate) error) {
	s.certGate = fn
}

func peerLeaf(ctx context.Context) *x509.Certificate {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.PeerCertificates) == 0 {
		return nil
	}
	return info.State.PeerCertificates[0]
}

// ListenAndServe starts the gRPC listener on addr, optionally with TLS.
func (s *HubServer) ListenAndServe(addr string, tlsConfig *tls.Config) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = lis

	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time: 15 * time.Second, // ping clients every 15s if idle
			// grpc-go also applies this as TCP_USER_TIMEOUT on every accepted
			// connection, so it doubles as "abort the socket after N seconds of
			// unacknowledged data". Keep it well above any plausible link blip:
			// a node on a flapping link loses its session for 20-40s otherwise.
			Timeout: 25 * time.Second, // wait 25s for a ping ack
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second, // minimum interval clients can ping
			PermitWithoutStream: true,
		}),
	}

	if tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	s.grpcServer = grpc.NewServer(opts...)
	hubpb.RegisterAgentHubServer(s.grpcServer, s)

	logger.GetLogger().WithField("addr", addr).Info("hub gRPC server listening")

	return s.grpcServer.Serve(lis)
}

// Stop performs a graceful shutdown of the gRPC server.
// It attempts a graceful stop for up to 5 seconds, then forces shutdown
// to avoid blocking on long-lived bidirectional streams (CommandChannel, etc.).
func (s *HubServer) Stop() {
	if s.grpcServer == nil {
		return
	}

	log := logger.GetLogger()
	log.Info("hub: stopping gRPC server...")

	stopped := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		log.Info("hub: gRPC server stopped gracefully")
	case <-time.After(5 * time.Second):
		log.Warn("hub: graceful shutdown timed out, forcing stop")
		s.grpcServer.Stop()
	}
}

// CommandChannel is the main bidirectional stream for agent registration,
// command routing, and heartbeat processing.
func (s *HubServer) CommandChannel(stream grpc.BidiStreamingServer[hubpb.AgentEnvelope, hubpb.HubEnvelope]) error {
	log := logger.GetLogger()

	// --- 1. Receive registration frame ---
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Unavailable, "failed to receive registration: %v", err)
	}

	reg := first.GetRegistration()
	if reg == nil {
		return status.Error(codes.InvalidArgument, "first message must be a registration event")
	}

	nodeUUID := reg.GetNodeUuid()
	if nodeUUID == "" {
		return status.Error(codes.InvalidArgument, "registration missing node_uuid")
	}

	// --- 2. Validate UUID and connect mode ---
	ctx := stream.Context()
	nodeID, connectMode, found := s.nodeLookup(ctx, nodeUUID)
	if !found {
		log.WithField("uuid", nodeUUID).Warn("hub: unknown node tried to connect")
		return status.Errorf(codes.NotFound, "node %s not found", nodeUUID)
	}
	if connectMode != "reverse" {
		log.WithFields(map[string]interface{}{
			"uuid": nodeUUID,
			"mode": connectMode,
		}).Warn("hub: node connected with non-reverse mode")
		return status.Errorf(codes.PermissionDenied, "node %s is not configured for reverse mode (got %q)", nodeUUID, connectMode)
	}

	if s.certGate != nil {
		if leaf := peerLeaf(ctx); leaf != nil {
			if err := s.certGate(nodeID, leaf); err != nil {
				log.WithFields(map[string]interface{}{
					"uuid":   nodeUUID,
					"nodeID": nodeID,
					"cn":     leaf.Subject.CommonName,
				}).Warn("hub: agent certificate refused")
				return status.Errorf(codes.PermissionDenied, "certificate refused: %v", err)
			}
		}
	}

	// --- 3. Register in registry ---
	session := s.registry.Register(nodeUUID, nodeID, stream, reg.GetAgentVersion())
	log.WithFields(map[string]interface{}{
		"uuid":    nodeUUID,
		"nodeID":  nodeID,
		"version": reg.GetAgentVersion(),
	}).Info("hub: agent registered")

	// --- 4. Deferred unregister ---
	// Pass the session pointer so that a stale handler whose session was
	// already replaced by a newer agent connection does not accidentally
	// remove the current (newer) session from the registry.
	defer func() {
		s.registry.Unregister(nodeUUID, session)
		log.WithFields(map[string]interface{}{
			"uuid":   nodeUUID,
			"nodeID": nodeID,
		}).Info("hub: agent disconnected")
	}()

	// --- 5. Recv loop ---
	// Runs in its own goroutine so a kick can end the stream; returning
	// cancels the stream context, which unblocks Recv.
	recvErr := make(chan error, 1)
	go func() {
		for {
			env, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}

			switch {
			case env.GetHeartbeat() != nil:
				pong := env.GetHeartbeat()
				session.UpdatePong(pong.GetRttMs(), "")
				if s.heartbeatCB != nil {
					s.heartbeatCB(nodeID, pong)
				}

			case env.GetRequestId() != "":
				s.registry.RouteResponse(nodeUUID, env)

			default:
				log.WithFields(map[string]interface{}{
					"uuid": nodeUUID,
				}).Debug("hub: received unknown envelope type, ignoring")
			}
		}
	}()

	select {
	case err := <-recvErr:
		return err
	case <-session.Kicked():
		log.WithFields(map[string]interface{}{
			"uuid":   nodeUUID,
			"nodeID": nodeID,
		}).Warn("hub: session closed by the hub")
		return status.Error(codes.PermissionDenied, "session closed by the hub")
	}
}

// TerminalChannel accepts a bidirectional terminal stream from an agent and
// delivers it to a waiting caller registered via RegisterTerminalWaiter.
func (s *HubServer) TerminalChannel(stream grpc.BidiStreamingServer[hubpb.TerminalFrame, hubpb.TerminalFrame]) error {
	log := logger.GetLogger()

	// Read the first frame to learn which session this stream belongs to.
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Unavailable, "terminal: failed to receive first frame: %v", err)
	}

	sessionID := first.GetSessionId()
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "terminal: first frame missing session_id")
	}

	// Closing the browser-owned session must terminate this server RPC even
	// though the reverse agent's shared command connection remains healthy.
	session := &terminalSessionStream{
		AgentHub_TerminalChannelServer: stream,
		done:                           make(chan struct{}),
	}
	s.terminalMu.Lock()
	waiter, ok := s.terminalWaiters[sessionID]
	if ok {
		// Registration provides one buffered slot. Deliver under the same
		// lock as cleanup so a cancelled opener cannot leave an orphan stream.
		select {
		case waiter <- session:
		default:
			ok = false
		}
	}
	s.terminalMu.Unlock()
	if !ok {
		return status.Errorf(codes.NotFound, "no available waiter for terminal session %s", sessionID)
	}
	log.WithField("session_id", sessionID).Info("hub: terminal stream delivered to waiter")

	select {
	case <-stream.Context().Done():
	case <-session.done:
	}
	return nil
}

// LogChannel accepts a server-streaming log stream from an agent and delivers
// it to a waiting caller registered via RegisterLogStreamWaiter.
func (s *HubServer) LogChannel(req *hubpb.LogStreamRequest, stream grpc.ServerStreamingServer[hubpb.LogStreamEntry]) error {
	log := logger.GetLogger()

	streamID := req.GetStreamId()
	if streamID == "" {
		return status.Error(codes.InvalidArgument, "log channel: missing stream_id")
	}

	s.logStreamMu.Lock()
	waiter, ok := s.logStreamWaiters[streamID]
	s.logStreamMu.Unlock()

	if !ok {
		log.WithField("stream_id", streamID).Warn("hub: no waiter for log stream")
		return status.Errorf(codes.NotFound, "no waiter for log stream %s", streamID)
	}

	select {
	case waiter <- stream:
		log.WithField("stream_id", streamID).Info("hub: log stream delivered to waiter")
	case <-time.After(5 * time.Second):
		return status.Errorf(codes.DeadlineExceeded, "log channel: timed out delivering stream to waiter for stream %s", streamID)
	}

	// Block until the stream context is done.
	<-stream.Context().Done()
	return nil
}

// RegisterTerminalWaiter creates a channel that will receive the terminal
// stream when the agent connects for the given sessionID.
func (s *HubServer) RegisterTerminalWaiter(sessionID string) <-chan hubpb.AgentHub_TerminalChannelServer {
	ch := make(chan hubpb.AgentHub_TerminalChannelServer, 1)
	s.terminalMu.Lock()
	s.terminalWaiters[sessionID] = ch
	s.terminalMu.Unlock()
	return ch
}

// CleanupTerminalWaiter removes the waiter channel for a terminal session.
func (s *HubServer) CleanupTerminalWaiter(sessionID string) {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if waiter, ok := s.terminalWaiters[sessionID]; ok {
		delete(s.terminalWaiters, sessionID)
		// A cancelled OpenTerminal may leave a just-delivered stream unread.
		select {
		case stream := <-waiter:
			if session, ok := stream.(*terminalSessionStream); ok {
				session.Close()
			}
		default:
		}
	}
}

// RegisterLogStreamWaiter creates a channel that will receive the log stream
// when the agent connects for the given streamID.
func (s *HubServer) RegisterLogStreamWaiter(streamID string) <-chan hubpb.AgentHub_LogChannelServer {
	ch := make(chan hubpb.AgentHub_LogChannelServer, 1)
	s.logStreamMu.Lock()
	s.logStreamWaiters[streamID] = ch
	s.logStreamMu.Unlock()
	return ch
}

// CleanupLogStreamWaiter removes the waiter channel for a log stream.
func (s *HubServer) CleanupLogStreamWaiter(streamID string) {
	s.logStreamMu.Lock()
	delete(s.logStreamWaiters, streamID)
	s.logStreamMu.Unlock()
}
