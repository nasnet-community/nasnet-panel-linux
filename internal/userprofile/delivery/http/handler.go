package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"
	_ "time/tzdata" // tz names for grouping chat days, even on hosts without zoneinfo

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/userprofile/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
)

type Handler struct {
	uc usecase.Usecase
}

func NewHandler(uc usecase.Usecase) *Handler { return &Handler{uc: uc} }

// RegisterAdminRoutes mounts the user-page read models under /admin. The
// group must already require admin auth.
func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	admin := rg.Group("/admin")
	admin.GET("/users/:id/overview", h.Overview)
	admin.GET("/users/:id/timeline", h.Timeline)
	admin.GET("/subscriptions/:id/usage-pattern", h.SubscriptionUsagePattern)
}

func parseID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		httputil.Error(c, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return uint(id), true
}

func fail(c *gin.Context, err error) {
	if errors.Is(err, usecase.ErrNotFound) {
		httputil.Error(c, http.StatusNotFound, "not found")
		return
	}
	httputil.Error(c, http.StatusInternalServerError, err.Error())
}

func (h *Handler) Overview(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	out, err := h.uc.Overview(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	httputil.OK(c, out)
}

// Timeline: ?kind=service|notice|admin&include_views=1
// &before=<next_cursor>&limit=&tz=Europe/London (tz groups chat by the
// viewer's day).
func (h *Handler) Timeline(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	q := usecase.TimelineQuery{Kind: c.Query("kind"), IncludeViews: c.Query("include_views") == "1"}
	switch q.Kind {
	case "", usecase.KindService, usecase.KindNotice, usecase.KindAdmin:
	default:
		httputil.Error(c, http.StatusBadRequest, "invalid kind")
		return
	}
	q.Before = c.Query("before")
	q.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "100"))
	if tz := c.Query("tz"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			q.Location = loc
		}
	}
	out, err := h.uc.Timeline(c.Request.Context(), id, q)
	if err != nil {
		fail(c, err)
		return
	}
	httputil.OK(c, out)
}

func (h *Handler) SubscriptionUsagePattern(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	points, err := h.uc.SubscriptionUsagePattern(c.Request.Context(), id, days)
	if err != nil {
		fail(c, err)
		return
	}
	httputil.OK(c, points)
}
