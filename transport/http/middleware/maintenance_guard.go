package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	mntDomain "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/domain"
	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
)

const defaultUserMaintenanceNotice = "Service maintenance in progress. Purchases and renewals are temporarily paused."

// NewMaintenanceWriteGuard blocks mutating requests (non-GET/HEAD/OPTIONS)
// when maintenance is active. /sub/:uuid paths resolve per-sub/per-node/global;
// other paths check global only. Register on public groups only (admin
// routes are not subject to this guard).
func NewMaintenanceWriteGuard(uc mntUC.Usecase, subs subUC.SubscriptionUsecase) gin.HandlerFunc {
	return func(c *gin.Context) {
		m := c.Request.Method
		if m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		// Sub panel auth is access control, not a mutation. Allow during
		// maintenance so users can still log in to the read-only panel.
		if strings.HasSuffix(path, "/auth") && strings.Contains(path, "/api/v1/public/sub/") {
			c.Next()
			return
		}

		uuid := extractSubUUID(path)

		if uuid == "" {
			if uc.IsGlobalActive() {
				respondMaintenance(c, uc.Resolve(c.Request.Context(), 0, nil, defaultUserMaintenanceNotice))
				return
			}
			c.Next()
			return
		}

		sub, err := subs.GetByConfigID(c.Request.Context(), uuid)
		if err != nil {
			// Unknown UUID — defer to handler's own 404. Still enforce global flag.
			if uc.IsGlobalActive() {
				respondMaintenance(c, uc.Resolve(c.Request.Context(), 0, nil, defaultUserMaintenanceNotice))
				return
			}
			c.Next()
			return
		}
		id := sub.ID
		status := uc.Resolve(c.Request.Context(), sub.GetUserID(), &id, defaultUserMaintenanceNotice)
		if status.Active {
			respondMaintenance(c, status)
			return
		}
		c.Next()
	}
}

func respondMaintenance(c *gin.Context, status mntDomain.Status) {
	var sinceStr string
	if status.Since != nil {
		sinceStr = status.Since.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"success": false,
		"code":    "MAINTENANCE",
		"error": gin.H{
			"message": status.Message,
			"scope":   string(status.Scope),
			"since":   sinceStr,
		},
	})
}

// extractSubUUID returns the :uuid segment from any path containing "/sub/<uuid>"
// or "/api/v1/public/sub/<uuid>". Returns "" when not present.
func extractSubUUID(path string) string {
	_, rest, ok := strings.Cut(path, "/sub/")
	if !ok || rest == "" {
		return ""
	}
	uuid, _, _ := strings.Cut(rest, "/")
	return uuid
}
