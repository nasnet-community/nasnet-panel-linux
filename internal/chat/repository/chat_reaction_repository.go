package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/chat/domain"
	"gorm.io/gorm"
)

func (r *chatRepository) AddReaction(ctx context.Context, rc *domain.ChatReaction) error {
	return r.db.WithContext(ctx).Create(rc).Error
}

func (r *chatRepository) RemoveReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, emoji string) (int64, error) {
	q := r.db.WithContext(ctx).
		Where("message_id = ? AND reactor = ? AND emoji = ?", messageID, reactor, emoji)
	if adminID != nil {
		q = q.Where("admin_id = ?", *adminID)
	} else {
		q = q.Where("admin_id IS NULL")
	}
	res := q.Delete(&domain.ChatReaction{})
	return res.RowsAffected, res.Error
}

func (r *chatRepository) ListReactionsByMessage(ctx context.Context, messageID uint) ([]domain.ChatReaction, error) {
	var rows []domain.ChatReaction
	err := r.db.WithContext(ctx).
		Where("message_id = ?", messageID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *chatRepository) ListReactionsByMessages(ctx context.Context, messageIDs []uint) ([]domain.ChatReaction, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var rows []domain.ChatReaction
	err := r.db.WithContext(ctx).
		Where("message_id IN ?", messageIDs).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// ReplaceReaction atomically swaps an actor's existing emoji on a message.
// Returns RowsAffected from the delete (0 means the original reaction was
// not found and nothing was inserted).
func (r *chatRepository) ReplaceReaction(ctx context.Context, messageID uint, reactor string, adminID *uint, oldEmoji, newEmoji string) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("message_id = ? AND reactor = ? AND emoji = ?", messageID, reactor, oldEmoji)
		if adminID != nil {
			q = q.Where("admin_id = ?", *adminID)
		} else {
			q = q.Where("admin_id IS NULL")
		}
		res := q.Delete(&domain.ChatReaction{})
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		if affected == 0 {
			return nil
		}
		rc := &domain.ChatReaction{
			MessageID: messageID,
			Reactor:   reactor,
			AdminID:   adminID,
			Emoji:     newEmoji,
		}
		return tx.Create(rc).Error
	})
	return affected, err
}
