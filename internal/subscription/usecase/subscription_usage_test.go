package usecase

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// ─── UpdateDataUsage ────────────────────────────────────────────────────

// TestUpdateDataUsage_PositiveDelta_AddsToLifetime: when bytesUsed is
// higher than the current DataUsed, the positive delta is added to
// lifetime total. Current write is unconditional.
func TestUpdateDataUsage_PositiveDelta_AddsToLifetime(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{ID: 1, DataUsed: 100})

	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())
	if err := uc.UpdateDataUsage(context.Background(), 1, 250); err != nil {
		t.Fatalf("update: %v", err)
	}

	if got := subRepo.lifetimeAddCalls[1]; got != 150 {
		t.Errorf("lifetime delta: want 150, got %d", got)
	}
	if got := subRepo.dataUsedWrites[1]; got != 250 {
		t.Errorf("DataUsed write: want 250, got %d", got)
	}
}

// TestUpdateDataUsage_NegativeDelta_NoLifetimeAdd: admin reset (e.g.
// bytes<current) should not negatively adjust lifetime — it's a
// monotonic counter. DataUsed still takes the new value.
func TestUpdateDataUsage_NegativeDelta_NoLifetimeAdd(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{ID: 1, DataUsed: 500})

	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())
	if err := uc.UpdateDataUsage(context.Background(), 1, 200); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, ok := subRepo.lifetimeAddCalls[1]; ok {
		t.Errorf("lifetime must not be touched on negative delta, got %+v", subRepo.lifetimeAddCalls)
	}
	if got := subRepo.dataUsedWrites[1]; got != 200 {
		t.Errorf("DataUsed write: want 200, got %d", got)
	}
}

// TestUpdateDataUsage_EqualValue_NoLifetimeAdd: same-value update is
// common when stats poll reports no change. Must not trip lifetime.
func TestUpdateDataUsage_EqualValue_NoLifetimeAdd(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{ID: 1, DataUsed: 300})
	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())

	if err := uc.UpdateDataUsage(context.Background(), 1, 300); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, ok := subRepo.lifetimeAddCalls[1]; ok {
		t.Error("equal-value update must not add to lifetime")
	}
}

// ─── CheckAndExpireSubscriptions ──────────────────────────────────────

// TestCheckAndExpire_EmptyList_NoOp: repo returns no expired subs → no
// status updates issued.
func TestCheckAndExpire_EmptyList_NoOp(t *testing.T) {
	subRepo := newMockSubscriptionRepo()
	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())
	if err := uc.CheckAndExpireSubscriptions(context.Background()); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if len(subRepo.statusLog) != 0 {
		t.Errorf("no subs expired → no status writes, got %+v", subRepo.statusLog)
	}
}

// TestCheckAndExpire_SingleExpired_Transitions: one expired sub without
// auto-renew should transition to Expired status.
func TestCheckAndExpire_SingleExpired_Transitions(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	sub := &domain.Subscription{
		ID:      1,
		UserID:  ptrUint(10),
		Status:  domain.SubscriptionStatusActive,
		EndDate: &past,
	}

	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(sub)
	subRepo.expiredSubs = []*domain.Subscription{sub}

	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())
	if err := uc.CheckAndExpireSubscriptions(context.Background()); err != nil {
		t.Fatalf("expire: %v", err)
	}

	found := false
	for _, e := range subRepo.statusLog {
		if e.ID == 1 && e.Status == domain.SubscriptionStatusExpired {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Expired status update, got %+v", subRepo.statusLog)
	}
}

// ─── CheckAndExpireByDataLimit ────────────────────────────────────────

func TestCheckAndExpireByDataLimit_TransitionsToTrafficExhausted(t *testing.T) {
	sub := &domain.Subscription{
		ID:        1,
		UserID:    ptrUint(10),
		Status:    domain.SubscriptionStatusActive,
		DataUsed:  1_000_000_000, // 1 GB
		DataLimit: 500_000_000,   // 500 MB
	}

	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(sub)
	subRepo.exhaustedSubs = []*domain.Subscription{sub}

	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())
	if err := uc.CheckAndExpireByDataLimit(context.Background()); err != nil {
		t.Fatalf("exhaust: %v", err)
	}

	found := false
	for _, e := range subRepo.statusLog {
		if e.ID == 1 && e.Status == domain.SubscriptionStatusTrafficExhausted {
			found = true
		}
	}
	if !found {
		t.Errorf("expected TrafficExhausted status, got %+v", subRepo.statusLog)
	}
}

// ─── SetCustomDataLimit ───────────────────────────────────────────────

// TestSetCustomDataLimit_NotFound: sentinel error for missing sub.
func TestSetCustomDataLimit_NotFound(t *testing.T) {
	uc := newCancelTestUsecase(newMockSubscriptionRepo(), newMockAccountMgr())
	gb := 10.0
	if err := uc.SetCustomDataLimit(context.Background(), 999, &gb); err != ErrSubscriptionNotFound {
		t.Errorf("want ErrSubscriptionNotFound, got %v", err)
	}
}

