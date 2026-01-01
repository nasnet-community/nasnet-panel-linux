package metrics

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DefaultStatsProvider implements StatsProvider using direct GORM queries.
type DefaultStatsProvider struct {
	db *gorm.DB
}

// NewDefaultStatsProvider creates a DefaultStatsProvider.
func NewDefaultStatsProvider(db *gorm.DB) *DefaultStatsProvider {
	return &DefaultStatsProvider{db: db}
}

func (p *DefaultStatsProvider) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	var s DashboardStats

	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM users WHERE deleted_at IS NULL").Scan(&s.TotalUsers).Error; err != nil {
		return nil, fmt.Errorf("stats: total users: %w", err)
	}
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND is_banned = false").Scan(&s.ActiveUsers).Error; err != nil {
		return nil, fmt.Errorf("stats: active users: %w", err)
	}
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND is_banned = true").Scan(&s.BannedUsers).Error; err != nil {
		return nil, fmt.Errorf("stats: banned users: %w", err)
	}
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND is_admin = true").Scan(&s.AdminUsers).Error; err != nil {
		return nil, fmt.Errorf("stats: admin users: %w", err)
	}

	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM subscriptions WHERE deleted_at IS NULL").Scan(&s.TotalSubscriptions).Error; err != nil {
		return nil, fmt.Errorf("stats: total subscriptions: %w", err)
	}
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM subscriptions WHERE deleted_at IS NULL AND status = 'active'").Scan(&s.ActiveSubscriptions).Error; err != nil {
		return nil, fmt.Errorf("stats: active subscriptions: %w", err)
	}
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM subscriptions WHERE deleted_at IS NULL AND status = 'expired'").Scan(&s.ExpiredSubscriptions).Error; err != nil {
		return nil, fmt.Errorf("stats: expired subscriptions: %w", err)
	}

	return &s, nil
}

func (p *DefaultStatsProvider) GetNodeStatuses(ctx context.Context) (online, offline int64, err error) {
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM nodes WHERE deleted_at IS NULL AND is_active = true AND is_online = true").Scan(&online).Error; err != nil {
		return 0, 0, fmt.Errorf("stats: online nodes: %w", err)
	}
	if err := p.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM nodes WHERE deleted_at IS NULL AND is_active = true AND is_online = false").Scan(&offline).Error; err != nil {
		return 0, 0, fmt.Errorf("stats: offline nodes: %w", err)
	}
	return online, offline, nil
}

func (p *DefaultStatsProvider) GetProvisioningCounts(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	if err := p.db.WithContext(ctx).Raw("SELECT status, COUNT(*) as count FROM provisioning_tasks WHERE status IN ('pending','processing','failed') GROUP BY status").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("stats: provisioning counts: %w", err)
	}

	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Status] = r.Count
	}
	return counts, nil
}

func (p *DefaultStatsProvider) GetCertificateExpiries(ctx context.Context) (map[string]time.Time, error) {
	type row struct {
		Domain    string
		ExpiresAt time.Time
	}
	var rows []row
	if err := p.db.WithContext(ctx).Raw("SELECT domain, expires_at FROM snis WHERE deleted_at IS NULL AND expires_at IS NOT NULL").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("stats: certificate expiries: %w", err)
	}

	expiries := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		expiries[r.Domain] = r.ExpiresAt
	}
	return expiries, nil
}
