package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

func (h *Handler) GetLAN(c *gin.Context) {
	view, err := h.uc.GetLAN(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// UpdateLAN routes through the two-phase apply: it writes .network files and
// restarts dnsmasq, so it arms the dead-man like any other network change.
func (h *Handler) UpdateLAN(c *gin.Context) {
	var cfg domain.LANConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	verdicts, view, err := h.uc.UpdateLAN(c.Request.Context(), cfg)
	switch {
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false, "error": err.Error(), "verdicts": verdicts})
	case view == nil:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "error": "validation failed", "verdicts": verdicts})
	default:
		c.JSON(http.StatusOK, gin.H{"success": true, "data": view, "verdicts": verdicts})
	}
}

func (h *Handler) ListPortForwards(c *gin.Context) {
	rows, err := h.uc.ListPortForwards(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// Confirmed is the operator's acknowledgement, not part of the stored row.
type portForwardBody struct {
	domain.PortForward
	Confirmed bool `json:"confirmed"`
}

func (h *Handler) CreatePortForward(c *gin.Context) {
	var req portForwardBody
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	verdicts, err := h.uc.CreatePortForward(c.Request.Context(), req.PortForward, req.Confirmed)
	respondPortForward(c, verdicts, err)
}

func (h *Handler) UpdatePortForward(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	var req portForwardBody
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	req.PortForward.ID = uint(id)

	verdicts, err := h.uc.UpdatePortForward(c.Request.Context(), req.PortForward, req.Confirmed)
	respondPortForward(c, verdicts, err)
}

func (h *Handler) DeletePortForward(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.DeletePortForward(c.Request.Context(), uint(id)); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func respondPortForward(c *gin.Context, verdicts []domain.Verdict, err error) {
	switch {
	case errors.Is(err, usecase.ErrValidationFailed):
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false, "error": "validation failed", "verdicts": verdicts})
	case errors.Is(err, usecase.ErrConfirmRequired):
		// 409, not 400: well-formed and permitted, just not yet acknowledged.
		c.JSON(http.StatusConflict, gin.H{
			"success": false, "error": "confirmation required", "verdicts": verdicts})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
	default:
		c.JSON(http.StatusOK, gin.H{"success": true, "verdicts": verdicts})
	}
}
