package xray

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// NodeClientFunc resolves a node ID to an agent.NodeClient.
// This callback is injected at startup so the provider can reach both
// direct-mode and reverse-mode nodes without depending on the node usecase directly.
type NodeClientFunc func(ctx context.Context, nodeID uint) (agent.NodeClient, error)

// WGDeviceProvisioner suspends/resumes a subscription's WireGuard peers when its
// (mixed) xray sub is deactivated/reactivated. Implemented by the wireguard usecase.
type WGDeviceProvisioner interface {
	ActivateSubscription(ctx context.Context, subID uint) error
	DeactivateSubscription(ctx context.Context, subID uint) error
}

// Provider implements the product.Provider interface for Xray
type Provider struct {
	grpcClient    *xray.GRPCClient
	linkBuilder   *LinkBuilder
	getNodeClient NodeClientFunc
	wgProvisioner WGDeviceProvisioner
}

// NewProvider creates a new Xray provider
func NewProvider(grpcClient *xray.GRPCClient) *Provider {
	return &Provider{
		grpcClient:  grpcClient,
		linkBuilder: NewLinkBuilder(),
	}
}

// SetNodeClientFunc injects the callback used to obtain an agent client for a given node.
// Must be called before the provider handles any provisioning requests.
func (p *Provider) SetNodeClientFunc(fn NodeClientFunc) {
	p.getNodeClient = fn
}

// SetWGProvisioner injects WireGuard per-device provisioning (optional, mixed plans).
func (p *Provider) SetWGProvisioner(w WGDeviceProvisioner) { p.wgProvisioner = w }

// GetType returns the product type
func (p *Provider) GetType() product.ProductType {
	return product.ProductTypeXray
}

