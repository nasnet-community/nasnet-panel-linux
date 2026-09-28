// Package usecase builds the read models behind the admin user page: a
// per-subscription overview (online IPs, forecast, node accounts) and a
// customer timeline merged from subscriptions, notices and audit entries.
package usecase

import (
	"context"
	"errors"
	"sort"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	notifDomain "github.com/nasnet-community/nasnet-panel-linux/internal/notification/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/analytics"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
)

// ErrNotFound is returned when the user or subscription does not exist.
var ErrNotFound = errors.New("not found")

// Narrow views of the repositories this package reads.
type (
	UserReader interface {
		FindByID(ctx context.Context, id uint) (*userDomain.User, error)
	}
	SubscriptionReader interface {
		FindByID(ctx context.Context, id uint) (*subDomain.Subscription, error)
		ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]*subDomain.Subscription, error)
		ListDailyUsage(ctx context.Context, subID uint, from, to time.Time) ([]*subDomain.SubscriptionDailyUsage, error)
	}
	SubscriptionIPReader interface {
		GetSubscriptionActiveIPs(ctx context.Context, subscriptionID uint, since time.Time) ([]subDomain.SubscriptionIP, error)
	}
	AccountReader interface {
		ListByUserID(ctx context.Context, userID uint) ([]*accountDomain.Account, error)
	}
	NotificationReader interface {
		ListSentToUser(ctx context.Context, userID uint, limit int) ([]*notifDomain.NotificationLog, error)
	}
	AuditReader interface {
		List(ctx context.Context, filters auditDomain.AuditListFilters) ([]*auditDomain.AuditLog, int64, error)
	}
	HourlyReader interface {
		GetHourlyAggregates(ctx context.Context, filter nodeRepo.AccessLogSummaryFilter) ([]nodeRepo.HourlyAggregate, error)
	}
)

// Deps groups the readers. All are required.
type Deps struct {
	Users         UserReader
	Subscriptions SubscriptionReader
	SubIPs        SubscriptionIPReader
	Accounts      AccountReader
	Notifications NotificationReader
	Audit         AuditReader
	Hourly        HourlyReader
}

type Usecase interface {
	Overview(ctx context.Context, userID uint) (*Overview, error)
	Timeline(ctx context.Context, userID uint, q TimelineQuery) (*Timeline, error)
	SubscriptionUsagePattern(ctx context.Context, subID uint, days int) ([]HourlyPoint, error)
}

type usecase struct{ d Deps }

func New(d Deps) Usecase { return &usecase{d: d} }

const (
	// Caps keep a pathological account from turning a page load into a scan.
	maxSubscriptions = 100
	maxNotices       = 1000
	maxAuditRows     = 500
)

// viewActionList holds read-only audit actions (an admin looked at
// something). The timeline hides them unless asked.
var viewActionList = []string{
	string(auditDomain.AuditSubViewAccessHistory),
	string(auditDomain.AuditSubSearchAccessHistory),
	string(auditDomain.AuditAccessHistoryGlobalSearch),
}

// ---------------------------------------------------------------- overview

type Overview struct {
	Traffic30d   int64      `json:"traffic_30d"`
	OnlineIPs    int        `json:"online_ips"`
	IPs30d       int        `json:"ips_30d"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	LastSeenNode string     `json:"last_seen_node,omitempty"`
	// Ban is the latest ban audit entry while the user is banned.
	Ban           *ActorAt               `json:"ban,omitempty"`
	Subscriptions []SubscriptionOverview `json:"subscriptions"`
}

type ActorAt struct {
	At    time.Time `json:"at"`
	Actor string    `json:"actor,omitempty"`
}

type SubscriptionOverview struct {
	SubscriptionID uint     `json:"subscription_id"`
	OnlineIPs      []string `json:"online_ips"`
	IPs30d         int      `json:"ips_30d"`
	Traffic30d     int64    `json:"traffic_30d"`
	// Forecast is set for active subscriptions with a data limit.
	Forecast *analytics.ExhaustionPrediction `json:"forecast,omitempty"`
	// ExhaustedAt is when the last data-exhausted notice went out.
	ExhaustedAt *time.Time `json:"exhausted_at,omitempty"`
	// Paused is the latest pause audit entry while paused.
	Paused   *ActorAt      `json:"paused,omitempty"`
	Accounts []NodeAccount `json:"accounts"`
}

type NodeAccount struct {
	AccountID      uint       `json:"account_id"`
	Status         string     `json:"status"`
	Protocol       string     `json:"protocol,omitempty"`
	InboundTag     string     `json:"inbound_tag,omitempty"`
	NodeID         uint       `json:"node_id,omitempty"`
	NodeName       string     `json:"node_name,omitempty"`
	NodeCountry    string     `json:"node_country,omitempty"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
}

