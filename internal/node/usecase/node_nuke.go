package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	auditdomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// NukeOptions carries the hub-orchestration inputs for a Wipe or Nuke run.
type NukeOptions struct {
	Mode          pb.NukeMode
	ShredRoot     bool
	DryRun        bool
	KeepHubRecord bool
	ActorID       uint
	ActorName     string
	IPAddress     string
}

// NukeEmitter receives per-phase progress frames. Pass nil when the caller
// doesn't need streaming progress (e.g. a CLI wipe that only wants the final
// report).
type NukeEmitter func(*pb.NukePhaseResult)

// ErrNukeInFlight is returned when a second Nuke/Wipe is attempted against a
// node that already has one running. Keeps the per-node state machine linear
// and avoids two concurrent RPCs fighting over the same filesystem.
var ErrNukeInFlight = errors.New("another nuke/wipe is already in progress for this node")

// ErrNukeFailed: agent ran but every phase failed. Distinct from transport
// errors; hub record left untouched.
var ErrNukeFailed = errors.New("nuke failed: all phases failed on the agent")

// Nuke: per-node mutex + audit + unary/streaming RPC dispatch +
// post-run record lifecycle. Keeps HTTP/SSE layer thin.
func (u *nodeUsecase) Nuke(ctx context.Context, nodeID uint, opts NukeOptions, emit NukeEmitter) (*pb.NukeReport, error) {
	// Grab the per-node lock; second caller gets a clean error rather than
	// racing against the first RPC.
	u.nukesInFlightMu.Lock()
	if _, exists := u.nukesInFlight[nodeID]; exists {
		u.nukesInFlightMu.Unlock()
		return nil, ErrNukeInFlight
	}
	u.nukesInFlight[nodeID] = struct{}{}
	u.nukesInFlightMu.Unlock()
	defer func() {
		u.nukesInFlightMu.Lock()
		delete(u.nukesInFlight, nodeID)
		u.nukesInFlightMu.Unlock()
	}()

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load node: %w", err)
	}

	u.writeAuditStarted(ctx, node, opts)

	req := &pb.NukeRequest{
		Mode:      opts.Mode,
		ShredRoot: opts.ShredRoot,
		DryRun:    opts.DryRun,
	}

	report, callErr := u.callAgentForNuke(ctx, node, req, emit)
	if callErr != nil {
		u.writeAuditFailed(ctx, node, opts, callErr)
		return nil, callErr
	}

	// All phases failed → hub record stays intact. Return report + sentinel
	// so HTTP layer distinguishes unreachable from ran-but-all-failed.
	if report != nil && report.Result == pb.NukeReport_NUKE_RESULT_FAILED {
		u.writeAuditFailedRun(ctx, node, opts, report)
		return report, ErrNukeFailed
	}

	u.writeAuditCompleted(ctx, node, opts, report)

	// Only mutate the hub record on real runs. Dry-runs must leave the node
	// untouched so operators can preview without consequences.
	if !opts.DryRun {
		// Detach DB-write ctx from request ctx: an SSE disconnect after the
		// agent finished must not abort UpdateNode/DeleteNode and leave the
		// hub record out of sync with the wiped agent.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := u.applyRecordLifecycle(writeCtx, node, opts, report); err != nil {
			// Report still succeeded on the remote — return it so the caller
			// can surface progress even if our own DB write failed.
			return report, err
		}
	}

	return report, nil
}

// callAgentForNuke dispatches on the requested mode — WIPE over the unary RPC,
// NUKE over the streaming one — and fans streaming frames out to the caller via
// emit. Node connect mode is not consulted: the hub talks to every node through
// the in-process embedded client, which supports both RPCs.
func (u *nodeUsecase) callAgentForNuke(ctx context.Context, node *domain.Node, req *pb.NukeRequest, emit NukeEmitter) (*pb.NukeReport, error) {
	client, err := u.getAgentClientForNuke(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("get agent client: %w", err)
	}
	defer closeAgentClient(client)

	// WIPE mode is unary. run it and synthesize frames from the report
	if req.Mode == pb.NukeMode_NUKE_MODE_WIPE {
		report, err := client.Wipe(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("wipe rpc: %w", err)
		}
		if emit != nil && report != nil {
			for _, p := range report.Phases {
				emit(p)
			}
		}
		return report, nil
	}

	stream, err := client.Nuke(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("nuke rpc: %w", err)
	}

	var finalReport *pb.NukeReport
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("nuke stream: %w", err)
		}
		switch ev := msg.Event.(type) {
		case *pb.NukeProgress_Phase:
			if emit != nil && ev.Phase != nil {
				emit(ev.Phase)
			}
		case *pb.NukeProgress_Done:
			finalReport = ev.Done
		}
	}
	if finalReport == nil {
		return nil, errors.New("nuke stream closed without a final report")
	}
	return finalReport, nil
}

// getAgentClientForNuke: tests swap via nukeAgentClientFactory; nil →
// getAgentClient.
func (u *nodeUsecase) getAgentClientForNuke(ctx context.Context, node *domain.Node) (agent.NodeClient, error) {
	if u.nukeAgentClientFactory != nil {
		return u.nukeAgentClientFactory(ctx, node)
	}
	return u.getAgentClient(node)
}