// GenerateConfig creates new credentials and generates links for ALL assigned inbounds
func (p *Provider) GenerateConfig(ctx context.Context, sub *product.SubscriptionInfo, planName string) (*product.ConfigResult, error) {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"user_id":       sub.UserID,
		"inbound_count": len(sub.Inbounds),
	}).Info("[XrayProvider] Generating config and provisioning user")

	uuid := xray.GenerateUUID()

	identifier := int64(sub.UserID)
	if sub.TelegramID != 0 {
		identifier = sub.TelegramID
	}

	shortUUID := ""
	if len(uuid) >= 8 {
		shortUUID = uuid[:8]
	} else {
		shortUUID = uuid
	}

	// Unique email identifier for Xray stats
	prefix := "user"
	if identifier == 0 {
		prefix = "manual"
	}
	email := fmt.Sprintf("%s_%d_%s", prefix, identifier, shortUUID)

	// Build client config with generated credentials
	tempSub := *sub
	tempSub.ConfigID = uuid
	tempSub.Email = email

	configData, err := p.GenerateClientConfig(ctx, &tempSub)
	if err != nil {
		log.WithError(err).Error("[XrayProvider] Failed to generate client config")
		return nil, err
	}

	// Provision user on all assigned nodes via gRPC
	var lastErr error
	var failedInbounds []product.ProvisionResult
	successCount := 0
	for _, inbound := range sub.Inbounds {
		// Skip info-only hosts — they don't have real servers to provision on
		if inbound.IsInfoOnly {
			continue
		}
		// Determine protocol enum
		var protocol xray.Protocol
		switch strings.ToLower(inbound.Protocol) {
		case "vmess":
			protocol = xray.ProtocolVMess
		case "vless":
			protocol = xray.ProtocolVLESS
		case "trojan":
			protocol = xray.ProtocolTrojan
		default:
			continue // Skip unknown protocols
		}

		// Determine bandwidth tier level from plan
		bwTier := bandwidth.GetTier(sub.BandwidthLimit)

		user := &xray.User{
			Email:    email,
			UUID:     uuid,
			Protocol: protocol,
			Level:    bwTier.Level,
			Flow:     "",
		}

		// Handle VLESS Vision specific flow from dedicated field
		if protocol == xray.ProtocolVLESS {
			if inbound.VLESSFlow != "" {
				user.Flow = inbound.VLESSFlow
			}
			user.Encryption = inbound.VLESSEncryption
		}

		// Use Agent to provision
		client, err := p.getNodeClient(ctx, inbound.NodeID)
		if err != nil {
			log.WithError(err).WithField("node_id", inbound.NodeID).Warn("[XrayProvider] Failed to get agent client")
			lastErr = err
			failedInbounds = append(failedInbounds, product.ProvisionResult{
				InboundTag: inbound.Tag, NodeID: inbound.NodeID, Success: false, Error: err.Error(),
			})
			continue
		}
		err = client.AddUser(ctx, inbound.Tag, user.Email, user.UUID, string(user.Protocol), user.Flow, user.Encryption, int32(user.Level))
		client.Close()
		if err != nil {
			log.WithError(err).Warn("[XrayProvider] Failed to add user via agent")
			lastErr = err
			failedInbounds = append(failedInbounds, product.ProvisionResult{
				InboundTag: inbound.Tag, NodeID: inbound.NodeID, Success: false, Error: err.Error(),
			})
		} else {
			successCount++
		}
	}

	// At least one node must succeed (or all inbounds were info-only/skipped)
	if successCount == 0 && lastErr != nil {
		log.WithError(lastErr).Error("[XrayProvider] Failed to provision user on any server")
		return nil, fmt.Errorf("failed to provision user on any server: %w", lastErr)
	}

	log.WithFields(map[string]interface{}{
		"email":         email,
		"success_count": successCount,
		"failed_count":  len(failedInbounds),
		"total_nodes":   len(sub.Inbounds),
	}).Info("[XrayProvider] User provisioned successfully")

	subLink := base64.StdEncoding.EncodeToString([]byte(configData))

	return &product.ConfigResult{
		ConfigData:     configData, // Raw text (line separated links)
		ConfigID:       uuid,
		ConfigEmail:    email,
		SubLink:        subLink, // Base64 encoded string
		FileExtension:  ".txt",
		FailedInbounds: failedInbounds,
	}, nil
}

