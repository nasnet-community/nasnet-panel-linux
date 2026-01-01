// Package usecase implements the per-subscription access-history query path.
// Fans hourly access_log_summaries rows across a subscription's emails,
// windowed by the retention setting.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"golang.org/x/sync/errgroup"
)

// AccountLister is the slim slice of internal/shared/contract.AccountManager
// that this package needs. Declared locally so tests can supply a stub
// without dragging the full contract surface in.
type AccountLister interface {
	ListAccountsBySubscription(ctx context.Context, subID uint) ([]*accountDomain.Account, error)
	// ListAccountsBySubscriptionIDs is the batched variant — global search
	// uses it to collapse N per-sub round-trips into one query.
	ListAccountsBySubscriptionIDs(ctx context.Context, subIDs []uint) ([]*accountDomain.Account, error)
	// ListAccountsByEmails reverse-resolves emails to their owning accounts
	// (with Subscription preloaded) so the global search can attach
	// (subscription_id, user_id, label) to each hit.
	ListAccountsByEmails(ctx context.Context, emails []string) ([]*accountDomain.Account, error)
}

// SettingReader matches the GetByKey method on internal/setting/domain.SettingUsecase.
type SettingReader interface {
	GetByKey(ctx context.Context, key string) (string, error)
}

// AccessLogReader is the slice of internal/node/repository.NodeRepository
// the access-history flow needs. Keeping it local avoids importing the
// full ~70-method NodeRepository surface in tests.
type AccessLogReader interface {
	GetAccessLogTimeSeries(ctx context.Context, filter nodeRepo.AccessLogSummaryFilter, granularity string) ([]nodeRepo.AccessLogTimeBucket, error)
	GetAccessLogTotals(ctx context.Context, filter nodeRepo.AccessLogSummaryFilter) (nodeRepo.AccessLogTotals, error)
	GetAccessLogTopDomains(ctx context.Context, filter nodeRepo.AccessLogSummaryFilter) ([]*nodeDomain.AccessLogSummary, error)
	SearchAccessLog(ctx context.Context, filter nodeRepo.AccessLogSearchFilter) ([]nodeRepo.AccessLogSearchHit, bool, error)
	// GetNodesLastAccessLogSyncedAt powers the freshness pill — nodes
	// with no recorded sync are omitted from the returned map.
	GetNodesLastAccessLogSyncedAt(ctx context.Context, nodeIDs []uint) (map[uint]time.Time, error)
}

// Usecase exposes the access-history query.
type Usecase interface {
	GetSubscriptionAccessHistory(ctx context.Context, req Request) (*Response, error)
	SearchSubscriptionAccessLog(ctx context.Context, req SearchRequest) (*SearchResponse, error)
	SearchGlobalAccessLog(ctx context.Context, req GlobalSearchRequest) (*GlobalSearchResponse, error)
}

// GlobalSearchRequest is the input to the cross-subscription search. The
// scope spans every subscription with retained summaries; the result attaches
// each hit to its owning subscription via a batched email lookup.
type GlobalSearchRequest struct {
	From             time.Time
	To               time.Time
	NodeIDs          []uint
	SubscriptionIDs  []uint
	Emails           []string
	Query            string
	Kinds            []string
	Limit            int
	IncludeSourceIPs bool
}

// GlobalSearchResponse mirrors SearchResponse but adds subscription
// resolution to each hit + an aggregate-by-subscription rollup so the UI
// doesn't have to re-derive that on the client.
type GlobalSearchResponse struct {
	From           time.Time              `json:"from"`
	To             time.Time              `json:"to"`
	Query          string                 `json:"query"`
	Kinds          []string               `json:"kinds"`
	Hits           []GlobalSearchHit      `json:"hits"`
	BySubscription []GlobalSubAggregate   `json:"by_subscription"`
	ByValue        []GlobalValueAggregate `json:"by_value"`
	Truncated      bool                   `json:"truncated"`
	RetentionDays  int                    `json:"retention_days"`
	NodesQueried   []uint                 `json:"nodes_queried"`
	LastSyncedAt   map[uint]time.Time     `json:"last_synced_at"`
}

