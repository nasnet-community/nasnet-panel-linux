package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

type NodeRepository interface {
	// Node Operations
	CreateNode(ctx context.Context, node *domain.Node) error
	GetNode(ctx context.Context, id uint) (*domain.Node, error)
	GetNodeByUUID(ctx context.Context, uuid string) (*domain.Node, error)
	UpdateNode(ctx context.Context, node *domain.Node) error
	DeleteNode(ctx context.Context, id uint) error
	ListNodes(ctx context.Context) ([]*domain.Node, error)
	ListActiveNodes(ctx context.Context) ([]*domain.Node, error)
	UpdateNodeStatus(ctx context.Context, id uint, isOnline bool, lastCheck time.Time) error
	UpdateNodeDNSSettings(ctx context.Context, nodeID uint, settings *domain.DNSSettings) error
	UpdateNodeFakeDNSSettings(ctx context.Context, nodeID uint, pools []domain.FakeDNSPool) error

	// Inbound Operations
	CreateInbound(ctx context.Context, inbound *domain.Inbound) error
	GetInbound(ctx context.Context, id uint) (*domain.Inbound, error)
	UpdateInbound(ctx context.Context, inbound *domain.Inbound) error
	DeleteInbound(ctx context.Context, id uint) error
	ListInboundsByNode(ctx context.Context, nodeID uint) ([]*domain.Inbound, error)

	ToggleInboundDisabled(ctx context.Context, id uint) error

	// Helper to fetch an inbound with its parent node details (for connection info)
	GetInboundWithNode(ctx context.Context, id uint) (*domain.Inbound, error)

	// Discovery-related methods
	GetInboundByTagAndNode(ctx context.Context, nodeID uint, tag string) (*domain.Inbound, error)
	BulkCreateInbounds(ctx context.Context, inbounds []*domain.Inbound) error
	DeleteInboundsByNodeExceptTags(ctx context.Context, nodeID uint, keepTags []string) (int64, error)

	// Outbound Operations
	CreateOutbound(ctx context.Context, outbound *domain.Outbound) error
	GetOutbound(ctx context.Context, id uint) (*domain.Outbound, error)
	UpdateOutbound(ctx context.Context, outbound *domain.Outbound) error
	DeleteOutbound(ctx context.Context, id uint) error
	ListOutboundsByNode(ctx context.Context, nodeID uint) ([]*domain.Outbound, error)

	// Helper to fetch an outbound with its parent node details
	GetOutboundWithNode(ctx context.Context, id uint) (*domain.Outbound, error)

	// Outbound Discovery-related methods
	GetOutboundByTagAndNode(ctx context.Context, nodeID uint, tag string) (*domain.Outbound, error)
	BulkCreateOutbounds(ctx context.Context, outbounds []*domain.Outbound) error
	DeleteOutboundsByNodeExceptTags(ctx context.Context, nodeID uint, keepTags []string) (int64, error)
	ToggleOutboundDisabled(ctx context.Context, id uint) error
	ListRoutingRulesByOutboundTag(ctx context.Context, nodeID uint, tag string) ([]*domain.RoutingRule, error)

	// Routing Rule Operations
	CreateRoutingRule(ctx context.Context, rule *domain.RoutingRule) error
	GetRoutingRule(ctx context.Context, id uint) (*domain.RoutingRule, error)
	UpdateRoutingRule(ctx context.Context, rule *domain.RoutingRule) error
	DeleteRoutingRule(ctx context.Context, id uint) error
	ListRoutingRulesByNode(ctx context.Context, nodeID uint) ([]*domain.RoutingRule, error)
	GetRoutingRuleWithNode(ctx context.Context, id uint) (*domain.RoutingRule, error)
	GetRoutingRuleByTagAndNode(ctx context.Context, nodeID uint, tag string) (*domain.RoutingRule, error)
	FindAdjacentRoutingRule(ctx context.Context, nodeID uint, currentPriority int, currentID uint, moveUp bool) (*domain.RoutingRule, error)
	ReorderRoutingRules(ctx context.Context, nodeID uint, ruleIDs []uint) error
	DeleteRoutingRulesByNodeAndSource(ctx context.Context, nodeID uint, source string) error

	// Balancing Rule Operations
	CreateBalancingRule(ctx context.Context, rule *domain.BalancingRule) error
	GetBalancingRule(ctx context.Context, id uint) (*domain.BalancingRule, error)
	ListBalancingRulesByNode(ctx context.Context, nodeID uint) ([]*domain.BalancingRule, error)
	UpdateBalancingRule(ctx context.Context, rule *domain.BalancingRule) error
	DeleteBalancingRule(ctx context.Context, id uint) error
	DeleteBalancingRulesByNode(ctx context.Context, nodeID uint) error

	// Traffic Accumulation
	AddNodeTraffic(ctx context.Context, nodeID uint, uplink, downlink int64) error
	AddOutboundTraffic(ctx context.Context, nodeID uint, tag string, uplink, downlink int64) error

	// Stats Operations
	CreateNodeStat(ctx context.Context, stat *domain.NodeStat) error
	GetNodeStatsHistory(ctx context.Context, nodeID uint, limit int) ([]*domain.NodeStat, error)
	// GetNodesStatsHistoryBulk: most recent `limit` per node, grouped in
	// Go. Powers /nodes/stats/history/bulk for sparklines.
	GetNodesStatsHistoryBulk(ctx context.Context, nodeIDs []uint, limit int) (map[uint][]*domain.NodeStat, error)

	// Bulk deletion by node
	DeleteOutboundsByNode(ctx context.Context, nodeID uint) error
	DeleteRoutingRulesByNode(ctx context.Context, nodeID uint) error
	DeleteNodeStatsByNode(ctx context.Context, nodeID uint) error

	// Transactions
	Transaction(ctx context.Context, fn func(txRepo NodeRepository) error) error

	// Reverse Proxy Operations
	CreateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error
	GetReverseProxy(ctx context.Context, id uint) (*domain.ReverseProxy, error)
	GetReverseProxyWithNode(ctx context.Context, id uint) (*domain.ReverseProxy, error)
	UpdateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error
	DeleteReverseProxy(ctx context.Context, id uint) error
	ListReverseProxiesByNode(ctx context.Context, nodeID uint) ([]*domain.ReverseProxy, error)
	DeleteReverseProxiesByNode(ctx context.Context, nodeID uint) error
	ListReverseProxiesByReferencedTag(ctx context.Context, nodeID uint, tag string) ([]*domain.ReverseProxy, error)

	// Cleanup
	CleanupOldNodeStats(ctx context.Context, olderThanDays int) (int64, error)
	CleanupOldNodeDailyTraffic(ctx context.Context, olderThanDays int) (int64, error)
	CleanupOldUptimeEvents(ctx context.Context, olderThanDays int) (int64, error)
	CleanupOldStarlinkStats(ctx context.Context, olderThanDays int) (int64, error)

	// Batch fetch inbounds by IDs with Node preloaded
	FindInboundsByIDs(ctx context.Context, ids []uint) ([]*domain.Inbound, error)

	// Host Operations
	CreateHost(ctx context.Context, host *domain.Host) error
	GetHost(ctx context.Context, id uint) (*domain.Host, error)
	GetHostWithInbound(ctx context.Context, id uint) (*domain.Host, error)
	UpdateHost(ctx context.Context, host *domain.Host) error
	DeleteHost(ctx context.Context, id uint) error
	ListHostsByInbound(ctx context.Context, inboundID uint) ([]*domain.Host, error)
	ListHostsByPlan(ctx context.Context, planID uint) ([]*domain.Host, error)
	DeleteHostsByInbound(ctx context.Context, inboundID uint) error
	ListAllHosts(ctx context.Context, search string, nodeID, inboundID, planID uint, isDisabled *bool, hostType string, tag string, sortBy string, sortOrder string, offset, limit int) ([]*domain.Host, int64, error)
	BulkUpdateHosts(ctx context.Context, ids []uint, fields map[string]any) (int64, error)
	ListHostTags(ctx context.Context) ([]string, error)

	// Host Template Operations
	CreateHostTemplate(ctx context.Context, template *domain.HostTemplate) error
	GetHostTemplate(ctx context.Context, id uint) (*domain.HostTemplate, error)
	UpdateHostTemplate(ctx context.Context, template *domain.HostTemplate) error
	DeleteHostTemplate(ctx context.Context, id uint) error
	ListHostTemplates(ctx context.Context) ([]*domain.HostTemplate, error)

	// Access Log Summaries
	UpsertAccessLogSummary(ctx context.Context, summary *domain.AccessLogSummary) error
	GetAccessLogSummaries(ctx context.Context, filter AccessLogSummaryFilter) ([]*domain.AccessLogSummary, int64, error)
	GetAccessLogTopDomains(ctx context.Context, filter AccessLogSummaryFilter) ([]*domain.AccessLogSummary, error)
	GetHourlyAggregates(ctx context.Context, filter AccessLogSummaryFilter) ([]HourlyAggregate, error)
	GetAccessLogTimeSeries(ctx context.Context, filter AccessLogSummaryFilter, granularity string) ([]AccessLogTimeBucket, error)
	GetAccessLogTotals(ctx context.Context, filter AccessLogSummaryFilter) (AccessLogTotals, error)
	SearchAccessLog(ctx context.Context, filter AccessLogSearchFilter) ([]AccessLogSearchHit, bool, error)
	CleanupOldAccessLogSummaries(ctx context.Context, before time.Time) (int64, error)
	MarkAccessLogSynced(ctx context.Context, nodeID uint, t time.Time) error
	GetNodesLastAccessLogSyncedAt(ctx context.Context, nodeIDs []uint) (map[uint]time.Time, error)

	// Daily Traffic
	AddNodeDailyTraffic(ctx context.Context, nodeID uint, date time.Time, uplink, downlink int64) error
	GetNodeDailyTraffic(ctx context.Context, nodeID uint, days int) ([]*domain.NodeDailyTraffic, error)

	// Uptime Events
	CreateUptimeEvent(ctx context.Context, event *domain.NodeUptimeEvent) error
	GetUptimeEvents(ctx context.Context, nodeID uint, since time.Time) ([]*domain.NodeUptimeEvent, error)

	// Starlink Stats
	CreateStarlinkStat(ctx context.Context, stat *domain.StarlinkStat) error
	GetStarlinkStatsHistory(ctx context.Context, nodeID uint, limit int, since *time.Time) ([]*domain.StarlinkStat, error)
}

