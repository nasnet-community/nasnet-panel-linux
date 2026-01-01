package usecase

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/analytics"
)

// GetUserUsagePattern returns hourly connection counts for a specific user.
func (u *adminUsecase) GetUserUsagePattern(ctx context.Context, userID uint, days int) ([]adminDomain.HourlyUsagePoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}

	// Get user's subscription emails
	subs, err := u.subRepo.ListByUserID(ctx, userID, 0, 1000)
	if err != nil {
		return nil, err
	}

	to := time.Now()
	from := to.AddDate(0, 0, -days)

	// Aggregate hourly counts across all user emails
	hourCounts := make([]int64, 24)

	for _, sub := range subs {
		if sub.ConfigEmail == "" {
			continue
		}

		filter := nodeRepo.AccessLogSummaryFilter{
			Email: sub.ConfigEmail,
			From:  from,
			To:    to,
		}

		aggregates, err := u.nodeRepo.GetHourlyAggregates(ctx, filter)
		if err != nil {
			continue
		}

		for _, agg := range aggregates {
			if agg.Hour >= 0 && agg.Hour < 24 {
				hourCounts[agg.Hour] += agg.Accepted + agg.Rejected
			}
		}
	}

	points := make([]adminDomain.HourlyUsagePoint, 24)
	for h := 0; h < 24; h++ {
		points[h] = adminDomain.HourlyUsagePoint{
			Hour:  h,
			Count: hourCounts[h],
		}
	}

	return points, nil
}

// GetPeakHours returns aggregated connection stats by hour of day across all nodes.
func (u *adminUsecase) GetPeakHours(ctx context.Context, days int, nodeIDs []uint) ([]adminDomain.PeakHourPoint, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}

	to := time.Now()
	from := to.AddDate(0, 0, -days)

	filter := nodeRepo.AccessLogSummaryFilter{
		NodeIDs: nodeIDs,
		From:    from,
		To:      to,
	}

	aggregates, err := u.nodeRepo.GetHourlyAggregates(ctx, filter)
	if err != nil {
		return nil, err
	}

	points := make([]adminDomain.PeakHourPoint, len(aggregates))
	for i, agg := range aggregates {
		points[i] = adminDomain.PeakHourPoint{
			Hour:        agg.Hour,
			Connections: agg.Accepted,
			Rejected:    agg.Rejected,
			UniqueUsers: agg.UniqueUsers,
			TcpCount:    agg.TcpCount,
			UdpCount:    agg.UdpCount,
		}
	}

	return points, nil
}

// GetBlockedDomainStats returns aggregated blocked domain statistics from access log summaries.
func (u *adminUsecase) GetBlockedDomainStats(ctx context.Context, days int, nodeIDs []uint, topN int) (*adminDomain.BlockedDomainSummary, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	if topN <= 0 {
		topN = 20
	}

	to := time.Now()
	from := to.AddDate(0, 0, -days)

	filter := nodeRepo.AccessLogSummaryFilter{
		NodeIDs: nodeIDs,
		From:    from,
		To:      to,
		Limit:   10000,
	}

	summaries, _, err := u.nodeRepo.GetAccessLogSummaries(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Aggregate blocked domain counts
	type domainInfo struct {
		count    int64
		nodes    map[uint]bool
		lastSeen time.Time
	}

	domainMap := make(map[string]*domainInfo)
	var totalRejected, totalAccepted int64

	for _, s := range summaries {
		totalAccepted += s.AcceptedCount
		totalRejected += s.RejectedCount

		// Parse rejected domains JSON
		if s.RejectedDomains == "" || s.RejectedDomains == "null" {
			continue
		}
		var domains map[string]int64
		if err := json.Unmarshal([]byte(s.RejectedDomains), &domains); err != nil {
			continue
		}
		for d, c := range domains {
			info, ok := domainMap[d]
			if !ok {
				info = &domainInfo{nodes: make(map[uint]bool)}
				domainMap[d] = info
			}
			info.count += c
			info.nodes[s.NodeID] = true
			if s.HourTime.After(info.lastSeen) {
				info.lastSeen = s.HourTime
			}
		}
	}

	// Sort by count and take top N
	type kv struct {
		domain string
		info   *domainInfo
	}
	pairs := make([]kv, 0, len(domainMap))
	for d, info := range domainMap {
		pairs = append(pairs, kv{d, info})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].info.count > pairs[j].info.count })

	if len(pairs) > topN {
		pairs = pairs[:topN]
	}

	domains := make([]adminDomain.BlockedDomainStat, len(pairs))
	for i, p := range pairs {
		domains[i] = adminDomain.BlockedDomainStat{
			Domain:        p.domain,
			RejectedCount: p.info.count,
			NodeCount:     len(p.info.nodes),
			LastSeen:      p.info.lastSeen.Format(time.RFC3339),
		}
	}

	var rejectionRate float64
	totalConn := totalAccepted + totalRejected
	if totalConn > 0 {
		rejectionRate = math.Round(float64(totalRejected)/float64(totalConn)*10000) / 100
	}

	return &adminDomain.BlockedDomainSummary{
		Domains:       domains,
		TotalRejected: totalRejected,
		TotalAccepted: totalAccepted,
		RejectionRate: rejectionRate,
		PeriodFrom:    from.Format("2006-01-02"),
		PeriodTo:      to.Format("2006-01-02"),
	}, nil
}

// GetExhaustionPrediction computes a data exhaustion forecast for a subscription.
func (u *adminUsecase) GetExhaustionPrediction(ctx context.Context, subID uint) (*adminDomain.ExhaustionPrediction, error) {
	sub, err := u.subRepo.FindByID(ctx, subID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)
	to := now.Truncate(24 * time.Hour)

	records, err := u.subRepo.ListDailyUsage(ctx, subID, from, to)
	if err != nil {
		return nil, err
	}

	pred := analytics.ComputeExhaustion(sub, records)

	return &adminDomain.ExhaustionPrediction{
		SubscriptionID:   sub.ID,
		Label:            sub.Label,
		DataLimit:        pred.DataLimit,
		DataUsed:         pred.DataUsed,
		DataRemaining:    pred.DataRemaining,
		DailyAvgBytes:    pred.DailyAvgBytes,
		DaysRemaining:    pred.DaysRemaining,
		EndDate:          pred.EndDate,
		DaysUntilExpiry:  pred.DaysUntilExpiry,
		ExhaustionDate:   pred.ExhaustionDate,
		WillExhaustFirst: pred.WillExhaustFirst,
		UsageTrend:       pred.UsageTrend,
		Confidence:       pred.Confidence,
		Unlimited:        pred.Unlimited,
	}, nil
}