// GlobalSearchHit: SearchHit + resolved sub/user identity. SubscriptionID=0
// when email no longer maps (account deleted, summary not yet aged out).
type GlobalSearchHit struct {
	Bucket            time.Time `json:"bucket"`
	NodeID            uint      `json:"node_id"`
	Email             string    `json:"email"`
	SubscriptionID    uint      `json:"subscription_id"`
	UserID            uint      `json:"user_id"`
	SubscriptionLabel string    `json:"subscription_label,omitempty"`
	Kind              string    `json:"kind"`
	Value             string    `json:"value"`
	Count             int64     `json:"count"`
}

// GlobalSubAggregate rolls up hits per (kind, value, subscription) so the UI
// can show "domain X — sub 42 hit 17 times across 3 hours".
type GlobalSubAggregate struct {
	Kind              string `json:"kind"`
	Value             string `json:"value"`
	SubscriptionID    uint   `json:"subscription_id"`
	UserID            uint   `json:"user_id"`
	SubscriptionLabel string `json:"subscription_label,omitempty"`
	Count             int64  `json:"count"`
	Hours             int    `json:"hours"`
}

// GlobalValueAggregate rolls up hits per (kind, value) across all
// subscriptions — the global "popularity" of a domain/IP in the window.
type GlobalValueAggregate struct {
	Kind          string `json:"kind"`
	Value         string `json:"value"`
	Count         int64  `json:"count"`
	Subscriptions int    `json:"subscriptions"`
	Hours         int    `json:"hours"`
}

// SearchRequest is the input to the global search query. Query is a
// case-insensitive substring; Kinds restricts the JSON columns scanned (empty =
// all three). Limit caps the number of returned hits.
type SearchRequest struct {
	SubscriptionID   uint
	From             time.Time
	To               time.Time
	NodeIDs          []uint
	Query            string
	Kinds            []string
	Limit            int
	IncludeSourceIPs bool // sub-panel callers should pass false
}

// SearchResponse is the assembled search payload.
type SearchResponse struct {
	From           time.Time          `json:"from"`
	To             time.Time          `json:"to"`
	Query          string             `json:"query"`
	Kinds          []string           `json:"kinds"`
	Hits           []SearchHit        `json:"hits"`
	Aggregates     []SearchAggregate  `json:"aggregates"`
	Truncated      bool               `json:"truncated"`
	NodesQueried   []uint             `json:"nodes_queried"`
	EmailsResolved int                `json:"emails_resolved"`
	RetentionDays  int                `json:"retention_days"`
	LastSyncedAt   map[uint]time.Time `json:"last_synced_at"`
}

// SearchHit is a frontend-friendly mirror of nodeRepo.AccessLogSearchHit.
type SearchHit struct {
	Bucket time.Time `json:"bucket"`
	NodeID uint      `json:"node_id"`
	Email  string    `json:"email"`
	Kind   string    `json:"kind"`
	Value  string    `json:"value"`
	Count  int64     `json:"count"`
}

// SearchAggregate rolls up matching hits by (kind, value) across the entire
// window so the UI can show totals without re-summing per row.
type SearchAggregate struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Count int64  `json:"count"`
	Hours int    `json:"hours"` // distinct hour buckets the value showed up in
}

// Request is the input to the history query.
type Request struct {
	SubscriptionID uint
	From           time.Time
	To             time.Time
	// Granularity is "hour" or "day". Empty selects "hour" for ranges
	// ≤ autoGranularityCutoff and "day" otherwise.
	Granularity string
	// NodeIDs optionally narrows the scope to a subset of the
	// subscription's nodes. Admin-only knob; sub-panel callers should
	// leave this empty.
	NodeIDs []uint
	// TopN is the number of top domains / IPs returned. Capped at maxTopN.
	TopN int
	// IncludeSourceIPs gates the source-IP aggregate. Sub-panel callers
	// should pass false unless the operator explicitly opts in.
	IncludeSourceIPs bool
}

