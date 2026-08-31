package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gopkg.in/telebot.v3"
)

// Subscription Management
func (u *adminUsecase) ListAllSubscriptions(ctx context.Context, status string, offset, limit int) ([]*subDomain.Subscription, error) {
	return u.subRepo.ListAll(ctx, status, offset, limit)
}

func (u *adminUsecase) ExtendSubscription(ctx context.Context, id uint, days int) error {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if sub.Status == subDomain.SubscriptionStatusExpired || sub.Status == subDomain.SubscriptionStatusActive {
		if err := u.subRepo.ExtendDays(ctx, id, days); err != nil {
			return err
		}
		// An expired sub's access was revoked on expiry; restore it here or the
		// extension only moves the date and the user stays cut off.
		u.setTunnelAccess(ctx, id, true)
		return u.subRepo.UpdateStatus(ctx, id, subDomain.SubscriptionStatusActive)
	}

	return u.subRepo.ExtendDays(ctx, id, days)
}

func (u *adminUsecase) GetSubscription(ctx context.Context, id uint) (*subDomain.Subscription, error) {
	return u.subRepo.FindByID(ctx, id)
}
func (u *adminUsecase) GetSubscriptionsByUser(ctx context.Context, userID uint) ([]*subDomain.Subscription, error) {
	return u.subRepo.ListByUserID(ctx, userID, 0, 100)
}

func (u *adminUsecase) RevokeSubscription(ctx context.Context, id uint) error {
	// Revokes WireGuard peers as well as Xray accounts; going through accountUC
	// alone left a subscription's peers live after a revoke.
	u.setTunnelAccess(ctx, id, false)

	return u.subRepo.UpdateStatus(ctx, id, subDomain.SubscriptionStatusCancelled)
}

func (u *adminUsecase) PauseSubscription(ctx context.Context, id uint) error {
	u.setTunnelAccess(ctx, id, false)
	return u.subRepo.UpdateStatus(ctx, id, subDomain.SubscriptionStatusPaused)
}

func (u *adminUsecase) ResumeSubscription(ctx context.Context, id uint) error {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if sub.IsExpired() {
		return errors.New("cannot resume expired subscription")
	}

	u.setTunnelAccess(ctx, id, true)

	return u.subRepo.UpdateStatus(ctx, id, subDomain.SubscriptionStatusActive)
}

func (u *adminUsecase) RegenerateSubscriptionKey(ctx context.Context, id uint, customKey string) (*subDomain.Subscription, error) {
	return u.subUC.RegenerateSubscriptionKey(ctx, id, customKey)
}

func (u *adminUsecase) RegenerateSubscriptionUUID(ctx context.Context, id uint) (*subDomain.Subscription, error) {
	return u.subUC.RegenerateUUID(ctx, id)
}

// SetSubscriptionUUID atomically applies a specific UUID to every account under
// the subscription. Used by the admin panel when an operator wants to pin a
// known UUID across all accounts (replaces the prior N-roundtrip client loop).
// Returns the number of accounts updated. A non-nil error means at least one
// account update failed; already-updated accounts remain with the new UUID.
func (u *adminUsecase) SetSubscriptionUUID(ctx context.Context, id uint, newUUID string) (int, error) {
	if u.accountUC == nil {
		return 0, fmt.Errorf("account manager not initialised")
	}
	if _, err := u.subRepo.FindByID(ctx, id); err != nil {
		return 0, err
	}
	return u.accountUC.SetAccountsUUIDBySubscription(ctx, id, newUUID)
}

func (u *adminUsecase) GetSubscriptionLink(ctx context.Context, id uint) (string, error) {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	return sub.SubLink, nil
}

func (u *adminUsecase) ResetDataUsage(ctx context.Context, id uint) error {
	return u.subRepo.ResetDataUsed(ctx, id)
}

func (u *adminUsecase) SetDataUsage(ctx context.Context, id uint, bytesUsed int64) error {
	return u.subRepo.UpdateDataUsed(ctx, id, bytesUsed)
}

// DeleteSubscription permanently removes a subscription from the database
func (u *adminUsecase) DeleteSubscription(ctx context.Context, id uint) error {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	_ = sub
	// Delete associated account records removes the user from every xray node

	if u.accountUC != nil {
		if err := u.accountUC.ForceDeleteAccountsBySubscription(ctx, id); err != nil {
			return fmt.Errorf("failed to delete accounts: %w", err)
		}
	}

	// Hard delete from database
	return u.subRepo.HardDelete(ctx, id)
}

