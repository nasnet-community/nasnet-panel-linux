package domain

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrMessageNotFound is returned when a chat message cannot be located
// (e.g. wrong subscription scope, soft-deleted, or never existed).
var ErrMessageNotFound = errors.New("chat: message not found")

// ChatMessage represents a single message in a subscription's chat thread.
type ChatMessage struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	SubscriptionID   uint           `gorm:"index:idx_chat_sub_created,priority:1;index:idx_chat_sub_read,priority:1;index:idx_chat_sub_sender_read,priority:1;not null" json:"subscription_id"`
	SenderType       string         `gorm:"size:10;not null;index:idx_chat_sub_sender_read,priority:2" json:"sender_type"` // "user" or "admin"
	AdminID          *uint          `json:"admin_id,omitempty"`
	Content          string         `gorm:"type:text;not null" json:"content"`
	IsRead           bool           `gorm:"default:false;index:idx_chat_sub_read,priority:2;index:idx_chat_sub_sender_read,priority:3" json:"is_read"`
	IsPinned         bool           `gorm:"default:false" json:"is_pinned"`
	ReplyToMessageID *uint          `gorm:"index" json:"reply_to_message_id,omitempty"`
	CreatedAt        time.Time      `gorm:"index:idx_chat_sub_created,priority:2" json:"created_at"`
	EditedAt         *time.Time     `json:"edited_at,omitempty"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ChatMessage) TableName() string {
	return "chat_messages"
}

// ChatReaction stores a single emoji reaction by either the subscription's user
// (Reactor == "user", AdminID nil) or by an admin (Reactor == "admin", AdminID set).
// Composite-unique on (message_id, reactor, admin_id, emoji).
type ChatReaction struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MessageID uint      `gorm:"index:idx_reaction_msg;uniqueIndex:uniq_reaction,priority:1;not null" json:"message_id"`
	Reactor   string    `gorm:"size:10;uniqueIndex:uniq_reaction,priority:2;not null" json:"reactor"`
	AdminID   *uint     `gorm:"uniqueIndex:uniq_reaction,priority:3" json:"admin_id,omitempty"`
	Emoji     string    `gorm:"size:32;uniqueIndex:uniq_reaction,priority:4;not null" json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

func (ChatReaction) TableName() string { return "chat_reactions" }

// ConversationSummary is a query projection for the admin conversation list.
type ConversationSummary struct {
	SubscriptionID     uint      `json:"subscription_id"`
	SubscriptionLabel  string    `json:"subscription_label"`
	SubscriptionStatus string    `json:"subscription_status"`
	UserID             *uint     `json:"user_id,omitempty"`
	Username           string    `json:"username,omitempty"`
	FirstName          string    `json:"first_name,omitempty"`
	LastMessageContent string    `json:"last_message_content"`
	LastMessageAt      time.Time `json:"last_message_at"`
	LastSenderType     string    `json:"last_sender_type"`
	UnreadCount        int64     `json:"unread_count"`
}

// ConversationFilters contains filter/sort/pagination options for the conversation list.
type ConversationFilters struct {
	Page        int
	Limit       int
	Search      string
	Status      string
	UnreadOnly  bool
	MineAdminID *uint
	PinnedOnly  bool
	SortBy      string
}

// ChatRepository defines the data access interface for chat messages.
type ChatRepository interface {
	Create(ctx context.Context, msg *ChatMessage) error
	ListBySubscription(ctx context.Context, subID uint, page, limit int) ([]ChatMessage, int64, error)
	SearchInSubscription(ctx context.Context, subID uint, q string, page, limit int) ([]ChatMessage, int64, error)
	MarkAsRead(ctx context.Context, subID uint) error
	MarkAsReadBySender(ctx context.Context, subID uint, senderType string) error
	GetTotalUnreadCount(ctx context.Context) (int64, error)
	GetConversationList(ctx context.Context, filters ConversationFilters) ([]ConversationSummary, int64, error)
	SetPinnedScoped(ctx context.Context, messageID, subID uint, pinned bool) (int64, error)
	GetPinnedMessages(ctx context.Context, subID uint) ([]ChatMessage, error)
	DeleteOlderThan(ctx context.Context, days int) (int64, error)
	EditMessageScoped(ctx context.Context, messageID, subID uint, content string) (int64, error)
	DeleteMessageScoped(ctx context.Context, messageID, subID uint) (int64, error)
	GetByID(ctx context.Context, messageID uint) (*ChatMessage, error)
	AddReaction(ctx context.Context, r *ChatReaction) error
	RemoveReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, emoji string) (int64, error)
	ReplaceReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, oldEmoji, newEmoji string) (int64, error)
	ListReactionsByMessage(ctx context.Context, messageID uint) ([]ChatReaction, error)
	ListReactionsByMessages(ctx context.Context, messageIDs []uint) ([]ChatReaction, error)
}

// ChatUsecase defines the business logic interface for chat.
type ChatUsecase interface {
	SendMessage(ctx context.Context, subID uint, senderType string, adminID *uint, content string, replyToMessageID *uint) (*ChatMessage, error)
	GetMessages(ctx context.Context, subID uint, page, limit int) ([]ChatMessage, int64, error)
	GetMessageByID(ctx context.Context, messageID uint) (*ChatMessage, error)
	SearchMessages(ctx context.Context, subID uint, q string, page, limit int) ([]ChatMessage, int64, error)
	MarkAsRead(ctx context.Context, subID uint, callerType string) error
	GetConversations(ctx context.Context, filters ConversationFilters) ([]ConversationSummary, int64, error)
	GetTotalUnreadCount(ctx context.Context) (int64, error)
	CleanupOldMessages(ctx context.Context) error
	PinMessage(ctx context.Context, messageID, subID uint) error
	UnpinMessage(ctx context.Context, messageID, subID uint) error
	GetPinnedMessages(ctx context.Context, subID uint) ([]ChatMessage, error)
	EditMessage(ctx context.Context, messageID, subID uint, callerType string, callerAdminID *uint, content string) (*ChatMessage, error)
	DeleteMessage(ctx context.Context, messageID, subID uint, callerType string, callerAdminID *uint) error
	AddReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, emoji string) error
	RemoveReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, emoji string) error
	ReplaceReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, oldEmoji, newEmoji string) error
	ListReactions(ctx context.Context, messageID uint) ([]ChatReaction, error)
	ListReactionsBatch(ctx context.Context, messageIDs []uint) ([]ChatReaction, error)
}