// Response is the assembled history payload.
type Response struct {
	From           time.Time                      `json:"from"`
	To             time.Time                      `json:"to"`
	Granularity    string                         `json:"granularity"`
	Series         []nodeRepo.AccessLogTimeBucket `json:"series"`
	TopDomains     []DomainCount                  `json:"top_domains"`
	TopRejected    []DomainCount                  `json:"top_rejected"`
	TopSourceIPs   []IPCount                      `json:"top_source_ips,omitempty"`
	Totals         nodeRepo.AccessLogTotals       `json:"totals"`
	NodesQueried   []uint                         `json:"nodes_queried"`
	EmailsResolved int                            `json:"emails_resolved"`
	RetentionDays  int                            `json:"retention_days"`
	LastSyncedAt   map[uint]time.Time             `json:"last_synced_at"`
}

// DomainCount is one row in the top-domains list.
type DomainCount struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

// IPCount is one row in the top-IPs list.
type IPCount struct {
	IP    string `json:"ip"`
	Count int64  `json:"count"`
}

// Errors surfaced by the usecase. HTTP layer can map these to status codes.
var (
	ErrInvalidRange          = errors.New("invalid date range")
	ErrRangeOutsideRetention = errors.New("date range exceeds retention window")
	ErrSubscriptionEmpty     = errors.New("subscription has no provisioned accounts")
	ErrInvalidQuery          = errors.New("invalid search query")
)

// Tunables. Hour granularity for ≤ 7 d, day for longer windows. The cap on
// TopN keeps the JSON merge bounded; 100 covers any realistic UI need.
const (
	autoGranularityCutoff = 7 * 24 * time.Hour
	maxTopN               = 100
	defaultTopN           = 20
	defaultRetentionDays  = 30
	settingKey            = "retention_access_log_days"

	// Search tunables.
	minQueryLen        = 2   // require ≥2 chars to keep result sets sane
	maxQueryLen        = 253 // longest valid hostname label-set
	defaultSearchLimit = 200
	maxSearchLimit     = 1000

	// Global search tunables — broader scope so we let it scan a bit more
	// before truncating, but capped to keep response payloads reasonable.
	defaultGlobalSearchLimit = 500
	maxGlobalSearchLimit     = 2000
)

type accessHistoryUsecase struct {
	logReader AccessLogReader
	accounts  AccountLister
	settings  SettingReader
	now       func() time.Time // injected for tests
}

// New builds the usecase. now is optional — pass nil to use time.Now.
func New(logReader AccessLogReader, accounts AccountLister, settings SettingReader, now func() time.Time) Usecase {
	if now == nil {
		now = time.Now
	}
	return &accessHistoryUsecase{
		logReader: logReader,
		accounts:  accounts,
		settings:  settings,
		now:       now,
	}
}

