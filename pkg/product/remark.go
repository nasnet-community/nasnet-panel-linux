package product

import (
	"strconv"
	"strings"
)

// DefaultRemarkTemplate is the remark template used for hosts that don't have a custom one.
const DefaultRemarkTemplate = "{flag} {node} | {data_left}"

// RemarkContext holds the variables available for remark template rendering.
type RemarkContext struct {
	Flag         string // Flag emoji (e.g., "🇩🇪")
	Country      string // Country name or code (e.g., "DE")
	CountryCode  string // ISO 2-letter code
	Node         string // Node name
	Port         int
	Protocol     string // vmess, vless, trojan
	Network      string // tcp, ws, grpc, xhttp
	Security     string // tls, reality, none
	DataUsed     string // e.g., "5.2 GB"
	DataLeft     string // e.g., "5.2 GB Left" or "♾️"
	DaysLeft     string // e.g., "12" or "∞"
	TimeLeft     string // e.g., "12d" or "∞" (days with unit, or ∞ for unlimited)
	DataLimit    string // e.g., "50 GB"
	UsagePercent string // e.g., "45%"
	StatusEmoji  string // e.g., "🟢" for active, "🔴" for expired
}

// RenderRemark substitutes {variable} placeholders in a remark template.
func RenderRemark(template string, ctx RemarkContext) string {
	r := strings.NewReplacer(
		"{flag}", ctx.Flag,
		"{country}", ctx.Country,
		"{country_code}", ctx.CountryCode,
		"{node}", ctx.Node,
		"{port}", strconv.Itoa(ctx.Port),
		"{protocol}", ctx.Protocol,
		"{network}", ctx.Network,
		"{security}", ctx.Security,
		"{data_used}", ctx.DataUsed,
		"{data_left}", ctx.DataLeft,
		"{days_left}", ctx.DaysLeft,
		"{time_left}", ctx.TimeLeft,
		"{data_limit}", ctx.DataLimit,
		"{usage_percent}", ctx.UsagePercent,
		"{status_emoji}", ctx.StatusEmoji,
	)
	return r.Replace(template)
}