func (u *usecase) Overview(ctx context.Context, userID uint) (*Overview, error) {
	user, err := u.d.Users.FindByID(ctx, userID)
	if err != nil || user == nil {
		return nil, ErrNotFound
	}
	subs, err := u.d.Subscriptions.ListByUserID(ctx, userID, 0, maxSubscriptions)
	if err != nil {
		return nil, err
	}
	accounts, err := u.d.Accounts.ListByUserID(ctx, userID)
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
	pauses, err := u.latestAudit(ctx, "subscription", subIDs, string(auditDomain.AuditSubPause))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	// Today and the 29 days before it, matching the panel's 30-day chart.
	to := now.Truncate(24 * time.Hour)
	from30 := to.AddDate(0, 0, -29)
	online := cache.GetAllOnlineIPs()

	out := &Overview{Subscriptions: make([]SubscriptionOverview, 0, len(subs))}
	allOnline := map[string]bool{}
	allIPs := map[string]bool{}

	accBySub := map[uint][]*accountDomain.Account{}
	for _, a := range accounts {
		if a.SubscriptionID != nil {
			accBySub[*a.SubscriptionID] = append(accBySub[*a.SubscriptionID], a)
		}
		if a.LastActivityAt != nil && (out.LastSeenAt == nil || a.LastActivityAt.After(*out.LastSeenAt)) {
			t := *a.LastActivityAt
			out.LastSeenAt = &t
			out.LastSeenNode = ""
			if a.Inbound != nil && a.Inbound.Node != nil {
				out.LastSeenNode = a.Inbound.Node.Name
			}
		}
	}

	exhaustedAt := map[uint]time.Time{}
	for _, n := range notices {
		// Renewals soft-delete (re-arm) notices, so a live row is this cycle's.
		if n.Type != notifDomain.NotificationTypeDataExhausted || n.DeletedAt.Valid {
			continue
		}
		if t, ok := exhaustedAt[n.SubscriptionID]; !ok || n.SentAt.After(t) {
			exhaustedAt[n.SubscriptionID] = n.SentAt
		}
	}

	for _, s := range subs {
		so := SubscriptionOverview{
			SubscriptionID: s.ID,
			OnlineIPs:      []string{},
			Accounts:       []NodeAccount{},
		}

		// Online IPs: the cache is keyed by account email; a subscription's
		// accounts normally share its config email, but check both.
		emails := map[string]bool{}
		if s.ConfigEmail != "" {
			emails[s.ConfigEmail] = true
		}
		for _, a := range accBySub[s.ID] {
			if a.Email != "" {
				emails[a.Email] = true
			}
		}
		seen := map[string]bool{}
		for e := range emails {
			for ip := range online[e] {
				if !seen[ip] {
					seen[ip] = true
					so.OnlineIPs = append(so.OnlineIPs, ip)
					allOnline[ip] = true
				}
			}
		}
		sort.Strings(so.OnlineIPs)

		ips, err := u.d.SubIPs.GetSubscriptionActiveIPs(ctx, s.ID, now.AddDate(0, 0, -30))
		if err != nil {
			return nil, err
		}
		distinct := map[string]bool{}
		for _, ip := range ips {
			distinct[ip.IP] = true
			allIPs[ip.IP] = true
		}
		so.IPs30d = len(distinct)

		records, err := u.d.Subscriptions.ListDailyUsage(ctx, s.ID, from30, to)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			if r.DataUsed > 0 {
				so.Traffic30d += r.DataUsed
			}
		}
		out.Traffic30d += so.Traffic30d

		if s.Status == subDomain.SubscriptionStatusActive && s.GetEffectiveDataLimit() > 0 {
			so.Forecast = analytics.ComputeExhaustion(s, records)
		}
		if t, ok := exhaustedAt[s.ID]; ok && s.Status == subDomain.SubscriptionStatusTrafficExhausted {
			so.ExhaustedAt = &t
		}
		if p, ok := pauses[s.ID]; ok && s.Status == subDomain.SubscriptionStatusPaused {
			so.Paused = p
		}

		for _, a := range accBySub[s.ID] {
			na := NodeAccount{AccountID: a.ID, Status: string(a.Status), LastActivityAt: a.LastActivityAt}
			if a.Inbound != nil {
				na.Protocol = a.Inbound.Protocol
				na.InboundTag = a.Inbound.Tag
				if a.Inbound.Node != nil {
					na.NodeID = a.Inbound.Node.ID
					na.NodeName = a.Inbound.Node.Name
					na.NodeCountry = a.Inbound.Node.CountryCode
				}
			}
			so.Accounts = append(so.Accounts, na)
		}
		sort.Slice(so.Accounts, func(i, j int) bool {
			if so.Accounts[i].NodeName != so.Accounts[j].NodeName {
				return so.Accounts[i].NodeName < so.Accounts[j].NodeName
			}
			return so.Accounts[i].AccountID < so.Accounts[j].AccountID
		})

		out.Subscriptions = append(out.Subscriptions, so)
	}
	out.OnlineIPs = len(allOnline)
	out.IPs30d = len(allIPs)

	if user.IsBanned {
		bans, err := u.latestAudit(ctx, "user", []uint{userID}, string(auditDomain.AuditUserBan))
		if err != nil {
			return nil, err
		}
		out.Ban = bans[userID]
	}
	return out, nil
}

