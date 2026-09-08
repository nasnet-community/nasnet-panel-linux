package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	accountRepo "github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// A gate covers collection through acknowledgement, not just database writes:
// two manual/scheduled readers of one agent would otherwise persist the same
// buffered snapshot. Idle gates are removed so deleted nodes do not accumulate.
type nodeStatsSyncGate struct {
	token chan struct{}
	users int
}

func (u *nodeUsecase) acquireStatsSync(ctx context.Context, nodeID uint) (func(), error) {
	u.statsSyncMu.Lock()
	if u.statsSyncs == nil {
		u.statsSyncs = make(map[uint]*nodeStatsSyncGate)
	}
	gate := u.statsSyncs[nodeID]
	if gate == nil {
		gate = &nodeStatsSyncGate{token: make(chan struct{}, 1)}
		u.statsSyncs[nodeID] = gate
	}
	gate.users++
	u.statsSyncMu.Unlock()
	releaseRef := func() {
		u.statsSyncMu.Lock()
		gate.users--
		if gate.users == 0 {
			delete(u.statsSyncs, nodeID)
		}
		u.statsSyncMu.Unlock()
	}
	select {
	case gate.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate.token
			releaseRef()
			return nil, err
		}
		return func() { <-gate.token; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}

type trafficBytes struct{ up, down int64 }
type trafficDayEmail struct {
	day   time.Time
	email string
}
type trafficAccountKey struct{ sub, inbound uint }

// persistBufferedTraffic resolves attribution before taking a transaction, then
// applies all counters atomically in bounded SQL batches. No agent RPC, online-IP
// update, system-stat write or access-log work runs inside this transaction.
//
// The legacy agent protocol exports mutable buckets and acknowledges timestamps.
// A timestamp may be reused after a drain, so it is NOT an ingestion identity.
// This function deliberately does not persist a timestamp-only dedup watermark.
func (u *nodeUsecase) persistBufferedTraffic(ctx context.Context, node *domain.Node, records []*agent.TrafficRecord) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}
	if u.tm == nil {
		return 0, fmt.Errorf("traffic transaction manager is not configured")
	}
	users := make(map[string]trafficBytes)
	dailyUsers := make(map[trafficDayEmail]trafficBytes)
	dailyNode := make(map[time.Time]trafficBytes)
	outbounds := make(map[string]trafficBytes)
	activeInbounds := make(map[string]bool)
	var nodeBytes trafficBytes
	var through int64
	for _, record := range records {
		if record == nil || record.Timestamp <= 0 {
			return 0, fmt.Errorf("invalid buffered traffic timestamp")
		}
		if record.Timestamp > through {
			through = record.Timestamp
		}
		day := time.Unix(record.Timestamp, 0).UTC().Truncate(24 * time.Hour)
		for email, bytes := range record.UserUplink {
			b := users[email]
			b.up += bytes
			users[email] = b
			k := trafficDayEmail{day, email}
			b = dailyUsers[k]
			b.up += bytes
			dailyUsers[k] = b
		}
		for email, bytes := range record.UserDownlink {
			b := users[email]
			b.down += bytes
			users[email] = b
			k := trafficDayEmail{day, email}
			b = dailyUsers[k]
			b.down += bytes
			dailyUsers[k] = b
		}
		for tag, bytes := range record.InboundUplink {
			if bytes > 0 {
				activeInbounds[tag] = true
			}
		}
		for tag, bytes := range record.InboundDownlink {
			if bytes > 0 {
				activeInbounds[tag] = true
			}
		}
		if record.TotalUplink > 0 || record.TotalDownlink > 0 {
			nodeBytes.up += record.TotalUplink
			nodeBytes.down += record.TotalDownlink
			b := dailyNode[day]
			b.up += record.TotalUplink
			b.down += record.TotalDownlink
			dailyNode[day] = b
		}
		for tag, bytes := range record.OutboundUplink {
			if bytes > 0 {
				b := outbounds[tag]
				b.up += bytes
				outbounds[tag] = b
			}
		}
		for tag, bytes := range record.OutboundDownlink {
			if bytes > 0 {
				b := outbounds[tag]
				b.down += bytes
				outbounds[tag] = b
			}
		}
	}
	emails := make([]string, 0, len(users))
	for email := range users {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	subs, err := u.subRepo.FindByConfigEmails(ctx, emails)
	if err != nil {
		return 0, fmt.Errorf("resolve traffic subscriptions: %w", err)
	}

	matchedByEmail := make(map[string][]uint)
	activeInboundIDs := make(map[uint]bool)
	for _, in := range node.Inbounds {
		if activeInbounds[in.Tag] {
			activeInboundIDs[in.ID] = true
		}
	}
	accountsBySub := make(map[trafficAccountKey]accountRepo.AccountTrafficRef)
	if len(users) > 0 && u.accountRepo != nil && len(node.Inbounds) > 0 {
		refs, err := u.accountRepo.ListTrafficRefsByNode(ctx, node.ID)
		if err != nil {
			return 0, fmt.Errorf("resolve traffic accounts: %w", err)
		}
		for _, ref := range refs {
			if activeInboundIDs[ref.InboundID] {
				matchedByEmail[ref.Email] = append(matchedByEmail[ref.Email], ref.ID)
			}
			key := trafficAccountKey{ref.SubscriptionID, ref.InboundID}
			// Match ListBySubscriptionID's newest WG account without sorting the
			// entire node's account projection on every stats pass.
			previous := accountsBySub[key]
			newer := previous.ID == 0 || ref.CreatedAt.After(previous.CreatedAt) || (ref.CreatedAt.Equal(previous.CreatedAt) && ref.ID > previous.ID)
			if ref.SubscriptionID != 0 && ref.Source != "admin_excluded" && newer {
				accountsBySub[key] = ref
			}
		}
	}
	wgIndex := make(map[string]wgRef)
	if u.wgPeerSource != nil {
		peers := make(map[string][]WGRenderPeer)
		for _, in := range node.Inbounds {
			if !strings.EqualFold(in.Protocol, "wireguard") {
				continue
			}
			// Do not enumerate every WG peer when this batch has no WG traffic.
			prefix := "wg:" + in.Tag + ":"
			needed := false
			for email := range users {
				if strings.HasPrefix(email, prefix) {
					needed = true
					break
				}
			}
			if !needed {
				continue
			}
			peers[in.Tag], err = u.wgPeerSource.ActivePeersByInbound(ctx, in.ID)
			if err != nil {
				return 0, fmt.Errorf("resolve WireGuard traffic peers: %w", err)
			}
		}
		wgIndex = buildWGIndex(peers)
	}
	now := time.Now()
	var subDeltas []subRepo.UsageDelta
	var dailyDeltas []subRepo.DailyUsageDelta
	var accountDeltas []accountRepo.UsageDelta
	var peerDeltas []contract.WGUsageDelta
	for k, b := range dailyUsers {
		if sub := subs[k.email]; sub != nil && (b.up != 0 || b.down != 0) {
			dailyDeltas = append(dailyDeltas, subRepo.DailyUsageDelta{SubscriptionID: sub.ID, Date: k.day, Upload: b.up, Download: b.down})
		}
	}
	for _, email := range emails {
		b := users[email]
		total := b.up + b.down
		if total <= 0 {
			continue
		}
		if ref, ok := wgIndex[email]; ok {
			subDeltas = append(subDeltas, subRepo.UsageDelta{SubscriptionID: ref.SubID, Upload: b.up, Download: b.down, LastActive: now})
			peerDeltas = append(peerDeltas, contract.WGUsageDelta{PeerID: ref.PeerID, Upload: b.up, Download: b.down, LastSeen: now})
			if id := accountsBySub[trafficAccountKey{ref.SubID, ref.InboundID}].ID; id != 0 {
				accountDeltas = append(accountDeltas, accountRepo.UsageDelta{AccountID: id, Bytes: total, LastActive: now})
			}
			// Preserve legacy WG daily-attribution behavior: synthetic emails do not
			// have a config-email subscription row, so no synthetic daily split is added.
			continue
		}
		sub := subs[email]
		if sub == nil {
			continue
		} // Unknown/deleted users were not attributable before either.
		subDeltas = append(subDeltas, subRepo.UsageDelta{SubscriptionID: sub.ID, Upload: b.up, Download: b.down, LastActive: now})
		matched := matchedByEmail[email]
		if len(matched) > 0 {
			share := total / int64(len(matched))
			for _, id := range matched {
				accountDeltas = append(accountDeltas, accountRepo.UsageDelta{AccountID: id, Bytes: share, LastActive: now})
			}
		}

	}
	days := make([]time.Time, 0, len(dailyNode))
	for d := range dailyNode {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	tags := make([]string, 0, len(outbounds))
	for tag := range outbounds {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	err = u.tm.Do(ctx, func(txCtx context.Context) error {
		if nodeBytes.up > 0 || nodeBytes.down > 0 {
			if err := u.nodeRepo.AddNodeTraffic(txCtx, node.ID, nodeBytes.up, nodeBytes.down); err != nil {
				return fmt.Errorf("node traffic: %w", err)
			}
		}
		for _, day := range days {
			b := dailyNode[day]
			if err := u.nodeRepo.AddNodeDailyTraffic(txCtx, node.ID, day, b.up, b.down); err != nil {
				return fmt.Errorf("node daily traffic: %w", err)
			}
		}
		for _, tag := range tags {
			b := outbounds[tag]
			if err := u.nodeRepo.AddOutboundTraffic(txCtx, node.ID, tag, b.up, b.down); err != nil {
				return fmt.Errorf("outbound traffic: %w", err)
			}
		}
		if len(dailyDeltas) > 0 {
			if err := u.subRepo.AddDailyUsageSplits(txCtx, dailyDeltas); err != nil {
				return fmt.Errorf("subscription daily traffic: %w", err)
			}
		}
		if len(subDeltas) > 0 {
			if err := u.subRepo.AddUsageDeltas(txCtx, subDeltas); err != nil {
				return fmt.Errorf("subscription traffic: %w", err)
			}
		}
		if len(accountDeltas) > 0 {
			if err := u.accountRepo.AddUsageDeltas(txCtx, accountDeltas); err != nil {
				return fmt.Errorf("account traffic: %w", err)
			}
		}
		if len(peerDeltas) > 0 {
			if err := u.wgPeerSource.AddPeerUsageBatch(txCtx, peerDeltas); err != nil {
				return fmt.Errorf("WireGuard peer traffic: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return through, nil
}
