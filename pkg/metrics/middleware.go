package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// GinMiddleware returns a Gin middleware that instruments HTTP requests.
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Enabled.Load() {
			c.Next()
			return
		}
		HTTPRequestsInFlight.Inc()
		start := time.Now()

		c.Next()

		HTTPRequestsInFlight.Dec()

		status := strconv.Itoa(c.Writer.Status())
		path := c.FullPath()
		if path == "" {
			path = "/unknown"
		}
		method := c.Request.Method
		elapsed := time.Since(start).Seconds()

		HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(method, path, status).Observe(elapsed)
	}
}
