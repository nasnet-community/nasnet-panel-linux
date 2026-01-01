package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"golang.org/x/crypto/bcrypt"
)

func (u *subscriptionUsecase) RenameSubscription(ctx context.Context, id uint, label string) error {
	return u.subRepo.UpdateLabel(ctx, id, label)
}

func (u *subscriptionUsecase) UpdateTelegramChatIDByConfigID(ctx context.Context, configID string, chatID int64) error {
	log := logger.GetLogger()

	sub, err := u.subRepo.FindByConfigID(ctx, configID)
	if err != nil {
		return ErrSubscriptionNotFound
	}

	// Always update the telegram_chat_id (notification preference)
	if err := u.subRepo.UpdateTelegramChatID(ctx, sub.ID, chatID); err != nil {
		return err
	}

	// If setting a chat ID, link the subscription to the matching telegram user
	if chatID > 0 {
		// Find the telegram user (or create one)
		tgUser, err := u.userRepo.FindByTelegramID(ctx, chatID)
		if err != nil {
			// User doesn't exist yet — create a minimal one
			tgUser = &userDomain.User{
				TelegramID: chatID,
				Username:   fmt.Sprintf("user_%d", chatID),
				Language:   "en",
			}
			if err := u.userRepo.Create(ctx, tgUser); err != nil {
				log.WithError(err).WithField("telegram_id", chatID).Warn("Failed to create user for telegram chat ID linking")
				return nil // chat ID was saved, user linking is best-effort
			}
			log.WithField("telegram_id", chatID).Info("Created user for telegram chat ID linking")
		}

		// Link subscription if it's unowned or owned by a different user
		if sub.UserID == nil || *sub.UserID != tgUser.ID {
			if err := u.subRepo.SetUserID(ctx, sub.ID, &tgUser.ID); err != nil {
				log.WithError(err).WithField("sub_id", sub.ID).Warn("Failed to link user to subscription")
				return nil
			}
			log.WithFields(map[string]interface{}{
				"sub_id":      sub.ID,
				"user_id":     tgUser.ID,
				"telegram_id": chatID,
			}).Info("Linked subscription to user via telegram chat ID")
		}
	}

	return nil
}