// HourlyAggregate holds connection stats aggregated by hour of day (0-23).
type HourlyAggregate struct {
	Hour        int   `json:"hour"`
	Accepted    int64 `json:"accepted"`
	Rejected    int64 `json:"rejected"`
	TcpCount    int64 `json:"tcp_count"`
	UdpCount    int64 `json:"udp_count"`
	UniqueUsers int64 `json:"unique_users"`
}

// AccessLogTimeBucket is one row in a time-series response — the count
// totals for everything that landed in [Bucket, Bucket+granularity).
type AccessLogTimeBucket struct {
	Bucket        time.Time `json:"bucket"`
	AcceptedCount int64     `json:"accepted_count"`
	RejectedCount int64     `json:"rejected_count"`
	TcpCount      int64     `json:"tcp_count"`
	UdpCount      int64     `json:"udp_count"`
}

// AccessLogTotals are the SUM aggregates over the entire filter window.
type AccessLogTotals struct {
	AcceptedCount int64 `json:"accepted_count"`
	RejectedCount int64 `json:"rejected_count"`
	TcpCount      int64 `json:"tcp_count"`
	UdpCount      int64 `json:"udp_count"`
	HourBuckets   int64 `json:"hour_buckets"` // number of (email, node, hour) rows in window
}

// AccessLogSearchHit is one match returned by SearchAccessLog: the (hour, node,
// email) bucket where the value was observed and the count it had inside the
// hourly top-N JSON blob.
type AccessLogSearchHit struct {
	Bucket time.Time `json:"bucket"`
	NodeID uint      `json:"node_id"`
	Email  string    `json:"email"`
	Kind   string    `json:"kind"` // "domain" | "rejected_domain" | "source_ip"
	Value  string    `json:"value"`
	Count  int64     `json:"count"`
}

// AccessLogSearchFilter narrows SearchAccessLog. Query is matched as a
// case-insensitive substring against the JSON keys (domain or IP) inside the
// requested Kinds. An empty Kinds slice matches all three kinds.
type AccessLogSearchFilter struct {
	NodeIDs         []uint
	Emails          []string
	SubscriptionIDs []uint
	From            time.Time
	To              time.Time
	Query           string
	Kinds           []string // subset of {"domain","rejected_domain","source_ip"}; empty = all
	Limit           int      // hard cap on returned hits; 0 → no cap
}

// AccessLogSummaryFilter. Email is the legacy single-user path; Emails
// is the multi-value path used by sub-history. Emails wins if both set.
// SubscriptionIDs is the preferred per-sub path post-denormalisation —
// hits the (subscription_id, hour_time) index directly without the
// email-expansion fan-out.
type AccessLogSummaryFilter struct {
	NodeIDs         []uint
	Email           string
	Emails          []string
	SubscriptionIDs []uint
	From            time.Time
	To              time.Time
	Limit           int
	Offset          int
}

type nodeRepository struct {
	db *gorm.DB
}

func NewNodeRepository(db *gorm.DB) NodeRepository {
	return &nodeRepository{db: db}
}

func (r *nodeRepository) CreateNode(ctx context.Context, node *domain.Node) error {
	return r.db.WithContext(ctx).Create(node).Error
}

func (r *nodeRepository) GetNode(ctx context.Context, id uint) (*domain.Node, error) {
	var node domain.Node
	if err := r.db.WithContext(ctx).Preload("Inbounds").First(&node, id).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *nodeRepository) GetNodeByUUID(ctx context.Context, uuid string) (*domain.Node, error) {
	var node domain.Node
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *nodeRepository) UpdateNode(ctx context.Context, node *domain.Node) error {
	return r.db.WithContext(ctx).Save(node).Error
}

func (r *nodeRepository) UpdateNodeStatus(ctx context.Context, id uint, isOnline bool, lastCheck time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", id).Updates(map[string]interface{}{
		"is_online":  isOnline,
		"last_check": lastCheck,
	}).Error
}

func (r *nodeRepository) UpdateNodeDNSSettings(ctx context.Context, nodeID uint, settings *domain.DNSSettings) error {
	var val any
	if settings != nil {
		b, err := json.Marshal(settings)
		if err != nil {
			return fmt.Errorf("marshal dns settings: %w", err)
		}
		val = string(b)
	}
	return r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", nodeID).Update("dns_settings", val).Error
}

