package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// WebhookChannel sends notifications to a generic webhook endpoint.
type WebhookChannel struct {
	settings    SettingProvider
	httpFactory *httpclient.Factory
}

// NewWebhookChannel creates a new generic webhook channel. f may be nil.
func NewWebhookChannel(settings SettingProvider, f *httpclient.Factory) *WebhookChannel {
	return &WebhookChannel{
		settings:    settings,
		httpFactory: f,
	}
}

func (c *WebhookChannel) httpClient() *http.Client {
	if c.httpFactory == nil {
		return &http.Client{Timeout: 10 * time.Second}
	}
	return c.httpFactory.ClientFor(httpclient.FeatureWebhooks, 10*time.Second)
}

func (c *WebhookChannel) Name() string { return "webhook" }

func (c *WebhookChannel) Send(ctx context.Context, msg *NotificationMessage) error {
	url, err := c.settings.GetByKey(ctx, "notification_webhook_url")
	if err != nil || url == "" {
		return nil // not configured, skip silently
	}

	payload := map[string]interface{}{
		"event":     msg.EventType,
		"title":     msg.Title,
		"body":      msg.PlainBody,
		"level":     msg.Level,
		"fields":    msg.Fields,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add HMAC-SHA256 signature if secret is configured
	secret, _ := c.settings.GetByKey(ctx, "notification_webhook_secret")
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Signature-256", "sha256="+signature)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[WebhookChannel] Failed to send webhook")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: endpoint returned status %d", resp.StatusCode)
	}

	return nil
}
