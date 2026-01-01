package agent

import (
	"context"
	"strings"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"google.golang.org/grpc"
)

// DefaultRPCDeadline is used for any NodeAgent RPC that is not explicitly
// listed in the method-specific table below. It bounds panel latency per call
// so a single slow agent cannot hang a handler goroutine for 30+ seconds.
const DefaultRPCDeadline = 10 * time.Second

// rpcDeadlines: per-method deadline budget. Keys are short names or full
// gRPC paths; both resolve via shortName(). Fast reads 5s, control 10-30s,
// binary uploads 5min. Streaming RPCs excluded (would cancel mid-use).
var rpcDeadlines = map[string]time.Duration{
	// Fast reads
	"GetStatus":                   5 * time.Second,
	"GetSystemStats":              5 * time.Second,
	"GetHostInfo":                 5 * time.Second,
	"GetXrayStats":                5 * time.Second,
	"GetVersion":                  5 * time.Second,
	"HealthCheck":                 5 * time.Second,
	"GetUserOnlineIPs":            5 * time.Second,
	"GetAllUsersOnlineIPs":        15 * time.Second,
	"GetBufferedTraffic":          10 * time.Second,
	"AckBufferedTraffic":          5 * time.Second,
	"GetAccessLogs":               10 * time.Second,
	"GetBufferedAccessLogSummary": 10 * time.Second,
	"AckBufferedAccessLogSummary": 5 * time.Second,
	"GetSSHStatus":                5 * time.Second,
	"GetStarlinkStatus":           5 * time.Second,
	"GetStarlinkObstructionMap":   10 * time.Second,
	"GetSelfChecksum":             5 * time.Second,
	"GetCurrentConfig":            5 * time.Second,

	// User / config operations (hot path via AlterInbound)
	"AddUser":             10 * time.Second,
	"RemoveUser":          10 * time.Second,
	"ListUsers":           10 * time.Second,
	"UpdateCertDenylist":  10 * time.Second,
	"ValidateConfig":      15 * time.Second,
	"PushConfig":          30 * time.Second,
	"UpdateXrayAPIConfig": 10 * time.Second,
	"WriteFile":           30 * time.Second,
	"GenerateVLESSKeys":   10 * time.Second,
	"UpdateSSHConfig":     15 * time.Second,
	"ClearSSHLogs":        10 * time.Second,
	"SetupBandwidth":      15 * time.Second,
	"TeardownBandwidth":   15 * time.Second,

	// Process control (xray restart includes a validate + kill + re-exec)
	"StartXray":      30 * time.Second,
	"StopXray":       30 * time.Second,
	"RestartXray":    45 * time.Second,
	"RestartSSH":     30 * time.Second,
	"TestOutbound":   30 * time.Second,
	"ExecuteCommand": 60 * time.Second,

	// Binary uploads and self-update
	"SelfUpdate":       5 * time.Minute,
	"UpdateXrayBinary": 5 * time.Minute,
	"Uninstall":        60 * time.Second,
}

// shortName strips the gRPC service prefix, returning the bare method name.
// "/nodeagent.NodeAgent/GetStatus" → "GetStatus".
// "GetStatus" → "GetStatus".
func shortName(method string) string {
	if i := strings.LastIndex(method, "/"); i >= 0 {
		return method[i+1:]
	}
	return method
}

// DeadlineFor returns the configured deadline for a NodeAgent RPC, falling
// back to DefaultRPCDeadline for unknown methods. method may be either a full
// gRPC path or a bare method name.
func DeadlineFor(method string) time.Duration {
	if d, ok := rpcDeadlines[method]; ok {
		return d
	}
	if d, ok := rpcDeadlines[shortName(method)]; ok {
		return d
	}
	return DefaultRPCDeadline
}

// WithRPCDeadline applies rpcDeadlines[method] to ctx if no deadline is
// already set. Use at every unbounded agent-RPC call site.
func WithRPCDeadline(ctx context.Context, method string) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, DeadlineFor(method))
}

// DeadlineUnaryInterceptor is a grpc.UnaryClientInterceptor that applies
// per-method deadlines to outgoing NodeAgent RPCs when the caller has not
// set one. Wire it into grpc.DialContext via grpc.WithUnaryInterceptor().
func DeadlineUnaryInterceptor() grpc.UnaryClientInterceptor {
	// Sanity check: the table must remain in sync with the proto so we catch
	// typos at startup time if someone removes a method.
	_ = pb.NodeAgent_GetStatus_FullMethodName

	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, DeadlineFor(method))
			defer cancel()
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
