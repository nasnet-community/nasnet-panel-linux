package http

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/analytics"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/cache"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	xrayProvider "github.com/nasnet-community/nasnet-panel-linux/pkg/product/xray"
	tg "github.com/nasnet-community/nasnet-panel-linux/pkg/telegram"
)

// panelServer is a single server entry in the panel response.
type panelServer struct {
	Name            string     `json:"name"`
	CountryCode     string     `json:"country_code"`
	Flag            string     `json:"flag"`
	Protocol        string     `json:"protocol"`
	Network         string     `json:"network"`
	Security        string     `json:"security"`
	Address         string     `json:"address"`
	Port            int        `json:"port"`
	Link            string     `json:"link"`
	IsOnline        bool       `json:"is_online"`
	LastActivityAt  *time.Time `json:"last_activity_at"`
	AccountEmail    string     `json:"account_email"`
	DataUsed        int64      `json:"data_used"`
	DataUsedDisplay string     `json:"data_used_display"`
}

// panelData is the JSON payload returned by the public sub panel API.
type panelData struct {
	Status               string        `json:"status"`
	Label                string        `json:"label"`
	PlanName             string        `json:"plan_name"`
	PlanDuration         int           `json:"plan_duration"`
	ProductType          string        `json:"product_type"`
	DataUsed             int64         `json:"data_used"`
	DataLimit            int64         `json:"data_limit"`
	DataUsedDisplay      string        `json:"data_used_display"`
	DataLimitDisplay     string        `json:"data_limit_display"`
	DataRemainingDisplay string        `json:"data_remaining_display"`
	DataUsagePercent     float64       `json:"data_usage_percent"`
	IsUnlimited          bool          `json:"is_unlimited"`
	DaysRemaining        int           `json:"days_remaining"`
	TimeRemaining        string        `json:"time_remaining"`
	TimeUsedPercent      float64       `json:"time_used_percent"`
	StartDate            *time.Time    `json:"start_date"`
	EndDate              *time.Time    `json:"end_date"`
	IsCustomExpiry       bool          `json:"is_custom_expiry"`
	IsCustomDataLimit    bool          `json:"is_custom_data_limit"`
	TelegramChatID       int64         `json:"telegram_chat_id"`
	TelegramConnected    bool          `json:"telegram_connected"`
	SubscriptionURL      string        `json:"subscription_url"`
	ConfigIDMasked       string        `json:"config_id_masked"`
	CreatedAt            time.Time     `json:"created_at"`
	IsOnline             bool          `json:"is_online"`
	OnlineCount          int           `json:"online_count"`
	OnlineIPs            []string      `json:"online_ips,omitempty"`
	LastActiveAt         *time.Time    `json:"last_active_at"`
	Servers              []panelServer `json:"servers"`
	ChatEnabled          bool          `json:"chat_enabled"`
	TelegramBotUsername  string        `json:"telegram_bot_username,omitempty"`
}

// GetSubPanel handles GET /api/v1/public/sub/:uuid
// Returns JSON with all subscription and server data for the panel UI.
func (h *Handler) GetSubPanel(c *gin.Context) {
	uuid := c.Param("uuid")

	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	data := h.buildFullPanelData(c.Request.Context(), sub, h.resolvePanelSettings(c.Request.Context()))
	if data == nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}

	httputil.OK(c, data)
}

// StreamSubPanel handles GET /api/v1/public/sub/:uuid/events
// Sends SSE updates every 5 seconds with full panel data.
func (h *Handler) StreamSubPanel(c *gin.Context) {
	uuid := c.Param("uuid")
	ctx := c.Request.Context()

	// Check auth before starting SSE stream
	sub, err := h.subUsecase.GetByConfigID(ctx, uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	// Resolve settings once for the lifetime of this stream (they don't change per tick).
	settings := h.resolvePanelSettings(ctx)

	// Validate that the subscription exists before committing to SSE
	initial := h.buildFullPanelData(ctx, sub, settings)
	if initial == nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}

	// Remove the server's WriteTimeout deadline for this long-lived SSE connection
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetWriteDeadline(time.Time{})

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// Send initial update immediately so the client doesn't wait for the first tick
	if data, err := json.Marshal(initial); err == nil {
		fmt.Fprintf(c.Writer, "event: update\ndata: %s\n\n", data)
		rc.Flush()
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ticker.C:
			freshSub, err := h.subUsecase.GetByConfigID(ctx, uuid)
			if err != nil {
				continue
			}
			panel := h.buildFullPanelData(ctx, freshSub, settings)
			if panel == nil {
				continue
			}
			data, err := json.Marshal(panel)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: update\ndata: %s\n\n", data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-ctx.Done():
			return
		case <-h.shutdownCtx.Done():
			return
		}
	}
}

