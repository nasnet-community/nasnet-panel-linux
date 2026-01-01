package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
	"gopkg.in/telebot.v3"
)

// CleanupDatabase performs a factory-reset of the database, wiping all user/transactional
// data while preserving infrastructure (nodes, inbounds, plans, settings, certificates)
// and admin accounts. Accounts are removed from active nodes via the provisioning system
// before their DB records are deleted.
func (u *adminUsecase) CleanupDatabase(ctx context.Context) (*CleanupResult, error) {
	log := logger.GetLogger()

	if u.db == nil {
		return nil, fmt.Errorf("database handle not available")
	}

	result := &CleanupResult{}

	// Phase 1: Remove accounts from nodes via provisioning system
	activeSubs, err := u.subRepo.ListAllActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list active subscriptions: %w", err)
	}

	if u.accountUC != nil {
		for _, sub := range activeSubs {
			if err := u.accountUC.ForceDeleteAccountsBySubscription(ctx, sub.ID); err != nil {
				// Log but continue — best effort removal
				log.WithField("sub_id", sub.ID).WithError(err).Warn("CleanupDatabase: failed to remove accounts")
			} else {
				result.AccountsRemoved++
			}
		}
	}

	// Phase 2: Bulk DB deletion in FK-safe order
	execAndCount := func(sql string, args ...interface{}) int64 {
		tx := u.db.Exec(sql, args...)
		if tx.Error != nil {
			log.WithError(tx.Error).WithField("query", sql).Warn("CleanupDatabase: SQL error")
			return 0
		}
		return tx.RowsAffected
	}

	// Delete orphan accounts (any remaining after phase 1)
	orphanAccounts := execAndCount("DELETE FROM accounts")
	result.AccountsRemoved += orphanAccounts

	// Delete subscriptions
	result.SubscriptionsDeleted = execAndCount("DELETE FROM subscriptions")

	// Delete non-admin users
	result.UsersDeleted = execAndCount("DELETE FROM users WHERE is_admin = false OR is_admin IS NULL")

	// Delete audit logs
	result.AuditLogsDeleted = execAndCount("DELETE FROM audit_logs")

	// Delete notification logs
	result.NotificationLogsDeleted = execAndCount("DELETE FROM notification_logs")

	// Delete old provisioning tasks (keep last 5 minutes for in-flight work)
	result.ProvisioningTasksDeleted = execAndCount(
		"DELETE FROM provisioning_tasks WHERE created_at < ?",
		time.Now().Add(-5*time.Minute),
	)

	// Delete user daily usage
	result.UserDailyUsageDeleted = execAndCount("DELETE FROM user_daily_usages")

	// Delete node stats
	result.NodeStatsDeleted = execAndCount("DELETE FROM node_stats")

	// Delete conversation sessions
	result.ConversationsDeleted = execAndCount("DELETE FROM conversation_sessions")

	return result, nil
}

// Broadcast
func (u *adminUsecase) BroadcastMessage(ctx context.Context, bot *telebot.Bot, message string, onlyActive bool) (*adminDomain.BroadcastResult, error) {
	var users []*userDomain.User
	var err error
	if onlyActive {
		users, err = u.userRepo.ListActiveSubscribers(ctx)
	} else {
		users, _, err = u.userRepo.ListAll(ctx, "", "", "", "", 0, 100000)
	}
	if err != nil {
		return nil, err
	}
	result := &adminDomain.BroadcastResult{TotalUsers: len(users)}
	for i, user := range users {
		recipient := &telebot.User{ID: user.TelegramID}
		_, err := bot.Send(recipient, message, telebot.ModeMarkdown)
		if err != nil {
			result.Failed++
		} else {
			result.Sent++
		}
		// Rate limit: ~25 messages/sec to stay within Telegram limits
		if (i+1)%25 == 0 {
			time.Sleep(time.Second)
		}
	}
	return result, nil
}

