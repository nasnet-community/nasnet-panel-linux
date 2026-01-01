package notification

import (
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

// templateFunc builds a NotificationMessage from an event.
type templateFunc func(e events.Event) *NotificationMessage

// templateRegistry maps event types to their template functions.
var templateRegistry = map[events.EventType]templateFunc{
	// Node status
	events.EventNodeOnline:  templateNodeOnline,
	events.EventNodeOffline: templateNodeOffline,

	// Node lifecycle
	events.EventNodeCreated: templateNodeCreated,
	events.EventNodeDeleted: templateNodeDeleted,

	// Subscription
	events.EventSubscriptionCreated:        templateSubscriptionCreated,
	events.EventSubscriptionRenewed:        templateSubscriptionRenewed,
	events.EventSubscriptionCancelled:      templateSubscriptionCancelled,
	events.EventSubscriptionExpired:        templateSubscriptionExpired,
	events.EventSubscriptionTrialActivated: templateSubscriptionTrialActivated,

	// User
	events.EventUserRegistered: templateUserRegistered,

	// System
	events.EventSystemAlert: templateSystemAlert,

	// Xray process
	events.EventXrayDown:      templateXrayDown,
	events.EventXrayUp:        templateXrayUp,
	events.EventXrayCrashLoop: templateXrayCrashLoop,

	// Crash recovery command
	events.EventXrayRecoveryCommand:   templateXrayRecoveryCommand,
	events.EventXrayRecoveryExhausted: templateXrayRecoveryExhausted,

	// Chat
	events.EventChatNewMessage: templateChatNewMessage,
}

// getTemplate returns the template function for the given event type, or nil.
func getTemplate(eventType events.EventType) templateFunc {
	return templateRegistry[eventType]
}

// --- Node status templates ---

func templateNodeOnline(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.NodeStatusPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server": p.NodeName,
		"IP":     p.IP,
		"Status": "ONLINE",
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Server Restored",
		Body:      fmt.Sprintf("✅ *Server Restored*\n\nServer: `%s`\nIP: `%s`\nStatus: *ONLINE* 🟢", p.NodeName, p.IP),
		PlainBody: fmt.Sprintf("Server Restored: %s (%s) is back ONLINE", p.NodeName, p.IP),
		Fields:    fields,
		Level:     "success",
	}
}

func templateNodeOffline(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.NodeStatusPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server": p.NodeName,
		"IP":     p.IP,
		"Status": "OFFLINE",
	}
	body := fmt.Sprintf("⚠️ *Server Offline*\n\nServer: `%s`\nIP: `%s`\nStatus: *OFFLINE* 🔴", p.NodeName, p.IP)
	plain := fmt.Sprintf("Server Offline: %s (%s) is OFFLINE", p.NodeName, p.IP)
	if p.Message != "" {
		body += fmt.Sprintf("\nError: `%s`", p.Message)
		plain += fmt.Sprintf(" - Error: %s", p.Message)
		fields["Error"] = p.Message
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Server Offline",
		Body:      body,
		PlainBody: plain,
		Fields:    fields,
		Level:     "error",
	}
}

// --- Node lifecycle templates ---

func templateNodeCreated(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.NodeLifecyclePayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server": p.NodeName,
		"IP":     p.IP,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Server Created",
		Body:      fmt.Sprintf("🆕 *Server Created*\n\nServer: `%s`\nIP: `%s`", p.NodeName, p.IP),
		PlainBody: fmt.Sprintf("Server Created: %s (%s)", p.NodeName, p.IP),
		Fields:    fields,
		Level:     "info",
	}
}

func templateNodeDeleted(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.NodeLifecyclePayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server": p.NodeName,
		"IP":     p.IP,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Server Deleted",
		Body:      fmt.Sprintf("🗑 *Server Deleted*\n\nServer: `%s`\nIP: `%s`", p.NodeName, p.IP),
		PlainBody: fmt.Sprintf("Server Deleted: %s (%s)", p.NodeName, p.IP),
		Fields:    fields,
		Level:     "warning",
	}
}

// --- Subscription templates ---

