// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
)

func licensesRig(t *testing.T, basePath string, files fs.FS) *rig {
	t.Helper()
	return newRigWith(t, basePath, func(cfg *Config, _ *jobs.Engine) { cfg.Licenses = files })
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// section is one file as the text shows it.
func section(name, body string) string {
	return strings.Repeat("#", 64) + "\n# " + name + "\n" + strings.Repeat("#", 64) + "\n\n" + body + "\n"
}

// noLicensesAnswer checks the plain "not found" a hub without licence files gives.
func noLicensesAnswer(t *testing.T, what string, code int, hdr http.Header, body []byte) {
	t.Helper()
	got := decode[map[string]any](t, body)
	if code != http.StatusNotFound || got["code"] != "not_found" || got["error"] == "" || len(got) != 2 || !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
		t.Errorf("%s: %d %q %s, want the JSON 404 of the API", what, code, hdr.Get("Content-Type"), body)
	}
}

func TestTheLicensesAreTheFilesOfTheDirectoryInOrder(t *testing.T) {
	files := fstest.MapFS{
		"zeta.txt":        file("Zeta.\n"),
		"web-notices.txt": file("Web.\n"),
		"NOTICES":         file("Notices.\n"),
		"alpha.txt":       file("Alpha."), // no newline at the end
		"LICENSE":         file("The licence.\n\n\n"),
		"Upper.TXT":       file("An upper-case extension counts."),
		// what is left out
		"README":       file("describes the directory, is not part of it"),
		"notes.md":     file("not a .txt"),
		".junk.txt":    file("what a copy leaves behind"),
		"empty.txt":    file(""),
		"blank.txt":    file("  \n\n"),
		"dir.txt/in":   file("a folder named like a file is not a file"),
		"fifo.txt":     {Mode: fs.ModeNamedPipe, Data: []byte("a named pipe must never be read")},
		"sub/deep.txt": file("only the top of the directory counts"),
	}
	want := strings.Join([]string{
		section("LICENSE", "The licence."),
		section("NOTICES", "Notices."),
		section("web-notices.txt", "Web."),
		section("Upper.TXT", "An upper-case extension counts."),
		section("alpha.txt", "Alpha."),
		section("zeta.txt", "Zeta."),
	}, "\n")

	for _, b := range []string{"", base} {
		r := licensesRig(t, b, files)
		for _, p := range prefixes(b) {
			code, hdr, body := r.call("GET", p+"/licenses", nil)
			if code != http.StatusOK || string(body) != want {
				t.Fatalf("base %q GET %s/licenses = %d\n%s\nwant\n%s", b, p, code, body, want)
			}
			if hdr.Get("Content-Type") != "text/plain; charset=utf-8" || hdr.Get("X-Content-Type-Options") != "nosniff" ||
				hdr.Get("Cache-Control") != "no-store" || hdr.Get("Content-Length") != strconv.Itoa(len(want)) {
				t.Errorf("headers = %v", hdr)
			}
		}
	}
}

// What the image really has: plain files in a directory, one of them a link.
func TestTheLicensesAreReadFromARealDirectoryAtEachRequest(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("LICENSE", "AGPL text\n")
	elsewhere := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(elsewhere, []byte("through a link\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, "linked.txt")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "dangling.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := licensesRig(t, "", os.DirFS(dir))

	code, _, body := r.call("GET", "/licenses", nil)
	if want := section("LICENSE", "AGPL text") + "\n" + section("linked.txt", "through a link"); code != http.StatusOK || string(body) != want {
		t.Fatalf("GET /licenses = %d\n%s\nwant\n%s", code, body, want)
	}

	// a file that appears, and one that changes, show up with no restart
	write("NOTICES", "new notices\n")
	write("LICENSE", "changed\n")
	code, _, body = r.call("GET", "/licenses", nil)
	if want := section("LICENSE", "changed") + "\n" + section("NOTICES", "new notices") + "\n" + section("linked.txt", "through a link"); code != http.StatusOK || string(body) != want {
		t.Fatalf("after the files changed, GET /licenses = %d\n%s\nwant\n%s", code, body, want)
	}
}

func TestWithoutLicenseFilesTheRouteIsPlainNotFound(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "no-such-directory")
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]fs.FS{
		"no directory configured":       nil,
		"a directory that is not there": os.DirFS(gone),
		"an empty directory":            os.DirFS(t.TempDir()),
		"a directory of other files": fstest.MapFS{
			"README": {Data: []byte("x")}, "notes.md": {Data: []byte("x")}, ".hidden.txt": {Data: []byte("x")}, "sub/a.txt": {Data: []byte("x")},
		},
		"files with nothing in them": fstest.MapFS{"LICENSE": {}, "NOTICES": {Data: []byte("\n\n")}, "a.txt": {Data: []byte("  ")}},
	}
	for name, files := range cases {
		for _, b := range []string{"", base} {
			r := licensesRig(t, b, files)
			for _, p := range prefixes(b) {
				code, hdr, body := r.call("GET", p+"/licenses", nil)
				noLicensesAnswer(t, name+" (base "+b+") "+p, code, hdr, body)
				if strings.Contains(string(body), gone) {
					t.Errorf("%s: the answer names the directory: %s", name, body)
				}
			}
		}
	}
	// a path where a directory should be is the hub's own trouble, not "nothing here"
	r := licensesRig(t, "", os.DirFS(file))
	code, _, body := r.call("GET", "/licenses", nil)
	if got := decode[map[string]any](t, body); code != http.StatusInternalServerError || got["code"] != "internal" || strings.Contains(string(body), file) {
		t.Errorf("a file for a directory = %d %s", code, body)
	}
	if !strings.Contains(r.log.String(), "request failed") {
		t.Errorf("the log has nothing about it:\n%s", r.log.String())
	}
}

