package http

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// chatExtraAllowedOrigins: extra Origin allowlist for chat WS upgrade
// beyond same-origin. From NASNET_WS_ALLOWED_ORIGINS env, comma-separated.
var chatExtraAllowedOrigins = loadChatExtraAllowedOrigins()

func loadChatExtraAllowedOrigins() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv("NASNET_WS_ALLOWED_ORIGINS"))
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out[strings.ToLower(s)] = struct{}{}
		}
	}
	return out
}

// chatCheckOrigin enforces same-origin for the chat WebSocket. Non-browser
// clients (empty Origin) are allowed — they already had to authenticate
// before reaching this handler. Browser clients must present an Origin
// that matches the request's Host, or be explicitly allow-listed via
// NASNET_WS_ALLOWED_ORIGINS for cross-origin panel deployments.
func chatCheckOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Non-browser client (CLI tooling, tests). Auth already happened
		// before reaching this handler; allow the upgrade.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Same-origin: Origin's host must equal the request Host.
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	// Explicit allow-list for split-domain panels.
	if _, ok := chatExtraAllowedOrigins[strings.ToLower(origin)]; ok {
		return true
	}
	logger.GetLogger().Warnf("Chat WS: rejecting cross-origin upgrade from %q (host %q)", origin, r.Host)
	return false
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     chatCheckOrigin,
}

// WSIncoming represents a message sent from the client to the server.
type WSIncoming struct {
	Type             string `json:"type"`
	Content          string `json:"content,omitempty"`
	Nonce            string `json:"nonce,omitempty"`
	ReplyToMessageID *uint  `json:"reply_to_message_id,omitempty"`
}

// WSOutgoing represents a message sent from the server to the client.
type WSOutgoing struct {
	Type       string              `json:"type"`
	Message    *domain.ChatMessage `json:"message,omitempty"`
	MessageID  *uint               `json:"message_id,omitempty"`
	Content    string              `json:"content,omitempty"`
	EditedAt   string              `json:"edited_at,omitempty"`
	SenderType string              `json:"sender_type,omitempty"`
	IsOnline   *bool               `json:"is_online,omitempty"`
	Error      string              `json:"error,omitempty"`
	Nonce      string              `json:"nonce,omitempty"`
	Reactor    string              `json:"reactor,omitempty"`
	Emoji      string              `json:"emoji,omitempty"`
}

const (
	WSTypeSendMessage     = "send_message"
	WSTypeTyping          = "typing"
	WSTypeMarkRead        = "mark_read"
	WSTypePing            = "ping"
	WSTypeNewMessage      = "new_message"
	WSTypeMessageAck      = "message_ack"
	WSTypeOnlineStatus    = "online_status"
	WSTypeMessagesRead    = "messages_read"
	WSTypePong            = "pong"
	WSTypeError           = "error"
	WSTypeMessageEdited   = "message_edited"
	WSTypeMessageDeleted  = "message_deleted"
	WSTypeReactionAdded   = "reaction_added"
	WSTypeReactionRemoved = "reaction_removed"
	WSTypeAdminRead       = "admin_messages_read"

	wsWriteWait  = 10 * time.Second
	wsPongWait   = 45 * time.Second
	wsPingPeriod = (wsPongWait * 9) / 10
	wsMaxMsgSize = 16 * 1024
)

func writeJSON(conn *websocket.Conn, msg WSOutgoing) error {
	conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	return conn.WriteJSON(msg)
}
