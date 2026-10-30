package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// wantAllowed is the managed allow-list as it was approved. Opting a call in
// or out takes a change here as well as in profile.go, on purpose.
var wantAllowed = map[string]bool{
	"GetStatus": true, "HealthCheck": true, "GetVersion": true, "GetHostInfo": true,
	"Heartbeat": true, "GetSystemStats": true,
	"StartXray": true, "StopXray": true, "RestartXray": true, "PushConfig": true,
	"ValidateConfig": true, "GetCurrentConfig": true,
	"AddUser": true, "RemoveUser": true, "ListUsers": true,
	"GetXrayStats": true, "GetStatsBundle": true, "GetBufferedTraffic": true,
	"AckBufferedTraffic": true, "GetUserOnlineIPs": true, "GetAllUsersOnlineIPs": true,
	"ProbeRealityDest": true, "GenerateVLESSKeys": true,
	"SelfUpdate": true, "GetSelfChecksum": true, "PushXrayBinary": true,
	"InstallCertificate": true, "UpdateCertDenylist": true,
	"StreamLogs": true,
}

// wantDenied is every other call and why it stays off. A call that is in
// neither table fails TestEveryCallHasAManagedDecision, so adding one to the
// proto forces a choice here.
var wantDenied = map[string]string{
	"ExecuteCommand": "runs any shell command",
	"WriteFile":      "writes files on the node",
	"OpenTerminal":   "opens a shell",

	"Uninstall": "removes the agent and Xray",
	"Wipe":      "wipes the node's footprint",
	"Nuke":      "wipes the node, streaming",

	"SwitchConnectMode": "moves the agent between direct and reverse",
	"MoveListenPort":    "changes the agent's port",
	"GetSSHStatus":      "not needed by a managed hub",
	"UpdateSSHConfig":   "changes the SSH service",
	"ClearSSHLogs":      "erases the SSH auth logs",
	"RestartSSH":        "restarts sshd",

	"UpdateXrayBinary":    "installs Xray from a URL or a shell script; PushXrayBinary is the way in",
	"UpdateXrayAPIConfig": "rewrites the agent's own config file; the plugin writes it once, at install",
	"InspectCertFile":     "reads whichever path the caller names",

	"TestOutbound":      "outbound probing an entry node doesn't do",
	"FetchPoolSource":   "outbound probing an entry node doesn't do",
	"TestPoolCandidate": "outbound probing an entry node doesn't do",

	"SetupBandwidth":    "not needed by a managed hub",
	"TeardownBandwidth": "not needed by a managed hub",

	"GetAccessLogs":               "per-subscription logs aren't kept for a managed hub",
	"GetBufferedAccessLogSummary": "per-subscription logs aren't kept for a managed hub",
	"AckBufferedAccessLogSummary": "per-subscription logs aren't kept for a managed hub",

	"GetStarlinkStatus":         "dish monitoring isn't part of an entry node",
	"GetStarlinkObstructionMap": "dish monitoring isn't part of an entry node",
	"StarlinkHandle":            "dish monitoring isn't part of an entry node",

	"ApplyCoreState":         "tunnel cores write files and run daemons",
	"GetCoreStatus":          "tunnel cores aren't part of an entry node",
	"ProbeDependencies":      "tunnel cores aren't part of an entry node",
	"StartDependencyInstall": "downloads and installs binaries",
	"GetInstallProgress":     "tunnel cores aren't part of an entry node",
}

func fullName(method string) string {
	return "/" + pb.NodeAgent_ServiceDesc.ServiceName + "/" + method
}

// serviceMethods are the names the proto declares, in its own order.
func serviceMethods(t *testing.T) []string {
	t.Helper()
	svc := pb.File_node_agent_proto.Services().ByName("NodeAgent")
	if svc == nil {
		t.Fatal("the NodeAgent service is missing from the proto descriptor")
	}
	var names []string
	for i := 0; i < svc.Methods().Len(); i++ {
		names = append(names, string(svc.Methods().Get(i).Name()))
	}
	return names
}

