// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fakeagent is a stand-in for a node's agent that a test tells what to
// answer. It serves the NodeAgent API behind the same mutual TLS the hub
// dials with, with certificates from the plugin's own CA, so a test runs the
// hub's real connection code against a peer it controls: one that restarts
// when it is updated, answers like an agent that predates a call, or runs the
// full profile.
//
// Only tests import it, and it links nothing heavier than gRPC. The real
// agent is run by the tests in package agentcheck.
package fakeagent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// Agent answers the calls the hub makes while it sets a node up and keeps it
// going. Anything else is Unimplemented. Its fields are what it answers; change
// them with Configure while it serves.
type Agent struct {
	pb.UnimplementedNodeAgentServer

	mu sync.Mutex

	// What GetVersion says.
	AgentVersion string // "v1.3.0"
	XrayLine     string // the first line of `xray version`
	Arch         string // "amd64"

	// Full is an agent that runs every call, as one older than the managed
	// profile does. Otherwise GetSSHStatus is refused the way the profile does.
	Full bool
	// NoPushXray makes PushXrayBinary Unimplemented, as on an agent that
	// predates it.
	NoPushXray bool

	// OnSSHStatus answers GetSSHStatus, in place of what Full says.
	OnSSHStatus func() (*pb.SSHStatus, error)
	// OnPushXray decides what a push answers, given its header and the unpacked
	// build. Nil accepts it, and the agent reports that version from then on.
	OnPushXray func(h *pb.XrayBinaryHeader, build []byte) *pb.CommandResponse
	// OnSelfUpdate decides what an update answers. Nil accepts it and restarts
	// the agent a moment later as the version it was sent.
	OnSelfUpdate func(r *pb.UpdateRequest) *pb.UpdateResponse
	// OnValidate answers ValidateConfig. Nil says valid.
	OnValidate func(config string) *pb.ValidationResult
	// OnConfig answers PushConfig. Nil accepts it.
	OnConfig func(config string) *pb.CommandResponse
	// OnStatus answers GetStatus. Nil says Xray runs.
	OnStatus func() *pb.NodeStatus
	// OnUsers answers ListUsers. Nil says there are none.
	OnUsers func(tag string) (*pb.UserList, error)
	// NoProbe makes ProbeRealityDest Unimplemented, as on an agent that
	// predates it.
	NoProbe bool
	// OnProbe answers ProbeRealityDest. Nil fails every name.
	OnProbe func(r *pb.RealityDestProbeRequest) *pb.RealityDestProbeResponse

	// restart is how long the agent stays down when it restarts.
	restartDown time.Duration

	calls  []string
	peers  []string
	pushes []Push

	t     testing.TB
	addr  string
	creds credentials.TransportCredentials
	gs    *grpc.Server
	done  chan struct{}
}

// Push is one build the agent was sent.
type Push struct {
	Header   *pb.XrayBinaryHeader
	Chunks   int    // the data messages
	MaxChunk int    // the largest of them, in bytes
	Build    []byte // what came out of them, unpacked when the header said gzip
}

// Pushes are the builds sent so far, in order.
func (a *Agent) Pushes() []Push {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Push(nil), a.pushes...)
}

// Start serves an agent on a loopback port, with a certificate for host (an IP
// address or a name) issued by ca as the node's. It stops with the test.
func Start(t testing.TB, ca *pki.CA, host string) *Agent {
	t.Helper()
	cert, err := ca.IssueNodeCert(1, "fake", host, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(cert.CertPEM, cert.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate())
	a := &Agent{
		AgentVersion: "v1.3.0",
		XrayLine:     "Xray 26.10.10 (Xray, Penetrates Everything.) fake (go1.27 linux/amd64)",
		Arch:         "amd64",
		restartDown:  300 * time.Millisecond,
		t:            t,
		creds: credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{pair},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pool,
			MinVersion:   tls.VersionTLS12,
		}),
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	a.addr = l.Addr().String()
	a.serve(l)
	t.Cleanup(a.Stop)
	return a
}

