package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// Handler manages SSE connections for real-time events
type Handler struct {
	eventBus *events.EventBus
}

// NewHandler creates a new events HTTP handler
func NewHandler(eventBus *events.EventBus) *Handler {
	return &Handler{eventBus: eventBus}
}

// RegisterRoutes registers the events routes
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/events/stream", h.StreamEvents)
}

// StreamEvents handles SSE connections for real-time event streaming.
// Writes SSE frames directly to the underlying ResponseWriter to avoid
// buffering that can occur with Gin's c.SSEvent / c.Stream helpers.
func (h *Handler) StreamEvents(c *gin.Context) {
	log := logger.GetLogger()

	// Remove the server's WriteTimeout deadline for this long-lived SSE connection.
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetWriteDeadline(time.Time{})

	// Generate unique subscriber ID
	subscriberID := uuid.New().String()

	// Set SSE headers — do NOT set Transfer-Encoding manually; Go handles it.
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// Subscribe to event bus
	eventCh := h.eventBus.Subscribe(subscriberID)
	defer h.eventBus.Unsubscribe(subscriberID)

	log.Infof("Events SSE: Client %s connected", subscriberID)

	// Send initial connection event and flush immediately
	fmt.Fprintf(c.Writer, "event: connected\ndata: {\"subscriber_id\":%q,\"message\":\"Connected to event stream\"}\n\n", subscriberID)
	rc.Flush()

	ctx := c.Request.Context()

	// Heartbeat keeps the connection alive through proxies/load balancers
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				log.Debugf("Events SSE: Channel closed for %s", subscriberID)
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				log.Errorf("Events SSE: Marshal error for %s: %v", subscriberID, err)
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
				log.Debugf("Events SSE: Write failed for %s: %v", subscriberID, err)
				return
			}
			if err := rc.Flush(); err != nil {
				log.Debugf("Events SSE: Flush failed for %s: %v", subscriberID, err)
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				log.Debugf("Events SSE: Heartbeat write failed for %s: %v", subscriberID, err)
				return
			}
			if err := rc.Flush(); err != nil {
				log.Debugf("Events SSE: Heartbeat flush failed for %s: %v", subscriberID, err)
				return
			}
		case <-ctx.Done():
			log.Infof("Events SSE: Client %s disconnected", subscriberID)
			return
		}
	}
}
