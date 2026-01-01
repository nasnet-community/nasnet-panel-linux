package http

import (
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
)

// TestTelegramReachable mirrors the recipient resolution used by the
// notification scheduler: a subscription is reachable on Telegram via an
// explicit per-sub chat ID OR by falling back to the owner's Telegram account.
func TestTelegramReachable(t *testing.T) {
	tests := []struct {
		name string
		sub  *domain.Subscription
		want bool
	}{
		{
			name: "explicit per-sub chat id set",
			sub:  &domain.Subscription{TelegramChatID: 12345},
			want: true,
		},
		{
			// The reported bug: owner-linked sub (e.g. bought via the bot)
			// with no explicit chat ID. Notifications reach the owner, so the
			// panel must report connected.
			name: "no chat id but owner has telegram account",
			sub:  &domain.Subscription{TelegramChatID: 0, User: &userDomain.User{TelegramID: 67890}},
			want: true,
		},
		{
			// Admin-created users without Telegram get a negative placeholder
			// telegram_id (admin_users.go). That is not a real account.
			name: "no chat id, owner has negative placeholder telegram id",
			sub:  &domain.Subscription{TelegramChatID: 0, User: &userDomain.User{TelegramID: -1718000000000000000}},
			want: false,
		},
		{
			name: "no chat id, no owner",
			sub:  &domain.Subscription{TelegramChatID: 0, User: nil},
			want: false,
		},
		{
			name: "no chat id, owner telegram id zero",
			sub:  &domain.Subscription{TelegramChatID: 0, User: &userDomain.User{TelegramID: 0}},
			want: false,
		},
		{
			name: "nil subscription",
			sub:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := telegramReachable(tt.sub); got != tt.want {
				t.Errorf("telegramReachable() = %v, want %v", got, tt.want)
			}
		})
	}
}
