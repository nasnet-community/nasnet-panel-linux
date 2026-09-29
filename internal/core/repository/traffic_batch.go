package repository

import (
	"context"
	"sort"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

const trafficBatchSize = 50

func (r *coreRepository) ListCoreTrafficRefsByNode(ctx context.Context, nodeID uint) ([]contract.CoreTrafficRef, error) {
	var refs []contract.CoreTrafficRef
	err := database.GetExecutor(r.db, ctx).WithContext(ctx).
		Model(&domain.CoreUser{}).
		Select("core_users.id, core_users.subscription_id, core_inbounds.protocol").
		Joins("JOIN core_inbounds ON core_inbounds.id = core_users.core_inbound_id").
		Where("core_inbounds.node_id = ?", nodeID).
		Order("core_users.id ASC").Find(&refs).Error
	return refs, err
}

// AddCoreUserDataBatch applies one increment per core user in bounded updates.
// The caller's context transaction covers every chunk when supplied.
func (r *coreRepository) AddCoreUserDataBatch(ctx context.Context, deltas []contract.CoreUsageDelta) error {
	byID := make(map[uint]int64, len(deltas))
	for _, delta := range deltas {
		byID[delta.CoreUserID] += delta.Bytes
	}
	ids := make([]uint, 0, len(byID))
	for id, bytes := range byID {
		if bytes != 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	exec := database.GetExecutor(r.db, ctx).WithContext(ctx)
	for start := 0; start < len(ids); start += trafficBatchSize {
		batchIDs := ids[start:min(start+trafficBatchSize, len(ids))]
		var sql strings.Builder
		sql.WriteString("data_used + CASE id")
		args := make([]interface{}, 0, 2*len(batchIDs))
		for _, id := range batchIDs {
			sql.WriteString(" WHEN ? THEN ?")
			args = append(args, id, byID[id])
		}
		sql.WriteString(" ELSE CAST(0 AS BIGINT) END")
		if err := exec.Model(&domain.CoreUser{}).Where("id IN ?", batchIDs).
			Update("data_used", gorm.Expr(sql.String(), args...)).Error; err != nil {
			return err
		}
	}
	return nil
}
