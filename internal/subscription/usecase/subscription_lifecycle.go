package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/wgkey"
)

// wgPeerIndex resolves which WireGuard peer backs a given endpoint (inbound +
// host). Peers pinned to a host win for that host; an unpinned peer is the
// fallback everywhere on its inbound, which keeps links working for devices
// created before hosts were pinnable.
type wgPeerIndex struct {
	byEndpoint map[[2]uint]*wgDomain.WGPeer
	byInbound  map[uint]*wgDomain.WGPeer
}

func newWGPeerIndex() *wgPeerIndex {
	return &wgPeerIndex{
		byEndpoint: map[[2]uint]*wgDomain.WGPeer{},
		byInbound:  map[uint]*wgDomain.WGPeer{},
	}
}

func (idx *wgPeerIndex) add(p *wgDomain.WGPeer) {
	var hostID uint
	if p.HostID != nil {
		hostID = *p.HostID
	}
	idx.byEndpoint[[2]uint{p.InboundID, hostID}] = p
	// Prefer an unpinned peer as the inbound-wide fallback; otherwise any peer,
	// so an inbound with only host-pinned devices still renders every host.
	if cur, ok := idx.byInbound[p.InboundID]; !ok || (cur.HostID != nil && hostID == 0) {
		idx.byInbound[p.InboundID] = p
	}
}

func (idx *wgPeerIndex) forEndpoint(inboundID, hostID uint) *wgDomain.WGPeer {
	if p, ok := idx.byEndpoint[[2]uint{inboundID, hostID}]; ok {
		return p
	}
	return idx.byInbound[inboundID]
}

// populateWGDetail fills the WG client-link fields from the peer + inbound
// settings. No peer / no stored private key => fields stay empty, link skipped.
func populateWGDetail(detail *product.InboundDetail, in *nodeDomain.Inbound, peer *wgDomain.WGPeer) {
	if peer == nil || peer.PrivateKey == "" {
		return
	}
	wg := in.GetWireGuardSettingsOrDefault()
	serverPub, err := wgkey.PublicKey(wg.SecretKey)
	if err != nil {
		return
	}
	detail.WGPrivateKey = peer.PrivateKey
	detail.WGServerPublicKey = serverPub
	detail.WGAddress = peer.AssignedIP
	detail.WGPresharedKey = peer.PresharedKey
	detail.WGMTU = wg.MTU
	detail.WGReserved = wg.Reserved
}

// extractSalamanderPassword reads the salamander obfs password from the
// finalmask UDP masks (array, or a lone object for legacy rows). "" if unset.
func extractSalamanderPassword(fm *nodeDomain.FinalMask) string {
	if fm == nil || len(fm.UDP) == 0 {
		return ""
	}
	type mask struct {
		Type     string          `json:"type"`
		Settings json.RawMessage `json:"settings"`
	}
	var masks []mask
	if err := json.Unmarshal(fm.UDP, &masks); err != nil {
		var one mask
		if json.Unmarshal(fm.UDP, &one) != nil {
			return ""
		}
		masks = []mask{one}
	}
	for _, m := range masks {
		if strings.EqualFold(m.Type, "salamander") && len(m.Settings) > 0 {
			var s struct {
				Password string `json:"password"`
			}
			if json.Unmarshal(m.Settings, &s) == nil && s.Password != "" {
				return s.Password
			}
		}
	}
	return ""
}

