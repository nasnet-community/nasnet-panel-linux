package http

import (
	"net/http"
	"net/url"
	"strings"
)

// proxyClientUAs contains User-Agent substrings for known proxy/VPN clients.
// If any of these appear in the UA, we treat the request as a proxy client.
var proxyClientUAs = []string{
	"v2rayng", "v2rayn", "clash", "sing-box", "shadowrocket",
	"streisand", "hiddify", "nekobox", "nekoray", "surfboard",
	"stash", "loon", "quantumult", "surge", "okhttp",
}

// browserUAs contains User-Agent substrings that indicate a web browser.
var browserUAs = []string{
	"mozilla", "chrome", "safari", "firefox", "edge", "opera",
}

// isBrowserRequest: text/html Accept + known browser UA, with known proxy
// UAs negative-matched first to avoid false positives. Default: proxy client.
func isBrowserRequest(r *http.Request) bool {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	accept := r.Header.Get("Accept")

	// Check for known proxy client User-Agents first (highest priority)
	for _, client := range proxyClientUAs {
		if strings.Contains(ua, client) {
			return false
		}
	}

	// Accept: text/html is the strongest browser signal
	if strings.Contains(accept, "text/html") {
		return true
	}

	// Check for known browser User-Agents
	for _, browser := range browserUAs {
		if strings.Contains(ua, browser) {
			return true
		}
	}

	// Default: proxy client (backward compatible)
	return false
}

// isSameOrigin returns true when the parsed panelURL has the same host(:port)
// as the incoming request, which would cause a redirect loop.
func isSameOrigin(r *http.Request, panelURL string) bool {
	u, err := url.Parse(panelURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
