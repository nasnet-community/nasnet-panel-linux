package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// portAgent plays an agent with one port open, and another while it moves.
type portAgent struct {
	mu    sync.Mutex
	open  map[int]bool
	calls []string
	busy  map[int]bool
	// blocked ports open on the node but nothing from the panel gets there
	blocked map[int]bool
}

func (a *portAgent) client(port int) agent.NodeClient { return &portClient{a: a, port: port} }

type portClient struct {
	agent.NodeClient
	a    *portAgent
	port int
}

func (c *portClient) Close() error { return nil }

func (c *portClient) HealthCheck(context.Context) (*agent.HealthResult, error) {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	if !c.a.open[c.port] || c.a.blocked[c.port] {
		return nil, errors.New("connection refused")
	}
	return &agent.HealthResult{}, nil
}

func (c *portClient) MoveListenPort(_ context.Context, req *pb.MoveListenPortRequest) (*pb.CommandResponse, error) {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	switch {
	case req.Finish:
		c.a.calls = append(c.a.calls, "finish")
		for p := range c.a.open {
			if p != c.port {
				delete(c.a.open, p)
			}
		}
	case req.Abort:
		c.a.calls = append(c.a.calls, "abort")
		for p := range c.a.open {
			if p != c.port {
				delete(c.a.open, p)
			}
		}
	default:
		c.a.calls = append(c.a.calls, "open")
		if c.a.busy[int(req.Port)] {
			return &pb.CommandResponse{ErrorCode: "PORT_BUSY", Message: "port is in use"}, nil
		}
		c.a.open[int(req.Port)] = true
	}
	return &pb.CommandResponse{Success: true}, nil
}

type movingRepo struct {
	*fakeNodeRepo
	ports []int
	ips   []string
}

func (r *movingRepo) SetNodeAgentPort(_ context.Context, id uint, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[id].AgentPort = port
	r.ports = append(r.ports, port)
	return nil
}

func movingNode() *domain.Node {
	return &domain.Node{ID: 7, Name: "fra-1", IP: "203.0.113.24", ConnectMode: domain.ConnectModeDirect, AgentPort: 9090, APIPort: 10085, IsOnline: true}
}

func moving(t *testing.T, a *portAgent) (*nodeUsecase, *movingRepo) {
	t.Helper()
	old := portWaitFor
	portWaitFor = 50 * time.Millisecond
	t.Cleanup(func() { portWaitFor = old })
	repo := &movingRepo{fakeNodeRepo: newFakeNodeRepo(movingNode())}
	u := &nodeUsecase{nodeRepo: repo}
	u.moveDial = func(n *domain.Node) (agent.NodeClient, error) { return a.client(n.AgentPort), nil }
	return u, repo
}

func TestTheAgentPortMovesOnlyOnceThePanelIsIn(t *testing.T) {
	a := &portAgent{open: map[int]bool{9090: true}}
	u, repo := moving(t, a)
	res, err := u.MoveAgentPort(context.Background(), 7, 9443)
	if err != nil || !res.Moved {
		t.Fatalf("move: %+v %v", res, err)
	}
	if strings.Join(a.calls, ",") != "open,finish" {
		t.Errorf("agent saw %v", a.calls)
	}
	if !a.open[9443] || a.open[9090] {
		t.Errorf("open ports %v", a.open)
	}
	if len(repo.ports) != 1 || repo.ports[0] != 9443 {
		t.Errorf("saved %v", repo.ports)
	}
}

func TestABlockedPortLeavesTheAgentWhereItWas(t *testing.T) {
	a := &portAgent{open: map[int]bool{9090: true}, blocked: map[int]bool{9443: true}}
	u, repo := moving(t, a)
	res, err := u.MoveAgentPort(context.Background(), 7, 9443)
	if err != nil || res.Moved || res.Step != "switch" {
		t.Fatalf("move: %+v %v", res, err)
	}
	if !strings.Contains(res.Message, "stays on 9090") || !strings.Contains(res.Message, "firewall") {
		t.Errorf("message %q", res.Message)
	}
	if strings.Join(a.calls, ",") != "open,abort" || !a.open[9090] || a.open[9443] {
		t.Errorf("agent saw %v, open %v", a.calls, a.open)
	}
	if len(repo.ports) != 0 {
		t.Errorf("the record must keep the old port, saved %v", repo.ports)
	}
}

