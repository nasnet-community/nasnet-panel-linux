package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/middleware"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// callLog records which calls got past the profile check: it sits inside it,
// so a refused call never shows up here.
type callLog struct {
	mu    sync.Mutex
	names []string
}

func (l *callLog) add(fullMethod string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, fullMethod[strings.LastIndex(fullMethod, "/")+1:])
}

func (l *callLog) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := slices.Clone(l.names)
	slices.Sort(out)
	return out
}

// onlyTheSafeOnes is what the sweeps register in place of a whole Server: one
// harmless call is real and the rest answer Unimplemented. A call that got
// past the profile check is then told from a refused one by its code, and
// nothing destructive can run if the check ever fails.
type onlyTheSafeOnes struct {
	pb.UnimplementedNodeAgentServer
	real *Server
}

func (o onlyTheSafeOnes) GetVersion(ctx context.Context, r *pb.Empty) (*pb.VersionInfo, error) {
	return o.real.GetVersion(ctx, r)
}

// liveAgent is a Server behind the options Start serves with, on a loopback
// port, and the two ways to reach it: the hub's own client, and a bare
// connection for calling by name.
type liveAgent struct {
	dir        string
	srv        *Server
	configFile string
	hub        *agent.Client
	conn       *grpc.ClientConn
	reached    *callLog
}

// startLiveAgent loads an agent config whose first line is profileLine the way
// the agent's main does, builds the Server from it, and serves it over mutual
// TLS. impl picks what answers the calls that get past the profile.
func startLiveAgent(t *testing.T, profileLine string, impl func(*Server) pb.NodeAgentServer) *liveAgent {
	t.Helper()
	dir := t.TempDir()

	ca, err := certs.GenerateCA(nil)
	if err != nil {
		t.Fatal(err)
	}
	agentCert, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID: 1, NodeName: "profile-e2e", IPAddresses: []string{"127.0.0.1"}, ValidMonths: 1,
	}, ca.Certificate, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	hubCert, err := certs.GenerateMasterCert(nil, ca.Certificate, ca.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"ca.crt": ca.Certificate, "server.crt": agentCert.Certificate, "server.key": agentCert.PrivateKey,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "xray"), 0o755); err != nil {
		t.Fatal(err)
	}

	xrayBin := fakeXray(t, dir, "xray-bin", "26.10.10")
	configFile := filepath.Join(dir, "agent.yaml")
	yaml := fmt.Sprintf(`%s
listen_addr: "127.0.0.1:0"
xray:
  binary_path: %q
  config_path: %q
  access_log_path: ""
traffic:
  buffer_enabled: false
process:
  service_mode: false
  watchdog_enabled: false
cores:
  data_dir: %q
tls:
  enabled: true
  ca_cert: %q
  server_cert: %q
  server_key: %q
`, profileLine, xrayBin, filepath.Join(dir, "xray", "config.json"), filepath.Join(dir, "cores"),
		filepath.Join(dir, "ca.crt"), filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"))
	if err := os.WriteFile(configFile, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	reached := &callLog{}
	opts, err := srv.serverOptions()
	if err != nil {
		t.Fatal(err)
	}
	opts = append(opts,
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			reached.add(info.FullMethod)
			return h(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			reached.add(info.FullMethod)
			return h(srv, ss)
		}),
	)
	gs := grpc.NewServer(opts...)
	pb.RegisterNodeAgentServer(gs, impl(srv))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	hub, err := agent.NewClient(lis.Addr().String(), &agent.TLSConfig{
		CACert: string(ca.Certificate), ClientCert: string(hubCert.Certificate), ClientKey: string(hubCert.PrivateKey),
	}, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	pair, err := tls.X509KeyPair(hubCert.Certificate, hubCert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca.Certificate)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair}, RootCAs: roots, MinVersion: tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &liveAgent{dir: dir, srv: srv, configFile: configFile, hub: hub, conn: conn, reached: reached}
}

// callByName makes the call the proto names, with an empty request, and
// returns how it ended. For a stream that is its first receive, which is where
// a refusal arrives.
func (a *liveAgent) callByName(t *testing.T, name string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	full := "/" + pb.NodeAgent_ServiceDesc.ServiceName + "/" + name

	for _, st := range pb.NodeAgent_ServiceDesc.Streams {
		if st.StreamName != name {
			continue
		}
		stream, err := a.conn.NewStream(ctx, &grpc.StreamDesc{
			StreamName: name, ClientStreams: st.ClientStreams, ServerStreams: st.ServerStreams,
		}, full)
		if err != nil {
			return err
		}
		if err := stream.SendMsg(&pb.Empty{}); err != nil && err != io.EOF {
			return err
		}
		_ = stream.CloseSend()
		return stream.RecvMsg(&pb.Empty{})
	}
	return a.conn.Invoke(ctx, full, &pb.Empty{}, &pb.Empty{})
}

func wantManagedRefusal(t *testing.T, what string, err error) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "this agent runs the managed profile") {
		t.Errorf("%s: error = %v, want PermissionDenied from the managed profile", what, err)
	}
}

