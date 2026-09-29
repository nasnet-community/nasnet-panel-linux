package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
)

// GetStarlinkStatus returns live Starlink dish status for a node.
func (h *Handler) GetStarlinkStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	status, err := h.nodeUsecase.GetStarlinkStatus(c.Request.Context(), uint(id))
	if err != nil {
		starlinkError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

// GetStarlinkObstructionMap returns the live obstruction map for a node's Starlink dish.
func (h *Handler) GetStarlinkObstructionMap(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	m, err := h.nodeUsecase.GetStarlinkObstructionMap(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": m})
}

// GetStarlinkHistory returns historical Starlink stats for a node.
func (h *Handler) GetStarlinkHistory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return
	}

	limit := 60
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	var since *time.Time
	if s := c.Query("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = &t
		}
	}

	stats, err := h.nodeUsecase.GetStarlinkHistory(c.Request.Context(), uint(id), limit, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": stats})
}

// starlinkError sends usecase errors that carry a code as 422 so the panel
// can explain them; everything else stays a 500.
func starlinkError(c *gin.Context, err error) {
	if code := usecase.StarlinkErrorCode(err); code != "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "error": err.Error(), "code": code})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
}

func starlinkNodeID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid id"})
		return 0, false
	}
	return uint(id), true
}

// GetStarlinkLive returns the dish's own 15-minute 1 s history, bucketed.
func (h *Handler) GetStarlinkLive(c *gin.Context) {
	id, ok := starlinkNodeID(c)
	if !ok {
		return
	}
	step := 5
	if s, err := strconv.Atoi(c.Query("step")); err == nil {
		step = min(max(s, 1), 60)
	}
	live, err := h.nodeUsecase.GetStarlinkLive(c.Request.Context(), id, step)
	if err != nil {
		starlinkError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": live})
}

func (h *Handler) GetStarlinkDevice(c *gin.Context) {
	id, ok := starlinkNodeID(c)
	if !ok {
		return
	}
	dev, err := h.nodeUsecase.GetStarlinkDevice(c.Request.Context(), id)
	if err != nil {
		starlinkError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": dev})
}

func (h *Handler) GetStarlinkEvents(c *gin.Context) {
	id, ok := starlinkNodeID(c)
	if !ok {
		return
	}
	since := time.Now().Add(-24 * time.Hour)
	if s := c.Query("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	events, err := h.nodeUsecase.GetStarlinkEvents(c.Request.Context(), id, since, limit)
	if err != nil {
		starlinkError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": events})
}

type starlinkActionBody struct {
	Confirm string `json:"confirm"`
}

func (h *Handler) RunStarlinkAction(c *gin.Context) {
	id, ok := starlinkNodeID(c)
	if !ok {
		return
	}
	var body starlinkActionBody
	_ = c.ShouldBindJSON(&body) // body is optional; only factory reset needs it
	ac := audit.FromGinContext(c)
	res, err := h.nodeUsecase.RunStarlinkAction(c.Request.Context(), id, usecase.StarlinkActionRequest{
		Action:    c.Param("action"),
		Confirm:   body.Confirm,
		ActorID:   ac.ActorID,
		ActorName: ac.ActorName,
		IPAddress: ac.IPAddress,
	})
	if err != nil {
		starlinkError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}
