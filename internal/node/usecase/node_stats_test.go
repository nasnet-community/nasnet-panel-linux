package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// fakeStatsNodeRepo: in-memory NodeRepository for stats-sync ack tests.
// Nil embed panics on unstubbed calls.
type fakeStatsNodeRepo struct {
	repository.NodeRepository // nil embed

	mu sync.Mutex

	nodes map[uint]*domain.Node

	addNodeTrafficCalls      int
	addNodeDailyTrafficCalls int
	addOutboundTrafficCalls  int

	// Error injection seams for Task 3 (S4) tests.
	addNodeTrafficErr      error
	addNodeDailyTrafficErr error
}

func newFakeStatsNodeRepo(nodes ...*domain.Node) *fakeStatsNodeRepo {
	r := &fakeStatsNodeRepo{nodes: map[uint]*domain.Node{}}
	for _, n := range nodes {
		r.nodes[n.ID] = n
	}
	return r
}

func (r *fakeStatsNodeRepo) GetNode(_ context.Context, id uint) (*domain.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nodes[id], nil
}

func (r *fakeStatsNodeRepo) AddNodeTraffic(_ context.Context, _ uint, _, _ int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addNodeTrafficCalls++
	return r.addNodeTrafficErr
}

func (r *fakeStatsNodeRepo) AddNodeDailyTraffic(_ context.Context, _ uint, _ time.Time, _, _ int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addNodeDailyTrafficCalls++
	return r.addNodeDailyTrafficErr
}

func (r *fakeStatsNodeRepo) AddOutboundTraffic(_ context.Context, _ uint, _ string, _, _ int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addOutboundTrafficCalls++
	return nil
}

// fakeStatsSubRepo: stub. FindByConfigEmails runs even with no traffic
// so it must return empty map (nil embed would panic).
type fakeStatsSubRepo struct {
	subRepo.SubscriptionRepository // nil embed

	// subs is an optional email-keyed map of subscriptions returned by
	// FindByConfigEmails / FindByConfigEmail. When nil, the methods return
	// empty results (original behaviour).
	subs map[string]*subDomain.Subscription

	// Error injection seam for Task 3 (S4) tests.
	addDailyUsageSplitErr error
}

func (r *fakeStatsSubRepo) FindByConfigEmails(_ context.Context, emails []string) (map[string]*subDomain.Subscription, error) {
	if r.subs == nil {
		return map[string]*subDomain.Subscription{}, nil
	}
	out := make(map[string]*subDomain.Subscription, len(emails))
	for _, e := range emails {
		if s, ok := r.subs[e]; ok {
			out[e] = s
		}
	}
	return out, nil
}

func (r *fakeStatsSubRepo) FindByConfigEmail(_ context.Context, email string) (*subDomain.Subscription, error) {
	if r.subs != nil {
		if s, ok := r.subs[email]; ok {
			return s, nil
		}
	}
	return nil, stubError("not found")
}

func (r *fakeStatsSubRepo) AddDailyUsageSplit(_ context.Context, _ uint, _ time.Time, _, _ int64) error {
	return r.addDailyUsageSplitErr
}

// The following stubs are no-ops so syncSingleNode can proceed past the
// user-traffic persist loop without panicking on the nil embed.

func (r *fakeStatsSubRepo) AddDataUsed(_ context.Context, _ uint, _ int64) error         { return nil }
func (r *fakeStatsSubRepo) AddLifetimeDataUsed(_ context.Context, _ uint, _ int64) error { return nil }
func (r *fakeStatsSubRepo) AddDataUpload(_ context.Context, _ uint, _ int64) error       { return nil }
func (r *fakeStatsSubRepo) AddLifetimeDataUpload(_ context.Context, _ uint, _ int64) error {
	return nil
}
func (r *fakeStatsSubRepo) AddDataDownload(_ context.Context, _ uint, _ int64) error { return nil }
func (r *fakeStatsSubRepo) AddLifetimeDataDownload(_ context.Context, _ uint, _ int64) error {
	return nil
}
func (r *fakeStatsSubRepo) UpdateLastActive(_ context.Context, _ uint, _ time.Time) error {
	return nil
}

// AddUsageDelta is a no-op so syncSingleNode can proceed past the
// user-traffic persist loop without panicking on the nil embed.
func (r *fakeStatsSubRepo) AddUsageDelta(_ context.Context, _ uint, _, _ int64, _ time.Time) error {
	return nil
}

