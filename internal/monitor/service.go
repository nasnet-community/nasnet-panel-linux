package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/notification"
)

type NodeRepository interface {
	ListNodes(ctx context.Context) ([]*domain.Node, error)
	UpdateNodeStatus(ctx context.Context, id uint, isOnline bool, lastCheck time.Time) error
}

type NodeHealthChecker interface {
	CheckNodeHealth(ctx context.Context, id uint) (*domain.NodeHealth, error)
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

	for _, node := range nodes {
		// Only check active nodes
		if !node.IsActive {
			continue
		}

		var isOnline bool
		var checkErr error

		res, err := s.healthChecker.CheckNodeHealth(ctx, node.ID)
		if err != nil {
			isOnline = false
			checkErr = err
		} else {
			isOnline = res.Healthy
			if !res.Healthy {
				checkErr = fmt.Errorf("%s", res.Message)
			}
		}

		// Determine if status changed
		statusChanged := node.IsOnline != isOnline

		// Update node status
		// Critical Fix: Use UpdateNodeStatus instead of UpdateNode/Save to avoid resurrecting soft-deleted nodes
		// if a deletion happened concurrently. Save() overwrites DeletedAt with nil.
		node.IsOnline = isOnline
		node.LastCheck = time.Now()

		if updateErr := s.nodeRepo.UpdateNodeStatus(ctx, node.ID, isOnline, node.LastCheck); updateErr != nil {
			log.Errorf("Monitor: Failed to update node %s status: %v", node.Name, updateErr)
		}

		if statusChanged {
			payload := events.NodeStatusPayload{
				NodeID:   node.ID,
				NodeName: node.Name,
				IP:       node.IP,
				IsOnline: isOnline,
			}

			if isOnline {
				log.Infof("Monitor: Node %s is back ONLINE", node.Name)
				if s.eventBus != nil {
					s.eventBus.Publish(events.Event{
						Type:      events.EventNodeOnline,
						Timestamp: time.Now(),
						Payload:   payload,
					})
				}
			} else {
				log.Warnf("Monitor: Node %s is OFFLINE: %v", node.Name, checkErr)
				payload.Message = checkErr.Error()
				if s.eventBus != nil {
					s.eventBus.Publish(events.Event{
						Type:      events.EventNodeOffline,
						Timestamp: time.Now(),
						Payload:   payload,
					})
				}
			}
		}
	}
}