// BulkSubscriptionAction performs an action on multiple subscriptions
func (u *adminUsecase) BulkSubscriptionAction(ctx context.Context, action string, ids []uint) (*BulkActionResult, error) {
	if len(ids) > 100 {
		return nil, fmt.Errorf("maximum 100 IDs per bulk operation")
	}

	result := &BulkActionResult{}
	for _, id := range ids {
		var err error
		switch action {
		case "delete":
			err = u.DeleteSubscription(ctx, id)
		case "pause":
			err = u.PauseSubscription(ctx, id)
		case "resume":
			err = u.ResumeSubscription(ctx, id)
		case "revoke":
			err = u.RevokeSubscription(ctx, id)
		default:
			return nil, fmt.Errorf("unknown action: %s", action)
		}
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("ID %d: %s", id, err.Error()))
		} else {
			result.Succeeded++
		}
	}
	return result, nil
}

// BulkSetBandwidthLimit sets a custom bandwidth limit on multiple subscriptions.
// Pass nil to reset all selected subscriptions to their plan default.
func (u *adminUsecase) BulkSetBandwidthLimit(ctx context.Context, ids []uint, limitMbps *int) (*BulkActionResult, error) {
	if len(ids) > 100 {
		return nil, fmt.Errorf("maximum 100 IDs per bulk operation")
	}

	result := &BulkActionResult{}
	for _, id := range ids {
		if err := u.subUC.SetCustomBandwidthLimit(ctx, id, limitMbps); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("ID %d: %s", id, err.Error()))
		} else {
			result.Succeeded++
		}
	}
	return result, nil
}

// CountAllSubscriptions returns total subscription count
func (u *adminUsecase) CountAllSubscriptions(ctx context.Context) (int64, error) {
	return u.subRepo.CountAll(ctx)
}

// CountSubscriptionsByStatus returns count of subscriptions with given status
func (u *adminUsecase) CountSubscriptionsByStatus(ctx context.Context, status string) (int64, error) {
	return u.subRepo.CountByStatus(ctx, status)
}

func (u *adminUsecase) ListAllFilteredSubscriptions(ctx context.Context, filter subRepo.SubscriptionFilter) ([]*subDomain.Subscription, int64, error) {
	return u.subRepo.ListAllFiltered(ctx, filter)
}

func (u *adminUsecase) CreateManualSubscription(ctx context.Context, req *subUC.ManualSubscriptionRequest) (*subDomain.Subscription, error) {
	sub, err := u.subUC.CreateManual(ctx, req)
	if err != nil {
		return nil, err
	}

	// Send notification if user is linked and has valid TelegramID
	if req.UserID != nil && u.bot != nil {
		user, _ := u.userRepo.FindByID(ctx, *req.UserID)
		if user != nil && user.TelegramID > 0 {
			endDateStr := "No Expiry"
			if sub.EndDate != nil {
				endDateStr = sub.EndDate.Format("Jan 2, 2006")
			}
			dataStr := "Unlimited"
			if sub.DataLimit > 0 {
				gb := float64(sub.DataLimit) / (1024 * 1024 * 1024)
				dataStr = fmt.Sprintf("%.1f GB", gb)
			}
			msg := fmt.Sprintf("🎉 *New Subscription Created!*\n\nA subscription has been created for your account.\n\n📋 Label: %s\n📊 Data: %s\n📅 Expires: %s\n\nUse /mysubs to view your subscriptions.", sub.Label, dataStr, endDateStr)
			_, _ = u.bot.Send(&telebot.User{ID: user.TelegramID}, msg, telebot.ModeMarkdown)
		}
	}

	return sub, nil
}

// Subscription Data/Expiry Management

func (u *adminUsecase) SetSubscriptionBandwidthLimit(ctx context.Context, id uint, limitMbps *int) error {
	return u.subUC.SetCustomBandwidthLimit(ctx, id, limitMbps)
}

func (u *adminUsecase) SetSubscriptionDataLimit(ctx context.Context, id uint, limitGB *float64) error {
	return u.subUC.SetCustomDataLimit(ctx, id, limitGB)
}

func (u *adminUsecase) SetSubscriptionMaxDevices(ctx context.Context, id uint, maxDevices int) error {
	return u.subUC.SetMaxDevices(ctx, id, maxDevices)
}

func (u *adminUsecase) SetSubscriptionEndDate(ctx context.Context, id uint, endDate *time.Time, unlimited bool) (*subDomain.Subscription, error) {
	if unlimited {
		return u.subUC.SetCustomEndDate(ctx, id, nil, true)
	}
	return u.subUC.SetCustomEndDate(ctx, id, endDate, endDate != nil)
}

