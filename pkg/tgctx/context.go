package tgctx

import (
	"context"
	"time"

	"gopkg.in/telebot.v3"
)

const DefaultTimeout = 30 * time.Second

func FromTelebot(c telebot.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultTimeout)
}

func FromTelebotWithTimeout(c telebot.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
