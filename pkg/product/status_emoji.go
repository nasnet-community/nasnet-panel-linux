package product

// StatusEmoji returns an emoji representing the subscription status.
func StatusEmoji(status string) string {
	switch status {
	case "active":
		return "🟢"
	case "paused":
		return "⏸️"
	case "expired":
		return "🔴"
	case "traffic_exhausted":
		return "⚠️"
	case "pending":
		return "⏳"
	case "cancelled":
		return "❌"
	default:
		return "⚪"
	}
}
