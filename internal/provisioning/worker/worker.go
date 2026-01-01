package worker

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/metrics"
)

const (
	DefaultMaxRetries     = 10
	DefaultBaseBackoff    = 10 * time.Second
	DefaultBatchSize      = 20
	DefaultTickerInterval = 5 * time.Second
)

type Config struct {
	MaxRetries     int
	BaseBackoff    time.Duration
	BatchSize      int
	TickerInterval time.Duration
}

func DefaultConfig() Config {
	return Config{
		MaxRetries:     DefaultMaxRetries,
		BaseBackoff:    DefaultBaseBackoff,
		BatchSize:      DefaultBatchSize,
		TickerInterval: DefaultTickerInterval,
	}
}

type Worker struct {
	repo     repository.ProvisioningRepository
	nodeUC   nodeUC.NodeUsecase
	cfg      Config
	stopChan chan struct{}
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewWorker(repo repository.ProvisioningRepository, nodeUC nodeUC.NodeUsecase, cfg ...Config) *Worker {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0]
		if c.MaxRetries == 0 {
			c.MaxRetries = DefaultMaxRetries
		}
		if c.BaseBackoff == 0 {
			c.BaseBackoff = DefaultBaseBackoff
		}
		if c.BatchSize == 0 {
			c.BatchSize = DefaultBatchSize
		}
		if c.TickerInterval == 0 {
			c.TickerInterval = DefaultTickerInterval
		}
	}
	return &Worker{
		repo:     repo,
		nodeUC:   nodeUC,
		cfg:      c,
		stopChan: make(chan struct{}),
	}
}

// Start begins the polling loop
func (w *Worker) Start() {
	log := logger.GetLogger()
	log.Info("[Provisioning Worker] Started")

	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.cfg.TickerInterval)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopChan:
				log.Info("[Provisioning Worker] Stopping...")
				return
			case <-ticker.C:
				w.processBatch()
			}
		}
	}()
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() {
	w.cancel()
	close(w.stopChan)
	w.wg.Wait()
}

func (w *Worker) processBatch() {
	ctx := w.ctx
	log := logger.GetLogger()

	// Fetch pending tasks
	tasks, err := w.repo.FetchPending(ctx, w.cfg.BatchSize)
	if err != nil {
		log.WithError(err).Error("[Provisioning Worker] Failed to fetch tasks")
		return
	}

	if len(tasks) == 0 {
		return
	}

	// Sequential to bound DB connection use; semaphore-based concurrency is a future opt.
	for _, task := range tasks {
		w.processTask(ctx, task)
	}
}

func (w *Worker) processTask(ctx context.Context, task *domain.ProvisioningTask) {
	log := logger.GetLogger()

	// 1. Mark as processing
	if err := w.repo.UpdateStatus(ctx, task.ID, domain.StatusProcessing); err != nil {
		log.WithError(err).Errorf("Failed to mark task %d as processing", task.ID)
		return
	}

	var err error

	// 2. Execute Action
	if task.Type == domain.TypeAddUser {
		err = w.addUserToXray(ctx, task)
	} else if task.Type == domain.TypeRemoveUser {
		err = w.removeUserFromXray(ctx, task)
	} else {
		err = fmt.Errorf("unknown task type: %s", task.Type)
	}

	// 3. Handle Result
	if err == nil {
		log.Infof("[Provisioning] Task %d (%s %s) COMPLETED", task.ID, task.Type, task.UserEmail)
		w.repo.MarkSuccess(ctx, task.ID)
		if metrics.Registry != nil {
			metrics.ProvisioningTasksProcessed.WithLabelValues(string(task.Type), "success").Inc()
		}
	} else {
		w.handleFailure(ctx, task, err)
	}
}

