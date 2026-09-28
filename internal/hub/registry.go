package hub

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"
)

// ReverseSession tracks an active reverse-connected agent.
type ReverseSession struct {
	NodeID       uint
	NodeUUID     string
	Stream       hubpb.AgentHub_CommandChannelServer
	ConnectedAt  time.Time
	AgentVersion string

	mu       sync.Mutex
	pending  map[string]chan *hubpb.AgentEnvelope
	lastPong time.Time
	lastRTT  int64

	kick     chan struct{}
	kickOnce sync.Once
}

// Kick ends the session; the agent reconnects and goes through registration,
// and its certificate check, again.
func (s *ReverseSession) Kick() { s.kickOnce.Do(func() { close(s.kick) }) }

// Kicked is closed once Kick has been called.
func (s *ReverseSession) Kicked() <-chan struct{} { return s.kick }

// ConnectedAgentRegistry maintains active reverse agent sessions.
type ConnectedAgentRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*ReverseSession // nodeUUID -> session
	byNodeID map[uint]*ReverseSession   // nodeID -> session

	// Callbacks
	onConnect    func(nodeID uint)
	onDisconnect func(nodeID uint)
}

// NewRegistry creates a new ConnectedAgentRegistry.
func NewRegistry() *ConnectedAgentRegistry {
	return &ConnectedAgentRegistry{
		sessions: make(map[string]*ReverseSession),
		byNodeID: make(map[uint]*ReverseSession),
	}
}

// SetCallbacks sets connection/disconnection callbacks.
func (r *ConnectedAgentRegistry) SetCallbacks(onConnect, onDisconnect func(nodeID uint)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onConnect = onConnect
	r.onDisconnect = onDisconnect
}

// Register adds or replaces a reverse session. If a session already exists
// for this UUID, the old session's pending channels are closed (the old
// stream should already be broken or will be cancelled by the caller).
func (r *ConnectedAgentRegistry) Register(nodeUUID string, nodeID uint, stream hubpb.AgentHub_CommandChannelServer, agentVersion string) *ReverseSession {
	r.mu.Lock()

	// Evict old session if exists
	if old, ok := r.sessions[nodeUUID]; ok {
		old.mu.Lock()
		for id, ch := range old.pending {
			close(ch)
			delete(old.pending, id)
		}
		old.mu.Unlock()
		delete(r.byNodeID, old.NodeID)
	}

	s := &ReverseSession{
		NodeID:       nodeID,
		NodeUUID:     nodeUUID,
		Stream:       stream,
		ConnectedAt:  time.Now(),
		AgentVersion: agentVersion,
		pending:      make(map[string]chan *hubpb.AgentEnvelope),
		kick:         make(chan struct{}),
	}
	r.sessions[nodeUUID] = s
	r.byNodeID[nodeID] = s

	onConnect := r.onConnect
	r.mu.Unlock()

	if onConnect != nil {
		onConnect(nodeID)
	}

	return s
}

// Unregister removes a reverse session and closes all pending channels.
// The caller must pass the session pointer it received from Register so that
// a stale handler (whose session was already replaced by a newer connection)
// does not accidentally remove the current session.
func (r *ConnectedAgentRegistry) Unregister(nodeUUID string, session *ReverseSession) {
	r.mu.Lock()

	current, ok := r.sessions[nodeUUID]
	if !ok || current != session {
		// Session was already replaced by a newer connection or removed.
		r.mu.Unlock()
		return
	}

	current.mu.Lock()
	for id, ch := range current.pending {
		close(ch)
		delete(current.pending, id)
	}
	current.mu.Unlock()

	delete(r.sessions, nodeUUID)
	delete(r.byNodeID, current.NodeID)

	onDisconnect := r.onDisconnect
	r.mu.Unlock()

	if onDisconnect != nil {
		onDisconnect(current.NodeID)
	}
}

// GetByUUID returns the session for a node UUID, or nil.
func (r *ConnectedAgentRegistry) GetByUUID(nodeUUID string) *ReverseSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[nodeUUID]
}

// GetByNodeID returns the session for a node ID, or nil.
func (r *ConnectedAgentRegistry) GetByNodeID(nodeID uint) *ReverseSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byNodeID[nodeID]
}

// IsConnected checks if a node UUID has an active session.
func (r *ConnectedAgentRegistry) IsConnected(nodeUUID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.sessions[nodeUUID]
	return ok
}

// ConnectedCount returns the number of active reverse sessions.
func (r *ConnectedAgentRegistry) ConnectedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

// SendCommand dispatches a command to a reverse-connected agent and waits for the response.
// It returns the serialized response payload or an error.
func (r *ConnectedAgentRegistry) SendCommand(nodeUUID, method string, payload []byte, timeout time.Duration) ([]byte, error) {
	r.mu.RLock()
	s, ok := r.sessions[nodeUUID]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("reverse agent %s is not connected", nodeUUID)
	}

	requestID := uuid.New().String()
	respCh := make(chan *hubpb.AgentEnvelope, 1)

	s.mu.Lock()
	s.pending[requestID] = respCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, requestID)
		s.mu.Unlock()
	}()

	env := &hubpb.HubEnvelope{
		RequestId:  requestID,
		Method:     method,
		Payload:    payload,
		DeadlineMs: timeout.Milliseconds(),
	}

	// Send under session lock to prevent concurrent writes to the stream
	s.mu.Lock()
	err := s.Stream.Send(env)
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to send command %s to %s: %w", method, nodeUUID, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-respCh:
		if !ok {
			return nil, fmt.Errorf("session closed while waiting for response to %s", method)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("agent error for %s: %s", method, resp.Error)
		}
		return resp.Payload, nil
	case <-timer.C:
		return nil, fmt.Errorf("timeout waiting for response to %s from %s", method, nodeUUID)
	}
}

// SendControl sends a control signal (ping, open terminal, open log stream)
// on the command channel without waiting for a response.
func (r *ConnectedAgentRegistry) SendControl(nodeUUID string, env *hubpb.HubEnvelope) error {
	r.mu.RLock()
	s, ok := r.sessions[nodeUUID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("reverse agent %s is not connected", nodeUUID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Stream.Send(env)
}

// RouteResponse delivers a response envelope to the pending request channel.
// Called by the hub server's recv loop when it gets an AgentEnvelope with a request_id.
func (r *ConnectedAgentRegistry) RouteResponse(nodeUUID string, env *hubpb.AgentEnvelope) {
	r.mu.RLock()
	s, ok := r.sessions[nodeUUID]
	r.mu.RUnlock()
	if !ok {
		return
	}

	s.mu.Lock()
	ch, ok := s.pending[env.RequestId]
	s.mu.Unlock()
	if !ok {
		return
	}

	select {
	case ch <- env:
	default:
		// Channel full or already closed — drop
	}
}

// LastRTT returns the latest heartbeat round trip in ms and when it arrived
// (zero time when none has yet).
func (s *ReverseSession) LastRTT() (int64, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRTT, s.lastPong
}

// UpdatePong records heartbeat pong data for a session.
func (s *ReverseSession) UpdatePong(rtt int64, configHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPong = time.Now()
	s.lastRTT = rtt
}
