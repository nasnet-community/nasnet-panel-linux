package http

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nasnet-community/nasnet-panel-linux/internal/auth/middleware"
	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// WSAdminHandler handles WebSocket connections for admin per-conversation chat sessions.
type WSAdminHandler struct {
	chatUC      domain.ChatUsecase
	eventBus    *events.EventBus
	rateLimiter *ChatRateLimiter
}

// NewWSAdminHandler creates a new admin WebSocket handler.
func NewWSAdminHandler(chatUC domain.ChatUsecase, eventBus *events.EventBus, rateLimiter *ChatRateLimiter) *WSAdminHandler {
	return &WSAdminHandler{
		chatUC:      chatUC,
		eventBus:    eventBus,
		rateLimiter: rateLimiter,
	}
}

// RegisterRoutes registers the admin WebSocket endpoint on the given router group.
func (h *WSAdminHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/admin/chats/:subscriptionId/ws", h.HandleAdminWS)
}

// HandleAdminWS upgrades to WebSocket and manages an admin chat session for a specific conversation.
func (h *WSAdminHandler) HandleAdminWS(c *gin.Context) {
	log := logger.GetLogger()

	subID, err := strconv.ParseUint(c.Param("subscriptionId"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid subscription ID"})
		return
	}
	targetSubID := uint(subID)

	adminID, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("Chat Admin WS: Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(wsMaxMsgSize)
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	subscriberID := "chat-ws-admin-" + uuid.New().String()
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

	// Publish admin online on connect
	h.eventBus.Publish(events.Event{
		Type: events.EventChatOnlineStatus,
		Payload: events.ChatOnlineStatusPayload{
			SubscriptionID: targetSubID,
			IsOnline:       true,
			SenderType:     "admin",
		},
	})
	// Publish admin offline on disconnect
	defer func() {
		h.eventBus.Publish(events.Event{
			Type: events.EventChatOnlineStatus,
			Payload: events.ChatOnlineStatusPayload{
				SubscriptionID: targetSubID,
				IsOnline:       false,
				SenderType:     "admin",
			},
		})
	}()

	var writeMu sync.Mutex

	log.Debugf("Chat Admin WS: Admin %d connected for sub %d (subscriber: %s)", adminID, targetSubID, subscriberID)

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
				out, ok := filterAdminEvent(event, targetSubID)
				if !ok {
					continue
				}
				writeMu.Lock()
				err := writeJSON(conn, out)
				writeMu.Unlock()
				if err != nil {
					log.Debugf("Chat Admin WS: Write error for sub %d: %v", targetSubID, err)
					cancel()
					return
				}

			case <-pingTicker.C:
				writeMu.Lock()
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					log.Debugf("Chat Admin WS: Ping error for sub %d: %v", targetSubID, err)
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
			log.Errorf("Chat Admin WS: reader panic for sub %d: %v", targetSubID, r)
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
				log.Debugf("Chat Admin WS: Admin disconnected normally for sub %d", targetSubID)
			} else {
				log.Debugf("Chat Admin WS: Read error for sub %d: %v", targetSubID, err)
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

			key := "admin:" + strconv.FormatUint(uint64(adminID), 10)
			if errMsg := h.rateLimiter.checkLimit(key, 1*time.Second, 120); errMsg != "" {
				writeMu.Lock()
				writeJSON(conn, WSOutgoing{Type: WSTypeError, Error: errMsg})
				writeMu.Unlock()
				continue
			}

			aid := adminID
			saved, err := h.chatUC.SendMessage(ctx, targetSubID, "admin", &aid, msg.Content, msg.ReplyToMessageID)
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

		case WSTypeTyping:
			if time.Since(lastTypingPub) < typingMinGap {
				continue
			}
			lastTypingPub = time.Now()
			h.eventBus.Publish(events.Event{
				Type: events.EventChatTyping,
				Payload: events.ChatTypingPayload{
					SubscriptionID: targetSubID,
					SenderType:     "admin",
				},
			})

		case WSTypeMarkRead:
			if err := h.chatUC.MarkAsRead(ctx, targetSubID, "admin"); err != nil {
				writeMu.Lock()
				writeJSON(conn, WSOutgoing{Type: WSTypeError, Error: "Failed to mark as read"})
				writeMu.Unlock()
			}

		case WSTypePing:
			writeMu.Lock()
			writeJSON(conn, WSOutgoing{Type: WSTypePong})
			writeMu.Unlock()
		}
	}
}

// filterAdminEvent filters EventBus events for the admin-side WS stream.
// Returns the outgoing message and whether it should be forwarded.
func filterAdminEvent(event events.Event, targetSubID uint) (WSOutgoing, bool) {
	switch event.Type {
	case events.EventChatUserMessage:
		p, ok := event.Payload.(events.ChatMessagePayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		partial := &domain.ChatMessage{
			ID:             p.MessageID,
			SubscriptionID: p.SubscriptionID,
			SenderType:     "user",
			Content:        p.ContentPreview,
		}
		return WSOutgoing{Type: WSTypeNewMessage, Message: partial}, true

	case events.EventChatTyping:
		p, ok := event.Payload.(events.ChatTypingPayload)
		if !ok || p.SubscriptionID != targetSubID || p.SenderType != "user" {
			return WSOutgoing{}, false
		}
		return WSOutgoing{Type: WSTypeTyping, SenderType: "user"}, true

	case events.EventChatOnlineStatus:
		p, ok := event.Payload.(events.ChatOnlineStatusPayload)
		if !ok || p.SubscriptionID != targetSubID || p.SenderType != "user" {
			return WSOutgoing{}, false
		}
		isOnline := p.IsOnline
		return WSOutgoing{Type: WSTypeOnlineStatus, SenderType: "user", IsOnline: &isOnline}, true

	case events.EventChatMessagesRead:
		p, ok := event.Payload.(events.ChatMessagePayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		return WSOutgoing{Type: WSTypeMessagesRead}, true

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

	case events.EventChatAdminMessagesRead:
		p, ok := event.Payload.(events.ChatMessagePayload)
		if !ok || p.SubscriptionID != targetSubID {
			return WSOutgoing{}, false
		}
		return WSOutgoing{Type: WSTypeAdminRead}, true
	}

	return WSOutgoing{}, false
}
