// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"strings"
	"testing"
)

func prefixes(b string) []string {
	if b == "" {
		return []string{""}
	}
	return []string{"", b}
}

func TestEveryAPIPathNeedsTheHeader(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		r.populate()
		for _, prefix := range prefixes(b) {
			for _, p := range []string{"/api/config", "/api/nodes", "/api/subscriptions", "/api/nothing", "/api"} {
				for _, hdr := range []map[string]string{nil, {UIHeader: ""}, {UIHeader: "0"}, {UIHeader: "yes"}} {
					code, _, body := r.call("GET", prefix+p, hdr)
					got := decode[map[string]any](t, body)
					if code != http.StatusForbidden || got["code"] != "forbidden" || got["error"] == "" {
						t.Errorf("GET %s with %v = %d %s, want 403 forbidden", prefix+p, hdr, code, body)
					}
				}
			}
		}
	}
}

func TestTheHeaderOpensTheRoutes(t *testing.T) {
	r := newRig(t, base)
	for _, p := range []string{"/api/config", base + "/api/config", "/api/nodes", base + "/api/subscriptions"} {
		if code, _, body := r.call("GET", p, map[string]string{UIHeader: "1"}); code != http.StatusOK {
			t.Errorf("GET %s = %d %s", p, code, body)
		}
	}
	// unknown, but a path the page could have asked for: told so only once it is let in
	if code, _, _ := r.call("GET", "/api/nothing", map[string]string{UIHeader: "1"}); code != http.StatusNotFound {
		t.Errorf("an unknown API path with the header = %d, want 404", code)
	}
}

// The base path itself starts with /api, which must not make the page, the
// health check or any other path under it count as an API call.
func TestTheBasePathIsNotTakenForTheAPI(t *testing.T) {
	r := newRig(t, base)
	for _, c := range []struct {
		path string
		want int
	}{
		{base + "/health", 200}, {base + "/", 200}, {base, 200},
		{base + "/nothing", 404}, {base + "/nothing/at/all", 404}, {"/nothing", 404},
		// merely starting the same is not being under it: these are paths under /api at the root
		{base + "x", 403}, {base + "x/api/config", 403},
	} {
		if code, _, body := r.call("GET", c.path, nil); code != c.want {
			t.Errorf("GET %s without the header = %d, want %d: %s", c.path, code, c.want, body)
		}
	}
}

func TestAPreflightIsRefused(t *testing.T) {
	r := newRig(t, "")
	code, hdr, _ := r.call("OPTIONS", "/api/config", map[string]string{
		"Origin": "https://evil.example", "Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "x-remotenodes-ui",
	})
	if code != http.StatusForbidden || hdr.Get("Access-Control-Allow-Origin") != "" || hdr.Get("Access-Control-Allow-Headers") != "" {
		t.Fatalf("preflight = %d with CORS headers %v", code, hdr)
	}
}

// What changes something has to come from this site as well. No route
// changes anything yet, so an unknown path under /api stands in: it is
// refused before it is told "not found", or let through to be told that.
func TestWritesMustComeFromThisSite(t *testing.T) {
	r := newRig(t, "")
	host := strings.TrimPrefix(r.srv.URL, "http://")
	with := func(extra ...string) map[string]string {
		m := map[string]string{UIHeader: "1"}
		for i := 0; i < len(extra); i += 2 {
			m[extra[i]] = extra[i+1]
		}
		return m
	}
	cases := []struct {
		name    string
		hdr     map[string]string
		refused bool
	}{
		{"no origin", with(), false},
		{"origin is where it was sent", with("Origin", "http://"+host), false},
		{"another site", with("Origin", "https://evil.example"), true},
		{"same host, another port", with("Origin", "http://127.0.0.1:1"), true},
		{"origin null", with("Origin", "null"), true},
		{"origin that is no URL", with("Origin", "::"), true},
		{"fetch metadata says same origin", with("Sec-Fetch-Site", "same-origin", "Origin", "https://behind-a-proxy.example"), false},
		{"fetch metadata says none", with("Sec-Fetch-Site", "none"), false},
		{"fetch metadata says cross-site", with("Sec-Fetch-Site", "cross-site", "Origin", "http://"+host), true},
		{"fetch metadata says same-site", with("Sec-Fetch-Site", "same-site"), true},
		{"proxy tells the host and it matches", with("Origin", "http://panel.lan", "X-Forwarded-Host", "panel.lan"), false},
		{"proxy tells the host and it differs", with("Origin", "http://evil.example", "X-Forwarded-Host", "panel.lan"), true},
		{"first of several forwarded hosts counts", with("Origin", "http://panel.lan", "X-Forwarded-Host", "panel.lan, proxy.internal"), false},
		{"proxy rewrote the host and said nothing else", with("Origin", "http://192.168.88.1", "X-Forwarded-For", "192.168.88.20"), false},
		{"proxy told the host, so for-only doesn't excuse a mismatch", with("Origin", "http://evil.example", "X-Forwarded-For", "192.168.88.20", "X-Forwarded-Host", "panel.lan"), true},
	}
	for _, c := range cases {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			code, _, body := r.call(method, "/api/nothing", c.hdr)
			want, wantCode := http.StatusNotFound, "not_found"
			if c.refused {
				want, wantCode = http.StatusForbidden, "forbidden"
			}
			if got := decode[map[string]any](t, body); code != want || got["code"] != wantCode {
				t.Errorf("%s: %s = %d %s, want %d %s", c.name, method, code, body, want, wantCode)
			}
		}
	}
	// reading is not held to it
	if code, _, _ := r.call("GET", "/api/nothing", with("Origin", "https://evil.example")); code != http.StatusNotFound {
		t.Errorf("a GET from another origin = %d, want it let through", code)
	}
	// and the header is still needed alongside a good origin
	if code, _, _ := r.call("POST", "/api/nothing", map[string]string{"Origin": "http://" + host}); code != http.StatusForbidden {
		t.Errorf("a POST without the header = %d, want 403", code)
	}
}