// fakeStatsAgentClient: canned BufferedTraffic + records ack timestamp.
// Nil embed panics on unstubbed calls.

type fakeStatsAgentClient struct {
	agent.NodeClient // nil embed

	buffered *agent.BufferedTrafficStats

	mu        sync.Mutex
	ackedAt   int64
	ackCalled bool
}

func (f *fakeStatsAgentClient) GetSystemStats(_ context.Context) (*agent.SystemStats, error) {
	// Returning an error keeps sysStats nil in syncSingleNode → CreateNodeStat
	// is skipped, so we don't need to stub that repo method.
	return nil, errStatsNotAvailable
}

func (f *fakeStatsAgentClient) GetStatus(_ context.Context) (*pb.NodeStatus, error) {
	// XrayRunning=true keeps xrayStatus = "running" so GetBufferedTraffic
	// fires. Without this the sweep short-circuits before touching the
	// buffered records.
	return &pb.NodeStatus{XrayRunning: true}, nil
}

func (f *fakeStatsAgentClient) GetVersion(_ context.Context) (*agent.VersionInfo, error) {
	return &agent.VersionInfo{}, nil
}

func (f *fakeStatsAgentClient) GetBufferedTraffic(_ context.Context) (*agent.BufferedTrafficStats, error) {
	return f.buffered, nil
}

func (f *fakeStatsAgentClient) AckBufferedTraffic(_ context.Context, ts int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ackCalled = true
	f.ackedAt = ts
	return nil
}

func (f *fakeStatsAgentClient) GetAllUsersOnlineIPs(_ context.Context) (map[string]map[string]int64, error) {
	return map[string]map[string]int64{}, nil
}

func (f *fakeStatsAgentClient) GetBufferedAccessLogSummary(_ context.Context) (*pb.BufferedAccessLogSummary, error) {
	return &pb.BufferedAccessLogSummary{}, nil
}

func (f *fakeStatsAgentClient) Close() error { return nil }

// errStatsNotAvailable is a sentinel the fake client returns from
// GetSystemStats so the stats branch short-circuits without the test needing
// to stub CreateNodeStat.
var errStatsNotAvailable = stubError("stats unavailable in test")

type stubError string

func (e stubError) Error() string { return string(e) }

// newSyncSingleNodeTestUsecase wires up a nodeUsecase with the minimum set of
// collaborators syncSingleNode touches: node repo, event bus, and an
// agent-client factory seam pre-populated with the given fake.
func newSyncSingleNodeTestUsecase(repo *fakeStatsNodeRepo, client agent.NodeClient) *nodeUsecase {
	return newSyncSingleNodeTestUsecaseWithSub(repo, &fakeStatsSubRepo{}, client)
}

// newSyncSingleNodeTestUsecaseWithSub is like newSyncSingleNodeTestUsecase but
// accepts a custom fakeStatsSubRepo so tests can inject error seams.
func newSyncSingleNodeTestUsecaseWithSub(repo *fakeStatsNodeRepo, sr *fakeStatsSubRepo, client agent.NodeClient) *nodeUsecase {
	return newSyncSingleNodeTestUsecaseWithAccount(repo, sr, nil, client)
}

// newSyncSingleNodeTestUsecaseWithAccount is like newSyncSingleNodeTestUsecaseWithSub
// but also accepts a fakeStatsAccountRepo for account-attribution tests.
func newSyncSingleNodeTestUsecaseWithAccount(repo *fakeStatsNodeRepo, sr *fakeStatsSubRepo, ar *fakeStatsAccountRepo, client agent.NodeClient) *nodeUsecase {
	u := &nodeUsecase{
		nodeRepo:             repo,
		subRepo:              sr,
		accountRepo:          ar,
		eventBus:             events.NewEventBus(),
		pushState:            map[uint]*configPushState{},
		nukesInFlight:        map[uint]struct{}{},
		lastPushedConfigHash: map[uint]string{},
	}
	u.statsAgentClientFactory = func(_ context.Context, _ *domain.Node) (agent.NodeClient, error) {
		return client, nil
	}
	return u
}

// ── tests ───────────────────────────────────────────────────────────────────