// GenerateClientConfig generates dynamic links with usage stats and flags
func (p *Provider) GenerateClientConfig(ctx context.Context, sub *product.SubscriptionInfo) (string, error) {
	var links []string

	// Sort inbounds by Priority (lower = first) — copy to avoid mutating caller's slice
	sortedInbounds := make([]product.InboundDetail, len(sub.Inbounds))
	copy(sortedInbounds, sub.Inbounds)
	sort.SliceStable(sortedInbounds, func(i, j int) bool {
		return sortedInbounds[i].Priority < sortedInbounds[j].Priority
	})

	// Generate the stats string once (same for all links)
	// e.g., "5.2 GB Left" or "∞"
	statsStr := p.generateStatsString(sub.DataUsed, sub.DataLimit)

	// Pre-compute remark template context data
	dataLeftStr := statsStr
	dataLimitStr := "♾️"
	dataUsedStr := func() string {
		const GB = 1024 * 1024 * 1024
		const MB = 1024 * 1024
		usedGB := float64(sub.DataUsed) / float64(GB)
		if usedGB >= 1.0 {
			return fmt.Sprintf("%.1f GB", usedGB)
		}
		usedMB := float64(sub.DataUsed) / float64(MB)
		if usedMB >= 1.0 {
			return fmt.Sprintf("%.0f MB", usedMB)
		}
		return "0 MB"
	}()
	usagePercentStr := "0%"
	if sub.DataLimit > 0 {
		const GB = 1024 * 1024 * 1024
		const MB = 1024 * 1024
		limitGB := float64(sub.DataLimit) / float64(GB)
		if limitGB >= 1.0 {
			dataLimitStr = fmt.Sprintf("%.1f GB", limitGB)
		} else {
			dataLimitStr = fmt.Sprintf("%.0f MB", float64(sub.DataLimit)/float64(MB))
		}
		pct := float64(sub.DataUsed) / float64(sub.DataLimit) * 100
		usagePercentStr = fmt.Sprintf("%.0f%%", pct)
	}

	// Compute days left if expiry info is available
	daysLeftStr := "∞"
	timeLeftStr := "∞"
	if !sub.ExpiresAt.IsZero() {
		daysLeft := int(time.Until(sub.ExpiresAt).Hours() / 24)
		if daysLeft < 0 {
			daysLeft = 0
		}
		daysLeftStr = fmt.Sprintf("%d", daysLeft)
		timeLeftStr = fmt.Sprintf("%dd", daysLeft)
	}

	statusEmojiStr := product.StatusEmoji(sub.Status)

	for _, inbound := range sortedInbounds {
		// Info-only hosts: generate placeholder VLESS link with rendered remark
		if inbound.IsInfoOnly {
			finalRemark := product.RenderRemark(inbound.Remark, product.RemarkContext{
				DataUsed:     dataUsedStr,
				DataLeft:     dataLeftStr,
				DaysLeft:     daysLeftStr,
				TimeLeft:     timeLeftStr,
				DataLimit:    dataLimitStr,
				UsagePercent: usagePercentStr,
				StatusEmoji:  statusEmojiStr,
			})
			link := fmt.Sprintf("vless://00000000-0000-0000-0000-000000000000@127.0.0.1:0?security=none&encryption=none&type=tcp#%s",
				url.PathEscape(finalRemark))
			links = append(links, link)
			continue
		}

		flag := getFlagEmoji(inbound.CountryCode)

		var finalRemark string
		if inbound.RemarkIsTemplate {
			// Render the host's remark template with available context
			finalRemark = product.RenderRemark(inbound.Remark, product.RemarkContext{
				Flag:         flag,
				Country:      inbound.CountryCode,
				CountryCode:  inbound.CountryCode,
				Node:         inbound.NodeName,
				Port:         inbound.PublicPort,
				Protocol:     inbound.Protocol,
				Network:      inbound.Network,
				Security:     inbound.Security,
				DataUsed:     dataUsedStr,
				DataLeft:     dataLeftStr,
				DaysLeft:     daysLeftStr,
				TimeLeft:     timeLeftStr,
				DataLimit:    dataLimitStr,
				UsagePercent: usagePercentStr,
				StatusEmoji:  statusEmojiStr,
			})
		} else {
			// Default template for hosts without a custom remark template.
			// Uses the same rendering system for consistency and v2rayNG compatibility.
			finalRemark = product.RenderRemark(product.DefaultRemarkTemplate, product.RemarkContext{
				Flag:         flag,
				Country:      inbound.CountryCode,
				CountryCode:  inbound.CountryCode,
				Node:         inbound.NodeName,
				Port:         inbound.PublicPort,
				Protocol:     inbound.Protocol,
				Network:      inbound.Network,
				Security:     inbound.Security,
				DataUsed:     dataUsedStr,
				DataLeft:     dataLeftStr,
				DaysLeft:     daysLeftStr,
				TimeLeft:     timeLeftStr,
				DataLimit:    dataLimitStr,
				UsagePercent: usagePercentStr,
				StatusEmoji:  statusEmojiStr,
			})
		}

		var link string

		// Build InboundInfo for both cases (needed for custom link enrichment too)
		inboundInfo := p.buildInboundInfo(&inbound)

		if inbound.LinkFormat != "" {
			// Use existing template link
			rawLink := p.generateLink(inbound.LinkFormat, sub.ConfigID, sub.Email, inbound.NodeIP, inbound.PublicPort, finalRemark)

			// fill in security/transport params the template might be missing
			link = p.enrichLinkWithInboundInfo(rawLink, inboundInfo)
		} else {
			// Generate proper link from full settings using config_generator
			generatedLink, err := xray.GenerateConfigLink(inboundInfo, sub.ConfigID, inbound.NodeIP, finalRemark)
			if err == nil {
				link = generatedLink
			}
		}

		if link != "" {
			links = append(links, link)
		}
	}

	return strings.Join(links, "\n"), nil
}