func (u *subscriptionUsecase) RegenerateUUID(ctx context.Context, id uint) (*domain.Subscription, error) {
	log := logger.GetLogger()
	log.WithField("subscription_id", id).Warn("[RegenerateUUID] Regenerating UUID (security-sensitive operation)")

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		log.WithError(err).WithField("subscription_id", id).Error("[RegenerateUUID] Failed to find subscription")
		return nil, err
	}

	provider, err := u.providerFactory.Get(sub.ProductType)
	if err != nil {
		log.WithError(err).Error("[RegenerateUUID] Provider not found")
		return nil, err
	}

	// rebuild info from the subscription's existing account records
	planName := "Manual"

	subInfoOld, prepErr := u.prepareManualSubscriptionInfo(ctx, sub)
	if prepErr != nil {
		log.WithError(prepErr).WithField("subscription_id", id).Warn("[RegenerateUUID] Failed to prepare old subscription info")
	}
	if subInfoOld != nil {
		subInfoOld.ConfigID = sub.ConfigID
		subInfoOld.Email = sub.ConfigEmail
		if deactivateErr := provider.DeactivateUser(ctx, subInfoOld); deactivateErr != nil {
			log.WithError(deactivateErr).WithField("subscription_id", id).Warn("[RegenerateUUID] Failed to deactivate old user on Xray")
		}
	}

	subInfoNew, err := u.prepareManualSubscriptionInfo(ctx, sub)
	if err != nil {
		log.WithError(err).Error("[RegenerateUUID] Failed to prepare subscription info from accounts")
		return nil, err
	}

	configResult, err := provider.GenerateConfig(ctx, subInfoNew, planName)
	if err != nil {
		log.WithError(err).Error("[RegenerateUUID] Failed to generate new config")
		return nil, err
	}

	sub.ConfigID = configResult.ConfigID
	sub.ConfigEmail = configResult.ConfigEmail
	sub.ConfigData = configResult.ConfigData
	sub.SubLink = configResult.SubLink
	sub.FileExt = configResult.FileExtension

	if err := u.subRepo.Update(ctx, sub); err != nil {
		log.WithError(err).Error("[RegenerateUUID] Failed to update subscription with new config")
		return nil, fmt.Errorf("failed to save new config: %w", err)
	}

	subInfoNew.ConfigID = configResult.ConfigID
	subInfoNew.Email = configResult.ConfigEmail
	if activateErr := provider.ActivateUser(ctx, subInfoNew); activateErr != nil {
		log.WithError(activateErr).WithField("subscription_id", id).Warn("[RegenerateUUID] Failed to activate new user on Xray")
	}

	// Recreate account records with new UUID/email
	if u.accountManager != nil {
		oldAccounts, accErr := u.accountManager.ListAccountsBySubscription(ctx, id)
		if accErr == nil && len(oldAccounts) > 0 {
			if delErr := u.accountManager.DeleteAccountsBySubscription(ctx, id); delErr != nil {
				log.WithError(delErr).WithField("subscription_id", id).Warn("[RegenerateUUID] Failed to delete old accounts")
			}
			for _, acc := range oldAccounts {
				if acc.Inbound == nil {
					continue
				}
				flow := acc.Flow
				encryption := acc.Encryption
				if err := u.accountManager.CreateAccountForSubscription(ctx, acc.InboundID, configResult.ConfigEmail, configResult.ConfigID, flow, encryption, id, acc.DataLimit); err != nil {
					log.WithError(err).Warnf("[RegenerateUUID] Failed to recreate account for inbound %d", acc.InboundID)
				}
			}
		}
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
	}).Info("[RegenerateUUID] UUID regenerated successfully")

	return sub, nil
}

// RegenerateSubscriptionKey changes only the subscription URL key (link_key)
// without touching the Xray user credentials on nodes. The old subscription link
// will stop working, but the user's connection stays active.
// If customKey is non-empty, it is used as the new key; otherwise a random UUID is generated.
func (u *subscriptionUsecase) RegenerateSubscriptionKey(ctx context.Context, id uint, customKey string) (*domain.Subscription, error) {
	log := logger.GetLogger()
	log.WithField("subscription_id", id).Info("[RegenerateSubscriptionKey] Regenerating subscription key")

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	newKey := customKey
	if newKey == "" {
		newKey = uuid.New().String()
	}
	sub.LinkKey = newKey

	if err := u.subRepo.Update(ctx, sub); err != nil {
		return nil, fmt.Errorf("failed to update subscription key: %w", err)
	}

	log.WithField("subscription_id", id).Info("[RegenerateSubscriptionKey] Subscription key regenerated")
	return sub, nil
}

// prepareManualSubscriptionInfo builds SubscriptionInfo for manual subscriptions
// by looking up existing accounts to find which inbounds are in use.
func (u *subscriptionUsecase) prepareManualSubscriptionInfo(ctx context.Context, sub *domain.Subscription) (*product.SubscriptionInfo, error) {
	if u.accountManager == nil {
		return nil, fmt.Errorf("account manager not available")
	}

	accounts, err := u.accountManager.ListAccountsBySubscription(ctx, sub.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no accounts found for manual subscription %d", sub.ID)
	}

	var inboundDetails []product.InboundDetail
	for _, acc := range accounts {
		if acc.Inbound == nil || acc.Inbound.Node == nil || !acc.Inbound.Node.IsActive {
			continue
		}

		activeHosts := acc.Inbound.GetActiveHosts()
		if len(activeHosts) == 0 {
			detail := u.buildInboundDetail(ctx, acc.Inbound)
			inboundDetails = append(inboundDetails, detail)
		} else {
			for _, host := range activeHosts {
				detail := u.buildInboundDetailFromHost(ctx, acc.Inbound, &host)
				inboundDetails = append(inboundDetails, detail)
			}
		}
	}

	if len(inboundDetails) == 0 {
		return nil, fmt.Errorf("no active inbounds found for manual subscription %d", sub.ID)
	}

	return &product.SubscriptionInfo{
		UserID:    sub.GetUserID(),
		DataLimit: sub.GetEffectiveDataLimit(),
		PlanName:  "Manual",
		Inbounds:  inboundDetails,
	}, nil
}

