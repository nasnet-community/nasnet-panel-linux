package monitor

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/notification"
)

type NodeRepository interface {
	ListNodes(ctx context.Context) ([]*domain.Node, error)
}

type NodeHealthChecker interface {
	CheckNodeHealth(ctx context.Context, id uint) (*domain.NodeHealth, error)
}

// heartbeatSessionProbe is satisfied by the node usecase. When available, the
// monitor skips nodes that already have a live heartbeat stream: two detectors
// at 8s and 10s on the same node race, and the paired flaps they produce are
// noise no debounce can fully absorb. Optional so the monitor still works
// standalone (tests, or a build without the heartbeat manager).
type heartbeatSessionProbe interface {
	HasLiveHeartbeatSession(nodeID uint) bool
}

// NodeStatsSyncer interface removed

type MonitorService struct {
	nodeRepo      NodeRepository
	healthChecker NodeHealthChecker
	// statsSyncer removed
	notifier notification.Notifier
	interval time.Duration
	eventBus *events.EventBus
}

func NewMonitorService(
	nodeRepo NodeRepository,
	healthChecker NodeHealthChecker,
	// statsSyncer removed
	notifier notification.Notifier,
	interval time.Duration,
	eventBus *events.EventBus,
) *MonitorService {
	if interval < 3*time.Second {
		interval = 1 * time.Minute
	}
	return &MonitorService{
		nodeRepo:      nodeRepo,
		healthChecker: healthChecker,
		notifier:      notifier,
		interval:      interval,
		eventBus:      eventBus,
	}
}

func (s *MonitorService) Start(ctx context.Context) {
	log := logger.GetLogger()
	log.Infof("Starting Server Monitor Service (Interval: %s)", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run immediately on start
	s.checkNodes(ctx)
	// s.syncStats(ctx) removed

	for {
		select {
		case <-ctx.Done():
			log.Info("Server Monitor Service stopped")
			return
		case <-ticker.C:
			s.checkNodes(ctx)
			// s.syncStats(ctx) removed
		}
	}
}

// syncStats removed

func (s *MonitorService) checkNodes(ctx context.Context) {
	log := logger.GetLogger()
	nodes, err := s.nodeRepo.ListNodes(ctx)
	if err != nil {
		log.Errorf("Monitor: Failed to list nodes: %v", err)
		return
	}

	probe, hasProbe := s.healthChecker.(heartbeatSessionProbe)

	for _, node := range nodes {
		// Only check active nodes
		if !node.IsActive {
			continue
		}
		// A live heartbeat stream is already reporting this node — polling it
		// would only add a second, slower opinion.
		if hasProbe && probe.HasLiveHeartbeatSession(node.ID) {
			continue
		}

		// The health check reports the result through the connectivity writer,
		// which owns is_online, the transition history and the SSE/alert event.
		// Nothing to do here but drive the poll for sessionless nodes.
		if _, err := s.healthChecker.CheckNodeHealth(ctx, node.ID); err != nil {
			log.Debugf("Monitor: health check for node %s failed to run: %v", node.Name, err)
		}
	}
}