func TestAPortTheNodeRefusesChangesNothing(t *testing.T) {
	a := &portAgent{open: map[int]bool{9090: true}, busy: map[int]bool{9443: true}}
	u, repo := moving(t, a)
	res, err := u.MoveAgentPort(context.Background(), 7, 9443)
	if err != nil || res.Moved || res.Step != "open" || res.Message != "Port is in use." {
		t.Fatalf("move: %+v %v", res, err)
	}
	if len(repo.ports) != 0 {
		t.Errorf("saved %v", repo.ports)
	}
}

func TestAgentPortMovesThePanelRefusesUpFront(t *testing.T) {
	for name, c := range map[string]struct {
		port int
		edit func(*domain.Node)
	}{
		"same port":    {port: 9090},
		"api port":     {port: 10085},
		"out of range": {port: 70000},
		"offline":      {port: 9443, edit: func(n *domain.Node) { n.IsOnline = false }},
		"reverse":      {port: 9443, edit: func(n *domain.Node) { n.ConnectMode = domain.ConnectModeReverse }},
	} {
		t.Run(name, func(t *testing.T) {
			a := &portAgent{open: map[int]bool{9090: true}}
			u, repo := moving(t, a)
			if c.edit != nil {
				c.edit(repo.nodes[7])
			}
			_, err := u.MoveAgentPort(context.Background(), 7, c.port)
			var bad *BadMoveError
			if !errors.As(err, &bad) {
				t.Fatalf("want a refusal, got %v", err)
			}
			if len(a.calls) != 0 {
				t.Errorf("the agent was asked: %v", a.calls)
			}
		})
	}
}

// modeAgent plays an agent switching from reverse to direct.
type modeAgent struct {
	portAgent
	switched, confirmed bool
	comesBack           bool
}

type modeClient struct {
	portClient
	m *modeAgent
}

func (c *modeClient) SwitchConnectMode(_ context.Context, req *pb.SwitchConnectModeRequest) (*pb.CommandResponse, error) {
	c.m.mu.Lock()
	defer c.m.mu.Unlock()
	if req.Confirm {
		c.m.confirmed = true
		return &pb.CommandResponse{Success: true}, nil
	}
	if req.Mode != "direct" || req.ListenPort != 9090 {
		return &pb.CommandResponse{ErrorCode: "BAD_MODE", Message: "unexpected request"}, nil
	}
	c.m.switched = true
	if c.m.comesBack {
		c.m.open[9090] = true
	}
	return &pb.CommandResponse{Success: true}, nil
}

func (r *movingRepo) SetNodeConnectMode(_ context.Context, id uint, mode string, _ bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[id].ConnectMode = mode
	return nil
}

func switching(t *testing.T, m *modeAgent) (*nodeUsecase, *movingRepo) {
	t.Helper()
	old := modeWaitFor
	modeWaitFor = 50 * time.Millisecond
	t.Cleanup(func() { modeWaitFor = old })
	n := movingNode()
	n.ConnectMode = domain.ConnectModeReverse
	repo := &movingRepo{fakeNodeRepo: newFakeNodeRepo(n)}
	u := &nodeUsecase{nodeRepo: repo}
	u.moveDial = func(n *domain.Node) (agent.NodeClient, error) {
		return &modeClient{portClient: portClient{a: &m.portAgent, port: n.AgentPort}, m: m}, nil
	}
	return u, repo
}

func TestAModeSwitchIsConfirmedOnceTheAgentIsBack(t *testing.T) {
	m := &modeAgent{portAgent: portAgent{open: map[int]bool{}}, comesBack: true}
	u, repo := switching(t, m)
	res, err := u.SwitchConnectMode(context.Background(), 7, domain.ConnectModeDirect)
	if err != nil || !res.Moved {
		t.Fatalf("switch: %+v %v", res, err)
	}
	if !m.switched || !m.confirmed {
		t.Errorf("switched %v, confirmed %v", m.switched, m.confirmed)
	}
	if repo.nodes[7].ConnectMode != domain.ConnectModeDirect {
		t.Errorf("recorded %q", repo.nodes[7].ConnectMode)
	}
}

func TestAnAgentThatNeverComesBackKeepsTheOldMode(t *testing.T) {
	m := &modeAgent{portAgent: portAgent{open: map[int]bool{}}}
	u, repo := switching(t, m)
	res, err := u.SwitchConnectMode(context.Background(), 7, domain.ConnectModeDirect)
	if err != nil || res.Moved || res.Step != "connect" {
		t.Fatalf("switch: %+v %v", res, err)
	}
	if !strings.Contains(res.Message, "returning to reverse mode by itself") {
		t.Errorf("message %q", res.Message)
	}
	if m.confirmed {
		t.Error("nothing should be confirmed")
	}
	if repo.nodes[7].ConnectMode != domain.ConnectModeReverse {
		t.Errorf("the record must go back to reverse, got %q", repo.nodes[7].ConnectMode)
	}
}

