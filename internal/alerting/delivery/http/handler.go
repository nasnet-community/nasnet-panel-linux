package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/alerting/usecase"
)

type Handler struct {
	uc usecase.AlertUsecase
}

func NewHandler(uc usecase.AlertUsecase) *Handler {
	return &Handler{uc: uc}
}

// RegisterRoutes wires /alerts/* onto the supplied admin router group.
// Caller is responsible for applying admin auth middleware beforehand.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	alerts := rg.Group("/alerts")
	{
		alerts.GET("/rules", h.ListRules)
		alerts.POST("/rules", h.CreateRule)
		alerts.GET("/rules/:id", h.GetRule)
		alerts.PUT("/rules/:id", h.UpdateRule)
		alerts.DELETE("/rules/:id", h.DeleteRule)
		alerts.PATCH("/rules/:id/enabled", h.SetEnabled)
		alerts.PATCH("/rules/:id/threshold", h.SetThreshold)
		alerts.POST("/rules/:id/test", h.TestSend)

		alerts.GET("/events", h.ListEvents)
	}
}

func (h *Handler) ListRules(c *gin.Context) {
	rules, err := h.uc.ListRules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rules})
}

func (h *Handler) GetRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	rule, err := h.uc.GetRule(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rule})
}

type createRuleRequest struct {
	Name        string           `json:"name" binding:"required"`
	RuleType    domain.RuleType  `json:"rule_type" binding:"required"`
	Scope       domain.ScopeKind `json:"scope"`
	ScopeValue  string           `json:"scope_value"`
	Threshold   domain.Threshold `json:"threshold"`
	CooldownSec int              `json:"cooldown_sec"`
	Enabled     bool             `json:"enabled"`
	Description string           `json:"description"`
}

func (h *Handler) CreateRule(c *gin.Context) {
	var req createRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	rule := &domain.Rule{
		Name:        req.Name,
		RuleType:    req.RuleType,
		Scope:       req.Scope,
		ScopeValue:  req.ScopeValue,
		Threshold:   req.Threshold,
		CooldownSec: req.CooldownSec,
		Enabled:     req.Enabled,
		Description: req.Description,
	}
	if err := h.uc.CreateRule(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rule})
}

func (h *Handler) UpdateRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	existing, err := h.uc.GetRule(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": err.Error()})
		return
	}
	var req createRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	existing.Name = req.Name
	existing.RuleType = req.RuleType
	existing.Scope = req.Scope
	existing.ScopeValue = req.ScopeValue
	existing.Threshold = req.Threshold
	if req.CooldownSec > 0 {
		existing.CooldownSec = req.CooldownSec
	}
	existing.Enabled = req.Enabled
	existing.Description = req.Description
	if err := h.uc.UpdateRule(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": existing})
}

type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) SetEnabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req setEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := h.uc.SetEnabled(c.Request.Context(), uint(id), req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type setThresholdRequest struct {
	Threshold   domain.Threshold `json:"threshold"`
	CooldownSec int              `json:"cooldown_sec"`
}

func (h *Handler) SetThreshold(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	var req setThresholdRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	if err := h.uc.SetThreshold(c.Request.Context(), uint(id), req.Threshold, req.CooldownSec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) DeleteRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	if err := h.uc.DeleteRule(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) TestSend(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}
	if err := h.uc.TestSend(c.Request.Context(), uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "test alert dispatched"})
}

func (h *Handler) ListEvents(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	events, err := h.uc.ListEvents(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": events})
}
