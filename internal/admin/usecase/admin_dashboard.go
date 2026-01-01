package usecase

import (
	"context"
	"time"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

func (u *adminUsecase) GetDashboardStats(ctx context.Context) (*adminDomain.DashboardStats, error) {
	log := logger.GetLogger()

	totalUsers, err := u.userRepo.CountAll(ctx)
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count users")
	}
	activeUsers, err := u.userRepo.CountActive(ctx)
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count active users")
	}
	bannedUsers, err := u.userRepo.CountBanned(ctx)
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count banned users")
	}
	adminUsers, err := u.userRepo.CountAdmins(ctx)
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count admins")
	}
	totalSubs, err := u.subRepo.CountAll(ctx)
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count subscriptions")
	}
	activeSubs, err := u.subRepo.CountByStatus(ctx, string(subDomain.SubscriptionStatusActive))
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count active subscriptions")
	}
	expiredSubs, err := u.subRepo.CountByStatus(ctx, string(subDomain.SubscriptionStatusExpired))
	if err != nil {
		log.WithError(err).Warn("Dashboard: failed to count expired subscriptions")
	}

	return &adminDomain.DashboardStats{
		TotalUsers:           totalUsers,
		ActiveUsers:          activeUsers,
		OnlineUsers:          cache.GetOnlineCount(),
		BannedUsers:          bannedUsers,
		AdminUsers:           adminUsers,
		TotalSubscriptions:   totalSubs,
		ActiveSubscriptions:  activeSubs,
		ExpiredSubscriptions: expiredSubs,
	}, nil
}

func (u *adminUsecase) GetXraySystemStats(ctx context.Context) (*adminDomain.XraySystemStats, error) {
	return &adminDomain.XraySystemStats{OnlineUsers: 0}, nil
}

func (u *adminUsecase) GetOnlineUsers(ctx context.Context) ([]string, error) {
	// Return online users from cache (populated by scheduler)
	return cache.GetOnlineUsers(), nil
}

func (u *adminUsecase) GetUserOnlineSessions(ctx context.Context, email string) (int64, error) {
	// Return online status from cache (populated by scheduler)
	return cache.GetUserOnlineCount(email), nil
}

func (u *adminUsecase) GetSubscriptionIPs(ctx context.Context, subID uint) ([]subDomain.SubscriptionIP, error) {
	if u.subIPRepo == nil {
		return nil, nil
	}
	return u.subIPRepo.GetSubscriptionIPs(ctx, subID)
}

func (u *adminUsecase) GetSubscriptionActiveIPs(ctx context.Context, subID uint) ([]subDomain.SubscriptionIP, error) {
	if u.subIPRepo == nil {
		return nil, nil
	}
	since := time.Now().Add(-60 * time.Second)
	return u.subIPRepo.GetSubscriptionActiveIPs(ctx, subID, since)
}

func (u *adminUsecase) GetOnlineUsersWithIPs(ctx context.Context) (map[string][]string, error) {
	allIPs := cache.GetAllOnlineIPs()
	result := make(map[string][]string, len(allIPs))
	for email, ips := range allIPs {
		ipList := make([]string, 0, len(ips))
		for ip := range ips {
			ipList = append(ipList, ip)
		}
		result[email] = ipList
	}
	return result, nil
}

// RecordOnlineUsersSnapshot writes one row capturing the current dedup'd
// global online-user count. Intended to be called from the scheduler at
// the end of each SyncNodeStats tick — the cache is warm at that point.
func (u *adminUsecase) RecordOnlineUsersSnapshot(ctx context.Context) error {
	snap := &adminDomain.OnlineUsersSnapshot{
		Count:     cache.GetOnlineCount(),
		CreatedAt: time.Now(),
	}
	return u.onlineUsersSnapshotRepo.Create(ctx, snap)
}

// GetOnlineUsersHistory returns snapshots newer than now - `minutes` in
// ascending CreatedAt order. `minutes` is clamped to [5, 1440].
func (u *adminUsecase) GetOnlineUsersHistory(ctx context.Context, minutes int) ([]*adminDomain.OnlineUsersSnapshot, error) {
	if minutes < 5 {
		minutes = 5
	}
	if minutes > 1440 {
		minutes = 1440
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)
	return u.onlineUsersSnapshotRepo.ListSince(ctx, since)
}