func (a *Agent) serve(l net.Listener) {
	gs := grpc.NewServer(
		grpc.Creds(a.creds),
		grpc.MaxRecvMsgSize(100<<20), // what the agent allows
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			a.note(ctx, info.FullMethod)
			return h(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			a.note(ss.Context(), info.FullMethod)
			return h(srv, ss)
		}),
	)
	pb.RegisterNodeAgentServer(gs, a)
	done := make(chan struct{})
	a.mu.Lock()
	a.gs, a.done = gs, done
	a.mu.Unlock()
	go func() {
		defer close(done)
		_ = gs.Serve(l)
	}()
}

func (a *Agent) note(ctx context.Context, method string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, method[len(pb.NodeAgent_ServiceDesc.ServiceName)+2:])
	if p, ok := peer.FromContext(ctx); ok {
		a.peers = append(a.peers, p.Addr.String())
	}
}

// Addr is where the agent listens, as host:port.
func (a *Agent) Addr() string { return a.addr }

// Port is the port it listens on.
func (a *Agent) Port() int {
	_, p, _ := net.SplitHostPort(a.addr)
	n, _ := strconv.Atoi(p)
	return n
}

// Configure changes what the agent answers, while it serves.
func (a *Agent) Configure(fn func(*Agent)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a)
}

// Calls is the names of the calls made so far, in order.
func (a *Agent) Calls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

// Peers is where each call came from, as the agent saw it.
func (a *Agent) Peers() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.peers...)
}

// Stop ends the agent and everything connected to it.
func (a *Agent) Stop() {
	a.mu.Lock()
	gs, done := a.gs, a.done
	a.mu.Unlock()
	if gs != nil {
		gs.Stop()
		<-done
	}
}

// Restart takes the agent down and brings it back, on the same address, as
// version, the way an updated agent comes back.
func (a *Agent) Restart(version string, down time.Duration) {
	a.Stop()
	a.mu.Lock()
	a.AgentVersion = version
	a.mu.Unlock()
	time.Sleep(down)
	l, err := net.Listen("tcp", a.addr)
	if err != nil {
		a.t.Errorf("fakeagent: can't listen again on %s: %v", a.addr, err)
		return
	}
	a.serve(l)
}

func (a *Agent) GetVersion(context.Context, *pb.Empty) (*pb.VersionInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &pb.VersionInfo{AgentVersion: a.AgentVersion, XrayVersion: a.XrayLine, Arch: a.Arch, Os: "linux", GoVersion: "go1.27"}, nil
}

func (a *Agent) GetSSHStatus(context.Context, *pb.Empty) (*pb.SSHStatus, error) {
	a.mu.Lock()
	hook, full := a.OnSSHStatus, a.Full
	a.mu.Unlock()
	if hook != nil {
		return hook()
	}
	if full {
		return &pb.SSHStatus{Enabled: true, Port: 22, IsActive: true}, nil
	}
	return nil, status.Error(codes.PermissionDenied, "this agent runs the managed profile: GetSSHStatus is not available")
}

