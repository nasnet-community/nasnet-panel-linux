package audit

import (
	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/transport/http/middleware"
)

// AuditContext holds common audit fields extracted from request context
type AuditContext struct {
	ActorID   uint
	ActorName string
	IPAddress string
	RequestID string
}

// FromGinContext extracts audit context from a Gin request context
func FromGinContext(c *gin.Context) AuditContext {
	ac := AuditContext{
		IPAddress: c.ClientIP(),
	}

	if reqID, exists := c.Get(middleware.RequestIDKey); exists {
		ac.RequestID, _ = reqID.(string)
	}

	// Extract actor info from JWT claims set by auth middleware
	if userID, exists := c.Get("user_id"); exists {
		switch v := userID.(type) {
		case uint:
			ac.ActorID = v
		case float64:
			ac.ActorID = uint(v)
		}
	}
	if username, exists := c.Get("username"); exists {
		ac.ActorName, _ = username.(string)
	}

	return ac
}
