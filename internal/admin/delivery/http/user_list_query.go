package http

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
)

// parseUserListQuery reads the users list parameters shared by the list and
// its CSV export. Unknown values mean "no filter".
func parseUserListQuery(c *gin.Context, now time.Time) adminDomain.UserListQuery {
	q := adminDomain.UserListQuery{
		Search:    strings.TrimSpace(c.Query("search")),
		Group:     adminDomain.ParseUserListGroup(c.Query("group")),
		Telegram:  oneOf(c.Query("telegram"), "linked", "unlinked"),
		Seen:      oneOf(c.Query("seen"), "online", "24h", "7d", "30d", "over30d", "never"),
		AdminOnly: c.Query("admin") == "1",
		Sort:      c.Query("sort"),
		Order:     c.Query("order"),
		Now:       now.UTC(),
	}
	q.JoinedFrom, q.JoinedTo = joinedRange(c.Query("joined"), c.Query("joined_from"), c.Query("joined_to"), q.Now)
	return q
}

func oneOf(v string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}

// joinedRange turns the Joined filter into [from, to) bounds. Custom dates are
// whole UTC days and "to" includes its own day.
func joinedRange(preset, from, to string, now time.Time) (*time.Time, *time.Time) {
	switch preset {
	case "7d", "30d", "90d":
		days, _ := strconv.Atoi(strings.TrimSuffix(preset, "d"))
		start := now.AddDate(0, 0, -days)
		return &start, nil
	case "custom":
		var start, end *time.Time
		if t, err := time.Parse("2006-01-02", from); err == nil {
			start = &t
		}
		if t, err := time.Parse("2006-01-02", to); err == nil {
			next := t.AddDate(0, 0, 1)
			end = &next
		}
		return start, end
	}
	return nil, nil
}

// parseIDs reads a comma-separated id list, at most 1000 entries.
func parseIDs(raw string) []uint {
	var ids []uint
	for _, part := range strings.Split(raw, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
		if err != nil || id == 0 {
			continue
		}
		ids = append(ids, uint(id))
		if len(ids) == 1000 {
			break
		}
	}
	return ids
}

// fillSubscriptionURLs adds the copyable link to every active subscription on
// the rows, the way the subscription endpoints build it.
func fillSubscriptionURLs(items []*adminDomain.UserListItem, baseURL string) {
	for _, item := range items {
		for _, s := range item.ActiveLinks {
			if s.LinkKey != "" {
				s.SubscriptionURL = baseURL + "/sub/" + s.LinkKey
			}
		}
		// The shown subscription is ActiveLinks[0] when it is active, so it already has its URL.
	}
}