func (r *nodeRepository) UpdateNodeFakeDNSSettings(ctx context.Context, nodeID uint, pools []domain.FakeDNSPool) error {
	var val any
	if pools != nil {
		b, err := json.Marshal(pools)
		if err != nil {
			return fmt.Errorf("marshal fakedns settings: %w", err)
		}
		val = string(b)
	}
	return r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", nodeID).Update("fake_dns_settings", val).Error
}

func (r *nodeRepository) DeleteNode(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.Node{}, id).Error
}

func (r *nodeRepository) ListNodes(ctx context.Context) ([]*domain.Node, error) {
	var nodes []*domain.Node
	if err := r.db.WithContext(ctx).Preload("Inbounds").Order("id ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func (r *nodeRepository) ListActiveNodes(ctx context.Context) ([]*domain.Node, error) {
	var nodes []*domain.Node
	if err := r.db.WithContext(ctx).
		Preload("Inbounds").
		Where("is_active = ?", true).
		Order("id ASC").
		Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func (r *nodeRepository) CreateInbound(ctx context.Context, inbound *domain.Inbound) error {
	return r.db.WithContext(ctx).Create(inbound).Error
}

func (r *nodeRepository) GetInbound(ctx context.Context, id uint) (*domain.Inbound, error) {
	var inbound domain.Inbound
	if err := r.db.WithContext(ctx).First(&inbound, id).Error; err != nil {
		return nil, err
	}
	return &inbound, nil
}

func (r *nodeRepository) GetInboundWithNode(ctx context.Context, id uint) (*domain.Inbound, error) {
	var inbound domain.Inbound
	// Joins/Preload to get the Node details required for dialing
	if err := r.db.WithContext(ctx).Joins("Node").First(&inbound, id).Error; err != nil {
		return nil, err
	}
	return &inbound, nil
}

func (r *nodeRepository) UpdateInbound(ctx context.Context, inbound *domain.Inbound) error {
	return r.db.WithContext(ctx).Save(inbound).Error
}

func (r *nodeRepository) DeleteInbound(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.Inbound{}, id).Error
}

func (r *nodeRepository) ToggleInboundDisabled(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&domain.Inbound{}).
		Where("id = ?", id).
		Update("is_disabled", gorm.Expr("NOT is_disabled")).Error
}

func (r *nodeRepository) ListInboundsByNode(ctx context.Context, nodeID uint) ([]*domain.Inbound, error) {
	var inbounds []*domain.Inbound
	if err := r.db.WithContext(ctx).
		Where("node_id = ?", nodeID).
		Order("port ASC").
		Find(&inbounds).Error; err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (r *nodeRepository) GetInboundByTagAndNode(ctx context.Context, nodeID uint, tag string) (*domain.Inbound, error) {
	var inbound domain.Inbound
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND tag = ?", nodeID, tag).
		First(&inbound).Error; err != nil {
		return nil, err
	}
	return &inbound, nil
}

func (r *nodeRepository) BulkCreateInbounds(ctx context.Context, inbounds []*domain.Inbound) error {
	if len(inbounds) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&inbounds).Error
}

func (r *nodeRepository) DeleteInboundsByNodeExceptTags(ctx context.Context, nodeID uint, keepTags []string) (int64, error) {
	query := r.db.WithContext(ctx).Where("node_id = ?", nodeID)
	if len(keepTags) > 0 {
		query = query.Where("tag NOT IN ?", keepTags)
	}
	result := query.Delete(&domain.Inbound{})
	return result.RowsAffected, result.Error
}

// === Outbound Operations ===

func (r *nodeRepository) CreateOutbound(ctx context.Context, outbound *domain.Outbound) error {
	return r.db.WithContext(ctx).Create(outbound).Error
}

func (r *nodeRepository) GetOutbound(ctx context.Context, id uint) (*domain.Outbound, error) {
	var outbound domain.Outbound
	if err := r.db.WithContext(ctx).First(&outbound, id).Error; err != nil {
		return nil, err
	}
	return &outbound, nil
}

func (r *nodeRepository) GetOutboundWithNode(ctx context.Context, id uint) (*domain.Outbound, error) {
	var outbound domain.Outbound
	if err := r.db.WithContext(ctx).Joins("Node").First(&outbound, id).Error; err != nil {
		return nil, err
	}
	return &outbound, nil
}

func (r *nodeRepository) UpdateOutbound(ctx context.Context, outbound *domain.Outbound) error {
	return r.db.WithContext(ctx).Save(outbound).Error
}

func (r *nodeRepository) DeleteOutbound(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.Outbound{}, id).Error
}

func (r *nodeRepository) ListOutboundsByNode(ctx context.Context, nodeID uint) ([]*domain.Outbound, error) {
	var outbounds []*domain.Outbound
	if err := r.db.WithContext(ctx).
		Where("node_id = ?", nodeID).
		Order("id ASC").
		Find(&outbounds).Error; err != nil {
		return nil, err
	}
	return outbounds, nil
}

func (r *nodeRepository) GetOutboundByTagAndNode(ctx context.Context, nodeID uint, tag string) (*domain.Outbound, error) {
	var outbound domain.Outbound
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND tag = ?", nodeID, tag).
		First(&outbound).Error; err != nil {
		return nil, err
	}
	return &outbound, nil
}

func (r *nodeRepository) BulkCreateOutbounds(ctx context.Context, outbounds []*domain.Outbound) error {
	if len(outbounds) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&outbounds).Error
}

func (r *nodeRepository) DeleteOutboundsByNodeExceptTags(ctx context.Context, nodeID uint, keepTags []string) (int64, error) {
	query := r.db.WithContext(ctx).Where("node_id = ?", nodeID)
	if len(keepTags) > 0 {
		query = query.Where("tag NOT IN ?", keepTags)
	}
	result := query.Delete(&domain.Outbound{})
	return result.RowsAffected, result.Error
}

// === Routing Rule Operations ===

func (r *nodeRepository) CreateRoutingRule(ctx context.Context, rule *domain.RoutingRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *nodeRepository) GetRoutingRule(ctx context.Context, id uint) (*domain.RoutingRule, error) {
	var rule domain.RoutingRule
	if err := r.db.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *nodeRepository) UpdateRoutingRule(ctx context.Context, rule *domain.RoutingRule) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

func (r *nodeRepository) DeleteRoutingRule(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.RoutingRule{}, id).Error
}

func (r *nodeRepository) ListRoutingRulesByNode(ctx context.Context, nodeID uint) ([]*domain.RoutingRule, error) {
	var rules []*domain.RoutingRule
	if err := r.db.WithContext(ctx).
		Where("node_id = ?", nodeID).
		Order("priority ASC, id ASC").
		Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *nodeRepository) GetRoutingRuleWithNode(ctx context.Context, id uint) (*domain.RoutingRule, error) {
	var rule domain.RoutingRule
	if err := r.db.WithContext(ctx).Preload("Node").First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *nodeRepository) GetRoutingRuleByTagAndNode(ctx context.Context, nodeID uint, tag string) (*domain.RoutingRule, error) {
	var rule domain.RoutingRule
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND rule_tag = ?", nodeID, tag).
		First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *nodeRepository) FindAdjacentRoutingRule(ctx context.Context, nodeID uint, currentPriority int, currentID uint, moveUp bool) (*domain.RoutingRule, error) {
	var rule domain.RoutingRule
	db := r.db.WithContext(ctx).Where("node_id = ?", nodeID)

	if moveUp {
		if err := db.Where("priority < ? OR (priority = ? AND id < ?)", currentPriority, currentPriority, currentID).
			Order("priority DESC, id DESC").First(&rule).Error; err != nil {
			return nil, err
		}
	} else {
		if err := db.Where("priority > ? OR (priority = ? AND id > ?)", currentPriority, currentPriority, currentID).
			Order("priority ASC, id ASC").First(&rule).Error; err != nil {
			return nil, err
		}
	}
	return &rule, nil
}

