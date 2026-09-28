package domain

import (
	"strings"
	"time"
)

// OnlineWindow is how recent account activity must be to count as online;
// the same rule the panel's Accounts page uses.
const OnlineWindow = 10 * time.Second

// UserListGroup is a lifecycle tab on the admin users list.
type UserListGroup string

const (
	UserGroupAll    UserListGroup = ""
	UserGroupActive UserListGroup = "active"
	UserGroupEnding UserListGroup = "ending"
	UserGroupLapsed UserListGroup = "lapsed"
	UserGroupNever  UserListGroup = "never"
	UserGroupBanned UserListGroup = "banned"
)

// UserListGroups are the groups a page counts, in tab order (All is implied).
var UserListGroups = []UserListGroup{UserGroupActive, UserGroupEnding, UserGroupLapsed, UserGroupNever, UserGroupBanned}

// ParseUserListGroup maps a query value to a group; anything unknown means all users.
func ParseUserListGroup(v string) UserListGroup {
	g := UserListGroup(strings.ToLower(strings.TrimSpace(v)))
	for _, known := range UserListGroups {
		if g == known {
			return g
		}
	}
	return UserGroupAll
}

// UserListQuery is everything the admin users list can ask for.
type UserListQuery struct {
	Search     string
	Group      UserListGroup
	Telegram   string // "", "linked", "unlinked"
	Seen       string // "", "online", "24h", "7d", "30d", "over30d", "never"
	JoinedFrom *time.Time
	JoinedTo   *time.Time // exclusive
	AdminOnly  bool
	IDs        []uint // only these users (export of a selection)
	Sort       string
	Order      string // "asc" or "desc"
	Offset     int
	Limit      int // 0 means no limit
	Now        time.Time
}

// UserGroupCounts is how many users each group holds under the same search and filters.
type UserGroupCounts struct {
	All    int64 `json:"all"`
	Active int64 `json:"active"`
	Ending int64 `json:"ending"`
	Lapsed int64 `json:"lapsed"`
	Never  int64 `json:"never"`
	Banned int64 `json:"banned"`
}

// Count returns the size of one group.
func (c UserGroupCounts) Count(g UserListGroup) int64 {
	switch g {
	case UserGroupActive:
		return c.Active
	case UserGroupEnding:
		return c.Ending
	case UserGroupLapsed:
		return c.Lapsed
	case UserGroupNever:
		return c.Never
	case UserGroupBanned:
		return c.Banned
	}
	return c.All
}

// UserListPage is one page of the admin users list.
type UserListPage struct {
	Items  []*UserListItem
	Total  int64
	Groups UserGroupCounts
}

// UserListSubscription summarises one subscription for a list row.
type UserListSubscription struct {
	ID              uint       `json:"id"`
	Status          string     `json:"status"`
	PauseReason     string     `json:"pause_reason,omitempty"`
	Label           string     `json:"label"`
	EndDate         *time.Time `json:"end_date"`
	DataUsed        int64      `json:"data_used"`
	DataLimit       int64      `json:"data_limit"`
	LinkKey         string     `json:"-"`
	SubscriptionURL string     `json:"subscription_url,omitempty"`
}

// UserListMatch names the subscription field a search hit when the user's own
// fields did not match.
type UserListMatch struct {
	Field          string `json:"field"` // "label", "email" or "link"
	Value          string `json:"value"`
	SubscriptionID uint   `json:"subscription_id"`
}