// buildInboundDetail converts a node Inbound (with Node preloaded) into a product.InboundDetail
func (u *subscriptionUsecase) buildInboundDetail(ctx context.Context, in *nodeDomain.Inbound) product.InboundDetail {
	nodeIP := in.Node.IP
	if in.Address != "" {
		nodeIP = in.Address
	}

	detail := product.InboundDetail{
		NodeID:      in.Node.ID,
		Tag:         in.Tag,
		Protocol:    in.Protocol,
		LinkFormat:  in.LinkFormat,
		NodeIP:      nodeIP,
		ProvisionIP: in.Node.IP, // Always the real node IP for provisioning
		APIPort:     in.Node.APIPort,
		PublicPort:  in.Port,
		Remark:      fmt.Sprintf("%s - %s", in.Node.Name, in.Remark),
		NodeName:    in.Node.Name,
		CountryCode: in.Node.CountryCode,
		Network:     in.Network,
		Security:    in.Security,
		PortRange:   in.PortRange,
	}

	// hysteria2 obfs: client link needs the server's salamander password
	if strings.EqualFold(in.Protocol, "hysteria2") {
		detail.HysteriaObfsPassword = extractSalamanderPassword(in.FinalMask)
	}

	// Populate TLS settings
	tlsSettings := in.GetTLSSettingsOrDefault()
	if tlsSettings != nil {
		detail.TLSSni = tlsSettings.ServerName
		detail.TLSALPN = tlsSettings.ALPN
		detail.TLSFingerprint = tlsSettings.Fingerprint
	}

	// Populate Reality settings
	realitySettings := in.GetRealitySettingsOrDefault()
	if realitySettings != nil {
		detail.RealityPublicKey = realitySettings.PublicKey
		detail.RealityShortID = realitySettings.ShortID
		if len(realitySettings.ServerNames) > 0 {
			detail.RealitySNI = realitySettings.ServerNames[0]
		}
		detail.RealityFingerprint = realitySettings.Fingerprint
		detail.RealitySpiderX = realitySettings.SpiderX
	}

	// Populate Transport settings
	transportSettings := in.GetTransportSettingsOrDefault()
	if transportSettings != nil {
		detail.TransportPath = transportSettings.Path
		detail.TransportHost = transportSettings.Host
		detail.TransportServiceName = transportSettings.ServiceName
		detail.TransportHeaderType = transportSettings.HeaderType
		detail.TransportMode = transportSettings.Mode
	}

	// Populate VLESS settings (for MLKEM encryption)
	if strings.ToLower(in.Protocol) == "vless" {
		vlessSettings := in.GetVLESSSettingsOrDefault()
		if vlessSettings != nil {
			detail.VLESSFlow = vlessSettings.Flow
			detail.VLESSEncryption = vlessSettings.Encryption
			detail.VLESSDecryption = vlessSettings.Decryption
		}
	}

	// Populate VMess settings (for AlterId / scy in URI)
	if strings.ToLower(in.Protocol) == "vmess" {
		if vmessSettings := in.GetVMessSettingsOrDefault(); vmessSettings != nil {
			if vmessSettings.AlterId > 0 {
				detail.VMessAlterId = uint32(vmessSettings.AlterId)
			}
			detail.VMessSecurity = vmessSettings.Security
		}
	}

	detail.AgentPort = in.Node.AgentPort

	return detail
}

// buildInboundDetailFromHost creates an InboundDetail using the inbound as base,
// then applies host overrides for address, port, remark, SNI, host, path, etc.
func (u *subscriptionUsecase) buildInboundDetailFromHost(ctx context.Context, in *nodeDomain.Inbound, host *nodeDomain.Host) product.InboundDetail {
	detail := u.buildInboundDetail(ctx, in)
	product.ApplyHostOverrides(&detail, host)
	return detail
}

func (u *subscriptionUsecase) GetSubscriptionConfig(ctx context.Context, configID string) (*SubscriptionConfigResult, error) {
	sub, err := u.subRepo.FindByConfigID(ctx, configID)
	if err != nil {
		return nil, err
	}

	if sub.Status != domain.SubscriptionStatusActive {
		return nil, errors.New("subscription is not active")
	}

	// Pre-fetch accounts once to avoid duplicate ListAccountsBySubscription calls.
	// Used both for planless inbound discovery and for xrayUUID/xrayEmail lookup.
	var accounts []*accountDomain.Account
	if u.accountManager != nil {
		accounts, _ = u.accountManager.ListAccountsBySubscription(ctx, sub.ID)
	}

	// Pre-fetch active WG peers for wireguard:// links. Only peers with a stored
	// private key qualify. Keyed by the endpoint they were provisioned for —
	// inbound plus pinned host — so a host advertises the device the customer
	// actually created for it.
	wgPeers := newWGPeerIndex()
	if u.wgPeerReader != nil {
		if peers, err := u.wgPeerReader.ListBySubscription(ctx, sub.ID); err == nil {
			for _, p := range peers {
				if p != nil && p.Status == wgDomain.WGPeerStatusActive && p.PrivateKey != "" {
					wgPeers.add(p)
				}
			}
		}
	}

	// Collect active inbounds from the subscription's accounts (with host expansion)
	var inboundDetails []product.InboundDetail
	if accounts != nil {
		// Manual/planless subscriptions: look up inbounds via the accounts table
		for _, acc := range accounts {
			if acc.Inbound == nil || acc.Inbound.IsDisabled || acc.Inbound.Node == nil || !acc.Inbound.Node.IsActive {
				continue
			}
			activeHosts := acc.Inbound.GetActiveHosts()
			if len(activeHosts) == 0 {
				detail := u.buildInboundDetail(ctx, acc.Inbound)
				populateWGDetail(&detail, acc.Inbound, wgPeers.forEndpoint(acc.Inbound.ID, 0))
				inboundDetails = append(inboundDetails, detail)
			} else {
				for i := range activeHosts {
					detail := u.buildInboundDetailFromHost(ctx, acc.Inbound, &activeHosts[i])
					populateWGDetail(&detail, acc.Inbound, wgPeers.forEndpoint(acc.Inbound.ID, activeHosts[i].ID))
					inboundDetails = append(inboundDetails, detail)
				}
			}
		}
	}

	if len(inboundDetails) == 0 {
		return nil, ErrNoNodesAvailable
	}

	// Use account UUID for config generation (may differ from config_id after key regeneration)
	xrayUUID := sub.ConfigID
	xrayEmail := sub.ConfigEmail
	if len(accounts) > 0 {
		xrayUUID = accounts[0].UUID
		xrayEmail = accounts[0].Email
	}

	planName := sub.Label
	if planName == "" {
		planName = "Manual"
	}

	subInfo := &product.SubscriptionInfo{
		ID:             sub.ID,
		UserID:         sub.GetUserID(),
		ConfigID:       xrayUUID,
		Email:          xrayEmail,
		DataLimit:      sub.GetEffectiveDataLimit(),
		DataUsed:       sub.DataUsed, // Pass current data usage
		PlanName:       planName,
		Status:         string(sub.Status),
		BandwidthLimit: sub.GetEffectiveBandwidthLimit(),
		Inbounds:       inboundDetails,
	}
	if effectiveEnd := sub.GetEffectiveEndDate(); effectiveEnd != nil {
		subInfo.ExpiresAt = *effectiveEnd
	}

	provider, err := u.providerFactory.Get(sub.ProductType)
	if err != nil {
		return nil, err
	}

	config, err := provider.GenerateClientConfig(ctx, subInfo)
	if err != nil {
		return nil, err
	}

	result := &SubscriptionConfigResult{
		Config:    config,
		DataUsed:  sub.DataUsed,
		DataLimit: sub.GetEffectiveDataLimit(),
		PlanName:  planName,
	}
	if effectiveEnd := sub.GetEffectiveEndDate(); effectiveEnd != nil {
		result.ExpiresAt = effectiveEnd
	}

	return result, nil
}

