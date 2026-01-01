package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const RequestIDKey = "request_id"

type requestIDKeyType struct{}

// RequestIDCtxKey is the context key for request ID propagation.
var RequestIDCtxKey = requestIDKeyType{}

// RequestIDMiddleware generates a unique request ID for each request
// and propagates it via both Gin context and context.Context.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.New().String()
		c.Set(RequestIDKey, id)
		c.Writer.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(c.Request.Context(), RequestIDCtxKey, id)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// GetRequestID extracts the request ID from a context.Context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDCtxKey).(string); ok {
		return id
	}
	return ""
}