func TestEveryCallHasAManagedDecision(t *testing.T) {
	m := NewProfileMiddleware(config.ProfileManaged)
	seen := map[string]bool{}
	for _, name := range serviceMethods(t) {
		seen[name] = true
		reason, denied := wantDenied[name]
		switch {
		case wantAllowed[name] && denied:
			t.Errorf("%s is on both lists", name)
			continue
		case !wantAllowed[name] && !denied:
			t.Errorf("%s has no managed decision: put it in wantAllowed (and profile.go) or in wantDenied with a reason", name)
			continue
		case denied && reason == "":
			t.Errorf("%s is denied without a reason", name)
		}
		err := m.CheckName(name)
		if wantAllowed[name] && err != nil {
			t.Errorf("%s should pass on the managed profile: %v", name, err)
		}
		if denied && status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s should be refused on the managed profile, got %v", name, err)
		}
	}
	// A stale or misspelled entry would deny a call by accident, or sit in a
	// table doing nothing.
	for name := range wantAllowed {
		if !seen[name] {
			t.Errorf("wantAllowed names %s, which is not a NodeAgent call", name)
		}
	}
	for name := range wantDenied {
		if !seen[name] {
			t.Errorf("wantDenied names %s, which is not a NodeAgent call", name)
		}
	}
	for full := range managedAllowed {
		if !seen[strings.TrimPrefix(full, fullName(""))] {
			t.Errorf("profile.go allows %s, which is not a NodeAgent call", full)
		}
	}
}

func TestManagedAllowListIsTheApprovedOne(t *testing.T) {
	if len(wantAllowed) != 29 || len(managedAllowed) != 29 {
		t.Fatalf("approved %d, allowed %d: want 29 each", len(wantAllowed), len(managedAllowed))
	}
	for name := range wantAllowed {
		if _, ok := managedAllowed[fullName(name)]; !ok {
			t.Errorf("%s is approved but not in managedAllowed", name)
		}
	}
	if len(wantDenied) != 31 {
		t.Errorf("%d denied calls, want 31", len(wantDenied))
	}
}

// What the server matches in an interceptor is the registered handler's name,
// so the same decisions have to hold for those, unary and streaming alike.
func TestManagedDecisionsHoldForTheRegisteredHandlers(t *testing.T) {
	m := NewProfileMiddleware(config.ProfileManaged)
	var registered []string
	for _, d := range pb.NodeAgent_ServiceDesc.Methods {
		registered = append(registered, d.MethodName)
	}
	for _, d := range pb.NodeAgent_ServiceDesc.Streams {
		registered = append(registered, d.StreamName)
	}
	if len(registered) != len(serviceMethods(t)) {
		t.Fatalf("%d handlers registered, %d calls in the proto", len(registered), len(serviceMethods(t)))
	}
	for _, name := range registered {
		err := m.Check(fullName(name))
		if wantAllowed[name] != (err == nil) {
			t.Errorf("%s: allowed %v, Check = %v", name, wantAllowed[name], err)
		}
	}
}

func TestManagedRefusalSaysWhy(t *testing.T) {
	err := NewProfileMiddleware(config.ProfileManaged).Check(pb.NodeAgent_ExecuteCommand_FullMethodName)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v", status.Code(err))
	}
	msg := status.Convert(err).Message()
	if !strings.Contains(msg, "this agent runs the managed profile") || !strings.Contains(msg, "ExecuteCommand") {
		t.Fatalf("message = %q", msg)
	}
}

func TestManagedMatchesTheFullNameOnly(t *testing.T) {
	m := NewProfileMiddleware(config.ProfileManaged)
	for _, method := range []string{
		"",
		"GetStatus",                      // a bare name is not a full one
		"/other.Service/GetStatus",       // another service with a familiar call
		"/nodeagent.NodeAgent/getstatus", // case
		"/nodeagent.NodeAgent/GetStatus ",
		"/nodeagent.NodeAgent/GetStatus/../ExecuteCommand",
		"/nodeagent.NodeAgent//GetStatus",
		"//nodeagent.NodeAgent/GetStatus",
		"/nodeagent.NodeAgent/",
		"/nodeagent.NodeAgent/Brand New Call",
	} {
		if err := m.Check(method); status.Code(err) != codes.PermissionDenied {
			t.Errorf("Check(%q) = %v, want PermissionDenied", method, err)
		}
	}
	for _, name := range []string{"", "getstatus", "GetStatus ", "x/GetStatus", "GetStatus/../ExecuteCommand", "/GetStatus"} {
		if err := m.CheckName(name); status.Code(err) != codes.PermissionDenied {
			t.Errorf("CheckName(%q) = %v, want PermissionDenied", name, err)
		}
	}
}

func TestRefusalMessageStaysShort(t *testing.T) {
	m := NewProfileMiddleware(config.ProfileManaged)
	msg := status.Convert(m.CheckName(strings.Repeat("A", 5000))).Message()
	if len(msg) > 200 {
		t.Fatalf("a %d byte message for a long method name", len(msg))
	}
	if msg := status.Convert(m.CheckName("")).Message(); !strings.Contains(msg, "empty") {
		t.Fatalf("message for no name = %q", msg)
	}
}

