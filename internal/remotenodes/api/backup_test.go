// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/backup"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// fakeBackup stands in for the manager's backups. What it was given it keeps,
// so that a test can see it came through as sent.
type fakeBackup struct {
	mu     sync.Mutex
	err    error
	name   string
	file   []byte
	nodes  int
	passes []string
	files  [][]byte
}

func (f *fakeBackup) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeBackup) ExportBackup(_ context.Context, pass string) (string, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.passes = append(f.passes, pass)
	if f.err != nil {
		return "", nil, f.err
	}
	return f.name, f.file, nil
}

func (f *fakeBackup) ImportBackup(_ context.Context, pass string, data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.passes = append(f.passes, pass)
	f.files = append(f.files, slices.Clone(data))
	if f.err != nil {
		return 0, f.err
	}
	return f.nodes, nil
}

func (f *fakeBackup) given() (passes []string, files [][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.passes), slices.Clone(f.files)
}

func newFakeBackup() *fakeBackup {
	return &fakeBackup{name: "remote-nodes-backup-2026-10-30.rnbackup", file: []byte("RNBACKUP\x01 pretend this is sealed"), nodes: 2}
}

func withBackup(b BackupOps) func(*Config, *jobs.Engine) {
	return func(c *Config, _ *jobs.Engine) { c.Backup = b }
}

// withOps applies several of the options above.
func withOps(mods ...func(*Config, *jobs.Engine)) func(*Config, *jobs.Engine) {
	return func(c *Config, j *jobs.Engine) {
		for _, m := range mods {
			m(c, j)
		}
	}
}

func backupRoutes() []subsReq {
	return []subsReq{
		{"POST", "/api/backup/export", `{"passphrase":"correct horse"}`},
		{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":"` + base64.StdEncoding.EncodeToString([]byte("RNBACKUP...")) + `"}`},
	}
}

func importBody(pass string, file []byte) string {
	b, _ := json.Marshal(map[string]string{"passphrase": pass, "data": base64.StdEncoding.EncodeToString(file)})
	return string(b)
}

func TestWithoutBackupOpsTheRoutesAreNotThere(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		for _, rt := range backupRoutes() {
			for _, prefix := range prefixes(b) {
				code, _, body := r.do(rt.method, prefix+rt.path, rt.body)
				wantError(t, rt.method+" "+prefix+rt.path, code, body, http.StatusNotFound, "not_found")
				code, _, body = r.subsCall(rt.method, prefix+rt.path, nil, []byte(rt.body), false)
				wantError(t, rt.method+" "+prefix+rt.path+" without the header", code, body, http.StatusForbidden, "forbidden")
			}
		}
	}
}

func TestBackupCallsNeedThePagesHeaderAndSite(t *testing.T) {
	fake := newFakeBackup()
	wantCalls := 0
	for _, b := range []string{"", base} {
		r := newRigWith(t, b, withBackup(fake))
		host := strings.TrimPrefix(r.srv.URL, "http://")
		for _, rt := range backupRoutes() {
			for _, prefix := range prefixes(b) {
				for name, hdr := range map[string]map[string]string{
					"no header": nil, "an empty header": {UIHeader: ""}, "a header that is not 1": {UIHeader: "yes"},
					"another site":      {UIHeader: "1", "Origin": "https://evil.example"},
					"fetch metadata":    {UIHeader: "1", "Sec-Fetch-Site": "cross-site"},
					"a null origin":     {UIHeader: "1", "Origin": "null"},
					"same-site, not it": {UIHeader: "1", "Sec-Fetch-Site": "same-site"},
				} {
					code, _, body := r.subsCall(rt.method, prefix+rt.path, hdr, []byte(rt.body), false)
					wantError(t, rt.method+" "+prefix+rt.path+" with "+name, code, body, http.StatusForbidden, "forbidden")
				}
				hdr := map[string]string{UIHeader: "1", "Origin": "http://" + host}
				if code, _, body := r.subsCall(rt.method, prefix+rt.path, hdr, []byte(rt.body), false); code != http.StatusOK {
					t.Errorf("%s %s from its own site = %d %.80s", rt.method, prefix+rt.path, code, body)
				}
				wantCalls++
			}
		}
	}
	if passes, _ := fake.given(); len(passes) != wantCalls {
		t.Errorf("the manager was asked %d times, want %d: only the calls from this site", len(passes), wantCalls)
	}
}

