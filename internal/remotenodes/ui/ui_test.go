// SPDX-License-Identifier: AGPL-3.0-or-later

package ui

import (
	"bytes"
	"errors"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/andybalholm/brotli"
)

func TestTheEmbeddedPageHasAHeadForTheBaseTag(t *testing.T) {
	page, err := Page()
	if err != nil {
		t.Fatal(err)
	}
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
		h, err := New(base)
		if err != nil {
			t.Fatal(err)
		}
		rec := get(h, http.MethodGet)
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

// packed is html as a brotli stream, like the one npm run pack writes. A
// middle level is enough here: the reader doesn't care, and the top one is slow.
func packed(t *testing.T, html []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := brotli.NewWriterLevel(&b, 5)
	if _, err := w.Write(html); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var (
	placeholder = []byte("<!doctype html><html><head></head><body>placeholder</body></html>")
	built       = []byte("<!doctype html><html><head><title>app</title></head><body>" + strings.Repeat("the app ", 300) + "</body></html>")
)

func TestPickPrefersTheBrotliBuild(t *testing.T) {
	both := fstest.MapFS{"index.html": {Data: placeholder}, "index.html.br": {Data: packed(t, built)}}
	got, err := pick(both)
	if err != nil || !bytes.Equal(got, built) {
		t.Fatalf("pick = %q, %v; want the build", got, err)
	}
}

// The real page is about 140 KB and doesn't repeat itself the way the filler
// in the other tests does, so the stream has real work in it.
func TestABigPageComesBackWhole(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	var sb strings.Builder
	sb.WriteString("<!doctype html><html><head></head><body><script>")
	for sb.Len() < 200<<10 {
		sb.WriteString(strconv.FormatUint(rng.Uint64(), 36))
		sb.WriteByte(';')
	}
	sb.WriteString("</script></body></html>")
	big := []byte(sb.String())

	got, err := pick(fstest.MapFS{"index.html.br": {Data: packed(t, big)}})
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("a %d byte page came back as %d bytes, %v", len(big), len(got), err)
	}
}

func TestACorruptBrotliBuildIsAnError(t *testing.T) {
	whole := packed(t, built)
	for name, data := range map[string][]byte{
		"not brotli":     []byte("not brotli at all"),
		"empty":          {},
		"cut in half":    whole[:len(whole)/2],
		"cut by a byte":  whole[:len(whole)-1],
		"the wrong file": placeholder,
	} {
		// the placeholder is there too: it must not stand in for a build that is broken
		fsys := fstest.MapFS{"index.html": {Data: placeholder}, "index.html.br": {Data: data}}
		got, err := pick(fsys)
		if err == nil || !strings.Contains(err.Error(), "corrupt") {
			t.Errorf("%s: pick = %q, %v; want a corrupt-page error", name, got, err)
		}
		if h, err := newFS(fsys, "/x"); err == nil || h != nil {
			t.Errorf("%s: newFS gave a handler for a broken page: %v", name, err)
		}
	}
}

func TestThePlaceholderServesWithoutABuild(t *testing.T) {
	got, err := pick(fstest.MapFS{"index.html": {Data: placeholder}})
	if err != nil || !bytes.Equal(got, placeholder) {
		t.Fatalf("pick = %q, %v; want the placeholder", got, err)
	}
	if _, err := pick(fstest.MapFS{}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nothing embedded: err = %v, want fs.ErrNotExist", err)
	}
}

func TestTheBaseTagGoesIntoWhicheverPageIsServed(t *testing.T) {
	for name, tc := range map[string]struct {
		fsys fs.FS
		page string // a phrase only that page has
	}{
		"the build":       {fstest.MapFS{"index.html": {Data: placeholder}, "index.html.br": {Data: packed(t, built)}}, "the app"},
		"the placeholder": {fstest.MapFS{"index.html": {Data: placeholder}}, "placeholder"},
	} {
		h, err := newFS(tc.fsys, "/api/plugin/view/remote-nodes")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body := get(h, http.MethodGet).Body.String()
		if !strings.Contains(body, `<head><base href="/api/plugin/view/remote-nodes/">`) || strings.Count(body, "<base ") != 1 {
			t.Errorf("%s: the base tag is not there exactly once: %s", name, body)
		}
		if !strings.Contains(body, tc.page) {
			t.Errorf("%s: served the wrong page: %s", name, body)
		}
	}
}
