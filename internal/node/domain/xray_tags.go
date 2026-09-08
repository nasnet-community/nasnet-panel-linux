package domain

import "strings"

// IsGeneratedXrayTag includes defaults absent from the database and legacy
// bandwidth tags that may still appear in imported configurations.
func IsGeneratedXrayTag(tag string) bool {
	switch tag {
	case "api", "direct", "blocked", "IPv4":
		return true
	}
	return IsRouterXrayTag(tag) || strings.HasPrefix(tag, "direct-bw") || strings.HasPrefix(tag, "__panel_bw/")
}

// Router mode owns these handlers, including stable per-secondary WAN tags.
func IsRouterXrayTag(tag string) bool {
	return tag == "direct-domestic" || tag == "direct-foreign" || strings.HasPrefix(tag, "direct-foreign-")
}