// panelSettings holds per-request settings used by the panel response.
// Resolved once per request (or once per SSE stream) instead of every tick.
type panelSettings struct {
	chatEnabled bool
	botUsername string
}

func (h *Handler) resolvePanelSettings(ctx context.Context) panelSettings {
	var s panelSettings
	if h.settingUC == nil {
		return s
	}
	if val, err := h.settingUC.GetByKey(ctx, "chat_enabled"); err == nil {
		s.chatEnabled = strings.EqualFold(val, "true")
	}
	if val, err := h.settingUC.GetByKey(ctx, "telegram_bot_username"); err == nil && val != "" {
		s.botUsername = val
	}
	return s
}

// buildFullPanelData constructs the full panel response for an already-fetched
// subscription. Shared by the REST endpoint and the SSE stream.
func (h *Handler) buildFullPanelData(ctx context.Context, sub *domain.Subscription, settings panelSettings) *panelData {
	if sub == nil {
		return nil
	}

	// Build subscription metadata
	effectiveLimit := sub.GetEffectiveDataLimit()
	effectiveEnd := sub.GetEffectiveEndDate()
	isUnlimited := effectiveLimit == 0

	dataUsedDisplay := domain.FormatBytes(sub.DataUsed)
	var dataLimitDisplay, dataRemainingDisplay string
	if isUnlimited {
		dataLimitDisplay = "Unlimited"
		dataRemainingDisplay = "Unlimited"
	} else {
		dataLimitDisplay = domain.FormatBytes(effectiveLimit)
		remaining := effectiveLimit - sub.DataUsed
		if remaining < 0 {
			remaining = 0
		}
		dataRemainingDisplay = domain.FormatBytes(remaining)
	}

	usagePercent := sub.GetDataUsagePercentage()
	usagePercent = math.Round(usagePercent*10) / 10

	planName := sub.Label
	planDuration := 0

	// Compute time used percentage
	var timeUsedPercent float64
	if sub.StartDate != nil && effectiveEnd != nil {
		totalDuration := effectiveEnd.Sub(*sub.StartDate).Hours()
		elapsed := time.Since(*sub.StartDate).Hours()
		if totalDuration > 0 {
			timeUsedPercent = math.Min((elapsed/totalDuration)*100, 100)
			timeUsedPercent = math.Round(timeUsedPercent*10) / 10
		}
	}

	// Build server list with online status
	servers, isOnline, onlineCount := h.buildPanelServersWithStatus(ctx, sub)

	// Collect online IPs from cache for this subscription's config email
	var onlineIPs []string
	if sub.ConfigEmail != "" {
		ips := cache.GetUserOnlineIPs(sub.ConfigEmail)
		for ip := range ips {
			onlineIPs = append(onlineIPs, ip)
		}
	}

	data := &panelData{
		Status:               string(sub.Status),
		Label:                sub.GetDisplayName(),
		PlanName:             planName,
		PlanDuration:         planDuration,
		ProductType:          string(sub.ProductType),
		DataUsed:             sub.DataUsed,
		DataLimit:            effectiveLimit,
		DataUsedDisplay:      dataUsedDisplay,
		DataLimitDisplay:     dataLimitDisplay,
		DataRemainingDisplay: dataRemainingDisplay,
		DataUsagePercent:     usagePercent,
		IsUnlimited:          isUnlimited,
		DaysRemaining:        sub.DaysRemaining(),
		TimeRemaining:        sub.TimeRemainingFormatted(),
		TimeUsedPercent:      timeUsedPercent,
		StartDate:            sub.StartDate,
		EndDate:              effectiveEnd,
		IsCustomExpiry:       sub.IsEndDateCustom,
		IsCustomDataLimit:    sub.IsDataLimitCustom,
		TelegramChatID:       sub.TelegramChatID,
		TelegramConnected:    telegramReachable(sub),
		SubscriptionURL:      h.getBaseURL(ctx) + "/sub/" + sub.GetLinkKey(),
		ConfigIDMasked:       maskConfigID(sub.ConfigID),
		CreatedAt:            sub.CreatedAt,
		IsOnline:             isOnline,
		OnlineCount:          onlineCount,
		OnlineIPs:            onlineIPs,
		LastActiveAt:         sub.LastActiveAt,
		Servers:              servers,
	}

	data.ChatEnabled = settings.chatEnabled
	data.TelegramBotUsername = settings.botUsername

	return data
}

