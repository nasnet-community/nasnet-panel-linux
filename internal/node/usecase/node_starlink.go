package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// GetStarlinkStatus retrieves live Starlink dish status via the node's agent.
func (u *nodeUsecase) GetStarlinkStatus(ctx context.Context, nodeID uint) (*agent.StarlinkStatus, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	settings := node.GetStarlinkSettingsOrDefault()
	if !settings.Enabled {
		return nil, fmt.Errorf("starlink monitoring is not enabled on this node")
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	return client.GetStarlinkStatus(ctx, settings.DishAddress)
}

// GetStarlinkObstructionMap retrieves the live obstruction map via the node's agent.
func (u *nodeUsecase) GetStarlinkObstructionMap(ctx context.Context, nodeID uint) (*agent.StarlinkObstructionMap, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	settings := node.GetStarlinkSettingsOrDefault()
	if !settings.Enabled {
		return nil, fmt.Errorf("starlink monitoring is not enabled on this node")
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	return client.GetStarlinkObstructionMap(ctx, settings.DishAddress)
}

// GetStarlinkHistory returns historical Starlink stats from the database.
func (u *nodeUsecase) GetStarlinkHistory(ctx context.Context, nodeID uint, limit int, since *time.Time) ([]*domain.StarlinkStat, error) {
	if limit <= 0 {
		limit = 60
	}
	if limit > 1000 {
		limit = 1000
	}
	return u.nodeRepo.GetStarlinkStatsHistory(ctx, nodeID, limit, since)
}

// starlinkSyncTimeout: 3s inner ceiling so a wedged dish can't starve
// the 10s per-node sweep. Covers status + context + location RPCs.
const starlinkSyncTimeout = 3 * time.Second

// syncStarlinkStats collects a Starlink stat snapshot for a node (called from SyncNodeStats).
func (u *nodeUsecase) syncStarlinkStats(ctx context.Context, node *domain.Node, client agent.NodeClient) {
	settings := node.GetStarlinkSettingsOrDefault()
	if !settings.Enabled {
		return
	}

	log := logger.GetLogger().WithField("node", node.Name)

	pollCtx, cancel := context.WithTimeout(ctx, starlinkSyncTimeout)
	defer cancel()

	status, err := client.GetStarlinkStatus(pollCtx, settings.DishAddress)
	if err != nil {
		log.Debugf("Failed to get starlink status: %v", err)
		return
	}

	if !status.Available {
		return
	}

	stat := &domain.StarlinkStat{
		NodeID:                node.ID,
		DownlinkThroughputBps: status.DownlinkThroughputBps,
		UplinkThroughputBps:   status.UplinkThroughputBps,
		PopPingLatencyMs:      status.PopPingLatencyMs,
		PopPingDropRate:       status.PopPingDropRate,
		ObstructionFraction:   status.ObstructionFraction,
		CurrentlyObstructed:   status.CurrentlyObstructed,
		TiltAngleDeg:          status.TiltAngleDeg,
		BoresightAzimuthDeg:   status.BoresightAzimuthDeg,
		BoresightElevationDeg: status.BoresightElevationDeg,
		GpsValid:              status.GpsValid,
		AlertFlags: domain.AlertFlagsFromBooleans(
			status.AlertThermalShutdown, status.AlertThermalThrottle,
			status.AlertMotorsStuck, status.AlertNoEthernetLink,
			status.AlertIsHeating, status.AlertSlowEthernet,
			status.AlertPowerSaveIdle, status.AlertMastNotNearVert,
			status.AlertRoaming, status.AlertUnexpectedLocation,
			status.AlertInstallPending,
		),
	}

	if err := u.nodeRepo.CreateStarlinkStat(ctx, stat); err != nil {
		log.Warnf("Failed to save starlink stat: %v", err)
	}
}
