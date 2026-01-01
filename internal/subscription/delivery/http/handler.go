package http

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	accountUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/account/usecase"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
)

type Handler struct {
	subUsecase     usecase.SubscriptionUsecase
	accountUsecase accountUsecase.AccountUsecase
	deviceUC       wireguardUC.DeviceUsecase // optional: enables WireGuard device mgmt on the panel
	settingUC      settingDomain.SettingUsecase
	baseURL        string
	subPanelURL    string          // When set, browser requests to /sub/:uuid redirect here
	shutdownCtx    context.Context // Canceled on server shutdown to unblock SSE streams
	authSecret     string          // HMAC signing secret for sub panel auth tokens
	spaFS          fs.FS           // embedded SPA filesystem (nil if not embedded)
	spaConfig      string          // runtime config JSON for SPA
	spaBasePath    string
}

func NewHandler(subUsecase usecase.SubscriptionUsecase, accountUsecase accountUsecase.AccountUsecase, baseURL, subPanelURL string, shutdownCtx context.Context, settingUC settingDomain.SettingUsecase, authSecret string) *Handler {
	return &Handler{
		subUsecase:     subUsecase,
		accountUsecase: accountUsecase,
		settingUC:      settingUC,
		baseURL:        baseURL,
		subPanelURL:    subPanelURL,
		shutdownCtx:    shutdownCtx,
		authSecret:     authSecret,
	}
}

// SetDeviceUsecase wires the WireGuard device usecase, enabling the public
// panel's device-management endpoints. When nil, those routes aren't registered
// (parity with the mini-app, which gates the same way).
func (h *Handler) SetDeviceUsecase(deviceUC wireguardUC.DeviceUsecase) {
	h.deviceUC = deviceUC
}

// SPAConfig holds the embedded filesystem and runtime config for SPA serving.
type SPAConfig struct {
	DistFS        fs.FS
	RuntimeConfig string
	BasePath      string
}

// SetSPAServing enables the handler to serve the SPA for browser requests to /sub/:uuid.
func (h *Handler) SetSPAServing(cfg *SPAConfig) {
	if cfg != nil {
		h.spaFS = cfg.DistFS
		h.spaConfig = cfg.RuntimeConfig
		h.spaBasePath = cfg.BasePath
	}
}

// serveIndex reads index.html and injects runtime config.
// Asset path rewriting is inlined here because importing transport/http
// would create a circular dependency.
func (h *Handler) serveIndex(w http.ResponseWriter) {
	data, err := fs.ReadFile(h.spaFS, "index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	html := strings.Replace(string(data),
		`"__RUNTIME_CONFIG_PLACEHOLDER__"`, h.spaConfig, 1)

	// Rewrite relative asset paths to absolute so they resolve correctly
	// when served from /sub/:uuid (otherwise ./assets/ becomes /sub/assets/)
	assetPrefix := h.spaBasePath + "/assets/"
	html = strings.ReplaceAll(html, `src="./assets/`, `src="`+assetPrefix)
	html = strings.ReplaceAll(html, `href="./assets/`, `href="`+assetPrefix)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(html))
}

// getBaseURL reads app_base_url from DB settings, falling back to the startup config value.
func (h *Handler) getBaseURL(ctx context.Context) string {
	if h.settingUC != nil {
		if v, err := h.settingUC.GetByKey(ctx, "app_base_url"); err == nil && v != "" {
			return v
		}
	}
	return h.baseURL
}