// The text is public and fixed: a link opened in a new tab can't send the
// page's header, so nothing under /licenses asks for it.
func TestTheLicensesNeedNoHeader(t *testing.T) {
	files := fstest.MapFS{"LICENSE": file("The licence.\n")}
	for _, b := range []string{"", base} {
		r := licensesRig(t, b, files)
		for _, p := range prefixes(b) {
			for _, hdr := range []map[string]string{nil, {UIHeader: "1"}, {UIHeader: "0"}, {UIHeader: ""}, {"Sec-Fetch-Site": "none"}} {
				if code, _, body := r.call("GET", p+"/licenses", hdr); code != http.StatusOK || !strings.Contains(string(body), "The licence.") {
					t.Errorf("GET %s/licenses with %v = %d %s", p, hdr, code, body)
				}
			}
		}
	}
}

func TestTheLicensesAreAlsoUnderTheBasePath(t *testing.T) {
	r := licensesRig(t, base, fstest.MapFS{"LICENSE": file("The licence.\n"), "NOTICES": file("Notices.\n")})
	_, _, plain := r.call("GET", "/licenses", nil)
	code, _, under := r.call("GET", base+"/licenses", nil)
	if code != http.StatusOK || len(plain) == 0 || !bytes.Equal(plain, under) {
		t.Errorf("under the base path = %d, differs from the plain one: %q vs %q", code, under, plain)
	}
	// only that exact path: no trailing slash, no sub-path, and not the page's base with nothing after it
	for _, p := range []string{"/licenses/", "/licenses/LICENSE", base + "/licenses/", base + "/licenses/NOTICES", "/licences", "/license"} {
		code, hdr, body := r.call("GET", p, nil)
		noLicensesAnswer(t, "GET "+p, code, hdr, body)
	}
}

func TestOnlyReadingIsAnswered(t *testing.T) {
	r := licensesRig(t, base, fstest.MapFS{"LICENSE": file("The licence.\n")})
	for _, p := range []string{"/licenses", base + "/licenses"} {
		_, getHdr, getBody := r.call("GET", p, nil)
		code, hdr, body := r.call("HEAD", p, nil)
		if code != http.StatusOK || len(body) != 0 || hdr.Get("Content-Type") != getHdr.Get("Content-Type") ||
			hdr.Get("Content-Length") != strconv.Itoa(len(getBody)) || len(getBody) == 0 {
			t.Errorf("HEAD %s = %d, %d body bytes, headers %v; GET has %d", p, code, len(body), hdr, len(getBody))
		}
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, h := range []map[string]string{nil, {UIHeader: "1"}} {
				code, hdr, body := r.call(method, p, h)
				noLicensesAnswer(t, method+" "+p, code, hdr, body)
				if hdr.Get("Access-Control-Allow-Origin") != "" || hdr.Get("Allow") != "" {
					t.Errorf("%s %s sends %v", method, p, hdr)
				}
			}
		}
	}
	// and HEAD says "not found" the way GET does
	empty := licensesRig(t, "", fstest.MapFS{})
	if code, _, body := empty.call("HEAD", "/licenses", nil); code != http.StatusNotFound || len(body) != 0 {
		t.Errorf("HEAD with no files = %d, %d bytes", code, len(body))
	}
}

