package repository

import (
	"context"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Tests run against an in-memory SQLite database. The repository SQL is
// dialect-aware (see pkg/database) so the truncation paths exercised here
// match the production SQLite branches.

func setupAccessLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.AccessLogSummary{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seed creates summaries with predictable counts so the assertions can stay
// readable. Times are quantised to the hour to match production rows.
func seed(t *testing.T, repo NodeRepository, rows []domain.AccessLogSummary) {
	t.Helper()
	for i := range rows {
		if err := repo.UpsertAccessLogSummary(context.Background(), &rows[i]); err != nil {
			t.Fatalf("upsert seed row %d: %v", i, err)
		}
	}
}

func TestAccessLogFilter_MultiEmail(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{NodeID: 1, Email: "alice@x", HourTime: base, AcceptedCount: 10},
		{NodeID: 1, Email: "bob@x", HourTime: base, AcceptedCount: 20},
		{NodeID: 1, Email: "eve@x", HourTime: base, AcceptedCount: 99},
	})

	got, _, err := repo.GetAccessLogSummaries(context.Background(), AccessLogSummaryFilter{
		Emails: []string{"alice@x", "bob@x"},
	})
	if err != nil {
		t.Fatalf("GetAccessLogSummaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	for _, r := range got {
		if r.Email == "eve@x" {
			t.Fatalf("eve@x leaked through Emails filter")
		}
	}
}

func TestAccessLogFilter_EmailsTakesPrecedenceOverEmail(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{NodeID: 1, Email: "alice@x", HourTime: base, AcceptedCount: 1},
		{NodeID: 1, Email: "bob@x", HourTime: base, AcceptedCount: 2},
	})

	got, _, err := repo.GetAccessLogSummaries(context.Background(), AccessLogSummaryFilter{
		Email:  "alice@x",
		Emails: []string{"bob@x"},
	})
	if err != nil {
		t.Fatalf("GetAccessLogSummaries: %v", err)
	}
	if len(got) != 1 || got[0].Email != "bob@x" {
		t.Fatalf("want bob@x only, got %+v", got)
	}
}

func TestGetAccessLogTimeSeries_HourGranularity(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	// Two rows at the same hour on different nodes — should fold into one bucket.
	// Plus one row in the next hour.
	seed(t, repo, []domain.AccessLogSummary{
		{NodeID: 1, Email: "u@x", HourTime: base, AcceptedCount: 5, RejectedCount: 1},
		{NodeID: 2, Email: "u@x", HourTime: base, AcceptedCount: 7, RejectedCount: 0},
		{NodeID: 1, Email: "u@x", HourTime: base.Add(time.Hour), AcceptedCount: 3, RejectedCount: 2},
	})

	buckets, err := repo.GetAccessLogTimeSeries(context.Background(), AccessLogSummaryFilter{
		Email: "u@x",
		From:  base.Add(-time.Hour),
		To:    base.Add(2 * time.Hour),
	}, "hour")
	if err != nil {
		t.Fatalf("GetAccessLogTimeSeries: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("want 2 buckets, got %d (%+v)", len(buckets), buckets)
	}
	if buckets[0].AcceptedCount != 12 || buckets[0].RejectedCount != 1 {
		t.Errorf("bucket 0: want accepted=12 rejected=1, got %+v", buckets[0])
	}
	if buckets[1].AcceptedCount != 3 || buckets[1].RejectedCount != 2 {
		t.Errorf("bucket 1: want accepted=3 rejected=2, got %+v", buckets[1])
	}
	// ASC ordering invariant.
	if !buckets[0].Bucket.Before(buckets[1].Bucket) {
		t.Errorf("buckets not ordered ASC: %v then %v", buckets[0].Bucket, buckets[1].Bucket)
	}
}

func TestGetAccessLogTimeSeries_DayGranularity(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	day1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 2, 3, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{NodeID: 1, Email: "u@x", HourTime: day1, AcceptedCount: 10},
		{NodeID: 1, Email: "u@x", HourTime: day1.Add(2 * time.Hour), AcceptedCount: 5},
		{NodeID: 1, Email: "u@x", HourTime: day2, AcceptedCount: 3},
	})

	buckets, err := repo.GetAccessLogTimeSeries(context.Background(), AccessLogSummaryFilter{
		Email: "u@x",
		From:  day1.Add(-time.Hour),
		To:    day2.Add(24 * time.Hour),
	}, "day")
	if err != nil {
		t.Fatalf("GetAccessLogTimeSeries: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("want 2 day buckets, got %d (%+v)", len(buckets), buckets)
	}
	if buckets[0].AcceptedCount != 15 {
		t.Errorf("day1 sum: want 15, got %d", buckets[0].AcceptedCount)
	}
	if buckets[1].AcceptedCount != 3 {
		t.Errorf("day2 sum: want 3, got %d", buckets[1].AcceptedCount)
	}
}

