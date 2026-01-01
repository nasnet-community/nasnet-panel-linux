package notification

// Notifier defines the interface for sending admin notifications
type Notifier interface {
	// NotifyAdmin sends a message to the admin(s)
	NotifyAdmin(message string) error
}
