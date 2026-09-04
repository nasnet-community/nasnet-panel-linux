package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

func (h *Handler) PortMapStatus(c *gin.Context) {
	view, err := h.uc.PortMapStatus(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *Handler) ForcePortMapProbe(c *gin.Context) {
	h.uc.ForcePortMapProbe(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *Handler) ListPortMapRules(c *gin.Context) {
	rows, err := h.uc.ListPortMapRules(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// Confirmed is the operator's acknowledgement, not part of the stored row.
type portMapRuleBody struct {
	domain.PortMapRule
	Confirmed bool `json:"confirmed"`
}

func (h *Handler) CreatePortMapRule(c *gin.Context) {
	var req portMapRuleBody
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	verdicts, err := h.uc.CreatePortMapRule(c.Request.Context(), req.PortMapRule, req.Confirmed)
	respondPortForward(c, verdicts, err)
}

func (h *Handler) UpdatePortMapRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var req portMapRuleBody
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	req.PortMapRule.ID = uint(id)
	verdicts, err := h.uc.UpdatePortMapRule(c.Request.Context(), req.PortMapRule, req.Confirmed)
	respondPortForward(c, verdicts, err)
}

func (h *Handler) DeletePortMapRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.DeletePortMapRule(c.Request.Context(), uint(id)); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
