// SPDX-License-Identifier: AGPL-3.0-or-later

package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestTheEmbeddedPageHasAHeadForTheBaseTag(t *testing.T) {
	if !bytes.Contains(page, []byte("<head>")) {
		t.Fatal("the embedded page has no <head> to put the base tag after")
	}
}

func get(h http.Handler, method string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
	return rec
}

func TestServesThePageWithTheBaseTag(t *testing.T) {
	for base, want := range map[string]string{
		"":                              `<base href="/">`,
		"/api/plugin/view/remote-nodes": `<base href="/api/plugin/view/remote-nodes/">`,
		"/a":                            `<base href="/a/">`,
	} {
		rec := get(New(base), http.MethodGet)
		if rec.Code != http.StatusOK {
			t.Fatalf("base %q: status %d", base, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, want) || strings.Count(body, "<base ") != 1 {
			t.Errorf("base %q: want %s once in\n%s", base, want, body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("content type %q", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("cache control %q", cc)
		}
		if rec.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
			t.Errorf("content length %q for %d bytes", rec.Header().Get("Content-Length"), len(body))
		}
	}
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + strings.TrimLeft(string(rune('0'+n%10)), ""))
}

func TestInjectLeavesAPageWithoutAHeadAlone(t *testing.T) {
	in := []byte("<html><body>x</body></html>")
	if got := Inject(in, "/a"); !bytes.Equal(got, in) {
		t.Fatalf("got %q", got)
	}
}

func TestBasePath(t *testing.T) {
	ok := map[string]string{
		"":                               "",
		"  ":                             "",
		"/":                              "",
		"/api/plugin/view/remote-nodes":  "/api/plugin/view/remote-nodes",
		"/api/plugin/view/remote-nodes/": "/api/plugin/view/remote-nodes",
		" /a/b.c_d~e-f ":                 "/a/b.c_d~e-f",
	}
	for in, want := range ok {
		if got, err := BasePath(in); err != nil || got != want {
			t.Errorf("BasePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"a", "api/x", "/a//b", "/a/./b", "/a/../b", "/..", "/a b", "/a\"b", "/a<b", "/a?x=1", "/a#x", "/a%2e", "/é", "/a\nb",
	} {
		if got, err := BasePath(in); err == nil {
			t.Errorf("BasePath(%q) = %q, want an error", in, got)
		}
	}
}

// plain stands in for a regexp; the two must agree.
var segment = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

func TestPlainMatchesTheRegexp(t *testing.T) {
	for _, s := range []string{
		"", ".", "..", "a", "A-z_0.9~", "a b", "a/b", "a\n", "\n", "é", "\xff", "a\x00", "%2e", "api", "remote-nodes",
		"a;b", "a?b", "a#b", `a"b`, "<", strings.Repeat("x", 500),
	} {
		if got, want := plain(s), segment.MatchString(s); got != want {
			t.Errorf("plain(%q) = %v, the regexp says %v", s, got, want)
		}
	}
}