// getSubPanelURL reads sub_panel_url from DB settings, falling back to the startup config value.
func (h *Handler) getSubPanelURL(ctx context.Context) string {
	if h.settingUC != nil {
		if v, err := h.settingUC.GetByKey(ctx, "sub_panel_url"); err == nil && v != "" {
			return v
		}
	}
	return h.subPanelURL
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	subs := rg.Group("/subscriptions")
	{
		// List all subscriptions (admin)
		subs.GET("", h.ListAll)

		// Single subscription operations
		subs.GET("/:id", h.GetByID)
		subs.GET("/:id/link", h.GetSubscriptionLink)
		subs.GET("/:id/usage-details", h.GetUsageDetails)
		subs.POST("/:id/cancel", h.Cancel)
		subs.PUT("/:id/data-usage", h.UpdateDataUsage)
		subs.PUT("/:id/label", h.RenameSubscription)

		// Custom data/expiry/bandwidth management (admin)
		subs.PUT("/:id/data-limit", h.SetDataLimit)
		subs.PUT("/:id/end-date", h.SetEndDate)
		subs.PUT("/:id/bandwidth-limit", h.SetBandwidthLimit)
		subs.PUT("/:id/max-devices", h.SetMaxDevices)
		subs.POST("/:id/add-data", h.AddData)
		subs.POST("/:id/reset-data", h.ResetData)
		// Inbound assignment
		subs.POST("/:id/assign-inbound", h.AssignToInbound)
		subs.PUT("/:id/panel-password", h.SetPanelPassword)

		// User-specific subscriptions
		subs.GET("/user/:user_id", h.ListByUserID)
		subs.GET("/user/:user_id/active", h.GetActiveByUserID)

		// Regenerate UUID (replaces subscription key)
		subs.POST("/:id/regenerate", h.RegenerateUUID)

		// Sync operations
		subs.POST("/:id/sync", h.SyncUsageFromXray)

		// Maintenance operations
		subs.POST("/check-expire", h.CheckAndExpireSubscriptions)
		subs.POST("/check-expire/data", h.CheckAndExpireByDataLimit)
		subs.POST("/reconcile", h.ReconcileUsers)
	}
}

// RegisterPublicRoutes registers routes that don't require admin auth (e.g. for App updates)
func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	rg.GET("/sub/:uuid", h.GetConfigRaw) // used by V2RayNG / Streisand / etc

	// Public API for the subscription panel UI
	rg.GET("/api/v1/public/sub/:uuid", h.GetSubPanel)

	// SSE stream for real-time panel updates
	rg.GET("/api/v1/public/sub/:uuid/events", h.StreamSubPanel)

	// Public endpoint to update Telegram chat ID
	rg.PUT("/api/v1/public/sub/:uuid/telegram-chat-id", h.UpdateTelegramChatID)

	// Public endpoint to mint a deep-link token for verified Telegram binding
	rg.POST("/api/v1/public/sub/:uuid/telegram-link-token", h.GenerateTelegramLinkToken)

	// Public analytics endpoints (auth by UUID only)
	rg.GET("/api/v1/public/sub/:uuid/exhaustion-prediction", h.GetSubExhaustionPrediction)
	rg.GET("/api/v1/public/sub/:uuid/usage-pattern", h.GetSubUsagePattern)
	rg.GET("/api/v1/public/sub/:uuid/usage-trend", h.GetSubUsageTrend)
	// IP geolocation (GeoLite2 offline)
	rg.GET("/api/v1/public/sub/:uuid/ip-geo", h.GetIPGeolocation)

	// Sub panel authentication
	rg.POST("/api/v1/public/sub/:uuid/auth", h.VerifySubPassword)
	rg.DELETE("/api/v1/public/sub/:uuid/auth", h.LogoutSub)

	// WireGuard device management
	if h.deviceUC != nil {
		rg.GET("/api/v1/public/sub/:uuid/wg/servers", h.PanelWGServers)
		rg.GET("/api/v1/public/sub/:uuid/devices", h.PanelDevices)
		rg.POST("/api/v1/public/sub/:uuid/devices", h.PanelAddDevice)
		rg.POST("/api/v1/public/sub/:uuid/devices/:deviceId/rotate", h.PanelRotateDevice)
		rg.DELETE("/api/v1/public/sub/:uuid/devices/:deviceId", h.PanelRemoveDevice)
	}
}

// parseFilter reads advanced filter query parameters and returns a SubscriptionFilter.
func (h *Handler) parseFilter(c *gin.Context) repository.SubscriptionFilter {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	f := repository.SubscriptionFilter{
		Offset: (page - 1) * perPage,
		Limit:  perPage,
		Status: c.Query("status"),
		Search: c.Query("search"),
	}

	if v := c.Query("source"); v != "" {
		switch v {
		case "manual":
			t := true
			f.IsManual = &t
		case "plan":
			t := false
			f.IsManual = &t
		}
	}

	if v := c.Query("exhausted"); v != "" {
		switch v {
		case "true":
			t := true
			f.Exhausted = &t
		case "false":
			t := false
			f.Exhausted = &t
		}
	}

	f.Sort = c.Query("sort")
	f.Order = c.Query("order")

	return f
}