// Record with TotalUp/Down>0 but no user/outbound traffic must still
// trigger ack; else agent re-delivers and node totals double-count.
func TestSyncSingleNode_AcksWhenOnlyNodeTrafficPresent(t *testing.T) {
	recordTS := time.Now().Unix()
	node := &domain.Node{ID: 1, Name: "n1", ConnectMode: "direct", IsActive: true, IsOnline: true}
	repo := newFakeStatsNodeRepo(node)
	fake := &fakeStatsAgentClient{
		buffered: &agent.BufferedTrafficStats{
			Records: []*agent.TrafficRecord{
				{
					Timestamp:        recordTS,
					TotalUplink:      1000,
					TotalDownlink:    2000,
					UserUplink:       map[string]int64{},
					UserDownlink:     map[string]int64{},
					OutboundUplink:   map[string]int64{},
					OutboundDownlink: map[string]int64{},
					InboundUplink:    map[string]int64{},
					InboundDownlink:  map[string]int64{},
				},
			},
			BufferStartTime: recordTS,
			BufferEndTime:   recordTS,
		},
	}
	u := newSyncSingleNodeTestUsecase(repo, fake)

	u.syncSingleNode(context.Background(), node, nil)

	if !fake.ackCalled {
		t.Fatal("AckBufferedTraffic was not called — the agent will re-deliver the same record and double-count node totals")
	}
	if fake.ackedAt != recordTS {
		t.Fatalf("ack timestamp = %d, want %d", fake.ackedAt, recordTS)
	}
	if repo.addNodeTrafficCalls == 0 {
		t.Fatal("expected AddNodeTraffic to be called for the TotalUplink/TotalDownlink record")
	}
	if repo.addNodeDailyTrafficCalls == 0 {
		t.Fatal("expected AddNodeDailyTraffic to be called for the TotalUplink/TotalDownlink record")
	}
}

// TestSyncSingleNode_AcksWhenOnlyOutboundTrafficPresent is a regression guard:
// the original ack predicate gated on persistedTraffic||persistedOutboundTraffic,
// so outbound-only records already ack correctly. Task 2 must not regress that.
func TestSyncSingleNode_AcksWhenOnlyOutboundTrafficPresent(t *testing.T) {
	recordTS := time.Now().Unix()
	node := &domain.Node{ID: 2, Name: "n2", ConnectMode: "direct", IsActive: true, IsOnline: true}
	repo := newFakeStatsNodeRepo(node)
	fake := &fakeStatsAgentClient{
		buffered: &agent.BufferedTrafficStats{
			Records: []*agent.TrafficRecord{
				{
					Timestamp:        recordTS,
					UserUplink:       map[string]int64{},
					UserDownlink:     map[string]int64{},
					OutboundUplink:   map[string]int64{"direct": 500},
					OutboundDownlink: map[string]int64{"direct": 750},
					InboundUplink:    map[string]int64{},
					InboundDownlink:  map[string]int64{},
				},
			},
			BufferStartTime: recordTS,
			BufferEndTime:   recordTS,
		},
	}
	u := newSyncSingleNodeTestUsecase(repo, fake)

	u.syncSingleNode(context.Background(), node, nil)

	if !fake.ackCalled {
		t.Fatal("AckBufferedTraffic was not called for outbound-only record")
	}
	if fake.ackedAt != recordTS {
		t.Fatalf("ack timestamp = %d, want %d", fake.ackedAt, recordTS)
	}
	if repo.addOutboundTrafficCalls == 0 {
		t.Fatal("expected AddOutboundTraffic to be called")
	}
}

// TestSyncSingleNode_SkipsAckOnDailyUsageSplitError guards Task 3 (S4):
// when AddDailyUsageSplit returns an error, persistError must be set and
// AckBufferedTraffic must NOT be called so the agent re-delivers the record.
func TestSyncSingleNode_SkipsAckOnDailyUsageSplitError(t *testing.T) {
	recordTS := time.Now().Unix()
	node := &domain.Node{ID: 3, Name: "n3", ConnectMode: "direct", IsActive: true, IsOnline: true}
	repo := newFakeStatsNodeRepo(node)

	aliceSub := &subDomain.Subscription{ID: 42}
	sr := &fakeStatsSubRepo{
		subs:                  map[string]*subDomain.Subscription{"alice": aliceSub},
		addDailyUsageSplitErr: stubError("db write failed"),
	}

	fake := &fakeStatsAgentClient{
		buffered: &agent.BufferedTrafficStats{
			Records: []*agent.TrafficRecord{
				{
					Timestamp:        recordTS,
					UserUplink:       map[string]int64{"alice": 500},
					UserDownlink:     map[string]int64{"alice": 400},
					OutboundUplink:   map[string]int64{},
					OutboundDownlink: map[string]int64{},
					InboundUplink:    map[string]int64{},
					InboundDownlink:  map[string]int64{},
				},
			},
			BufferStartTime: recordTS,
			BufferEndTime:   recordTS,
		},
	}

	u := newSyncSingleNodeTestUsecaseWithSub(repo, sr, fake)
	u.syncSingleNode(context.Background(), node, nil)

	if fake.ackCalled {
		t.Fatal("AckBufferedTraffic must NOT be called when AddDailyUsageSplit errors")
	}
}

