package usecase

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

const (
	maxContentLength = 2000
	userEditWindow   = 5 * time.Minute
)

type chatUsecase struct {
	repo       domain.ChatRepository
	settingUC  settingDomain.SettingUsecase
	eventBus   *events.EventBus
	subLabelFn func(ctx context.Context, subID uint) string
}

func NewChatUsecase(
	repo domain.ChatRepository,
	settingUC settingDomain.SettingUsecase,
	eventBus *events.EventBus,
	subLabelFn func(ctx context.Context, subID uint) string,
) domain.ChatUsecase {
	return &chatUsecase{
		repo:       repo,
		settingUC:  settingUC,
		eventBus:   eventBus,
		subLabelFn: subLabelFn,
	}
}

func (u *chatUsecase) SendMessage(ctx context.Context, subID uint, senderType string, adminID *uint, content string, replyToMessageID *uint) (*domain.ChatMessage, error) {
	if len(content) > maxContentLength*4 {
		return nil, errors.New("message content too large")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("message content cannot be empty")
	}
	if len([]rune(content)) > maxContentLength {
		return nil, errors.New("message content exceeds maximum length of 2000 characters")
	}
	if senderType != "user" && senderType != "admin" {
		return nil, errors.New("invalid sender type")
	}

	if senderType == "user" {
		enabled, err := u.settingUC.GetByKey(ctx, "chat_enabled")
		if err != nil {
			logger.GetLogger().WithError(err).Warn("Chat: failed to read chat_enabled setting; treating chat as disabled")
		}
		if !strings.EqualFold(enabled, "true") {
			return nil, errors.New("chat is disabled")
		}
	}

	if replyToMessageID != nil {
		orig, err := u.repo.GetByID(ctx, *replyToMessageID)
		if err != nil || orig.SubscriptionID != subID {
			return nil, errors.New("invalid reply target")
		}
	}

	msg := &domain.ChatMessage{
		SubscriptionID:   subID,
		SenderType:       senderType,
		AdminID:          adminID,
		Content:          content,
		IsRead:           false,
		ReplyToMessageID: replyToMessageID,
	}

	if err := u.repo.Create(ctx, msg); err != nil {
		return nil, err
	}

	preview := content
	if runes := []rune(preview); len(runes) > 100 {
		preview = string(runes[:100]) + "..."
	}

	label := ""
	if u.subLabelFn != nil {
		label = u.subLabelFn(ctx, subID)
	}

	if senderType == "user" {
		u.eventBus.Publish(events.Event{
			Type: events.EventChatUserMessage,
			Payload: events.ChatMessagePayload{
				SubscriptionID:    subID,
				MessageID:         msg.ID,
				ContentPreview:    preview,
				SubscriptionLabel: label,
				SenderType:        "user",
			},
		})
		u.eventBus.Publish(events.Event{
			Type: events.EventChatNewMessage,
			Payload: events.ChatMessagePayload{
				SubscriptionID:    subID,
				MessageID:         msg.ID,
				ContentPreview:    preview,
				SubscriptionLabel: label,
				SenderType:        "user",
			},
		})
	} else {
		u.eventBus.Publish(events.Event{
			Type: events.EventChatAdminMessage,
			Payload: events.ChatMessagePayload{
				SubscriptionID: subID,
				MessageID:      msg.ID,
				SenderType:     "admin",
			},
		})
	}

	return msg, nil
}

func (u *chatUsecase) GetMessages(ctx context.Context, subID uint, page, limit int) ([]domain.ChatMessage, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return u.repo.ListBySubscription(ctx, subID, page, limit)
}

// MarkAsRead flips messages produced by the *other* side to read for this
// subscription. callerType in {"user","admin"}.
func (u *chatUsecase) MarkAsRead(ctx context.Context, subID uint, callerType string) error {
	var senderType string
	var evType events.EventType
	switch callerType {
	case "admin":
		senderType = "user"
		evType = events.EventChatMessagesRead
	case "user":
		senderType = "admin"
		evType = events.EventChatAdminMessagesRead
	default:
		return errors.New("invalid caller")
	}
	if err := u.repo.MarkAsReadBySender(ctx, subID, senderType); err != nil {
		return err
	}
	u.eventBus.Publish(events.Event{
		Type:    evType,
		Payload: events.ChatMessagePayload{SubscriptionID: subID},
	})
	return nil
}

