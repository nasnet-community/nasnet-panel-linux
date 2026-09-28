package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

const (
	endingWindow = 7 * 24 * time.Hour
	endingRatio  = 0.8 // hasEndingSub spells the same threshold in SQL
)

// SQL pieces for the admin users list. The outer users table is always "u";
// every subquery brings its own alias.
const (
	subEffectiveEnd   = "(CASE WHEN s.is_end_date_custom THEN s.custom_end_date ELSE s.end_date END)"
	subEffectiveLimit = "COALESCE(s.custom_data_limit, s.data_limit)"

	hasActiveSub = "EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id = u.id AND s.deleted_at IS NULL AND s.status = 'active')"
	hasAnySub    = "EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id = u.id AND s.deleted_at IS NULL)"
	hasEndingSub = "EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id = u.id AND s.deleted_at IS NULL AND s.status = 'active' AND ((" +
		subEffectiveEnd + " IS NOT NULL AND " + subEffectiveEnd + " <= ?) OR (" +
		subEffectiveLimit + " > 0 AND s.data_used >= 0.8 * " + subEffectiveLimit + ")))"

	activityBase = "EXISTS (SELECT 1 FROM accounts a JOIN subscriptions sa ON sa.id = a.subscription_id AND sa.deleted_at IS NULL " +
		"WHERE sa.user_id = u.id AND a.deleted_at IS NULL AND a.last_activity_at IS NOT NULL"
	hasActivity      = activityBase + ")"
	hasActivitySince = activityBase + " AND a.last_activity_at >= ?)"
)

// groupCondition is the SQL rule for one group, with its bind arguments.
func groupCondition(g adminDomain.UserListGroup, now time.Time) (string, []any) {
	switch g {
	case adminDomain.UserGroupActive:
		return "u.is_banned = ? AND " + hasActiveSub, []any{false}
	case adminDomain.UserGroupEnding:
		return "u.is_banned = ? AND " + hasEndingSub, []any{false, now.Add(endingWindow)}
	case adminDomain.UserGroupLapsed:
		return "u.is_banned = ? AND " + hasAnySub + " AND NOT " + hasActiveSub, []any{false}
	case adminDomain.UserGroupNever:
		return "u.is_banned = ? AND u.telegram_id > 0 AND NOT " + hasAnySub, []any{false}
	case adminDomain.UserGroupBanned:
		return "u.is_banned = ?", []any{true}
	}
	return "", nil
}

// seenCondition is the Last seen filter.
func seenCondition(seen string, now time.Time) (string, []any) {
	day := 24 * time.Hour
	switch seen {
	case "online":
		return hasActivitySince, []any{now.Add(-adminDomain.OnlineWindow)}
	case "24h":
		return hasActivitySince, []any{now.Add(-day)}
	case "7d":
		return hasActivitySince, []any{now.Add(-7 * day)}
	case "30d":
		return hasActivitySince, []any{now.Add(-30 * day)}
	case "over30d":
		return hasActivity + " AND NOT " + hasActivitySince, []any{now.Add(-30 * day)}
	case "never":
		return "NOT " + hasActivity, nil
	}
	return "", nil
}

// subscriptionSearchKey reduces a pasted subscription URL (…/sub/<key>) or a
// config URI (vless://<uuid>@host…, vmess://<base64 json>) to the link key or
// config ID the subscriptions table stores. Anything else comes back as is.
func subscriptionSearchKey(term string) string {
	if i := strings.LastIndex(term, "/sub/"); i >= 0 {
		key := term[i+len("/sub/"):]
		if j := strings.IndexAny(key, "/?#"); j >= 0 {
			key = key[:j]
		}
		if key != "" {
			return key
		}
		return term
	}
	scheme, rest, ok := strings.Cut(term, "://")
	if !ok {
		return term
	}
	if strings.EqualFold(scheme, "vmess") {
		rest, _, _ = strings.Cut(rest, "#")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			raw, err := enc.DecodeString(rest)
			if err != nil {
				continue
			}
			var v struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &v) == nil && v.ID != "" {
				return v.ID
			}
		}
		return term
	}
	if user, _, found := strings.Cut(rest, "@"); found && user != "" {
		return user
	}
	return term
}

// normalizedSearch trims the term and drops a leading "@", which usernames never store.
func normalizedSearch(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "@")
}

