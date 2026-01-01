package worker

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/domain"
)

func newTestWorker(repo *mockProvisioningRepo, nodeUC *mockNodeUsecase) *Worker {
	w := NewWorker(repo, nodeUC)
	w.ctx, w.cancel = context.WithCancel(context.Background())
	return w
}

// TestProcessTask_AddUser_Success verifies that an ADD_USER task with a valid
// node is marked PROCESSING then COMPLETED, and AddUserViaAgent is called with
// the correct arguments.
func TestProcessTask_AddUser_Success(t *testing.T) {
	repo := newMockProvisioningRepo()
	nuc := newMockNodeUsecase()

	// Register a node
	nuc.nodes[1] = &nodeDomain.Node{ID: 1, Name: "test-node", IP: "1.2.3.4"}

	task := &domain.ProvisioningTask{
		ID:             1,
		AccountID:      100,
		NodeID:         1,
		Type:           domain.TypeAddUser,
		Status:         domain.StatusPending,
		InboundTag:     "vless-in",
		UserEmail:      "user@test.com",
		UserUUID:       "abc-123",
		UserFlow:       "xtls-rprx-vision",
		UserEncryption: "",
		Protocol:       "vless",
	}
	repo.tasks[task.ID] = task

	w := newTestWorker(repo, nuc)
	w.processTask(w.ctx, task)

	// Verify UpdateStatus was called with PROCESSING
	if len(repo.updateStatusCalls) == 0 {
		t.Fatal("expected UpdateStatus to be called")
	}
	if repo.updateStatusCalls[0].Status != domain.StatusProcessing {
		t.Errorf("expected status PROCESSING, got %s", repo.updateStatusCalls[0].Status)
	}

	// Verify MarkSuccess was called
	if len(repo.markSuccessCalls) != 1 {
		t.Fatalf("expected 1 MarkSuccess call, got %d", len(repo.markSuccessCalls))
	}
	if repo.markSuccessCalls[0] != task.ID {
		t.Errorf("MarkSuccess called with ID %d, want %d", repo.markSuccessCalls[0], task.ID)
	}

	// Verify AddUserViaAgent was called with correct args
	if len(nuc.addUserCalls) != 1 {
		t.Fatalf("expected 1 AddUserViaAgent call, got %d", len(nuc.addUserCalls))
	}
	call := nuc.addUserCalls[0]
	if call.NodeID != 1 {
		t.Errorf("AddUserViaAgent node ID = %d, want 1", call.NodeID)
	}
	if call.InboundTag != "vless-in" {
		t.Errorf("AddUserViaAgent inboundTag = %q, want %q", call.InboundTag, "vless-in")
	}
	if call.Email != "user@test.com" {
		t.Errorf("AddUserViaAgent email = %q, want %q", call.Email, "user@test.com")
	}
	if call.UUID != "abc-123" {
		t.Errorf("AddUserViaAgent uuid = %q, want %q", call.UUID, "abc-123")
	}

	// Verify no failure calls
	if len(repo.markFailedCalls) != 0 {
		t.Errorf("expected 0 MarkFailed calls, got %d", len(repo.markFailedCalls))
	}
}

// TestProcessTask_AddUser_AlreadyExists verifies idempotent handling:
// when AddUserViaAgent returns "already exists", the task is still completed.
func TestProcessTask_AddUser_AlreadyExists(t *testing.T) {
	repo := newMockProvisioningRepo()
	nuc := newMockNodeUsecase()

	nuc.nodes[1] = &nodeDomain.Node{ID: 1, Name: "test-node", IP: "1.2.3.4"}
	nuc.addUserErr = fmt.Errorf("user already exists on this inbound")

	task := &domain.ProvisioningTask{
		ID:         2,
		NodeID:     1,
		Type:       domain.TypeAddUser,
		Status:     domain.StatusPending,
		InboundTag: "vless-in",
		UserEmail:  "user@test.com",
		UserUUID:   "abc-123",
		Protocol:   "vless",
	}
	repo.tasks[task.ID] = task

	w := newTestWorker(repo, nuc)
	w.processTask(w.ctx, task)

	// "already exists" should be treated as success
	if len(repo.markSuccessCalls) != 1 {
		t.Fatalf("expected 1 MarkSuccess call (idempotent), got %d", len(repo.markSuccessCalls))
	}
	if len(repo.markFailedCalls) != 0 {
		t.Errorf("expected 0 MarkFailed calls, got %d", len(repo.markFailedCalls))
	}
}