// deactivateOnNodes deactivates a subscription's user on all Xray nodes and disables
// the associated accounts in DB. Used by Cancel, CheckAndExpire*, etc.
func (u *subscriptionUsecase) deactivateOnNodes(ctx context.Context, sub *domain.Subscription) {
	log := logger.GetLogger()

	subInfo, prepErr := u.prepareManualSubscriptionInfo(ctx, sub)
	if prepErr != nil {
		log.WithError(prepErr).WithField("subscription_id", sub.ID).Warn("[deactivateOnNodes] Failed to prepare subscription info")
	}
	if subInfo == nil {
		// Planless/manual sub: still deactivate via the provider so WG peers
		// (keyed by sub ID) get disabled. No plan inbounds to remove from Xray.
		subInfo = &product.SubscriptionInfo{}
	}
	subInfo.ID = sub.ID // needed so the provider can disable WG peers (keyed by sub ID)
	subInfo.ConfigID = sub.ConfigID
	subInfo.Email = sub.ConfigEmail
	if u.providerFactory != nil {
		if provider, provErr := u.providerFactory.Get(sub.ProductType); provErr != nil {
			log.WithError(provErr).WithField("subscription_id", sub.ID).Warn("[deactivateOnNodes] Provider not found")
		} else if provider != nil {
			if deactivateErr := provider.DeactivateUser(ctx, subInfo); deactivateErr != nil {
				log.WithError(deactivateErr).WithField("subscription_id", sub.ID).Warn("[deactivateOnNodes] Failed to deactivate user on Xray")
			}
		}
	}

	if u.accountManager != nil {
		if err := u.accountManager.DisableAccountsBySubscription(ctx, sub.ID); err != nil {
			log.WithError(err).WithField("subscription_id", sub.ID).Warn("[deactivateOnNodes] Failed to disable accounts")
		}
	}
}

func (u *subscriptionUsecase) Cancel(ctx context.Context, id uint) error {
	log := logger.GetLogger()
	collector := events.NewEventCollector()
	log.WithField("subscription_id", id).Info("[Cancel] Cancelling subscription")

	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		log.WithError(err).WithField("subscription_id", id).Error("[Cancel] Failed to find subscription")
		return err
	}

	u.deactivateOnNodes(ctx, sub)

	err = u.subRepo.UpdateStatus(ctx, id, domain.SubscriptionStatusCancelled)
	if err != nil {
		log.WithError(err).WithField("subscription_id", id).Error("[Cancel] Failed to update status")
		return err
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": id,
		"user_id":         sub.GetUserID(),
	}).Info("[Cancel] Subscription cancelled successfully")

	// Buffer subscription cancelled event
	planName := sub.Label
	collector.Add(events.Event{
		Type:      events.EventSubscriptionCancelled,
		Timestamp: time.Now(),
		Payload: events.SubscriptionEventPayload{
			SubscriptionID: id,
			UserID:         sub.GetUserID(),
			PlanName:       planName,
		},
	})

	collector.Flush(u.eventBus)
	return nil
}