// applyListFilters narrows a query on "users u" by search and the secondary
// filters. The group is applied separately so the counts can ignore it.
func applyListFilters(tx *gorm.DB, q adminDomain.UserListQuery) *gorm.DB {
	tx = tx.Where("u.deleted_at IS NULL")
	if term := normalizedSearch(q.Search); term != "" {
		pattern := "%" + term + "%"
		key := subscriptionSearchKey(term)
		tx = tx.Where(fmt.Sprintf(
			"(%s OR %s OR %s OR CAST(u.telegram_id AS TEXT) LIKE ? OR EXISTS (SELECT 1 FROM subscriptions ss WHERE ss.user_id = u.id AND ss.deleted_at IS NULL AND (%s OR %s OR ss.link_key = ? OR ss.config_id = ?)))",
			database.ILike("u.username", "?"), database.ILike("u.first_name", "?"), database.ILike("u.last_name", "?"),
			database.ILike("ss.label", "?"), database.ILike("ss.config_email", "?"),
		), pattern, pattern, pattern, pattern, pattern, pattern, key, key)
	}
	switch q.Telegram {
	case "linked":
		tx = tx.Where("u.telegram_id > 0")
	case "unlinked":
		tx = tx.Where("u.telegram_id <= 0")
	}
	if q.AdminOnly {
		tx = tx.Where("u.is_admin = ?", true)
	}
	if q.JoinedFrom != nil {
		tx = tx.Where("u.created_at >= ?", *q.JoinedFrom)
	}
	if q.JoinedTo != nil {
		tx = tx.Where("u.created_at < ?", *q.JoinedTo)
	}
	if cond, args := seenCondition(q.Seen, q.Now); cond != "" {
		tx = tx.Where(cond, args...)
	}
	if len(q.IDs) > 0 {
		tx = tx.Where("u.id IN ?", q.IDs)
	}
	return tx
}

// listOrder returns the ORDER BY for a sort key, with a stable id tiebreak,
// and which aggregate joins the ID query needs for it.
func listOrder(sortKey, order string) (clause string, needsSubs, needsActivity bool) {
	dir := "DESC"
	if strings.EqualFold(order, "asc") {
		dir = "ASC"
	}
	var expr string
	switch sortKey {
	case "username":
		expr = "u.username"
	case "created_at":
		expr = "u.created_at"
	case "active_subscriptions":
		expr, needsSubs = "COALESCE(sc.active_count, 0)", true
	case "total_subscriptions":
		expr, needsSubs = "COALESCE(sc.total_count, 0)", true
	case "data_used":
		expr, needsSubs = "COALESCE(sc.data_used, 0)", true
	case "last_active_at":
		expr, needsActivity = "ua.last_active_at", true
	case "time_left":
		return database.NullsLast("sc.next_end "+dir) + ", " + database.NullsLast("sc.max_ratio DESC") + ", u.id " + dir, true, false
	default:
		expr = "u.id"
	}
	return database.NullsLast(expr+" "+dir) + ", u.id " + dir, needsSubs, needsActivity
}

// subscriptionAggregates is one row per user; ids scopes it to a page (nil = everyone).
func (r *userRepository) subscriptionAggregates(ctx context.Context, ids []uint) *gorm.DB {
	q := r.db.WithContext(ctx).Table("subscriptions s").
		Select("s.user_id, COUNT(*) AS total_count, " +
			database.CountFilter("s.status = 'active'") + " AS active_count, " +
			"MIN(CASE WHEN s.status = 'active' THEN " + subEffectiveEnd + " END) AS next_end, " +
			"MAX(CASE WHEN s.status = 'active' AND " + subEffectiveLimit + " > 0 THEN " + database.CastFloat("s.data_used") + " / " + subEffectiveLimit + " END) AS max_ratio, " +
			"CAST(COALESCE(SUM(s.lifetime_data_used), 0) AS BIGINT) AS data_used").
		Where("s.deleted_at IS NULL AND s.user_id IS NOT NULL").
		Group("s.user_id")
	if len(ids) > 0 {
		q = q.Where("s.user_id IN ?", ids)
	}
	return q
}