func (w *Worker) addUserToXray(ctx context.Context, task *domain.ProvisioningTask) error {
	// WireGuard isn't an xray user — peers are provisioned on-demand as devices
	// (wg_peers + config push), not via AddUser. Nothing to do here.
	if strings.EqualFold(task.Protocol, "wireguard") {
		return nil
	}
	if task.NodeID == 0 {
		return fmt.Errorf("task %d has no NodeID", task.ID)
	}
	node, err := w.nodeUC.GetNode(ctx, task.NodeID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "record not found") {
			logger.GetLogger().Warnf("[Worker] Node %d not found. Aborting task %d (marked as success to clear queue).", task.NodeID, task.ID)
			return nil
		}
		return fmt.Errorf("failed to get node %d: %w", task.NodeID, err)
	}

	flow := task.UserFlow
	protocol := strings.ToLower(task.Protocol)
	if protocol == "" {
		protocol = "vless"
	}
	encryption := task.UserEncryption
	if encryption == "" && protocol == "vless" {
		encryption = "none"
	}

	err = w.nodeUC.AddUserViaAgent(ctx, node, task.InboundTag, task.UserEmail, task.UserUUID, protocol, flow, encryption)
	if err != nil && strings.Contains(err.Error(), "unsupported protocol") {
		// Agent build can't hot-add this protocol (e.g. an old hysteria2 agent);
		// fall back to a full config push, which includes every active account.
		logger.GetLogger().Infof("[Worker] Hot-add unsupported for protocol %q on %s; falling back to full config push", protocol, task.InboundTag)
		return w.nodeUC.PushConfigViaAgent(ctx, task.NodeID)
	}
	if err != nil && strings.Contains(err.Error(), "already exists") {
		// AddUser won't overwrite an existing email, so a stale user keeps its old
		// credentials (rotated UUID → EOF after restart). Remove, then re-add.
		logger.GetLogger().Infof("[Worker] User %s already present on %s; removing then re-adding to apply current credentials", task.UserEmail, task.InboundTag)
		if remErr := w.nodeUC.RemoveUserViaAgent(ctx, node, task.InboundTag, task.UserEmail); remErr != nil && !strings.Contains(remErr.Error(), "not found") {
			// Couldn't clear the stale user; fail so we retry instead of
			// reporting a success that never applied the new UUID.
			return fmt.Errorf("user %s already exists but remove for re-add failed: %w", task.UserEmail, remErr)
		}
		err = w.nodeUC.AddUserViaAgent(ctx, node, task.InboundTag, task.UserEmail, task.UserUUID, protocol, flow, encryption)
		if err != nil && strings.Contains(err.Error(), "already exists") {
			// A concurrent task re-added the same user; accept as done.
			return nil
		}
	}
	return err
}

func (w *Worker) removeUserFromXray(ctx context.Context, task *domain.ProvisioningTask) error {
	// WireGuard peers aren't xray users; removal is handled via the device flow.
	if strings.EqualFold(task.Protocol, "wireguard") {
		return nil
	}
	if task.NodeID == 0 {
		return fmt.Errorf("task %d has no NodeID", task.ID)
	}
	node, err := w.nodeUC.GetNode(ctx, task.NodeID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "record not found") {
			logger.GetLogger().Warnf("[Worker] Node %d not found. Aborting task %d (marked as success to clear queue).", task.NodeID, task.ID)
			return nil
		}
		return fmt.Errorf("failed to get node %d: %w", task.NodeID, err)
	}

	err = w.nodeUC.RemoveUserViaAgent(ctx, node, task.InboundTag, task.UserEmail)
	// "not found" is treated as success — the user is already absent on the node.
	if err != nil && strings.Contains(err.Error(), "not found") {
		return nil
	}
	if err != nil && strings.Contains(err.Error(), "unsupported protocol") {
		// Same fallback as add: a full config push drops the removed account.
		logger.GetLogger().Infof("[Worker] Hot-remove unsupported for protocol %q on %s; falling back to full config push", task.Protocol, task.InboundTag)
		return w.nodeUC.PushConfigViaAgent(ctx, task.NodeID)
	}
	return err
}

func (w *Worker) handleFailure(ctx context.Context, task *domain.ProvisioningTask, err error) {
	log := logger.GetLogger()

	isDead := task.RetryCount >= w.cfg.MaxRetries

	// Exponential backoff: BaseBackoff * 2^retry.
	backoffSeconds := math.Pow(2, float64(task.RetryCount)) * w.cfg.BaseBackoff.Seconds()
	nextRetry := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

	log.Warnf("[Provisioning] Task %d FAILED (Retry %d/%d): %v. Next retry: %s",
		task.ID, task.RetryCount+1, w.cfg.MaxRetries, err, nextRetry.Format("15:04:05"))

	if isDead {
		log.Errorf("[Provisioning] Task %d marked as DEAD. Manual intervention required.", task.ID)
	}

	if metrics.Registry != nil {
		metrics.ProvisioningTasksProcessed.WithLabelValues(string(task.Type), "failure").Inc()
	}

	w.repo.MarkFailed(ctx, task.ID, err.Error(), nextRetry, isDead)
}