func (u *adminUsecase) RenameSubscription(ctx context.Context, id uint, label string) error {
	return u.subUC.RenameSubscription(ctx, id, label)
}

func (u *adminUsecase) SetSubscriptionPanelPassword(ctx context.Context, id uint, mode, password string) error {
	return u.subUC.SetPanelPassword(ctx, id, mode, password)
}

func (u *adminUsecase) AddSubscriptionData(ctx context.Context, id uint, additionalGB float64) error {
	return u.subUC.AddData(ctx, id, additionalGB)
}

func (u *adminUsecase) ResetSubscriptionData(ctx context.Context, id uint) error {
	return u.subUC.ResetDataUsed(ctx, id)
}

func (u *adminUsecase) GetSubscriptionUsageDetails(ctx context.Context, id uint) (*subUC.SubscriptionUsageDetails, error) {
	return u.subUC.GetUsageDetails(ctx, id)
}

func (u *adminUsecase) GetSubscriptionUsageHistory(ctx context.Context, id uint, days int) ([]subUC.UsageHistoryPoint, error) {
	return u.subUC.GetSubscriptionUsageHistory(ctx, id, days)
}

// AssignSubscriptionUser assigns or unlinks a user from a subscription
func (u *adminUsecase) AssignSubscriptionUser(ctx context.Context, subID uint, userID *uint) (*subDomain.Subscription, error) {
	// Validate subscription exists
	sub, err := u.subRepo.FindByID(ctx, subID)
	if err != nil {
		return nil, fmt.Errorf("subscription not found: %w", err)
	}

	// Validate user if linking
	if userID != nil {
		if _, err := u.userRepo.FindByID(ctx, *userID); err != nil {
			return nil, fmt.Errorf("user not found: %w", err)
		}
	}

	// Update user_id
	if err := u.subRepo.SetUserID(ctx, subID, userID); err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	// Send notification if user has valid TelegramID
	if userID != nil && u.bot != nil {
		user, _ := u.userRepo.FindByID(ctx, *userID)
		if user != nil && user.TelegramID > 0 {
			endDateStr := "No Expiry"
			if sub.EndDate != nil {
				endDateStr = sub.EndDate.Format("Jan 2, 2006")
			}
			dataStr := "Unlimited"
			if sub.DataLimit > 0 {
				gb := float64(sub.DataLimit) / (1024 * 1024 * 1024)
				dataStr = fmt.Sprintf("%.1f GB", gb)
			}
			msg := fmt.Sprintf("🎉 *New Subscription Assigned!*\n\nA subscription has been linked to your account.\n\n📋 Label: %s\n📊 Data: %s\n📅 Expires: %s\n\nUse /mysubs to view your subscriptions.", sub.Label, dataStr, endDateStr)
			_, _ = u.bot.Send(&telebot.User{ID: user.TelegramID}, msg, telebot.ModeMarkdown)
		}
	}

	// Re-fetch to return updated data
	return u.subRepo.FindByID(ctx, subID)
}

// GetBulkInboundSummary returns a mapping of inbound ID to the number of
// selected subscriptions that have an active account on that inbound.
func (u *adminUsecase) GetBulkInboundSummary(ctx context.Context, subscriptionIDs []uint) (*BulkInboundSummary, error) {
	if len(subscriptionIDs) == 0 {
		return &BulkInboundSummary{InboundCounts: map[uint]int{}, TotalSubscriptions: 0}, nil
	}
	type row struct {
		InboundID uint
		Count     int
	}
	var rows []row
	err := u.db.WithContext(ctx).
		Table("accounts").
		Select("inbound_id, COUNT(DISTINCT subscription_id) as count").
		Where("subscription_id IN ?", subscriptionIDs).
		Where("status NOT IN ?", []string{"disabled", "pending_removal"}).
		Where("deleted_at IS NULL").
		Group("inbound_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query inbound summary: %w", err)
	}
	counts := make(map[uint]int, len(rows))
	for _, r := range rows {
		counts[r.InboundID] = r.Count
	}
	return &BulkInboundSummary{InboundCounts: counts, TotalSubscriptions: len(subscriptionIDs)}, nil
}