// GetInboundDetails retrieves details about the Xray inbound
// UPDATED: Iterates over all active nodes to get counts
func (u *adminUsecase) GetInboundDetails(ctx context.Context) (string, error) {
	nodes, err := u.nodeRepo.ListActiveNodes(ctx)
	if err != nil {
		return "", err
	}

	var report string
	totalUsers := int64(0)

	for _, node := range nodes {
		report += fmt.Sprintf("🖥 %s (%s)\n", node.Name, node.IP)
		target := fmt.Sprintf("%s:%d", node.IP, node.APIPort)

		for _, in := range node.Inbounds {
			count, err := u.grpcClient.GetInboundUsersCount(ctx, target, in.Tag)
			if err != nil {
				report += fmt.Sprintf("  ├ %s: ⚠️ Error (%s)\n", in.Tag, err.Error())
			} else {
				report += fmt.Sprintf("  ├ %s: %d users\n", in.Tag, count)
				totalUsers += count
			}
		}
		report += "\n"
	}

	if report == "" {
		return "No active nodes or inbounds found.", nil
	}

	report += fmt.Sprintf("📊 Total Active Users (In-Memory): %d", totalUsers)
	return report, nil
}

// DiscoverNodeInbounds triggers inbound discovery for a node
func (u *adminUsecase) DiscoverNodeInbounds(ctx context.Context, nodeID uint) ([]*nodeDomain.Inbound, error) {
	return u.nodeUC.DiscoverInbounds(ctx, nodeID)
}

// SyncNodeInbounds syncs a node's inbounds with live Xray config
func (u *adminUsecase) SyncNodeInbounds(ctx context.Context, nodeID uint) (*nodeUC.SyncResult, error) {
	return u.nodeUC.SyncInbounds(ctx, nodeID)
}

// Manual User Management

func (u *adminUsecase) AddUserToInbound(ctx context.Context, nodeID uint, inboundTag, email string) (*xray.User, string, error) {
	// Find the database inbound by node+tag
	dbInbound, err := u.nodeRepo.GetInboundByTagAndNode(ctx, nodeID, inboundTag)
	if err != nil {
		return nil, "", fmt.Errorf("inbound '%s' not found in database: %w", inboundTag, err)
	}

	// Generate UUID
	uuid := xray.GenerateUUID()

	// Determine flow for VLESS with Reality
	flow := ""
	/*
		if strings.ToLower(dbInbound.Protocol) == "vless" && dbInbound.Security == "reality" {
			flow = "xtls-rprx-vision"
		}
	*/

	// Use AccountCreator if available
	if u.accountUC != nil {
		encryption := ""
		if strings.ToLower(dbInbound.Protocol) == "vless" {
			encryption = dbInbound.GetVLESSSettingsOrDefault().Encryption
		}
		_, link, err := u.accountUC.CreateAccountManual(ctx, dbInbound.ID, email, uuid, flow, encryption)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create account: %w", err)
		}

		// Build xray.User response for backward compatibility
		var protocol xray.Protocol
		switch strings.ToLower(dbInbound.Protocol) {
		case "vmess":
			protocol = xray.ProtocolVMess
		case "vless":
			protocol = xray.ProtocolVLESS
		case "trojan":
			protocol = xray.ProtocolTrojan
		case "shadowsocks":
			protocol = xray.ProtocolShadowsocks
		default:
			protocol = xray.ProtocolVLESS
		}

		user := &xray.User{
			Email:    email,
			UUID:     uuid,
			Protocol: protocol,
			Level:    0,
			Flow:     flow,
		}

		return user, link, nil
	}

	// Fallback to old behavior if accountUC not injected
	return nil, "", fmt.Errorf("account service not available")
}

func (u *adminUsecase) GenerateCustomConfigLink(ctx context.Context, nodeID uint, inboundTag, email, uuid string) (string, error) {
	// Use AccountCreator if available
	if u.accountUC != nil {
		return u.accountUC.GetLinkByEmail(ctx, email)
	}

	// Fallback to old behavior
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return "", err
	}

	target := fmt.Sprintf("%s:%d", node.IP, node.APIPort)
	inbounds, err := u.grpcClient.ListInbounds(ctx, target, false)
	if err != nil {
		return "", fmt.Errorf("failed to fetch inbounds: %w", err)
	}

	var targetInbound *xray.InboundInfo
	for _, in := range inbounds {
		if in.Tag == inboundTag {
			targetInbound = in
			break
		}
	}
	if targetInbound == nil {
		return "", fmt.Errorf("inbound '%s' not found", inboundTag)
	}

	remark := fmt.Sprintf("Manual-%s-%s", node.Name, email)
	return xray.GenerateConfigLink(targetInbound, uuid, node.IP, remark)
}
