package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
)

func (h *Handler) Health(c *gin.Context) {
	view, err := h.uc.HealthState(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *Handler) SetUplinkForce(c *gin.Context) {
	var req struct {
		State string `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.uc.SetUplinkForce(c.Request.Context(), c.Param("ifname"), req.State); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, usecase.ErrBadInput) {
			code = http.StatusBadRequest
		}
		fail(c, code, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"state": req.State}})
}
