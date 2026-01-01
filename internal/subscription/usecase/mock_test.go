package usecase

import (
	"context"
	"fmt"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	userRepoErr "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/money"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
)

// ─── mockSubscriptionRepo ───────────────────────────────────────────────────────

type mockSubscriptionRepo struct {
	subs      map[uint]*domain.Subscription
	nextID    uint
	statusLog []struct {
		ID     uint
		Status domain.SubscriptionStatus
	}

	// Seed hooks for tests that exercise expiry / data-exhaust paths.
	expiredSubs   []*domain.Subscription
	exhaustedSubs []*domain.Subscription

	// Tracking counters for usage-flow tests. Zero-value friendly so
	// existing tests that never inspect them stay green.
	lifetimeAddCalls map[uint]int64 // id -> total bytes added to lifetime counter
	dataUsedWrites   map[uint]int64 // id -> last bytes value written

	findByIDErr     error
	updateStatusErr error
	createErr       error
	updateErr       error

	// Fields for AddDailyUsageSplit / ListDailyUsageRange tests.
	dailySplitCalls []dailySplitCall
	dailyRangeRows  []*domain.SubscriptionDailyUsage
	dailyRangeErr   error
}

type dailySplitCall struct {
	subID    uint
	date     time.Time
	upload   int64
	download int64
}

func newMockSubscriptionRepo() *mockSubscriptionRepo {
	return &mockSubscriptionRepo{
		subs:   make(map[uint]*domain.Subscription),
		nextID: 1,
	}
}

func (m *mockSubscriptionRepo) seedSubscription(sub *domain.Subscription) {
	if sub.ID == 0 {
		sub.ID = m.nextID
		m.nextID++
	} else if sub.ID >= m.nextID {
		m.nextID = sub.ID + 1
	}
	m.subs[sub.ID] = sub
}

func (m *mockSubscriptionRepo) Create(_ context.Context, sub *domain.Subscription) error {
	if m.createErr != nil {
		return m.createErr
	}
	sub.ID = m.nextID
	m.nextID++
	m.subs[sub.ID] = sub
	return nil
}

func (m *mockSubscriptionRepo) FindByID(_ context.Context, id uint) (*domain.Subscription, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	s, ok := m.subs[id]
	if !ok {
		return nil, fmt.Errorf("subscription not found")
	}
	return s, nil
}

func (m *mockSubscriptionRepo) FindByConfigID(_ context.Context, _ string) (*domain.Subscription, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSubscriptionRepo) FindByConfigEmail(_ context.Context, _ string) (*domain.Subscription, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSubscriptionRepo) FindByConfigEmails(_ context.Context, _ []string) (map[string]*domain.Subscription, error) {
	return map[string]*domain.Subscription{}, nil
}

func (m *mockSubscriptionRepo) Update(_ context.Context, sub *domain.Subscription) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.subs[sub.ID] = sub
	return nil
}

func (m *mockSubscriptionRepo) Delete(_ context.Context, _ uint) error { return nil }

func (m *mockSubscriptionRepo) ListByUserID(_ context.Context, _ uint, _, _ int) ([]*domain.Subscription, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) FindActiveByUserID(_ context.Context, _ uint) ([]*domain.Subscription, error) {
	return nil, nil
}

func (m *mockSubscriptionRepo) UpdateStatus(_ context.Context, id uint, status domain.SubscriptionStatus) error {
	if m.updateStatusErr != nil {
		return m.updateStatusErr
	}
	s, ok := m.subs[id]
	if !ok {
		return fmt.Errorf("subscription not found")
	}
	s.Status = status
	m.statusLog = append(m.statusLog, struct {
		ID     uint
		Status domain.SubscriptionStatus
	}{id, status})
	return nil
}