// CreateDirect creates a subscription directly without provisioning (for migrations)
func (u *subscriptionUsecase) CreateDirect(ctx context.Context, sub *domain.Subscription) error {
	return u.subRepo.Create(ctx, sub)
}

// AssignToUser reassigns a subscription to a different user
func (u *subscriptionUsecase) AssignToUser(ctx context.Context, subID, userID uint) error {
	return u.subRepo.UpdateUserID(ctx, subID, userID)
}

// CreateManual creates a standalone subscription not linked to any user or plan.
func (u *subscriptionUsecase) CreateManual(ctx context.Context, req *ManualSubscriptionRequest) (*domain.Subscription, error) {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"label":       req.Label,
		"inbound_ids": req.InboundIDs,
		"data_limit":  req.DataLimit,
		"max_devices": req.MaxDevices,
	}).Info("[CreateManual] Creating manual subscription")

	// Fetch inbounds with nodes preloaded
	inbounds, err := u.nodeRepo.FindInboundsByIDs(ctx, req.InboundIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch inbounds: %w", err)
	}
	if len(inbounds) == 0 {
		return nil, fmt.Errorf("no valid inbounds found for the given IDs")
	}

	// Validate all inbounds exist and nodes are active
	for _, in := range inbounds {
		if in.IsDisabled {
			return nil, fmt.Errorf("inbound %d is disabled", in.ID)
		}
		if in.Node == nil || !in.Node.IsActive {
			return nil, fmt.Errorf("inbound %d belongs to an inactive or missing node", in.ID)
		}
	}

	// Sync nodes (best effort)
	if u.nodeSyncer != nil {
		syncedNodes := make(map[uint]bool)
		for _, in := range inbounds {
			if in.NodeID > 0 && !syncedNodes[in.NodeID] {
				if syncErr := u.nodeSyncer.SyncInbounds(ctx, in.NodeID); syncErr != nil {
					log.WithError(syncErr).WithField("node_id", in.NodeID).Warn("[CreateManual] Failed to sync inbounds for node")
				}
				syncedNodes[in.NodeID] = true
			}
		}
	}

	// Build inbound details
	var inboundDetails []product.InboundDetail
	for _, in := range inbounds {
		detail := u.buildInboundDetail(ctx, in)
		inboundDetails = append(inboundDetails, detail)
	}

	// Build subscription info (no user, no plan)
	subInfo := &product.SubscriptionInfo{
		UserID:         0,
		DataLimit:      req.DataLimit,
		BandwidthLimit: req.BandwidthLimit,
		PlanName:       "Manual",
		Inbounds:       inboundDetails,
	}

	// Get xray provider (manual subs are xray-only)
	provider, err := u.providerFactory.Get("xray")
	if err != nil {
		return nil, fmt.Errorf("xray provider not found: %w", err)
	}

	// Generate config and provision on nodes
	configResult, err := provider.GenerateConfig(ctx, subInfo, "Manual")
	if err != nil {
		return nil, fmt.Errorf("provisioning failed: %w", err)
	}

	now := time.Now()
	linkKey := uuid.New().String() // Separate link key from the Xray UUID (ConfigID)
	subscription := &domain.Subscription{
		UserID:      req.UserID,
		IsManual:    true,
		ProductType: "xray",
		Status:      domain.SubscriptionStatusActive,
		Label:       req.Label,
		MaxDevices:  req.MaxDevices,
		StartDate:   &now,
		EndDate:     req.EndDate,
		DataLimit:   req.DataLimit,
		DataUsed:    0,
		ConfigID:    configResult.ConfigID,
		LinkKey:     linkKey,
		ConfigEmail: configResult.ConfigEmail,
		ConfigData:  configResult.ConfigData,
		SubLink:     configResult.SubLink,
		FileExt:     configResult.FileExtension,
	}

	// Store bandwidth as custom override for manual subs (no plan to inherit from)
	if req.BandwidthLimit > 0 {
		bw := req.BandwidthLimit
		subscription.CustomBandwidthLimit = &bw
		subscription.IsBandwidthCustom = true
	}

	if err := u.subRepo.Create(ctx, subscription); err != nil {
		// Rollback: deactivate provisioned users
		log.WithError(err).Error("[CreateManual] DB create failed, rolling back")
		rollbackInfo := &product.SubscriptionInfo{
			ConfigID: configResult.ConfigID,
			Email:    configResult.ConfigEmail,
			Inbounds: inboundDetails,
		}
		if rollbackErr := provider.DeactivateUser(ctx, rollbackInfo); rollbackErr != nil {
			log.WithError(rollbackErr).Error("[CreateManual] Rollback also failed")
		}
		return nil, err
	}

	// Create Account records per inbound
	if u.accountManager != nil {
		for _, in := range inbounds {
			flow := ""
			encryption := ""
			if strings.ToLower(in.Protocol) == "vless" {
				vless := in.GetVLESSSettingsOrDefault()
				encryption = vless.Encryption
				if strings.Contains(in.LinkFormat, "xtls-rprx-vision") {
					flow = "xtls-rprx-vision"
				}
			}
			if err := u.accountManager.CreateAccountForSubscription(ctx, in.ID, configResult.ConfigEmail, configResult.ConfigID, flow, encryption, subscription.ID, 0); err != nil {
				log.WithError(err).Warnf("[CreateManual] Failed to create account for inbound %d", in.ID)
			}
		}
	}

	// Push updated xray config to affected agent nodes so the new user is persisted
	// in the config file (not just added via runtime gRPC API).
	if u.nodeSyncer != nil {
		syncedNodes := make(map[uint]bool)
		for _, in := range inbounds {
			if in.NodeID > 0 && !syncedNodes[in.NodeID] {
				if err := u.nodeSyncer.SyncInbounds(ctx, in.NodeID); err != nil {
					log.Warnf("[CreateManual] Failed to push config to node %d: %v", in.NodeID, err)
				}
				syncedNodes[in.NodeID] = true
			}
		}
	}

	log.WithField("subscription_id", subscription.ID).Info("[CreateManual] Manual subscription created successfully")
	return subscription, nil
}

// SetPanelPassword sets the panel password mode and optional custom password for a subscription.
func (u *subscriptionUsecase) SetPanelPassword(ctx context.Context, id uint, mode string, password string) error {
	log := logger.GetLogger()

	switch mode {
	case "default", "disabled":
		return u.subRepo.SetPanelPassword(ctx, id, "", mode)
	case "custom":
		if password == "" {
			return fmt.Errorf("password is required for custom mode")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.WithError(err).Error("[SetPanelPassword] Failed to hash password")
			return fmt.Errorf("failed to hash password")
		}
		return u.subRepo.SetPanelPassword(ctx, id, string(hash), mode)
	default:
		return fmt.Errorf("invalid panel password mode: %s", mode)
	}
}

// GetPanelPasswordHash returns the panel password hash and mode for a subscription.
func (u *subscriptionUsecase) GetPanelPasswordHash(ctx context.Context, id uint) (string, string, error) {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return "", "", ErrSubscriptionNotFound
	}
	return sub.PanelPasswordHash, sub.PanelPasswordMode, nil
}