func templateSubscriptionCreated(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.SubscriptionEventPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Subscription": fmt.Sprintf("#%d", p.SubscriptionID),
		"User":         usernameOrID(p.Username, p.UserID),
		"Plan":         p.PlanName,
	}
	if p.ExpiresAt != "" {
		fields["Expires"] = p.ExpiresAt
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "New Subscription",
		Body:      fmt.Sprintf("📦 *New Subscription*\n\nSubscription: `#%d`\nUser: `%s`\nPlan: `%s`", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		PlainBody: fmt.Sprintf("New Subscription #%d by %s for plan %s", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		Fields:    fields,
		Level:     "info",
	}
}

func templateSubscriptionRenewed(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.SubscriptionEventPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Subscription": fmt.Sprintf("#%d", p.SubscriptionID),
		"User":         usernameOrID(p.Username, p.UserID),
		"Plan":         p.PlanName,
	}
	if p.ExpiresAt != "" {
		fields["New Expiry"] = p.ExpiresAt
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Subscription Renewed",
		Body:      fmt.Sprintf("🔄 *Subscription Renewed*\n\nSubscription: `#%d`\nUser: `%s`\nPlan: `%s`", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		PlainBody: fmt.Sprintf("Subscription #%d renewed by %s for plan %s", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		Fields:    fields,
		Level:     "success",
	}
}

func templateSubscriptionCancelled(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.SubscriptionEventPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Subscription": fmt.Sprintf("#%d", p.SubscriptionID),
		"User":         usernameOrID(p.Username, p.UserID),
		"Plan":         p.PlanName,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Subscription Cancelled",
		Body:      fmt.Sprintf("🚫 *Subscription Cancelled*\n\nSubscription: `#%d`\nUser: `%s`\nPlan: `%s`", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		PlainBody: fmt.Sprintf("Subscription #%d cancelled by %s (plan: %s)", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		Fields:    fields,
		Level:     "warning",
	}
}

func templateSubscriptionExpired(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.SubscriptionEventPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Subscription": fmt.Sprintf("#%d", p.SubscriptionID),
		"User":         usernameOrID(p.Username, p.UserID),
		"Plan":         p.PlanName,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Subscription Expired",
		Body:      fmt.Sprintf("⏰ *Subscription Expired*\n\nSubscription: `#%d`\nUser: `%s`\nPlan: `%s`", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		PlainBody: fmt.Sprintf("Subscription #%d expired for %s (plan: %s)", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		Fields:    fields,
		Level:     "warning",
	}
}

func templateSubscriptionTrialActivated(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.SubscriptionEventPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Subscription": fmt.Sprintf("#%d", p.SubscriptionID),
		"User":         usernameOrID(p.Username, p.UserID),
		"Plan":         p.PlanName,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Trial Activated",
		Body:      fmt.Sprintf("🎁 *Trial Activated*\n\nSubscription: `#%d`\nUser: `%s`\nPlan: `%s`", p.SubscriptionID, usernameOrID(p.Username, p.UserID), p.PlanName),
		PlainBody: fmt.Sprintf("Trial activated for %s (Subscription #%d, plan: %s)", usernameOrID(p.Username, p.UserID), p.SubscriptionID, p.PlanName),
		Fields:    fields,
		Level:     "info",
	}
}

// --- User templates ---

func templateUserRegistered(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.UserRegisteredPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"User ID":     fmt.Sprintf("%d", p.UserID),
		"Telegram ID": fmt.Sprintf("%d", p.TelegramID),
		"Username":    p.Username,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "New User",
		Body:      fmt.Sprintf("👤 *New User Registered*\n\nUser: `%s`\nTelegram ID: `%d`", p.Username, p.TelegramID),
		PlainBody: fmt.Sprintf("New user registered: %s (Telegram ID: %d)", p.Username, p.TelegramID),
		Fields:    fields,
		Level:     "info",
	}
}

// --- System templates ---

func templateSystemAlert(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.SystemAlertPayload)
	if !ok {
		return nil
	}
	level := p.Level
	if level == "" {
		level = "warning"
	}
	fields := map[string]string{
		"Title":   p.Title,
		"Message": p.Message,
		"Level":   level,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "System Alert",
		Body:      fmt.Sprintf("🔔 *System Alert*\n\n*%s*\n%s", p.Title, p.Message),
		PlainBody: fmt.Sprintf("System Alert: %s - %s", p.Title, p.Message),
		Fields:    fields,
		Level:     level,
	}
}

// --- Xray process templates ---

func templateXrayDown(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.XrayStatusPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server": p.NodeName,
		"IP":     p.IP,
		"Crash":  fmt.Sprintf("#%d", p.CrashCount),
	}
	body := fmt.Sprintf("🔴 *Xray Process Down*\n\nServer: `%s`\nIP: `%s`\nCrash #%d", p.NodeName, p.IP, p.CrashCount)
	plain := fmt.Sprintf("Xray Down: %s (%s) - crash #%d", p.NodeName, p.IP, p.CrashCount)
	if p.ErrorLog != "" {
		body += fmt.Sprintf("\nLast Error:\n```\n%s\n```", p.ErrorLog)
		plain += fmt.Sprintf(" - Error: %s", p.ErrorLog)
		fields["Error"] = p.ErrorLog
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Xray Process Down",
		Body:      body,
		PlainBody: plain,
		Fields:    fields,
		Level:     "error",
	}
}

func templateXrayUp(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.XrayStatusPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server":  p.NodeName,
		"IP":      p.IP,
		"Crashes": fmt.Sprintf("%d", p.CrashCount),
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Xray Process Recovered",
		Body:      fmt.Sprintf("🟢 *Xray Process Recovered*\n\nServer: `%s`\nIP: `%s`\nCrashes before recovery: %d", p.NodeName, p.IP, p.CrashCount),
		PlainBody: fmt.Sprintf("Xray Recovered: %s (%s) - %d crashes before recovery", p.NodeName, p.IP, p.CrashCount),
		Fields:    fields,
		Level:     "success",
	}
}