// applyRecordLifecycle: KeepHubRecord=false → DeleteNode cascade;
// true → mark nuked_at/nuke_mode for the tombstone state.
func (u *nodeUsecase) applyRecordLifecycle(ctx context.Context, node *domain.Node, opts NukeOptions, report *pb.NukeReport) error {
	if !opts.KeepHubRecord {
		if err := u.DeleteNode(ctx, node.ID, true); err != nil {
			logger.GetLogger().WithError(err).
				WithField("node_id", node.ID).
				Warn("[Nuke] cascade delete failed after remote wipe succeeded")
			return fmt.Errorf("delete node record: %w", err)
		}
		return nil
	}

	now := time.Now()
	node.NukedAt = &now
	switch {
	case opts.Mode == pb.NukeMode_NUKE_MODE_NUKE && report.Result == pb.NukeReport_NUKE_RESULT_PARTIAL:
		node.NukeMode = "NUKE_PARTIAL"
	case opts.Mode == pb.NukeMode_NUKE_MODE_NUKE:
		node.NukeMode = "NUKE"
	case report.Result == pb.NukeReport_NUKE_RESULT_PARTIAL:
		node.NukeMode = "WIPE_PARTIAL"
	default:
		node.NukeMode = "WIPE"
	}
	// A wiped node can't serve traffic anymore; flip both flags so listings
	// and dispatchers stop selecting it.
	node.IsActive = false
	node.IsOnline = false
	if err := u.nodeRepo.UpdateNode(ctx, node); err != nil {
		return fmt.Errorf("mark node nuked: %w", err)
	}
	return nil
}

// ── Audit helpers ───────────────────────────────────────────────────────────

func (u *nodeUsecase) writeAuditStarted(ctx context.Context, node *domain.Node, opts NukeOptions) {
	if u.auditUC == nil || node == nil {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"status":          "started",
		"mode":            opts.Mode.String(),
		"shred_root":      opts.ShredRoot,
		"dry_run":         opts.DryRun,
		"keep_hub_record": opts.KeepHubRecord,
	})
	u.auditUC.Log(ctx, &auditdomain.AuditLog{
		Action:     string(nukeAuditAction(opts.Mode)),
		ActorID:    opts.ActorID,
		ActorName:  opts.ActorName,
		EntityType: "node",
		EntityID:   node.ID,
		NewValues:  string(payload),
		IPAddress:  opts.IPAddress,
		Source:     "admin_api",
	})
}

func (u *nodeUsecase) writeAuditCompleted(ctx context.Context, node *domain.Node, opts NukeOptions, report *pb.NukeReport) {
	if u.auditUC == nil || node == nil || report == nil {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"status":            "completed",
		"result":            report.Result.String(),
		"total_duration_ms": report.TotalDurationMs,
		"phases":            report.Phases,
		"dry_run":           opts.DryRun,
	})
	u.auditUC.Log(ctx, &auditdomain.AuditLog{
		Action:     string(nukeAuditAction(opts.Mode)),
		ActorID:    opts.ActorID,
		ActorName:  opts.ActorName,
		EntityType: "node",
		EntityID:   node.ID,
		NewValues:  string(payload),
		IPAddress:  opts.IPAddress,
		Source:     "admin_api",
	})
}

// writeAuditFailedRun records the "agent ran but every phase failed" case.
// Distinct from writeAuditFailed (transport error, agent never ran) so the
// audit trail can tell them apart.
func (u *nodeUsecase) writeAuditFailedRun(ctx context.Context, node *domain.Node, opts NukeOptions, report *pb.NukeReport) {
	if u.auditUC == nil || node == nil || report == nil {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"status":            "failed",
		"result":            report.Result.String(),
		"total_duration_ms": report.TotalDurationMs,
		"phases":            report.Phases,
		"mode":              opts.Mode.String(),
		"dry_run":           opts.DryRun,
	})
	u.auditUC.Log(ctx, &auditdomain.AuditLog{
		Action:     string(nukeAuditAction(opts.Mode)),
		ActorID:    opts.ActorID,
		ActorName:  opts.ActorName,
		EntityType: "node",
		EntityID:   node.ID,
		NewValues:  string(payload),
		IPAddress:  opts.IPAddress,
		Source:     "admin_api",
	})
}

func (u *nodeUsecase) writeAuditFailed(ctx context.Context, node *domain.Node, opts NukeOptions, err error) {
	if u.auditUC == nil || node == nil {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"status":  "failed_to_start",
		"error":   err.Error(),
		"mode":    opts.Mode.String(),
		"dry_run": opts.DryRun,
	})
	u.auditUC.Log(ctx, &auditdomain.AuditLog{
		Action:     string(nukeAuditAction(opts.Mode)),
		ActorID:    opts.ActorID,
		ActorName:  opts.ActorName,
		EntityType: "node",
		EntityID:   node.ID,
		NewValues:  string(payload),
		IPAddress:  opts.IPAddress,
		Source:     "admin_api",
	})
}

func nukeAuditAction(m pb.NukeMode) auditdomain.AuditAction {
	if m == pb.NukeMode_NUKE_MODE_NUKE {
		return auditdomain.AuditNodeNuke
	}
	return auditdomain.AuditNodeWipe
}
