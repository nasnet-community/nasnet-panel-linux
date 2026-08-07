package http

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	subRepo "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

type Handler struct {
	adminUsecase usecase.AdminUsecase
	baseURL      string
	auditUC      auditDomain.AuditLogUsecase
	botToken     string
	settingUC    settingDomain.SettingUsecase
	httpFactory  *httpclient.Factory
}

// SetHTTPClientFactory injects the outbound-proxy-aware HTTP factory used
// for Telegram getFile downloads. nil leaves direct-internet behavior.
func (h *Handler) SetHTTPClientFactory(f *httpclient.Factory) {
	h.httpFactory = f
}

// telegramClient returns an *http.Client for Telegram API calls, honoring
// proxy_use_telegram when the factory is wired.
func (h *Handler) telegramClient(timeout time.Duration) *http.Client {
	if h.httpFactory == nil {
		return &http.Client{Timeout: timeout}
	}
	return h.httpFactory.ClientFor(httpclient.FeatureTelegram, httpclient.EgressForeign, timeout)
}

func NewHandler(adminUsecase usecase.AdminUsecase, baseURL string, auditUC auditDomain.AuditLogUsecase, botToken string, settingUC settingDomain.SettingUsecase) *Handler {
	return &Handler{
		adminUsecase: adminUsecase,
		baseURL:      baseURL,
		auditUC:      auditUC,
		botToken:     botToken,
		settingUC:    settingUC,
	}
}

// getBaseURL reads app_base_url from DB settings, falling back to the startup config value.
func (h *Handler) getBaseURL(c *gin.Context) string {
	if h.settingUC != nil {
		if v, err := h.settingUC.GetByKey(c.Request.Context(), "app_base_url"); err == nil && v != "" {
			return v
		}
	}
	return h.baseURL
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	admin := rg.Group("/admin")
	{
		// Dashboard & Stats
		admin.GET("/dashboard", h.GetDashboardStats)
		admin.GET("/dashboard/online-users-history", h.GetOnlineUsersHistory)
		admin.GET("/xray/stats", h.GetXraySystemStats)
		admin.GET("/users/online", h.GetOnlineUsers)
		admin.GET("/xray/inbounds", h.GetInboundDetails)

		// Analytics
		admin.GET("/analytics/peak-hours", h.GetPeakHours)
		admin.GET("/analytics/blocked-domains", h.GetBlockedDomainStats)

		// User Management
		admin.GET("/users", h.ListUsers)
		admin.GET("/users/:id/details", h.GetUserDetails)
		admin.POST("/users/:id/ban", h.BanUser)
		admin.POST("/users/:id/unban", h.UnbanUser)
		admin.PUT("/users/:id/admin", h.SetAdmin)
		admin.PUT("/users/:id/telegram-id", h.UpdateUserTelegramID)
		admin.PUT("/users/:id/notes", h.UpdateUserNotes)
		admin.GET("/users/:id/usage-history", h.GetUserUsageHistory)
		admin.GET("/users/:id/usage-pattern", h.GetUserUsagePattern)
		admin.GET("/users/:id/activity", h.GetUserActivity)
		admin.GET("/users/:id/accounts", h.GetUserAccounts)
		admin.POST("/users", h.CreateUser)

		// Subscription Management
		admin.GET("/subscriptions", h.ListAllSubscriptions)
		admin.GET("/subscriptions/counts", h.GetSubscriptionCounts)
		admin.POST("/subscriptions/bulk", h.BulkSubscriptionAction)
		admin.POST("/subscriptions/bulk-bandwidth", h.BulkSetBandwidthLimit)
		admin.POST("/subscriptions/bulk-inbounds", h.BulkManageInbounds)
		admin.POST("/subscriptions/bulk-inbound-summary", h.BulkInboundSummary)
		admin.DELETE("/subscriptions/:id", h.DeleteSubscription)
		admin.GET("/subscriptions/:id", h.GetSubscription)
		admin.GET("/subscriptions/user/:user_id", h.GetSubscriptionsByUser)
		admin.POST("/subscriptions/:id/extend", h.ExtendSubscription)
		admin.POST("/subscriptions/:id/revoke", h.RevokeSubscription)
		admin.POST("/subscriptions/:id/pause", h.PauseSubscription)
		admin.POST("/subscriptions/:id/resume", h.ResumeSubscription)
		admin.POST("/subscriptions/:id/regenerate-key", h.RegenerateSubscriptionKey)
		admin.POST("/subscriptions/:id/regenerate-uuid", h.RegenerateSubscriptionUUID)
		admin.PUT("/subscriptions/:id/uuid", h.SetSubscriptionUUID)
		admin.GET("/subscriptions/:id/link", h.GetSubscriptionLink)
		admin.PUT("/subscriptions/:id/label", h.RenameSubscription)
		admin.PUT("/subscriptions/:id/panel-password", h.SetSubscriptionPanelPassword)

		admin.GET("/subscriptions/:id/ips", h.GetSubscriptionIPs)
		admin.GET("/subscriptions/:id/ips/active", h.GetSubscriptionActiveIPs)
		admin.GET("/subscriptions/:id/usage-history", h.GetSubscriptionUsageHistory)
		admin.GET("/users/online/ips", h.GetOnlineUsersWithIPs)

		// Data retention — stats + on-demand cleanup
		admin.GET("/retention/stats", h.GetRetentionStats)
		admin.POST("/retention/cleanup", h.RunRetentionCleanup)

		admin.PUT("/subscriptions/:id/data-usage", h.SetDataUsage)
		admin.PUT("/subscriptions/:id/data-limit", h.SetSubscriptionDataLimit)
		admin.PUT("/subscriptions/:id/expiry", h.SetSubscriptionExpiry)
		admin.PUT("/subscriptions/:id/end-date", h.SetSubscriptionExpiry)
		admin.POST("/subscriptions/:id/add-data", h.AddSubscriptionData)
		admin.POST("/subscriptions/:id/reset-data", h.ResetSubscriptionData)
		admin.PUT("/subscriptions/:id/bandwidth-limit", h.SetSubscriptionBandwidthLimit)
		admin.PUT("/subscriptions/:id/max-devices", h.SetSubscriptionMaxDevices)
		admin.GET("/subscriptions/:id/exhaustion-prediction", h.GetExhaustionPrediction)
		admin.POST("/subscriptions/manual", h.CreateManualSubscription)
		admin.PUT("/subscriptions/:id/user", h.AssignSubscriptionUser)

		// Node Management
		admin.GET("/nodes", h.ListAllNodes)
		admin.GET("/nodes/:id/inbounds/discover", h.DiscoverNodeInbounds)
		admin.POST("/nodes/:id/inbounds/sync", h.SyncNodeInbounds)

		// Manual User Management
		admin.POST("/manual/user", h.AddUserToInbound)
		admin.POST("/manual/link", h.GenerateCustomConfigLink)

		// Database Management
		admin.POST("/database/cleanup", h.CleanupDatabase)

		// Server Management
		admin.POST("/server/restart", h.RestartServer)
	}
}