func TestAnExportIsAFileTheBrowserSaves(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := newFakeBackup()
		r := newRigWith(t, b, withBackup(fake))
		for _, prefix := range prefixes(b) {
			// the passphrase goes through as typed: spaces, Persian and all
			pass := "  رمز عبور طولانی  "
			body, _ := json.Marshal(map[string]string{"passphrase": pass})
			code, hdr, got := r.subsCall("POST", prefix+"/api/backup/export", subsUI, body, false)
			if code != http.StatusOK || !bytes.Equal(got, fake.file) {
				t.Fatalf("export = %d %q", code, got)
			}
			if hdr.Get("Content-Type") != "application/octet-stream" || hdr.Get("Cache-Control") != "no-store" ||
				hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Content-Length") != strconv.Itoa(len(fake.file)) {
				t.Errorf("headers = %v", hdr)
			}
			// the page takes the file's name from this header
			kind, params, err := mime.ParseMediaType(hdr.Get("Content-Disposition"))
			if err != nil || kind != "attachment" || params["filename"] != "remote-nodes-backup-2026-10-30.rnbackup" {
				t.Errorf("Content-Disposition = %q (%v, %v)", hdr.Get("Content-Disposition"), kind, params)
			}
			if got := hdr.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment; filename=") {
				t.Errorf("Content-Disposition = %q, want attachment; filename=...", got)
			}
		}
		if passes, _ := fake.given(); len(passes) != len(prefixes(b)) || passes[0] != "  رمز عبور طولانی  " {
			t.Errorf("the manager was given %q", passes)
		}
	}
}

// A name that is not plain ASCII still makes a header the page reads back, and
// nothing in a name can start another header.
func TestAFileNameOfAnyKindMakesAHeaderThatReadsBack(t *testing.T) {
	for _, name := range []string{"plain.rnbackup", "naïve 1.rnbackup", "پشتیبان.rnbackup", "bad\r\nSet-Cookie: x=y", `quote".rnbackup`} {
		fake := newFakeBackup()
		fake.name = name
		r := newRigWith(t, "", withBackup(fake))
		code, hdr, _ := r.do("POST", "/api/backup/export", `{"passphrase":"correct horse"}`)
		got := hdr.Get("Content-Disposition")
		if code != http.StatusOK || !strings.HasPrefix(got, "attachment; filename") || hdr.Get("Set-Cookie") != "" {
			t.Errorf("%q: export = %d with Content-Disposition %q", name, code, got)
		}
		if _, params, err := mime.ParseMediaType(got); err != nil || params["filename"] != name {
			t.Errorf("%q came back as %q (%v)", name, params["filename"], err)
		}
	}
}

func TestAnImportHandsTheManagerTheDecodedFileAndTheTypedPassphrase(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := newFakeBackup()
		fake.nodes = 3
		r := newRigWith(t, b, withBackup(fake))
		file := []byte("RNBACKUP\x01\x00\xff binary \x00 bytes")
		for _, prefix := range prefixes(b) {
			code, hdr, body := r.do("POST", prefix+"/api/backup/import", importBody(" pass phrase ", file))
			if string(body) != `{"ok":true,"nodes":3}`+"\n" || code != http.StatusOK || hdr.Get("Cache-Control") != "no-store" {
				t.Errorf("import = %d %q", code, body)
			}
		}
		passes, files := fake.given()
		for i := range prefixes(b) {
			if passes[i] != " pass phrase " || !bytes.Equal(files[i], file) {
				t.Errorf("the manager was given %q and %q", passes[i], files[i])
			}
		}
	}
}

