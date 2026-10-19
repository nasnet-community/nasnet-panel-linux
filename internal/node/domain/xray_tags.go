package domain

import "strings"

// IsGeneratedXrayTag includes defaults absent from the database, legacy
// bandwidth tags that may still appear in imported configurations, and the
// tags reverse tunnels generate.
func IsGeneratedXrayTag(tag string) bool {
	switch tag {
	case "api", "direct", "blocked", "IPv4":
		return true
	}
	return IsRouterXrayTag(tag) || strings.HasPrefix(tag, "direct-bw") || strings.HasPrefix(tag, "__panel_bw/") || IsReverseTag(tag)
}

// Router mode owns these handlers, including stable per-secondary WAN tags.
func IsRouterXrayTag(tag string) bool {
	return tag == "direct-domestic" || tag == "direct-foreign" || strings.HasPrefix(tag, "direct-foreign-")
}

// IsReverseTag reports whether tag is in the namespace reverse tunnels own.
func IsReverseTag(tag string) bool { return strings.HasPrefix(tag, ReverseTagPrefix) }