// buildInboundInfo converts product.InboundDetail to xray.InboundInfo for config generation
func (p *Provider) buildInboundInfo(detail *product.InboundDetail) *xray.InboundInfo {
	return BuildInboundInfo(detail)
}

// BuildInboundInfo: InboundDetail → InboundInfo for the URI generators.
// Single mapping point; new fields on either side must be wired here.
// ServerLink builds a client config link for one inbound, the same way the
// panel and bot per-server views need it. LinkFormat templates win; otherwise
// it generates from the inbound settings.
func ServerLink(detail product.InboundDetail, configID, host string) string {
	if detail.LinkFormat != "" {
		return linkFromTemplate(detail.LinkFormat, configID, "", host, detail.PublicPort, detail.Remark)
	}
	link, err := xray.GenerateConfigLink(BuildInboundInfo(&detail), configID, host, detail.Remark)
	if err != nil {
		return ""
	}
	return link
}

func linkFromTemplate(format, uuid, email, host string, port int, remark string) string {
	r := strings.NewReplacer(
		"{uuid}", uuid,
		"{email}", email,
		"{host}", host,
		"{port}", fmt.Sprintf("%d", port),
		"{name}", remark,
	)
	return r.Replace(format)
}

func BuildInboundInfo(detail *product.InboundDetail) *xray.InboundInfo {
	info := &xray.InboundInfo{
		Tag:      detail.Tag,
		Protocol: detail.Protocol,
		Port:     uint32(detail.PublicPort),
		Network:  detail.Network,
		Security: detail.Security,
	}

	// TLS Settings — only populate when the inbound actually negotiates TLS.
	// This prevents leaking SNI/ALPN/fp into a "none" or "reality" link.
	if strings.EqualFold(detail.Security, "tls") {
		if detail.TLSSni != "" || detail.TLSFingerprint != "" || len(detail.TLSALPN) > 0 || detail.AllowInsecure != nil {
			info.TLSConfig = &xray.TLSInfoConfig{
				SNI:         detail.TLSSni,
				ALPN:        detail.TLSALPN,
				Fingerprint: detail.TLSFingerprint,
			}
			if detail.AllowInsecure != nil {
				info.TLSConfig.AllowInsecure = *detail.AllowInsecure
			}
		}
	}

	// Reality Settings — only populate when security == reality.
	if strings.EqualFold(detail.Security, "reality") {
		if detail.RealityPublicKey != "" || detail.RealitySNI != "" {
			info.RealityConfig = &xray.RealityInfoConfig{
				PublicKey:   detail.RealityPublicKey,
				ShortID:     detail.RealityShortID,
				ServerName:  detail.RealitySNI,
				Fingerprint: detail.RealityFingerprint,
				SpiderX:     detail.RealitySpiderX,
			}
		}
	}

	// Transport-specific settings based on network
	switch strings.ToLower(detail.Network) {
	case "ws", "websocket":
		info.WSPath = detail.TransportPath
		info.WSHost = detail.TransportHost
	case "grpc":
		info.GRPCServiceName = detail.TransportServiceName
	case "xhttp", "splithttp":
		info.XHTTPPath = detail.TransportPath
		info.XHTTPHost = detail.TransportHost
		info.XHTTPMode = detail.TransportMode
	case "httpupgrade":
		info.HTTPUpgradePath = detail.TransportPath
		info.HTTPUpgradeHost = detail.TransportHost
	case "tcp":
		info.HeaderType = detail.TransportHeaderType
		info.HTTPPath = detail.TransportPath
	case "http", "h2":
		info.HTTPPath = detail.TransportPath
	}

	// VLESS settings
	if strings.ToLower(detail.Protocol) == "vless" {
		info.Flow = detail.VLESSFlow
		info.VLESSEncryption = detail.VLESSEncryption
		info.VLESSDecryption = detail.VLESSDecryption
	}

	// VMess settings
	if strings.ToLower(detail.Protocol) == "vmess" {
		info.VMessAlterId = detail.VMessAlterId
		info.VMessSecurity = detail.VMessSecurity
	}

	// Fragment (anti-censorship) — surfaced into the URI as fragment=...
	info.HysteriaObfsPassword = detail.HysteriaObfsPassword
	info.PortRange = detail.PortRange
	info.WGPrivateKey = detail.WGPrivateKey
	info.WGServerPublicKey = detail.WGServerPublicKey
	info.WGAddress = detail.WGAddress
	info.WGPresharedKey = detail.WGPresharedKey
	info.WGMTU = detail.WGMTU
	info.WGReserved = detail.WGReserved
	if detail.Fragment != nil {
		info.Fragment = &xray.FragmentInfoConfig{
			Packets:  detail.Fragment.Packets,
			Length:   detail.Fragment.Length,
			Interval: detail.Fragment.Interval,
		}
	}

	return info
}

