package middleware

import (
	"context"

	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	"gopkg.in/telebot.v3"
)

// AdminMiddleware handles admin authorization for Telegram commands
type AdminMiddleware struct {
	userRepo        userRepo.UserRepository
	initialAdminIDs []int64
}

// NewAdminMiddleware creates a new AdminMiddleware
func NewAdminMiddleware(userRepo userRepo.UserRepository, initialAdminIDs []int64) *AdminMiddleware {
	return &AdminMiddleware{
		userRepo:        userRepo,
		initialAdminIDs: initialAdminIDs,
	}
}

func (m *AdminMiddleware) IsAdmin(telegramID int64) bool {
	// Check if user is in initial admin list (from env)
	for _, id := range m.initialAdminIDs {
		if id == telegramID {
			return true
		}
	}

	// Check if user is marked as admin in database
	ctx := context.Background()
	user, err := m.userRepo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return false
	}

	return user.IsAdmin
}

// RequireAdmin wraps a handler to require admin privileges
func (m *AdminMiddleware) RequireAdmin(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		if !m.IsAdmin(c.Sender().ID) {
			return c.Send("⛔ *Access Denied*\n\nYou don't have permission to use this command.", telebot.ModeMarkdown)
		}
		return next(c)
	}
}

// RequireAdminWithCallback wraps a callback handler to require admin privileges
func (m *AdminMiddleware) RequireAdminWithCallback(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		if !m.IsAdmin(c.Sender().ID) {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "Access denied. Admin only.",
				ShowAlert: true,
			})
		}
		return next(c)
	}
}