// ==================== Dashboard & Stats ====================

func (h *Handler) GetDashboardStats(c *gin.Context) {
	stats, err := h.adminUsecase.GetDashboardStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

func (h *Handler) GetPeakHours(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))

	var nodeIDs []uint
	if nodeIDsStr := c.Query("node_ids"); nodeIDsStr != "" {
		for _, idStr := range strings.Split(nodeIDsStr, ",") {
			if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 32); err == nil {
				nodeIDs = append(nodeIDs, uint(id))
			}
		}
	}

	result, err := h.adminUsecase.GetPeakHours(c.Request.Context(), days, nodeIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *Handler) GetBlockedDomainStats(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	topN, _ := strconv.Atoi(c.DefaultQuery("top", "20"))

	var nodeIDs []uint
	if nodeIDsStr := c.Query("node_ids"); nodeIDsStr != "" {
		for _, idStr := range strings.Split(nodeIDsStr, ",") {
			if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 32); err == nil {
				nodeIDs = append(nodeIDs, uint(id))
			}
		}
	}

	result, err := h.adminUsecase.GetBlockedDomainStats(c.Request.Context(), days, nodeIDs, topN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *Handler) GetUserUsagePattern(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid user ID"})
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))

	result, err := h.adminUsecase.GetUserUsagePattern(c.Request.Context(), uint(id), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *Handler) GetExhaustionPrediction(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid subscription ID"})
		return
	}

	result, err := h.adminUsecase.GetExhaustionPrediction(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *Handler) GetXraySystemStats(c *gin.Context) {
	stats, err := h.adminUsecase.GetXraySystemStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

func (h *Handler) GetOnlineUsers(c *gin.Context) {
	users, err := h.adminUsecase.GetOnlineUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": users, "count": len(users)})
}

// GetOnlineUsersHistory returns recent dedup'd global online-user counts
// for sparkline rendering.
func (h *Handler) GetOnlineUsersHistory(c *gin.Context) {
	minutesStr := c.DefaultQuery("minutes", "15")
	minutes, err := strconv.Atoi(minutesStr)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "error": "invalid 'minutes' parameter"})
		return
	}

	snapshots, err := h.adminUsecase.GetOnlineUsersHistory(c.Request.Context(), minutes)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": err.Error()})
		return
	}

	points := make([]gin.H, 0, len(snapshots))
	for _, s := range snapshots {
		points = append(points, gin.H{
			"created_at": s.CreatedAt,
			"count":      s.Count,
		})
	}

	c.JSON(200, gin.H{"success": true, "data": gin.H{"points": points}})
}