// generateLink uses xray-knife to parse template and rebuild with new credentials
func (p *Provider) generateLink(format, uuid, email, host string, port int, name string) string {
	return p.linkBuilder.BuildLinkWithFallback(format, uuid, email, host, port, name)
}

// ActivateUser adds the user to Xray-core on ALL nodes (Idempotent)
func (p *Provider) ActivateUser(ctx context.Context, sub *product.SubscriptionInfo) error {
	log := logger.GetLogger()
	log.WithField("email", sub.Email).Debug("[XrayProvider] Activating user on nodes")

	for _, inbound := range sub.Inbounds {
		if inbound.IsInfoOnly {
			continue
		}
		var protocol xray.Protocol
		switch strings.ToLower(inbound.Protocol) {
		case "vmess":
			protocol = xray.ProtocolVMess
		case "vless":
			protocol = xray.ProtocolVLESS
		case "trojan":
			protocol = xray.ProtocolTrojan
		case "hysteria2", "hysteria":
			protocol = xray.ProtocolHysteria2
		default:
			continue
		}

		bwTier := bandwidth.GetTier(sub.BandwidthLimit)

		user := &xray.User{
			Email:    sub.Email,
			UUID:     sub.ConfigID,
			Protocol: protocol,
			Level:    bwTier.Level,
		}
		if protocol == xray.ProtocolVLESS {
			if inbound.VLESSFlow != "" {
				user.Flow = inbound.VLESSFlow
			}
			user.Encryption = inbound.VLESSEncryption
		}

		client, err := p.getNodeClient(ctx, inbound.NodeID)
		if err != nil {
			log.WithError(err).WithField("node_id", inbound.NodeID).Warn("[XrayProvider] Failed to get agent client for ActivateUser, skipping")
			continue
		}
		_ = client.AddUser(ctx, inbound.Tag, sub.Email, sub.ConfigID, string(protocol), user.Flow, user.Encryption, int32(user.Level))
		client.Close()
	}
	// Keyed by sub ID and a no-op when the sub has no peers, so don't gate on
	// hasWireGuard — that misses WG inbounds attached directly to the sub
	// (manual accounts), not just plan inbounds.
	if p.wgProvisioner != nil && sub.ID != 0 {
		_ = p.wgProvisioner.ActivateSubscription(ctx, sub.ID)
	}
	return nil
}

