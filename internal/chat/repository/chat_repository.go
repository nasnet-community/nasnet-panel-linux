package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

type chatRepository struct {
	db *gorm.DB
}

func NewChatRepository(db *gorm.DB) domain.ChatRepository {
	return &chatRepository{db: db}
}

func (r *chatRepository) Create(ctx context.Context, msg *domain.ChatMessage) error {
	return r.db.WithContext(ctx).Create(msg).Error
}

func (r *chatRepository) ListBySubscription(ctx context.Context, subID uint, page, limit int) ([]domain.ChatMessage, int64, error) {
	var messages []domain.ChatMessage
	var total int64

	q := r.db.WithContext(ctx).Where("subscription_id = ?", subID)
	if err := q.Model(&domain.ChatMessage{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&messages).Error; err != nil {
		return nil, 0, err
	}
	return messages, total, nil
}

func (r *chatRepository) MarkAsRead(ctx context.Context, subID uint) error {
	return r.MarkAsReadBySender(ctx, subID, "user")
}

// MarkAsReadBySender flips is_read=true for messages produced by senderType in
// the given subscription. The opposite side reads them — so admin reads "user",
// user reads "admin".
func (r *chatRepository) MarkAsReadBySender(ctx context.Context, subID uint, senderType string) error {
	return r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("subscription_id = ? AND sender_type = ? AND is_read = ?", subID, senderType, false).
		Update("is_read", true).Error
}

func (r *chatRepository) GetTotalUnreadCount(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("sender_type = ? AND is_read = ?", "user", false).
		Count(&count).Error
	return count, err
}

func (r *chatRepository) GetConversationList(ctx context.Context, filters domain.ConversationFilters) ([]domain.ConversationSummary, int64, error) {
	latestMsg := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Select("subscription_id, MAX(created_at) as max_created_at").
		Group("subscription_id")

	q := r.db.WithContext(ctx).
		Table("chat_messages AS cm").
		Select(`
			cm.subscription_id,
			s.label AS subscription_label,
			s.status AS subscription_status,
			s.user_id,
			u.username,
			u.first_name,
			cm.content AS last_message_content,
			cm.created_at AS last_message_at,
			cm.sender_type AS last_sender_type,
			COALESCE(unread.cnt, 0) AS unread_count
		`).
		Where("cm.deleted_at IS NULL").
		Joins("JOIN (?) AS latest ON cm.subscription_id = latest.subscription_id AND cm.created_at = latest.max_created_at", latestMsg).
		Joins("JOIN subscriptions AS s ON s.id = cm.subscription_id").
		Joins("LEFT JOIN users AS u ON u.id = s.user_id").
		Joins(`LEFT JOIN (
			SELECT subscription_id, COUNT(*) AS cnt
			FROM chat_messages
			WHERE sender_type = 'user' AND is_read = false AND deleted_at IS NULL
			GROUP BY subscription_id
		) AS unread ON unread.subscription_id = cm.subscription_id`)

	if filters.Search != "" {
		searchPattern := "%" + filters.Search + "%"
		clause := fmt.Sprintf("%s OR %s OR %s",
			database.ILike("s.label", "?"),
			database.ILike("u.username", "?"),
			database.ILike("u.first_name", "?"),
		)
		q = q.Where(clause, searchPattern, searchPattern, searchPattern)
	}

	if filters.Status != "" {
		q = q.Where("s.status = ?", filters.Status)
	}

	if filters.UnreadOnly {
		q = q.Where("COALESCE(unread.cnt, 0) > 0")
	}

	if filters.MineAdminID != nil {
		q = q.Where(`EXISTS (
			SELECT 1 FROM chat_messages mm
			WHERE mm.subscription_id = cm.subscription_id
			  AND mm.sender_type = 'admin'
			  AND mm.admin_id = ?
			  AND mm.deleted_at IS NULL
		)`, *filters.MineAdminID)
	}
	if filters.PinnedOnly {
		q = q.Where(`EXISTS (
			SELECT 1 FROM chat_messages mp
			WHERE mp.subscription_id = cm.subscription_id
			  AND mp.is_pinned = true
			  AND mp.deleted_at IS NULL
		)`)
	}

	var total int64
	countQ := r.db.WithContext(ctx).
		Table("(?) AS conversations", q).
		Count(&total)
	if countQ.Error != nil {
		return nil, 0, countQ.Error
	}

	// Determine sort order
	orderClause := "cm.created_at DESC"
	switch filters.SortBy {
	case "unread":
		orderClause = "unread_count DESC, cm.created_at DESC"
	case "oldest":
		orderClause = "cm.created_at ASC"
	default:
		// "recent" or empty — default sort
		orderClause = "cm.created_at DESC"
	}

	offset := (filters.Page - 1) * filters.Limit
	var summaries []domain.ConversationSummary
	if err := q.Order(orderClause).Offset(offset).Limit(filters.Limit).Scan(&summaries).Error; err != nil {
		return nil, 0, err
	}

	for i := range summaries {
		runes := []rune(summaries[i].LastMessageContent)
		if len(runes) > 100 {
			summaries[i].LastMessageContent = string(runes[:100]) + "..."
		}
	}

	return summaries, total, nil
}

func (r *chatRepository) SetPinnedScoped(ctx context.Context, messageID, subID uint, pinned bool) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("id = ? AND subscription_id = ?", messageID, subID).
		Update("is_pinned", pinned)
	return result.RowsAffected, result.Error
}

func (r *chatRepository) GetPinnedMessages(ctx context.Context, subID uint) ([]domain.ChatMessage, error) {
	var messages []domain.ChatMessage
	err := r.db.WithContext(ctx).
		Where("subscription_id = ? AND is_pinned = ?", subID, true).
		Order("created_at DESC").
		Find(&messages).Error
	return messages, err
}

func (r *chatRepository) DeleteOlderThan(ctx context.Context, days int) (int64, error) {
	threshold := time.Now().AddDate(0, 0, -days)
	res := r.db.WithContext(ctx).Unscoped().
		Where("created_at < ?", threshold).
		Delete(&domain.ChatMessage{})
	return res.RowsAffected, res.Error
}

func (r *chatRepository) SearchInSubscription(ctx context.Context, subID uint, q string, page, limit int) ([]domain.ChatMessage, int64, error) {
	pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), `%`, `\%`) + "%"
	base := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("subscription_id = ?", subID).
		Where(database.ILike("content", "?"), pattern)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	var rows []domain.ChatMessage
	if err := base.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *chatRepository) EditMessageScoped(ctx context.Context, messageID, subID uint, content string) (int64, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("id = ? AND subscription_id = ?", messageID, subID).
		Updates(map[string]interface{}{"content": content, "edited_at": &now})
	return res.RowsAffected, res.Error
}

func (r *chatRepository) DeleteMessageScoped(ctx context.Context, messageID, subID uint) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("id = ? AND subscription_id = ?", messageID, subID).
		Delete(&domain.ChatMessage{})
	return res.RowsAffected, res.Error
}

func (r *chatRepository) GetByID(ctx context.Context, messageID uint) (*domain.ChatMessage, error) {
	var m domain.ChatMessage
	if err := r.db.WithContext(ctx).First(&m, messageID).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
