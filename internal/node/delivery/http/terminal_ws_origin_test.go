package http

import (
	"net/http"
	"testing"
)

func TestCheckOrigin_SameOrigin(t *testing.T) {
	r := &http.Request{
		Host:   "panel.example.com",
		Header: http.Header{"Origin": []string{"https://panel.example.com"}},
	}
	if !checkOrigin(r) {
		t.Error("same-origin should be allowed")
	}
}

func TestCheckOrigin_EmptyOriginAllowed(t *testing.T) {
	r := &http.Request{Host: "panel.example.com", Header: http.Header{}}
	if !checkOrigin(r) {
		t.Error("empty origin (non-browser client) should be allowed — auth already happened")
	}
}

func TestCheckOrigin_CrossOriginRejected(t *testing.T) {
	r := &http.Request{
		Host:   "panel.example.com",
		Header: http.Header{"Origin": []string{"https://evil.com"}},
	}
	if checkOrigin(r) {
		t.Error("cross-origin must be rejected without an allow-list entry")
	}
}

func TestCheckOrigin_MalformedOriginRejected(t *testing.T) {
	r := &http.Request{
		Host:   "panel.example.com",
		Header: http.Header{"Origin": []string{"::::"}},
	}
	if checkOrigin(r) {
		t.Error("malformed origin must be rejected")
	}
}

func TestCheckOrigin_AllowlistExtra(t *testing.T) {
	t.Setenv("NASNET_WS_ALLOWED_ORIGINS", "https://admin.example.com,https://other.example.com")
	// extraAllowedWSOrigins is initialised at package load; reload for this test.
	extraAllowedWSOrigins = loadExtraAllowedWSOrigins()
	t.Cleanup(func() {
		t.Setenv("NASNET_WS_ALLOWED_ORIGINS", "")
		extraAllowedWSOrigins = loadExtraAllowedWSOrigins()
	})

	r := &http.Request{
		Host:   "panel.internal",
		Header: http.Header{"Origin": []string{"https://admin.example.com"}},
	}
	if !checkOrigin(r) {
		t.Error("allow-listed origin should pass")
	}

	r2 := &http.Request{
		Host:   "panel.internal",
		Header: http.Header{"Origin": []string{"https://not-listed.example.com"}},
	}
	if checkOrigin(r2) {
		t.Error("origin not in allow-list must still be rejected")
	}
}

func TestCheckOrigin_CaseInsensitiveHost(t *testing.T) {
	r := &http.Request{
		Host:   "Panel.Example.com",
		Header: http.Header{"Origin": []string{"https://panel.example.com"}},
	}
	if !checkOrigin(r) {
		t.Error("host comparison must be case-insensitive")
	}
}
