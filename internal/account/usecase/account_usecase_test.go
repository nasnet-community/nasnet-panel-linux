package usecase

import (
	"context"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// stubAccountRepo embeds the full AccountRepository interface so it
// satisfies the type with zero method spelling. Only FindByID is
// overridden; any other repo method the code-under-test reaches would
// nil-panic — which is the point, since the new SyncAccountStats body
// should touch nothing but FindByID + the node-sweep trigger.
type stubAccountRepo struct {
	repository.AccountRepository
	byID map[uint]*domain.Account
}

func (s *stubAccountRepo) FindByID(_ context.Context, id uint) (*domain.Account, error) {
	if acc, ok := s.byID[id]; ok {
		return acc, nil
	}
	return nil, ErrAccountNotFound
}

// fakeNodeSync records every node ID passed to SyncSingleNodeByID so
// the test can assert the delegation happened exactly once.
type fakeNodeSync struct {
	mu    sync.Mutex
	calls []uint
}

func (f *fakeNodeSync) SyncSingleNodeByID(_ context.Context, nodeID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, nodeID)
	return nil
}

// TestSyncAccountStats_DelegatesToNodeSweep verifies the new contract:
// SyncAccountStats must pull the account's node ID and delegate to
// NodeSyncTrigger.SyncSingleNodeByID; it must NOT call grpcClient —
// which is why grpcClient is left nil below (a regression reaching for
// it would nil-panic, failing the test loudly).
func TestSyncAccountStats_DelegatesToNodeSweep(t *testing.T) {
	fake := &fakeNodeSync{}
	repo := &stubAccountRepo{
		byID: map[uint]*domain.Account{
			42: {
				ID: 42,
				Inbound: &nodeDomain.Inbound{
					ID:   7,
					Node: &nodeDomain.Node{ID: 99, IsActive: true},
				},
			},
		},
	}

	uc := &accountUsecase{
		accountRepo: repo,
		nodeSync:    fake,
		// grpcClient deliberately nil — must not be reached.
	}

	if err := uc.SyncAccountStats(context.Background(), 42); err != nil {
		t.Fatalf("SyncAccountStats returned unexpected error: %v", err)
	}

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 sweep call, got %d (%v)", len(fake.calls), fake.calls)
	}
	if fake.calls[0] != 99 {
		t.Errorf("expected sweep for node 99, got %d", fake.calls[0])
	}
}

// TestSyncAccountStats_AccountNotFound maps the repo ErrAccountNotFound
// surface correctly — no nodeSync call for a bogus id.
func TestSyncAccountStats_AccountNotFound(t *testing.T) {
	fake := &fakeNodeSync{}
	repo := &stubAccountRepo{byID: map[uint]*domain.Account{}}
	uc := &accountUsecase{accountRepo: repo, nodeSync: fake}

	if err := uc.SyncAccountStats(context.Background(), 999); err != ErrAccountNotFound {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected no sweep calls for missing account, got %v", fake.calls)
	}
}

// TestSyncAccountStats_MissingInbound returns a sentinel error without
// hitting the sweep trigger.
func TestSyncAccountStats_MissingInbound(t *testing.T) {
	fake := &fakeNodeSync{}
	repo := &stubAccountRepo{
		byID: map[uint]*domain.Account{
			1: {ID: 1, Inbound: nil},
		},
	}
	uc := &accountUsecase{accountRepo: repo, nodeSync: fake}

	if err := uc.SyncAccountStats(context.Background(), 1); err == nil {
		t.Fatalf("expected error for missing inbound, got nil")
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected no sweep calls when inbound missing, got %v", fake.calls)
	}
}

// TestSyncAccountStats_NodeSyncNotWired exercises the bootstrap-guard
// branch: even with a valid account, a nil nodeSync must surface an
// error rather than nil-panic in production.
func TestSyncAccountStats_NodeSyncNotWired(t *testing.T) {
	repo := &stubAccountRepo{
		byID: map[uint]*domain.Account{
			1: {
				ID: 1,
				Inbound: &nodeDomain.Inbound{
					ID:   2,
					Node: &nodeDomain.Node{ID: 3, IsActive: true},
				},
			},
		},
	}
	uc := &accountUsecase{accountRepo: repo, nodeSync: nil}

	if err := uc.SyncAccountStats(context.Background(), 1); err == nil {
		t.Fatalf("expected error when nodeSync not wired, got nil")
	}
}
