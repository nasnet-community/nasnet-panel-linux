package usecase

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// AgentMove is how a guarded move of the agent ended: a port move or a mode
// switch. Step names where it stopped when it didn't finish.
type AgentMove struct {
	Moved   bool   `json:"moved"`
	Step    string `json:"step,omitempty"`
	Message string `json:"message"`
}

// BadMoveError is a move the panel refuses before asking the node.
type BadMoveError struct{ Msg string }

func (e *BadMoveError) Error() string { return e.Msg }

func badMove(format string, a ...any) error { return &BadMoveError{Msg: fmt.Sprintf(format, a...)} }

const (
	// how long the agent keeps an unconfirmed mode before going back, and how
	// long the panel waits for it under the new one (well inside that)
	modeGrace = 3 * time.Minute
	modeWait  = 150 * time.Second
	portWait  = 20 * time.Second
)

// sentence turns an agent's message into one for the user.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	s = string(r)
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

// tooOld reads an agent that predates the call.
func tooOld(err error, what string) string {
	if status.Code(err) == codes.Unimplemented {
		return fmt.Sprintf("This agent is too old to %s. Update the agent first.", what)
	}
	return ""
}

// moveClient reaches node's agent where its record says, on a fresh
// connection for a direct node, so a pooled one on another port can't
// answer for this one.
func (u *nodeUsecase) moveClient(node *domain.Node) (agent.NodeClient, error) {
	if u.moveDial != nil {
		return u.moveDial(node)
	}
	if node.ConnectMode == domain.ConnectModeDirect {
		return u.dialDirectAgent(node, net.JoinHostPort(node.IP, strconv.Itoa(node.AgentPort)))
	}
	return u.getAgentClient(node)
}

