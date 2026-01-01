package usecase

import (
	"context"
	"sync"
	"time"
)

// NodeBulkActionResult is the per-node outcome of a bulk action.
// Success=false means the action failed for that node — Error holds the reason.
type NodeBulkActionResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

const (
	// bulkActionConcurrency caps simultaneous per-node operations during a
	// bulk action. Keep this low enough that a 50-node fleet doesn't
	// overwhelm the agent RPC layer, high enough that operators don't wait
	// serial-seconds for a restart sweep.
	bulkActionConcurrency = 8
)

// runBulkNodeAction fans out a per-node op across many IDs in parallel.
// Each op gets its own timeout derived from ttl; failures are returned
// inline rather than aborting the whole batch.
func (u *nodeUsecase) runBulkNodeAction(
	ctx context.Context,
	ids []uint,
	ttl time.Duration,
	op func(ctx context.Context, nodeID uint) error,
) map[uint]*NodeBulkActionResult {
	results := make(map[uint]*NodeBulkActionResult, len(ids))
	if len(ids) == 0 {
		return results
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, bulkActionConcurrency)

	for _, id := range ids {
		select {
		case <-ctx.Done():
			// Record remaining ids as cancelled so callers see a complete map.
			mu.Lock()
			if _, ok := results[id]; !ok {
				results[id] = &NodeBulkActionResult{Error: ctx.Err().Error()}
			}
			mu.Unlock()
			continue
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(nodeID uint) {
			defer wg.Done()
			defer func() { <-sem }()

			callCtx, cancel := context.WithTimeout(ctx, ttl)
			defer cancel()

			entry := &NodeBulkActionResult{Success: true}
			if err := op(callCtx, nodeID); err != nil {
				entry.Success = false
				entry.Error = err.Error()
			}
			mu.Lock()
			results[nodeID] = entry
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	return results
}

// BulkRestartXray restarts Xray on every listed node. 45s per node matches
// the per-method deadline table for RestartXray.
func (u *nodeUsecase) BulkRestartXray(ctx context.Context, ids []uint) map[uint]*NodeBulkActionResult {
	return u.runBulkNodeAction(ctx, ids, 45*time.Second, func(ctx context.Context, nodeID uint) error {
		return u.RestartXray(ctx, nodeID)
	})
}

// BulkPushConfig pushes full Xray config to every listed node. Push is the
// slowest common op (cert reload, stats reset) — give each node 90s.
func (u *nodeUsecase) BulkPushConfig(ctx context.Context, ids []uint) map[uint]*NodeBulkActionResult {
	return u.runBulkNodeAction(ctx, ids, 90*time.Second, func(ctx context.Context, nodeID uint) error {
		return u.PushFullConfig(ctx, nodeID)
	})
}

// BulkCheckHealth probes every listed node. 15s covers a cold dial + one
// status RPC; unreachable nodes surface as per-entry errors.
func (u *nodeUsecase) BulkCheckHealth(ctx context.Context, ids []uint) map[uint]*NodeBulkActionResult {
	return u.runBulkNodeAction(ctx, ids, 15*time.Second, func(ctx context.Context, nodeID uint) error {
		_, err := u.CheckNodeHealth(ctx, nodeID)
		return err
	})
}

// BulkUpdateXrayVersion fans out an xray-core version update to every listed
// node. 6 minutes per node covers the agent's 5-minute UpdateXrayBinary RPC
// deadline plus dial, GetHostInfo, and token generation overhead.
func (u *nodeUsecase) BulkUpdateXrayVersion(ctx context.Context, ids []uint, version string) map[uint]*NodeBulkActionResult {
	return u.runBulkNodeAction(ctx, ids, 6*time.Minute, func(ctx context.Context, nodeID uint) error {
		return u.UpdateXrayVersion(ctx, nodeID, version)
	})
}