func (m *mockSubscriptionRepo) UpdateDataUsed(_ context.Context, id uint, bytes int64) error {
	if m.dataUsedWrites == nil {
		m.dataUsedWrites = map[uint]int64{}
	}
	m.dataUsedWrites[id] = bytes
	if s, ok := m.subs[id]; ok {
		s.DataUsed = bytes
	}
	return nil
}
func (m *mockSubscriptionRepo) ListExpired(_ context.Context) ([]*domain.Subscription, error) {
	return m.expiredSubs, nil
}
func (m *mockSubscriptionRepo) ListDataExhausted(_ context.Context) ([]*domain.Subscription, error) {
	return m.exhaustedSubs, nil
}
func (m *mockSubscriptionRepo) ListAllActive(_ context.Context) ([]*domain.Subscription, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) ListActiveByNode(_ context.Context, _ uint) ([]*domain.Subscription, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) ListAll(_ context.Context, _ string, _, _ int) ([]*domain.Subscription, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) CountAll(_ context.Context) (int64, error) { return 0, nil }
func (m *mockSubscriptionRepo) CountByStatus(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (m *mockSubscriptionRepo) ExtendDays(_ context.Context, _ uint, _ int) error     { return nil }
func (m *mockSubscriptionRepo) ResetDataUsed(_ context.Context, _ uint) error         { return nil }
func (m *mockSubscriptionRepo) UpdateLabel(_ context.Context, _ uint, _ string) error { return nil }
func (m *mockSubscriptionRepo) UpdateTelegramChatID(_ context.Context, _ uint, _ int64) error {
	return nil
}
func (m *mockSubscriptionRepo) UpdateLastActive(_ context.Context, _ uint, _ time.Time) error {
	return nil
}
func (m *mockSubscriptionRepo) CountActiveByNode(_ context.Context, _ uint, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockSubscriptionRepo) CountActiveByInbound(_ context.Context, _ uint, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockSubscriptionRepo) ListAllFiltered(_ context.Context, _ repository.SubscriptionFilter) ([]*domain.Subscription, int64, error) {
	return nil, 0, nil
}
func (m *mockSubscriptionRepo) HardDelete(_ context.Context, _ uint) error           { return nil }
func (m *mockSubscriptionRepo) UpdateUserID(_ context.Context, _ uint, _ uint) error { return nil }
func (m *mockSubscriptionRepo) SetUserID(_ context.Context, _ uint, _ *uint) error   { return nil }
func (m *mockSubscriptionRepo) SetCustomDataLimit(_ context.Context, _ uint, _ *int64) error {
	return nil
}
func (m *mockSubscriptionRepo) SetCustomEndDate(_ context.Context, _ uint, _ *time.Time, _ bool) error {
	return nil
}
func (m *mockSubscriptionRepo) SetCustomBandwidthLimit(_ context.Context, _ uint, _ *int) error {
	return nil
}
func (m *mockSubscriptionRepo) SetMaxDevices(_ context.Context, _ uint, _ int) error { return nil }
func (m *mockSubscriptionRepo) AddDataUsed(_ context.Context, _ uint, _ int64) error { return nil }
func (m *mockSubscriptionRepo) AddLifetimeDataUsed(_ context.Context, id uint, bytes int64) error {
	if m.lifetimeAddCalls == nil {
		m.lifetimeAddCalls = map[uint]int64{}
	}
	m.lifetimeAddCalls[id] += bytes
	return nil
}
func (m *mockSubscriptionRepo) AddDataUpload(_ context.Context, _ uint, _ int64) error   { return nil }
func (m *mockSubscriptionRepo) AddDataDownload(_ context.Context, _ uint, _ int64) error { return nil }
func (m *mockSubscriptionRepo) AddLifetimeDataUpload(_ context.Context, _ uint, _ int64) error {
	return nil
}
func (m *mockSubscriptionRepo) AddLifetimeDataDownload(_ context.Context, _ uint, _ int64) error {
	return nil
}
func (m *mockSubscriptionRepo) ExistsByUserAndPlan(_ context.Context, _, _ uint) (bool, error) {
	return false, nil
}
func (m *mockSubscriptionRepo) UpdateDataWarningLevel(_ context.Context, _ uint, _ int) error {
	return nil
}
func (m *mockSubscriptionRepo) ListApproachingDataLimit(_ context.Context, _ float64) ([]*domain.Subscription, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) ResetDataWarningLevel(_ context.Context, _ uint) error { return nil }
func (m *mockSubscriptionRepo) SetPanelPassword(_ context.Context, _ uint, _, _ string) error {
	return nil
}
func (m *mockSubscriptionRepo) ListDailyUsage(_ context.Context, _ uint, _, _ time.Time) ([]*domain.SubscriptionDailyUsage, error) {
	return nil, nil
}
func (m *mockSubscriptionRepo) CleanupOldDailyUsage(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (m *mockSubscriptionRepo) UpdateRenewalConfig(_ context.Context, _ uint, _ *int, _ *float64) error {
	return nil
}
func (m *mockSubscriptionRepo) UpdateAutoRenew(_ context.Context, _ uint, _ bool, _ int) error {
	return nil
}
func (m *mockSubscriptionRepo) IncrementAutoRenewUsed(_ context.Context, _ uint) error {
	return nil
}
func (m *mockSubscriptionRepo) AddDailyUsageSplit(ctx context.Context, subID uint, date time.Time, upload, download int64) error {
	m.dailySplitCalls = append(m.dailySplitCalls, dailySplitCall{subID: subID, date: date, upload: upload, download: download})
	return nil
}
func (m *mockSubscriptionRepo) ListDailyUsageRange(ctx context.Context, subID uint, from, to time.Time) ([]*domain.SubscriptionDailyUsage, error) {
	if m.dailyRangeErr != nil {
		return nil, m.dailyRangeErr
	}
	var out []*domain.SubscriptionDailyUsage
	for _, r := range m.dailyRangeRows {
		if r.SubscriptionID == subID && !r.Date.Before(from) && !r.Date.After(to) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ─── mockSubUserRepo ────────────────────────────────────────────────────────────

type mockSubUserRepo struct {
	users            map[uint]*userDomain.User
	deductBalanceErr error
}

func newMockSubUserRepo() *mockSubUserRepo {
	return &mockSubUserRepo{users: make(map[uint]*userDomain.User)}
}

func (m *mockSubUserRepo) Create(_ context.Context, _ *userDomain.User) error { return nil }
func (m *mockSubUserRepo) FindByID(_ context.Context, id uint) (*userDomain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	return u, nil
}
func (m *mockSubUserRepo) FindByTelegramID(_ context.Context, _ int64) (*userDomain.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSubUserRepo) FindByUsername(_ context.Context, _ string) (*userDomain.User, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSubUserRepo) Update(_ context.Context, _ *userDomain.User) error { return nil }
func (m *mockSubUserRepo) Delete(_ context.Context, _ uint) error             { return nil }
func (m *mockSubUserRepo) List(_ context.Context, _, _ int) ([]*userDomain.User, error) {
	return nil, nil
}
func (m *mockSubUserRepo) UpdateBalance(_ context.Context, _ uint, _ money.Money) error { return nil }
func (m *mockSubUserRepo) DeductBalance(_ context.Context, _ uint, _ money.Money) error {
	if m.deductBalanceErr != nil {
		return m.deductBalanceErr
	}
	return nil
}
func (m *mockSubUserRepo) UpdateLanguage(_ context.Context, _ uint, _ string) error { return nil }
func (m *mockSubUserRepo) ListAll(_ context.Context, _, _, _, _ string, _, _ int) ([]*userDomain.User, int64, error) {
	return nil, 0, nil
}
func (m *mockSubUserRepo) ListAllEnriched(_ context.Context, _, _, _, _ string, _, _ int) ([]*adminDomain.UserListItem, int64, error) {
	return nil, 0, nil
}
func (m *mockSubUserRepo) SetBalance(_ context.Context, _ uint, _ money.Money) error { return nil }
func (m *mockSubUserRepo) UpdateBanStatus(_ context.Context, _ uint, _ bool) error   { return nil }
func (m *mockSubUserRepo) UpdateAdminStatus(_ context.Context, _ uint, _ bool) error { return nil }
func (m *mockSubUserRepo) CountAll(_ context.Context) (int64, error)                 { return 0, nil }
func (m *mockSubUserRepo) CountActive(_ context.Context) (int64, error)              { return 0, nil }
func (m *mockSubUserRepo) CountBanned(_ context.Context) (int64, error)              { return 0, nil }
func (m *mockSubUserRepo) CountAdmins(_ context.Context) (int64, error)              { return 0, nil }
func (m *mockSubUserRepo) ListActiveSubscribers(_ context.Context) ([]*userDomain.User, error) {
	return nil, nil
}
func (m *mockSubUserRepo) MarkTrialUsed(_ context.Context, _ uint) error  { return nil }
func (m *mockSubUserRepo) ResetTrialFlag(_ context.Context, _ uint) error { return nil }
func (m *mockSubUserRepo) UpdateAdminNotes(_ context.Context, _ uint, _ string) error {
	return nil
}
func (m *mockSubUserRepo) ListAdmins(_ context.Context) ([]*userDomain.User, error) {
	return nil, nil
}

// ─── mockAccountMgr ─────────────────────────────────────────────────────────────

type mockAccountMgr struct {
	disableCalls int
	enableCalls  int
	accounts     map[uint][]*accountDomain.Account // by subID
}

func newMockAccountMgr() *mockAccountMgr {
	return &mockAccountMgr{
		accounts: make(map[uint][]*accountDomain.Account),
	}
}

func (m *mockAccountMgr) CreateAccountForSubscription(_ context.Context, _ uint, _, _, _, _ string, _ uint, _ int64) error {
	return nil
}
func (m *mockAccountMgr) CreateAccountForSubscriptionNoEnqueue(_ context.Context, _ uint, _, _, _, _ string, _ uint, _ int64) error {
	return nil
}
func (m *mockAccountMgr) CreateAccountManual(_ context.Context, _ uint, _, _, _, _ string) (uint, string, error) {
	return 0, "", nil
}
func (m *mockAccountMgr) GetLinkByEmail(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockAccountMgr) DisableAccountsBySubscription(_ context.Context, subID uint) error {
	m.disableCalls++
	return nil
}
func (m *mockAccountMgr) EnableAccountsBySubscription(_ context.Context, subID uint) error {
	m.enableCalls++
	return nil
}
func (m *mockAccountMgr) DeleteAccountsBySubscription(_ context.Context, _ uint) error { return nil }
func (m *mockAccountMgr) ForceDeleteAccountsBySubscription(_ context.Context, _ uint) error {
	return nil
}
func (m *mockAccountMgr) DeleteAccount(_ context.Context, _ uint) error { return nil }
func (m *mockAccountMgr) ListAccountsBySubscription(_ context.Context, subID uint) ([]*accountDomain.Account, error) {
	return m.accounts[subID], nil
}
func (m *mockAccountMgr) ListAllAccountsBySubscription(_ context.Context, subID uint) ([]*accountDomain.Account, error) {
	return m.accounts[subID], nil
}
func (m *mockAccountMgr) ClearAccountDataLimitsBySubscription(_ context.Context, _ uint) error {
	return nil
}
func (m *mockAccountMgr) ResetAccountDataUsedBySubscription(_ context.Context, _ uint) error {
	return nil
}
func (m *mockAccountMgr) SetAccountsUUIDBySubscription(_ context.Context, _ uint, _ string) (int, error) {
	return 0, nil
}

// ─── mockNodeSyncer ─────────────────────────────────────────────────────────────

type mockNodeSyncer struct{}

func (m *mockNodeSyncer) SyncInbounds(_ context.Context, _ uint) error { return nil }

// ─── mockAccountReader ──────────────────────────────────────────────────────────

type mockAccountReader struct{}

func (m *mockAccountReader) ListActiveAccountInfos(_ context.Context) ([]*contract.AccountInfo, error) {
	return nil, nil
}

// ─── mockNodeRepo ───────────────────────────────────────────────────────────────

type mockNodeRepo struct{}

func (m *mockNodeRepo) CreateNode(_ context.Context, _ *nodeDomain.Node) error { return nil }
func (m *mockNodeRepo) GetNode(_ context.Context, _ uint) (*nodeDomain.Node, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) GetNodeByUUID(_ context.Context, _ string) (*nodeDomain.Node, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateNode(_ context.Context, _ *nodeDomain.Node) error { return nil }
func (m *mockNodeRepo) DeleteNode(_ context.Context, _ uint) error             { return nil }
func (m *mockNodeRepo) ListNodes(_ context.Context) ([]*nodeDomain.Node, error) {
	return nil, nil
}
func (m *mockNodeRepo) ListActiveNodes(_ context.Context) ([]*nodeDomain.Node, error) {
	return nil, nil
}
func (m *mockNodeRepo) UpdateNodeStatus(_ context.Context, _ uint, _ bool, _ time.Time) error {
	return nil
}
func (m *mockNodeRepo) UpdateNodeDNSSettings(_ context.Context, _ uint, _ *nodeDomain.DNSSettings) error {
	return nil
}
func (m *mockNodeRepo) UpdateNodeFakeDNSSettings(_ context.Context, _ uint, _ []nodeDomain.FakeDNSPool) error {
	return nil
}
func (m *mockNodeRepo) CreateInbound(_ context.Context, _ *nodeDomain.Inbound) error { return nil }
func (m *mockNodeRepo) GetInbound(_ context.Context, _ uint) (*nodeDomain.Inbound, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateInbound(_ context.Context, _ *nodeDomain.Inbound) error { return nil }
func (m *mockNodeRepo) DeleteInbound(_ context.Context, _ uint) error                { return nil }
func (m *mockNodeRepo) ListInboundsByNode(_ context.Context, _ uint) ([]*nodeDomain.Inbound, error) {
	return nil, nil
}
func (m *mockNodeRepo) ToggleInboundDisabled(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) GetInboundWithNode(_ context.Context, _ uint) (*nodeDomain.Inbound, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) GetInboundByTagAndNode(_ context.Context, _ uint, _ string) (*nodeDomain.Inbound, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) BulkCreateInbounds(_ context.Context, _ []*nodeDomain.Inbound) error {
	return nil
}
func (m *mockNodeRepo) DeleteInboundsByNodeExceptTags(_ context.Context, _ uint, _ []string) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) CreateOutbound(_ context.Context, _ *nodeDomain.Outbound) error { return nil }
func (m *mockNodeRepo) GetOutbound(_ context.Context, _ uint) (*nodeDomain.Outbound, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateOutbound(_ context.Context, _ *nodeDomain.Outbound) error { return nil }
func (m *mockNodeRepo) DeleteOutbound(_ context.Context, _ uint) error                 { return nil }
func (m *mockNodeRepo) ListOutboundsByNode(_ context.Context, _ uint) ([]*nodeDomain.Outbound, error) {
	return nil, nil
}
func (m *mockNodeRepo) GetOutboundWithNode(_ context.Context, _ uint) (*nodeDomain.Outbound, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) GetOutboundByTagAndNode(_ context.Context, _ uint, _ string) (*nodeDomain.Outbound, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) BulkCreateOutbounds(_ context.Context, _ []*nodeDomain.Outbound) error {
	return nil
}
func (m *mockNodeRepo) DeleteOutboundsByNodeExceptTags(_ context.Context, _ uint, _ []string) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) ToggleOutboundDisabled(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) ListRoutingRulesByOutboundTag(_ context.Context, _ uint, _ string) ([]*nodeDomain.RoutingRule, error) {
	return nil, nil
}
func (m *mockNodeRepo) CreateRoutingRule(_ context.Context, _ *nodeDomain.RoutingRule) error {
	return nil
}
func (m *mockNodeRepo) GetRoutingRule(_ context.Context, _ uint) (*nodeDomain.RoutingRule, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateRoutingRule(_ context.Context, _ *nodeDomain.RoutingRule) error {
	return nil
}
func (m *mockNodeRepo) DeleteRoutingRule(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) ListRoutingRulesByNode(_ context.Context, _ uint) ([]*nodeDomain.RoutingRule, error) {
	return nil, nil
}
func (m *mockNodeRepo) GetRoutingRuleWithNode(_ context.Context, _ uint) (*nodeDomain.RoutingRule, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) GetRoutingRuleByTagAndNode(_ context.Context, _ uint, _ string) (*nodeDomain.RoutingRule, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) FindAdjacentRoutingRule(_ context.Context, _ uint, _ int, _ uint, _ bool) (*nodeDomain.RoutingRule, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) ReorderRoutingRules(_ context.Context, _ uint, _ []uint) error { return nil }
func (m *mockNodeRepo) DeleteRoutingRulesByNodeAndSource(_ context.Context, _ uint, _ string) error {
	return nil
}
func (m *mockNodeRepo) CreateBalancingRule(_ context.Context, _ *nodeDomain.BalancingRule) error {
	return nil
}
func (m *mockNodeRepo) GetBalancingRule(_ context.Context, _ uint) (*nodeDomain.BalancingRule, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) ListBalancingRulesByNode(_ context.Context, _ uint) ([]*nodeDomain.BalancingRule, error) {
	return nil, nil
}
func (m *mockNodeRepo) UpdateBalancingRule(_ context.Context, _ *nodeDomain.BalancingRule) error {
	return nil
}
func (m *mockNodeRepo) DeleteBalancingRule(_ context.Context, _ uint) error        { return nil }
func (m *mockNodeRepo) DeleteBalancingRulesByNode(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) AddNodeTraffic(_ context.Context, _ uint, _, _ int64) error { return nil }
func (m *mockNodeRepo) AddOutboundTraffic(_ context.Context, _ uint, _ string, _, _ int64) error {
	return nil
}
func (m *mockNodeRepo) CreateNodeStat(_ context.Context, _ *nodeDomain.NodeStat) error { return nil }
func (m *mockNodeRepo) GetNodeStatsHistory(_ context.Context, _ uint, _ int) ([]*nodeDomain.NodeStat, error) {
	return nil, nil
}
func (m *mockNodeRepo) GetNodesStatsHistoryBulk(_ context.Context, _ []uint, _ int) (map[uint][]*nodeDomain.NodeStat, error) {
	return map[uint][]*nodeDomain.NodeStat{}, nil
}
func (m *mockNodeRepo) DeleteOutboundsByNode(_ context.Context, _ uint) error    { return nil }
func (m *mockNodeRepo) DeleteRoutingRulesByNode(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) DeleteNodeStatsByNode(_ context.Context, _ uint) error    { return nil }
func (m *mockNodeRepo) Transaction(_ context.Context, fn func(txRepo nodeRepo.NodeRepository) error) error {
	return fn(m)
}
func (m *mockNodeRepo) CreateReverseProxy(_ context.Context, _ *nodeDomain.ReverseProxy) error {
	return nil
}
func (m *mockNodeRepo) GetReverseProxy(_ context.Context, _ uint) (*nodeDomain.ReverseProxy, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) GetReverseProxyWithNode(_ context.Context, _ uint) (*nodeDomain.ReverseProxy, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateReverseProxy(_ context.Context, _ *nodeDomain.ReverseProxy) error {
	return nil
}
func (m *mockNodeRepo) DeleteReverseProxy(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) ListReverseProxiesByNode(_ context.Context, _ uint) ([]*nodeDomain.ReverseProxy, error) {
	return nil, nil
}
func (m *mockNodeRepo) DeleteReverseProxiesByNode(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) ListReverseProxiesByReferencedTag(_ context.Context, _ uint, _ string) ([]*nodeDomain.ReverseProxy, error) {
	return nil, nil
}
func (m *mockNodeRepo) CleanupOldNodeStats(_ context.Context, _ int) (int64, error) { return 0, nil }
func (m *mockNodeRepo) CleanupOldNodeDailyTraffic(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) CleanupOldUptimeEvents(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) CleanupOldStarlinkStats(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) FindInboundsByIDs(_ context.Context, _ []uint) ([]*nodeDomain.Inbound, error) {
	return nil, nil
}
func (m *mockNodeRepo) CreateHost(_ context.Context, _ *nodeDomain.Host) error { return nil }
func (m *mockNodeRepo) GetHost(_ context.Context, _ uint) (*nodeDomain.Host, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) GetHostWithInbound(_ context.Context, _ uint) (*nodeDomain.Host, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateHost(_ context.Context, _ *nodeDomain.Host) error { return nil }
func (m *mockNodeRepo) DeleteHost(_ context.Context, _ uint) error             { return nil }
func (m *mockNodeRepo) ListHostsByInbound(_ context.Context, _ uint) ([]*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeRepo) ListHostsByPlan(_ context.Context, _ uint) ([]*nodeDomain.Host, error) {
	return nil, nil
}
func (m *mockNodeRepo) DeleteHostsByInbound(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) ListAllHosts(_ context.Context, _ string, _, _, _ uint, _ *bool, _, _, _ string, _ string, _, _ int) ([]*nodeDomain.Host, int64, error) {
	return nil, 0, nil
}
func (m *mockNodeRepo) BulkUpdateHosts(_ context.Context, _ []uint, _ map[string]any) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) ListHostTags(_ context.Context) ([]string, error) { return nil, nil }
func (m *mockNodeRepo) CreateHostTemplate(_ context.Context, _ *nodeDomain.HostTemplate) error {
	return nil
}
func (m *mockNodeRepo) GetHostTemplate(_ context.Context, _ uint) (*nodeDomain.HostTemplate, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockNodeRepo) UpdateHostTemplate(_ context.Context, _ *nodeDomain.HostTemplate) error {
	return nil
}
func (m *mockNodeRepo) DeleteHostTemplate(_ context.Context, _ uint) error { return nil }
func (m *mockNodeRepo) ListHostTemplates(_ context.Context) ([]*nodeDomain.HostTemplate, error) {
	return nil, nil
}
func (m *mockNodeRepo) UpsertAccessLogSummary(_ context.Context, _ *nodeDomain.AccessLogSummary) error {
	return nil
}
func (m *mockNodeRepo) GetAccessLogSummaries(_ context.Context, _ nodeRepo.AccessLogSummaryFilter) ([]*nodeDomain.AccessLogSummary, int64, error) {
	return nil, 0, nil
}
func (m *mockNodeRepo) GetAccessLogTopDomains(_ context.Context, _ nodeRepo.AccessLogSummaryFilter) ([]*nodeDomain.AccessLogSummary, error) {
	return nil, nil
}
func (m *mockNodeRepo) GetHourlyAggregates(_ context.Context, _ nodeRepo.AccessLogSummaryFilter) ([]nodeRepo.HourlyAggregate, error) {
	return nil, nil
}
func (m *mockNodeRepo) GetAccessLogTimeSeries(_ context.Context, _ nodeRepo.AccessLogSummaryFilter, _ string) ([]nodeRepo.AccessLogTimeBucket, error) {
	return nil, nil
}
func (m *mockNodeRepo) GetAccessLogTotals(_ context.Context, _ nodeRepo.AccessLogSummaryFilter) (nodeRepo.AccessLogTotals, error) {
	return nodeRepo.AccessLogTotals{}, nil
}
func (m *mockNodeRepo) SearchAccessLog(_ context.Context, _ nodeRepo.AccessLogSearchFilter) ([]nodeRepo.AccessLogSearchHit, bool, error) {
	return nil, false, nil
}
func (m *mockNodeRepo) CleanupOldAccessLogSummaries(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (m *mockNodeRepo) MarkAccessLogSynced(_ context.Context, _ uint, _ time.Time) error {
	return nil
}
func (m *mockNodeRepo) GetNodesLastAccessLogSyncedAt(_ context.Context, _ []uint) (map[uint]time.Time, error) {
	return map[uint]time.Time{}, nil
}
func (m *mockNodeRepo) AddNodeDailyTraffic(_ context.Context, _ uint, _ time.Time, _, _ int64) error {
	return nil
}
func (m *mockNodeRepo) GetNodeDailyTraffic(_ context.Context, _ uint, _ int) ([]*nodeDomain.NodeDailyTraffic, error) {
	return nil, nil
}
func (m *mockNodeRepo) CreateUptimeEvent(_ context.Context, _ *nodeDomain.NodeUptimeEvent) error {
	return nil
}
func (m *mockNodeRepo) GetUptimeEvents(_ context.Context, _ uint, _ time.Time) ([]*nodeDomain.NodeUptimeEvent, error) {
	return nil, nil
}
func (m *mockNodeRepo) CreateStarlinkStat(_ context.Context, _ *nodeDomain.StarlinkStat) error {
	return nil
}
func (m *mockNodeRepo) GetStarlinkStatsHistory(_ context.Context, _ uint, _ int, _ *time.Time) ([]*nodeDomain.StarlinkStat, error) {
	return nil, nil
}

// ─── mockSubTxManager ───────────────────────────────────────────────────────────

type mockSubTxManager struct {
	shouldFail bool
}

func (m *mockSubTxManager) Do(_ context.Context, fn func(context.Context) error) error {
	if m.shouldFail {
		return fmt.Errorf("transaction failed")
	}
	return fn(context.Background())
}

// ─── mockProvider ───────────────────────────────────────────────────────────────

type mockProvider struct {
	generateConfigResult *product.ConfigResult
	generateConfigErr    error
	deactivateCalls      int
}

func (m *mockProvider) GetType() product.ProductType { return product.ProductTypeXray }
func (m *mockProvider) GenerateConfig(_ context.Context, _ *product.SubscriptionInfo, _ string) (*product.ConfigResult, error) {
	if m.generateConfigErr != nil {
		return nil, m.generateConfigErr
	}
	return m.generateConfigResult, nil
}
func (m *mockProvider) GenerateClientConfig(_ context.Context, _ *product.SubscriptionInfo) (string, error) {
	return "", nil
}
func (m *mockProvider) ActivateUser(_ context.Context, _ *product.SubscriptionInfo) error { return nil }
func (m *mockProvider) DeactivateUser(_ context.Context, _ *product.SubscriptionInfo) error {
	m.deactivateCalls++
	return nil
}
func (m *mockProvider) GetUsageStats(_ context.Context, _ *product.SubscriptionInfo) (*product.UsageStats, error) {
	return &product.UsageStats{}, nil
}
func (m *mockProvider) ValidateConfig(_ string) error { return nil }

// Ensure interface compliance.
var _ product.Provider = (*mockProvider)(nil)

// Ensure interface compliance for all mocks.
var _ repository.SubscriptionRepository = (*mockSubscriptionRepo)(nil)
var _ userRepoErr.UserRepository = (*mockSubUserRepo)(nil)
var _ contract.AccountManager = (*mockAccountMgr)(nil)
var _ contract.NodeSyncer = (*mockNodeSyncer)(nil)
var _ contract.AccountReader = (*mockAccountReader)(nil)
var _ nodeRepo.NodeRepository = (*mockNodeRepo)(nil)