func (u *chatUsecase) GetConversations(ctx context.Context, filters domain.ConversationFilters) ([]domain.ConversationSummary, int64, error) {
	if filters.Page < 1 {
		filters.Page = 1
	}
	if filters.Limit < 1 || filters.Limit > 100 {
		filters.Limit = 20
	}
	return u.repo.GetConversationList(ctx, filters)
}

func (u *chatUsecase) PinMessage(ctx context.Context, messageID, subID uint) error {
	rows, err := u.repo.SetPinnedScoped(ctx, messageID, subID, true)
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrMessageNotFound
	}
	return nil
}

func (u *chatUsecase) UnpinMessage(ctx context.Context, messageID, subID uint) error {
	rows, err := u.repo.SetPinnedScoped(ctx, messageID, subID, false)
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrMessageNotFound
	}
	return nil
}

func (u *chatUsecase) GetPinnedMessages(ctx context.Context, subID uint) ([]domain.ChatMessage, error) {
	return u.repo.GetPinnedMessages(ctx, subID)
}

func (u *chatUsecase) GetTotalUnreadCount(ctx context.Context) (int64, error) {
	return u.repo.GetTotalUnreadCount(ctx)
}

func (u *chatUsecase) CleanupOldMessages(ctx context.Context) error {
	log := logger.GetLogger()
	daysStr, err := u.settingUC.GetByKey(ctx, "retention_chat_messages_days")
	if err != nil || daysStr == "" {
		return nil
	}
	days, err := strconv.Atoi(daysStr)
	if err != nil || days <= 0 {
		return nil
	}
	deleted, err := u.repo.DeleteOlderThan(ctx, days)
	if err != nil {
		log.WithError(err).Warn("Retention: failed to cleanup chat messages")
		return err
	}
	if deleted > 0 {
		log.WithField("deleted", deleted).Info("Retention: cleaned up old chat messages")
	}
	return nil
}

func (u *chatUsecase) SearchMessages(ctx context.Context, subID uint, q string, page, limit int) ([]domain.ChatMessage, int64, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []domain.ChatMessage{}, 0, nil
	}
	if len(q) > 200 {
		q = q[:200]
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	return u.repo.SearchInSubscription(ctx, subID, q, page, limit)
}

func (u *chatUsecase) EditMessage(ctx context.Context, messageID, subID uint, callerType string, callerAdminID *uint, content string) (*domain.ChatMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("message content cannot be empty")
	}
	if len([]rune(content)) > maxContentLength {
		return nil, errors.New("message content exceeds maximum length")
	}
	original, err := u.repo.GetByID(ctx, messageID)
	if err != nil {
		return nil, domain.ErrMessageNotFound
	}
	if original.SubscriptionID != subID {
		return nil, domain.ErrMessageNotFound
	}
	switch callerType {
	case "admin":
		if original.SenderType != "admin" {
			return nil, errors.New("admin can only edit admin messages")
		}
	case "user":
		if original.SenderType != "user" {
			return nil, errors.New("user can only edit own messages")
		}
		if time.Since(original.CreatedAt) > userEditWindow {
			return nil, errors.New("edit window expired")
		}
	default:
		return nil, errors.New("invalid caller")
	}
	n, err := u.repo.EditMessageScoped(ctx, messageID, subID, content)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, domain.ErrMessageNotFound
	}
	updated, _ := u.repo.GetByID(ctx, messageID)
	if updated != nil {
		editedAt := ""
		if updated.EditedAt != nil {
			editedAt = updated.EditedAt.Format(time.RFC3339)
		}
		u.eventBus.Publish(events.Event{
			Type: events.EventChatMessageEdited,
			Payload: events.ChatMessageMutationPayload{
				SubscriptionID: subID,
				MessageID:      messageID,
				Content:        updated.Content,
				EditedAt:       editedAt,
				By:             callerType,
			},
		})
	}
	return updated, nil
}

