package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
)

type Handler struct {
	auditUC domain.AuditLogUsecase
}

func NewHandler(auditUC domain.AuditLogUsecase) *Handler {
	return &Handler{auditUC: auditUC}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/admin/audit", h.ListAuditLogs)
}

func (h *Handler) ListAuditLogs(c *gin.Context) {
	var filters domain.AuditListFilters

	filters.Action = c.Query("action")
	filters.EntityType = c.Query("entity_type")

	if v := c.Query("entity_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 32); err == nil {
			filters.EntityID = uint(id)
		}
	}
	if v := c.Query("actor_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 32); err == nil {
			filters.ActorID = uint(id)
		}
	}
	if v := c.Query("date_from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filters.DateFrom = &t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			filters.DateFrom = &t
		}
	}
	if v := c.Query("date_to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filters.DateTo = &t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			filters.DateTo = &t
		}
	}

	filters.Offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))
	filters.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))

	logs, total, err := h.auditUC.List(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    logs,
		"meta": gin.H{
			"total":  total,
			"offset": filters.Offset,
			"limit":  filters.Limit,
		},
	})
}