func (r *movingRepo) SetNodeIP(_ context.Context, id uint, ip string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[id].IP = ip
	r.ips = append(r.ips, ip)
	return nil
}

// addressing plays a node that answers at the addresses in at.
func addressing(t *testing.T, at map[string]bool, certErr error) (*nodeUsecase, *movingRepo, *[][]string) {
	t.Helper()
	old := portWaitFor
	portWaitFor = 50 * time.Millisecond
	t.Cleanup(func() { portWaitFor = old })
	repo := &movingRepo{fakeNodeRepo: newFakeNodeRepo(movingNode())}
	u := &nodeUsecase{nodeRepo: repo}
	a := &portAgent{open: map[int]bool{}}
	u.moveDial = func(n *domain.Node) (agent.NodeClient, error) {
		a.mu.Lock()
		a.open[9090] = at[n.IP]
		a.mu.Unlock()
		return a.client(9090), nil
	}
	var certs [][]string
	u.moveInstallCert = func(_ context.Context, _ *domain.Node, _ agent.NodeClient, addrs []string) error {
		certs = append(certs, addrs)
		return certErr
	}
	return u, repo, &certs
}

func TestTheAddressMovesOnceTheNodeAnswersThere(t *testing.T) {
	u, repo, certs := addressing(t, map[string]bool{"198.51.100.9": true}, nil)
	res, err := u.MoveNodeAddress(context.Background(), 7, " 198.51.100.9 ")
	if err != nil || !res.Moved {
		t.Fatalf("move: %+v %v", res, err)
	}
	if len(*certs) != 1 || strings.Join((*certs)[0], ",") != "198.51.100.9,203.0.113.24" {
		t.Errorf("certificate for %v", *certs)
	}
	if repo.nodes[7].IP != "198.51.100.9" {
		t.Errorf("saved %q", repo.nodes[7].IP)
	}
}

func TestAnAddressNothingAnswersAtChangesNothing(t *testing.T) {
	u, repo, certs := addressing(t, map[string]bool{}, nil)
	res, err := u.MoveNodeAddress(context.Background(), 7, "vps.example.com")
	if err != nil || res.Moved || res.Step != "reach" || !strings.Contains(res.Message, "stays on 203.0.113.24") {
		t.Fatalf("move: %+v %v", res, err)
	}
	if len(*certs) != 0 || len(repo.ips) != 0 {
		t.Errorf("nothing should change: certs %v, saved %v", *certs, repo.ips)
	}
}

func TestAnAddressMoveStopsWhenTheCertificateFails(t *testing.T) {
	u, repo, _ := addressing(t, map[string]bool{"198.51.100.9": true}, errors.New("agent too old"))
	res, err := u.MoveNodeAddress(context.Background(), 7, "198.51.100.9")
	if err != nil || res.Moved || res.Step != "certificate" {
		t.Fatalf("move: %+v %v", res, err)
	}
	if len(repo.ips) != 0 {
		t.Errorf("saved %v", repo.ips)
	}
}

func TestAReverseNodeTakesItsNewAddressStraightAway(t *testing.T) {
	u, repo, certs := addressing(t, map[string]bool{}, nil)
	repo.nodes[7].ConnectMode = domain.ConnectModeReverse
	res, err := u.MoveNodeAddress(context.Background(), 7, "vps.example.com")
	if err != nil || !res.Moved {
		t.Fatalf("move: %+v %v", res, err)
	}
	if repo.nodes[7].IP != "vps.example.com" || len(*certs) != 0 {
		t.Errorf("saved %q, certs %v", repo.nodes[7].IP, *certs)
	}
}

func TestAddressesThePanelRefuses(t *testing.T) {
	for _, a := range []string{"", "203.0.113.24", "bad host", "-x.example", "a/b"} {
		u, _, _ := addressing(t, map[string]bool{}, nil)
		var bad *BadMoveError
		if _, err := u.MoveNodeAddress(context.Background(), 7, a); !errors.As(err, &bad) {
			t.Errorf("%q: want a refusal, got %v", a, err)
		}
	}
}
