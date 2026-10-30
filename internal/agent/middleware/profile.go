package middleware

import (
	"context"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// managedAllowed is every call a managed agent answers, by full method name.
// It is an allow-list: whatever isn't here is refused, a call added to the
// proto later included, until someone opts it in here and in profile_test.go.
var managedAllowed = methodSet(
	// state and health
	pb.NodeAgent_GetStatus_FullMethodName,
	pb.NodeAgent_HealthCheck_FullMethodName,
	pb.NodeAgent_GetVersion_FullMethodName,
	pb.NodeAgent_GetHostInfo_FullMethodName,
	pb.NodeAgent_Heartbeat_FullMethodName,
	pb.NodeAgent_GetSystemStats_FullMethodName,
	// the Xray process and its config
	pb.NodeAgent_StartXray_FullMethodName,
	pb.NodeAgent_StopXray_FullMethodName,
	pb.NodeAgent_RestartXray_FullMethodName,
	pb.NodeAgent_PushConfig_FullMethodName,
	pb.NodeAgent_ValidateConfig_FullMethodName,
	pb.NodeAgent_GetCurrentConfig_FullMethodName,
	pb.NodeAgent_UpdateXrayAPIConfig_FullMethodName,
	// users on the node
	pb.NodeAgent_AddUser_FullMethodName,
	pb.NodeAgent_RemoveUser_FullMethodName,
	pb.NodeAgent_ListUsers_FullMethodName,
	// traffic and who is online
	pb.NodeAgent_GetXrayStats_FullMethodName,
	pb.NodeAgent_GetStatsBundle_FullMethodName,
	pb.NodeAgent_GetBufferedTraffic_FullMethodName,
	pb.NodeAgent_AckBufferedTraffic_FullMethodName,
	pb.NodeAgent_GetUserOnlineIPs_FullMethodName,
	pb.NodeAgent_GetAllUsersOnlineIPs_FullMethodName,
	// set-up helpers
	pb.NodeAgent_ProbeRealityDest_FullMethodName,
	pb.NodeAgent_GenerateVLESSKeys_FullMethodName,
	// updates and certificates
	pb.NodeAgent_SelfUpdate_FullMethodName,
	pb.NodeAgent_GetSelfChecksum_FullMethodName,
	pb.NodeAgent_PushXrayBinary_FullMethodName,
	pb.NodeAgent_InstallCertificate_FullMethodName,
	pb.NodeAgent_UpdateCertDenylist_FullMethodName,
	// logs
	pb.NodeAgent_StreamLogs_FullMethodName,
)

func methodSet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}

// ProfileMiddleware holds an agent profile and turns away the calls it doesn't
// allow. On the full profile it lets everything through.
type ProfileMiddleware struct {
	managed bool
}

// NewProfileMiddleware takes the profile once; nothing changes it afterwards.
func NewProfileMiddleware(p config.Profile) *ProfileMiddleware {
	return &ProfileMiddleware{managed: p.Managed()}
}

// Check answers nil when the call may run and PermissionDenied when the
// profile keeps it out. method is the full gRPC name, as in
// "/nodeagent.NodeAgent/GetStatus"; one from another service never matches.
func (m *ProfileMiddleware) Check(method string) error {
	if !m.managed {
		return nil
	}
	if _, ok := managedAllowed[method]; ok {
		return nil
	}
	return status.Errorf(codes.PermissionDenied, "this agent runs the managed profile: %s is not available", label(method))
}

// CheckName is Check for a transport that only has the method's own name, as
// the reverse channel does.
func (m *ProfileMiddleware) CheckName(name string) error {
	return m.Check("/" + pb.NodeAgent_ServiceDesc.ServiceName + "/" + name)
}

// label is the method's own name, short enough to put in a message.
func label(method string) string {
	if i := strings.LastIndex(method, "/"); i >= 0 {
		method = method[i+1:]
	}
	if method == "" {
		return "an empty method name"
	}
	if len(method) > 64 {
		method = method[:64] + "..."
	}
	return method
}

// UnaryServerInterceptor refuses a unary call the profile doesn't allow.
func (m *ProfileMiddleware) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if err := m.Check(info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor refuses a streaming call the profile doesn't allow.
// The caller sees the status on its first Send or Recv.
func (m *ProfileMiddleware) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := m.Check(info.FullMethod); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}