func (r *nodeRepository) ReorderRoutingRules(ctx context.Context, nodeID uint, ruleIDs []uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, id := range ruleIDs {
			result := tx.Model(&domain.RoutingRule{}).
				Where("id = ? AND node_id = ?", id, nodeID).
				Update("priority", i)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("routing rule %d not found for node %d", id, nodeID)
			}
		}
		return nil
	})
}

func (r *nodeRepository) DeleteRoutingRulesByNodeAndSource(ctx context.Context, nodeID uint, source string) error {
	return r.db.WithContext(ctx).
		Where("node_id = ? AND source = ?", nodeID, source).
		Delete(&domain.RoutingRule{}).Error
}

// === Balancing Rule Operations ===

func (r *nodeRepository) CreateBalancingRule(ctx context.Context, rule *domain.BalancingRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *nodeRepository) GetBalancingRule(ctx context.Context, id uint) (*domain.BalancingRule, error) {
	var rule domain.BalancingRule
	if err := r.db.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *nodeRepository) ListBalancingRulesByNode(ctx context.Context, nodeID uint) ([]*domain.BalancingRule, error) {
	var rules []*domain.BalancingRule
	if err := r.db.WithContext(ctx).Where("node_id = ?", nodeID).Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *nodeRepository) UpdateBalancingRule(ctx context.Context, rule *domain.BalancingRule) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

func (r *nodeRepository) DeleteBalancingRule(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.BalancingRule{}, id).Error
}

func (r *nodeRepository) DeleteBalancingRulesByNode(ctx context.Context, nodeID uint) error {
	return r.db.WithContext(ctx).Where("node_id = ?", nodeID).Delete(&domain.BalancingRule{}).Error
}

// === Traffic Accumulation ===

func (r *nodeRepository) AddNodeTraffic(ctx context.Context, nodeID uint, uplink, downlink int64) error {
	return r.db.WithContext(ctx).Model(&domain.Node{}).Where("id = ?", nodeID).Updates(map[string]interface{}{
		"total_uplink":   gorm.Expr("total_uplink + ?", uplink),
		"total_downlink": gorm.Expr("total_downlink + ?", downlink),
	}).Error
}

func (r *nodeRepository) AddOutboundTraffic(ctx context.Context, nodeID uint, tag string, uplink, downlink int64) error {
	return r.db.WithContext(ctx).Model(&domain.Outbound{}).
		Where("node_id = ? AND tag = ?", nodeID, tag).
		Updates(map[string]interface{}{
			"uplink":   gorm.Expr("uplink + ?", uplink),
			"downlink": gorm.Expr("downlink + ?", downlink),
		}).Error
}

// === Stats Operations ===

func (r *nodeRepository) CreateNodeStat(ctx context.Context, stat *domain.NodeStat) error {
	return r.db.WithContext(ctx).Create(stat).Error
}

func (r *nodeRepository) GetNodeStatsHistory(ctx context.Context, nodeID uint, limit int) ([]*domain.NodeStat, error) {
	var stats []*domain.NodeStat
	if err := r.db.WithContext(ctx).
		Where("node_id = ?", nodeID).
		Order("created_at DESC").
		Limit(limit).
		Find(&stats).Error; err != nil {
		return nil, err
	}
	// Returned newest-first; callers reverse for chart rendering.
	return stats, nil
}

// GetNodesStatsHistoryBulk loads recent stats for several nodes in
// one round-trip and groups them into a per-node slice. Portable
// across Postgres and SQLite — avoids the Postgres-only ROW_NUMBER
// window function by pulling (nodes × limit) rows bounded and
// partitioning in Go.
func (r *nodeRepository) GetNodesStatsHistoryBulk(ctx context.Context, nodeIDs []uint, limit int) (map[uint][]*domain.NodeStat, error) {
	out := make(map[uint][]*domain.NodeStat, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	if limit <= 0 {
		limit = 60
	}

	// Cap fetched rows to bound (nodes × limit). Slight over-fetch lets
	// the per-node trim keep `limit` rows even with skewed sample counts.
	queryLimit := len(nodeIDs) * limit

	var rows []*domain.NodeStat
	if err := r.db.WithContext(ctx).
		Where("node_id IN ?", nodeIDs).
		Order("node_id ASC, created_at DESC").
		Limit(queryLimit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, s := range rows {
		existing := out[s.NodeID]
		if len(existing) >= limit {
			continue
		}
		out[s.NodeID] = append(existing, s)
	}
	return out, nil
}

func (r *nodeRepository) CleanupOldNodeStats(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&domain.NodeStat{})
	return result.RowsAffected, result.Error
}

// CleanupOldNodeDailyTraffic prunes NodeDailyTraffic older than retention.
func (r *nodeRepository) CleanupOldNodeDailyTraffic(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("date < ?", cutoff).
		Delete(&domain.NodeDailyTraffic{})
	return result.RowsAffected, result.Error
}

// CleanupOldUptimeEvents prunes uptime events older than retention.
// Grows fast under network flapping (1 row per transition).
func (r *nodeRepository) CleanupOldUptimeEvents(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("timestamp < ?", cutoff).
		Delete(&domain.NodeUptimeEvent{})
	return result.RowsAffected, result.Error
}

// CleanupOldStarlinkStats prunes StarlinkStat rows older than the retention
// window by `created_at`. Sample-rate is higher than NodeStat (dish telemetry
// is polled per stats-tick), so retention here is usually the shortest.
func (r *nodeRepository) CleanupOldStarlinkStats(ctx context.Context, olderThanDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&domain.StarlinkStat{})
	return result.RowsAffected, result.Error
}

func (r *nodeRepository) DeleteOutboundsByNode(ctx context.Context, nodeID uint) error {
	return r.db.WithContext(ctx).Where("node_id = ?", nodeID).Delete(&domain.Outbound{}).Error
}

func (r *nodeRepository) DeleteRoutingRulesByNode(ctx context.Context, nodeID uint) error {
	return r.db.WithContext(ctx).Where("node_id = ?", nodeID).Delete(&domain.RoutingRule{}).Error
}

func (r *nodeRepository) DeleteNodeStatsByNode(ctx context.Context, nodeID uint) error {
	return r.db.WithContext(ctx).Where("node_id = ?", nodeID).Delete(&domain.NodeStat{}).Error
}

func (r *nodeRepository) FindInboundsByIDs(ctx context.Context, ids []uint) ([]*domain.Inbound, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var inbounds []*domain.Inbound
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Preload("Node").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	return inbounds, nil
}

// === Host Operations ===

func (r *nodeRepository) CreateHost(ctx context.Context, host *domain.Host) error {
	return r.db.WithContext(ctx).Create(host).Error
}

func (r *nodeRepository) GetHost(ctx context.Context, id uint) (*domain.Host, error) {
	var host domain.Host
	if err := r.db.WithContext(ctx).First(&host, id).Error; err != nil {
		return nil, err
	}
	return &host, nil
}

