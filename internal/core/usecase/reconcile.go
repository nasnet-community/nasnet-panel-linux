package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/nodebridge"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// CoreStatePusher is the narrow slice of the agent client that ReconcileNode
// needs: it pushes state, and reads which inbounds each core runs so the ones
// deleted on the hub are torn down. Every agent.NodeClient satisfies it.
type CoreStatePusher interface {
	ApplyCoreState(ctx context.Context, payload *pb.CoreStatePayload) error
	CoreStatusReader
}

// WithUserPolicyResolver wires the subscription-derived expiry / rate-limit
// lookup pushed with each core_user. Optional — without it, cores receive no
// policy and access is governed solely by the hub's enable/disable.
func WithUserPolicyResolver(fn nodebridge.UserPolicyResolver) Option {
	return func(u *CoreUsecase) { u.userPolicy = fn }
}

// ReconcileNode pushes the desired state of every core_inbound on a node to its
// agent. Called after the Xray config push in the node reconcile path. Every
// inbound is attempted: one that fails (a taken port, say) must not keep the
// others on the node down.
func (u *CoreUsecase) ReconcileNode(ctx context.Context, pusher CoreStatePusher, nodeID uint) error {
	payloads, err := nodebridge.BuildCoreStatePayloads(ctx, u.repo, nodeID, u.userPolicy)
	if err != nil {
		return fmt.Errorf("build core payloads for node %d: %w", nodeID, err)
	}
	if !u.servesUsers(ctx, nodeID) {
		// out of service: the tunnels keep running but let nobody in, and live sessions end
		for _, p := range payloads {
			for _, cu := range p.Users {
				cu.Enabled = false
			}
		}
	}
	var errs []error
	known := make(map[string]bool, len(payloads))
	for _, p := range payloads {
		known[p.GetInboundId()] = true
		if err := pusher.ApplyCoreState(ctx, p); err != nil {
			errs = append(errs, fmt.Errorf("apply core %s (inbound %s): %w", p.GetCoreType(), p.GetInboundId(), err))
		}
	}
	errs = append(errs, pruneDeletedInbounds(ctx, pusher, known)...)
	return errors.Join(errs...)
}

// pruneDeletedInbounds tears down inbounds a node's cores still run (or would
// replay after a restart) that the hub no longer has: a delete made while the
// node was unreachable is caught up on its next reconcile. A core the agent
// does not register, or an agent too old to report inbounds, is skipped.
func pruneDeletedInbounds(ctx context.Context, pusher CoreStatePusher, known map[string]bool) []error {
	var errs []error
	for _, proto := range domain.AllCoreProtocols() {
		st, err := pusher.GetCoreStatus(ctx, string(proto))
		if err != nil {
			continue
		}
		for _, id := range st.GetInboundIds() {
			if known[id] {
				continue
			}
			teardown := &pb.CoreStatePayload{CoreType: string(proto), InboundId: id, Enabled: false}
			if err := pusher.ApplyCoreState(ctx, teardown); err != nil {
				errs = append(errs, fmt.Errorf("tear down core %s (inbound %s): %w", proto, id, err))
			}
		}
	}
	return errs
}