func TestFullProfileLetsEveryCallThrough(t *testing.T) {
	for _, p := range []config.Profile{config.ProfileFull, ""} {
		m := NewProfileMiddleware(p)
		for _, name := range serviceMethods(t) {
			if err := m.CheckName(name); err != nil {
				t.Errorf("profile %q refused %s: %v", p, name, err)
			}
			if err := m.Check(fullName(name)); err != nil {
				t.Errorf("profile %q refused %s: %v", p, fullName(name), err)
			}
		}
		// it has no opinion on a call it has never heard of
		if err := m.Check("/other.Service/Anything"); err != nil {
			t.Errorf("profile %q refused an unknown call: %v", p, err)
		}
	}
}

// A profile that skipped parsing can only tighten the agent.
func TestAnUncheckedProfileGetsTheManagedList(t *testing.T) {
	m := NewProfileMiddleware("Managed ")
	if err := m.CheckName("ExecuteCommand"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ExecuteCommand = %v", err)
	}
	if err := m.CheckName("GetStatus"); err != nil {
		t.Fatalf("GetStatus = %v", err)
	}
}

func TestUnaryInterceptor(t *testing.T) {
	type call struct{ ran bool }
	run := func(m *ProfileMiddleware, method string) (*call, interface{}, error) {
		c := &call{}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			c.ran = true
			return "reply to " + req.(string), nil
		}
		resp, err := m.UnaryServerInterceptor()(context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: method}, handler)
		return c, resp, err
	}

	managed := NewProfileMiddleware(config.ProfileManaged)
	c, resp, err := run(managed, pb.NodeAgent_GetStatus_FullMethodName)
	if err != nil || !c.ran || resp != "reply to request" {
		t.Fatalf("an allowed call: ran %v, %v, %v", c.ran, resp, err)
	}
	for _, method := range []string{
		pb.NodeAgent_ExecuteCommand_FullMethodName,
		pb.NodeAgent_WriteFile_FullMethodName,
		pb.NodeAgent_Uninstall_FullMethodName,
		pb.NodeAgent_SwitchConnectMode_FullMethodName,
		"/nodeagent.NodeAgent/AddedNextYear",
	} {
		c, resp, err := run(managed, method)
		if c.ran || resp != nil {
			t.Errorf("%s reached the handler (ran %v, resp %v)", method, c.ran, resp)
		}
		if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "managed profile") {
			t.Errorf("%s: error = %v", method, err)
		}
	}

	// the handler's own error is not touched
	boom := errors.New("boom")
	_, err = managed.UnaryServerInterceptor()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: pb.NodeAgent_GetStatus_FullMethodName},
		func(context.Context, interface{}) (interface{}, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Errorf("handler error became %v", err)
	}

	// and the full profile passes anything
	for _, name := range serviceMethods(t) {
		c, resp, err := run(NewProfileMiddleware(config.ProfileFull), fullName(name))
		if err != nil || !c.ran || resp != "reply to request" {
			t.Errorf("full profile, %s: ran %v, %v, %v", name, c.ran, resp, err)
		}
	}
}

type fakeServerStream struct{ grpc.ServerStream }

func TestStreamInterceptor(t *testing.T) {
	run := func(m *ProfileMiddleware, method string) (ran bool, got grpc.ServerStream, err error) {
		ss := &fakeServerStream{}
		handler := func(srv interface{}, stream grpc.ServerStream) error {
			ran, got = true, stream
			return nil
		}
		err = m.StreamServerInterceptor()("server", ss, &grpc.StreamServerInfo{FullMethod: method}, handler)
		if ran && got != grpc.ServerStream(ss) {
			t.Errorf("%s: the handler got a different stream", method)
		}
		return ran, got, err
	}

	managed := NewProfileMiddleware(config.ProfileManaged)
	for _, name := range []string{"Heartbeat", "StreamLogs", "PushXrayBinary"} {
		if ran, _, err := run(managed, fullName(name)); !ran || err != nil {
			t.Errorf("%s: ran %v, %v", name, ran, err)
		}
	}
	for _, name := range []string{"OpenTerminal", "Nuke", "AddedNextYear"} {
		ran, _, err := run(managed, fullName(name))
		if ran {
			t.Errorf("%s reached the handler", name)
		}
		if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), "managed profile") {
			t.Errorf("%s: error = %v", name, err)
		}
	}

	boom := errors.New("boom")
	err := managed.StreamServerInterceptor()(nil, &fakeServerStream{},
		&grpc.StreamServerInfo{FullMethod: pb.NodeAgent_Heartbeat_FullMethodName},
		func(interface{}, grpc.ServerStream) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("handler error became %v", err)
	}

	for _, name := range serviceMethods(t) {
		if ran, _, err := run(NewProfileMiddleware(config.ProfileFull), fullName(name)); !ran || err != nil {
			t.Errorf("full profile, %s: ran %v, %v", name, ran, err)
		}
	}
}
