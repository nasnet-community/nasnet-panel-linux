package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
)

// ─── Fakes ──────────────────────────────────────────────────────────

type fakeAccounts struct {
	accs       []*accountDomain.Account
	err        error
	byEmail    map[string]*accountDomain.Account
	byEmailErr error
	gotEmails  []string
}

func (f *fakeAccounts) ListAccountsBySubscription(_ context.Context, _ uint) ([]*accountDomain.Account, error) {
	return f.accs, f.err
}

func (f *fakeAccounts) ListAccountsBySubscriptionIDs(_ context.Context, _ []uint) ([]*accountDomain.Account, error) {
	return f.accs, f.err
}

func (f *fakeAccounts) ListAccountsByEmails(_ context.Context, emails []string) ([]*accountDomain.Account, error) {
	f.gotEmails = append([]string(nil), emails...)
	if f.byEmailErr != nil {
		return nil, f.byEmailErr
	}
	out := make([]*accountDomain.Account, 0, len(emails))
	for _, e := range emails {
		if a, ok := f.byEmail[e]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeSettings struct {
	value string
	err   error
}

func (f *fakeSettings) GetByKey(_ context.Context, _ string) (string, error) {
	return f.value, f.err
}

type fakeLogReader struct {
	series    []nodeRepo.AccessLogTimeBucket
	totals    nodeRepo.AccessLogTotals
	rows      []*nodeDomain.AccessLogSummary
	gotFilter nodeRepo.AccessLogSummaryFilter
	gotGran   string
	err       error
	// Search wiring.
	searchHits      []nodeRepo.AccessLogSearchHit
	searchTrunc     bool
	searchErr       error
	gotSearchFilter nodeRepo.AccessLogSearchFilter
}

func (f *fakeLogReader) GetAccessLogTimeSeries(_ context.Context, filter nodeRepo.AccessLogSummaryFilter, granularity string) ([]nodeRepo.AccessLogTimeBucket, error) {
	f.gotFilter = filter
	f.gotGran = granularity
	return f.series, f.err
}

func (f *fakeLogReader) GetAccessLogTotals(_ context.Context, _ nodeRepo.AccessLogSummaryFilter) (nodeRepo.AccessLogTotals, error) {
	return f.totals, f.err
}

func (f *fakeLogReader) GetAccessLogTopDomains(_ context.Context, _ nodeRepo.AccessLogSummaryFilter) ([]*nodeDomain.AccessLogSummary, error) {
	return f.rows, f.err
}

func (f *fakeLogReader) SearchAccessLog(_ context.Context, filter nodeRepo.AccessLogSearchFilter) ([]nodeRepo.AccessLogSearchHit, bool, error) {
	f.gotSearchFilter = filter
	return f.searchHits, f.searchTrunc, f.searchErr
}

func (f *fakeLogReader) GetNodesLastAccessLogSyncedAt(_ context.Context, _ []uint) (map[uint]time.Time, error) {
	return map[uint]time.Time{}, nil
}

// ─── Helpers ────────────────────────────────────────────────────────

func mustJSON(t *testing.T, v map[string]int64) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func acc(email string, nodeID uint) *accountDomain.Account {
	return &accountDomain.Account{
		Email:   email,
		Inbound: &nodeDomain.Inbound{NodeID: nodeID},
	}
}

func newUsecase(t *testing.T, accs []*accountDomain.Account, retention string, reader *fakeLogReader, now time.Time) Usecase {
	t.Helper()
	return New(reader, &fakeAccounts{accs: accs}, &fakeSettings{value: retention}, func() time.Time { return now })
}

// ─── Tests ──────────────────────────────────────────────────────────

func TestValidateRange_Required(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		from time.Time
		to   time.Time
		want error
	}{
		{"missing from", time.Time{}, now, ErrInvalidRange},
		{"missing to", now.Add(-time.Hour), time.Time{}, ErrInvalidRange},
		{"to ≤ from", now, now.Add(-time.Hour), ErrInvalidRange},
		{"to in future", now.Add(-time.Hour), now.Add(48 * time.Hour), ErrInvalidRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRange(tc.from, tc.to, 30, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateRange_Retention(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	from := now.Add(-31 * 24 * time.Hour)
	if err := validateRange(from, now, 30, now); !errors.Is(err, ErrRangeOutsideRetention) {
		t.Fatalf("expected retention error, got %v", err)
	}
	// Inside retention is allowed.
	from = now.Add(-29 * 24 * time.Hour)
	if err := validateRange(from, now, 30, now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPickGranularity(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		req  string
		to   time.Time
		want string
	}{
		{"explicit hour", "hour", from.Add(48 * time.Hour), "hour"},
		{"explicit day", "day", from.Add(time.Hour), "day"},
		{"auto, short range → hour", "", from.Add(24 * time.Hour), "hour"},
		{"auto, exactly 7d → hour (boundary)", "", from.Add(7 * 24 * time.Hour), "hour"},
		{"auto, > 7d → day", "", from.Add(8 * 24 * time.Hour), "day"},
		{"unknown value falls through to auto", "minute", from.Add(8 * 24 * time.Hour), "day"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickGranularity(tc.req, from, tc.to); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestIntersectNodeScope(t *testing.T) {
	owned := []uint{1, 2, 3}
	if got := intersectNodeScope(owned, nil); !reflect.DeepEqual(got, owned) {
		t.Errorf("nil filter should return owned: got %v", got)
	}
	if got := intersectNodeScope(owned, []uint{2}); !reflect.DeepEqual(got, []uint{2}) {
		t.Errorf("matching filter should narrow: got %v", got)
	}
	if got := intersectNodeScope(owned, []uint{99}); !reflect.DeepEqual(got, owned) {
		t.Errorf("zero overlap should fall back to owned (avoid silent empty): got %v", got)
	}
	// Sorted output for deterministic SQL plans.
	if got := intersectNodeScope(owned, []uint{3, 1}); !reflect.DeepEqual(got, []uint{1, 3}) {
		t.Errorf("expected sorted: got %v", got)
	}
}

func TestMergeJSONCounts(t *testing.T) {
	rows := []*nodeDomain.AccessLogSummary{
		{TopDomains: mustJSON(t, map[string]int64{"a.com": 10, "b.com": 5})},
		{TopDomains: mustJSON(t, map[string]int64{"a.com": 7, "c.com": 3})},
		{TopDomains: ""},                // empty row — skip
		{TopDomains: "{not valid json"}, // malformed — skip, must not panic
	}
	got := mergeJSONCounts(rows, func(s *nodeDomain.AccessLogSummary) string { return s.TopDomains }, 10)
	want := []DomainCount{
		{Domain: "a.com", Count: 17},
		{Domain: "b.com", Count: 5},
		{Domain: "c.com", Count: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge mismatch:\n want %+v\n got  %+v", want, got)
	}
}

func TestMergeJSONCounts_TopNCap(t *testing.T) {
	rows := []*nodeDomain.AccessLogSummary{
		{TopDomains: mustJSON(t, map[string]int64{"a": 1, "b": 2, "c": 3, "d": 4})},
	}
	got := mergeJSONCounts(rows, func(s *nodeDomain.AccessLogSummary) string { return s.TopDomains }, 2)
	if len(got) != 2 {
		t.Fatalf("topN should cap to 2, got %d", len(got))
	}
	if got[0].Domain != "d" || got[1].Domain != "c" {
		t.Fatalf("want top-2 by count desc, got %+v", got)
	}
}

func TestGetSubscriptionAccessHistory_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour)

	reader := &fakeLogReader{
		series: []nodeRepo.AccessLogTimeBucket{
			{Bucket: from, AcceptedCount: 10},
			{Bucket: from.Add(time.Hour), AcceptedCount: 5},
		},
		totals: nodeRepo.AccessLogTotals{AcceptedCount: 15, RejectedCount: 1, HourBuckets: 2},
		rows: []*nodeDomain.AccessLogSummary{
			{TopDomains: mustJSON(t, map[string]int64{"x.com": 10})},
			{TopDomains: mustJSON(t, map[string]int64{"y.com": 3, "x.com": 2})},
			{RejectedDomains: mustJSON(t, map[string]int64{"blocked.com": 4})},
			{SourceIPs: mustJSON(t, map[string]int64{"1.2.3.4": 7})},
		},
	}

	uc := newUsecase(t, []*accountDomain.Account{
		acc("alice@x", 1),
		acc("alice@x", 2), // duplicate email different node — should fold into one email
		acc("bob@x", 1),
	}, "30", reader, now)

	resp, err := uc.GetSubscriptionAccessHistory(context.Background(), Request{
		SubscriptionID:   42,
		From:             from,
		To:               now,
		IncludeSourceIPs: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Granularity != "hour" {
		t.Errorf("granularity: want hour, got %q", resp.Granularity)
	}
	if resp.EmailsResolved != 2 {
		t.Errorf("emails resolved: want 2, got %d", resp.EmailsResolved)
	}
	if !reflect.DeepEqual(resp.NodesQueried, []uint{1, 2}) {
		t.Errorf("nodes queried: want [1 2], got %v", resp.NodesQueried)
	}
	if resp.RetentionDays != 30 {
		t.Errorf("retention: want 30, got %d", resp.RetentionDays)
	}
	if !reflect.DeepEqual(reader.gotFilter.SubscriptionIDs, []uint{42}) {
		t.Errorf("filter subscription_ids not propagated: %v", reader.gotFilter.SubscriptionIDs)
	}
	if reader.gotFilter.From != from || reader.gotFilter.To != now {
		t.Errorf("filter window not propagated: %+v", reader.gotFilter)
	}
	if reader.gotGran != "hour" {
		t.Errorf("granularity not propagated to repo: %q", reader.gotGran)
	}

	if len(resp.TopDomains) != 2 || resp.TopDomains[0].Domain != "x.com" || resp.TopDomains[0].Count != 12 {
		t.Errorf("top domains: %+v", resp.TopDomains)
	}
	if len(resp.TopRejected) != 1 || resp.TopRejected[0].Domain != "blocked.com" {
		t.Errorf("top rejected: %+v", resp.TopRejected)
	}
	if len(resp.TopSourceIPs) != 1 || resp.TopSourceIPs[0].IP != "1.2.3.4" {
		t.Errorf("source ips: %+v", resp.TopSourceIPs)
	}
	if resp.Totals.AcceptedCount != 15 {
		t.Errorf("totals: %+v", resp.Totals)
	}
}

func TestGetSubscriptionAccessHistory_OmitsSourceIPsByDefault(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	reader := &fakeLogReader{
		rows: []*nodeDomain.AccessLogSummary{{SourceIPs: mustJSON(t, map[string]int64{"1.1.1.1": 1})}},
	}
	uc := newUsecase(t, []*accountDomain.Account{acc("u@x", 1)}, "30", reader, now)
	resp, err := uc.GetSubscriptionAccessHistory(context.Background(), Request{
		SubscriptionID: 1,
		From:           now.Add(-time.Hour),
		To:             now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TopSourceIPs != nil {
		t.Errorf("source IPs leaked when IncludeSourceIPs=false: %+v", resp.TopSourceIPs)
	}
}

func TestGetSubscriptionAccessHistory_EmptySubscription(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	uc := newUsecase(t, nil, "30", &fakeLogReader{}, now)
	_, err := uc.GetSubscriptionAccessHistory(context.Background(), Request{
		SubscriptionID: 1,
		From:           now.Add(-time.Hour),
		To:             now,
	})
	if !errors.Is(err, ErrSubscriptionEmpty) {
		t.Fatalf("want ErrSubscriptionEmpty, got %v", err)
	}
}

func TestGetSubscriptionAccessHistory_RangeRejected(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	uc := newUsecase(t, []*accountDomain.Account{acc("u@x", 1)}, "30", &fakeLogReader{}, now)
	_, err := uc.GetSubscriptionAccessHistory(context.Background(), Request{
		SubscriptionID: 1,
		From:           now.Add(-90 * 24 * time.Hour),
		To:             now,
	})
	if !errors.Is(err, ErrRangeOutsideRetention) {
		t.Fatalf("want retention error, got %v", err)
	}
}

// ─── Search ─────────────────────────────────────────────────────────

func TestSearch_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour)
	to := now

	hourA := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	hourB := time.Date(2026, 5, 6, 15, 0, 0, 0, time.UTC)

	reader := &fakeLogReader{
		searchHits: []nodeRepo.AccessLogSearchHit{
			{Bucket: hourA, NodeID: 1, Email: "u1@a", Kind: "domain", Value: "google.com", Count: 12},
			{Bucket: hourB, NodeID: 1, Email: "u1@a", Kind: "domain", Value: "google.com", Count: 5},
			{Bucket: hourA, NodeID: 2, Email: "u2@a", Kind: "rejected_domain", Value: "ads.google.com", Count: 3},
		},
	}
	uc := newUsecase(t, []*accountDomain.Account{acc("u1@a", 1), acc("u2@a", 2)}, "30", reader, now)

	resp, err := uc.SearchSubscriptionAccessLog(context.Background(), SearchRequest{
		SubscriptionID: 7,
		From:           from,
		To:             to,
		Query:          "google",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Query != "google" {
		t.Errorf("query: %q", resp.Query)
	}
	if len(resp.Hits) != 3 {
		t.Errorf("hits: %d", len(resp.Hits))
	}
	if len(resp.Aggregates) != 2 {
		t.Fatalf("aggregates: %d", len(resp.Aggregates))
	}
	// Top aggregate = google.com (12+5=17 across 2 hours).
	if resp.Aggregates[0].Value != "google.com" || resp.Aggregates[0].Count != 17 || resp.Aggregates[0].Hours != 2 {
		t.Errorf("top aggregate: %+v", resp.Aggregates[0])
	}
	if resp.Aggregates[1].Value != "ads.google.com" || resp.Aggregates[1].Count != 3 || resp.Aggregates[1].Hours != 1 {
		t.Errorf("second aggregate: %+v", resp.Aggregates[1])
	}
	// Source-IP scanning blocked unless explicitly opted in.
	for _, k := range reader.gotSearchFilter.Kinds {
		if k == "source_ip" {
			t.Errorf("source_ip leaked into kinds: %v", reader.gotSearchFilter.Kinds)
		}
	}
}

func TestSearch_QueryTooShort(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	uc := newUsecase(t, []*accountDomain.Account{acc("u@a", 1)}, "30", &fakeLogReader{}, now)
	_, err := uc.SearchSubscriptionAccessLog(context.Background(), SearchRequest{
		SubscriptionID: 1,
		From:           now.Add(-time.Hour),
		To:             now,
		Query:          "a",
	})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("want ErrInvalidQuery, got %v", err)
	}
}

func TestSearch_SourceIPGated(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	reader := &fakeLogReader{}
	uc := newUsecase(t, []*accountDomain.Account{acc("u@a", 1)}, "30", reader, now)

	// Without IncludeSourceIPs, requesting source_ip kind silently drops it
	// and falls back to default kinds (domain + rejected_domain).
	if _, err := uc.SearchSubscriptionAccessLog(context.Background(), SearchRequest{
		SubscriptionID: 1,
		From:           now.Add(-time.Hour),
		To:             now,
		Query:          "1.2",
		Kinds:          []string{"source_ip"},
	}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, k := range reader.gotSearchFilter.Kinds {
		if k == "source_ip" {
			t.Errorf("source_ip leaked when IncludeSourceIPs=false: %v", reader.gotSearchFilter.Kinds)
		}
	}

	// With opt-in, source_ip survives.
	if _, err := uc.SearchSubscriptionAccessLog(context.Background(), SearchRequest{
		SubscriptionID:   1,
		From:             now.Add(-time.Hour),
		To:               now,
		Query:            "1.2",
		Kinds:            []string{"source_ip"},
		IncludeSourceIPs: true,
	}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(reader.gotSearchFilter.Kinds) != 1 || reader.gotSearchFilter.Kinds[0] != "source_ip" {
		t.Errorf("source_ip not propagated: %v", reader.gotSearchFilter.Kinds)
	}
}

func TestSearch_EmptySubscription(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	uc := newUsecase(t, nil, "30", &fakeLogReader{}, now)
	_, err := uc.SearchSubscriptionAccessLog(context.Background(), SearchRequest{
		SubscriptionID: 1,
		From:           now.Add(-time.Hour),
		To:             now,
		Query:          "google",
	})
	if !errors.Is(err, ErrSubscriptionEmpty) {
		t.Fatalf("want ErrSubscriptionEmpty, got %v", err)
	}
}

func TestSearch_RangeRejected(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	uc := newUsecase(t, []*accountDomain.Account{acc("u@a", 1)}, "30", &fakeLogReader{}, now)
	_, err := uc.SearchSubscriptionAccessLog(context.Background(), SearchRequest{
		SubscriptionID: 1,
		From:           now.Add(-365 * 24 * time.Hour),
		To:             now,
		Query:          "google",
	})
	if !errors.Is(err, ErrRangeOutsideRetention) {
		t.Fatalf("want ErrRangeOutsideRetention, got %v", err)
	}
}

func TestRetentionDays_Defaults(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		raw  string
		err  error
		want int
	}{
		{"unset", "", nil, defaultRetentionDays},
		{"setting error → default", "", errors.New("boom"), defaultRetentionDays},
		{"valid number", "60", nil, 60},
		{"zero means forever (large cap)", "0", nil, 365 * 10},
		{"garbage falls back", "abc", nil, defaultRetentionDays},
		{"negative falls back", "-5", nil, defaultRetentionDays},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := New(&fakeLogReader{}, &fakeAccounts{}, &fakeSettings{value: tc.raw, err: tc.err}, func() time.Time { return now }).(*accessHistoryUsecase)
			got := uc.readRetentionDays(context.Background())
			if got != tc.want {
				t.Fatalf("want %d, got %d", tc.want, got)
			}
		})
	}
}

// ─── Global Search ──────────────────────────────────────────────────

func TestSearchGlobal_HappyPath_BatchResolves(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour)
	to := now

	hourA := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	hourB := time.Date(2026, 5, 6, 15, 0, 0, 0, time.UTC)

	subA := uint(101)
	subB := uint(202)
	userA := uint(11)
	userB := uint(22)

	reader := &fakeLogReader{
		searchHits: []nodeRepo.AccessLogSearchHit{
			{Bucket: hourA, NodeID: 1, Email: "u1@x", Kind: "domain", Value: "google.com", Count: 12},
			{Bucket: hourB, NodeID: 1, Email: "u1@x", Kind: "domain", Value: "google.com", Count: 5},
			{Bucket: hourA, NodeID: 2, Email: "u2@x", Kind: "domain", Value: "google.com", Count: 3},
			{Bucket: hourA, NodeID: 1, Email: "ghost@x", Kind: "domain", Value: "google.com", Count: 1}, // unattached
		},
	}
	accs := &fakeAccounts{
		byEmail: map[string]*accountDomain.Account{
			"u1@x": {
				Email:          "u1@x",
				SubscriptionID: &subA,
				Subscription:   &subDomain.Subscription{ID: subA, UserID: &userA, Label: "Alice"},
			},
			"u2@x": {
				Email:          "u2@x",
				SubscriptionID: &subB,
				Subscription:   &subDomain.Subscription{ID: subB, UserID: &userB, Label: "Bob"},
			},
		},
	}
	uc := New(reader, accs, &fakeSettings{value: "30"}, func() time.Time { return now })

	resp, err := uc.SearchGlobalAccessLog(context.Background(), GlobalSearchRequest{
		From:  from,
		To:    to,
		Query: "google",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if len(resp.Hits) != 4 {
		t.Errorf("hits: %d", len(resp.Hits))
	}
	// Unattached hit retained but with subID/userID = 0.
	var ghost *GlobalSearchHit
	for i := range resp.Hits {
		if resp.Hits[i].Email == "ghost@x" {
			ghost = &resp.Hits[i]
			break
		}
	}
	if ghost == nil {
		t.Fatal("ghost hit missing")
	}
	if ghost.SubscriptionID != 0 || ghost.UserID != 0 {
		t.Errorf("unattached hit should have zero IDs: %+v", ghost)
	}

	// Aggregates by subscription: subA has 17 hits across 2 hours; subB has 3
	// hits across 1 hour; ghost (subID=0) has 1 hit.
	if len(resp.BySubscription) != 3 {
		t.Fatalf("by_subscription: want 3 rows, got %d", len(resp.BySubscription))
	}
	if resp.BySubscription[0].SubscriptionID != subA || resp.BySubscription[0].Count != 17 || resp.BySubscription[0].Hours != 2 {
		t.Errorf("top sub agg: %+v", resp.BySubscription[0])
	}

	// Aggregates by value: google.com has 21 total, across 3 distinct subs
	// (subA, subB, 0) and 2 distinct hour buckets.
	if len(resp.ByValue) != 1 {
		t.Fatalf("by_value: want 1 row, got %d", len(resp.ByValue))
	}
	v := resp.ByValue[0]
	if v.Value != "google.com" || v.Count != 21 || v.Subscriptions != 3 || v.Hours != 2 {
		t.Errorf("by_value row: %+v", v)
	}

	// Email batch contained the unique set; no per-row queries.
	if got := len(accs.gotEmails); got != 3 {
		t.Errorf("expected 3 unique emails resolved in one batch, got %d", got)
	}
}

func TestSearchGlobal_QueryTooShort(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	uc := New(&fakeLogReader{}, &fakeAccounts{}, &fakeSettings{value: "30"}, func() time.Time { return now })
	_, err := uc.SearchGlobalAccessLog(context.Background(), GlobalSearchRequest{
		From:  now.Add(-time.Hour),
		To:    now,
		Query: "a",
	})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("want ErrInvalidQuery, got %v", err)
	}
}

func TestSearchGlobal_RangeRejected(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	uc := New(&fakeLogReader{}, &fakeAccounts{}, &fakeSettings{value: "30"}, func() time.Time { return now })
	_, err := uc.SearchGlobalAccessLog(context.Background(), GlobalSearchRequest{
		From:  now.Add(-365 * 24 * time.Hour),
		To:    now,
		Query: "google",
	})
	if !errors.Is(err, ErrRangeOutsideRetention) {
		t.Fatalf("want ErrRangeOutsideRetention, got %v", err)
	}
}

func TestSearchGlobal_SourceIPGated(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	reader := &fakeLogReader{}
	uc := New(reader, &fakeAccounts{}, &fakeSettings{value: "30"}, func() time.Time { return now })
	if _, err := uc.SearchGlobalAccessLog(context.Background(), GlobalSearchRequest{
		From:  now.Add(-time.Hour),
		To:    now,
		Query: "1.2",
		Kinds: []string{"source_ip"},
	}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, k := range reader.gotSearchFilter.Kinds {
		if k == "source_ip" {
			t.Errorf("source_ip leaked when IncludeSourceIPs=false: %v", reader.gotSearchFilter.Kinds)
		}
	}
}