// What is served can't grow without limit, however the directory got big.
func TestTheLicensesAreCapped(t *testing.T) {
	// one file, no newline at the end: the text is its heading, the file and a newline
	exactly := maxLicenseBytes - len(licenseHeading("LICENSE")) - 1
	fits := fstest.MapFS{"LICENSE": {Data: bytes.Repeat([]byte("a"), exactly)}}
	r := licensesRig(t, "", fits)
	code, hdr, body := r.call("GET", "/licenses", nil)
	if code != http.StatusOK || len(body) != maxLicenseBytes || hdr.Get("Content-Length") != strconv.Itoa(maxLicenseBytes) {
		t.Fatalf("exactly %d bytes = %d, %d bytes served", maxLicenseBytes, code, len(body))
	}

	// reads is how many times a file was read: a directory that is far too big is refused
	// from the sizes alone, and only a file that lied about its size gets read before
	refused := func(what string, files fstest.MapFS, wantReads bool) {
		t.Helper()
		reads := 0
		r := licensesRig(t, "", counting{files, &reads})
		code, hdr, body := r.call("GET", "/licenses", nil)
		got := decode[map[string]any](t, body)
		if code != http.StatusInternalServerError || got["code"] != "internal" || len(body) > 300 || !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
			t.Errorf("%s = %d %q (%d bytes), want the JSON 500 of the API", what, code, hdr.Get("Content-Type"), len(body))
		}
		if bytes.Contains(body, []byte("aaaa")) {
			t.Errorf("%s: the answer carries the text", what)
		}
		if !strings.Contains(r.log.String(), "bytes that are served") {
			t.Errorf("%s: the log doesn't say why:\n%s", what, r.log.String())
		}
		if (reads > 0) != wantReads {
			t.Errorf("%s: %d reads, want some: %v", what, reads, wantReads)
		}
	}
	refused("one byte over", fstest.MapFS{"LICENSE": {Data: bytes.Repeat([]byte("a"), exactly+1)}}, false)
	refused("several files that add up to too much", fstest.MapFS{
		"LICENSE": {Data: bytes.Repeat([]byte("a"), maxLicenseBytes/2)}, "NOTICES": {Data: bytes.Repeat([]byte("a"), maxLicenseBytes/2)},
	}, false)
	refused("a big file nobody named", fstest.MapFS{"LICENSE": file("x\n"), "dump.txt": {Data: bytes.Repeat([]byte("a"), 3<<20)}}, false)

	// a file that is bigger than it said it was when it was measured
	reads := 0
	r = licensesRig(t, "", counting{growing{fstest.MapFS{"LICENSE": {Data: bytes.Repeat([]byte("a"), 3<<20)}}}, &reads})
	code, _, body = r.call("GET", "/licenses", nil)
	if got := decode[map[string]any](t, body); code != http.StatusInternalServerError || got["code"] != "internal" || len(body) > 300 || reads == 0 {
		t.Errorf("a file that grew = %d %.100s, %d reads", code, body, reads)
	}
	if !strings.Contains(r.log.String(), "bytes that are served") {
		t.Errorf("the log doesn't say why:\n%s", r.log.String())
	}
}

// counting counts the reads of the files in the directory (not of the directory itself).
type counting struct {
	fs.FS
	reads *int
}

func (c counting) Open(name string) (fs.File, error) {
	f, err := c.FS.Open(name)
	if err != nil || name == "." {
		return f, err
	}
	return countedFile{f, c.reads}, nil
}

func (c counting) Stat(name string) (fs.FileInfo, error) { return fs.Stat(c.FS, name) }

func (c counting) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(c.FS, name) }

type countedFile struct {
	fs.File
	reads *int
}

func (c countedFile) Read(p []byte) (int, error) {
	*c.reads++
	return c.File.Read(p)
}

// growing says every file is a few bytes long, as one that grew since would.
type growing struct{ fstest.MapFS }

func (g growing) Stat(name string) (fs.FileInfo, error) {
	fi, err := g.MapFS.Stat(name)
	if err != nil || fi.IsDir() {
		return fi, err
	}
	return smallInfo{fi}, nil
}

type smallInfo struct{ fs.FileInfo }

func (smallInfo) Size() int64 { return 10 }

// failing is a directory that can't be read, the way a permission would make one.
type failing struct{ err error }

func (f failing) Open(string) (fs.File, error) { return nil, f.err }

func TestAFailureToReadIsTheHubsOwnAndTheLogHasIt(t *testing.T) {
	r := licensesRig(t, "", failing{&fs.PathError{Op: "open", Path: "/secret/place", Err: fs.ErrPermission}})
	code, _, body := r.call("GET", "/licenses", nil)
	got := decode[map[string]any](t, body)
	if code != http.StatusInternalServerError || got["code"] != "internal" || strings.Contains(string(body), "/secret/place") {
		t.Errorf("an unreadable directory = %d %s", code, body)
	}
	if !strings.Contains(r.log.String(), "/secret/place") {
		t.Errorf("the log lacks the cause:\n%s", r.log.String())
	}

	// a file that lists but won't open
	r = licensesRig(t, "", openFails{fstest.MapFS{"LICENSE": file("x\n")}})
	if code, _, body := r.call("GET", "/licenses", nil); code != http.StatusInternalServerError {
		t.Errorf("a file that won't open = %d %s", code, body)
	}
}

type openFails struct{ fstest.MapFS }

func (o openFails) Open(name string) (fs.File, error) {
	if name == "." {
		return o.MapFS.Open(name)
	}
	return nil, errors.New("disk on fire")
}