func (r *nodeRepository) GetHostWithInbound(ctx context.Context, id uint) (*domain.Host, error) {
	var host domain.Host
	if err := r.db.WithContext(ctx).Preload("Inbound").Preload("Inbound.Node").First(&host, id).Error; err != nil {
		return nil, err
	}
	return &host, nil
}

func (r *nodeRepository) UpdateHost(ctx context.Context, host *domain.Host) error {
	return r.db.WithContext(ctx).Save(host).Error
}

func (r *nodeRepository) DeleteHost(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.Host{}, id).Error
}

func (r *nodeRepository) ListHostsByInbound(ctx context.Context, inboundID uint) ([]*domain.Host, error) {
	var hosts []*domain.Host
	if err := r.db.WithContext(ctx).
		Where("inbound_id = ?", inboundID).
		Order("priority ASC, id ASC").
		Find(&hosts).Error; err != nil {
		return nil, err
	}
	return hosts, nil
}

func (r *nodeRepository) DeleteHostsByInbound(ctx context.Context, inboundID uint) error {
	return r.db.WithContext(ctx).Where("inbound_id = ?", inboundID).Delete(&domain.Host{}).Error
}

func (r *nodeRepository) ListAllHosts(ctx context.Context, search string, nodeID, inboundID, planID uint, isDisabled *bool, hostType string, tag string, sortBy string, sortOrder string, offset, limit int) ([]*domain.Host, int64, error) {
	query := r.db.WithContext(ctx).Model(&domain.Host{}).
		Joins("LEFT JOIN inbounds ON inbounds.id = hosts.inbound_id AND inbounds.deleted_at IS NULL")

	if search != "" {
		escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(search)
		like := "%" + escaped + "%"
		query = query.Where("(hosts.remark LIKE ? OR hosts.address LIKE ?)", like, like)
	}
	if nodeID > 0 {
		query = query.Where("inbounds.node_id = ?", nodeID)
	}
	if inboundID > 0 {
		query = query.Where("hosts.inbound_id = ?", inboundID)
	}
	if planID > 0 {
		query = query.Where("hosts.plan_id = ?", planID)
	}
	if isDisabled != nil {
		query = query.Where("hosts.is_disabled = ?", *isDisabled)
	}
	switch hostType {
	case "server":
		query = query.Where("hosts.inbound_id IS NOT NULL")
	case "info":
		query = query.Where("hosts.plan_id IS NOT NULL AND hosts.inbound_id IS NULL")
	}
	if tag != "" {
		if database.IsPostgres() {
			query = query.Where("hosts.tags @> ?", fmt.Sprintf(`["%s"]`, tag))
		} else {
			query = query.Where("EXISTS (SELECT 1 FROM json_each(hosts.tags) WHERE json_each.value = ?)", tag)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Build ORDER BY with validated sort column
	allowedSortColumns := map[string]bool{
		"priority": true, "remark": true, "address": true,
		"security": true, "created_at": true, "updated_at": true,
	}
	orderClause := "hosts.priority ASC, hosts.id ASC"
	if sortBy != "" && allowedSortColumns[sortBy] {
		dir := "ASC"
		if strings.EqualFold(sortOrder, "desc") {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf("hosts.%s %s, hosts.id ASC", sortBy, dir)
	}

	var hosts []*domain.Host
	if err := query.
		Preload("Inbound").
		Preload("Inbound.Node").
		Select("hosts.*").
		Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&hosts).Error; err != nil {
		return nil, 0, err
	}

	return hosts, total, nil
}

func (r *nodeRepository) ListHostsByPlan(ctx context.Context, planID uint) ([]*domain.Host, error) {
	var hosts []*domain.Host
	if err := r.db.WithContext(ctx).
		Where("plan_id = ?", planID).
		Order("priority ASC, id ASC").
		Find(&hosts).Error; err != nil {
		return nil, err
	}
	return hosts, nil
}

func (r *nodeRepository) BulkUpdateHosts(ctx context.Context, ids []uint, fields map[string]any) (int64, error) {
	result := r.db.WithContext(ctx).Model(&domain.Host{}).Where("id IN ?", ids).Updates(fields)
	return result.RowsAffected, result.Error
}

func (r *nodeRepository) ListHostTags(ctx context.Context) ([]string, error) {
	var tags []string
	var query string
	if database.IsPostgres() {
		query = "SELECT DISTINCT jsonb_array_elements_text(tags) as tag FROM hosts WHERE deleted_at IS NULL ORDER BY tag"
	} else {
		query = "SELECT DISTINCT je.value as tag FROM hosts, json_each(hosts.tags) je WHERE hosts.deleted_at IS NULL ORDER BY tag"
	}
	err := r.db.WithContext(ctx).Raw(query).Scan(&tags).Error
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// === Host Template Operations ===

func (r *nodeRepository) CreateHostTemplate(ctx context.Context, template *domain.HostTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

func (r *nodeRepository) GetHostTemplate(ctx context.Context, id uint) (*domain.HostTemplate, error) {
	var template domain.HostTemplate
	if err := r.db.WithContext(ctx).First(&template, id).Error; err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *nodeRepository) UpdateHostTemplate(ctx context.Context, template *domain.HostTemplate) error {
	return r.db.WithContext(ctx).Save(template).Error
}

func (r *nodeRepository) DeleteHostTemplate(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.HostTemplate{}, id).Error
}

func (r *nodeRepository) ListHostTemplates(ctx context.Context) ([]*domain.HostTemplate, error) {
	var templates []*domain.HostTemplate
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

// === Access Log Summary Operations ===

func (r *nodeRepository) UpsertAccessLogSummary(ctx context.Context, summary *domain.AccessLogSummary) error {
	// Try to find existing record for the same (node_id, email, hour_time)
	var existing domain.AccessLogSummary
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND email = ? AND hour_time = ?", summary.NodeID, summary.Email, summary.HourTime).
		First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		return r.db.WithContext(ctx).Create(summary).Error
	}
	if err != nil {
		return err
	}

	// Replace all fields — agent sends full accumulated counts (not deltas),
	// so re-delivery after failed ack should overwrite, not double-count.
	updates := map[string]interface{}{
		"accepted_count":   summary.AcceptedCount,
		"rejected_count":   summary.RejectedCount,
		"tcp_count":        summary.TcpCount,
		"udp_count":        summary.UdpCount,
		"top_domains":      summary.TopDomains,
		"rejected_domains": summary.RejectedDomains,
		"source_ips":       summary.SourceIPs,
	}
	// Only overwrite subscription_id when caller actually resolved one;
	// a 0 value from a transient lookup miss must not clobber a good prior.
	if summary.SubscriptionID != 0 {
		updates["subscription_id"] = summary.SubscriptionID
	}
	return r.db.WithContext(ctx).Model(&existing).Updates(updates).Error
}

func (r *nodeRepository) GetAccessLogSummaries(ctx context.Context, filter AccessLogSummaryFilter) ([]*domain.AccessLogSummary, int64, error) {
	query := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	query = applyAccessLogFilter(query, filter)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var summaries []*domain.AccessLogSummary
	q := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	q = applyAccessLogFilter(q, filter)
	q = q.Order("hour_time DESC")
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	if err := q.Find(&summaries).Error; err != nil {
		return nil, 0, err
	}
	return summaries, total, nil
}

func (r *nodeRepository) GetAccessLogTopDomains(ctx context.Context, filter AccessLogSummaryFilter) ([]*domain.AccessLogSummary, error) {
	query := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	query = applyAccessLogFilter(query, filter)
	query = query.Select("top_domains")

	var summaries []*domain.AccessLogSummary
	if err := query.Find(&summaries).Error; err != nil {
		return nil, err
	}
	return summaries, nil
}

func (r *nodeRepository) GetHourlyAggregates(ctx context.Context, filter AccessLogSummaryFilter) ([]HourlyAggregate, error) {
	query := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	query = applyAccessLogFilter(query, filter)

	type rawRow struct {
		Hour        int   `gorm:"column:hour"`
		Accepted    int64 `gorm:"column:accepted"`
		Rejected    int64 `gorm:"column:rejected"`
		TcpCount    int64 `gorm:"column:tcp_count"`
		UdpCount    int64 `gorm:"column:udp_count"`
		UniqueUsers int64 `gorm:"column:unique_users"`
	}

	var rows []rawRow
	hourExpr := database.ExtractHour("hour_time")
	err := query.Select(fmt.Sprintf(`
		%s as hour,
		COALESCE(SUM(accepted_count), 0) as accepted,
		COALESCE(SUM(rejected_count), 0) as rejected,
		COALESCE(SUM(tcp_count), 0) as tcp_count,
		COALESCE(SUM(udp_count), 0) as udp_count,
		COUNT(DISTINCT email) as unique_users
	`, hourExpr)).Group("hour").Order("hour ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// Fill all 24 hours
	hourMap := make(map[int]*rawRow, len(rows))
	for i := range rows {
		hourMap[rows[i].Hour] = &rows[i]
	}

	result := make([]HourlyAggregate, 24)
	for h := 0; h < 24; h++ {
		result[h] = HourlyAggregate{Hour: h}
		if r, ok := hourMap[h]; ok {
			result[h].Accepted = r.Accepted
			result[h].Rejected = r.Rejected
			result[h].TcpCount = r.TcpCount
			result[h].UdpCount = r.UdpCount
			result[h].UniqueUsers = r.UniqueUsers
		}
	}
	return result, nil
}

// GetAccessLogTimeSeries: ASC, sparse (no zero-fill). granularity ∈ {hour, day};
// unknown falls back to hour.
func (r *nodeRepository) GetAccessLogTimeSeries(ctx context.Context, filter AccessLogSummaryFilter, granularity string) ([]AccessLogTimeBucket, error) {
	if granularity != "day" {
		granularity = "hour"
	}
	bucketExpr := database.TruncateTime("hour_time", granularity)

	// Scan the bucket column as a string so the dialect-specific output
	// formats round-trip safely. PostgreSQL's DATE_TRUNC returns a timestamp
	// (driver maps to time.Time → string via Stringer) and SQLite's
	// strftime returns a text value; both deserialise into a string here
	// and we parse to time.Time once on the Go side.
	type rawRow struct {
		Bucket        string `gorm:"column:bucket"`
		AcceptedCount int64  `gorm:"column:accepted"`
		RejectedCount int64  `gorm:"column:rejected"`
		TcpCount      int64  `gorm:"column:tcp_count"`
		UdpCount      int64  `gorm:"column:udp_count"`
	}

	q := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	q = applyAccessLogFilter(q, filter)
	q = q.Select(fmt.Sprintf(`
		%s as bucket,
		COALESCE(SUM(accepted_count), 0) as accepted,
		COALESCE(SUM(rejected_count), 0) as rejected,
		COALESCE(SUM(tcp_count), 0) as tcp_count,
		COALESCE(SUM(udp_count), 0) as udp_count
	`, bucketExpr)).Group("bucket").Order("bucket ASC")

	var rows []rawRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AccessLogTimeBucket, 0, len(rows))
	for _, r := range rows {
		t, err := parseBucketTime(r.Bucket, granularity)
		if err != nil {
			return nil, fmt.Errorf("parse bucket %q: %w", r.Bucket, err)
		}
		out = append(out, AccessLogTimeBucket{
			Bucket:        t,
			AcceptedCount: r.AcceptedCount,
			RejectedCount: r.RejectedCount,
			TcpCount:      r.TcpCount,
			UdpCount:      r.UdpCount,
		})
	}
	return out, nil
}

// parseBucketTime: accepts PG (RFC3339-ish) and SQLite ("YYYY-MM-DD HH:MM:SS"
// hour / "YYYY-MM-DD" day) formats. All buckets UTC.
func parseBucketTime(s, granularity string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST", // gorm time.Time stringer fallback
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	if granularity == "day" {
		// Try date-only formats first to avoid mis-parsing as midnight in
		// the local timezone.
		layouts = append([]string{"2006-01-02"}, layouts...)
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised bucket format")
}

// GetAccessLogTotals returns SUM aggregates for the filter window. Cheaper
// than counting summary rows in Go and avoids dragging the JSON top-domain
// blobs over the wire when the caller only needs totals.
func (r *nodeRepository) GetAccessLogTotals(ctx context.Context, filter AccessLogSummaryFilter) (AccessLogTotals, error) {
	q := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	q = applyAccessLogFilter(q, filter)

	var t AccessLogTotals
	row := struct {
		Accepted    int64 `gorm:"column:accepted"`
		Rejected    int64 `gorm:"column:rejected"`
		TcpCount    int64 `gorm:"column:tcp_count"`
		UdpCount    int64 `gorm:"column:udp_count"`
		HourBuckets int64 `gorm:"column:hour_buckets"`
	}{}
	if err := q.Select(`
		COALESCE(SUM(accepted_count), 0) as accepted,
		COALESCE(SUM(rejected_count), 0) as rejected,
		COALESCE(SUM(tcp_count), 0) as tcp_count,
		COALESCE(SUM(udp_count), 0) as udp_count,
		COUNT(*) as hour_buckets
	`).Scan(&row).Error; err != nil {
		return t, err
	}
	t.AcceptedCount = row.Accepted
	t.RejectedCount = row.Rejected
	t.TcpCount = row.TcpCount
	t.UdpCount = row.UdpCount
	t.HourBuckets = row.HourBuckets
	return t, nil
}

// SearchAccessLog: ILIKE pre-filter + Go-side substring match against the
// hourly top-N JSON blobs (need per-domain count, not just row presence).
// Blind spot: keys below the agent's per-hour top-N cap aren't stored.
func (r *nodeRepository) SearchAccessLog(ctx context.Context, filter AccessLogSearchFilter) ([]AccessLogSearchHit, bool, error) {
	q := strings.TrimSpace(filter.Query)
	if q == "" {
		return nil, false, nil
	}

	wantDomain, wantRejected, wantSourceIP := resolveSearchKinds(filter.Kinds)

	tx := r.db.WithContext(ctx).Model(&domain.AccessLogSummary{})
	if len(filter.NodeIDs) > 0 {
		tx = tx.Where("node_id IN ?", filter.NodeIDs)
	}
	// Hybrid scope (see applyAccessLogFilter for rationale).
	switch {
	case len(filter.SubscriptionIDs) > 0 && len(filter.Emails) > 0:
		tx = tx.Where(
			"subscription_id IN ? OR ((subscription_id IS NULL OR subscription_id = 0) AND email IN ?)",
			filter.SubscriptionIDs, filter.Emails,
		)
	case len(filter.SubscriptionIDs) > 0:
		tx = tx.Where("subscription_id IN ?", filter.SubscriptionIDs)
	case len(filter.Emails) > 0:
		tx = tx.Where("email IN ?", filter.Emails)
	}
	if !filter.From.IsZero() {
		tx = tx.Where("hour_time >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		tx = tx.Where("hour_time <= ?", filter.To)
	}

	// SQL pre-filter: rows where AT LEAST ONE requested JSON blob contains the
	// substring. Go-side parse below confirms a real key match (LIKE '%foo%'
	// can spuriously match counts or other keys).
	like := "%" + strings.ToLower(q) + "%"
	var clauses []string
	var args []interface{}
	if wantDomain {
		clauses = append(clauses, database.ILike("top_domains", "?"))
		args = append(args, like)
	}
	if wantRejected {
		clauses = append(clauses, database.ILike("rejected_domains", "?"))
		args = append(args, like)
	}
	if wantSourceIP {
		clauses = append(clauses, database.ILike("source_ips", "?"))
		args = append(args, like)
	}
	if len(clauses) == 0 {
		return nil, false, nil
	}
	tx = tx.Where(strings.Join(clauses, " OR "), args...)

	type row struct {
		NodeID          uint      `gorm:"column:node_id"`
		Email           string    `gorm:"column:email"`
		HourTime        time.Time `gorm:"column:hour_time"`
		TopDomains      string    `gorm:"column:top_domains"`
		RejectedDomains string    `gorm:"column:rejected_domains"`
		SourceIPs       string    `gorm:"column:source_ips"`
	}
	var rows []row
	if err := tx.Select("node_id, email, hour_time, top_domains, rejected_domains, source_ips").
		Order("hour_time ASC, node_id ASC, email ASC").
		Find(&rows).Error; err != nil {
		return nil, false, err
	}

	needle := strings.ToLower(q)
	hits := make([]AccessLogSearchHit, 0, len(rows))
	limit := filter.Limit
	truncated := false

	appendMatches := func(blob, kind string, hourTime time.Time, nodeID uint, email string) bool {
		if blob == "" {
			return true
		}
		var m map[string]int64
		if err := json.Unmarshal([]byte(blob), &m); err != nil {
			// Bad payload — skip this row's blob and keep going. The summary
			// is still useful for other kinds and other rows.
			return true
		}
		for k, v := range m {
			if !strings.Contains(strings.ToLower(k), needle) {
				continue
			}
			hits = append(hits, AccessLogSearchHit{
				Bucket: hourTime.UTC(),
				NodeID: nodeID,
				Email:  email,
				Kind:   kind,
				Value:  k,
				Count:  v,
			})
			if limit > 0 && len(hits) >= limit {
				truncated = true
				return false
			}
		}
		return true
	}

	for _, r := range rows {
		if wantDomain && !appendMatches(r.TopDomains, "domain", r.HourTime, r.NodeID, r.Email) {
			break
		}
		if wantRejected && !appendMatches(r.RejectedDomains, "rejected_domain", r.HourTime, r.NodeID, r.Email) {
			break
		}
		if wantSourceIP && !appendMatches(r.SourceIPs, "source_ip", r.HourTime, r.NodeID, r.Email) {
			break
		}
	}
	return hits, truncated, nil
}

// resolveSearchKinds normalises requested kinds into the three boolean toggles
// SearchAccessLog uses. An empty slice opts into all three.
func resolveSearchKinds(kinds []string) (domain, rejected, sourceIP bool) {
	if len(kinds) == 0 {
		return true, true, true
	}
	for _, k := range kinds {
		switch k {
		case "domain":
			domain = true
		case "rejected_domain":
			rejected = true
		case "source_ip":
			sourceIP = true
		}
	}
	return
}

func (r *nodeRepository) CleanupOldAccessLogSummaries(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("hour_time < ?", before).
		Delete(&domain.AccessLogSummary{})
	return result.RowsAffected, result.Error
}

// MarkAccessLogSynced stamps the node's last_access_log_synced_at to t.
// Called from node_stats after a successful summary Ack so the UI can
// render a freshness pill per node.
func (r *nodeRepository) MarkAccessLogSynced(ctx context.Context, nodeID uint, t time.Time) error {
	return r.db.WithContext(ctx).
		Model(&domain.Node{}).
		Where("id = ?", nodeID).
		Update("last_access_log_synced_at", t).Error
}

// GetNodesLastAccessLogSyncedAt returns the last successful sync time
// for each requested node. Nodes with no recorded sync are omitted from
// the result map. Empty input → empty map.
func (r *nodeRepository) GetNodesLastAccessLogSyncedAt(ctx context.Context, nodeIDs []uint) (map[uint]time.Time, error) {
	out := map[uint]time.Time{}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	type row struct {
		ID                    uint       `gorm:"column:id"`
		LastAccessLogSyncedAt *time.Time `gorm:"column:last_access_log_synced_at"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&domain.Node{}).
		Select("id, last_access_log_synced_at").
		Where("id IN ?", nodeIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.LastAccessLogSyncedAt != nil {
			out[r.ID] = r.LastAccessLogSyncedAt.UTC()
		}
	}
	return out, nil
}

func applyAccessLogFilter(query *gorm.DB, filter AccessLogSummaryFilter) *gorm.DB {
	if len(filter.NodeIDs) > 0 {
		query = query.Where("node_id IN ?", filter.NodeIDs)
	}
	// Hybrid scope when caller supplies BOTH subscription_ids and emails:
	// match post-backfill rows by subscription_id OR pre-backfill rows
	// (subscription_id NULL/0 — NULL for rows pre-dating the column add)
	// by email. Leak prevention preserved — emails were resolved from
	// the same subscription set.
	switch {
	case len(filter.SubscriptionIDs) > 0 && len(filter.Emails) > 0:
		query = query.Where(
			"subscription_id IN ? OR ((subscription_id IS NULL OR subscription_id = 0) AND email IN ?)",
			filter.SubscriptionIDs, filter.Emails,
		)
	case len(filter.SubscriptionIDs) > 0:
		query = query.Where("subscription_id IN ?", filter.SubscriptionIDs)
	case len(filter.Emails) > 0:
		query = query.Where("email IN ?", filter.Emails)
	case filter.Email != "":
		query = query.Where("email = ?", filter.Email)
	}
	if !filter.From.IsZero() {
		query = query.Where("hour_time >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		query = query.Where("hour_time <= ?", filter.To)
	}
	return query
}

// === Daily Traffic Operations ===

func (r *nodeRepository) AddNodeDailyTraffic(ctx context.Context, nodeID uint, date time.Time, uplink, downlink int64) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO node_daily_traffics (node_id, date, uplink, downlink, created_at)
		VALUES (?, ?, ?, ?, `+database.Now()+`)
		ON CONFLICT (node_id, date) DO UPDATE SET
			uplink = node_daily_traffics.uplink + EXCLUDED.uplink,
			downlink = node_daily_traffics.downlink + EXCLUDED.downlink
	`, nodeID, date, uplink, downlink).Error
}

func (r *nodeRepository) GetNodeDailyTraffic(ctx context.Context, nodeID uint, days int) ([]*domain.NodeDailyTraffic, error) {
	var records []*domain.NodeDailyTraffic
	since := time.Now().AddDate(0, 0, -days)
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND date >= ?", nodeID, since).
		Order("date ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// === Uptime Event Operations ===

func (r *nodeRepository) CreateUptimeEvent(ctx context.Context, event *domain.NodeUptimeEvent) error {
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *nodeRepository) GetUptimeEvents(ctx context.Context, nodeID uint, since time.Time) ([]*domain.NodeUptimeEvent, error) {
	var events []*domain.NodeUptimeEvent
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND timestamp >= ?", nodeID, since).
		Order("timestamp ASC").
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// === Starlink Stats Operations ===

func (r *nodeRepository) CreateStarlinkStat(ctx context.Context, stat *domain.StarlinkStat) error {
	return r.db.WithContext(ctx).Create(stat).Error
}

// GetStarlinkStatsHistory: ASC samples. With `since`, load the window and
// downsample in Go to `limit` rows (SQL ORDER DESC + LIMIT would collapse a
// 7d view to ~the last hour at 5s sample rate). Without `since`, last
// `limit` rows reversed to ASC.
func (r *nodeRepository) GetStarlinkStatsHistory(ctx context.Context, nodeID uint, limit int, since *time.Time) ([]*domain.StarlinkStat, error) {
	if limit <= 0 {
		return nil, nil
	}

	if since == nil {
		var stats []*domain.StarlinkStat
		if err := r.db.WithContext(ctx).
			Where("node_id = ?", nodeID).
			Order("created_at DESC").
			Limit(limit).
			Find(&stats).Error; err != nil {
			return nil, err
		}
		// Reverse in place so the result is oldest→newest, same as the
		// windowed branch.
		for i, j := 0, len(stats)-1; i < j; i, j = i+1, j-1 {
			stats[i], stats[j] = stats[j], stats[i]
		}
		return stats, nil
	}

	var stats []*domain.StarlinkStat
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND created_at >= ?", nodeID, *since).
		Order("created_at ASC").
		Find(&stats).Error; err != nil {
		return nil, err
	}

	if len(stats) <= limit {
		return stats, nil
	}
	return downsampleStarlinkStats(stats, limit), nil
}

// downsampleStarlinkStats: mean numerics + OR alert flags per bucket.
// CreatedAt taken from bucket midpoint so X-axis stays monotonic.
func downsampleStarlinkStats(src []*domain.StarlinkStat, limit int) []*domain.StarlinkStat {
	if limit <= 0 || len(src) <= limit {
		return src
	}
	out := make([]*domain.StarlinkStat, 0, limit)
	n := len(src)
	for i := 0; i < limit; i++ {
		start := i * n / limit
		end := (i + 1) * n / limit
		if end <= start {
			end = start + 1
		}
		if end > n {
			end = n
		}
		bucket := src[start:end]
		var (
			dl, ul, lat, drop, obs, tilt, az, el float64
			obstructed, gpsValid                 bool
			flags                                uint32
			nodeID                               uint
		)
		for _, s := range bucket {
			dl += s.DownlinkThroughputBps
			ul += s.UplinkThroughputBps
			lat += s.PopPingLatencyMs
			drop += s.PopPingDropRate
			obs += s.ObstructionFraction
			tilt += s.TiltAngleDeg
			az += s.BoresightAzimuthDeg
			el += s.BoresightElevationDeg
			obstructed = obstructed || s.CurrentlyObstructed
			gpsValid = gpsValid || s.GpsValid
			flags |= s.AlertFlags
			nodeID = s.NodeID
		}
		count := float64(len(bucket))
		mid := bucket[len(bucket)/2]
		out = append(out, &domain.StarlinkStat{
			ID:                    mid.ID,
			NodeID:                nodeID,
			DownlinkThroughputBps: dl / count,
			UplinkThroughputBps:   ul / count,
			PopPingLatencyMs:      lat / count,
			PopPingDropRate:       drop / count,
			ObstructionFraction:   obs / count,
			CurrentlyObstructed:   obstructed,
			TiltAngleDeg:          tilt / count,
			BoresightAzimuthDeg:   az / count,
			BoresightElevationDeg: el / count,
			GpsValid:              gpsValid,
			AlertFlags:            flags,
			CreatedAt:             mid.CreatedAt,
		})
	}
	return out
}

// === Transaction Support ===

func (r *nodeRepository) Transaction(ctx context.Context, fn func(txRepo NodeRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&nodeRepository{db: tx})
	})
}

// === Reverse Proxy Operations ===

func (r *nodeRepository) CreateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error {
	// Purge any soft-deleted row with the same (node_id, tag) to avoid unique constraint conflicts
	r.db.WithContext(ctx).Unscoped().
		Where("node_id = ? AND tag = ? AND deleted_at IS NOT NULL", rp.NodeID, rp.Tag).
		Delete(&domain.ReverseProxy{})
	return r.db.WithContext(ctx).Create(rp).Error
}

func (r *nodeRepository) GetReverseProxy(ctx context.Context, id uint) (*domain.ReverseProxy, error) {
	var rp domain.ReverseProxy
	if err := r.db.WithContext(ctx).First(&rp, id).Error; err != nil {
		return nil, err
	}
	return &rp, nil
}

func (r *nodeRepository) GetReverseProxyWithNode(ctx context.Context, id uint) (*domain.ReverseProxy, error) {
	var rp domain.ReverseProxy
	if err := r.db.WithContext(ctx).Preload("Node").First(&rp, id).Error; err != nil {
		return nil, err
	}
	return &rp, nil
}

func (r *nodeRepository) UpdateReverseProxy(ctx context.Context, rp *domain.ReverseProxy) error {
	return r.db.WithContext(ctx).Save(rp).Error
}

func (r *nodeRepository) DeleteReverseProxy(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.ReverseProxy{}, id).Error
}

func (r *nodeRepository) ListReverseProxiesByNode(ctx context.Context, nodeID uint) ([]*domain.ReverseProxy, error) {
	var rps []*domain.ReverseProxy
	if err := r.db.WithContext(ctx).
		Where("node_id = ?", nodeID).
		Order("id ASC").
		Find(&rps).Error; err != nil {
		return nil, err
	}
	return rps, nil
}

func (r *nodeRepository) DeleteReverseProxiesByNode(ctx context.Context, nodeID uint) error {
	// Also clean up auto-generated routing rules referenced by these reverse proxies
	var rps []*domain.ReverseProxy
	if err := r.db.WithContext(ctx).Where("node_id = ?", nodeID).Find(&rps).Error; err == nil {
		for _, rp := range rps {
			if rp.Rule1ID != nil {
				r.db.WithContext(ctx).Delete(&domain.RoutingRule{}, *rp.Rule1ID)
			}
			if rp.Rule2ID != nil {
				r.db.WithContext(ctx).Delete(&domain.RoutingRule{}, *rp.Rule2ID)
			}
		}
	}
	return r.db.WithContext(ctx).Where("node_id = ?", nodeID).Delete(&domain.ReverseProxy{}).Error
}

func (r *nodeRepository) ListReverseProxiesByReferencedTag(ctx context.Context, nodeID uint, tag string) ([]*domain.ReverseProxy, error) {
	var rps []*domain.ReverseProxy
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND (interconnection_tag = ? OR outbound_tag = ? OR interconnection_tags LIKE ? OR inbound_tags LIKE ?)",
			nodeID, tag, tag, `%"`+tag+`"%`, `%"`+tag+`"%`).
		Find(&rps).Error; err != nil {
		return nil, err
	}
	return rps, nil
}

func (r *nodeRepository) ToggleOutboundDisabled(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&domain.Outbound{}).
		Where("id = ?", id).
		Update("is_disabled", gorm.Expr("NOT is_disabled")).Error
}

func (r *nodeRepository) ListRoutingRulesByOutboundTag(ctx context.Context, nodeID uint, tag string) ([]*domain.RoutingRule, error) {
	var rules []*domain.RoutingRule
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND outbound_tag = ?", nodeID, tag).
		Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}
