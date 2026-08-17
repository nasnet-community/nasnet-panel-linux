package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

func (h *Handler) FlowGraph(c *gin.Context) {
	view, err := h.uc.FlowGraph(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// TraceFlow answers "where would this go" — it never sends a packet.
func (h *Handler) TraceFlow(c *gin.Context) {
	var req usecase.TraceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	view, err := h.uc.TraceFlow(c.Request.Context(), req)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, usecase.ErrBadTraceInput) {
			code = http.StatusBadRequest
		}
		fail(c, code, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *Handler) FlowConns(c *gin.Context) {
	view, err := h.uc.FlowConns(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *Handler) FlowEvents(c *gin.Context) {
	evs, err := h.uc.RecentNetworkEvents(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	if evs == nil {
		evs = []events.Event{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": evs})
}