func (u *chatUsecase) DeleteMessage(ctx context.Context, messageID, subID uint, callerType string, callerAdminID *uint) error {
	original, err := u.repo.GetByID(ctx, messageID)
	if err != nil {
		return domain.ErrMessageNotFound
	}
	if original.SubscriptionID != subID {
		return domain.ErrMessageNotFound
	}
	switch callerType {
	case "admin":
		// admin may delete any message in its conversation
	case "user":
		if original.SenderType != "user" {
			return errors.New("user can only delete own messages")
		}
		if time.Since(original.CreatedAt) > userEditWindow {
			return errors.New("delete window expired")
		}
	default:
		return errors.New("invalid caller")
	}
	n, err := u.repo.DeleteMessageScoped(ctx, messageID, subID)
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrMessageNotFound
	}
	u.eventBus.Publish(events.Event{
		Type: events.EventChatMessageDeleted,
		Payload: events.ChatMessageMutationPayload{
			SubscriptionID: subID,
			MessageID:      messageID,
			By:             callerType,
		},
	})
	return nil
}

func (u *chatUsecase) GetMessageByID(ctx context.Context, messageID uint) (*domain.ChatMessage, error) {
	return u.repo.GetByID(ctx, messageID)
}

var allowedReactionEmojis = map[string]struct{}{
	"👍": {}, "❤️": {}, "😂": {}, "🎉": {}, "🤔": {}, "😢": {},
}

func (u *chatUsecase) AddReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, emoji string) error {
	if _, ok := allowedReactionEmojis[emoji]; !ok {
		return errors.New("emoji not allowed")
	}
	if reactor != "user" && reactor != "admin" {
		return errors.New("invalid reactor")
	}
	msg, err := u.repo.GetByID(ctx, messageID)
	if err != nil {
		return domain.ErrMessageNotFound
	}
	r := &domain.ChatReaction{
		MessageID: messageID,
		Reactor:   reactor,
		AdminID:   adminID,
		Emoji:     emoji,
	}
	if err := u.repo.AddReaction(ctx, r); err != nil {
		return err
	}
	u.eventBus.Publish(events.Event{
		Type: events.EventChatReactionAdded,
		Payload: events.ChatReactionPayload{
			SubscriptionID: msg.SubscriptionID,
			MessageID:      messageID,
			Reactor:        reactor,
			Emoji:          emoji,
		},
	})
	return nil
}

func (u *chatUsecase) RemoveReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, emoji string) error {
	msg, err := u.repo.GetByID(ctx, messageID)
	if err != nil {
		return domain.ErrMessageNotFound
	}
	n, err := u.repo.RemoveReaction(ctx, messageID, reactor, adminID, emoji)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	u.eventBus.Publish(events.Event{
		Type: events.EventChatReactionRemoved,
		Payload: events.ChatReactionPayload{
			SubscriptionID: msg.SubscriptionID,
			MessageID:      messageID,
			Reactor:        reactor,
			Emoji:          emoji,
		},
	})
	return nil
}

func (u *chatUsecase) ListReactions(ctx context.Context, messageID uint) ([]domain.ChatReaction, error) {
	return u.repo.ListReactionsByMessage(ctx, messageID)
}

func (u *chatUsecase) ListReactionsBatch(ctx context.Context, messageIDs []uint) ([]domain.ChatReaction, error) {
	return u.repo.ListReactionsByMessages(ctx, messageIDs)
}

func (u *chatUsecase) ReplaceReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, oldEmoji, newEmoji string) error {
	if _, ok := allowedReactionEmojis[newEmoji]; !ok {
		return errors.New("emoji not allowed")
	}
	if reactor != "user" && reactor != "admin" {
		return errors.New("invalid reactor")
	}
	if oldEmoji == newEmoji {
		return nil
	}
	msg, err := u.repo.GetByID(ctx, messageID)
	if err != nil {
		return domain.ErrMessageNotFound
	}
	n, err := u.repo.ReplaceReaction(ctx, messageID, reactor, adminID, oldEmoji, newEmoji)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("existing reaction not found")
	}
	u.eventBus.Publish(events.Event{
		Type: events.EventChatReactionRemoved,
		Payload: events.ChatReactionPayload{
			SubscriptionID: msg.SubscriptionID,
			MessageID:      messageID,
			Reactor:        reactor,
			Emoji:          oldEmoji,
		},
	})
	u.eventBus.Publish(events.Event{
		Type: events.EventChatReactionAdded,
		Payload: events.ChatReactionPayload{
			SubscriptionID: msg.SubscriptionID,
			MessageID:      messageID,
			Reactor:        reactor,
			Emoji:          newEmoji,
		},
	})
	return nil
}
