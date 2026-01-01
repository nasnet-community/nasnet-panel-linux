package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/usecase"
)

type Handler struct {
	accountUsecase usecase.AccountUsecase
}

func (h *Handler) ListByNode(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("node_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid node_id"})
		return
	}

	// Support pagination via query params
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "0"))

	if page > 0 && perPage > 0 {
		accounts, total, err := h.accountUsecase.ListAccountsByNodePaginated(c.Request.Context(), uint(nodeID), page, perPage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"data":     accounts,
			"total":    total,
			"page":     page,
			"per_page": perPage,
		})
		return
	}

	// Fallback: return all (backward-compatible)
	accounts, err := h.accountUsecase.ListAccountsByNode(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accounts})
}

func NewHandler(accountUsecase usecase.AccountUsecase) *Handler {
	return &Handler{accountUsecase: accountUsecase}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	accounts := rg.Group("/admin/accounts")
	{
		// List & Count
		accounts.GET("", h.ListAll)
		accounts.GET("/count", h.Count)
		accounts.GET("/active", h.ListActive)

		// Get by various identifiers
		accounts.GET("/nodes/:node_id", h.ListByNode)
		accounts.GET("/:id", h.GetByID)
		accounts.GET("/email/:email", h.GetByEmail)
		accounts.GET("/inbound/:inbound_id", h.ListByInbound)
		accounts.GET("/subscription/:sub_id", h.ListBySubscription)

		// Create
		accounts.POST("", h.CreateManual)

		// State management
		accounts.PUT("/:id", h.Update)
		accounts.POST("/:id/disable", h.Disable)
		accounts.POST("/:id/enable", h.Enable)
		accounts.POST("/:id/migrate", h.Migrate)
		accounts.DELETE("/:id", h.Delete)

		// Link generation
		accounts.GET("/:id/link", h.GetLink)

		// Stats
		accounts.POST("/:id/sync", h.SyncStats)
		accounts.PUT("/:id/data", h.UpdateDataUsed)
	}
}

type updateAccountRequest struct {
	Email     string  `json:"email" binding:"required"`
	UUID      string  `json:"uuid" binding:"required"`
	Flow      string  `json:"flow"`
	DataLimit int64   `json:"data_limit"`
	ExpiresAt *string `json:"expires_at"` // RFC3339
	Enabled   bool    `json:"enabled"`
}

func (h *Handler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid date format, use RFC3339"})
			return
		}
		expiresAt = &parsed
	}

	if err := h.accountUsecase.UpdateAccount(
		c.Request.Context(),
		uint(id),
		req.Email,
		req.UUID,
		req.Flow,
		req.DataLimit,
		expiresAt,
		req.Enabled,
	); err != nil {
		status := http.StatusInternalServerError
		if err == usecase.ErrDuplicateUUID || err == usecase.ErrAccountNotFound || err == usecase.ErrEmailExists {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "account updated"})
}

func (h *Handler) parseFilter(c *gin.Context) repository.AccountFilter {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	status := domain.AccountStatus(c.Query("status"))
	search := c.Query("search")

	var exhausted *bool
	if val := c.Query("exhausted"); val != "" {
		isExhausted := val == "true"
		exhausted = &isExhausted
	}

	// New filters
	var nodeID *uint
	if val := c.Query("node_id"); val != "" {
		if id, err := strconv.ParseUint(val, 10, 32); err == nil {
			uid := uint(id)
			nodeID = &uid
		}
	}

	var inboundID *uint
	if val := c.Query("inbound_id"); val != "" {
		if id, err := strconv.ParseUint(val, 10, 32); err == nil {
			uid := uint(id)
			inboundID = &uid
		}
	}

	source := c.Query("source")

	return repository.AccountFilter{
		Offset:    offset,
		Limit:     limit,
		Status:    status,
		Search:    search,
		Exhausted: exhausted,
		NodeID:    nodeID,
		InboundID: inboundID,
		Source:    source,
	}
}

func (h *Handler) ListAll(c *gin.Context) {
	filter := h.parseFilter(c)

	accounts, err := h.accountUsecase.ListAllAccounts(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    accounts,
		"meta": gin.H{
			"offset": filter.Offset,
			"limit":  filter.Limit,
		},
	})
}

func (h *Handler) Count(c *gin.Context) {
	filter := h.parseFilter(c)
	count, err := h.accountUsecase.CountAccounts(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "count": count})
}

func (h *Handler) ListActive(c *gin.Context) {
	accounts, err := h.accountUsecase.ListActiveAccounts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accounts})
}

func (h *Handler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	account, err := h.accountUsecase.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": account})
}

func (h *Handler) GetByEmail(c *gin.Context) {
	email := c.Param("email")

	account, err := h.accountUsecase.GetAccountByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": account})
}

func (h *Handler) ListByInbound(c *gin.Context) {
	inboundID, err := strconv.ParseUint(c.Param("inbound_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid inbound_id"})
		return
	}

	accounts, err := h.accountUsecase.ListAccountsByInbound(c.Request.Context(), uint(inboundID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accounts})
}

func (h *Handler) ListBySubscription(c *gin.Context) {
	subID, err := strconv.ParseUint(c.Param("sub_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid sub_id"})
		return
	}

	accounts, err := h.accountUsecase.ListAccountsBySubscription(c.Request.Context(), uint(subID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": accounts})
}

type createAccountRequest struct {
	InboundID  uint   `json:"inbound_id" binding:"required"`
	Email      string `json:"email" binding:"required"`
	UUID       string `json:"uuid"`       // Optional, will be generated if empty
	Flow       string `json:"flow"`       // Optional, for VLESS
	Encryption string `json:"encryption"` // Optional, for VLESS
}

func (h *Handler) CreateManual(c *gin.Context) {
	var req createAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	accountID, link, err := h.accountUsecase.CreateAccountManual(
		c.Request.Context(),
		req.InboundID,
		req.Email,
		req.UUID,
		req.Flow,
		req.Encryption,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// Fetch the created account to return full details
	account, _ := h.accountUsecase.GetAccount(c.Request.Context(), accountID)

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    account,
		"link":    link,
	})
}

func (h *Handler) Disable(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.accountUsecase.DisableAccount(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "account disabled"})
}

func (h *Handler) Enable(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.accountUsecase.EnableAccount(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "account enabled"})
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.accountUsecase.DeleteAccount(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "account deleted"})
}

func (h *Handler) GetLink(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	link, err := h.accountUsecase.GenerateAccountLink(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"link": link}})
}

func (h *Handler) SyncStats(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	if err := h.accountUsecase.SyncAccountStats(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "account stats synced"})
}

type updateDataUsedRequest struct {
	DataUsed int64 `json:"data_used" binding:"required"`
}

func (h *Handler) UpdateDataUsed(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req updateDataUsedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.accountUsecase.UpdateDataUsed(c.Request.Context(), uint(id), req.DataUsed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "data usage updated"})
}

// Additional helper types for future extensions
var _ domain.AccountSource // Ensure domain import is used
type migrateAccountRequest struct {
	TargetInboundID uint `json:"target_inbound_id" binding:"required"`
}

func (h *Handler) Migrate(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	var req migrateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	if err := h.accountUsecase.MigrateAccount(c.Request.Context(), uint(id), req.TargetInboundID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "account migrated successfully"})
}
