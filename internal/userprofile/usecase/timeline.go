package usecase

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
)

// Event kinds, matching the Activity tab's filter chips.
const (
	KindService = "service"
	KindNotice  = "notice"
	KindAdmin   = "admin"
)

type TimelineQuery struct {
	Kind         string // "" = all
	IncludeViews bool   // include admin read-only audit entries
	Before       string // cursor from a previous page's NextCursor; "" = newest
	Limit        int
	Location     *time.Location // reserved for day grouping; nil = UTC
}

type Timeline struct {
	Events []Event `json:"events"`
	// Counts per kind plus "all" and "views" (hidden admin views), over the
	// whole history rather than the current page.
	Counts     map[string]int `json:"counts"`
	Total      int            `json:"total"`
	NextCursor *string        `json:"next_cursor,omitempty"`
}

// Event is one timeline row. Type says what happened; the other fields are
// filled when they apply.
type Event struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Type           string    `json:"type"`
	At             time.Time `json:"at"`
	SubscriptionID *uint     `json:"subscription_id,omitempty"`

	Status      string `json:"status,omitempty"`
	Description string `json:"description,omitempty"`

	Actor     string `json:"actor,omitempty"`
	OldValues string `json:"old_values,omitempty"`
	NewValues string `json:"new_values,omitempty"`
}

const (
	defaultTimelineLimit = 100
	maxTimelineLimit     = 500
)

func (u *usecase) Timeline(ctx context.Context, userID uint, q TimelineQuery) (*Timeline, error) {
	user, err := u.d.Users.FindByID(ctx, userID)
	if err != nil || user == nil {
		return nil, ErrNotFound
	}
	subs, err := u.d.Subscriptions.ListByUserID(ctx, userID, 0, maxSubscriptions)
	if err != nil {
		return nil, err
	}
	notices, err := u.d.Notifications.ListSentToUser(ctx, userID, maxNotices)
	if err != nil {
		return nil, err
	}

	subIDs := make([]uint, 0, len(subs))
	for _, s := range subs {
		subIDs = append(subIDs, s.ID)
	}

	audits, views, err := u.auditFor(ctx, userID, subIDs, q.IncludeViews)
	if err != nil {
		return nil, err
	}

	events := make([]Event, 0, 1+2*len(subs)+len(notices)+len(audits))
	events = append(events, Event{ID: "joined", Kind: KindService, Type: "user.joined", At: user.CreatedAt})

	for _, s := range subs {
		id := s.ID
		events = append(events, Event{
			ID: fmt.Sprintf("sub-created-%d", s.ID), Kind: KindService, Type: "subscription.created",
			At: s.CreatedAt, SubscriptionID: &id, Description: subscriptionName(s),
		})
		if s.Status == subDomain.SubscriptionStatusExpired || s.Status == subDomain.SubscriptionStatusCancelled {
			events = append(events, Event{
				ID: fmt.Sprintf("sub-ended-%d", s.ID), Kind: KindService, Type: "subscription.ended",
				At: endedAt(s), SubscriptionID: &id, Status: string(s.Status), Description: subscriptionName(s),
			})
		}
	}

	for _, n := range notices {
		e := Event{ID: fmt.Sprintf("notice-%d", n.ID), Kind: KindNotice, Type: "notice." + string(n.Type), At: n.SentAt}
		if n.SubscriptionID > 0 {
			id := n.SubscriptionID
			e.SubscriptionID = &id
		}
		events = append(events, e)
	}

	for _, a := range audits {
		e := Event{
			ID: fmt.Sprintf("audit-%d", a.ID), Kind: KindAdmin, Type: a.Action, At: a.CreatedAt,
			Actor: a.ActorName, OldValues: a.OldValues, NewValues: a.NewValues,
		}
		id := a.EntityID
		if a.EntityType == "subscription" {
			e.SubscriptionID = &id
		}
		events = append(events, e)
	}

	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].At.Equal(events[j].At) {
			return events[i].At.After(events[j].At)
		}
		return events[i].ID > events[j].ID
	})

	counts := map[string]int{"all": len(events), "views": views,
		KindService: 0, KindNotice: 0, KindAdmin: 0}
	filtered := events
	if q.Kind != "" {
		filtered = filtered[:0:0]
	}
	for _, e := range events {
		counts[e.Kind]++
		if q.Kind != "" && e.Kind == q.Kind {
			filtered = append(filtered, e)
		}
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultTimelineLimit
	}
	if limit > maxTimelineLimit {
		limit = maxTimelineLimit
	}
	// Cursor paging: new events land above the cursor, so later pages
	// don't shift the way offsets would.
	start := 0
	if at, id, ok := parseCursor(q.Before); ok {
		start = sort.Search(len(filtered), func(i int) bool {
			n := filtered[i].At.UnixNano()
			return n < at || (n == at && filtered[i].ID < id)
		})
	}
	out := &Timeline{Counts: counts, Total: len(filtered), Events: []Event{}}
	if start < len(filtered) {
		end := min(start+limit, len(filtered))
		out.Events = filtered[start:end]
		if end < len(filtered) {
			last := filtered[end-1]
			c := fmt.Sprintf("%d|%s", last.At.UnixNano(), last.ID)
			out.NextCursor = &c
		}
	}
	return out, nil
}

// parseCursor reads "<unix nanos>|<event id>".
func parseCursor(c string) (int64, string, bool) {
	at, id, found := strings.Cut(c, "|")
	if !found {
		return 0, "", false
	}
	n, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return n, id, true
}

// auditFor collects audit entries about the user and their subscriptions,
// newest first, without duplicates. Read-only views are
// left out in SQL unless asked for (so they can't crowd out real actions
// under the row cap) and counted either way.
func (u *usecase) auditFor(ctx context.Context, userID uint, subIDs []uint, includeViews bool) ([]*auditDomain.AuditLog, int, error) {
	type ref struct {
		entity string
		ids    []uint
	}
	refs := []ref{{"user", []uint{userID}}, {"subscription", subIDs}}
	seen := map[uint]bool{}
	var out []*auditDomain.AuditLog
	views := 0
	for _, r := range refs {
		if len(r.ids) == 0 {
			continue
		}
		f := auditDomain.AuditListFilters{EntityType: r.entity, EntityIDs: r.ids, Limit: maxAuditRows}
		if !includeViews {
			f.ExcludeActions = viewActionList
		}
		rows, _, err := u.d.Audit.List(ctx, f)
		if err != nil {
			return nil, 0, err
		}
		for _, row := range rows {
			if !seen[row.ID] {
				seen[row.ID] = true
				out = append(out, row)
			}
		}
		_, n, err := u.d.Audit.List(ctx, auditDomain.AuditListFilters{EntityType: r.entity, EntityIDs: r.ids, Actions: viewActionList, Limit: 1})
		if err != nil {
			return nil, 0, err
		}
		views += int(n)
	}
	return out, views, nil
}

func subscriptionName(s *subDomain.Subscription) string {
	if s.Label != "" {
		return s.Label
	}
	return fmt.Sprintf("Subscription #%d", s.ID)
}

// endedAt is the end date when it has passed, else the last status change.
func endedAt(s *subDomain.Subscription) time.Time {
	if ed := s.GetEffectiveEndDate(); ed != nil && ed.Before(time.Now()) {
		return *ed
	}
	return s.UpdatedAt
}
