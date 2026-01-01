package metrics

import (
	"context"
	"database/sql"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// DashboardStats mirrors the fields needed from the admin dashboard.
type DashboardStats struct {
	TotalUsers           int64
	ActiveUsers          int64
	BannedUsers          int64
	AdminUsers           int64
	TotalSubscriptions   int64
	ActiveSubscriptions  int64
	ExpiredSubscriptions int64
}

// StatsProvider abstracts the queries needed by the metrics collector.
type StatsProvider interface {
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	GetNodeStatuses(ctx context.Context) (online, offline int64, err error)
	GetProvisioningCounts(ctx context.Context) (map[string]int64, error)
	GetCertificateExpiries(ctx context.Context) (map[string]time.Time, error)
}

// SettingReader reads a setting value by key (subset of SettingUsecase).
type SettingReader interface {
	GetByKey(ctx context.Context, key string) (string, error)
}

// Collector gathers business and infrastructure metrics on each tick.
type Collector struct {
	db            *sql.DB
	statsProvider StatsProvider
	settingUC     SettingReader
}

// NewCollector creates a Collector.
func NewCollector(db *sql.DB, sp StatsProvider, settingUC SettingReader) *Collector {
	return &Collector{db: db, statsProvider: sp, settingUC: settingUC}
}

// Collect queries all stats and updates Prometheus gauges.
func (c *Collector) Collect(ctx context.Context) {
	// Sync the Enabled flag from the DB setting (runs every scheduler tick ~5s)
	if c.settingUC != nil {
		if val, err := c.settingUC.GetByKey(ctx, "metrics_enabled"); err == nil {
			Enabled.Store(val != "false")
		}
	}
	if !Enabled.Load() {
		return
	}

	log := logger.GetLogger()

	// Dashboard stats (users, subscriptions)
	if stats, err := c.statsProvider.GetDashboardStats(ctx); err == nil {
		UsersTotal.WithLabelValues("total").Set(float64(stats.TotalUsers))
		UsersTotal.WithLabelValues("active").Set(float64(stats.ActiveUsers))
		UsersTotal.WithLabelValues("banned").Set(float64(stats.BannedUsers))
		UsersTotal.WithLabelValues("admin").Set(float64(stats.AdminUsers))

		SubscriptionsTotal.WithLabelValues("total").Set(float64(stats.TotalSubscriptions))
		SubscriptionsTotal.WithLabelValues("active").Set(float64(stats.ActiveSubscriptions))
		SubscriptionsTotal.WithLabelValues("expired").Set(float64(stats.ExpiredSubscriptions))
	} else {
		log.WithError(err).Debug("Metrics: failed to collect dashboard stats")
	}

	// Node online/offline
	if online, offline, err := c.statsProvider.GetNodeStatuses(ctx); err == nil {
		NodesTotal.WithLabelValues("online").Set(float64(online))
		NodesTotal.WithLabelValues("offline").Set(float64(offline))
	} else {
		log.WithError(err).Debug("Metrics: failed to collect node statuses")
	}

	// Provisioning queue
	if counts, err := c.statsProvider.GetProvisioningCounts(ctx); err == nil {
		for status, count := range counts {
			ProvisioningQueueDepth.WithLabelValues(status).Set(float64(count))
		}
	} else {
		log.WithError(err).Debug("Metrics: failed to collect provisioning counts")
	}

	// Certificate expiries
	if expiries, err := c.statsProvider.GetCertificateExpiries(ctx); err == nil {
		now := time.Now()
		for domain, expiresAt := range expiries {
			CertificateExpirySeconds.WithLabelValues(domain).Set(expiresAt.Sub(now).Seconds())
		}
	} else {
		log.WithError(err).Debug("Metrics: failed to collect certificate expiries")
	}

	// DB connection pool
	if c.db != nil {
		dbStats := c.db.Stats()
		DBOpenConnections.WithLabelValues("in_use").Set(float64(dbStats.InUse))
		DBOpenConnections.WithLabelValues("idle").Set(float64(dbStats.Idle))
		DBMaxConnections.Set(float64(dbStats.MaxOpenConnections))
	}
}