func TestABackupBodyThatIsNotWhatTheCallTakesIsRefusedBeforeTheManager(t *testing.T) {
	fake := newFakeBackup()
	r := newRigWith(t, "", withBackup(fake))
	good := base64.StdEncoding.EncodeToString([]byte("RNBACKUP..."))
	for name, c := range map[string]struct {
		rt   subsReq
		code string
	}{
		"export: no body":                     {subsReq{"POST", "/api/backup/export", ""}, "bad_request"},
		"export: no passphrase field":         {subsReq{"POST", "/api/backup/export", `{}`}, "bad_request"},
		"export: a passphrase that is a list": {subsReq{"POST", "/api/backup/export", `{"passphrase":["x"]}`}, "bad_request"},
		"export: another field":               {subsReq{"POST", "/api/backup/export", `{"passphrase":"correct horse","data":"x"}`}, "bad_request"},
		"export: two values":                  {subsReq{"POST", "/api/backup/export", `{"passphrase":"correct horse"}{}`}, "bad_request"},
		"import: no body":                     {subsReq{"POST", "/api/backup/import", ""}, "bad_request"},
		"import: no passphrase":               {subsReq{"POST", "/api/backup/import", `{"data":"` + good + `"}`}, "bad_request"},
		"import: an empty passphrase":         {subsReq{"POST", "/api/backup/import", `{"passphrase":"","data":"` + good + `"}`}, "bad_request"},
		"import: another field":               {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":"` + good + `","nodes":2}`}, "bad_request"},
		"import: data that is a number":       {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":5}`}, "bad_request"},
		"import: no data":                     {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse"}`}, "backup_bad_file"},
		"import: empty data":                  {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":""}`}, "backup_bad_file"},
		"import: not base64":                  {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":"not base64 at all!"}`}, "backup_bad_file"},
		"import: base64 without its padding":  {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":"` + strings.TrimRight(good, "=") + `"}`}, "backup_bad_file"},
		"import: the other alphabet":          {subsReq{"POST", "/api/backup/import", `{"passphrase":"correct horse","data":"__--"}`}, "backup_bad_file"},
	} {
		code, _, body := r.do(c.rt.method, c.rt.path, c.rt.body)
		got := wantError(t, name, code, body, http.StatusBadRequest, c.code)
		for _, sent := range []string{"correct horse", good, "not base64"} {
			if strings.Contains(fmt.Sprint(got["error"]), sent) {
				t.Errorf("%s: the error repeats what was sent: %v", name, got["error"])
			}
		}
	}
	if passes, _ := fake.given(); len(passes) != 0 {
		t.Errorf("the manager was asked %d times", len(passes))
	}
}

// An import may send up to 4 MiB, the file in base64 inside the JSON, whether
// or not the length is known up front, and not a byte more. What the file is
// decoded is held to 4 MiB too, before anything is decoded.
func TestAnImportIsCappedAt4MiBBeforeAnythingIsDecoded(t *testing.T) {
	fake := newFakeBackup()
	r := newRigWith(t, "", withBackup(fake))
	const frame = `{"passphrase":"p","data":""}`
	room := maxBackupBody - len(frame)
	if room%4 != 0 {
		t.Fatalf("the room for base64 is %d characters, not a whole number of groups", room)
	}
	exact := `{"passphrase":"p","data":"` + strings.Repeat("QUJD", room/4) + `"}`
	if len(exact) != maxBackupBody {
		t.Fatalf("test body is %d bytes", len(exact))
	}
	over := `{"passphrase":"p","data":"` + strings.Repeat("QUJD", room/4) + `QUJD"}`
	for _, chunked := range []bool{false, true} {
		code, _, body := r.subsCall("POST", "/api/backup/import", subsUI, []byte(exact), chunked)
		if code != http.StatusOK {
			t.Errorf("chunked=%v: a body of exactly %d bytes = %d %.80s", chunked, len(exact), code, body)
		}
		code, _, body = r.subsCall("POST", "/api/backup/import", subsUI, []byte(over), chunked)
		got := wantError(t, fmt.Sprint("chunked=", chunked, ": a body one group over"), code, body, http.StatusBadRequest, "backup_bad_file")
		if !strings.Contains(fmt.Sprint(got["error"]), "too large") {
			t.Errorf("chunked=%v: the reason is %v", chunked, got["error"])
		}
	}
	code, body := announceTooMuch(t, r, "/api/backup/import", maxBackupBody+1)
	wantError(t, "an announced body over the limit", code, body, http.StatusBadRequest, "backup_bad_file")
	if _, files := fake.given(); len(files) != 2 || len(files[0]) != room/4*3 {
		t.Errorf("the manager was given %d files", len(files))
	}

	// the decoded size is held to the same limit, before the decoding
	if _, err := backupFile(strings.Repeat("QUJD", maxBackupBytes/3+1)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("a file that decodes to over the limit: %v", err)
	}
	if got, err := backupFile(strings.Repeat("QUJD", maxBackupBytes/3/4)); err != nil || len(got) != maxBackupBytes/3/4*3 {
		t.Errorf("a file that decodes to under it: %d bytes, %v", len(got), err)
	}
}

func TestTheBackupCallsAnswerWithTheContractsCodes(t *testing.T) {
	newer := fmt.Errorf("%w: %w", store.ErrBadSnapshot, fmt.Errorf("%w: the file is version 9", model.ErrNewer))
	_, _, _, invalid := errorsFromTheModel(t)
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		says   string
	}{
		{"a wrong passphrase", backup.ErrBadPassphrase, 400, CodeBackupBadPassphrase, "passphrase"},
		{"a file that isn't one", fmt.Errorf("%w: it isn't a backup file", backup.ErrBadFile), 400, CodeBackupBadFile, "it isn't a backup file."},
		{"a file with no reason", backup.ErrBadFile, 400, CodeBackupBadFile, "not a backup this version can open."},
		{"a weak passphrase", fmt.Errorf("%w: use at least 8 characters", backup.ErrWeakPassphrase), 400, CodeBadRequest, "at least 8 characters"},
		{"an install that is not empty", store.ErrNotEmpty, 400, CodeBadRequest, "already has nodes or subscriptions"},
		{"a backup from a newer release", newer, 400, CodeBackupBadFile, "newer version"},
		{"a backup that is no state", fmt.Errorf("%w: unreadable state", store.ErrBadSnapshot), 400, CodeBackupBadFile, "can't use"},
		{"a backup with no CA", &model.InvalidError{Msg: "this backup has no certificate authority"}, 400, CodeBadRequest, "no certificate authority"},
		{"a bad value", invalid, 400, CodeBadRequest, ""},
		{"the hub's own fault", errors.New("scrypt ran out of memory"), 500, CodeInternal, "inside the hub"},
	}
	for _, c := range cases {
		fake := newFakeBackup()
		fake.fail(c.err)
		r := newRigWith(t, "", withBackup(fake))
		for _, rt := range backupRoutes() {
			code, _, body := r.do(rt.method, rt.path, rt.body)
			got := wantError(t, c.name+": "+rt.path, code, body, c.status, c.code)
			if !strings.Contains(fmt.Sprint(got["error"]), c.says) {
				t.Errorf("%s: %s says %q, want it to say %q", c.name, rt.path, got["error"], c.says)
			}
			if c.code == CodeInternal && strings.Contains(string(body), "scrypt") {
				t.Errorf("an error of the hub's own is shown to the page: %s", body)
			}
		}
	}
}

// A request that went away while a backup waited is not a failure.
func TestABackupRequestThatWentAwayIsNotAFailure(t *testing.T) {
	fake := newFakeBackup()
	fake.fail(context.Canceled)
	r := newRigWith(t, "", withBackup(fake))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := New(Config{Store: r.st, Backup: fake, Log: nil})
	for _, rt := range backupRoutes() {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body)).WithContext(ctx)
		req.Header.Set(UIHeader, "1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Body.Len() != 0 {
			t.Errorf("%s answered %q", rt.path, rec.Body.String())
		}
	}
}

// The passphrase is nobody's but the owner's: it is not in an answer, an error
// or the log, whether the call worked or not.
func TestThePassphraseIsNeverLoggedOrEchoed(t *testing.T) {
	const pass = "MARKER-passphrase-of-the-owner"
	fake := newFakeBackup()
	r := newRigWith(t, "", withBackup(fake))
	var heard []byte
	say := func(code int, body []byte) { heard = append(heard, body...); _ = code }

	for _, err := range []error{nil, backup.ErrBadPassphrase, backup.ErrWeakPassphrase, store.ErrNotEmpty, errors.New("something broke")} {
		fake.fail(err)
		code, _, body := r.do("POST", "/api/backup/export", `{"passphrase":"`+pass+`"}`)
		if err != nil {
			say(code, body)
		}
		code, _, body = r.do("POST", "/api/backup/import", importBody(pass, []byte("RNBACKUP...")))
		say(code, body)
		// and a body that doesn't parse, with the passphrase in it
		code, _, body = r.do("POST", "/api/backup/import", `{"passphrase":"`+pass+`","data":5}`)
		say(code, body)
		code, _, body = r.do("POST", "/api/backup/export", `{"passphrase":"`+pass+`","extra":1}`)
		say(code, body)
	}
	if bytes.Contains(heard, []byte(pass)) {
		t.Errorf("an answer carries the passphrase: %s", heard)
	}
	if strings.Contains(r.log.String(), pass) {
		t.Errorf("the log carries the passphrase:\n%s", r.log.String())
	}
}

// backupBodies are what the calls that are not the file answer: the import's
// own answer and its refusals, which carry no secret.
func backupBodies(r *rig, fake *fakeBackup, b string) map[string][]byte {
	bodies := map[string][]byte{}
	record := func(what string, code int, body []byte) { bodies[fmt.Sprint(b, " ", what, " ", code)] = body }
	pass := "MARKER-" + privateKey
	for _, bad := range []error{nil, backup.ErrBadPassphrase, backup.ErrBadFile, store.ErrNotEmpty,
		errors.New("boom " + privateKey + " " + hostKeyPin), model.ErrNameTaken} {
		fake.fail(bad)
		code, _, body := r.do("POST", b+"/api/backup/import", importBody(pass, []byte("RNBACKUP...")))
		record(fmt.Sprint("import failing with ", bad), code, body)
		if bad != nil {
			code, _, body = r.do("POST", b+"/api/backup/export", `{"passphrase":"`+pass+`"}`)
			record(fmt.Sprint("export failing with ", bad), code, body)
		}
	}
	fake.fail(nil)
	code, _, body := r.do("POST", b+"/api/backup/import", `{"passphrase":"`+pass+`","data":5}`)
	record("import with a bad body", code, body)
	code, _, body = r.subsCall("POST", b+"/api/backup/import", nil, []byte(importBody(pass, []byte("x"))), false)
	record("import without the header", code, body)
	return bodies
}

func TestNoSecretReachesABrowserFromTheBackupCalls(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := newFakeBackup()
		r := newRigWith(t, b, withBackup(fake))
		r.populate()
		bodies := backupBodies(r, fake, b)
		if len(bodies) < 10 {
			t.Errorf("only %d bodies were looked at", len(bodies))
		}
		for what, body := range bodies {
			for _, s := range append(secrets(), "MARKER-") {
				if bytes.Contains(body, []byte(s)) {
					t.Errorf("%s carries %q", what, s)
				}
			}
			var generic any
			if json.Unmarshal(body, &generic) == nil {
				walkKeys(generic, func(key string) {
					if secretish(key) {
						t.Errorf("%s has a key named %q", what, key)
					}
				})
			}
		}
	}
}

// The limits are the contract's: 64 KiB for the calls that take a few words, and
// 4 MiB for an import, in the request and once decoded. The tests above are
// built from these, so a change of the numbers is pinned here.
func TestTheLimitsAre64KiBAnd4MiB(t *testing.T) {
	if maxSmallBody != 64*1024 || maxBackupBody != 4*1024*1024 || maxBackupBytes != 4*1024*1024 {
		t.Errorf("limits are %d, %d and %d bytes", maxSmallBody, maxBackupBody, maxBackupBytes)
	}
}
