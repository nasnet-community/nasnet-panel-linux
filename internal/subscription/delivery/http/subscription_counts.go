package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httputil"
	httpMiddleware "github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
)

// RegisterAdminRoutes must be registered on the same admin-authenticated group
// as the admin subscription list and status counts. It is fleet-wide, so it
// intentionally has no public registration.
func (h *Handler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	rg.GET("/admin/subscriptions/expiring-soon/count", h.CountExpiring)
}

func (h *Handler) CountExpiring(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "7"))
	if err != nil || days < 1 || days > 365 {
		httputil.Error(c, http.StatusBadRequest, "days must be between 1 and 365")
		return
	}
	count, err := h.subUsecase.CountExpiringSubscriptions(c.Request.Context(), days)
	if err != nil {
		httpMiddleware.LogAndRespondError(c, http.StatusInternalServerError, err, "")
		return
	}
	httputil.OK(c, gin.H{"count": count})
}