func TestGetAccessLogTimeSeries_RangeNarrowing(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{NodeID: 1, Email: "u@x", HourTime: base.Add(-2 * time.Hour), AcceptedCount: 100},
		{NodeID: 1, Email: "u@x", HourTime: base, AcceptedCount: 5},
		{NodeID: 1, Email: "u@x", HourTime: base.Add(2 * time.Hour), AcceptedCount: 200},
	})

	buckets, err := repo.GetAccessLogTimeSeries(context.Background(), AccessLogSummaryFilter{
		Email: "u@x",
		From:  base.Add(-30 * time.Minute),
		To:    base.Add(time.Hour),
	}, "hour")
	if err != nil {
		t.Fatalf("GetAccessLogTimeSeries: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("range filter must drop out-of-window rows: got %d buckets", len(buckets))
	}
	if buckets[0].AcceptedCount != 5 {
		t.Errorf("only the in-range row should contribute: got %d", buckets[0].AcceptedCount)
	}
}

func TestGetAccessLogTotals(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{NodeID: 1, Email: "u@x", HourTime: base, AcceptedCount: 10, RejectedCount: 2, TcpCount: 8, UdpCount: 4},
		{NodeID: 1, Email: "u@x", HourTime: base.Add(time.Hour), AcceptedCount: 1, RejectedCount: 0, TcpCount: 1, UdpCount: 0},
	})

	totals, err := repo.GetAccessLogTotals(context.Background(), AccessLogSummaryFilter{Email: "u@x"})
	if err != nil {
		t.Fatalf("GetAccessLogTotals: %v", err)
	}
	if totals.AcceptedCount != 11 || totals.RejectedCount != 2 || totals.TcpCount != 9 || totals.UdpCount != 4 {
		t.Errorf("unexpected totals: %+v", totals)
	}
	if totals.HourBuckets != 2 {
		t.Errorf("HourBuckets: want 2, got %d", totals.HourBuckets)
	}
}

func TestGetAccessLogTotals_EmptyWindow(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	totals, err := repo.GetAccessLogTotals(context.Background(), AccessLogSummaryFilter{
		Email: "ghost@x",
		From:  time.Now().Add(-time.Hour),
		To:    time.Now(),
	})
	if err != nil {
		t.Fatalf("GetAccessLogTotals: %v", err)
	}
	zero := AccessLogTotals{}
	if totals != zero {
		t.Errorf("empty window should yield zero totals, got %+v", totals)
	}
}

// ─── SearchAccessLog ────────────────────────────────────────────────

