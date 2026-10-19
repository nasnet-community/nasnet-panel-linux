package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// ==================== Reverse Tunnel Operations ====================

func (h *Handler) ListReverseTunnels(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node ID"})
		return
	}
	tunnels, err := h.nodeUsecase.ListReverseTunnels(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tunnels})
}

func (h *Handler) GetReverseTunnel(c *gin.Context) {
	id, ok := reverseTunnelID(c)
	if !ok {
		return
	}
	t, err := h.nodeUsecase.GetReverseTunnel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "reverse tunnel not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": t})
}

func (h *Handler) CreateReverseTunnel(c *gin.Context) {
	var t domain.ReverseTunnel
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	view, err := h.nodeUsecase.CreateReverseTunnel(c.Request.Context(), &t)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": view})
}

func (h *Handler) UpdateReverseTunnel(c *gin.Context) {
	id, ok := reverseTunnelID(c)
	if !ok {
		return
	}
	var t domain.ReverseTunnel
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	view, err := h.nodeUsecase.UpdateReverseTunnel(c.Request.Context(), id, &t)
	if err != nil {
		reverseTunnelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *Handler) DeleteReverseTunnel(c *gin.Context) {
	id, ok := reverseTunnelID(c)
	if !ok {
		return
	}
	if err := h.nodeUsecase.DeleteReverseTunnel(c.Request.Context(), id); err != nil {
		reverseTunnelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "reverse tunnel deleted"})
}

func (h *Handler) RotateReverseTunnel(c *gin.Context) {
	id, ok := reverseTunnelID(c)
	if !ok {
		return
	}
	if err := h.nodeUsecase.RotateReverseTunnel(c.Request.Context(), id); err != nil {
		reverseTunnelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "reverse tunnel id rotated"})
}

func (h *Handler) ReverseTunnelPeerConfig(c *gin.Context) {
	id, ok := reverseTunnelID(c)
	if !ok {
		return
	}
	peer, err := h.nodeUsecase.ReverseTunnelPeerConfig(c.Request.Context(), id)
	if err != nil {
		reverseTunnelError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": peer})
}

func reverseTunnelID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return 0, false
	}
	return uint(id), true
}

func reverseTunnelError(c *gin.Context, err error) {
	if strings.HasPrefix(err.Error(), "reverse tunnel not found") {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