// ListAll returns all subscriptions with optional filters and pagination
func (h *Handler) ListAll(c *gin.Context) {
	filter := h.parseFilter(c)

	page := (filter.Offset / filter.Limit) + 1

	subs, total, err := h.subUsecase.ListAllFilteredSubscriptions(c.Request.Context(), filter)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}

	// Populate SubscriptionURL
	for _, sub := range subs {
		sub.SubscriptionURL = h.getBaseURL(c.Request.Context()) + "/sub/" + sub.GetLinkKey()
	}

	totalPages := int(total) / filter.Limit
	if int(total)%filter.Limit > 0 {
		totalPages++
	}

	httputil.Paged(c, subs, &httputil.Meta{
		Page:       page,
		PerPage:    filter.Limit,
		Total:      int(total),
		TotalPages: totalPages,
	})
}

func (h *Handler) GetByID(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	sub, err := h.subUsecase.GetByID(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c.Request.Context()) + "/sub/" + sub.GetLinkKey()

	httputil.OK(c, sub)
}

// checkUserOwnership parses the user_id URL param and verifies that the
// authenticated user either owns the resource or is an admin. It writes
// the appropriate error response and returns (0, false) on failure so
// callers can simply "return" immediately.
func (h *Handler) checkUserOwnership(c *gin.Context) (uint, bool) {
	requestedUserID, ok := httputil.ParamUint(c, "user_id")
	if !ok {
		return 0, false
	}

	authUserID, _ := c.Get("user_id")
	isAdmin, _ := c.Get("is_admin")

	if authUserID != requestedUserID && isAdmin != true {
		httputil.Error(c, http.StatusForbidden, "access denied")
		return 0, false
	}

	return requestedUserID, true
}

func (h *Handler) ListByUserID(c *gin.Context) {
	userID, ok := h.checkUserOwnership(c)
	if !ok {
		return
	}

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	subs, err := h.subUsecase.ListByUserID(c.Request.Context(), userID, offset, limit)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}

	// Populate SubscriptionURL
	for _, sub := range subs {
		sub.SubscriptionURL = h.getBaseURL(c.Request.Context()) + "/sub/" + sub.GetLinkKey()
	}

	httputil.OK(c, subs)
}

func (h *Handler) GetActiveByUserID(c *gin.Context) {
	userID, ok := h.checkUserOwnership(c)
	if !ok {
		return
	}

	subs, err := h.subUsecase.GetActiveByUserID(c.Request.Context(), userID)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}

	// Populate SubscriptionURL
	for _, sub := range subs {
		sub.SubscriptionURL = h.getBaseURL(c.Request.Context()) + "/sub/" + sub.GetLinkKey()
	}

	httputil.OK(c, subs)
}

func (h *Handler) Cancel(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	if err := h.subUsecase.Cancel(c.Request.Context(), id); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}
	httputil.OK(c, map[string]string{"message": "subscription cancelled"})
}

type updateDataUsageRequest struct {
	BytesUsed int64 `json:"bytes_used" binding:"required"`
}

func (h *Handler) UpdateDataUsage(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req updateDataUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.UpdateDataUsage(c.Request.Context(), id, req.BytesUsed); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}
	httputil.OK(c, map[string]string{"message": "data usage updated"})
}

func (h *Handler) GetSubscriptionLink(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	link, err := h.subUsecase.GetSubscriptionLink(c.Request.Context(), id)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}
	httputil.OK(c, map[string]string{"link": link})
}

type renameRequest struct {
	Label string `json:"label" binding:"required"`
}

// RenameSubscription updates the subscription label
func (h *Handler) RenameSubscription(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req renameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.RenameSubscription(c.Request.Context(), id, req.Label); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}
	httputil.OK(c, map[string]string{"message": "subscription renamed"})
}