// TestSyncSingleNode_SkipsAckOnNodeTrafficError guards Task 3 (S4):
// when AddNodeTraffic and AddNodeDailyTraffic both error, persistError must be
// set and AckBufferedTraffic must NOT be called.
func TestSyncSingleNode_SkipsAckOnNodeTrafficError(t *testing.T) {
	recordTS := time.Now().Unix()
	node := &domain.Node{ID: 4, Name: "n4", ConnectMode: "direct", IsActive: true, IsOnline: true}
	repo := newFakeStatsNodeRepo(node)
	repo.addNodeTrafficErr = stubError("db down")
	repo.addNodeDailyTrafficErr = stubError("db down")

	fake := &fakeStatsAgentClient{
		buffered: &agent.BufferedTrafficStats{
			Records: []*agent.TrafficRecord{
				{
					Timestamp:        recordTS,
					TotalUplink:      1000,
					TotalDownlink:    0,
					UserUplink:       map[string]int64{},
					UserDownlink:     map[string]int64{},
					OutboundUplink:   map[string]int64{},
					OutboundDownlink: map[string]int64{},
					InboundUplink:    map[string]int64{},
					InboundDownlink:  map[string]int64{},
				},
			},
			BufferStartTime: recordTS,
			BufferEndTime:   recordTS,
		},
	}

	u := newSyncSingleNodeTestUsecase(repo, fake)
	u.syncSingleNode(context.Background(), node, nil)

	if fake.ackCalled {
		t.Fatal("AckBufferedTraffic must NOT be called when AddNodeTraffic and AddNodeDailyTraffic both error")
	}
}

// fakeStatsAccountRepo: stubs FindByEmailAndInbound, AddDataUsed,
// UpdateLastActive; nil embed panics on unstubbed calls.

type fakeStatsAccountRepo struct {
	accountRepo.AccountRepository // nil embed

	mu sync.Mutex

	// accounts maps (email, inboundID) to an Account.
	accounts map[accountLookupKey]*accountDomain.Account

	// addDataUsedCalls records each (id, bytes) pair passed to AddDataUsed.
	addDataUsedCalls []accountDataUsedCall
}

type accountLookupKey struct {
	email     string
	inboundID uint
}

type accountDataUsedCall struct {
	id    uint
	bytes int64
}

func newFakeStatsAccountRepo() *fakeStatsAccountRepo {
	return &fakeStatsAccountRepo{
		accounts: map[accountLookupKey]*accountDomain.Account{},
	}
}

func (r *fakeStatsAccountRepo) addAccount(email string, inboundID uint, account *accountDomain.Account) {
	r.accounts[accountLookupKey{email: email, inboundID: inboundID}] = account
}