func TestSearchAccessLog_DomainMatch(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{
			NodeID: 1, Email: "u@x", HourTime: base,
			TopDomains:      `{"google.com":12,"github.com":3}`,
			RejectedDomains: `{"ads.google.com":4}`,
			SourceIPs:       `{"1.2.3.4":7}`,
		},
		{
			NodeID: 1, Email: "u@x", HourTime: base.Add(time.Hour),
			TopDomains: `{"google.com":2}`,
			SourceIPs:  `{"1.2.3.4":1}`,
		},
		{
			// No match for "google" anywhere → must be filtered out by SQL.
			NodeID: 2, Email: "u@x", HourTime: base,
			TopDomains: `{"example.com":50}`,
			SourceIPs:  `{"9.9.9.9":50}`,
		},
	})

	hits, truncated, err := repo.SearchAccessLog(context.Background(), AccessLogSearchFilter{
		Emails: []string{"u@x"},
		From:   base.Add(-time.Hour),
		To:     base.Add(2 * time.Hour),
		Query:  "google",
		// Empty Kinds = all three. Source IP must NOT spuriously match the
		// substring "google" so we only expect domain + rejected_domain hits.
	})
	if err != nil {
		t.Fatalf("SearchAccessLog: %v", err)
	}
	if truncated {
		t.Errorf("truncated should be false")
	}
	if len(hits) != 3 {
		t.Fatalf("want 3 hits (google.com×2 + ads.google.com×1), got %d (%+v)", len(hits), hits)
	}
	for _, h := range hits {
		if h.Kind == "source_ip" {
			t.Errorf("source_ip hit should not match query 'google': %+v", h)
		}
	}
}

func TestSearchAccessLog_IPMatch_KindFilter(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	seed(t, repo, []domain.AccessLogSummary{
		{
			NodeID: 1, Email: "u@x", HourTime: base,
			TopDomains: `{"10.0.0.0":5}`, // a fake "domain" containing the IP literal
			SourceIPs:  `{"10.0.0.5":3,"10.0.0.6":1}`,
		},
	})

	// Restrict to source_ip kind — domain matches must NOT come back even
	// though the substring exists in TopDomains.
	hits, _, err := repo.SearchAccessLog(context.Background(), AccessLogSearchFilter{
		Emails: []string{"u@x"},
		From:   base.Add(-time.Hour),
		To:     base.Add(time.Hour),
		Query:  "10.0.0",
		Kinds:  []string{"source_ip"},
	})
	if err != nil {
		t.Fatalf("SearchAccessLog: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 source_ip hits, got %d", len(hits))
	}
	for _, h := range hits {
		if h.Kind != "source_ip" {
			t.Errorf("non-source_ip leaked: %+v", h)
		}
	}
}

func TestSearchAccessLog_LimitTruncates(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	// Five rows × one matching domain each = 5 potential hits.
	for i := 0; i < 5; i++ {
		seed(t, repo, []domain.AccessLogSummary{{
			NodeID: 1, Email: "u@x", HourTime: base.Add(time.Duration(i) * time.Hour),
			TopDomains: `{"google.com":1}`,
		}})
	}

	hits, truncated, err := repo.SearchAccessLog(context.Background(), AccessLogSearchFilter{
		Emails: []string{"u@x"},
		From:   base.Add(-time.Hour),
		To:     base.Add(10 * time.Hour),
		Query:  "google",
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("SearchAccessLog: %v", err)
	}
	if !truncated {
		t.Errorf("expected truncated=true")
	}
	if len(hits) != 2 {
		t.Fatalf("limit not enforced: got %d hits", len(hits))
	}
}

func TestSearchAccessLog_EmptyQueryReturnsNothing(t *testing.T) {
	db := setupAccessLogTestDB(t)
	repo := NewNodeRepository(db)

	hits, truncated, err := repo.SearchAccessLog(context.Background(), AccessLogSearchFilter{
		Emails: []string{"u@x"},
		Query:  "  ",
	})
	if err != nil {
		t.Fatalf("SearchAccessLog: %v", err)
	}
	if len(hits) != 0 || truncated {
		t.Errorf("blank query must return zero hits, got %d (truncated=%v)", len(hits), truncated)
	}
}