// RegenerateUUID generates a new UUID for the subscription (invalidates old links)
func (h *Handler) RegenerateUUID(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	sub, err := h.subUsecase.RegenerateUUID(c.Request.Context(), id)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}
	httputil.OK(c, sub)
}

// SyncUsageFromXray syncs usage stats from Xray for a single subscription
func (h *Handler) SyncUsageFromXray(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	if err := h.subUsecase.SyncUsageFromXray(c.Request.Context(), id); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}
	httputil.OK(c, map[string]string{"message": "usage synced"})
}

// CheckAndExpireSubscriptions checks and expires subscriptions past their end date
func (h *Handler) CheckAndExpireSubscriptions(c *gin.Context) {
	if err := h.subUsecase.CheckAndExpireSubscriptions(c.Request.Context()); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}
	httputil.OK(c, map[string]string{"message": "expiration check completed"})
}

// CheckAndExpireByDataLimit checks and expires subscriptions that exceeded data limit
func (h *Handler) CheckAndExpireByDataLimit(c *gin.Context) {
	if err := h.subUsecase.CheckAndExpireByDataLimit(c.Request.Context()); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}
	httputil.OK(c, map[string]string{"message": "data limit check completed"})
}

// ReconcileUsers syncs users between database and Xray
func (h *Handler) ReconcileUsers(c *gin.Context) {
	stats, err := h.subUsecase.ReconcileUsers(c.Request.Context())
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}
	httputil.OK(c, stats)
}

// Updated to use GetSubscriptionConfig for real-time link generation.
func (h *Handler) GetConfigRaw(c *gin.Context) {
	uuid := c.Param("uuid")

	// Allow direct download from browser panel
	isDownload := c.Query("download") == "1"

	// Redirect browsers to the subscription panel when configured (skip for downloads).
	// Guard against redirect loops: skip if the panel URL matches the current request host.
	subPanelURL := h.getSubPanelURL(c.Request.Context())
	if !isDownload && subPanelURL != "" && isBrowserRequest(c.Request) && !isSameOrigin(c.Request, subPanelURL) {
		c.Redirect(http.StatusFound, subPanelURL+"/sub/"+uuid)
		return
	}

	// If this is a browser request and the SPA is embedded, serve the SPA.
	// React Router will render the /sub/:uuid page client-side.
	if !isDownload && isBrowserRequest(c.Request) && h.spaFS != nil {
		h.serveIndex(c.Writer)
		return
	}

	result, err := h.subUsecase.GetSubscriptionConfig(c.Request.Context(), uuid)
	if err != nil {
		// Differentiate error types if needed, for now general 404/403 behavior
		c.String(http.StatusNotFound, "Subscription not found or expired")
		return
	}

	// For Xray/V2Ray subscriptions, clients expect a Base64 encoded list of links
	encoded := base64.StdEncoding.EncodeToString([]byte(result.Config))

	// Set standard subscription headers for v2ray clients
	c.Header("Content-Type", "text/plain; charset=utf-8")
	if isDownload {
		c.Header("Content-Disposition", "attachment; filename=\"config.txt\"")
	} else {
		c.Header("Content-Disposition", "inline; filename=\"sub.txt\"")
	}

	// subscription-userinfo: standard header for data usage display in clients
	uploadBytes := int64(0)
	downloadBytes := result.DataUsed
	totalBytes := result.DataLimit
	subUserInfo := fmt.Sprintf("upload=%d; download=%d; total=%d", uploadBytes, downloadBytes, totalBytes)
	if result.ExpiresAt != nil {
		subUserInfo += fmt.Sprintf("; expire=%d", result.ExpiresAt.Unix())
	}
	c.Header("subscription-userinfo", subUserInfo)

	// profile-title: display name in subscription clients (base64: prefix required)
	if result.PlanName != "" {
		c.Header("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte(result.PlanName)))
	}

	// profile-update-interval: auto-update interval in hours
	c.Header("profile-update-interval", "1")

	// Return the encoded config
	c.String(http.StatusOK, encoded)
}

// ==================== Custom Data/Expiry Management ====================

type setDataLimitRequest struct {
	LimitGB *float64 `json:"limit_gb"` // nil = reset to plan default
}

// SetDataLimit sets a custom data limit for a subscription
func (h *Handler) SetDataLimit(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req setDataLimitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.SetCustomDataLimit(c.Request.Context(), id, req.LimitGB); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	msg := "data limit reset to plan default"
	if req.LimitGB != nil {
		msg = "custom data limit set"
	}

	httputil.OK(c, map[string]string{"message": msg})
}

type setBandwidthLimitRequest struct {
	LimitMbps *int `json:"limit_mbps"` // nil = reset to plan default, 0 = unlimited
}

// SetBandwidthLimit sets a custom bandwidth limit for a subscription
func (h *Handler) SetBandwidthLimit(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req setBandwidthLimitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.SetCustomBandwidthLimit(c.Request.Context(), id, req.LimitMbps); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	msg := "bandwidth limit reset to plan default"
	if req.LimitMbps != nil {
		if *req.LimitMbps == 0 {
			msg = "bandwidth set to unlimited"
		} else {
			msg = fmt.Sprintf("bandwidth limit set to %d Mbps", *req.LimitMbps)
		}
	}

	httputil.OK(c, map[string]string{"message": msg})
}

type setMaxDevicesRequest struct {
	MaxDevices int `json:"max_devices"` // 0 = inherit plan default
}

// SetMaxDevices sets the per-subscription device cap (0 = inherit plan default)
func (h *Handler) SetMaxDevices(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req setMaxDevicesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.SetMaxDevices(c.Request.Context(), id, req.MaxDevices); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	msg := "device limit reset to plan default"
	if req.MaxDevices > 0 {
		msg = fmt.Sprintf("device limit set to %d", req.MaxDevices)
	}

	httputil.OK(c, map[string]string{"message": msg})
}

type setEndDateRequest struct {
	EndDate *string `json:"end_date"` // ISO 8601 format, nil = reset to plan default
}

// SetEndDate sets a custom end date for a subscription
func (h *Handler) SetEndDate(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req setEndDateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	var endDate *time.Time
	if req.EndDate != nil {
		parsed, err := time.Parse(time.RFC3339, *req.EndDate)
		if err != nil {
			// Try alternate format
			parsed, err = time.Parse("2006-01-02", *req.EndDate)
			if err != nil {
				httputil.Error(c, http.StatusBadRequest, "invalid date format, use ISO 8601 (YYYY-MM-DD or RFC3339)")
				return
			}
		}
		endDate = &parsed
	}

	sub, err := h.subUsecase.SetCustomEndDate(c.Request.Context(), id, endDate, endDate != nil)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c.Request.Context()) + "/sub/" + sub.GetLinkKey()

	httputil.OK(c, sub)
}