// Exhausted sub + new higher limit fitting current usage → reactivate.
func TestSetCustomDataLimit_DoesntReactivateWhenAboveNewLimit(t *testing.T) {
	// DataUsed=1GB, new limit=500MB → still exhausted, no reactivation.
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{
		ID:       1,
		UserID:   ptrUint(10),
		Status:   domain.SubscriptionStatusTrafficExhausted,
		DataUsed: 1_000_000_000,
	})

	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())
	limit := 0.5 // 500MB
	if err := uc.SetCustomDataLimit(context.Background(), 1, &limit); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Sub should remain TrafficExhausted (no re-activation attempt).
	for _, e := range subRepo.statusLog {
		if e.Status == domain.SubscriptionStatusActive {
			t.Errorf("should not reactivate when DataUsed > new limit: %+v", subRepo.statusLog)
		}
	}
}

// ─── SetCustomBandwidthLimit ──────────────────────────────────────────

func TestSetCustomBandwidthLimit_NotFound(t *testing.T) {
	uc := newCancelTestUsecase(newMockSubscriptionRepo(), newMockAccountMgr())
	mbps := 100
	if err := uc.SetCustomBandwidthLimit(context.Background(), 999, &mbps); err != ErrSubscriptionNotFound {
		t.Errorf("want ErrSubscriptionNotFound, got %v", err)
	}
}

func TestSetCustomBandwidthLimit_ZeroMeansUnlimited(t *testing.T) {
	// Pass pointer to 0 — semantic "unlimited", not "reset to default".
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{ID: 1})
	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())

	unlimited := 0
	if err := uc.SetCustomBandwidthLimit(context.Background(), 1, &unlimited); err != nil {
		t.Errorf("zero-pointer unlimited should be accepted, got %v", err)
	}
}

func TestSetCustomBandwidthLimit_NilResets(t *testing.T) {
	// nil pointer — reset to plan default.
	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{ID: 1})
	uc := newCancelTestUsecase(subRepo, newMockAccountMgr())

	if err := uc.SetCustomBandwidthLimit(context.Background(), 1, nil); err != nil {
		t.Errorf("nil-pointer reset should succeed, got %v", err)
	}
}

// ─── GetSubscriptionUsageTrend ───────────────────────────────────────────────

// newTrendTestUsecase reuses the existing cancel-test helper; both paths only
// need subRepo wired — all other deps can be nil/zero.
func newTrendTestUsecase(subRepo *mockSubscriptionRepo) SubscriptionUsecase {
	return newCancelTestUsecase(subRepo, newMockAccountMgr())
}

func TestGetSubscriptionUsageTrend_Valid7dWithMixedRows(t *testing.T) {
	subRepo := &mockSubscriptionRepo{}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	up := int64(1_000_000)
	dn := int64(4_000_000)
	subRepo.dailyRangeRows = []*domain.SubscriptionDailyUsage{
		{SubscriptionID: 1, Date: today.AddDate(0, 0, -2), DataUsed: 3_000_000, DataUpload: nil, DataDownload: nil},
		{SubscriptionID: 1, Date: today.AddDate(0, 0, -1), DataUsed: up + dn, DataUpload: &up, DataDownload: &dn},
	}
	u := newTrendTestUsecase(subRepo)

	got, err := u.GetSubscriptionUsageTrend(context.Background(), 1, 7)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Range != "7d" {
		t.Fatalf("expected range 7d, got %q", got.Range)
	}
	if len(got.Points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(got.Points))
	}
	if got.Points[0].Upload != nil || got.Points[0].Download != nil {
		t.Fatalf("expected legacy row nil splits, got up=%v dn=%v", got.Points[0].Upload, got.Points[0].Download)
	}
	if got.Points[1].Upload == nil || *got.Points[1].Upload != up {
		t.Fatalf("expected upload %d, got %v", up, got.Points[1].Upload)
	}
	if got.UnitHint != "MB" {
		t.Fatalf("expected MB hint for 5MB max, got %q", got.UnitHint)
	}
}

func TestGetSubscriptionUsageTrend_InvalidRange(t *testing.T) {
	u := newTrendTestUsecase(&mockSubscriptionRepo{})
	if _, err := u.GetSubscriptionUsageTrend(context.Background(), 1, 14); err == nil {
		t.Fatalf("expected error for range=14, got nil")
	}
}

func TestGetSubscriptionUsageTrend_EmptySubscription(t *testing.T) {
	subRepo := &mockSubscriptionRepo{}
	u := newTrendTestUsecase(subRepo)
	got, err := u.GetSubscriptionUsageTrend(context.Background(), 1, 30)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got.Points) != 0 {
		t.Fatalf("expected 0 points, got %d", len(got.Points))
	}
	if got.UnitHint != "KB" {
		t.Fatalf("expected KB for empty range, got %q", got.UnitHint)
	}
}