// telegramReachable reports whether the subscription can receive Telegram
// notifications — either via an explicit per-sub chat ID or by falling back to
// the owner's Telegram account. This mirrors the recipient resolution in the
// notification scheduler so the panel's "connected" state matches reality.
// Requires the subscription's User relation to be preloaded.
func telegramReachable(sub *domain.Subscription) bool {
	if sub == nil {
		return false
	}
	if sub.TelegramChatID > 0 {
		return true
	}
	// User.TelegramID > 0 excludes the negative placeholder assigned to
	// admin-created users without a real Telegram account.
	return sub.User != nil && sub.User.TelegramID > 0
}

// buildPanelServersWithStatus collects active inbounds from the subscription's plan,
// generates client links, and enriches each server with online status from accounts.
func (h *Handler) buildPanelServersWithStatus(ctx context.Context, sub *domain.Subscription) ([]panelServer, bool, int) {
	// Load accounts for this subscription to get per-server online status
	accountMap := make(map[uint]struct {
		uuid           string
		email          string
		nodeID         uint
		lastActivityAt *time.Time
		dataUsed       int64
	})

	var accounts []*accountDomain.Account
	if h.accountUsecase != nil {
		if a, err := h.accountUsecase.ListAccountsBySubscription(ctx, sub.ID); err == nil {
			accounts = a
		}
	}
	for _, acc := range accounts {
		var nodeID uint
		if acc.Inbound != nil {
			nodeID = acc.Inbound.NodeID
		}
		accountMap[acc.InboundID] = struct {
			uuid           string
			email          string
			nodeID         uint
			lastActivityAt *time.Time
			dataUsed       int64
		}{
			uuid:           acc.UUID,
			email:          acc.Email,
			nodeID:         nodeID,
			lastActivityAt: acc.LastActivityAt,
			dataUsed:       acc.DataUsed,
		}
	}

	// Collect inbounds from the plan AND from accounts attached directly to the
	// sub (manual attach), deduped — so manually-added inbounds also show.
	type inboundEntry struct {
		inbound *nodeDomain.Inbound
	}
	var inboundEntries []inboundEntry
	seenInbound := make(map[uint]bool)
	addInboundEntry := func(in *nodeDomain.Inbound) {
		if in == nil || seenInbound[in.ID] {
			return
		}
		seenInbound[in.ID] = true
		inboundEntries = append(inboundEntries, inboundEntry{inbound: in})
	}

	for _, acc := range accounts {
		addInboundEntry(acc.Inbound)
	}

	servers := make([]panelServer, 0)
	anyOnline := false
	onlineCount := 0

	// Pre-compute remark template context data
	dataLeftStr := "♾️"
	dataLimitStr := "♾️"
	dataUsedStr := domain.FormatBytes(sub.DataUsed)
	usagePercentStr := "0%"
	effectiveLimit := sub.GetEffectiveDataLimit()
	if effectiveLimit > 0 {
		remaining := effectiveLimit - sub.DataUsed
		if remaining < 0 {
			remaining = 0
		}
		dataLeftStr = domain.FormatBytes(remaining)
		dataLimitStr = domain.FormatBytes(effectiveLimit)
		pct := sub.GetDataUsagePercentage()
		usagePercentStr = fmt.Sprintf("%.0f%%", pct)
	}
	daysLeft := sub.DaysRemaining()
	daysLeftStr := "∞"
	if daysLeft >= 0 {
		daysLeftStr = fmt.Sprintf("%d", daysLeft)
	}

	statusEmojiStr := product.StatusEmoji(string(sub.Status))

	for _, entry := range inboundEntries {
		in := entry.inbound
		if in.IsDisabled || in.Node == nil || !in.Node.IsActive {
			continue
		}

		// Build base detail
		nodeIP := in.Node.IP
		if in.Address != "" {
			nodeIP = in.Address
		}

		baseDetail := product.InboundDetail{
			NodeID:      in.Node.ID,
			Tag:         in.Tag,
			Protocol:    in.Protocol,
			LinkFormat:  in.LinkFormat,
			NodeIP:      nodeIP,
			APIPort:     in.Node.APIPort,
			PublicPort:  in.Port,
			Remark:      fmt.Sprintf("%s - %s", in.Node.Name, in.Remark),
			NodeName:    in.Node.Name,
			CountryCode: in.Node.CountryCode,
			Network:     in.Network,
			Security:    in.Security,
		}

		// Populate protocol-specific settings
		if strings.ToLower(in.Protocol) == "vless" {
			vless := in.GetVLESSSettingsOrDefault()
			baseDetail.VLESSFlow = vless.Flow
			baseDetail.VLESSEncryption = vless.Encryption
			baseDetail.VLESSDecryption = vless.Decryption
		}
		if strings.ToLower(in.Protocol) == "vmess" {
			if vmess := in.GetVMessSettingsOrDefault(); vmess != nil {
				if vmess.AlterId > 0 {
					baseDetail.VMessAlterId = uint32(vmess.AlterId)
				}
				baseDetail.VMessSecurity = vmess.Security
			}
		}

		if tls := in.GetTLSSettingsOrDefault(); tls != nil {
			baseDetail.TLSSni = tls.ServerName
			baseDetail.TLSALPN = tls.ALPN
			baseDetail.TLSFingerprint = tls.Fingerprint
		}

		if reality := in.GetRealitySettingsOrDefault(); reality != nil {
			baseDetail.RealityPublicKey = reality.PublicKey
			baseDetail.RealityShortID = reality.ShortID
			if len(reality.ServerNames) > 0 {
				baseDetail.RealitySNI = reality.ServerNames[0]
			}
			baseDetail.RealityFingerprint = reality.Fingerprint
			baseDetail.RealitySpiderX = reality.SpiderX
		}

		if transport := in.GetTransportSettingsOrDefault(); transport != nil {
			baseDetail.TransportPath = transport.Path
			baseDetail.TransportHost = transport.Host
			baseDetail.TransportServiceName = transport.ServiceName
			baseDetail.TransportHeaderType = transport.HeaderType
			baseDetail.TransportMode = transport.Mode
		}

		// Host expansion: if the inbound has active hosts, produce one entry per host
		var expandedDetails []product.InboundDetail
		activeHosts := in.GetActiveHosts()
		if len(activeHosts) == 0 {
			expandedDetails = append(expandedDetails, baseDetail)
		} else {
			for i := range activeHosts {
				d := baseDetail
				product.ApplyHostOverrides(&d, &activeHosts[i])
				expandedDetails = append(expandedDetails, d)
			}
		}

		for _, d := range expandedDetails {
			// Render remark template if needed
			displayName := d.Remark
			if d.RemarkIsTemplate {
				displayName = product.RenderRemark(d.Remark, product.RemarkContext{
					Flag:         getFlagEmoji(d.CountryCode),
					Country:      d.CountryCode,
					CountryCode:  d.CountryCode,
					Node:         d.NodeName,
					Port:         d.PublicPort,
					Protocol:     d.Protocol,
					Network:      d.Network,
					Security:     d.Security,
					DataUsed:     dataUsedStr,
					DataLeft:     dataLeftStr,
					DaysLeft:     daysLeftStr,
					DataLimit:    dataLimitStr,
					UsagePercent: usagePercentStr,
					StatusEmoji:  statusEmojiStr,
				})
			}

			// Use rendered remark in the generated link so clients show the
			// resolved name instead of raw template variables like {node}.
			d.Remark = displayName

			// Use account UUID for link generation (may differ from ConfigID after key regeneration)
			linkUUID := sub.ConfigID
			var serverOnline bool
			var lastActivity *time.Time
			var maskedEmail string
			var serverDataUsed int64
			if accInfo, ok := accountMap[in.ID]; ok {
				if accInfo.uuid != "" {
					linkUUID = accInfo.uuid
				}
				// Check online status per-node so that being connected on one server
				// doesn't make all servers show as online.
				serverOnline = cache.IsOnlineOnNode(accInfo.email, accInfo.nodeID)
				lastActivity = accInfo.lastActivityAt
				maskedEmail = maskEmail(accInfo.email)
				serverDataUsed = accInfo.dataUsed
				if serverOnline {
					anyOnline = true
					onlineCount++
				}
			}

			link := xrayProvider.ServerLink(d, linkUUID, d.NodeIP)
			flag := getFlagEmoji(d.CountryCode)

			servers = append(servers, panelServer{
				Name:            displayName,
				CountryCode:     d.CountryCode,
				Flag:            flag,
				Protocol:        strings.ToUpper(d.Protocol),
				Network:         d.Network,
				Security:        d.Security,
				Address:         d.NodeIP,
				Port:            d.PublicPort,
				Link:            link,
				IsOnline:        serverOnline,
				LastActivityAt:  lastActivity,
				AccountEmail:    maskedEmail,
				DataUsed:        serverDataUsed,
				DataUsedDisplay: domain.FormatBytes(serverDataUsed),
			})
		}
	}

	return servers, anyOnline, onlineCount
}