// TestProcessTask_AddUser_Failure verifies that when AddUserViaAgent returns
// a non-idempotent error, the task is marked failed with the error message.
func TestProcessTask_AddUser_Failure(t *testing.T) {
	repo := newMockProvisioningRepo()
	nuc := newMockNodeUsecase()

	nuc.nodes[1] = &nodeDomain.Node{ID: 1, Name: "test-node", IP: "1.2.3.4"}
	nuc.addUserErr = fmt.Errorf("connection refused")

	task := &domain.ProvisioningTask{
		ID:         3,
		NodeID:     1,
		Type:       domain.TypeAddUser,
		Status:     domain.StatusPending,
		InboundTag: "vless-in",
		UserEmail:  "user@test.com",
		UserUUID:   "abc-123",
		Protocol:   "vless",
	}
	repo.tasks[task.ID] = task

	w := newTestWorker(repo, nuc)
	w.processTask(w.ctx, task)

	// Should NOT be marked as success
	if len(repo.markSuccessCalls) != 0 {
		t.Errorf("expected 0 MarkSuccess calls, got %d", len(repo.markSuccessCalls))
	}

	// Should be marked as failed
	if len(repo.markFailedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(repo.markFailedCalls))
	}
	fc := repo.markFailedCalls[0]
	if fc.ID != task.ID {
		t.Errorf("MarkFailed ID = %d, want %d", fc.ID, task.ID)
	}
	if fc.ErrStr != "connection refused" {
		t.Errorf("MarkFailed errStr = %q, want %q", fc.ErrStr, "connection refused")
	}
	if fc.IsDead {
		t.Error("task should not be marked DEAD on first failure (RetryCount=0)")
	}
}

// TestHandleFailure_ExponentialBackoff verifies that the exponential backoff
// calculation is correct: 10s * 2^RetryCount.
func TestHandleFailure_ExponentialBackoff(t *testing.T) {
	repo := newMockProvisioningRepo()
	task := &domain.ProvisioningTask{
		ID:         1,
		RetryCount: 3,
		Status:     domain.StatusProcessing,
	}
	repo.tasks[1] = task

	w := newTestWorker(repo, nil)
	before := time.Now()
	w.handleFailure(w.ctx, task, fmt.Errorf("timeout"))

	// Expected backoff: 10s * 2^3 = 80s
	expectedBackoff := math.Pow(2, 3) * DefaultBaseBackoff.Seconds()
	expectedRetry := before.Add(time.Duration(expectedBackoff) * time.Second)

	// Allow 2 second tolerance
	if task.NextRetryAt.Before(expectedRetry.Add(-2*time.Second)) || task.NextRetryAt.After(expectedRetry.Add(2*time.Second)) {
		t.Errorf("backoff wrong: expected ~%v, got %v", expectedRetry, task.NextRetryAt)
	}

	// Verify MarkFailed was called with correct parameters
	if len(repo.markFailedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(repo.markFailedCalls))
	}
	fc := repo.markFailedCalls[0]
	if fc.IsDead {
		t.Error("task with RetryCount=3 should not be DEAD")
	}
	if fc.ErrStr != "timeout" {
		t.Errorf("MarkFailed errStr = %q, want %q", fc.ErrStr, "timeout")
	}
}

// TestHandleFailure_MarksDeadAfterMaxRetries verifies that a task with
// RetryCount >= MaxRetries (10) is marked as DEAD.
func TestHandleFailure_MarksDeadAfterMaxRetries(t *testing.T) {
	repo := newMockProvisioningRepo()
	task := &domain.ProvisioningTask{
		ID:         1,
		RetryCount: DefaultMaxRetries, // 10 — at the limit
		Status:     domain.StatusProcessing,
	}
	repo.tasks[1] = task

	w := newTestWorker(repo, nil)
	w.handleFailure(w.ctx, task, fmt.Errorf("persistent failure"))

	if len(repo.markFailedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(repo.markFailedCalls))
	}
	fc := repo.markFailedCalls[0]
	if !fc.IsDead {
		t.Error("task with RetryCount >= MaxRetries should be marked DEAD")
	}

	// Also verify the task object was updated by the mock
	if task.Status != domain.StatusDead {
		t.Errorf("task status = %s, want %s", task.Status, domain.StatusDead)
	}
}

