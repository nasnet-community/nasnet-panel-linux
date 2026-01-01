package notification

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// TelegramChannel sends notifications via the existing Notifier interface.
type TelegramChannel struct {
	notifier Notifier
}

// NewTelegramChannel wraps the existing Notifier to implement Channel.
func NewTelegramChannel(notifier Notifier) *TelegramChannel {
	return &TelegramChannel{notifier: notifier}
}

func (c *TelegramChannel) Name() string { return "telegram" }

func (c *TelegramChannel) Send(ctx context.Context, msg *NotificationMessage) error {
	if c.notifier == nil {
		return nil
	}
	if err := c.notifier.NotifyAdmin(msg.Body); err != nil {
		logger.GetLogger().WithError(err).Warn("[TelegramChannel] Failed to send notification")
		return err
	}
	return nil
}