type addDataRequest struct {
	AdditionalGB float64 `json:"additional_gb" binding:"required,min=0.1"`
}

// AddData adds additional data to a subscription's limit
func (h *Handler) AddData(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	var req addDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.AddData(c.Request.Context(), id, req.AdditionalGB); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	httputil.OK(c, map[string]interface{}{"message": "data added", "added_gb": req.AdditionalGB})
}

type assignInboundRequest struct {
	InboundID uint `json:"inbound_id" binding:"required"`
}

// ResetData resets the data used counter to 0
func (h *Handler) ResetData(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	if err := h.subUsecase.ResetDataUsed(c.Request.Context(), id); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	httputil.OK(c, map[string]string{"message": "data usage reset"})
}

// AssignToInbound assigns a subscription to a specific inbound
func (h *Handler) AssignToInbound(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		httputil.Error(c, http.StatusBadRequest, "invalid subscription id")
		return
	}

	var req assignInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusBadRequest, err, err.Error())
		return
	}

	if err := h.subUsecase.AssignToInbound(c.Request.Context(), uint(id), req.InboundID); err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}

	httputil.OK(c, nil)
}

// GetUsageDetails returns detailed usage information for a subscription
func (h *Handler) GetUsageDetails(c *gin.Context) {
	id, ok := httputil.ParamUint(c, "id")
	if !ok {
		return
	}

	details, err := h.subUsecase.GetUsageDetails(c.Request.Context(), id)
	if err != nil {
		httputil.Error(c, http.StatusNotFound, err.Error())
		return
	}

	httputil.OK(c, details)
}
