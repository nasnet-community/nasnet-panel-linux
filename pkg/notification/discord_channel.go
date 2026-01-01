package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// DiscordChannel sends notifications to a Discord webhook.
type DiscordChannel struct {
	settings    SettingProvider
	httpFactory *httpclient.Factory
}

// NewDiscordChannel creates a new Discord webhook channel. f may be nil
// (channel will use a direct-internet client).
func NewDiscordChannel(settings SettingProvider, f *httpclient.Factory) *DiscordChannel {
	return &DiscordChannel{
		settings:    settings,
		httpFactory: f,
	}
}

// httpClient returns a freshly-derived client each call so live proxy reloads
// take effect without reconstructing the channel.
func (c *DiscordChannel) httpClient() *http.Client {
	if c.httpFactory == nil {
		return &http.Client{Timeout: 10 * time.Second}
	}
	return c.httpFactory.ClientFor(httpclient.FeatureWebhooks, 10*time.Second)
}

func (c *DiscordChannel) Name() string { return "discord" }

func (c *DiscordChannel) Send(ctx context.Context, msg *NotificationMessage) error {
	url, err := c.settings.GetByKey(ctx, "notification_discord_webhook_url")
	if err != nil || url == "" {
		return nil // not configured, skip silently
	}

	color := levelToDiscordColor(msg.Level)

	var fields []map[string]interface{}
	for k, v := range msg.Fields {
		fields = append(fields, map[string]interface{}{
			"name":   k,
			"value":  v,
			"inline": true,
		})
	}

	embed := map[string]interface{}{
		"title":       msg.Title,
		"description": msg.PlainBody,
		"color":       color,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{embed},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord: marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[DiscordChannel] Failed to send webhook")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord: webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// levelToDiscordColor maps notification levels to Discord embed colors.
func levelToDiscordColor(level string) int {
	switch level {
	case "success":
		return 0x2ECC71 // green
	case "error":
		return 0xE74C3C // red
	case "warning":
		return 0xF39C12 // yellow/orange
	default: // "info"
		return 0x3498DB // blue
	}
}