// DeactivateUser removes the user from Xray-core on ALL nodes
func (p *Provider) DeactivateUser(ctx context.Context, sub *product.SubscriptionInfo) error {
	log := logger.GetLogger()
	log.WithField("email", sub.Email).Info("[XrayProvider] Deactivating user on nodes")

	for _, inbound := range sub.Inbounds {
		if inbound.IsInfoOnly {
			continue
		}
		client, err := p.getNodeClient(ctx, inbound.NodeID)
		if err != nil {
			log.WithError(err).WithField("node_id", inbound.NodeID).Warn("[XrayProvider] Failed to get agent client for DeactivateUser, skipping")
			continue
		}
		_ = client.RemoveUser(ctx, inbound.Tag, sub.Email)
		client.Close()
	}
	// See ActivateUser: keyed by sub ID, no-op without peers, so no hasWireGuard gate.
	if p.wgProvisioner != nil && sub.ID != 0 {
		_ = p.wgProvisioner.DeactivateSubscription(ctx, sub.ID)
	}
	return nil
}

// GetUsageStats retrieves usage statistics (Aggregate from ALL nodes)
func (p *Provider) GetUsageStats(ctx context.Context, sub *product.SubscriptionInfo) (*product.UsageStats, error) {
	log := logger.GetLogger()
	totalStats := &product.UsageStats{}

	for _, inbound := range sub.Inbounds {
		if inbound.IsInfoOnly {
			continue
		}
		client, err := p.getNodeClient(ctx, inbound.NodeID)
		if err != nil {
			log.WithError(err).WithField("node_id", inbound.NodeID).Debug("[XrayProvider] GetUsageStats: failed to get agent client")
			continue
		}
		stats, err := client.GetXrayStats(ctx, true)
		client.Close()
		if err != nil {
			log.WithError(err).WithField("node_id", inbound.NodeID).Debug("[XrayProvider] GetUsageStats: failed to get stats from agent")
			continue
		}
		if up, ok := stats.UserUplink[sub.Email]; ok {
			totalStats.Uplink += up
		}
		if down, ok := stats.UserDownlink[sub.Email]; ok {
			totalStats.Downlink += down
		}
		totalStats.Total = totalStats.Uplink + totalStats.Downlink
	}

	return totalStats, nil
}

// ValidateConfig validates Xray-specific configuration
func (p *Provider) ValidateConfig(config string) error {
	return nil
}

// Helper: Generate usage summary string
func (p *Provider) generateStatsString(used, limit int64) string {
	if limit == 0 {
		return "♾️"
	}

	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}

	// Calculate percentage for a bar (Optional visual)
	// percent := float64(used) / float64(limit) * 100

	// Readable Format
	const (
		GB = 1024 * 1024 * 1024
		MB = 1024 * 1024
	)

	remGB := float64(remaining) / float64(GB)
	if remGB >= 1.0 {
		return fmt.Sprintf("%.1f GB Left", remGB)
	}

	remMB := float64(remaining) / float64(MB)
	return fmt.Sprintf("%.0f MB Left", remMB)
}

// Helper: Convert country code to Flag Emoji
func getFlagEmoji(countryCode string) string {
	if len(countryCode) != 2 {
		return "🌍" // Default Globe
	}
	countryCode = strings.ToUpper(countryCode)
	// Flag calculation: Regional Indicator Symbol A (U+1F1E6) is 127462 decimal.
	// 'A' is 65. So 127462 - 65 = 127397.
	return string(rune(countryCode[0])+127397) + string(rune(countryCode[1])+127397)
}