type updateTelegramChatIDRequest struct {
	ChatID *int64 `json:"chat_id"`
}

// UpdateTelegramChatID handles PUT /api/v1/public/sub/:uuid/telegram-chat-id
func (h *Handler) UpdateTelegramChatID(c *gin.Context) {
	uuid := c.Param("uuid")

	// Check auth
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	var req updateTelegramChatIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ChatID == nil {
		httputil.Error(c, http.StatusBadRequest, "chat_id is required")
		return
	}

	if err := h.subUsecase.UpdateTelegramChatIDByConfigID(c.Request.Context(), uuid, *req.ChatID); err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}

	httputil.OK(c, map[string]string{"message": "telegram chat id updated"})
}

// GenerateTelegramLinkToken handles POST /api/v1/public/sub/:uuid/telegram-link-token.
// It returns a short-lived deep link the customer taps to bind their Telegram
// account to this subscription. The bot reads the real sender's ID from the
// signed token, so the chat ID is verified — never typed.
func (h *Handler) GenerateTelegramLinkToken(c *gin.Context) {
	uuid := c.Param("uuid")
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}
	if h.authSecret == "" {
		httputil.Error(c, http.StatusServiceUnavailable, "telegram linking unavailable")
		return
	}
	username, err := h.settingUC.GetByKey(c.Request.Context(), "telegram_bot_username")
	if err != nil || strings.TrimSpace(username) == "" {
		httputil.Error(c, http.StatusServiceUnavailable, "telegram bot not configured")
		return
	}
	token := tg.SignLinkToken(uint64(sub.ID), h.authSecret, 15*time.Minute)
	url := fmt.Sprintf("https://t.me/%s?start=%s", strings.TrimPrefix(strings.TrimSpace(username), "@"), token)
	httputil.OK(c, map[string]string{"url": url})
}