func (a *Agent) PushXrayBinary(stream pb.NodeAgent_PushXrayBinaryServer) error {
	a.mu.Lock()
	no, hook := a.NoPushXray, a.OnPushXray
	a.mu.Unlock()
	if no {
		return status.Error(codes.Unimplemented, "method PushXrayBinary not implemented")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	h := first.GetHeader()
	if h == nil {
		return status.Error(codes.InvalidArgument, "the first message must be the header")
	}
	var data bytes.Buffer
	var chunks, biggest int
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(m.GetData()) > 1<<20 {
			return status.Error(codes.InvalidArgument, "a data message is over 1 MiB")
		}
		chunks++
		biggest = max(biggest, len(m.GetData()))
		data.Write(m.GetData())
	}
	build := data.Bytes()
	if h.Gzip {
		zr, err := gzip.NewReader(bytes.NewReader(build))
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "the data is not a complete gzip stream: %v", err)
		}
		if build, err = io.ReadAll(zr); err != nil {
			return status.Errorf(codes.InvalidArgument, "the data is not a complete gzip stream: %v", err)
		}
	}
	a.mu.Lock()
	a.pushes = append(a.pushes, Push{Header: h, Chunks: chunks, MaxChunk: biggest, Build: build})
	a.mu.Unlock()
	if hook != nil {
		return stream.SendAndClose(hook(h, build))
	}
	a.mu.Lock()
	a.XrayLine = "Xray " + h.Version + " (Xray, Penetrates Everything.) fake (go1.27 linux/amd64)"
	a.mu.Unlock()
	return stream.SendAndClose(&pb.CommandResponse{Success: true, Message: "Xray updated to " + h.Version + " from hub"})
}

func (a *Agent) SelfUpdate(_ context.Context, r *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	a.mu.Lock()
	hook, old, down := a.OnSelfUpdate, a.AgentVersion, a.restartDown
	a.mu.Unlock()
	if hook != nil {
		return hook(r), nil
	}
	if r.RestartAfter {
		go func() {
			time.Sleep(200 * time.Millisecond) // after the answer is out
			a.Restart(r.Version, down)
		}()
	}
	return &pb.UpdateResponse{Success: true, Message: "Update successful, restarting...", OldVersion: old, NewVersion: r.Version}, nil
}

func (a *Agent) ValidateConfig(_ context.Context, r *pb.ConfigPayload) (*pb.ValidationResult, error) {
	a.mu.Lock()
	hook := a.OnValidate
	a.mu.Unlock()
	if hook != nil {
		return hook(r.JsonContent), nil
	}
	return &pb.ValidationResult{Valid: true}, nil
}

func (a *Agent) PushConfig(_ context.Context, r *pb.ConfigPayload) (*pb.CommandResponse, error) {
	a.mu.Lock()
	hook := a.OnConfig
	a.mu.Unlock()
	if hook != nil {
		return hook(r.JsonContent), nil
	}
	return &pb.CommandResponse{Success: true, Message: "Configuration updated and xray restarted"}, nil
}

func (a *Agent) GetStatus(context.Context, *pb.Empty) (*pb.NodeStatus, error) {
	a.mu.Lock()
	hook, line := a.OnStatus, a.XrayLine
	a.mu.Unlock()
	if hook != nil {
		return hook(), nil
	}
	return &pb.NodeStatus{XrayRunning: true, XrayVersion: line}, nil
}

func (a *Agent) StartXray(context.Context, *pb.Empty) (*pb.CommandResponse, error) {
	return &pb.CommandResponse{Success: true, Message: "Xray started successfully"}, nil
}

func (a *Agent) ListUsers(_ context.Context, r *pb.InboundSelector) (*pb.UserList, error) {
	a.mu.Lock()
	hook := a.OnUsers
	a.mu.Unlock()
	if hook != nil {
		return hook(r.InboundTag)
	}
	return &pb.UserList{}, nil
}

func (a *Agent) ProbeRealityDest(_ context.Context, r *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
	a.mu.Lock()
	hook, no := a.OnProbe, a.NoProbe
	a.mu.Unlock()
	if no {
		return nil, status.Error(codes.Unimplemented, "method ProbeRealityDest not implemented")
	}
	if hook != nil {
		return hook(r), nil
	}
	resp := &pb.RealityDestProbeResponse{Dest: r.Dest}
	for _, n := range r.ServerNames {
		resp.Checks = append(resp.Checks, &pb.RealityDestCheck{ServerName: n, Reason: "dial_failed", Message: "Could not connect to " + r.Dest + ": connection refused"})
	}
	return resp, nil
}
