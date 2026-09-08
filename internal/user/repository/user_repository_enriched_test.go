package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"gorm.io/gorm/logger"
)

type enrichmentSQLLog struct {
	logger.Interface
	queries []string
}

func (l *enrichmentSQLLog) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	query, _ := fc()
	l.queries = append(l.queries, query)
}

func TestListAllEnriched_PageScopedAggregatesAndGlobalSorting(t *testing.T) {
	db, repo := setupUserDB(t)
	for i, name := range []string{"user-one", "user-two", "user-three", "user-four"} {
		seedUser(t, repo, &domain.User{TelegramID: int64(i + 1), Username: name})
	}
	for _, statement := range []string{
		`CREATE TABLE subscriptions (id INTEGER PRIMARY KEY, user_id INTEGER, status TEXT, deleted_at DATETIME)`,
		`CREATE INDEX subscriptions_user_id ON subscriptions(user_id)`,
		`CREATE TABLE accounts (id INTEGER PRIMARY KEY, subscription_id INTEGER, last_activity_at DATETIME, deleted_at DATETIME)`,
		`CREATE INDEX accounts_subscription_id ON accounts(subscription_id)`,
		`INSERT INTO subscriptions (id, user_id, status, deleted_at) VALUES
		 (1,1,'active',NULL), (2,1,'expired',NULL), (3,1,'active','2026-01-01'),
		 (4,2,'active',NULL), (5,2,'active',NULL), (6,2,'expired',NULL),
		 (7,3,'cancelled',NULL), (8,NULL,'active',NULL)`,
		`INSERT INTO accounts (subscription_id, last_activity_at, deleted_at) VALUES
		 (1,'2026-01-01 00:00:00',NULL), (2,'2026-02-01 00:00:00',NULL),
		 (3,'2026-12-01 00:00:00',NULL), (4,'2026-03-01 00:00:00',NULL),
		 (5,'2026-12-01 00:00:00','2026-01-01'), (7,NULL,NULL), (8,'2026-12-01 00:00:00',NULL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	capture := &enrichmentSQLLog{Interface: logger.Default}
	db.Logger = capture

	tests := []struct {
		name, search, filter, sort, order string
		offset, limit                     int
		wantIDs                           []uint
		wantTotal                         int64
	}{
		{name: "ordinary second page", sort: "id", order: "asc", offset: 1, limit: 2, wantIDs: []uint{2, 3}, wantTotal: 4},
		{name: "largest active count globally", sort: "active_subscriptions", limit: 1, wantIDs: []uint{2}, wantTotal: 4},
		{name: "second active count globally", sort: "active_subscriptions", offset: 1, limit: 1, wantIDs: []uint{1}, wantTotal: 4},
		{name: "second total count globally", sort: "total_subscriptions", offset: 1, limit: 1, wantIDs: []uint{1}, wantTotal: 4},
		{name: "activity globally with nulls last", sort: "last_active_at", order: "asc", offset: 1, limit: 1, wantIDs: []uint{2}, wantTotal: 4},
		{name: "active subscription filter", filter: "has_subscription", sort: "id", order: "asc", offset: 1, limit: 1, wantIDs: []uint{2}, wantTotal: 2},
		{name: "no active subscription filter", filter: "no_subscription", sort: "id", order: "asc", limit: 2, wantIDs: []uint{3, 4}, wantTotal: 2},
		{name: "search", search: "user-two", sort: "id", limit: 1, wantIDs: []uint{2}, wantTotal: 1},
		{name: "empty page", sort: "id", offset: 4, limit: 2, wantIDs: []uint{}, wantTotal: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture.queries = nil
			items, total, err := repo.ListAllEnriched(context.Background(), tt.search, tt.filter, tt.sort, tt.order, tt.offset, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]uint, len(items))
			for i, item := range items {
				ids[i] = item.ID
				wantActive := map[uint]int{1: 1, 2: 2}
				wantCount := map[uint]int{1: 2, 2: 3, 3: 1}
				wantActivity := map[uint]string{1: "2026-02-01", 2: "2026-03-01"}
				if item.ActiveSubscriptions != wantActive[item.ID] || item.TotalSubscriptions != wantCount[item.ID] {
					t.Errorf("incorrect enrichment: %+v", item)
				}
				if date, ok := wantActivity[item.ID]; ok {
					if item.LastActiveAt == nil || !strings.HasPrefix(*item.LastActiveAt, date) {
						t.Errorf("user %d activity = %v, want %s", item.ID, item.LastActiveAt, date)
					}
				} else if item.LastActiveAt != nil {
					t.Errorf("user %d activity = %v, want nil", item.ID, item.LastActiveAt)
				}
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) || total != tt.wantTotal {
				t.Errorf("IDs %v / total %d, want %v / %d", ids, total, tt.wantIDs, tt.wantTotal)
			}
			if len(items) == 0 {
				if len(capture.queries) != 2 {
					t.Errorf("empty page ran %d queries, want only count and IDs", len(capture.queries))
				}
				return
			}
			if len(capture.queries) != 3 {
				t.Fatalf("ran %d queries, want count, IDs, and enrichment", len(capture.queries))
			}
			// Protect the performance contract: filter both aggregate inputs,
			// not just the users returned after full-table GROUP BYs.
			finalQuery := capture.queries[2]
			if !strings.Contains(finalQuery, "AND user_id IN (") || !strings.Contains(finalQuery, "AND sub.user_id IN (") {
				t.Errorf("aggregate inputs are not page-scoped: %s", finalQuery)
			}
			idQuery := capture.queries[1]
			if strings.Contains(idQuery, "AND user_id IN (") || strings.Contains(idQuery, "AND sub.user_id IN (") {
				t.Errorf("global ordering was restricted to a page: %s", idQuery)
			}
		})
	}
}