// hourlyUsagePoint is the public response type for usage pattern data.
type hourlyUsagePoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

// GetSubExhaustionPrediction handles GET /api/v1/public/sub/:uuid/exhaustion-prediction
func (h *Handler) GetSubExhaustionPrediction(c *gin.Context) {
	uuid := c.Param("uuid")
	ctx := c.Request.Context()

	sub, err := h.subUsecase.GetByConfigID(ctx, uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	records, err := h.subUsecase.ListDailyUsageRecords(ctx, sub.ID, 30)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "Failed to load usage history")
		return
	}

	pred := analytics.ComputeExhaustion(sub, records)

	httputil.OK(c, pred)
}

// GetSubUsagePattern handles GET /api/v1/public/sub/:uuid/usage-pattern
func (h *Handler) GetSubUsagePattern(c *gin.Context) {
	uuid := c.Param("uuid")

	// Check auth
	sub, err := h.subUsecase.GetByConfigID(c.Request.Context(), uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	points, err := h.subUsecase.GetSubscriptionUsagePattern(c.Request.Context(), uuid, 30)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}

	// Convert to response type
	result := make([]hourlyUsagePoint, len(points))
	for i, p := range points {
		result[i] = hourlyUsagePoint{Hour: p.Hour, Count: p.Count}
	}

	httputil.OK(c, result)
}