// reachAgent waits for node's agent to answer at its recorded address and port.
func (u *nodeUsecase) reachAgent(ctx context.Context, node *domain.Node, within time.Duration) (agent.NodeClient, error) {
	deadline := time.Now().Add(within)
	for {
		c, err := u.moveClient(node)
		if err == nil {
			hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err = c.HealthCheck(hctx)
			cancel()
			if err == nil {
				return c, nil
			}
			c.Close()
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// how long the moves wait; tests shorten them
var (
	portWaitFor = portWait
	modeWaitFor = modeWait
)

// relink drops the panel's cached ways to a node so the next call dials anew.
func (u *nodeUsecase) relink(nodeID uint) {
	if u.clientPool != nil {
		u.clientPool.Evict(nodeID)
	}
	if u.heartbeatMgr != nil {
		u.heartbeatMgr.StopNode(nodeID)
		u.heartbeatMgr.TriggerSync()
	}
}

// MoveAgentPort moves a direct node's agent to another port without losing
// it: the agent opens the new port beside the old, the panel switches only
// once it gets in there, and only then does the old port close.
func (u *nodeUsecase) MoveAgentPort(ctx context.Context, nodeID uint, port int) (*AgentMove, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	switch {
	case node.IsLocal():
		return nil, ErrLocalNode
	case node.IsLite():
		return nil, ErrLiteNode
	case node.ConnectMode != domain.ConnectModeDirect:
		return nil, badMove("Only a node in direct mode listens on an agent port.")
	case !node.IsOnline:
		return nil, badMove("The node is offline. Its agent port can move once it's back.")
	case port < 1 || port > 65535:
		return nil, badMove("Use a port from 1 to 65535.")
	case port == node.AgentPort:
		return nil, badMove("The agent already uses port %d.", port)
	case port == node.APIPort:
		return nil, badMove("Port %d is Xray's API port.", port)
	}
	old := node.AgentPort
	log := logger.GetLogger().WithField("node_id", nodeID).WithField("from", old).WithField("to", port)

	client, err := u.moveClient(node)
	if err != nil {
		return &AgentMove{Step: "open", Message: fmt.Sprintf("Couldn't reach the agent to open port %d: %v", port, err)}, nil
	}
	defer client.Close()
	octx, cancel := context.WithTimeout(ctx, 20*time.Second)
	resp, err := client.MoveListenPort(octx, &pb.MoveListenPortRequest{Port: int32(port)})
	cancel()
	if err != nil {
		if msg := tooOld(err, "move its port"); msg != "" {
			return &AgentMove{Step: "open", Message: msg}, nil
		}
		return &AgentMove{Step: "open", Message: fmt.Sprintf("Couldn't open port %d on the node: %v", port, err)}, nil
	}
	if !resp.Success {
		return &AgentMove{Step: "open", Message: sentence(resp.Message)}, nil
	}

	abort := func() {
		actx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if _, err := client.MoveListenPort(actx, &pb.MoveListenPortRequest{Abort: true}); err != nil {
			log.WithError(err).Warn("[MoveAgentPort] Couldn't close the new port again")
		}
	}

	moved := *node
	moved.AgentPort = port
	fresh, err := u.reachAgent(ctx, &moved, portWaitFor)
	if err != nil {
		log.WithError(err).Info("[MoveAgentPort] The new port didn't answer; staying")
		abort()
		return &AgentMove{Step: "switch", Message: fmt.Sprintf("The panel couldn't reach the node on port %d, so it stays on %d. A firewall in front of the server may block that port.", port, old)}, nil
	}
	defer fresh.Close()

	if err := u.nodeRepo.SetNodeAgentPort(ctx, nodeID, port); err != nil {
		abort()
		return nil, fmt.Errorf("couldn't save the new port: %w", err)
	}
	u.relink(nodeID)

	fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	resp, err = fresh.MoveListenPort(fctx, &pb.MoveListenPortRequest{Finish: true})
	cancel()
	if err != nil || !resp.Success {
		why := ""
		if err != nil {
			why = err.Error()
		} else {
			why = resp.Message
		}
		return &AgentMove{Moved: true, Step: "close", Message: fmt.Sprintf("Moved to port %d, but port %d is still open: %s", port, old, sentence(why))}, nil
	}
	log.Info("[MoveAgentPort] Moved")
	return &AgentMove{Moved: true, Message: fmt.Sprintf("Now on port %d. Port %d is closed.", port, old)}, nil
}

// SwitchConnectMode moves a node between direct and reverse. The agent
// rewrites only its connection settings and restarts; the panel confirms
// once it's back under the new mode, and an unconfirmed agent returns to
// the old one by itself.
func (u *nodeUsecase) SwitchConnectMode(ctx context.Context, nodeID uint, newMode string) (*AgentMove, error) {
	if newMode == domain.ConnectModeLocal {
		return nil, ErrLocalNode // there is one built-in node, made at boot
	}
	if newMode != domain.ConnectModeDirect && newMode != domain.ConnectModeReverse {
		return nil, badMove("Unknown connection mode %q.", newMode)
	}
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	switch {
	case node.IsLocal():
		return nil, ErrLocalNode
	case node.IsLite():
		return nil, ErrLiteNode
	case node.IsStealth:
		return nil, badMove("cannot switch connect mode on stealth node %d", nodeID)
	case node.ConnectMode == newMode:
		return nil, badMove("The node is already in %s mode.", newMode)
	case !node.IsOnline:
		return nil, badMove("The node is offline. Its mode can change once it's back.")
	case newMode == domain.ConnectModeDirect && node.IP == "":
		return nil, badMove("The node has no address for the panel to dial.")
	}
	old := node.ConnectMode
	log := logger.GetLogger().WithField("node_id", nodeID).WithField("from", old).WithField("to", newMode)

	req := &pb.SwitchConnectModeRequest{Mode: newMode, GraceSeconds: int32(modeGrace / time.Second), NodeUuid: node.UUID}
	if newMode == domain.ConnectModeReverse {
		req.HubUrl = u.hubURL(ctx)
	} else {
		req.ListenPort = int32(node.AgentPort)
	}
	client, err := u.moveClient(node)
	if err != nil {
		return &AgentMove{Step: "write", Message: fmt.Sprintf("Couldn't reach the agent: %v", err)}, nil
	}
	wctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	resp, err := client.SwitchConnectMode(wctx, req)
	cancel()
	client.Close()
	if err != nil {
		if msg := tooOld(err, "switch modes"); msg != "" {
			return &AgentMove{Step: "write", Message: msg}, nil
		}
		return &AgentMove{Step: "write", Message: fmt.Sprintf("Couldn't change the agent's settings: %v", err)}, nil
	}
	if !resp.Success {
		return &AgentMove{Step: "write", Message: sentence(resp.Message)}, nil
	}

	// the hub admits a reverse agent only once the node is recorded as one
	if err := u.nodeRepo.SetNodeConnectMode(ctx, nodeID, newMode, node.IsActive); err != nil {
		return nil, fmt.Errorf("couldn't save the new mode (the agent goes back by itself): %w", err)
	}
	u.relink(nodeID)
	goBack := func(msg string) (*AgentMove, error) {
		if err := u.nodeRepo.SetNodeConnectMode(ctx, nodeID, old, node.IsActive); err != nil {
			log.WithError(err).Error("[SwitchConnectMode] Couldn't record the old mode again")
		}
		u.relink(nodeID)
		return &AgentMove{Step: "connect", Message: msg}, nil
	}

	switched := *node
	switched.ConnectMode = newMode
	back, err := u.awaitMode(ctx, &switched)
	if err != nil {
		log.WithError(err).Warn("[SwitchConnectMode] The agent didn't come back under the new mode")
		return goBack(fmt.Sprintf("The agent didn't connect back, so it's returning to %s mode by itself.", old))
	}
	defer back.Close()
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	resp, err = back.SwitchConnectMode(cctx, &pb.SwitchConnectModeRequest{Confirm: true})
	cancel()
	if err != nil || !resp.Success {
		return goBack(fmt.Sprintf("The agent connected but the new mode couldn't be confirmed, so it's returning to %s mode by itself.", old))
	}
	log.Info("[SwitchConnectMode] Switched")
	return &AgentMove{Moved: true, Message: fmt.Sprintf("Now in %s mode.", newMode)}, nil
}

// awaitMode waits for the agent to come back under node's (new) mode.
func (u *nodeUsecase) awaitMode(ctx context.Context, node *domain.Node) (agent.NodeClient, error) {
	if node.ConnectMode == domain.ConnectModeDirect {
		return u.reachAgent(ctx, node, modeWaitFor)
	}
	deadline := time.Now().Add(modeWaitFor)
	for {
		if u.reverseRegistry != nil && u.reverseRegistry.GetByNodeID(node.ID) != nil {
			return u.moveClient(node)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no reverse connection within %s", modeWaitFor)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// validAddress is an IP or a host name of letters, digits, dots and dashes.
func validAddress(a string) bool {
	if net.ParseIP(a) != nil {
		return true
	}
	if a == "" || len(a) > 253 || strings.HasPrefix(a, ".") || strings.HasSuffix(a, ".") || strings.HasPrefix(a, "-") {
		return false
	}
	for _, r := range a {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// MoveNodeAddress points the panel at a node's new address. A direct node
// must answer there as itself first (checked against the certificate it
// already has), then gets a certificate for the new address too, and only
// then does the panel switch. Users' links follow from the next refresh.
func (u *nodeUsecase) MoveNodeAddress(ctx context.Context, nodeID uint, address string) (*AgentMove, error) {
	address = strings.TrimSpace(address)
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	switch {
	case node.IsLocal():
		return nil, ErrLocalNode
	case node.IsLite():
		return nil, ErrLiteNode
	case !validAddress(address):
		return nil, badMove("Use an IP address or a host name.")
	case strings.EqualFold(address, node.IP):
		return nil, badMove("The node already uses this address.")
	case node.IsStealth && node.ConnectMode == domain.ConnectModeDirect:
		return nil, ErrNodeStealth
	}
	old := node.IP
	done := &AgentMove{Moved: true, Message: fmt.Sprintf("Moved to %s. Users get the new links when their app refreshes.", address)}

	// a reverse agent dials the panel: the address only goes into users' links
	if node.ConnectMode != domain.ConnectModeDirect {
		if err := u.nodeRepo.SetNodeIP(ctx, nodeID, address); err != nil {
			return nil, fmt.Errorf("couldn't save the new address: %w", err)
		}
		return done, nil
	}

	lock, _ := u.certRotateLocks.LoadOrStore(nodeID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	moved := *node
	moved.IP = address
	client, err := u.reachAgentAs(ctx, &moved, old, portWaitFor)
	if err != nil {
		return &AgentMove{Step: "reach", Message: fmt.Sprintf("Nothing answered at %s as this node, so it stays on %s.", address, old)}, nil
	}
	defer client.Close()

	install := u.moveInstallCert
	if install == nil {
		install = func(ctx context.Context, n *domain.Node, c agent.NodeClient, addrs []string) error {
			_, err := u.installAgentCert(ctx, n, c, addrs)
			return err
		}
	}
	if err := install(ctx, node, client, []string{address, old}); err != nil {
		return &AgentMove{Step: "certificate", Message: fmt.Sprintf("Couldn't give the node a certificate for %s, so it stays on %s: %v", address, old, err)}, nil
	}
	if err := u.nodeRepo.SetNodeIP(ctx, nodeID, address); err != nil {
		return nil, fmt.Errorf("couldn't save the new address: %w", err)
	}
	u.relink(nodeID)
	return done, nil
}

// reachAgentAs waits for node's agent at its (new) address, checking its
// certificate against name, the address it was issued for.
func (u *nodeUsecase) reachAgentAs(ctx context.Context, node *domain.Node, name string, within time.Duration) (agent.NodeClient, error) {
	if u.moveDial != nil {
		return u.reachAgent(ctx, node, within)
	}
	target := net.JoinHostPort(node.IP, strconv.Itoa(node.AgentPort))
	deadline := time.Now().Add(within)
	for {
		c, err := u.dialDirectAgentAs(node, target, name)
		if err == nil {
			hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err = c.HealthCheck(hctx)
			cancel()
			if err == nil {
				return c, nil
			}
			c.Close()
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