// latestAudit returns the newest audit entry of action per entity.
func (u *usecase) latestAudit(ctx context.Context, entityType string, ids []uint, action string) (map[uint]*ActorAt, error) {
	out := map[uint]*ActorAt{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, _, err := u.d.Audit.List(ctx, auditDomain.AuditListFilters{
		Action: action, EntityType: entityType, EntityIDs: ids, Limit: maxAuditRows,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows { // newest first
		if _, ok := out[r.EntityID]; !ok {
			out[r.EntityID] = &ActorAt{At: r.CreatedAt, Actor: r.ActorName}
		}
	}
	return out, nil
}

// HourlyPoint is one hour of day (UTC) and its connection count.
type HourlyPoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

// SubscriptionUsagePattern returns connections per UTC hour of day for one
// subscription over the last days (max 30, the access-log retention).
func (u *usecase) SubscriptionUsagePattern(ctx context.Context, subID uint, days int) ([]HourlyPoint, error) {
	sub, err := u.d.Subscriptions.FindByID(ctx, subID)
	if err != nil || sub == nil {
		return nil, ErrNotFound
	}
	if days <= 0 || days > 30 {
		days = 30
	}
	points := make([]HourlyPoint, 24)
	for h := range points {
		points[h].Hour = h
	}
	if sub.ConfigEmail == "" {
		return points, nil
	}
	to := time.Now()
	aggs, err := u.d.Hourly.GetHourlyAggregates(ctx, nodeRepo.AccessLogSummaryFilter{
		Email: sub.ConfigEmail, From: to.AddDate(0, 0, -days), To: to,
	})
	if err != nil {
		return nil, err
	}
	for _, a := range aggs {
		if a.Hour >= 0 && a.Hour < 24 {
			points[a.Hour].Count += a.Accepted + a.Rejected
		}
	}
	return points, nil
}
