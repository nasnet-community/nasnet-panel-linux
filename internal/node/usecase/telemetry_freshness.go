package usecase

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

func getOnlineIPSnapshot(ctx context.Context, client agent.NodeClient) (*agent.OnlineIPSnapshot, error) {
	if source, ok := client.(interface {
		GetOnlineIPSnapshot(context.Context) (*agent.OnlineIPSnapshot, error)
	}); ok {
		return source.GetOnlineIPSnapshot(ctx)
	}
	users, err := client.GetAllUsersOnlineIPs(ctx)
	if err != nil {
		return nil, err
	}
	return &agent.OnlineIPSnapshot{Users: users}, nil
}

// Legacy agents observed data during the RPC. Future timestamps are clamped
// to receipt time so a node clock cannot keep an observation live indefinitely.
func telemetrySampleTime(collectedAtUnixMs int64, receivedAt time.Time) time.Time {
	if collectedAtUnixMs <= 0 {
		return receivedAt
	}
	observedAt := time.UnixMilli(collectedAtUnixMs)
	if observedAt.After(receivedAt) {
		return receivedAt
	}
	return observedAt
}