// ─── SyncUsageFromXray ───────────────────────────────────────────────────────

// fakeNodeUC: embeds full NodeUsecase, only SyncSingleNodeByID stubbed.
// Other calls nil-panic loudly on regression.
type fakeNodeUC struct {
	nodeUC.NodeUsecase
	mu        sync.Mutex
	syncedIDs []uint
}

func (f *fakeNodeUC) SyncSingleNodeByID(_ context.Context, nodeID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncedIDs = append(f.syncedIDs, nodeID)
	return nil
}

// newSyncUsageTestUsecase wires a subscription usecase with the supplied
// fakeNodeUC and nil grpcClient. If the code-under-test attempts to call
// grpcClient it will nil-panic — the test asserts that path is dead.
//
// acctMgr is injected rather than constructed here because SyncUsageFromXray
// discovers nodes through the subscription's accounts, so the test needs to seed
// it.
func newSyncUsageTestUsecase(subRepo *mockSubscriptionRepo, fake *fakeNodeUC, acctMgr *mockAccountMgr) SubscriptionUsecase {
	return NewSubscriptionUsecase(
		subRepo,
		newMockSubUserRepo(),
		&mockNodeRepo{},
		nil, // providerFactory
		nil, // grpcClient — MUST NOT be called
		fake,
		nil,                  // nodeSyncer
		acctMgr,              // accountManager
		&mockAccountReader{}, // accountReader
		&mockSubTxManager{},
		events.NewEventBus(),
	)
}

// TestSyncUsageFromXray_TriggersPerNodeSweep: SyncUsageFromXray must call
// SyncSingleNodeByID per distinct node and NOT grpcClient.GetUserStats
// (grpcClient nil above → any call nil-panics).
func TestSyncUsageFromXray_TriggersPerNodeSweep(t *testing.T) {
	// Two nodes, two accounts on node 10, one on node 11.
	// The second account on node 10 must dedupe to a single sweep call.
	node10 := &nodeDomain.Node{ID: 10, IsActive: true}
	node11 := &nodeDomain.Node{ID: 11, IsActive: true}

	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{
		ID:          1,
		Status:      domain.SubscriptionStatusActive,
		ConfigEmail: "sub1@nasnet",
	})

	// Nodes are reached via the subscription's accounts.
	acctMgr := newMockAccountMgr()
	acctMgr.accounts[1] = []*accountDomain.Account{
		{Inbound: &nodeDomain.Inbound{ID: 100, NodeID: 10, Node: node10}},
		{Inbound: &nodeDomain.Inbound{ID: 101, NodeID: 10, Node: node10}},
		{Inbound: &nodeDomain.Inbound{ID: 200, NodeID: 11, Node: node11}},
	}

	fake := &fakeNodeUC{}
	uc := newSyncUsageTestUsecase(subRepo, fake, acctMgr)

	if err := uc.SyncUsageFromXray(context.Background(), 1); err != nil {
		t.Fatalf("SyncUsageFromXray returned unexpected error: %v", err)
	}

	got := append([]uint{}, fake.syncedIDs...)
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	want := []uint{10, 11}
	if len(got) != len(want) {
		t.Fatalf("expected %d node sweeps, got %d (ids=%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sweep id[%d]: want %d, got %d", i, want[i], got[i])
		}
	}
}

// TestSyncUsageFromXray_InactiveNodeSkipped confirms the filter still
// applies: inactive nodes must NOT receive a sweep call.
func TestSyncUsageFromXray_InactiveNodeSkipped(t *testing.T) {
	activeNode := &nodeDomain.Node{ID: 20, IsActive: true}
	inactiveNode := &nodeDomain.Node{ID: 21, IsActive: false}

	subRepo := newMockSubscriptionRepo()
	subRepo.seedSubscription(&domain.Subscription{
		ID:     2,
		Status: domain.SubscriptionStatusActive,
	})

	acctMgr := newMockAccountMgr()
	acctMgr.accounts[2] = []*accountDomain.Account{
		{Inbound: &nodeDomain.Inbound{ID: 300, NodeID: 20, Node: activeNode}},
		{Inbound: &nodeDomain.Inbound{ID: 400, NodeID: 21, Node: inactiveNode}},
	}

	fake := &fakeNodeUC{}
	uc := newSyncUsageTestUsecase(subRepo, fake, acctMgr)

	if err := uc.SyncUsageFromXray(context.Background(), 2); err != nil {
		t.Fatalf("SyncUsageFromXray returned unexpected error: %v", err)
	}

	if len(fake.syncedIDs) != 1 || fake.syncedIDs[0] != 20 {
		t.Errorf("expected sweep only for active node 20, got %v", fake.syncedIDs)
	}
}