// BulkManageInbounds adds or removes inbound accounts across multiple subscriptions.
// For adds: creates accounts with source=admin_bulk and enqueues provisioning.
// For removes: marks accounts for removal and enqueues deprovisioning.
func (u *adminUsecase) BulkManageInbounds(ctx context.Context, subscriptionIDs []uint, addInboundIDs, removeInboundIDs []uint) (*BulkInboundResult, error) {
	log := logger.GetLogger()

	if len(subscriptionIDs) == 0 {
		return &BulkInboundResult{}, nil
	}

	// Validate no overlap between add and remove lists
	addSet := make(map[uint]bool, len(addInboundIDs))
	for _, id := range addInboundIDs {
		addSet[id] = true
	}
	for _, id := range removeInboundIDs {
		if addSet[id] {
			return nil, fmt.Errorf("inbound ID %d appears in both add and remove lists", id)
		}
	}

	// Preload inbound info for adds (need protocol/flow/encryption and node for provisioning)
	allInboundIDs := make([]uint, 0, len(addInboundIDs)+len(removeInboundIDs))
	allInboundIDs = append(allInboundIDs, addInboundIDs...)
	allInboundIDs = append(allInboundIDs, removeInboundIDs...)

	inboundMap := make(map[uint]*nodeDomain.Inbound)
	if len(allInboundIDs) > 0 {
		inbounds, err := u.nodeRepo.FindInboundsByIDs(ctx, allInboundIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to load inbounds: %w", err)
		}
		for _, in := range inbounds {
			inboundMap[in.ID] = in
		}
	}

	result := &BulkInboundResult{}

	for _, subID := range subscriptionIDs {
		subIDCopy := subID
		affected := false

		// Get all accounts for this subscription, including admin_excluded ones for reactivation
		existingAccounts, err := u.accountRepo.ListAllBySubscriptionID(ctx, subID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("sub %d: failed to list accounts: %s", subID, err.Error()))
			continue
		}

		// Build lookup of existing accounts by inbound ID
		accountByInbound := make(map[uint]*accountDomain.Account, len(existingAccounts))
		for _, acc := range existingAccounts {
			accountByInbound[acc.InboundID] = acc
		}

		// Determine UUID from first existing account (all accounts for a subscription share the same UUID)
		var subUUID string
		for _, acc := range existingAccounts {
			if acc.UUID != "" {
				subUUID = acc.UUID
				break
			}
		}
		if subUUID == "" {
			// Fallback: load subscription to get UUID
			sub, subErr := u.subRepo.FindByID(ctx, subID)
			if subErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("sub %d: failed to load subscription: %s", subID, subErr.Error()))
				continue
			}
			subUUID = sub.ConfigID
		}

		// Determine email from first existing account
		var subEmail string
		for _, acc := range existingAccounts {
			if acc.Email != "" {
				subEmail = acc.Email
				break
			}
		}
		if subEmail == "" {
			// Fallback: load subscription to get email
			sub, subErr := u.subRepo.FindByID(ctx, subID)
			if subErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("sub %d: failed to load subscription: %s", subID, subErr.Error()))
				continue
			}
			subEmail = sub.ConfigEmail
		}
		if subEmail == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("sub %d: cannot determine email", subID))
			continue
		}

		// === ADD LOOP ===
		for _, inboundID := range addInboundIDs {
			inbound, ok := inboundMap[inboundID]
			if !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("sub %d: inbound %d not found", subID, inboundID))
				continue
			}

			existing, exists := accountByInbound[inboundID]

			if exists && existing.Source == accountDomain.AccountSourceAdminExcluded {
				// Reactivate: was previously excluded by admin
				existing.Source = accountDomain.AccountSourceAdminBulk
				existing.Status = accountDomain.AccountStatusActive
				if err := u.accountRepo.Update(ctx, existing); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("sub %d: failed to reactivate account on inbound %d: %s", subID, inboundID, err.Error()))
					continue
				}
				// Enqueue provisioning
				if inbound.Node != nil {
					existing.Inbound = inbound
					target := fmt.Sprintf("%s:%d", inbound.Node.IP, inbound.Node.APIPort)
					if provErr := u.provService.EnqueueAddUser(ctx, existing, target); provErr != nil {
						log.WithError(provErr).Warnf("BulkManageInbounds: failed to enqueue provision for reactivated account %s on inbound %d", existing.Email, inboundID)
					}
				}
				result.AccountsAdded++
				affected = true
				continue
			}

			if exists {
				// Already has an active (or other non-excluded) account on this inbound
				result.Skipped++
				continue
			}

			// Derive flow and encryption from inbound protocol settings
			flow := ""
			encryption := ""
			protocol := strings.ToLower(inbound.Protocol)
			switch protocol {
			case "vless":
				vless := inbound.GetVLESSSettingsOrDefault()
				encryption = vless.Encryption
				if strings.Contains(inbound.LinkFormat, "xtls-rprx-vision") {
					flow = "xtls-rprx-vision"
				}
			case "vmess":
				encryption = "auto"
			}

			// Check for soft-deleted account with same email+inbound (unique constraint)
			existingDeleted, findErr := u.accountRepo.FindByEmailAndInbound(ctx, subEmail, inboundID)
			if findErr == nil && existingDeleted != nil {
				// Should not happen (scoped query), but guard against it
				result.Skipped++
				continue
			}

			// Create new account
			account := &accountDomain.Account{
				InboundID:      inboundID,
				Email:          subEmail,
				UUID:           subUUID,
				Flow:           flow,
				Encryption:     encryption,
				Source:         accountDomain.AccountSourceAdminBulk,
				SubscriptionID: &subIDCopy,
				Status:         accountDomain.AccountStatusActive,
				Inbound:        inbound,
			}

			// Check for soft-deleted account holding the unique constraint
			if deletedAcc, _ := u.accountRepo.FindByEmailAndInboundUnscoped(ctx, subEmail, inboundID); deletedAcc != nil && deletedAcc.DeletedAt.Valid {
				_ = u.accountRepo.ForceDelete(ctx, deletedAcc.ID)
			}

			if err := u.accountRepo.Create(ctx, account); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("sub %d: failed to create account on inbound %d: %s", subID, inboundID, err.Error()))
				continue
			}

			// Enqueue provisioning
			if inbound.Node != nil {
				target := fmt.Sprintf("%s:%d", inbound.Node.IP, inbound.Node.APIPort)
				if provErr := u.provService.EnqueueAddUser(ctx, account, target); provErr != nil {
					log.WithError(provErr).Warnf("BulkManageInbounds: failed to enqueue provision for account %s on inbound %d", subEmail, inboundID)
					account.Status = accountDomain.AccountStatusPendingProvision
					_ = u.accountRepo.Update(ctx, account)
				}
			}
			result.AccountsAdded++
			affected = true
		}

		// === REMOVE LOOP ===
		for _, inboundID := range removeInboundIDs {
			existing, exists := accountByInbound[inboundID]

			if !exists {
				result.Skipped++
				continue
			}

			// Skip only if already admin_excluded (already removed by admin)
			if existing.Source == accountDomain.AccountSourceAdminExcluded {
				result.Skipped++
				continue
			}

			inbound, ok := inboundMap[inboundID]

			// Mark as admin_excluded so the ADD path can reactivate it,
			// and the accounts query filters it out from the UI.
			existing.Source = accountDomain.AccountSourceAdminExcluded
			existing.Status = accountDomain.AccountStatusDisabled

			if err := u.accountRepo.Update(ctx, existing); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("sub %d: failed to update account on inbound %d: %s", subID, inboundID, err.Error()))
				continue
			}

			// Enqueue deprovisioning
			if ok && inbound.Node != nil {
				existing.Inbound = inbound
				target := fmt.Sprintf("%s:%d", inbound.Node.IP, inbound.Node.APIPort)
				if provErr := u.provService.EnqueueRemoveUser(ctx, existing, target); provErr != nil {
					log.WithError(provErr).Warnf("BulkManageInbounds: failed to enqueue deprovision for account %s on inbound %d", existing.Email, inboundID)
				}
			}
			result.AccountsMarkedRemoval++
			affected = true
		}

		if affected {
			result.SubscriptionsAffected++
		}
	}

	return result, nil
}

// setTunnelAccess revokes or restores every access artifact a subscription owns
// (Xray accounts AND WireGuard peers). Prefers the subscription usecase, which
// knows about both; falls back to the account manager alone when the admin
// usecase was built without it. Best-effort: a failure is logged, never fatal
// to the status transition the caller is performing.
func (u *adminUsecase) setTunnelAccess(ctx context.Context, id uint, enabled bool) {
	if u.subUC != nil {
		if err := u.subUC.SetTunnelAccess(ctx, id, enabled); err != nil {
			logger.GetLogger().WithError(err).WithFields(map[string]interface{}{
				"subscription_id": id,
				"enabled":         enabled,
			}).Warn("[admin] Failed to update tunnel access")
		}
		return
	}
	if u.accountUC == nil {
		return
	}
	if enabled {
		_ = u.accountUC.EnableAccountsBySubscription(ctx, id)
	} else {
		_ = u.accountUC.DisableAccountsBySubscription(ctx, id)
	}
}
