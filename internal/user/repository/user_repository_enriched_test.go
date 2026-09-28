package repository

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
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

func TestListForAdmin_PageScopedAggregatesAndGlobalSorting(t *testing.T) {
	db, repo := setupUserDB(t)
	for i, name := range []string{"user-one", "user-two", "user-three", "user-four"} {
		seedUser(t, repo, &domain.User{TelegramID: int64(i + 1), Username: name})
	}
	for _, statement := range []string{
		`CREATE TABLE subscriptions (id INTEGER PRIMARY KEY, user_id INTEGER, status TEXT, pause_reason TEXT DEFAULT '',
		 label TEXT DEFAULT '', config_email TEXT DEFAULT '', link_key TEXT DEFAULT '', config_id TEXT DEFAULT '',
		 is_end_date_custom BOOLEAN DEFAULT 0, custom_end_date DATETIME, end_date DATETIME,
		 custom_data_limit INTEGER, data_limit INTEGER DEFAULT 0, data_used INTEGER DEFAULT 0, lifetime_data_used INTEGER DEFAULT 0,
		 deleted_at DATETIME)`,
		`CREATE INDEX subscriptions_user_id ON subscriptions(user_id)`,
		`CREATE TABLE accounts (id INTEGER PRIMARY KEY, subscription_id INTEGER, last_activity_at DATETIME, deleted_at DATETIME)`,
		`CREATE INDEX accounts_subscription_id ON accounts(subscription_id)`,
		`INSERT INTO subscriptions (id, user_id, status, lifetime_data_used, deleted_at) VALUES
		 (1,1,'active',10,NULL), (2,1,'expired',5,NULL), (3,1,'active',0,'2026-01-01'),
		 (4,2,'active',1,NULL), (5,2,'active',1,NULL), (6,2,'expired',1,NULL),
		 (7,3,'cancelled',0,NULL), (8,NULL,'active',0,NULL)`,
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
		name, search, sort, order string
		group                     adminDomain.UserListGroup
		offset, limit             int
		wantIDs                   []uint
		wantTotal                 int64
	}{
		{name: "ordinary second page", sort: "id", order: "asc", offset: 1, limit: 2, wantIDs: []uint{2, 3}, wantTotal: 4},
		{name: "largest active count globally", sort: "active_subscriptions", limit: 1, wantIDs: []uint{2}, wantTotal: 4},
		{name: "second active count globally", sort: "active_subscriptions", offset: 1, limit: 1, wantIDs: []uint{1}, wantTotal: 4},
		{name: "second total count globally", sort: "total_subscriptions", offset: 1, limit: 1, wantIDs: []uint{1}, wantTotal: 4},
		{name: "activity globally with nulls last", sort: "last_active_at", order: "asc", offset: 1, limit: 1, wantIDs: []uint{2}, wantTotal: 4},
		{name: "active group", group: adminDomain.UserGroupActive, sort: "id", order: "asc", offset: 1, limit: 1, wantIDs: []uint{2}, wantTotal: 2},
		{name: "lapsed group", group: adminDomain.UserGroupLapsed, sort: "id", order: "asc", limit: 2, wantIDs: []uint{3}, wantTotal: 1},
		{name: "never subscribed group", group: adminDomain.UserGroupNever, sort: "id", order: "asc", limit: 2, wantIDs: []uint{4}, wantTotal: 1},
		{name: "search", search: "user-two", sort: "id", limit: 1, wantIDs: []uint{2}, wantTotal: 1},
		{name: "empty page", sort: "id", offset: 4, limit: 2, wantIDs: []uint{}, wantTotal: 4},
	}
	wantActive := map[uint]int{1: 1, 2: 2}
	wantCount := map[uint]int{1: 2, 2: 3, 3: 1}
	wantActivity := map[uint]string{1: "2026-02-01", 2: "2026-03-01"}
	wantData := map[uint]int64{1: 15, 2: 3}
	wantShown := map[uint]uint{1: 1, 3: 7}
	wantGroup := map[uint]adminDomain.UserListGroup{1: "active", 2: "active", 3: "lapsed", 4: "never"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture.queries = nil
			page, err := repo.ListForAdmin(context.Background(), adminDomain.UserListQuery{
				Search: tt.search, Group: tt.group, Sort: tt.sort, Order: tt.order, Offset: tt.offset, Limit: tt.limit,
			})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]uint, len(page.Items))
			for i, item := range page.Items {
				ids[i] = item.ID
				if item.ActiveSubscriptions != wantActive[item.ID] || item.TotalSubscriptions != wantCount[item.ID] {
					t.Errorf("incorrect enrichment: %+v", item)
				}
				if item.DataUsed != wantData[item.ID] || item.Group != wantGroup[item.ID] {
					t.Errorf("user %d data/group = %d/%q", item.ID, item.DataUsed, item.Group)
				}
				if want, ok := wantShown[item.ID]; ok && (item.Subscription == nil || item.Subscription.ID != want) {
					t.Errorf("user %d shows subscription %+v, want %d", item.ID, item.Subscription, want)
				}
				if date, ok := wantActivity[item.ID]; ok {
					if item.LastActiveAt == nil || !strings.HasPrefix(*item.LastActiveAt, date) {
						t.Errorf("user %d activity = %v, want %s", item.ID, item.LastActiveAt, date)
					}
				} else if item.LastActiveAt != nil {
					t.Errorf("user %d activity = %v, want nil", item.ID, item.LastActiveAt)
				}
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) || page.Total != tt.wantTotal {
				t.Errorf("IDs %v / total %d, want %v / %d", ids, page.Total, tt.wantIDs, tt.wantTotal)
			}
			if len(page.Items) == 0 {
				if len(capture.queries) != 2 {
					t.Errorf("empty page ran %d queries, want only counts and IDs", len(capture.queries))
				}
				return
			}
			if len(capture.queries) != 4 {
				t.Fatalf("ran %d queries, want counts, IDs, enrichment and subscriptions", len(capture.queries))
			}
			// Protect the performance contract: the page's aggregates are
			// scoped to its IDs, the global ordering is not.
			finalQuery := capture.queries[2]
			for _, scope := range []string{"AND s.user_id IN (", "AND sub.user_id IN ("} {
				if !strings.Contains(finalQuery, scope) {
					t.Errorf("aggregate inputs are not page-scoped (%s): %s", scope, finalQuery)
				}
			}
			idQuery := capture.queries[1]
			for _, scope := range []string{"AND s.user_id IN (", "AND sub.user_id IN ("} {
				if strings.Contains(idQuery, scope) {
					t.Errorf("global ordering was restricted to a page: %s", idQuery)
				}
			}
		})
	}
}
