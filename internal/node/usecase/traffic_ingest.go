package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// IngestTraffic takes the node's stats gate so two ingestions can't book the same bytes.
func (u *nodeUsecase) IngestTraffic(ctx context.Context, nodeID uint, records []*agent.TrafficRecord, inTx func(ctx context.Context) error) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("ingest traffic: %w", err)
	}
	release, err := u.acquireStatsSync(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	defer release()
	return u.persistBufferedTrafficThen(ctx, node, records, inTx)
}

// RecordNodeStat saves a sample the stats sweep can't take, an MTProto node's; in UTC like the sweep's.
func (u *nodeUsecase) RecordNodeStat(ctx context.Context, stat *domain.NodeStat) error {
	if stat.CreatedAt.IsZero() {
		stat.CreatedAt = time.Now()
	}
	stat.CreatedAt = stat.CreatedAt.UTC()
	return u.nodeRepo.CreateNodeStat(ctx, stat)
}