// trendPointJSON is the per-day entry in a usage-trend response.
type trendPointJSON struct {
	Date     string `json:"date"`
	Upload   *int64 `json:"upload"`
	Download *int64 `json:"download"`
	Total    int64  `json:"total"`
}

// GetSubUsageTrend handles GET /api/v1/public/sub/:uuid/usage-trend
func (h *Handler) GetSubUsageTrend(c *gin.Context) {
	uuid := c.Param("uuid")
	ctx := c.Request.Context()

	// Parse and validate range query parameter
	rangeParam := c.DefaultQuery("range", "7d")
	var rangeDays int
	switch rangeParam {
	case "7d":
		rangeDays = 7
	case "30d":
		rangeDays = 30
	default:
		httputil.Error(c, http.StatusBadRequest, "invalid range: use 7d or 30d")
		return
	}

	sub, err := h.subUsecase.GetByConfigID(ctx, uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	trend, err := h.subUsecase.GetSubscriptionUsageTrend(ctx, sub.ID, rangeDays)
	if err != nil {
		httputil.Error(c, http.StatusInternalServerError, "Failed to load usage trend")
		return
	}

	points := make([]trendPointJSON, len(trend.Points))
	for i, p := range trend.Points {
		points[i] = trendPointJSON{
			Date:     p.Date.Format("2006-01-02"),
			Upload:   p.Upload,
			Download: p.Download,
			Total:    p.Total,
		}
	}

	out := struct {
		Range    string           `json:"range"`
		Points   []trendPointJSON `json:"points"`
		UnitHint string           `json:"unit_hint"`
	}{
		Range:    rangeParam,
		Points:   points,
		UnitHint: trend.UnitHint,
	}

	c.Header("Cache-Control", "max-age=60")
	httputil.OK(c, out)
}

// GetIPGeolocation handles GET /api/v1/public/sub/:uuid/ip-geo
// Returns geolocation data for the subscription's connected IPs using GeoLite2.
func (h *Handler) GetIPGeolocation(c *gin.Context) {
	uuid := c.Param("uuid")
	ctx := c.Request.Context()

	sub, err := h.subUsecase.GetByConfigID(ctx, uuid)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, "Subscription not found")
		return
	}
	if !h.checkSubAuth(c, sub) {
		return
	}

	// Collect online IPs from cache
	var ips []string
	if sub.ConfigEmail != "" {
		onlineIPs := cache.GetUserOnlineIPs(sub.ConfigEmail)
		for ip := range onlineIPs {
			ips = append(ips, ip)
		}
	}

	if len(ips) == 0 || !geoip.GeoLite2Available() {
		httputil.OK(c, map[string]*geoip.CityLocation{})
		return
	}

	results := geoip.LookupCityBatch(ips)

	httputil.OK(c, results)
}

// maskEmail masks an email for display (e.g., "user123...@sub")
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		if len(email) <= 6 {
			return email
		}
		return email[:6] + "..."
	}
	local := email[:at]
	domainPart := email[at:]
	if len(local) <= 6 {
		return local + "..." + domainPart
	}
	return local[:6] + "..." + domainPart
}

// maskConfigID returns a partially masked config ID for display.
func maskConfigID(configID string) string {
	if len(configID) <= 12 {
		return configID
	}
	return configID[:8] + "..." + configID[len(configID)-4:]
}

// getFlagEmoji converts a 2-letter country code to a flag emoji.
func getFlagEmoji(countryCode string) string {
	if len(countryCode) != 2 {
		return "🌍"
	}
	countryCode = strings.ToUpper(countryCode)
	return string(rune(countryCode[0])+127397) + string(rune(countryCode[1])+127397)
}
