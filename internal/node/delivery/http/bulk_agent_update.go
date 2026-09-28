package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
)

// streamBulkAgentUpdate keeps a multi-wave agent update alive through the
// server's write deadline and reverse-proxy idle timeouts. JSON permits leading
// whitespace, so heartbeats preserve the ordinary bulk-action response shape.
// Request validation must finish before entering this method and sending 200.
func (h *Handler) streamBulkAgentUpdate(c *gin.Context, ids []uint) {
	const heartbeatInterval = 15 * time.Second

	writer := http.ResponseWriter(c.Writer)
	if unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter }); ok {
		// Bypass Gin's Flush wrapper so ResponseController can observe
		// transport flush errors and cancel work when the client disconnects.
		writer = unwrapper.Unwrap()
	}
	controller := http.NewResponseController(writer)
	refreshDeadline := func() error {
		err := controller.SetWriteDeadline(time.Now().Add(2 * heartbeatInterval))
		if errors.Is(err, http.ErrNotSupported) {
			return nil // e.g. httptest.ResponseRecorder
		}
		return err
	}
	if err := refreshDeadline(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to prepare update response"})
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	writeHeartbeat := func() error {
		if err := refreshDeadline(); err != nil {
			return err
		}
		if _, err := c.Writer.Write([]byte("\n")); err != nil {
			return err
		}
		return controller.Flush()
	}
	if err := writeHeartbeat(); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	results := make(chan map[uint]*usecase.NodeBulkActionResult, 1)
	go func() {
		results <- h.nodeUsecase.BulkUpdateAgent(ctx, ids)
	}()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := writeHeartbeat(); err != nil {
				return
			}
		case result := <-results:
			if err := refreshDeadline(); err != nil {
				return
			}
			_ = json.NewEncoder(c.Writer).Encode(bulkActionResponse(result))
			return
		}
	}
}
