package scheduler

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// reconcileNodes iterates through active nodes and ensures their inbounds match the DB
func (s *Scheduler) reconcileNodes(ctx context.Context) {
	log := logger.GetLogger()
	nodes, err := s.nodeUsecase.ListNodes(ctx)
	if err != nil {
		log.WithError(err).Error("Scheduler: Failed to list nodes")
		return
	}

	for _, node := range nodes {
		if !node.IsActive {
			continue
		}

		res, err := s.nodeUsecase.SyncInbounds(ctx, node.ID)
		if err != nil {
			log.WithField("node", node.Name).WithError(err).Warn("Scheduler: Failed to sync inbounds")
			continue
		}

		if res.Restored > 0 || res.Imported > 0 {
			log.WithFields(map[string]interface{}{
				"node":     node.Name,
				"restored": res.Restored,
				"imported": res.Imported,
			}).Info("Scheduler: Node Inbounds Reconciled (Server Recovery)")
		}
	}
}

// syncAgentNodes syncs configuration (including users) to Agent nodes on startup
func (s *Scheduler) syncAgentNodes(ctx context.Context) {
	log := logger.GetLogger()
	nodes, err := s.nodeUsecase.ListNodes(ctx)
	if err != nil {
		log.WithError(err).Error("Scheduler: Failed to list nodes for agent sync")
		return
	}

	for _, node := range nodes {
		if !node.IsActive {
			continue
		}

		log.WithField("node", node.Name).Info("Scheduler: Syncing Agent Node (Startup)...")
		if _, err := s.nodeUsecase.SyncInbounds(ctx, node.ID); err != nil {
			log.WithField("node", node.Name).WithError(err).Warn("Scheduler: Failed to sync agent node")
		} else {
			log.WithField("node", node.Name).Info("Scheduler: Agent Node Synced Successfully")
		}
	}
}