// ==================== Subscription IP Tracking ====================

func (h *Handler) GetSubscriptionIPs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid subscription ID"})
		return
	}
	ips, err := h.adminUsecase.GetSubscriptionIPs(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ips})
}

func (h *Handler) GetSubscriptionActiveIPs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid subscription ID"})
		return
	}
	ips, err := h.adminUsecase.GetSubscriptionActiveIPs(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": ips})
}

func (h *Handler) GetOnlineUsersWithIPs(c *gin.Context) {
	result, err := h.adminUsecase.GetOnlineUsersWithIPs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *Handler) GetInboundDetails(c *gin.Context) {
	details, err := h.adminUsecase.GetInboundDetails(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": details})
}

// ==================== Data Retention ====================

// GetRetentionStats returns per-table row counts + oldest row timestamps
// used by the admin settings panel to show the impact of a retention change.
func (h *Handler) GetRetentionStats(c *gin.Context) {
	stats, err := h.adminUsecase.GetRetentionStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

// RunRetentionCleanup triggers the retention sweep synchronously and returns
// per-task deleted counts. The UI surfaces this as a "deleted N rows across
// M tables" toast. Safe to call while the scheduled sweep is also running.
func (h *Handler) RunRetentionCleanup(c *gin.Context) {
	deleted := h.adminUsecase.RunRetentionCleanup(c.Request.Context())
	var total int64
	for _, n := range deleted {
		total += n
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"deleted":    deleted,
			"total_rows": total,
			"task_count": len(deleted),
		},
	})
}

// ==================== User Management ====================

func (h *Handler) ListUsers(c *gin.Context) {
	search := c.Query("search")
	filter := c.Query("filter")
	sort := c.Query("sort")
	order := c.Query("order")
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	users, total, err := h.adminUsecase.ListUsers(c.Request.Context(), search, filter, sort, order, offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    users,
		"meta": gin.H{
			"total":  total,
			"offset": offset,
			"limit":  limit,
		},
	})
}

func (h *Handler) GetUserDetails(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	details, err := h.adminUsecase.GetUserDetails(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": details})
}

func (h *Handler) BanUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.BanUser(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAudit(c, auditDomain.AuditUserBan, "user", uint(id))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "user banned"})
}

func (h *Handler) UnbanUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.UnbanUser(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAudit(c, auditDomain.AuditUserUnban, "user", uint(id))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "user unbanned"})
}

type setAdminRequest struct {
	IsAdmin bool `json:"is_admin"`
}

func (h *Handler) SetAdmin(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.SetAdmin(c.Request.Context(), uint(id), req.IsAdmin); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAuditWithValues(c, auditDomain.AuditUserToggleAdmin, "user", uint(id), "", fmt.Sprintf("%v", req.IsAdmin))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "admin status updated"})
}

type updateUserTelegramIDRequest struct {
	TelegramID int64 `json:"telegram_id"`
}

func (h *Handler) UpdateUserTelegramID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req updateUserTelegramIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.UpdateUserTelegramID(c.Request.Context(), uint(id), req.TelegramID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "telegram ID updated"})
}