// GetSubscriptionAccessHistory resolves the subscription's emails / nodes,
// validates the requested window against retention, and returns the merged
// time series + top-N aggregates.
func (u *accessHistoryUsecase) GetSubscriptionAccessHistory(ctx context.Context, req Request) (*Response, error) {
	retentionDays := u.readRetentionDays(ctx)

	if err := validateRange(req.From, req.To, retentionDays, u.now()); err != nil {
		return nil, err
	}

	emails, ownedNodeIDs, err := u.resolveSubscriptionScope(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if len(emails) == 0 {
		return nil, ErrSubscriptionEmpty
	}

	nodeIDs := intersectNodeScope(ownedNodeIDs, req.NodeIDs)

	granularity := pickGranularity(req.Granularity, req.From, req.To)
	topN := req.TopN
	if topN <= 0 {
		topN = defaultTopN
	}
	if topN > maxTopN {
		topN = maxTopN
	}

	// Pass BOTH subscription_id and emails so the repository can do a
	// hybrid match: post-backfill rows match via subscription_id; pre-backfill
	// rows (subscription_id=0) still match via email. Leak prevention is
	// preserved because emails were resolved from this exact subscription.
	filter := nodeRepo.AccessLogSummaryFilter{
		SubscriptionIDs: []uint{req.SubscriptionID},
		Emails:          emails,
		NodeIDs:         nodeIDs,
		From:            req.From,
		To:              req.To,
	}

	var (
		series      []nodeRepo.AccessLogTimeBucket
		totals      nodeRepo.AccessLogTotals
		summaryRows []*nodeDomain.AccessLogSummary
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		series, err = u.logReader.GetAccessLogTimeSeries(gctx, filter, granularity)
		return err
	})
	g.Go(func() error {
		var err error
		totals, err = u.logReader.GetAccessLogTotals(gctx, filter)
		return err
	})
	g.Go(func() error {
		var err error
		summaryRows, err = u.logReader.GetAccessLogTopDomains(gctx, filter)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("query access history: %w", err)
	}

	topDomains := mergeJSONCounts(summaryRows, func(s *nodeDomain.AccessLogSummary) string { return s.TopDomains }, topN)
	topRejected := mergeJSONCounts(summaryRows, func(s *nodeDomain.AccessLogSummary) string { return s.RejectedDomains }, topN)

	resp := &Response{
		From:           req.From,
		To:             req.To,
		Granularity:    granularity,
		Series:         series,
		TopDomains:     topDomains,
		TopRejected:    topRejected,
		Totals:         totals,
		NodesQueried:   nodeIDs,
		EmailsResolved: len(emails),
		RetentionDays:  retentionDays,
		LastSyncedAt:   u.fetchLastSynced(ctx, nodeIDs),
	}

	if req.IncludeSourceIPs {
		ips := mergeIPCounts(summaryRows, topN)
		resp.TopSourceIPs = ips
	}

	return resp, nil
}

func (u *accessHistoryUsecase) readRetentionDays(ctx context.Context) int {
	raw, err := u.settings.GetByKey(ctx, settingKey)
	if err != nil || raw == "" {
		return defaultRetentionDays
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultRetentionDays
	}
	if n == 0 {
		// 0 means "keep forever" in settings semantics. For the panel
		// limit we treat it as effectively unbounded — anything on disk
		// is fair game.
		return 365 * 10
	}
	return n
}

func (u *accessHistoryUsecase) resolveSubscriptionScope(ctx context.Context, subID uint) (emails []string, nodeIDs []uint, err error) {
	accs, err := u.accounts.ListAccountsBySubscription(ctx, subID)
	if err != nil {
		return nil, nil, fmt.Errorf("list subscription accounts: %w", err)
	}
	emailSet := map[string]struct{}{}
	nodeSet := map[uint]struct{}{}
	for _, a := range accs {
		if a == nil || a.Email == "" {
			continue
		}
		emailSet[a.Email] = struct{}{}
		if a.Inbound != nil && a.Inbound.NodeID != 0 {
			nodeSet[a.Inbound.NodeID] = struct{}{}
		}
	}
	emails = make([]string, 0, len(emailSet))
	for e := range emailSet {
		emails = append(emails, e)
	}
	sort.Strings(emails) // stable output for tests + UI memoization

	nodeIDs = make([]uint, 0, len(nodeSet))
	for n := range nodeSet {
		nodeIDs = append(nodeIDs, n)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	return emails, nodeIDs, nil
}

func validateRange(from, to time.Time, retentionDays int, now time.Time) error {
	if from.IsZero() || to.IsZero() {
		return fmt.Errorf("%w: from and to are required", ErrInvalidRange)
	}
	if !to.After(from) {
		return fmt.Errorf("%w: to must be after from", ErrInvalidRange)
	}
	if to.After(now.Add(time.Hour)) {
		// Tolerate small clock skew; reject obviously future ranges.
		return fmt.Errorf("%w: to is in the future", ErrInvalidRange)
	}
	maxWindow := time.Duration(retentionDays) * 24 * time.Hour
	if to.Sub(from) > maxWindow {
		return fmt.Errorf("%w: window %s exceeds retention %d days", ErrRangeOutsideRetention, to.Sub(from), retentionDays)
	}
	return nil
}

func pickGranularity(requested string, from, to time.Time) string {
	switch requested {
	case "hour", "day":
		return requested
	}
	if to.Sub(from) > autoGranularityCutoff {
		return "day"
	}
	return "hour"
}

// intersectNodeScope: owned ∩ requested. Empty requested → owned.
// Empty intersection also → owned (don't silently mask misuse).
func intersectNodeScope(owned, requested []uint) []uint {
	if len(requested) == 0 {
		return owned
	}
	ownedSet := map[uint]struct{}{}
	for _, n := range owned {
		ownedSet[n] = struct{}{}
	}
	out := make([]uint, 0, len(requested))
	for _, n := range requested {
		if _, ok := ownedSet[n]; ok {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return owned
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// mergeJSONCounts unrolls per-row top-N JSON maps and re-aggregates.
// Malformed rows skipped (one bad row must not break the response).
func mergeJSONCounts(rows []*nodeDomain.AccessLogSummary, pick func(*nodeDomain.AccessLogSummary) string, topN int) []DomainCount {
	combined := map[string]int64{}
	for _, r := range rows {
		raw := pick(r)
		if raw == "" {
			continue
		}
		m := map[string]int64{}
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			continue
		}
		for k, v := range m {
			combined[k] += v
		}
	}
	out := make([]DomainCount, 0, len(combined))
	for k, v := range combined {
		out = append(out, DomainCount{Domain: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Domain < out[j].Domain
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// SearchSubscriptionAccessLog: ILIKE-prefiltered substring scan over the
// per-hour top-N JSON blobs. Retention-windowed like GetSubscriptionAccessHistory.
// Blind spot: keys below the agent's per-hour top-N cap aren't stored.
func (u *accessHistoryUsecase) SearchSubscriptionAccessLog(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	retentionDays := u.readRetentionDays(ctx)
	if err := validateRange(req.From, req.To, retentionDays, u.now()); err != nil {
		return nil, err
	}
	q, kinds, err := normaliseSearchInput(req.Query, req.Kinds, req.IncludeSourceIPs)
	if err != nil {
		return nil, err
	}

	emails, ownedNodeIDs, err := u.resolveSubscriptionScope(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if len(emails) == 0 {
		return nil, ErrSubscriptionEmpty
	}
	nodeIDs := intersectNodeScope(ownedNodeIDs, req.NodeIDs)

	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	hits, truncated, err := u.logReader.SearchAccessLog(ctx, nodeRepo.AccessLogSearchFilter{
		// Hybrid scope: matches post-backfill rows by subscription_id and
		// legacy/un-backfilled rows by email. See per-sub history filter
		// for the rationale.
		SubscriptionIDs: []uint{req.SubscriptionID},
		Emails:          emails,
		NodeIDs:         nodeIDs,
		From:            req.From,
		To:              req.To,
		Query:           q,
		Kinds:           kinds,
		Limit:           limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search access log: %w", err)
	}

	out := &SearchResponse{
		From:           req.From,
		To:             req.To,
		Query:          q,
		Kinds:          kinds,
		Hits:           make([]SearchHit, 0, len(hits)),
		Aggregates:     aggregateSearchHits(hits),
		Truncated:      truncated,
		NodesQueried:   nodeIDs,
		EmailsResolved: len(emails),
		RetentionDays:  retentionDays,
		LastSyncedAt:   u.fetchLastSynced(ctx, nodeIDs),
	}
	for _, h := range hits {
		out.Hits = append(out.Hits, SearchHit{
			Bucket: h.Bucket,
			NodeID: h.NodeID,
			Email:  h.Email,
			Kind:   h.Kind,
			Value:  h.Value,
			Count:  h.Count,
		})
	}
	return out, nil
}

// normaliseSearchInput validates the query string and resolves the kinds
// slice. Source-IP scanning is only allowed when IncludeSourceIPs is true so
// the sub-panel can't bypass the privacy gate by submitting kinds=source_ip.
func normaliseSearchInput(query string, kinds []string, includeSourceIPs bool) (string, []string, error) {
	q := strings.TrimSpace(query)
	if len(q) < minQueryLen {
		return "", nil, fmt.Errorf("%w: query must be at least %d characters", ErrInvalidQuery, minQueryLen)
	}
	if len(q) > maxQueryLen {
		return "", nil, fmt.Errorf("%w: query exceeds %d characters", ErrInvalidQuery, maxQueryLen)
	}
	allowed := map[string]struct{}{"domain": {}, "rejected_domain": {}}
	if includeSourceIPs {
		allowed["source_ip"] = struct{}{}
	}
	out := make([]string, 0, len(kinds))
	seen := map[string]struct{}{}
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if _, ok := allowed[k]; !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	if len(out) == 0 {
		// Default to all allowed kinds (excludes source_ip when not opted in).
		for k := range allowed {
			out = append(out, k)
		}
		sort.Strings(out)
	}
	return q, out, nil
}

// aggregateSearchHits collapses (kind, value) duplicates across hours so the
// UI can show "this domain showed up 47 times across 8 hours" without redoing
// the math client-side. Sort: count desc, hours desc, value asc.
func aggregateSearchHits(hits []nodeRepo.AccessLogSearchHit) []SearchAggregate {
	type key struct{ kind, value string }
	bucketSet := map[key]map[time.Time]struct{}{}
	totals := map[key]int64{}
	for _, h := range hits {
		k := key{h.Kind, h.Value}
		totals[k] += h.Count
		set, ok := bucketSet[k]
		if !ok {
			set = map[time.Time]struct{}{}
			bucketSet[k] = set
		}
		set[h.Bucket] = struct{}{}
	}
	out := make([]SearchAggregate, 0, len(totals))
	for k, total := range totals {
		out = append(out, SearchAggregate{
			Kind:  k.kind,
			Value: k.value,
			Count: total,
			Hours: len(bucketSet[k]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Hours != out[j].Hours {
			return out[i].Hours > out[j].Hours
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// mergeIPCounts mirrors mergeJSONCounts but for the SourceIPs JSON column.
func mergeIPCounts(rows []*nodeDomain.AccessLogSummary, topN int) []IPCount {
	combined := map[string]int64{}
	for _, r := range rows {
		if r.SourceIPs == "" {
			continue
		}
		m := map[string]int64{}
		if err := json.Unmarshal([]byte(r.SourceIPs), &m); err != nil {
			continue
		}
		for k, v := range m {
			combined[k] += v
		}
	}
	out := make([]IPCount, 0, len(combined))
	for k, v := range combined {
		out = append(out, IPCount{IP: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// SearchGlobalAccessLog: substring scan across every subscription's
// emails. One batched account lookup reverse-resolves hit emails to
// (sub_id, user_id) — no N+1 follow-up.
func (u *accessHistoryUsecase) SearchGlobalAccessLog(ctx context.Context, req GlobalSearchRequest) (*GlobalSearchResponse, error) {
	retentionDays := u.readRetentionDays(ctx)
	if err := validateRange(req.From, req.To, retentionDays, u.now()); err != nil {
		return nil, err
	}
	q, kinds, err := normaliseSearchInput(req.Query, req.Kinds, req.IncludeSourceIPs)
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultGlobalSearchLimit
	}
	if limit > maxGlobalSearchLimit {
		limit = maxGlobalSearchLimit
	}

	// Subscription IDs and explicit emails are AND-combined into a single
	// scopedEmails slice that narrows the repo query. Either input alone is
	// also fine. If the intersection is empty, short-circuit.
	emptyResp := func() *GlobalSearchResponse {
		return &GlobalSearchResponse{
			From: req.From, To: req.To, Query: q, Kinds: kinds,
			Hits:           []GlobalSearchHit{},
			BySubscription: []GlobalSubAggregate{},
			ByValue:        []GlobalValueAggregate{},
			NodesQueried:   req.NodeIDs,
			RetentionDays:  retentionDays,
			LastSyncedAt:   map[uint]time.Time{},
		}
	}
	var scopedEmails []string
	var scopedSubs []uint
	if len(req.SubscriptionIDs) > 0 {
		scopedSubs = append(scopedSubs, req.SubscriptionIDs...)
		// One batched call replaces the per-sub loop; subscription_id is
		// passed into the search filter directly, emails are kept for the
		// downstream resolved-emails AND-combine + hit labelling.
		accs, err := u.accounts.ListAccountsBySubscriptionIDs(ctx, req.SubscriptionIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve subscription emails: %w", err)
		}
		seen := map[string]struct{}{}
		for _, a := range accs {
			if a == nil || a.Email == "" {
				continue
			}
			if _, dup := seen[a.Email]; dup {
				continue
			}
			seen[a.Email] = struct{}{}
			scopedEmails = append(scopedEmails, a.Email)
		}
		if len(scopedEmails) == 0 {
			return emptyResp(), nil
		}
	}
	if len(req.Emails) > 0 {
		if len(scopedEmails) == 0 {
			scopedEmails = append(scopedEmails, req.Emails...)
		} else {
			want := map[string]struct{}{}
			for _, e := range req.Emails {
				want[e] = struct{}{}
			}
			filtered := scopedEmails[:0]
			for _, e := range scopedEmails {
				if _, ok := want[e]; ok {
					filtered = append(filtered, e)
				}
			}
			scopedEmails = filtered
			if len(scopedEmails) == 0 {
				return emptyResp(), nil
			}
		}
	}

	hits, truncated, err := u.logReader.SearchAccessLog(ctx, nodeRepo.AccessLogSearchFilter{
		// Emails empty → repository scans every email; non-empty narrows scope.
		// SubscriptionIDs narrows via the denormalised column when set.
		NodeIDs:         req.NodeIDs,
		Emails:          scopedEmails,
		SubscriptionIDs: scopedSubs,
		From:            req.From,
		To:              req.To,
		Query:           q,
		Kinds:           kinds,
		Limit:           limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search access log: %w", err)
	}

	// Bulk resolve distinct hit emails → owning sub. Unmapped emails
	// (account hard-deleted, summary still in retention) → SubscriptionID=0
	// ("unattached" in UI).
	emailSet := make(map[string]struct{}, len(hits))
	for _, h := range hits {
		if h.Email != "" {
			emailSet[h.Email] = struct{}{}
		}
	}
	emails := make([]string, 0, len(emailSet))
	for e := range emailSet {
		emails = append(emails, e)
	}
	type subInfo struct {
		subID  uint
		userID uint
		label  string
	}
	resolved := map[string]subInfo{}
	if len(emails) > 0 {
		accs, err := u.accounts.ListAccountsByEmails(ctx, emails)
		if err != nil {
			return nil, fmt.Errorf("resolve emails: %w", err)
		}
		for _, a := range accs {
			if a == nil || a.Email == "" {
				continue
			}
			if _, dup := resolved[a.Email]; dup {
				// One email may live on N inbounds. Keep the first sub seen
				// — they all point at the same subscription in practice.
				continue
			}
			info := subInfo{}
			if a.SubscriptionID != nil {
				info.subID = *a.SubscriptionID
			}
			if a.Subscription != nil {
				// Label → @username → name → plan; empty only when nothing is
				// known, leaving the UI to show "Sub #<id>".
				info.label = a.Subscription.DisplayLabel()
				if a.Subscription.UserID != nil {
					info.userID = *a.Subscription.UserID
				}
			}
			resolved[a.Email] = info
		}
	}

	out := &GlobalSearchResponse{
		From:          req.From,
		To:            req.To,
		Query:         q,
		Kinds:         kinds,
		Hits:          make([]GlobalSearchHit, 0, len(hits)),
		Truncated:     truncated,
		RetentionDays: retentionDays,
		NodesQueried:  req.NodeIDs,
		LastSyncedAt:  u.fetchLastSyncedFromHits(ctx, req.NodeIDs, hits),
	}
	for _, h := range hits {
		info := resolved[h.Email]
		out.Hits = append(out.Hits, GlobalSearchHit{
			Bucket:            h.Bucket,
			NodeID:            h.NodeID,
			Email:             h.Email,
			SubscriptionID:    info.subID,
			UserID:            info.userID,
			SubscriptionLabel: info.label,
			Kind:              h.Kind,
			Value:             h.Value,
			Count:             h.Count,
		})
	}
	out.BySubscription = aggregateGlobalBySubscription(out.Hits)
	out.ByValue = aggregateGlobalByValue(out.Hits)
	return out, nil
}

// aggregateGlobalBySubscription folds hits into one row per
// (kind, value, subscription). Sorted: count desc, hours desc, value asc.
func aggregateGlobalBySubscription(hits []GlobalSearchHit) []GlobalSubAggregate {
	type key struct {
		kind  string
		value string
		sub   uint
	}
	totals := map[key]int64{}
	hourSets := map[key]map[time.Time]struct{}{}
	users := map[key]uint{}
	labels := map[key]string{}
	for _, h := range hits {
		k := key{h.Kind, h.Value, h.SubscriptionID}
		totals[k] += h.Count
		set, ok := hourSets[k]
		if !ok {
			set = map[time.Time]struct{}{}
			hourSets[k] = set
		}
		set[h.Bucket] = struct{}{}
		if _, ok := users[k]; !ok {
			users[k] = h.UserID
		}
		if _, ok := labels[k]; !ok {
			labels[k] = h.SubscriptionLabel
		}
	}
	out := make([]GlobalSubAggregate, 0, len(totals))
	for k, total := range totals {
		out = append(out, GlobalSubAggregate{
			Kind:              k.kind,
			Value:             k.value,
			SubscriptionID:    k.sub,
			UserID:            users[k],
			SubscriptionLabel: labels[k],
			Count:             total,
			Hours:             len(hourSets[k]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Hours != out[j].Hours {
			return out[i].Hours > out[j].Hours
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// aggregateGlobalByValue is the (kind, value) rollup across every
// subscription — the "popularity" signal.
func aggregateGlobalByValue(hits []GlobalSearchHit) []GlobalValueAggregate {
	type key struct {
		kind  string
		value string
	}
	totals := map[key]int64{}
	subSets := map[key]map[uint]struct{}{}
	hourSets := map[key]map[time.Time]struct{}{}
	for _, h := range hits {
		k := key{h.Kind, h.Value}
		totals[k] += h.Count
		ss, ok := subSets[k]
		if !ok {
			ss = map[uint]struct{}{}
			subSets[k] = ss
		}
		ss[h.SubscriptionID] = struct{}{}
		hs, ok := hourSets[k]
		if !ok {
			hs = map[time.Time]struct{}{}
			hourSets[k] = hs
		}
		hs[h.Bucket] = struct{}{}
	}
	out := make([]GlobalValueAggregate, 0, len(totals))
	for k, total := range totals {
		out = append(out, GlobalValueAggregate{
			Kind:          k.kind,
			Value:         k.value,
			Count:         total,
			Subscriptions: len(subSets[k]),
			Hours:         len(hourSets[k]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Subscriptions != out[j].Subscriptions {
			return out[i].Subscriptions > out[j].Subscriptions
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// fetchLastSynced returns the freshness map for nodeIDs. Empty input
// returns an empty map. Errors are swallowed — a lookup failure must
// never block the main response, just hide the pill.
func (u *accessHistoryUsecase) fetchLastSynced(ctx context.Context, nodeIDs []uint) map[uint]time.Time {
	if len(nodeIDs) == 0 {
		return map[uint]time.Time{}
	}
	m, err := u.logReader.GetNodesLastAccessLogSyncedAt(ctx, nodeIDs)
	if err != nil {
		return map[uint]time.Time{}
	}
	return m
}

// fetchLastSyncedFromHits prefers explicit nodeIDs when set, otherwise
// derives the set from the search hits. Same swallow-on-error policy.
func (u *accessHistoryUsecase) fetchLastSyncedFromHits(ctx context.Context, nodeIDs []uint, hits []nodeRepo.AccessLogSearchHit) map[uint]time.Time {
	if len(nodeIDs) > 0 {
		return u.fetchLastSynced(ctx, nodeIDs)
	}
	seen := map[uint]struct{}{}
	derived := make([]uint, 0, len(hits))
	for _, h := range hits {
		if h.NodeID == 0 {
			continue
		}
		if _, dup := seen[h.NodeID]; dup {
			continue
		}
		seen[h.NodeID] = struct{}{}
		derived = append(derived, h.NodeID)
	}
	return u.fetchLastSynced(ctx, derived)
}