func (r *fakeStatsAccountRepo) FindByEmailAndInbound(_ context.Context, email string, inboundID uint) (*accountDomain.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.accounts[accountLookupKey{email: email, inboundID: inboundID}]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func (r *fakeStatsAccountRepo) ListTrafficRefsByNode(_ context.Context, _ uint) ([]accountRepo.AccountTrafficRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	refs := make([]accountRepo.AccountTrafficRef, 0, len(r.accounts))
	for k, a := range r.accounts {
		refs = append(refs, accountRepo.AccountTrafficRef{ID: a.ID, Email: k.email, InboundID: k.inboundID})
	}
	return refs, nil
}

func (r *fakeStatsAccountRepo) AddDataUsed(_ context.Context, id uint, bytes int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addDataUsedCalls = append(r.addDataUsedCalls, accountDataUsedCall{id: id, bytes: bytes})
	return nil
}

func (r *fakeStatsAccountRepo) UpdateLastActive(_ context.Context, _ uint, _ time.Time) error {
	return nil
}

// ── Task 5 (S5) tests ───────────────────────────────────────────────────────

// TestSyncSingleNode_SplitsBytesAcrossActiveInbounds guards the equal-split
// fix: when an email has accounts on two active inbounds, bytes must be halved
// rather than attributed in full to both accounts.
func TestSyncSingleNode_SplitsBytesAcrossActiveInbounds(t *testing.T) {
	recordTS := time.Now().Unix()

	node := &domain.Node{
		ID: 10, Name: "n10", ConnectMode: "direct", IsActive: true, IsOnline: true,
		Inbounds: []domain.Inbound{
			{ID: 101, Tag: "vless-in"},
			{ID: 102, Tag: "vmess-in"},
		},
	}
	repo := newFakeStatsNodeRepo(node)

	bobSub := &subDomain.Subscription{ID: 5}
	sr := &fakeStatsSubRepo{
		subs: map[string]*subDomain.Subscription{"bob": bobSub},
	}

	ar := newFakeStatsAccountRepo()
	ar.addAccount("bob", 101, &accountDomain.Account{ID: 201})
	ar.addAccount("bob", 102, &accountDomain.Account{ID: 202})

	fake := &fakeStatsAgentClient{
		buffered: &agent.BufferedTrafficStats{
			Records: []*agent.TrafficRecord{
				{
					Timestamp:        recordTS,
					UserUplink:       map[string]int64{"bob": 600},
					UserDownlink:     map[string]int64{"bob": 400},
					OutboundUplink:   map[string]int64{},
					OutboundDownlink: map[string]int64{},
					InboundUplink:    map[string]int64{"vless-in": 300, "vmess-in": 300},
					InboundDownlink:  map[string]int64{},
				},
			},
			BufferStartTime: recordTS,
			BufferEndTime:   recordTS,
		},
	}

	u := newSyncSingleNodeTestUsecaseWithAccount(repo, sr, ar, fake)
	u.syncSingleNode(context.Background(), node, nil)

	// Expect exactly two AddDataUsed calls, each with 500 bytes (1000 / 2).
	ar.mu.Lock()
	calls := ar.addDataUsedCalls
	ar.mu.Unlock()

	if len(calls) != 2 {
		t.Fatalf("expected 2 AddDataUsed calls, got %d: %+v", len(calls), calls)
	}
	byID := map[uint]int64{}
	for _, c := range calls {
		byID[c.id] += c.bytes
	}
	if byID[201] != 500 {
		t.Errorf("account 201 got %d bytes, want 500", byID[201])
	}
	if byID[202] != 500 {
		t.Errorf("account 202 got %d bytes, want 500", byID[202])
	}
}

// TestSyncSingleNode_SingleInboundGetsFullBytes is a regression guard for the
// common case: a single active inbound must receive the full byte count.
func TestSyncSingleNode_SingleInboundGetsFullBytes(t *testing.T) {
	recordTS := time.Now().Unix()

	node := &domain.Node{
		ID: 11, Name: "n11", ConnectMode: "direct", IsActive: true, IsOnline: true,
		Inbounds: []domain.Inbound{
			{ID: 101, Tag: "vless-in"},
		},
	}
	repo := newFakeStatsNodeRepo(node)

	carolSub := &subDomain.Subscription{ID: 9}
	sr := &fakeStatsSubRepo{
		subs: map[string]*subDomain.Subscription{"carol": carolSub},
	}

	ar := newFakeStatsAccountRepo()
	ar.addAccount("carol", 101, &accountDomain.Account{ID: 301})

	fake := &fakeStatsAgentClient{
		buffered: &agent.BufferedTrafficStats{
			Records: []*agent.TrafficRecord{
				{
					Timestamp:        recordTS,
					UserUplink:       map[string]int64{"carol": 700},
					UserDownlink:     map[string]int64{"carol": 300},
					OutboundUplink:   map[string]int64{},
					OutboundDownlink: map[string]int64{},
					InboundUplink:    map[string]int64{"vless-in": 1000},
					InboundDownlink:  map[string]int64{},
				},
			},
			BufferStartTime: recordTS,
			BufferEndTime:   recordTS,
		},
	}

	u := newSyncSingleNodeTestUsecaseWithAccount(repo, sr, ar, fake)
	u.syncSingleNode(context.Background(), node, nil)

	ar.mu.Lock()
	calls := ar.addDataUsedCalls
	ar.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddDataUsed call, got %d: %+v", len(calls), calls)
	}
	if calls[0].id != 301 || calls[0].bytes != 1000 {
		t.Errorf("expected AddDataUsed(301, 1000), got AddDataUsed(%d, %d)", calls[0].id, calls[0].bytes)
	}
}
