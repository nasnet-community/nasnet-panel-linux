package usecase

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/realitydest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RealityDestError is returned when a REALITY inbound is saved with a dest the
// server could not hijack. The report carries the per-server-name detail the
// panel shows next to the "Save anyway" action.
type RealityDestError struct {
	Report *realitydest.Report
}

func (e *RealityDestError) Error() string {
	return "REALITY destination check failed: " + e.Report.Summary
}

// hubVantageWarning is attached when the check had to run from the hub.
const hubVantageWarning = "Checked from the hub because the agent on this node predates destination checks. Update the agent to test from the node's own network, which is what clients will hit."

// CheckRealityDest performs the dest handshake from the node's network. When
// the agent is too old for the RPC the hub runs the same probe itself and says
// so, since reachability and TLS behaviour can differ between the two.
func (u *nodeUsecase) CheckRealityDest(ctx context.Context, nodeID uint, settings *domain.RealitySettings) (*realitydest.Report, error) {
	if settings == nil {
		return nil, fmt.Errorf("reality settings are required")
	}
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, ErrNodeNotFound
	}
	req := realitydest.Request{Dest: settings.Dest, Xver: settings.Xver, ServerNames: settings.ServerNames}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if len(req.ServerNames) > realitydest.MaxServerNames {
		return nil, fmt.Errorf("at most %d server names per check", realitydest.MaxServerNames)
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, fmt.Errorf("node is unreachable: %w", err)
	}
	defer closeAgentClient(client)
	// The remote RPC's deadline is 45s; in-process calls and the hub's own
	// probe get the same bound.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	if rc, ok := client.(agent.RealityDestClient); ok {
		resp, err := rc.ProbeRealityDest(ctx, realitydest.ToProto(req))
		switch {
		case err == nil:
			report := realitydest.ReportFromProto(resp)
			report.CheckedFrom = "node"
			return report, nil
		case status.Code(err) == codes.InvalidArgument:
			return nil, fmt.Errorf("%s", status.Convert(err).Message())
		case status.Code(err) != codes.Unimplemented && !isUnknownMethod(err):
			return nil, fmt.Errorf("destination check failed on node: %w", err)
		}
	}

	report, err := realitydest.Probe(ctx, req)
	if err != nil {
		return nil, err
	}
	report.CheckedFrom = "hub"
	report.Warnings = append([]string{hubVantageWarning}, report.Warnings...)
	return report, nil
}

// isUnknownMethod matches the reverse tunnel's answer for an RPC the agent
// build does not dispatch; direct gRPC reports codes.Unimplemented instead.
func isUnknownMethod(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown method") || strings.Contains(msg, "code = unimplemented")
}

// guardRealityDest blocks saving a REALITY inbound whose dest cannot be
// hijacked. It runs only when the dest-related fields are new or changed and
// the caller did not opt out. A node that cannot be reached is not a verdict:
// the push that follows reports that on its own. A remote node already marked
// offline is skipped rather than dialled, so the save doesn't wait out the
// check's deadline.
func (u *nodeUsecase) guardRealityDest(ctx context.Context, inbound, prev *domain.Inbound) error {
	if inbound.Security != "reality" || inbound.RealitySettings == nil || inbound.SkipDestCheck {
		return nil
	}
	if prev != nil && prev.Security == "reality" && prev.RealitySettings != nil && sameRealityDest(prev.RealitySettings, inbound.RealitySettings) {
		return nil
	}
	if node, err := u.nodeRepo.GetNode(ctx, inbound.NodeID); err == nil && !node.IsLocal() && !node.IsOnline {
		logger.GetLogger().WithFields(map[string]interface{}{
			"node_id": inbound.NodeID,
			"tag":     inbound.Tag,
		}).Warn("[guardRealityDest] Destination check skipped, node offline")
		return nil
	}
	report, err := u.CheckRealityDest(ctx, inbound.NodeID, inbound.RealitySettings)
	if err != nil {
		logger.GetLogger().WithError(err).WithFields(map[string]interface{}{
			"node_id": inbound.NodeID,
			"tag":     inbound.Tag,
		}).Warn("[guardRealityDest] Destination check skipped")
		return nil
	}
	if !report.OK {
		return &RealityDestError{Report: report}
	}
	return nil
}

func sameRealityDest(a, b *domain.RealitySettings) bool {
	return a.Dest == b.Dest && a.Xver == b.Xver && slices.Equal(a.ServerNames, b.ServerNames)
}