// enrichLinkWithInboundInfo parses a generated link and ensures critical security/transport parameters
// from InboundInfo are present, overriding if necessary. This handles cases where custom
// LinkFormats are incomplete (e.g. missing security=tls).
func (p *Provider) enrichLinkWithInboundInfo(link string, info *xray.InboundInfo) string {
	u, err := url.Parse(link)
	if err != nil {
		return link // Return original if parsing fails
	}

	// hysteria2 uses a different query (insecure, obfs, …)
	if s := strings.ToLower(u.Scheme); s == "hysteria2" || s == "hysteria" {
		return link
	}

	q := u.Query()
	changed := false

	// Enforce critical security params.

	if info.Security == "tls" {
		if q.Get("security") != "tls" {
			q.Set("security", "tls")
			changed = true
		}
		if info.TLSConfig != nil {
			if info.TLSConfig.SNI != "" && q.Get("sni") == "" {
				q.Set("sni", info.TLSConfig.SNI)
				changed = true
			}
			if len(info.TLSConfig.ALPN) > 0 && q.Get("alpn") == "" {
				q.Set("alpn", strings.Join(info.TLSConfig.ALPN, ","))
				changed = true
			}
			if info.TLSConfig.Fingerprint != "" && q.Get("fp") == "" {
				q.Set("fp", info.TLSConfig.Fingerprint)
				changed = true
			}
			if info.TLSConfig.AllowInsecure && q.Get("allowInsecure") == "" {
				q.Set("allowInsecure", "1")
				changed = true
			}
		}
	} else if info.Security == "reality" {
		if q.Get("security") != "reality" {
			q.Set("security", "reality")
			changed = true
		}
		if info.RealityConfig != nil {
			if info.RealityConfig.ServerName != "" && q.Get("sni") == "" {
				q.Set("sni", info.RealityConfig.ServerName)
				changed = true
			}
			if info.RealityConfig.PublicKey != "" && q.Get("pbk") == "" {
				q.Set("pbk", info.RealityConfig.PublicKey)
				changed = true
			}
			if info.RealityConfig.ShortID != "" && q.Get("sid") == "" {
				q.Set("sid", info.RealityConfig.ShortID)
				changed = true
			}
			if info.RealityConfig.Fingerprint != "" && q.Get("fp") == "" {
				q.Set("fp", info.RealityConfig.Fingerprint)
				changed = true
			}
			if info.RealityConfig.SpiderX != "" && q.Get("spx") == "" {
				q.Set("spx", info.RealityConfig.SpiderX)
				changed = true
			}
		}
	}

	// Fragment (anti-censorship)
	if info.Fragment != nil && q.Get("fragment") == "" {
		parts := []string{info.Fragment.Packets, info.Fragment.Length, info.Fragment.Interval}
		for _, p := range parts {
			if p != "" {
				q.Set("fragment", strings.Join(parts, ","))
				changed = true
				break
			}
		}
	}

	// Transport (Path/Host for WS/XHTTP/HTTP Upgrade)
	// We only inject if missing to allow overrides in template
	switch info.Network {
	case "ws":
		if info.WSPath != "" && q.Get("path") == "" {
			q.Set("path", info.WSPath)
			changed = true
		}
		if info.WSHost != "" && q.Get("host") == "" {
			q.Set("host", info.WSHost)
			changed = true
		}
	case "xhttp", "splithttp":
		if info.XHTTPPath != "" && q.Get("path") == "" {
			q.Set("path", info.XHTTPPath)
			changed = true
		}
		if info.XHTTPHost != "" && q.Get("host") == "" {
			q.Set("host", info.XHTTPHost)
			changed = true
		}
		if info.XHTTPMode != "" && q.Get("mode") == "" {
			q.Set("mode", info.XHTTPMode)
			changed = true
		}
	case "httpupgrade":
		if info.HTTPUpgradePath != "" && q.Get("path") == "" {
			q.Set("path", info.HTTPUpgradePath)
			changed = true
		}
		if info.HTTPUpgradeHost != "" && q.Get("host") == "" {
			q.Set("host", info.HTTPUpgradeHost)
			changed = true
		}
	case "grpc":
		if info.GRPCServiceName != "" && q.Get("serviceName") == "" {
			q.Set("serviceName", info.GRPCServiceName)
			changed = true
		}
	}

	if changed {
		u.RawQuery = q.Encode()
		return u.String()
	}

	return link
}