// userActivity is each user's latest account activity.
func (r *userRepository) userActivity(ctx context.Context, ids []uint) *gorm.DB {
	q := r.db.WithContext(ctx).Table("accounts acc").
		Select("sub.user_id, MAX(acc.last_activity_at) AS last_active_at").
		Joins("JOIN subscriptions sub ON sub.id = acc.subscription_id AND sub.deleted_at IS NULL").
		Where("acc.deleted_at IS NULL AND sub.user_id IS NOT NULL").
		Group("sub.user_id")
	if len(ids) > 0 {
		q = q.Where("sub.user_id IN ?", ids)
	}
	return q
}

type adminSubRow struct {
	ID              uint       `gorm:"column:id"`
	UserID          uint       `gorm:"column:user_id"`
	Status          string     `gorm:"column:status"`
	PauseReason     string     `gorm:"column:pause_reason"`
	Label           string     `gorm:"column:label"`
	ConfigEmail     string     `gorm:"column:config_email"`
	LinkKey         string     `gorm:"column:link_key"`
	ConfigID        string     `gorm:"column:config_id"`
	IsEndDateCustom bool       `gorm:"column:is_end_date_custom"`
	CustomEndDate   *time.Time `gorm:"column:custom_end_date"`
	EndDate         *time.Time `gorm:"column:end_date"`
	CustomDataLimit *int64     `gorm:"column:custom_data_limit"`
	DataLimit       int64      `gorm:"column:data_limit"`
	DataUsed        int64      `gorm:"column:data_used"`
}

// summary mirrors GetEffectiveEndDate and GetEffectiveDataLimit.
func (s adminSubRow) summary() *adminDomain.UserListSubscription {
	end := s.EndDate
	if s.IsEndDateCustom {
		end = s.CustomEndDate
	}
	limit := s.DataLimit
	if s.CustomDataLimit != nil {
		limit = *s.CustomDataLimit
	}
	return &adminDomain.UserListSubscription{
		ID: s.ID, Status: s.Status, PauseReason: s.PauseReason, Label: s.Label,
		EndDate: end, DataUsed: s.DataUsed, DataLimit: limit, LinkKey: s.LinkKey,
	}
}

func usedRatio(s *adminDomain.UserListSubscription) float64 {
	if s.DataLimit <= 0 {
		return 0
	}
	return float64(s.DataUsed) / float64(s.DataLimit)
}

// pickSubscriptions orders the active subscriptions by urgency (soonest end
// first, no end last, then most data used) and picks the one the row shows:
// the most urgent active one, else the most recently ended other one.
func pickSubscriptions(subs []*adminDomain.UserListSubscription) (shown *adminDomain.UserListSubscription, active []*adminDomain.UserListSubscription) {
	var others []*adminDomain.UserListSubscription
	for _, s := range subs {
		if s.Status == "active" {
			active = append(active, s)
		} else {
			others = append(others, s)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		a, b := active[i], active[j]
		if (a.EndDate == nil) != (b.EndDate == nil) {
			return a.EndDate != nil
		}
		if a.EndDate != nil && !a.EndDate.Equal(*b.EndDate) {
			return a.EndDate.Before(*b.EndDate)
		}
		return usedRatio(a) > usedRatio(b)
	})
	if len(active) > 0 {
		return active[0], active
	}
	sort.SliceStable(others, func(i, j int) bool {
		a, b := others[i], others[j]
		if (a.EndDate == nil) != (b.EndDate == nil) {
			return a.EndDate != nil
		}
		if a.EndDate != nil && !a.EndDate.Equal(*b.EndDate) {
			return a.EndDate.After(*b.EndDate)
		}
		return a.ID > b.ID
	})
	if len(others) > 0 {
		return others[0], nil
	}
	return nil, nil
}

// rowGroup is the user's most specific group, with the same rules as groupCondition.
func rowGroup(item *adminDomain.UserListItem, active []*adminDomain.UserListSubscription, now time.Time) adminDomain.UserListGroup {
	switch {
	case item.IsBanned:
		return adminDomain.UserGroupBanned
	case len(active) > 0:
		for _, s := range active {
			if (s.EndDate != nil && !s.EndDate.After(now.Add(endingWindow))) || (s.DataLimit > 0 && usedRatio(s) >= endingRatio) {
				return adminDomain.UserGroupEnding
			}
		}
		return adminDomain.UserGroupActive
	case item.TotalSubscriptions > 0:
		return adminDomain.UserGroupLapsed
	case item.TelegramID > 0:
		return adminDomain.UserGroupNever
	}
	return adminDomain.UserGroupAll
}