type createUserRequest struct {
	Username   string `json:"username" binding:"required"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	TelegramID int64  `json:"telegram_id"`
}

func (h *Handler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	user, err := h.adminUsecase.CreateUser(c.Request.Context(), req.Username, req.FirstName, req.LastName, req.TelegramID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": user})
}

type assignSubscriptionUserRequest struct {
	UserID *uint `json:"user_id"`
}

func (h *Handler) AssignSubscriptionUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req assignSubscriptionUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub, err := h.adminUsecase.AssignSubscriptionUser(c.Request.Context(), uint(id), req.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": sub})
}

// ==================== User Detail Endpoints ====================

type updateUserNotesRequest struct {
	Notes string `json:"notes"`
}

func (h *Handler) UpdateUserNotes(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req updateUserNotesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.UpdateUserNotes(c.Request.Context(), uint(id), req.Notes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAuditWithValues(c, auditDomain.AuditUserUpdateNotes, "user", uint(id), "", "notes updated")
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "notes updated"})
}

func (h *Handler) GetUserUsageHistory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	days := 30
	if d, err := strconv.Atoi(c.DefaultQuery("days", "30")); err == nil && d > 0 {
		days = d
	}

	points, err := h.adminUsecase.GetUserUsageHistory(c.Request.Context(), uint(id), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": points})
}

func (h *Handler) GetUserActivity(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit > 100 {
		limit = 100
	}

	ctx := c.Request.Context()

	// Query audit logs for user-related events
	userFilter := auditDomain.AuditListFilters{
		EntityType: "user",
		EntityID:   uint(id),
		Offset:     0,
		Limit:      200,
	}
	userLogs, _, _ := h.auditUC.List(ctx, userFilter)

	// Query audit logs for subscription events (get user's sub IDs)
	subFilter := auditDomain.AuditListFilters{
		EntityType: "subscription",
		Offset:     0,
		Limit:      200,
	}
	subLogs, _, _ := h.auditUC.List(ctx, subFilter)

	// Query payment events
	payFilter := auditDomain.AuditListFilters{
		EntityType: "payment",
		Offset:     0,
		Limit:      200,
	}
	payLogs, _, _ := h.auditUC.List(ctx, payFilter)

	// Merge all logs
	var allLogs []*auditDomain.AuditLog
	allLogs = append(allLogs, userLogs...)
	allLogs = append(allLogs, subLogs...)
	allLogs = append(allLogs, payLogs...)

	// Sort by created_at DESC
	for i := 0; i < len(allLogs); i++ {
		for j := i + 1; j < len(allLogs); j++ {
			if allLogs[j].CreatedAt.After(allLogs[i].CreatedAt) {
				allLogs[i], allLogs[j] = allLogs[j], allLogs[i]
			}
		}
	}

	// Apply offset/limit
	if offset >= len(allLogs) {
		allLogs = nil
	} else {
		end := offset + limit
		if end > len(allLogs) {
			end = len(allLogs)
		}
		allLogs = allLogs[offset:end]
	}

	// Transform to response
	type activityEvent struct {
		ID         uint   `json:"id"`
		Action     string `json:"action"`
		ActorName  string `json:"actor_name"`
		EntityType string `json:"entity_type"`
		EntityID   uint   `json:"entity_id"`
		OldValues  string `json:"old_values,omitempty"`
		NewValues  string `json:"new_values,omitempty"`
		CreatedAt  string `json:"created_at"`
	}

	events := make([]activityEvent, 0, len(allLogs))
	for _, log := range allLogs {
		events = append(events, activityEvent{
			ID:         log.ID,
			Action:     log.Action,
			ActorName:  log.ActorName,
			EntityType: log.EntityType,
			EntityID:   log.EntityID,
			OldValues:  log.OldValues,
			NewValues:  log.NewValues,
			CreatedAt:  log.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": events})
}

func (h *Handler) GetUserAccounts(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	accounts, err := h.adminUsecase.GetUserAccounts(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accounts})
}

// ==================== Plan Management ====================

func (h *Handler) ListAllSubscriptions(c *gin.Context) {
	// Parse filter params
	status := c.Query("status")
	search := c.Query("search")
	source := c.Query("source")

	// Page-based pagination (preferred)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "0"))

	// Backward compat: offset/limit
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	// If page-based params are provided, convert to offset/limit
	if page > 0 && perPage > 0 {
		offset = (page - 1) * perPage
		limit = perPage
	}

	// Optional filters
	var exhausted *bool
	if ex := c.Query("exhausted"); ex != "" {
		b := ex == "true"
		exhausted = &b
	}
	var isManual *bool
	if source == "manual" {
		t := true
		isManual = &t
	} else if source == "plan" {
		f := false
		isManual = &f
	}

	filter := subRepo.SubscriptionFilter{
		Offset:    offset,
		Limit:     limit,
		Status:    status,
		Search:    search,
		IsManual:  isManual,
		Exhausted: exhausted,
		Sort:      c.Query("sort"),
		Order:     c.Query("order"),
	}

	subs, total, err := h.adminUsecase.ListAllFilteredSubscriptions(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Populate SubscriptionURL
	for _, sub := range subs {
		sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()
	}

	// Calculate pagination meta
	totalPages := int64(0)
	if limit > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    subs,
		"meta": gin.H{
			"total":       total,
			"page":        page,
			"per_page":    limit,
			"total_pages": totalPages,
			"offset":      offset,
			"limit":       limit,
		},
	})
}

func (h *Handler) GetSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	sub, err := h.adminUsecase.GetSubscription(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()

	c.JSON(http.StatusOK, gin.H{"success": true, "data": sub})
}

func (h *Handler) GetSubscriptionsByUser(c *gin.Context) {
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid user_id"})
		return
	}

	subs, err := h.adminUsecase.GetSubscriptionsByUser(c.Request.Context(), uint(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Populate SubscriptionURL
	for _, sub := range subs {
		sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": subs})
}

type extendSubscriptionRequest struct {
	Days int `json:"days" binding:"required"`
}

func (h *Handler) ExtendSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req extendSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.ExtendSubscription(c.Request.Context(), uint(id), req.Days); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAuditWithValues(c, auditDomain.AuditSubExtend, "subscription", uint(id), "", fmt.Sprintf("%d days", req.Days))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription extended"})
}

type attachPlanRequest struct {
	PlanID uint `json:"plan_id" binding:"required"`
}

func (h *Handler) RevokeSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.RevokeSubscription(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAudit(c, auditDomain.AuditSubRevoke, "subscription", uint(id))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription revoked"})
}

func (h *Handler) PauseSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.PauseSubscription(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription paused"})
}

func (h *Handler) ResumeSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.ResumeSubscription(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription resumed"})
}

func (h *Handler) RegenerateSubscriptionKey(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var body struct {
		Key string `json:"key"`
	}
	_ = c.ShouldBindJSON(&body) // optional body — if absent, server generates a random key

	sub, err := h.adminUsecase.RegenerateSubscriptionKey(c.Request.Context(), uint(id), body.Key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription key regenerated", "data": sub})
}

func (h *Handler) RegenerateSubscriptionUUID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	sub, err := h.adminUsecase.RegenerateSubscriptionUUID(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "UUID regenerated", "data": sub})
}

// GetSubscriptionUsageHistory returns the last N days of per-day usage deltas
// for a subscription, used by the admin panel to render a usage sparkline.
func (h *Handler) GetSubscriptionUsageHistory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	days := 30
	if q := c.Query("days"); q != "" {
		if parsed, perr := strconv.Atoi(q); perr == nil && parsed > 0 {
			days = parsed
		}
	}

	points, err := h.adminUsecase.GetSubscriptionUsageHistory(c.Request.Context(), uint(id), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	if points == nil {
		points = []subUC.UsageHistoryPoint{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": points})
}

type setSubscriptionUUIDRequest struct {
	UUID string `json:"uuid" binding:"required"`
}

// SetSubscriptionUUID applies a caller-provided UUID to every account under a
// subscription atomically, avoiding the prior per-account roundtrip loop on
// the client.
func (h *Handler) SetSubscriptionUUID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setSubscriptionUUIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	trimmed := strings.TrimSpace(req.UUID)
	if _, err := uuid.Parse(trimmed); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid uuid format"})
		return
	}

	updated, err := h.adminUsecase.SetSubscriptionUUID(c.Request.Context(), uint(id), trimmed)
	if err != nil {
		// Partial success is still reported; the updated count tells the client
		// how many accounts were changed before the failure.
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"data":    gin.H{"updated": updated},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("UUID applied to %d account(s)", updated),
		"data":    gin.H{"updated": updated},
	})
}

func (h *Handler) GetSubscriptionLink(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	link, err := h.adminUsecase.GetSubscriptionLink(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "link": link})
}

type setDataUsageRequest struct {
	BytesUsed int64 `json:"bytes_used" binding:"required"`
}

func (h *Handler) SetDataUsage(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setDataUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.SetDataUsage(c.Request.Context(), uint(id), req.BytesUsed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "data usage updated"})
}

type setSubDataLimitRequest struct {
	LimitGB *float64 `json:"limit_gb"` // nil = reset to plan default
}

func (h *Handler) SetSubscriptionDataLimit(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setSubDataLimitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.SetSubscriptionDataLimit(c.Request.Context(), uint(id), req.LimitGB); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	msg := "data limit reset to plan default"
	if req.LimitGB != nil {
		msg = "custom data limit set"
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
}

type setSubBandwidthLimitRequest struct {
	LimitMbps *int `json:"limit_mbps"` // nil = reset to plan default, 0 = unlimited
}

func (h *Handler) SetSubscriptionBandwidthLimit(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setSubBandwidthLimitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.SetSubscriptionBandwidthLimit(c.Request.Context(), uint(id), req.LimitMbps); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
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
	c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
}

type setSubMaxDevicesRequest struct {
	MaxDevices int `json:"max_devices"` // 0 = inherit plan default
}

func (h *Handler) SetSubscriptionMaxDevices(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setSubMaxDevicesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.SetSubscriptionMaxDevices(c.Request.Context(), uint(id), req.MaxDevices); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	msg := "device limit reset to plan default"
	if req.MaxDevices > 0 {
		msg = fmt.Sprintf("device limit set to %d", req.MaxDevices)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": msg})
}

type setSubExpiryRequest struct {
	EndDate   *string `json:"end_date"`  // ISO 8601 format
	Unlimited bool    `json:"unlimited"` // true = no expiry (unlimited)
}

func (h *Handler) SetSubscriptionExpiry(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setSubExpiryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	var endDate *time.Time
	if req.EndDate != nil {
		parsed, err := time.Parse(time.RFC3339, *req.EndDate)
		if err != nil {
			// Try alternate format
			parsed, err = time.Parse("2006-01-02", *req.EndDate)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid date format, use ISO 8601"})
				return
			}
		}
		endDate = &parsed
	}

	sub, err := h.adminUsecase.SetSubscriptionEndDate(c.Request.Context(), uint(id), endDate, req.Unlimited)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()

	msg := "end date reset to plan default"
	if req.Unlimited {
		msg = "expiry set to unlimited"
	} else if endDate != nil {
		msg = "custom end date set"
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": msg, "data": sub})
}

type addSubDataRequest struct {
	AdditionalGB float64 `json:"additional_gb" binding:"required,min=0.1"`
}

func (h *Handler) AddSubscriptionData(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req addSubDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.AddSubscriptionData(c.Request.Context(), uint(id), req.AdditionalGB); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "data added", "added_gb": req.AdditionalGB})
}

func (h *Handler) ResetSubscriptionData(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.ResetSubscriptionData(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "data usage reset"})
}

type renameSubscriptionRequest struct {
	Label string `json:"label" binding:"required"`
}

func (h *Handler) RenameSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req renameSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.RenameSubscription(c.Request.Context(), uint(id), req.Label); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription renamed"})
}

type setSubscriptionPanelPasswordRequest struct {
	Mode     string `json:"mode" binding:"required"`
	Password string `json:"password"`
}

func (h *Handler) SetSubscriptionPanelPassword(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req setSubscriptionPanelPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.adminUsecase.SetSubscriptionPanelPassword(c.Request.Context(), uint(id), req.Mode, req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "panel password updated"})
}

func (h *Handler) DeleteSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.adminUsecase.DeleteSubscription(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAudit(c, auditDomain.AuditSubDelete, "subscription", uint(id))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "subscription deleted"})
}

type bulkSubscriptionActionRequest struct {
	Action string `json:"action" binding:"required"`
	IDs    []uint `json:"ids" binding:"required"`
}

func (h *Handler) BulkSubscriptionAction(c *gin.Context) {
	var req bulkSubscriptionActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	validActions := map[string]bool{"delete": true, "pause": true, "resume": true, "revoke": true}
	if !validActions[req.Action] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "action must be one of: delete, pause, resume, revoke"})
		return
	}

	if len(req.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "maximum 100 IDs per request"})
		return
	}

	result, err := h.adminUsecase.BulkSubscriptionAction(c.Request.Context(), req.Action, req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAuditWithValues(c, auditDomain.AuditSubBulk, "subscription", 0, "", fmt.Sprintf("%s: %d IDs", req.Action, len(req.IDs)))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

type bulkSetBandwidthRequest struct {
	IDs            []uint `json:"ids" binding:"required"`
	BandwidthLimit *int   `json:"bandwidth_limit"` // nil = reset to plan default
}

func (h *Handler) BulkSetBandwidthLimit(c *gin.Context) {
	var req bulkSetBandwidthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(req.IDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "maximum 100 IDs per request"})
		return
	}

	result, err := h.adminUsecase.BulkSetBandwidthLimit(c.Request.Context(), req.IDs, req.BandwidthLimit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	label := "reset"
	if req.BandwidthLimit != nil {
		label = fmt.Sprintf("%d Mbps", *req.BandwidthLimit)
	}
	h.logAuditWithValues(c, auditDomain.AuditSubBulk, "subscription", 0, "", fmt.Sprintf("set_bandwidth(%s): %d IDs", label, len(req.IDs)))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

type bulkManageInboundsRequest struct {
	SubscriptionIDs  []uint `json:"subscription_ids" binding:"required"`
	AddInboundIDs    []uint `json:"add_inbound_ids"`
	RemoveInboundIDs []uint `json:"remove_inbound_ids"`
}

func (h *Handler) BulkManageInbounds(c *gin.Context) {
	var req bulkManageInboundsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(req.SubscriptionIDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "maximum 500 subscription IDs per request"})
		return
	}
	if len(req.AddInboundIDs) == 0 && len(req.RemoveInboundIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "at least one of add_inbound_ids or remove_inbound_ids must be provided"})
		return
	}

	result, err := h.adminUsecase.BulkManageInbounds(c.Request.Context(), req.SubscriptionIDs, req.AddInboundIDs, req.RemoveInboundIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	h.logAuditWithValues(c, auditDomain.AuditSubBulk, "subscription", 0, "",
		fmt.Sprintf("bulk-inbounds: add=%v remove=%v subs=%d", req.AddInboundIDs, req.RemoveInboundIDs, len(req.SubscriptionIDs)))

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

type bulkAttachPlanRequest struct {
	SubscriptionIDs []uint `json:"subscription_ids" binding:"required"`
	PlanID          uint   `json:"plan_id" binding:"required"`
}

type bulkInboundSummaryRequest struct {
	SubscriptionIDs []uint `json:"subscription_ids" binding:"required"`
}

func (h *Handler) BulkInboundSummary(c *gin.Context) {
	var req bulkInboundSummaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if len(req.SubscriptionIDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "maximum 500 subscription IDs"})
		return
	}

	summary, err := h.adminUsecase.GetBulkInboundSummary(c.Request.Context(), req.SubscriptionIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": summary})
}

func (h *Handler) GetSubscriptionCounts(c *gin.Context) {
	ctx := c.Request.Context()
	all, _ := h.adminUsecase.CountAllSubscriptions(ctx)
	active, _ := h.adminUsecase.CountSubscriptionsByStatus(ctx, "active")
	paused, _ := h.adminUsecase.CountSubscriptionsByStatus(ctx, "paused")
	expired, _ := h.adminUsecase.CountSubscriptionsByStatus(ctx, "expired")
	cancelled, _ := h.adminUsecase.CountSubscriptionsByStatus(ctx, "cancelled")
	trafficExhausted, _ := h.adminUsecase.CountSubscriptionsByStatus(ctx, "traffic_exhausted")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"all":               all,
			"active":            active,
			"paused":            paused,
			"expired":           expired,
			"cancelled":         cancelled,
			"traffic_exhausted": trafficExhausted,
		},
	})
}

func (h *Handler) ListAllNodes(c *gin.Context) {
	nodes, err := h.adminUsecase.ListAllNodes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": nodes})
}

func (h *Handler) DiscoverNodeInbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node_id"})
		return
	}

	inbounds, err := h.adminUsecase.DiscoverNodeInbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": inbounds})
}

func (h *Handler) SyncNodeInbounds(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node_id"})
		return
	}

	result, err := h.adminUsecase.SyncNodeInbounds(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ==================== Manual User Management ====================

type addUserToInboundRequest struct {
	NodeID     uint   `json:"node_id" binding:"required"`
	InboundTag string `json:"inbound_tag" binding:"required"`
	Email      string `json:"email" binding:"required"`
}

func (h *Handler) AddUserToInbound(c *gin.Context) {
	var req addUserToInboundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	user, link, err := h.adminUsecase.AddUserToInbound(c.Request.Context(), req.NodeID, req.InboundTag, req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": user, "link": link})
}

type generateCustomLinkRequest struct {
	NodeID     uint   `json:"node_id" binding:"required"`
	InboundTag string `json:"inbound_tag" binding:"required"`
	Email      string `json:"email" binding:"required"`
	UUID       string `json:"uuid" binding:"required"`
}

func (h *Handler) GenerateCustomConfigLink(c *gin.Context) {
	var req generateCustomLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	link, err := h.adminUsecase.GenerateCustomConfigLink(c.Request.Context(), req.NodeID, req.InboundTag, req.Email, req.UUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "link": link})
}

// ==================== Manual Subscription ====================

type createManualSubscriptionRequest struct {
	Label          string   `json:"label"`
	InboundIDs     []uint   `json:"inbound_ids" binding:"required,min=1"`
	DataLimitGB    *float64 `json:"data_limit_gb"`
	BandwidthLimit int      `json:"bandwidth_limit"` // Mbps, 0 = unlimited
	MaxDevices     int      `json:"max_devices"`
	EndDate        *string  `json:"end_date"`
	UserID         *uint    `json:"user_id"`
}

func (h *Handler) CreateManualSubscription(c *gin.Context) {
	var req createManualSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Convert data limit from GB to bytes
	var dataLimit int64
	if req.DataLimitGB != nil && *req.DataLimitGB > 0 {
		dataLimit = int64(*req.DataLimitGB * 1024 * 1024 * 1024)
	}

	// Parse end date
	var endDate *time.Time
	if req.EndDate != nil && *req.EndDate != "" {
		t, err := time.Parse(time.RFC3339, *req.EndDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid end_date format, use RFC3339"})
			return
		}
		endDate = &t
	}

	sub, err := h.adminUsecase.CreateManualSubscription(c.Request.Context(), &subUC.ManualSubscriptionRequest{
		Label:          req.Label,
		InboundIDs:     req.InboundIDs,
		DataLimit:      dataLimit,
		BandwidthLimit: req.BandwidthLimit,
		MaxDevices:     req.MaxDevices,
		EndDate:        endDate,
		UserID:         req.UserID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	sub.SubscriptionURL = h.getBaseURL(c) + "/sub/" + sub.GetLinkKey()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": sub})
}

// ==================== Database Management ====================

func (h *Handler) CleanupDatabase(c *gin.Context) {
	result, err := h.adminUsecase.CleanupDatabase(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	h.logAudit(c, auditDomain.AuditDatabaseCleanup, "database", 0)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// ==================== Server Management ====================

// ==================== Plan Users ====================

func (h *Handler) RestartServer(c *gin.Context) {
	h.logAudit(c, auditDomain.AuditServerRestart, "server", 0)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"message": "Server restart initiated"}})

	// Send SIGTERM after a short delay so the response can reach the client
	time.AfterFunc(500*time.Millisecond, func() {
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
	})
}

// logAudit is a helper to emit an audit log entry from admin handler actions
func (h *Handler) logAudit(c *gin.Context, action auditDomain.AuditAction, entityType string, entityID uint) {
	if h.auditUC == nil {
		return
	}
	ac := audit.FromGinContext(c)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(action),
		ActorID:    ac.ActorID,
		ActorName:  ac.ActorName,
		EntityType: entityType,
		EntityID:   entityID,
		IPAddress:  ac.IPAddress,
		RequestID:  ac.RequestID,
		Source:     "http",
	})
}

// logAuditWithValues emits an audit log with old/new value snapshots
func (h *Handler) logAuditWithValues(c *gin.Context, action auditDomain.AuditAction, entityType string, entityID uint, oldVal, newVal string) {
	if h.auditUC == nil {
		return
	}
	ac := audit.FromGinContext(c)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(action),
		ActorID:    ac.ActorID,
		ActorName:  ac.ActorName,
		EntityType: entityType,
		EntityID:   entityID,
		OldValues:  oldVal,
		NewValues:  newVal,
		IPAddress:  ac.IPAddress,
		RequestID:  ac.RequestID,
		Source:     "http",
	})
}