func allCalls(t *testing.T) []string {
	t.Helper()
	svc := pb.File_node_agent_proto.Services().ByName("NodeAgent")
	var names []string
	for i := 0; i < svc.Methods().Len(); i++ {
		names = append(names, string(svc.Methods().Get(i).Name()))
	}
	return names
}

// A server started with `profile: managed` in its config file, called over a
// real mutually authenticated connection by the hub's own client: one allowed
// call is answered, the ones that would give a shell, a file, a mode switch or
// an uninstall are refused with the profile's PermissionDenied, and the
// handlers never see them.
func TestManagedAgentOverARealConnection(t *testing.T) {
	// the spelling is on purpose: the profile reads without regard to case
	a := startLiveAgent(t, "profile: Managed", func(s *Server) pb.NodeAgentServer { return onlyTheSafeOnes{real: s} })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !a.srv.profile.Managed() {
		t.Fatalf("the server runs profile %q", a.srv.profile)
	}

	// allowed: answered by the real handler
	v, err := a.hub.GetVersion(ctx)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if !strings.Contains(v.XrayVersion, "26.10.10") {
		t.Fatalf("GetVersion answered %+v", v)
	}

	// denied: the calls the profile exists to keep out
	ranMarker := filepath.Join(a.dir, "command-ran")
	_, err = a.hub.ExecuteCommand(ctx, "touch '"+ranMarker+"'", 5)
	wantManagedRefusal(t, "ExecuteCommand", err)
	_, err = a.hub.WriteFile(ctx, "evil.txt", []byte("x"), 0o644)
	wantManagedRefusal(t, "WriteFile", err)
	wantManagedRefusal(t, "Uninstall", a.hub.Uninstall(ctx))
	_, err = a.hub.SwitchConnectMode(ctx, &pb.SwitchConnectModeRequest{Mode: "reverse", HubUrl: "hub.example:9091", NodeUuid: "n"})
	wantManagedRefusal(t, "SwitchConnectMode", err)
	_, err = a.hub.MoveListenPort(ctx, &pb.MoveListenPortRequest{Port: 9999})
	wantManagedRefusal(t, "MoveListenPort", err)
	wantManagedRefusal(t, "UpdateSSHConfig", a.hub.UpdateSSHConfig(ctx, true, 2222))
	wantManagedRefusal(t, "RestartSSH", a.hub.RestartSSH(ctx))
	_, err = a.hub.Wipe(ctx, &pb.NukeRequest{DryRun: true})
	wantManagedRefusal(t, "Wipe", err)

	// denied, and streaming: the refusal arrives on the first receive
	term, err := a.hub.OpenTerminal(ctx)
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	_, err = term.Recv()
	wantManagedRefusal(t, "OpenTerminal", err)
	nuke, err := a.hub.Nuke(ctx, &pb.NukeRequest{DryRun: true})
	if err != nil {
		t.Fatalf("Nuke: %v", err)
	}
	_, err = nuke.Recv()
	wantManagedRefusal(t, "Nuke", err)

	// a managed agent never rewrites its own config file: the one call that
	// would is refused and the file stays byte for byte as it was
	before, err := os.ReadFile(a.configFile)
	if err != nil {
		t.Fatal(err)
	}
	wantManagedRefusal(t, "UpdateXrayAPIConfig", a.hub.UpdateXrayAPIConfig(ctx, "127.0.0.1:10999"))
	after, err := os.ReadFile(a.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the config file changed:\n%s", after)
	}
	reloaded, err := config.Load(a.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Profile != config.ProfileManaged {
		t.Fatalf("the config says profile %q", reloaded.Profile)
	}
	_, err = a.hub.ExecuteCommand(ctx, "id", 5)
	wantManagedRefusal(t, "ExecuteCommand after the refused rewrite", err)

	// none of that reached a handler but the one allowed call
	for _, p := range []string{ranMarker, filepath.Join(a.dir, "xray", "evil.txt")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s exists: %v", p, err)
		}
	}
	if got, want := a.reached.seen(), []string{"GetVersion"}; !slices.Equal(got, want) {
		t.Fatalf("calls that got past the profile: %v, want %v", got, want)
	}
}

// Every call in the proto, over the wire: refused exactly when the profile says
// so, and the ones it lets in are the ones that reach the handlers.
func TestManagedAgentSweepOfEveryCall(t *testing.T) {
	a := startLiveAgent(t, "profile: managed", func(s *Server) pb.NodeAgentServer { return onlyTheSafeOnes{real: s} })
	oracle := middleware.NewProfileMiddleware(config.ProfileManaged)

	var wantIn []string
	for _, name := range allCalls(t) {
		err := a.callByName(t, name)
		if oracle.CheckName(name) != nil {
			wantManagedRefusal(t, name, err)
			continue
		}
		wantIn = append(wantIn, name)
		if status.Code(err) == codes.PermissionDenied {
			t.Errorf("%s was refused although the managed profile allows it: %v", name, err)
		}
	}
	slices.Sort(wantIn)
	if got := a.reached.seen(); !slices.Equal(got, wantIn) {
		t.Fatalf("calls that got past the profile:\n  %v\nwant\n  %v", got, wantIn)
	}
	if len(wantIn) != 29 || len(allCalls(t))-len(wantIn) != 31 {
		t.Fatalf("%d calls let in, %d refused: want 29 and 31", len(wantIn), len(allCalls(t))-len(wantIn))
	}
}