// TestProcessTask_NodeNotFound_MarkedAsSuccess documents current behavior:
// when the target node is not found, the task is marked as success to clear the queue.
func TestProcessTask_NodeNotFound_MarkedAsSuccess(t *testing.T) {
	repo := newMockProvisioningRepo()
	nuc := newMockNodeUsecase()
	// Node 999 does NOT exist in nuc.nodes — GetNode will return ErrNodeNotFound

	task := &domain.ProvisioningTask{
		ID:         4,
		NodeID:     999,
		Type:       domain.TypeAddUser,
		Status:     domain.StatusPending,
		InboundTag: "vless-in",
		UserEmail:  "user@test.com",
		UserUUID:   "abc-123",
		Protocol:   "vless",
	}
	repo.tasks[task.ID] = task

	w := newTestWorker(repo, nuc)
	w.processTask(w.ctx, task)

	// Node not found -> addUserToXray returns nil -> MarkSuccess
	if len(repo.markSuccessCalls) != 1 {
		t.Fatalf("expected 1 MarkSuccess call (node not found -> success), got %d", len(repo.markSuccessCalls))
	}
	if len(repo.markFailedCalls) != 0 {
		t.Errorf("expected 0 MarkFailed calls, got %d", len(repo.markFailedCalls))
	}

	// AddUserViaAgent should NOT have been called
	if len(nuc.addUserCalls) != 0 {
		t.Errorf("expected 0 AddUserViaAgent calls when node not found, got %d", len(nuc.addUserCalls))
	}
}

// TestProcessBatch_ProcessesMultipleTasks verifies that processBatch fetches
// pending tasks and processes all of them to completion.
func TestProcessBatch_ProcessesMultipleTasks(t *testing.T) {
	repo := newMockProvisioningRepo()
	nuc := newMockNodeUsecase()

	nuc.nodes[1] = &nodeDomain.Node{ID: 1, Name: "node-1", IP: "1.2.3.4"}

	tasks := []*domain.ProvisioningTask{
		{
			ID:         10,
			NodeID:     1,
			Type:       domain.TypeAddUser,
			Status:     domain.StatusPending,
			InboundTag: "vless-in",
			UserEmail:  "user1@test.com",
			UserUUID:   "uuid-1",
			Protocol:   "vless",
		},
		{
			ID:         11,
			NodeID:     1,
			Type:       domain.TypeAddUser,
			Status:     domain.StatusPending,
			InboundTag: "vless-in",
			UserEmail:  "user2@test.com",
			UserUUID:   "uuid-2",
			Protocol:   "vless",
		},
		{
			ID:         12,
			NodeID:     1,
			Type:       domain.TypeAddUser,
			Status:     domain.StatusPending,
			InboundTag: "vmess-in",
			UserEmail:  "user3@test.com",
			UserUUID:   "uuid-3",
			Protocol:   "vmess",
		},
	}

	// Store tasks in the repo and set them as the FetchPending result
	for _, task := range tasks {
		repo.tasks[task.ID] = task
	}
	repo.fetchPendingResult = tasks

	w := newTestWorker(repo, nuc)
	w.processBatch()

	// All 3 tasks should be marked as success
	if len(repo.markSuccessCalls) != 3 {
		t.Fatalf("expected 3 MarkSuccess calls, got %d", len(repo.markSuccessCalls))
	}

	// All 3 should have been marked PROCESSING first
	if len(repo.updateStatusCalls) != 3 {
		t.Fatalf("expected 3 UpdateStatus calls, got %d", len(repo.updateStatusCalls))
	}
	for i, call := range repo.updateStatusCalls {
		if call.Status != domain.StatusProcessing {
			t.Errorf("UpdateStatus[%d] status = %s, want PROCESSING", i, call.Status)
		}
	}

	// AddUserViaAgent should have been called 3 times
	if len(nuc.addUserCalls) != 3 {
		t.Fatalf("expected 3 AddUserViaAgent calls, got %d", len(nuc.addUserCalls))
	}

	// Verify all task IDs were processed
	successIDs := make(map[uint]bool)
	for _, id := range repo.markSuccessCalls {
		successIDs[id] = true
	}
	for _, task := range tasks {
		if !successIDs[task.ID] {
			t.Errorf("task %d was not marked as success", task.ID)
		}
	}
}
