package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// LogAndRespondError logs the error server-side with context and returns a JSON error response.
func LogAndRespondError(c *gin.Context, status int, err error, publicMsg string) {
	log := logger.GetLogger()
	fields := map[string]interface{}{
		"status": status,
		"method": c.Request.Method,
		"path":   c.Request.URL.Path,
	}
	if reqID := GetRequestID(c.Request.Context()); reqID != "" {
		fields["request_id"] = reqID
	}
	if userID, exists := c.Get("user_id"); exists {
		fields["user_id"] = userID
	}

	if status >= http.StatusInternalServerError {
		log.WithError(err).WithFields(fields).Error("HTTP handler error")
	} else {
		log.WithError(err).WithFields(fields).Warn("HTTP handler client error")
	}

	msg := publicMsg
	if msg == "" {
		if status >= http.StatusInternalServerError {
			msg = "internal server error"
		} else {
			msg = err.Error()
		}
	}
	c.JSON(status, gin.H{"success": false, "error": msg})
}
