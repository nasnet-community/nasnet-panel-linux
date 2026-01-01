package http

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// SubLookup resolves a subscription config-ID (UUID) to its numeric ID.
type SubLookup func(ctx context.Context, configID string) (uint, error)

// WSHandler handles WebSocket connections for user (widget) chat sessions.
type WSHandler struct {
	chatUC      domain.ChatUsecase
	eventBus    *events.EventBus
	subLookup   SubLookup
	rateLimiter *ChatRateLimiter
}

// NewWSHandler creates a new user WebSocket handler.
func NewWSHandler(chatUC domain.ChatUsecase, eventBus *events.EventBus, subLookup SubLookup, rateLimiter *ChatRateLimiter) *WSHandler {
	return &WSHandler{
		chatUC:      chatUC,
		eventBus:    eventBus,
		subLookup:   subLookup,
		rateLimiter: rateLimiter,
	}
}

// RegisterRoutes registers the user WebSocket endpoint on the given router group.
func (h *WSHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/chat/ws", h.HandleUserWS)
}

// HandleUserWS upgrades to WebSocket and manages a user chat session.
func (h *WSHandler) HandleUserWS(c *gin.Context) {
	log := logger.GetLogger()
	subUUID := c.Param("uuid")

	targetSubID, err := h.subLookup(c.Request.Context(), subUUID)
	if err != nil {
		c.JSON(404, gin.H{"error": "Subscription not found"})
		return
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("Chat WS: Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(wsMaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	subscriberID := "chat-ws-user-" + uuid.New().String()
	eventCh := h.eventBus.SubscribeFiltered(subscriberID, func(e events.Event) bool {
		switch p := e.Payload.(type) {
		case events.ChatMessagePayload:
			return p.SubscriptionID == targetSubID
		case events.ChatTypingPayload:
			return p.SubscriptionID == targetSubID
		case events.ChatOnlineStatusPayload:
			return p.SubscriptionID == targetSubID
		case events.ChatMessageMutationPayload:
			return p.SubscriptionID == targetSubID
		case events.ChatReactionPayload:
			return p.SubscriptionID == targetSubID
		}
		return false
	})
	defer h.eventBus.Unsubscribe(subscriberID)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Publish user online on connect
	h.eventBus.Publish(events.Event{
		Type: events.EventChatOnlineStatus,
		Payload: events.ChatOnlineStatusPayload{
			SubscriptionID: targetSubID,
			IsOnline:       true,
			SenderType:     "user",
		},
	})
	// Publish user offline on disconnect
	defer func() {
		h.eventBus.Publish(events.Event{
			Type: events.EventChatOnlineStatus,
			Payload: events.ChatOnlineStatusPayload{
				SubscriptionID: targetSubID,
				IsOnline:       false,
				SenderType:     "user",
			},
		})
	}()

	var writeMu sync.Mutex

	log.Debugf("Chat WS: User connected for sub %d (subscriber: %s)", targetSubID, subscriberID)

	// Writer goroutine: EventBus events + ping ticker → WS frames
	go func() {
		pingTicker := time.NewTicker(wsPingPeriod)
		defer pingTicker.Stop()

		for {
			select {
			case event, ok := <-eventCh:
				if !ok {
					cancel()
					return
				}
				out, ok := filterUserEvent(event, targetSubID)
				if !ok {
					continue
				}
				writeMu.Lock()
				err := writeJSON(conn, out)
				writeMu.Unlock()
				if err != nil {
					log.Debugf("Chat WS: Write error for sub %d: %v", targetSubID, err)
					cancel()
					return
				}

			case <-pingTicker.C:
				writeMu.Lock()
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					log.Debugf("Chat WS: Ping error for sub %d: %v", targetSubID, err)
					cancel()
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	var lastTypingPub time.Time
	const typingMinGap = 1500 * time.Millisecond
	nonceSeen := make(map[string]uint)

	// Reader loop: parse incoming JSON messages
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Chat WS: reader panic for sub %d: %v", targetSubID, r)
		}
		cancel()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var msg WSIncoming
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Debugf("Chat WS: User disconnected normally for sub %d", targetSubID)
			} else {
				log.Debugf("Chat WS: Read error for sub %d: %v", targetSubID, err)
			}
			return
		}

		switch msg.Type {
		case WSTypeSendMessage:
			// Idempotent send: if we've seen this nonce, ack the original message.
			if msg.Nonce != "" {
				if existingID, ok := nonceSeen[msg.Nonce]; ok {
					writeMu.Lock()
					writeJSON(conn, WSOutgoing{
						Type:    WSTypeMessageAck,
						Nonce:   msg.Nonce,
						Message: &domain.ChatMessage{ID: existingID, SubscriptionID: targetSubID},
					})
					writeMu.Unlock()
					continue
				}
			}

			if msg.Content == "" {
				writeMu.Lock()
				writeJSON(conn, WSOutgoing{Type: WSTypeError, Error: "Content is required"})
				writeMu.Unlock()
				continue
			}

			// Rate limit check
			if errMsg := h.rateLimiter.checkLimit(subUUID, 3*time.Second, 30); errMsg != "" {
				writeMu.Lock()
				writeJSON(conn, WSOutgoing{Type: WSTypeError, Error: errMsg})
				writeMu.Unlock()
				continue
			}

			saved, err := h.chatUC.SendMessage(ctx, targetSubID, "user", nil, msg.Content, msg.ReplyToMessageID)
			if err != nil {
				writeMu.Lock()
				writeJSON(conn, WSOutgoing{Type: WSTypeError, Error: err.Error()})
				writeMu.Unlock()
				continue
			}

			if msg.Nonce != "" {
				nonceSeen[msg.Nonce] = saved.ID
			}

			writeMu.Lock()
			writeJSON(conn, WSOutgoing{Type: WSTypeMessageAck, Nonce: msg.Nonce, Message: saved})
			writeMu.Unlock()

		case WSTypeMarkRead:
			if err := h.chatUC.MarkAsRead(ctx, targetSubID, "user"); err != nil {
				writeMu.Lock()
				writeJSON(conn, WSOutgoing{Type: WSTypeError, Error: "Failed to mark as read"})
				writeMu.Unlock()
			}

		case WSTypeTyping:
			if time.Since(lastTypingPub) < typingMinGap {
				continue
			}
			lastTypingPub = time.Now()
			h.eventBus.Publish(events.Event{
				Type: events.EventChatTyping,
				Payload: events.ChatTypingPayload{
					SubscriptionID: targetSubID,
					SenderType:     "user",
				},
			})

		case WSTypePing:
			writeMu.Lock()
			writeJSON(conn, WSOutgoing{Type: WSTypePong})
			writeMu.Unlock()
		}
	}
}

// filterUserEvent filters EventBus events for the user-side WS stream.
// Returns the outgoing message and whether it should be forwarded.
func filterUserEvent(event events.Event, targetSubID uint) (WSOutgoing, bool) {
	switch event.Type {
	case events.EventChatAdminMessage:
		p, ok := event.Payload.(events.ChatMessagePayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		// Build a partial ChatMessage from the preview payload.
		// The client will invalidate its query cache to fetch the full message.
		partial := &domain.ChatMessage{
			ID:             p.MessageID,
			SubscriptionID: p.SubscriptionID,
			SenderType:     "admin",
			Content:        p.ContentPreview,
		}
		return WSOutgoing{Type: WSTypeNewMessage, Message: partial}, true

	case events.EventChatTyping:
		p, ok := event.Payload.(events.ChatTypingPayload)
		if !ok || p.SubscriptionID != targetSubID || p.SenderType != "admin" {
			return WSOutgoing{}, false
		}
		return WSOutgoing{Type: WSTypeTyping, SenderType: "admin"}, true

	case events.EventChatOnlineStatus:
		p, ok := event.Payload.(events.ChatOnlineStatusPayload)
		if !ok || p.SubscriptionID != targetSubID || p.SenderType != "admin" {
			return WSOutgoing{}, false
		}
		isOnline := p.IsOnline
		return WSOutgoing{Type: WSTypeOnlineStatus, SenderType: "admin", IsOnline: &isOnline}, true

	case events.EventChatMessageEdited:
		p, ok := event.Payload.(events.ChatMessageMutationPayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		mid := p.MessageID
		return WSOutgoing{Type: WSTypeMessageEdited, MessageID: &mid, Content: p.Content, EditedAt: p.EditedAt}, true

	case events.EventChatMessageDeleted:
		p, ok := event.Payload.(events.ChatMessageMutationPayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		mid := p.MessageID
		return WSOutgoing{Type: WSTypeMessageDeleted, MessageID: &mid}, true

	case events.EventChatReactionAdded:
		p, ok := event.Payload.(events.ChatReactionPayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		mid := p.MessageID
		return WSOutgoing{Type: WSTypeReactionAdded, MessageID: &mid, Reactor: p.Reactor, Emoji: p.Emoji}, true

	case events.EventChatReactionRemoved:
		p, ok := event.Payload.(events.ChatReactionPayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		mid := p.MessageID
		return WSOutgoing{Type: WSTypeReactionRemoved, MessageID: &mid, Reactor: p.Reactor, Emoji: p.Emoji}, true

	case events.EventChatMessagesRead:
		p, ok := event.Payload.(events.ChatMessagePayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		// User-side hears that admin marked the user's messages as read
		// (but it's of limited interest); broadcast as read receipt anyway.
		return WSOutgoing{Type: WSTypeMessagesRead}, true
	}

	return WSOutgoing{}, false
}
