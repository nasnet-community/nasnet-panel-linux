package repository

import (
	"context"
	"sort"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

// Each peer uses at most eight parameters plus one batch UpdatedAt, below SQLite's
// conservative 999-parameter limit while also bounding PostgreSQL statements.
const peerTrafficBatchSize = 50

// AddUsageBatch increments peer counters without reading them first. Callers
// should supply a transaction context when all chunks must commit together.
func (r *wgPeerRepository) AddUsageBatch(ctx context.Context, deltas []contract.WGUsageDelta) error {
	merged := make(map[uint]contract.WGUsageDelta, len(deltas))
	for _, delta := range deltas {
		current := merged[delta.PeerID]
		current.PeerID = delta.PeerID
		current.Upload += delta.Upload
		current.Download += delta.Download
		if delta.LastSeen.After(current.LastSeen) {
			current.LastSeen = delta.LastSeen
		}
		merged[delta.PeerID] = current
	}
	ids := make([]uint, 0, len(merged))
	for id, delta := range merged {
		if delta.Upload != 0 || delta.Download != 0 || !delta.LastSeen.IsZero() {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	exec := database.GetExecutor(r.db, ctx)
	for start := 0; start < len(ids); start += peerTrafficBatchSize {
		batchIDs := ids[start:min(start+peerTrafficBatchSize, len(ids))]
		var upload, download, lastSeen strings.Builder
		upload.WriteString("up_bytes + CASE id")
		download.WriteString("down_bytes + CASE id")
		lastSeen.WriteString("CASE id")
		uploadArgs := make([]any, 0, len(batchIDs)*2)
		downloadArgs := make([]any, 0, len(batchIDs)*2)
		lastSeenArgs := make([]any, 0, len(batchIDs)*3)
		for _, id := range batchIDs {
			delta := merged[id]
			upload.WriteString(" WHEN ? THEN ?")
			uploadArgs = append(uploadArgs, id, delta.Upload)
			download.WriteString(" WHEN ? THEN ?")
			downloadArgs = append(downloadArgs, id, delta.Download)
			if !delta.LastSeen.IsZero() {
				lastSeen.WriteString(" WHEN ? THEN CASE WHEN last_seen IS NULL OR last_seen < ? THEN ? ELSE last_seen END")
				lastSeenArgs = append(lastSeenArgs, id, delta.LastSeen, delta.LastSeen)
			}
		}
		upload.WriteString(" ELSE CAST(0 AS BIGINT) END")
		download.WriteString(" ELSE CAST(0 AS BIGINT) END")
		updates := map[string]any{
			"up_bytes":   gorm.Expr(upload.String(), uploadArgs...),
			"down_bytes": gorm.Expr(download.String(), downloadArgs...),
		}
		if len(lastSeenArgs) > 0 {
			lastSeen.WriteString(" ELSE last_seen END")
			updates["last_seen"] = gorm.Expr(lastSeen.String(), lastSeenArgs...)
		}
		if err := exec.Model(&domain.WGPeer{}).Where("id IN ?", batchIDs).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}
