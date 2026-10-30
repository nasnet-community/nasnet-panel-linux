package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// pushSpy records the messages the agent receives on a push.
type pushSpy struct {
	mu       sync.Mutex
	headerAt int // index of the header among the messages, -1 until one arrives
	messages int
	dataMsgs int
	maxData  int
}

func (p *pushSpy) seen(c *pb.XrayBinaryChunk) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c.GetHeader() != nil && p.headerAt < 0 {
		p.headerAt = p.messages
	}
	if c.GetHeader() == nil {
		p.dataMsgs++
		p.maxData = max(p.maxData, len(c.GetData()))
	}
	p.messages++
}

func (p *pushSpy) snapshot() (headerAt, dataMsgs, maxData int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.headerAt, p.dataMsgs, p.maxData
}

type spyStream struct {
	grpc.ServerStream
	spy *pushSpy
}

func (s *spyStream) RecvMsg(m any) error {
	err := s.ServerStream.RecvMsg(m)
	if c, ok := m.(*pb.XrayBinaryChunk); ok && err == nil {
		s.spy.seen(c)
	}
	return err
}

// hubClient serves the node's agent on a loopback port behind mutual TLS and
// dials it with agent.NewClient, the constructor the hub uses.
func (n *xrayNode) hubClient(t *testing.T) (*agent.Client, *pushSpy) {
	t.Helper()
	ca, err := certs.GenerateCA(nil)
	if err != nil {
		t.Fatal(err)
	}
	agentCert, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID: 1, NodeName: "e2e", IPAddresses: []string{"127.0.0.1"}, ValidMonths: 1,
	}, ca.Certificate, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	hubCert, err := certs.GenerateMasterCert(nil, ca.Certificate, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(agentCert.Certificate, agentCert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.Certificate)

	spy := &pushSpy{headerAt: -1}
	gs := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{pair},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pool,
			MinVersion:   tls.VersionTLS12,
		})),
		grpc.MaxRecvMsgSize(100*mib), // what Start sets
		grpc.StreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			if info.FullMethod == pb.NodeAgent_PushXrayBinary_FullMethodName {
				ss = &spyStream{ServerStream: ss, spy: spy}
			}
			return h(srv, ss)
		}),
	)
	pb.RegisterNodeAgentServer(gs, n.s)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	client, err := agent.NewClient(lis.Addr().String(), &agent.TLSConfig{
		CACert:     string(ca.Certificate),
		ClientCert: string(hubCert.Certificate),
		ClientKey:  string(hubCert.PrivateKey),
	}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, spy
}

func TestAgentClientAndAgentAgreeOnTheChunkSize(t *testing.T) {
	if agent.XrayPushChunkSize != maxXrayChunk {
		t.Fatalf("the client cuts at %d, the agent takes %d", agent.XrayPushChunkSize, maxXrayChunk)
	}
}

func TestAgentClientPushesABuildToTheAgent(t *testing.T) {
	for _, gzipped := range []bool{false, true} {
		n := newXrayNode(t, "26.10.10")
		n.start(t)
		oldPID := n.s.xrayMgr.PID()
		client, spy := n.hubClient(t)
		next := bigBuild(t, "26.11.1", 4*mib+mib/2+7)
		body := next
		if gzipped {
			body = gz(t, next)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		err := client.PushXrayBinary(ctx, "26.11.1", sum(next), bytes.NewReader(body), true, gzipped)
		cancel()

		if err != nil {
			t.Fatalf("gzip=%v: %v", gzipped, err)
		}
		if !bytes.Equal(n.installed(t), next) {
			t.Fatalf("gzip=%v: the installed build is not what was pushed", gzipped)
		}
		if !n.s.xrayMgr.IsRunning() || n.s.xrayMgr.PID() == oldPID {
			t.Fatalf("gzip=%v: xray was not restarted", gzipped)
		}
		headerAt, dataMsgs, maxData := spy.snapshot()
		if headerAt != 0 {
			t.Fatalf("gzip=%v: the header came as message %d", gzipped, headerAt)
		}
		if dataMsgs < 3 || maxData < mib {
			t.Fatalf("gzip=%v: %d data messages, the biggest %d bytes: not a build cut into full chunks", gzipped, dataMsgs, maxData)
		}
		if maxData > mib {
			t.Fatalf("gzip=%v: a data message of %d bytes is over 1 MiB", gzipped, maxData)
		}
	}
}

func TestAgentClientPushIsRefusedWithTheAgentsStatus(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old, pid := n.installed(t), n.s.xrayMgr.PID()
	client, _ := n.hubClient(t)
	next := build(t, "26.11.1")

	err := client.PushXrayBinary(context.Background(), "26.11.1", sum([]byte("not this")), bytes.NewReader(next), true, false)

	wantCode(t, err, codes.InvalidArgument, "checksum mismatch")
	if agent.IsUnimplemented(err) || status.Code(err) == codes.Unknown {
		t.Fatalf("the agent's status was lost: %v", err)
	}
	n.untouched(t, old, pid)
}

func TestAgentClientPushRollsBackABuildThatReportsNoVersion(t *testing.T) {
	n := newXrayNode(t, "26.10.10")
	n.start(t)
	old := n.installed(t)
	client, _ := n.hubClient(t)
	next := build(t, "")

	err := client.PushXrayBinary(context.Background(), "26.11.1", sum(next), bytes.NewReader(next), true, false)

	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("rolled back")) {
		t.Fatalf("error = %v", err)
	}
	if !bytes.Equal(n.installed(t), old) || !n.s.xrayMgr.IsRunning() {
		t.Fatal("the old build is not back and running")
	}
}