// What the hub is there for still works on a managed node, through the same
// interceptors: pushing an Xray build (a client stream) and a config.
func TestManagedAgentDoesTheHubsJob(t *testing.T) {
	a := startLiveAgent(t, "profile: managed", func(s *Server) pb.NodeAgentServer {
		s.xrayCheck = scriptOnly // the fake builds are scripts, not ELF files
		return s
	})
	// started here, so stopping it never goes looking for a stray xray
	if err := os.WriteFile(a.srv.xrayMgr.ConfigPath(), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.srv.xrayMgr.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.srv.xrayMgr.Stop(5 * time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	next := build(t, "26.11.1")
	if err := a.hub.PushXrayBinary(ctx, "26.11.1", sum(next), bytes.NewReader(next), true, false); err != nil {
		t.Fatalf("PushXrayBinary: %v", err)
	}
	if got, err := os.ReadFile(a.srv.xrayMgr.BinaryPath()); err != nil || !bytes.Equal(got, next) {
		t.Fatalf("the installed build is not the pushed one: %v", err)
	}

	const conf = `{"log":{"loglevel":"warning"}}`
	if ok, errs, _, err := a.hub.ValidateConfig(ctx, conf); err != nil || !ok {
		t.Fatalf("ValidateConfig: %v, %v, %v", ok, errs, err)
	}
	if err := a.hub.PushConfig(ctx, conf); err != nil {
		t.Fatalf("PushConfig: %v", err)
	}
	if got, _, err := a.hub.GetCurrentConfig(ctx); err != nil || got != conf {
		t.Fatalf("GetCurrentConfig: %q, %v", got, err)
	}
}

// With no profile line, or `profile: full`, the same server refuses nothing.
func TestFullAgentOverARealConnectionIsUnchanged(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	for _, line := range []string{"# no profile line", "profile: full", "profile: FULL"} {
		a := startLiveAgent(t, line, func(s *Server) pb.NodeAgentServer { return s })
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		res, err := a.hub.ExecuteCommand(ctx, "echo full-profile-ok", 10)
		if err != nil || !res.Success || !strings.Contains(res.Stdout, "full-profile-ok") {
			t.Fatalf("%q: ExecuteCommand: %+v, %v", line, res, err)
		}
		path, err := a.hub.WriteFile(ctx, "probe.txt", []byte("written"), 0o644)
		if err != nil {
			t.Fatalf("%q: WriteFile: %v", line, err)
		}
		if got, _ := os.ReadFile(path); string(got) != "written" {
			t.Fatalf("%q: the file holds %q", line, got)
		}
		term, err := a.hub.OpenTerminal(ctx)
		if err != nil {
			t.Fatalf("%q: OpenTerminal: %v", line, err)
		}
		if _, err := term.Recv(); err != nil {
			t.Fatalf("%q: the terminal did not open: %v", line, err)
		}
		_ = term.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Close{Close: true}})
		cancel()
	}
}

func TestFullAgentSweepOfEveryCallRefusesNothing(t *testing.T) {
	a := startLiveAgent(t, "profile: full", func(s *Server) pb.NodeAgentServer { return onlyTheSafeOnes{real: s} })
	for _, name := range allCalls(t) {
		if err := a.callByName(t, name); status.Code(err) == codes.PermissionDenied {
			t.Errorf("%s was refused on the full profile: %v", name, err)
		}
	}
	want := allCalls(t)
	slices.Sort(want)
	if got := a.reached.seen(); !slices.Equal(got, want) {
		t.Fatalf("calls that reached the handlers: %d of %d\n%v", len(got), len(want), got)
	}
}

// An agent told to use a profile it doesn't know doesn't start serving as
// something else: the config doesn't load.
func TestAnUnknownProfileNeverGetsAServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("profile: restricted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := config.Load(path); err == nil {
		t.Fatalf("loaded a config with profile %q", cfg.Profile)
	}
}

// NewServer keeps the profile it was started with: wiping the config struct
// afterwards, which is the worst a call that rewrites the config could do,
// changes nothing about what gets served.
func TestServerKeepsTheProfileItStartedWith(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile = config.ProfileManaged
	cfg.Traffic.BufferEnabled = false
	cfg.Xray.AccessLogPath = ""
	cfg.Process.ServiceMode = false
	cfg.Process.WatchdogEnabled = false
	cfg.Cores.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cfg.Profile = config.ProfileFull
	*cfg = *config.DefaultConfig()
	if srv.cfg.Profile != config.ProfileFull {
		t.Fatal("the test did not change the config the server holds")
	}

	opts, err := srv.serverOptions()
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer(opts...)
	pb.RegisterNodeAgentServer(gs, onlyTheSafeOnes{real: srv})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	_, err = pb.NewNodeAgentClient(conn).ExecuteCommand(context.Background(), &pb.ExecuteCommandRequest{Command: "id"})
	wantManagedRefusal(t, "ExecuteCommand", err)
}
