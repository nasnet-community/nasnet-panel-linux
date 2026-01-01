package notification

import "context"

// NotificationMessage represents a formatted notification ready to be sent.
type NotificationMessage struct {
	EventType string
	Title     string            // Short: "Server Offline"
	Body      string            // Markdown-formatted
	PlainBody string            // Plain text (for webhooks)
	Fields    map[string]string // Key-value for Discord embeds
	Level     string            // "info", "warning", "error", "success"
}

// Channel is the interface that notification channels must implement.
type Channel interface {
	Name() string
	Send(ctx context.Context, msg *NotificationMessage) error
}

// SettingProvider avoids importing full settings domain.
type SettingProvider interface {
	GetByKey(ctx context.Context, key string) (string, error)
}