func templateXrayCrashLoop(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.XrayStatusPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server":  p.NodeName,
		"IP":      p.IP,
		"Crashes": fmt.Sprintf("%d", p.CrashCount),
	}
	body := fmt.Sprintf("⚠️ *Xray Crash Loop Detected*\n\nServer: `%s`\nIP: `%s`\nCrashes: %d", p.NodeName, p.IP, p.CrashCount)
	plain := fmt.Sprintf("Xray Crash Loop: %s (%s) - %d crashes", p.NodeName, p.IP, p.CrashCount)
	if p.ErrorLog != "" {
		body += fmt.Sprintf("\nLast Error:\n```\n%s\n```", p.ErrorLog)
		plain += fmt.Sprintf(" - Error: %s", p.ErrorLog)
		fields["Error"] = p.ErrorLog
	}
	if p.Message != "" {
		body += fmt.Sprintf("\n_%s_", p.Message)
		plain += fmt.Sprintf(" (%s)", p.Message)
		fields["Note"] = p.Message
	} else {
		body += "\n_Notifications paused — will re-alert if still looping after cooldown._"
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Xray Crash Loop Detected",
		Body:      body,
		PlainBody: plain,
		Fields:    fields,
		Level:     "error",
	}
}

func templateXrayRecoveryCommand(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.XrayRecoveryPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server":  p.NodeName,
		"IP":      p.IP,
		"Command": p.Command,
	}
	maxStr := fmt.Sprintf("%d", p.MaxAttempts)
	if p.MaxAttempts == 0 {
		maxStr = "∞"
	}
	body := fmt.Sprintf("🔧 *Xray Recovery Command*\n\nServer: `%s`\nIP: `%s`\nAttempt: %d/%s\nCommand: `%s`", p.NodeName, p.IP, p.AttemptNum, maxStr, p.Command)
	plain := fmt.Sprintf("Xray Recovery Command: %s (%s) - attempt %d/%s - command: %s", p.NodeName, p.IP, p.AttemptNum, maxStr, p.Command)

	if p.ErrorMessage != "" {
		body += fmt.Sprintf("\nError: `%s`", p.ErrorMessage)
		plain += fmt.Sprintf(" - Error: %s", p.ErrorMessage)
		fields["Error"] = p.ErrorMessage
	} else {
		body += fmt.Sprintf("\nExit Code: %d", p.ExitCode)
		fields["Exit Code"] = fmt.Sprintf("%d", p.ExitCode)
		if p.Stdout != "" {
			body += fmt.Sprintf("\nStdout:\n```\n%s\n```", p.Stdout)
			fields["Stdout"] = p.Stdout
		}
		if p.Stderr != "" {
			body += fmt.Sprintf("\nStderr:\n```\n%s\n```", p.Stderr)
			fields["Stderr"] = p.Stderr
		}
	}
	body += "\n_Starting xray after recovery command..._"

	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Xray Recovery Command",
		Body:      body,
		PlainBody: plain,
		Fields:    fields,
		Level:     "warning",
	}
}

func templateXrayRecoveryExhausted(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.XrayRecoveryPayload)
	if !ok {
		return nil
	}
	fields := map[string]string{
		"Server":   p.NodeName,
		"IP":       p.IP,
		"Attempts": fmt.Sprintf("%d/%d", p.AttemptNum, p.MaxAttempts),
	}
	body := fmt.Sprintf("🚫 *Xray Recovery Exhausted*\n\nServer: `%s`\nIP: `%s`\nRecovery command ran %d/%d times without success.\nManual intervention required.", p.NodeName, p.IP, p.AttemptNum, p.MaxAttempts)
	plain := fmt.Sprintf("Xray Recovery Exhausted: %s (%s) - %d/%d attempts used. Manual intervention required.", p.NodeName, p.IP, p.AttemptNum, p.MaxAttempts)
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "Xray Recovery Exhausted",
		Body:      body,
		PlainBody: plain,
		Fields:    fields,
		Level:     "error",
	}
}

// --- Chat templates ---

func templateChatNewMessage(e events.Event) *NotificationMessage {
	p, ok := e.Payload.(events.ChatMessagePayload)
	if !ok {
		return nil
	}
	label := p.SubscriptionLabel
	if label == "" {
		label = fmt.Sprintf("Sub #%d", p.SubscriptionID)
	}
	preview := p.ContentPreview
	if preview == "" {
		preview = "(empty)"
	}
	fields := map[string]string{
		"Subscription": label,
		"Message":      preview,
	}
	return &NotificationMessage{
		EventType: string(e.Type),
		Title:     "New Chat Message",
		Body:      fmt.Sprintf("💬 *New Chat Message*\n\nFrom: `%s`\nMessage: %s", label, preview),
		PlainBody: fmt.Sprintf("New chat message from %s: %s", label, preview),
		Fields:    fields,
		Level:     "info",
	}
}

// --- Helpers ---

func usernameOrID(username string, userID uint) string {
	if username != "" {
		return username
	}
	return fmt.Sprintf("user#%d", userID)
}

// eventTypeToSettingKey converts "node.offline" → "node_offline"
func eventTypeToSettingKey(eventType string) string {
	return strings.ReplaceAll(eventType, ".", "_")
}