// reactivateSubscription handles logic to reactivate a subscription (DB status, accounts, provider)
func (u *subscriptionUsecase) reactivateSubscription(ctx context.Context, id uint) error {
	log := logger.GetLogger()

	// Update status to Active
	if err := u.subRepo.UpdateStatus(ctx, id, domain.SubscriptionStatusActive); err != nil {
		return err
	}

	// Enable associated accounts
	if u.accountManager != nil {
		if err := u.accountManager.EnableAccountsBySubscription(ctx, id); err != nil {
			log.WithError(err).WithField("subscription_id", id).Warn("Failed to enable accounts during reactivation")
		}
	}

	// Activate user on Xray Provider
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		log.WithError(err).Error("Failed to fetch subscription for provider activation")
		return nil // Non-critical
	}

	subInfo, err := u.prepareManualSubscriptionInfo(ctx, sub)
	if err != nil {
		log.WithError(err).Warn("Failed to prepare subscription info for provider activation")
	} else if subInfo != nil {
		subInfo.ID = sub.ID // so WG peers re-activate on reactivation
		subInfo.ConfigID = sub.ConfigID
		subInfo.Email = sub.ConfigEmail
		provider, err := u.providerFactory.Get(sub.ProductType)
		if err == nil && provider != nil {
			if err := provider.ActivateUser(ctx, subInfo); err != nil {
				log.WithError(err).Warn("Failed to activate user on provider")
			}
		}
	}

	log.WithField("subscription_id", id).Info("Subscription automatically reactivated due to data limit increase")
	return nil
}

func (u *subscriptionUsecase) AssignToInbound(ctx context.Context, subscriptionID, inboundID uint) error {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"subscription_id": subscriptionID,
		"inbound_id":      inboundID,
	}).Info("[AssignToInbound] Assignment requested")

	// Load subscription
	sub, err := u.subRepo.FindByID(ctx, subscriptionID)
	if err != nil {
		return fmt.Errorf("subscription not found: %w", err)
	}

	if sub.ConfigEmail == "" || sub.ConfigID == "" {
		return errors.New("subscription has empty credentials (ConfigEmail or ConfigID)")
	}

	// Use the UUID from existing accounts if available, because the user may
	// have edited the UUID after subscription creation (the edit only updates
	// account records, not subscription.config_id).  Fall back to
	// sub.ConfigID when there are no accounts yet.
	configUUID := sub.ConfigID
	if existingAccounts, accErr := u.accountManager.ListAccountsBySubscription(ctx, subscriptionID); accErr == nil && len(existingAccounts) > 0 {
		configUUID = existingAccounts[0].UUID
	}

	// Load target inbound (GetInbound loads all columns including NodeID and
	// JSON settings like VLESSSettings; Node relation is NOT preloaded but
	// we only need NodeID which is a stored column)
	inbound, err := u.nodeRepo.GetInbound(ctx, inboundID)
	if err != nil {
		return fmt.Errorf("inbound not found: %w", err)
	}

	// Determine credentials for target inbound protocol
	flow := ""
	encryption := ""
	protocol := strings.ToLower(inbound.Protocol)
	switch protocol {
	case "vless":
		settings := inbound.GetVLESSSettingsOrDefault()
		flow = settings.Flow
		encryption = settings.Encryption
	case "vmess":
		encryption = "auto"
	}

	// Determine data limit
	var dataLimit int64

	// Create account via account manager (handles duplicate check + provisioning enqueue)
	if u.accountManager == nil {
		return errors.New("account manager not available")
	}
	if err := u.accountManager.CreateAccountForSubscription(
		ctx, inboundID, sub.ConfigEmail, configUUID,
		flow, encryption, sub.ID, dataLimit,
	); err != nil {
		return fmt.Errorf("failed to create account: %w", err)
	}

	// Push config to the target node via nodeSyncer
	if inbound.NodeID > 0 {
		go func() {
			pushCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := u.nodeSyncer.SyncInbounds(pushCtx, inbound.NodeID); err != nil {
				log.Warnf("[AssignToInbound] Failed to sync node %d: %v", inbound.NodeID, err)
			}
		}()
	}

	log.WithFields(map[string]interface{}{
		"subscription_id": subscriptionID,
		"inbound_id":      inboundID,
		"email":           sub.ConfigEmail,
	}).Info("[AssignToInbound] Assignment completed")

	return nil
}