// searchMatch says which subscription the search hit when the user's own fields didn't.
func searchMatch(item *adminDomain.UserListItem, subs []adminSubRow, search string) *adminDomain.UserListMatch {
	raw := normalizedSearch(search)
	term := strings.ToLower(raw)
	if term == "" {
		return nil
	}
	for _, own := range []string{item.Username, item.FirstName, item.LastName, strconv.FormatInt(item.TelegramID, 10)} {
		if strings.Contains(strings.ToLower(own), term) {
			return nil
		}
	}
	key := subscriptionSearchKey(raw)
	for _, s := range subs {
		if (s.LinkKey != "" && s.LinkKey == key) || (s.ConfigID != "" && s.ConfigID == key) {
			return &adminDomain.UserListMatch{Field: "link", Value: key, SubscriptionID: s.ID}
		}
	}
	for _, s := range subs {
		if strings.Contains(strings.ToLower(s.Label), term) {
			return &adminDomain.UserListMatch{Field: "label", Value: s.Label, SubscriptionID: s.ID}
		}
		if strings.Contains(strings.ToLower(s.ConfigEmail), term) {
			return &adminDomain.UserListMatch{Field: "email", Value: s.ConfigEmail, SubscriptionID: s.ID}
		}
	}
	return nil
}

// ListForAdmin returns one page of the admin users list plus every group's
// size under the same search and filters. Aggregates run for the page's IDs
// only, except where the sort itself needs them.
func (r *userRepository) ListForAdmin(ctx context.Context, q adminDomain.UserListQuery) (*adminDomain.UserListPage, error) {
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	// SQLite compares times as text and the panel writes them in local time,
	// so every bound goes in local time too. The instants stay the same.
	q.Now = q.Now.Local()
	if q.JoinedFrom != nil {
		from := q.JoinedFrom.Local()
		q.JoinedFrom = &from
	}
	if q.JoinedTo != nil {
		to := q.JoinedTo.Local()
		q.JoinedTo = &to
	}
	page := &adminDomain.UserListPage{Items: []*adminDomain.UserListItem{}}

	// Step 1: every group's size.
	selects := []string{"COUNT(*) AS all_users"}
	var selectArgs []any
	for _, g := range adminDomain.UserListGroups {
		cond, args := groupCondition(g, q.Now)
		selects = append(selects, fmt.Sprintf("COALESCE(SUM(CASE WHEN %s THEN 1 ELSE 0 END), 0) AS %s_users", cond, g))
		selectArgs = append(selectArgs, args...)
	}
	var counts struct {
		AllUsers    int64 `gorm:"column:all_users"`
		ActiveUsers int64 `gorm:"column:active_users"`
		EndingUsers int64 `gorm:"column:ending_users"`
		LapsedUsers int64 `gorm:"column:lapsed_users"`
		NeverUsers  int64 `gorm:"column:never_users"`
		BannedUsers int64 `gorm:"column:banned_users"`
	}
	countQuery := applyListFilters(r.db.WithContext(ctx).Table("users u").Select(strings.Join(selects, ", "), selectArgs...), q)
	if err := countQuery.Scan(&counts).Error; err != nil {
		return nil, err
	}
	page.Groups = adminDomain.UserGroupCounts{
		All: counts.AllUsers, Active: counts.ActiveUsers, Ending: counts.EndingUsers,
		Lapsed: counts.LapsedUsers, Never: counts.NeverUsers, Banned: counts.BannedUsers,
	}
	page.Total = page.Groups.Count(q.Group)

	// Step 2: the page's IDs in sort order. Aggregates here span every user
	// on purpose: the order is global, only the page is small.
	order, needsSubs, needsActivity := listOrder(q.Sort, q.Order)
	idQuery := r.db.WithContext(ctx).Table("users u").Select("u.id")
	if needsSubs {
		idQuery = idQuery.Joins("LEFT JOIN (?) sc ON sc.user_id = u.id", r.subscriptionAggregates(ctx, nil))
	}
	if needsActivity {
		idQuery = idQuery.Joins("LEFT JOIN (?) ua ON ua.user_id = u.id", r.userActivity(ctx, nil))
	}
	idQuery = applyListFilters(idQuery, q)
	if cond, args := groupCondition(q.Group, q.Now); cond != "" {
		idQuery = idQuery.Where(cond, args...)
	}
	idQuery = idQuery.Order(order).Offset(q.Offset)
	if q.Limit > 0 {
		idQuery = idQuery.Limit(q.Limit)
	}
	var ids []uint
	if err := idQuery.Pluck("u.id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return page, nil
	}

	// Step 3: enrich only the page.
	type enrichedRow struct {
		ID                  uint    `gorm:"column:id"`
		TelegramID          int64   `gorm:"column:telegram_id"`
		Username            string  `gorm:"column:username"`
		FirstName           string  `gorm:"column:first_name"`
		LastName            string  `gorm:"column:last_name"`
		IsAdmin             bool    `gorm:"column:is_admin"`
		IsBanned            bool    `gorm:"column:is_banned"`
		ActiveSubscriptions int     `gorm:"column:active_subscriptions"`
		TotalSubscriptions  int     `gorm:"column:total_subscriptions"`
		DataUsed            int64   `gorm:"column:data_used"`
		Online              bool    `gorm:"column:online"`
		LastActiveAt        *string `gorm:"column:last_active_at"`
		CreatedAt           string  `gorm:"column:created_at"`
		UpdatedAt           string  `gorm:"column:updated_at"`
	}
	var rows []enrichedRow
	if err := r.db.WithContext(ctx).Table("users u").
		Select(`u.id, u.telegram_id, u.username, u.first_name, u.last_name,
			u.is_admin, u.is_banned, u.created_at, u.updated_at,
			COALESCE(sc.active_count, 0) AS active_subscriptions,
			COALESCE(sc.total_count, 0) AS total_subscriptions,
			COALESCE(sc.data_used, 0) AS data_used,
			ua.last_active_at,
			CASE WHEN ua.last_active_at >= ? THEN 1 ELSE 0 END AS online`, q.Now.Add(-adminDomain.OnlineWindow)).
		Joins("LEFT JOIN (?) sc ON sc.user_id = u.id", r.subscriptionAggregates(ctx, ids)).
		Joins("LEFT JOIN (?) ua ON ua.user_id = u.id", r.userActivity(ctx, ids)).
		Where("u.id IN ? AND u.deleted_at IS NULL", ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	var subs []adminSubRow
	if err := r.db.WithContext(ctx).Table("subscriptions s").
		Select(`s.id, s.user_id, s.status, COALESCE(s.pause_reason, '') AS pause_reason, COALESCE(s.label, '') AS label,
			COALESCE(s.config_email, '') AS config_email, COALESCE(s.link_key, '') AS link_key, COALESCE(s.config_id, '') AS config_id,
			COALESCE(s.is_end_date_custom, FALSE) AS is_end_date_custom, s.custom_end_date, s.end_date, s.custom_data_limit,
			COALESCE(s.data_limit, 0) AS data_limit, COALESCE(s.data_used, 0) AS data_used`).
		Where("s.deleted_at IS NULL AND s.user_id IN ?", ids).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	subsByUser := make(map[uint][]adminSubRow, len(ids))
	for _, s := range subs {
		subsByUser[s.UserID] = append(subsByUser[s.UserID], s)
	}

	rowByID := make(map[uint]enrichedRow, len(rows))
	for _, row := range rows {
		rowByID[row.ID] = row
	}
	for _, id := range ids {
		row, ok := rowByID[id]
		if !ok {
			continue // deleted between the two queries
		}
		item := &adminDomain.UserListItem{
			ID: row.ID, TelegramID: row.TelegramID, Username: row.Username, FirstName: row.FirstName, LastName: row.LastName,
			IsAdmin: row.IsAdmin, IsBanned: row.IsBanned,
			ActiveSubscriptions: row.ActiveSubscriptions, TotalSubscriptions: row.TotalSubscriptions,
			LastActiveAt: row.LastActiveAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			DataUsed: row.DataUsed, Online: row.Online,
		}
		summaries := make([]*adminDomain.UserListSubscription, 0, len(subsByUser[id]))
		for _, s := range subsByUser[id] {
			summaries = append(summaries, s.summary())
		}
		item.Subscription, item.ActiveLinks = pickSubscriptions(summaries)
		item.Group = rowGroup(item, item.ActiveLinks, q.Now)
		item.Match = searchMatch(item, subsByUser[id], q.Search)
		page.Items = append(page.Items, item)
	}
	return page, nil
}
