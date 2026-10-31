// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// fakeNodeOps records what the endpoints ask of the manager, and answers what a
// test says. RenameNode changes the state as the manager would, which is what
// the answer is read from.
type fakeNodeOps struct {
	mu sync.Mutex
	st *store.Store

	calls []string
	err   error // every call fails with it

	probe     ProbeRequest
	install   InstallRequest
	renamed   string
	settings  SettingsRequest
	handshake string
	updated   string
	removed   RemoveRequest
	ids       []uint // the node of each call that has one
}

func (f *fakeNodeOps) note(call string, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if id != 0 {
		f.ids = append(f.ids, id)
	}
	return f.err
}

func (f *fakeNodeOps) Probe(_ context.Context, req ProbeRequest) (ProbeResult, error) {
	err := f.note("Probe", 0)
	f.mu.Lock()
	f.probe = req
	f.mu.Unlock()
	return ProbeResult{Fingerprint: "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq", Status: ProbeKnown}, err
}

func (f *fakeNodeOps) AddNode(_ context.Context, req InstallRequest) (string, error) {
	err := f.note("AddNode", 0)
	f.mu.Lock()
	f.install = req
	f.mu.Unlock()
	return "job-install", err
}

func (f *fakeNodeOps) RenameNode(_ context.Context, id uint, name string) error {
	if err := f.note("RenameNode", id); err != nil {
		return err
	}
	f.mu.Lock()
	f.renamed = name
	f.mu.Unlock()
	return f.st.Update(func(s *model.State) error {
		s.FindNode(id).Name = name
		return nil
	})
}

func (f *fakeNodeOps) ChangeSettings(_ context.Context, id uint, req SettingsRequest) (string, error) {
	err := f.note("ChangeSettings", id)
	f.mu.Lock()
	f.settings = req
	f.mu.Unlock()
	return "job-settings", err
}

func (f *fakeNodeOps) CheckHandshake(_ context.Context, id uint, target string) (HandshakeCheck, error) {
	err := f.note("CheckHandshake", id)
	f.mu.Lock()
	f.handshake = target
	f.mu.Unlock()
	return HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 4200, LatencyMs: 38}, err
}

func (f *fakeNodeOps) CheckUpdates(_ context.Context, id uint) error {
	return f.note("CheckUpdates", id)
}

func (f *fakeNodeOps) UpdateNode(_ context.Context, id uint, kind string) (string, error) {
	err := f.note("UpdateNode", id)
	f.mu.Lock()
	f.updated = kind
	f.mu.Unlock()
	return "job-update", err
}

func (f *fakeNodeOps) RenewCert(_ context.Context, id uint) (string, error) {
	return "job-renew", f.note("RenewCert", id)
}

func (f *fakeNodeOps) RotateKeys(_ context.Context, id uint) (string, error) {
	return "job-rotate", f.note("RotateKeys", id)
}

func (f *fakeNodeOps) RemoveNode(_ context.Context, id uint, req RemoveRequest) (string, error) {
	err := f.note("RemoveNode", id)
	f.mu.Lock()
	f.removed = req
	f.mu.Unlock()
	return "job-remove", err
}

func (f *fakeNodeOps) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeNodeOps) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// opsRig is a hub with the node endpoints, over a state that has a node that is
// set up (1) and one that is still installing (2).
func opsRig(t *testing.T, basePath string) (*rig, *fakeNodeOps) {
	t.Helper()
	ops := &fakeNodeOps{}
	r := newRigWith(t, basePath, func(c *Config, _ *jobs.Engine) { c.NodeOps = ops })
	ops.st = r.st
	r.populate()
	return r, ops
}

// send makes a request with a body and the headers given.
func (r *rig) send(method, path, body string, headers map[string]string) (int, http.Header, []byte) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		r.t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, out
}

// page is what the page does: the call with its header, as the page.
func (r *rig) page(method, path, body string) (int, http.Header, []byte) {
	r.t.Helper()
	h := map[string]string{UIHeader: "1"}
	if body != "" {
		h["Content-Type"] = "application/json"
	}
	return r.send(method, path, body, h)
}

// every call the contract has to change a node, with a body the page sends.
var nodeCalls = []struct {
	name, method, path, body string
	id                       bool // names node 1 in its path
}{
	{"probe", "POST", "/api/probe", `{"host":"203.0.113.9","sshPort":22,"sshUser":"root"}`, false},
	{"install", "POST", "/api/nodes", `{"sshPort":22,"sshUser":"root","password":"p","hostKey":"SHA256:x","name":"Frankfurt","host":"203.0.113.9","entryPort":8443}`, false},
	{"rename", "PATCH", "/api/nodes/1", `{"name":"Berlin"}`, true},
	{"settings", "PATCH", "/api/nodes/1/settings", `{"entryPort":8443,"handshakeTarget":"www.example.com:443","sni":"www.example.com"}`, true},
	{"handshake-check", "POST", "/api/nodes/1/handshake-check", `{"target":"www.example.com:443"}`, true},
	{"check-updates", "POST", "/api/nodes/1/check-updates", ``, true},
	{"update", "POST", "/api/nodes/1/update", `{"kind":"agent"}`, true},
	{"renew-cert", "POST", "/api/nodes/1/renew-cert", ``, true},
	{"rotate-keys", "POST", "/api/nodes/1/rotate-keys", ``, true},
	{"remove", "POST", "/api/nodes/1/remove", `{"force":true}`, true},
}

func TestEveryNodeCallAnswersInTheContractsShape(t *testing.T) {
	for _, b := range []string{"", base} {
		for _, prefix := range prefixes(b) {
			r, ops := opsRig(t, b)
			t.Run(fmt.Sprintf("base %q prefix %q", b, prefix), func(t *testing.T) {
				// what each call answers
				got := map[string]struct {
					code int
					body string
				}{}
				for _, c := range nodeCalls {
					code, hdr, body := r.page(c.method, prefix+c.path, c.body)
					if !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") || hdr.Get("Cache-Control") != "no-store" || hdr.Get("X-Content-Type-Options") != "nosniff" {
						t.Errorf("%s: headers %v", c.name, hdr)
					}
					got[c.name] = struct {
						code int
						body string
					}{code, string(body)}
				}
				for name, want := range map[string]struct {
					code int
					body string
				}{
					"probe":           {200, `{"fingerprint":"SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq","status":"known"}`},
					"install":         {202, `{"jobId":"job-install"}`},
					"settings":        {202, `{"jobId":"job-settings"}`},
					"handshake-check": {200, `{"ok":true,"tls13":true,"x25519":true,"handshakeBytes":4200,"latencyMs":38,"error":""}`},
					"update":          {202, `{"jobId":"job-update"}`},
					"renew-cert":      {202, `{"jobId":"job-renew"}`},
					"rotate-keys":     {202, `{"jobId":"job-rotate"}`},
					"remove":          {202, `{"jobId":"job-remove"}`},
				} {
					if g := got[name]; g.code != want.code || strings.TrimSpace(g.body) != want.body {
						t.Errorf("%s answered %d %s, want %d %s", name, g.code, g.body, want.code, want.body)
					}
				}
				// the two that answer with the node give the node as the page reads it, events included
				for _, name := range []string{"rename", "check-updates"} {
					g := got[name]
					if g.code != 200 {
						t.Errorf("%s answered %d %s", name, g.code, g.body)
						continue
					}
					n := decode[Node](t, []byte(g.body))
					raw := decode[map[string]json.RawMessage](t, []byte(g.body))
					if n.ID != 1 || string(raw["events"]) != "[]" || n.Registered != true {
						t.Errorf("%s answered %s", name, g.body)
					}
					wantKeys(t, name, decode[map[string]any](t, []byte(g.body)), "id name host sshPort sshUser registered createdAt entryPort handshakeTarget sni sniCustom publicKey shortId arch agentVersion xrayVersion lastSeenAt subscriptionCount tunnel agent cert updates usage events")
				}
				if n := decode[Node](t, []byte(got["rename"].body)); n.Name != "Berlin" {
					t.Errorf("the renamed node is %q", n.Name)
				}
			})

			// what the manager was asked
			if want := []string{"Probe", "AddNode", "RenameNode", "ChangeSettings", "CheckHandshake", "CheckUpdates", "UpdateNode", "RenewCert", "RotateKeys", "RemoveNode"}; fmt.Sprint(ops.called()) != fmt.Sprint(want) {
				t.Errorf("calls %v, want %v", ops.called(), want)
			}
			for _, id := range ops.ids {
				if id != 1 {
					t.Errorf("a call was for node %d", id)
				}
			}
			if len(ops.ids) != 8 {
				t.Errorf("%d calls with a node", len(ops.ids))
			}
			if p := ops.probe; p != (ProbeRequest{Host: "203.0.113.9", SSHPort: 22, SSHUser: "root"}) {
				t.Errorf("probe = %+v", p)
			}
			want := InstallRequest{Login: Login{SSHPort: 22, SSHUser: "root", Password: "p", HostKey: "SHA256:x"}, Name: "Frankfurt", Host: "203.0.113.9", EntryPort: 8443}
			if ops.install != want {
				t.Errorf("install = %v", ops.install)
			}
			s := ops.settings
			if s.EntryPort == nil || *s.EntryPort != 8443 || s.HandshakeTarget == nil || *s.HandshakeTarget != "www.example.com:443" || s.SNI == nil || *s.SNI != "www.example.com" {
				t.Errorf("settings = %+v", s)
			}
			if ops.renamed != "Berlin" || ops.handshake != "www.example.com:443" || ops.updated != "agent" || !ops.removed.Force || ops.removed.Login != nil {
				t.Errorf("renamed %q, handshake %q, updated %q, removed %v", ops.renamed, ops.handshake, ops.updated, ops.removed)
			}
		}
	}
}

// What the page sends for the optional parts: a settings call that names one
// field, a removal with a login and one with none, an install that leaves the
// entry port out.
func TestOptionalPartsOfARequestAreOptional(t *testing.T) {
	r, ops := opsRig(t, "")
	for _, c := range []struct{ path, body string }{
		{"/api/nodes/1/settings", `{"sni":""}`},
		{"/api/nodes/1/settings", `{"entryPort":8443}`},
		{"/api/nodes/1/remove", `{}`},
		{"/api/nodes/1/remove", `{"login":{"sshPort":22,"sshUser":"root","privateKey":"k","passphrase":"q","hostKey":"SHA256:x"}}`},
	} {
		method := "POST"
		if strings.HasSuffix(c.path, "settings") {
			method = "PATCH"
		}
		if code, _, body := r.page(method, c.path, c.body); code != 202 {
			t.Errorf("%s %s %q = %d %s", method, c.path, c.body, code, body)
		}
	}
	if code, _, body := r.page("POST", "/api/nodes", `{"password":"p","hostKey":"SHA256:x","name":"n","host":"h"}`); code != 202 || ops.install.EntryPort != 0 || ops.install.SSHPort != 0 {
		t.Errorf("an install with no ports = %d %s, %+v", code, body, ops.install)
	}
	if l := ops.removed.Login; l == nil || l.PrivateKey != "k" || l.Passphrase != "q" || l.Password != "" {
		t.Errorf("the last removal's login = %+v", l)
	}
	// a call that reads a body wants one, though every field in it may be left out
	if code, _, body := r.page("POST", "/api/nodes/1/remove", ""); code != 400 || !strings.Contains(string(body), "needs a JSON body") {
		t.Errorf("a removal with no body = %d %s", code, body)
	}
	// an empty SNI is one that was sent, which is not one left out
	if got := ops.calls[0]; got != "ChangeSettings" {
		t.Fatalf("calls %v", ops.calls)
	}
}

func TestEveryNodeCallIsForThePageOnly(t *testing.T) {
	for _, b := range []string{"", base} {
		r, ops := opsRig(t, b)
		host := strings.TrimPrefix(r.srv.URL, "http://")
		for _, prefix := range prefixes(b) {
			for _, c := range nodeCalls {
				path := prefix + c.path
				for what, h := range map[string]map[string]string{
					"no header":       nil,
					"a wrong header":  {UIHeader: "0"},
					"another origin":  {UIHeader: "1", "Origin": "https://evil.example"},
					"cross-site":      {UIHeader: "1", "Sec-Fetch-Site": "cross-site"},
					"same-site":       {UIHeader: "1", "Sec-Fetch-Site": "same-site"},
					"null origin":     {UIHeader: "1", "Origin": "null"},
					"origin by port":  {UIHeader: "1", "Origin": "http://127.0.0.1:1"},
					"header, no body": {"Origin": "http://" + host},
				} {
					code, _, body := r.send(c.method, path, c.body, h)
					got := decode[map[string]any](t, body)
					if code != http.StatusForbidden || got["code"] != "forbidden" || got["error"] == "" {
						t.Errorf("%s %s (%s) = %d %s, want 403 forbidden", c.method, path, what, code, body)
					}
				}
			}
		}
		if calls := ops.called(); len(calls) != 0 {
			t.Errorf("base %q: a call that wasn't from the page got through: %v", b, calls)
		}
	}
}

// With no manager behind them the endpoints are not there: a request is
// told what any path that isn't is told, once it has shown it comes from the page.
func TestWithoutNodeOpsTheEndpointsAreNotThere(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		r.populate()
		for _, prefix := range prefixes(b) {
			for _, c := range nodeCalls {
				code, _, body := r.page(c.method, prefix+c.path, c.body)
				got := decode[map[string]any](t, body)
				if code != http.StatusNotFound || got["code"] != "not_found" {
					t.Errorf("%s %s = %d %s, want 404 not_found", c.method, prefix+c.path, code, body)
				}
				if code, _, _ := r.send(c.method, prefix+c.path, c.body, nil); code != http.StatusForbidden {
					t.Errorf("%s %s without the header = %d, want 403", c.method, prefix+c.path, code)
				}
			}
		}
	}
}

func TestACallForANodeNobodyHasIsNotFoundWhateverElseIsWrong(t *testing.T) {
	r, ops := opsRig(t, "")
	for _, c := range nodeCalls {
		if !c.id {
			continue
		}
		for _, id := range []string{"99", "0", "abc", "-1", "1.5", "99999999999999999999", "%20"} {
			path := strings.Replace(c.path, "/nodes/1", "/nodes/"+id, 1)
			// with a body that is wrong too: the node is what is asked about first
			code, _, body := r.page(c.method, path, `{"nope": [`)
			got := decode[map[string]any](t, body)
			if code != http.StatusNotFound || got["code"] != "not_found" {
				t.Errorf("%s %s = %d %s", c.method, path, code, body)
			}
		}
	}
	if calls := ops.called(); len(calls) != 0 {
		t.Errorf("calls %v", calls)
	}
}

// A body that is not what the contract says is refused whole, before the
// manager hears of it, with a sentence that says what was wrong with its
// shape and nothing of what it held.
func TestARequestThatIsNotWhatTheContractSaysIsRefused(t *testing.T) {
	r, ops := opsRig(t, "")
	withBody := []string{"/api/probe", "/api/nodes", "/api/nodes/1", "/api/nodes/1/settings", "/api/nodes/1/handshake-check", "/api/nodes/1/update", "/api/nodes/1/remove"}
	methods := map[string]string{"/api/nodes/1": "PATCH", "/api/nodes/1/settings": "PATCH"}
	const shape = "The request body is not what this call takes."
	for _, path := range withBody {
		method := methods[path]
		if method == "" {
			method = "POST"
		}
		for name, c := range map[string]struct{ body, want string }{
			"a field the call doesn't take": {`{"unknownField":1}`, shape},
			"a field of another call":       {`{"host":"x","force":true,"nopeNope":1}`, shape},
			"a value of the wrong type":     {`{"name":5,"target":5,"kind":5,"host":5,"entryPort":"443","force":"yes"}`, shape},
			"JSON that stops short":         {`{"name":"x"`, shape},
			"not JSON":                      {`name=x`, shape},
			"two objects":                   {`{} {}`, shape},
			"something after the object":    {`{}x`, shape},
			"an array":                      {`[]`, shape},
			"a string":                      {`"x"`, shape},
			"nothing at all":                {``, "The request needs a JSON body."},
		} {
			code, _, body := r.page(method, path, c.body)
			got := decode[map[string]any](t, body)
			if code != http.StatusBadRequest || got["code"] != "bad_request" || got["error"] != c.want {
				t.Errorf("%s %s with %s (%s) = %d %s, want 400 bad_request saying %q", method, path, name, c.body, code, body, c.want)
			}
		}
	}
	if calls := ops.called(); len(calls) != 0 {
		t.Errorf("a refused request reached the manager: %v", calls)
	}
	// the calls that take no body read none, so what comes with one is not their business
	for _, path := range []string{"/api/nodes/1/check-updates", "/api/nodes/1/renew-cert", "/api/nodes/1/rotate-keys"} {
		for _, body := range []string{``, `{}`, `{"force":true}`} {
			if code, _, out := r.page("POST", path, body); code != http.StatusAccepted && code != http.StatusOK {
				t.Errorf("POST %s with %q = %d %s", path, body, code, out)
			}
		}
	}
}

// Nothing is repeated of a body that was refused, a login least of all.
func TestARefusedBodyIsNeverEchoed(t *testing.T) {
	r, ops := opsRig(t, "")
	body := `{"password":"MARKER-P4SS","privateKey":"MARKER-KEY","bogus":true,"hostKey":5}`
	code, _, out := r.page("POST", "/api/nodes", body)
	if code != 400 || bytes.Contains(out, []byte("MARKER")) {
		t.Errorf("answer = %d %s", code, out)
	}
	code, _, out = r.page("POST", "/api/nodes", `{"password":"MARKER-P4SS","privateKey":"MARKER-KEY"`)
	if code != 400 || bytes.Contains(out, []byte("MARKER")) {
		t.Errorf("answer = %d %s", code, out)
	}
	if strings.Contains(r.log.String(), "MARKER") || len(ops.called()) != 0 {
		t.Errorf("log:\n%s\ncalls %v", r.log, ops.called())
	}
}

func TestARequestOverSixtyFourKiBIsRefusedWhole(t *testing.T) {
	r, ops := opsRig(t, "")
	// a body of exactly the limit is taken, and one more byte is not
	pad := func(n int) string {
		b := `{"name":"Berlin"}`
		return b + strings.Repeat(" ", n-len(b))
	}
	if code, _, body := r.page("PATCH", "/api/nodes/1", pad(64<<10)); code != http.StatusOK {
		t.Fatalf("a body of 64 KiB = %d %s", code, body)
	}
	calls := len(ops.called())
	code, _, body := r.page("PATCH", "/api/nodes/1", pad(64<<10+1))
	got := decode[map[string]any](t, body)
	if code != http.StatusBadRequest || got["code"] != "bad_request" || got["error"] != "The request is too large." {
		t.Errorf("a body of 64 KiB and a byte = %d %s", code, body)
	}
	// the same for a login that carries a key that is far too long, which must not be repeated
	huge := `{"password":"p","hostKey":"SHA256:x","name":"n","host":"h","privateKey":"MARKER-` + strings.Repeat("A", 1<<20) + `"}`
	code, _, body = r.page("POST", "/api/nodes", huge)
	if code != http.StatusBadRequest || bytes.Contains(body, []byte("MARKER")) || !strings.Contains(string(body), "too large") {
		t.Errorf("a body of a megabyte = %d %.200s", code, body)
	}
	// one that says nothing of its length, which the reader has to stop
	code, _, body = r.send("PATCH", "/api/nodes/1", "", map[string]string{UIHeader: "1"})
	if code != http.StatusBadRequest {
		t.Errorf("an empty body = %d %s", code, body)
	}
	if len(ops.called()) != calls {
		t.Errorf("an oversize request reached the manager: %v", ops.called())
	}
}

// Every failure the manager can name is answered with the code and the status
// of the contract, and the page's switch on a missing login.
func TestTheManagersFailuresAreAnsweredWithTheContractsCodes(t *testing.T) {
	nodeLimit, _, taken, invalid := errorsFromTheModel(t)
	for _, c := range []struct {
		name  string
		err   error
		code  int
		kind  string
		login bool
	}{
		{"the limit", nodeLimit, 409, "node_limit", false},
		{"a name taken", taken, 409, "name_taken", false},
		{"a bad value", invalid, 400, "bad_request", false},
		{"no such node", fmt.Errorf("%w: node 1", model.ErrNotFound), 404, "not_found", false},
		{"a job on the node", &jobs.BusyError{NodeID: 1, JobID: "abc"}, 409, "busy", false},
		{"a login is needed", &Error{CodeNeedsLogin, "Send a login."}, 409, "needs_login", true},
		{"the node doesn't answer", &Error{CodeNodeUnreachable, "No answer."}, 502, "node_unreachable", false},
		{"a host key that changed", &Error{CodeHostKeyChanged, "Another key."}, 409, "host_key_changed", false},
		{"another Xray", &Error{CodeVersionMismatch, "Another Xray."}, 409, "version_mismatch", false},
		{"an agent that is too old", &Error{CodeAgentTooOld, "Too old."}, 409, "agent_too_old", false},
		{"a refusal of the hub's own", &Error{CodeInternal, "The hub can't use its CA."}, 500, "internal", false},
		{"something nobody foresaw", errors.New("open /data/state.json: permission denied"), 500, "internal", false},
	} {
		r, ops := opsRig(t, "")
		ops.setErr(c.err)
		for _, call := range nodeCalls {
			code, _, body := r.page(call.method, call.path, call.body)
			got := decode[map[string]any](t, body)
			if code != c.code || got["code"] != c.kind || got["error"] == "" {
				t.Errorf("%s: %s answered %d %s, want %d %s", c.name, call.name, code, body, c.code, c.kind)
			}
			if _, has := got["needLogin"]; has != c.login {
				t.Errorf("%s: %s: needLogin present %v, want %v (%s)", c.name, call.name, has, c.login, body)
			}
			if c.login && got["needLogin"] != true {
				t.Errorf("%s: %s: %s", c.name, call.name, body)
			}
		}
		// what is the hub's own is logged and not shown
		if c.kind == "internal" && errors.As(c.err, new(*Error)) == false {
			if strings.Contains(r.log.String(), "permission denied") == false {
				t.Errorf("%s: the log doesn't say what went wrong:\n%s", c.name, r.log)
			}
		}
	}
	// and the text of one the hub doesn't know is not what the page is told
	r, ops := opsRig(t, "")
	ops.setErr(errors.New("open /data/state.json: permission denied"))
	if _, _, body := r.page("POST", "/api/nodes/1/rotate-keys", ""); strings.Contains(string(body), "state.json") || strings.Contains(string(body), "permission") {
		t.Errorf("the answer shows the hub's own error: %s", body)
	}
}

// A login that comes back in an error's text, from whatever library said it, is
// taken out before the text is sent or logged, for an error the hub knows and
// one it doesn't.
func TestALoginInAnErrorIsTakenOutBeforeItIsSentOrLogged(t *testing.T) {
	const key = "-----BEGIN OPENSSH PRIVATE KEY-----\nMARKER-b3BlbnNzaC1rZXktdjEAAAAABG5vbmU\nMARKER-AAAAB3NzaC1yc2EAAAADAQABAAABgQDx\n-----END OPENSSH PRIVATE KEY-----\n"
	install := fmt.Sprintf(`{"sshPort":22,"sshUser":"root","password":"MARKER-hunter2","privateKey":%q,"passphrase":"MARKER-open sesame","hostKey":"SHA256:x","name":"n","host":"h"}`, key)
	remove := fmt.Sprintf(`{"login":{"sshPort":22,"sshUser":"root","password":"MARKER-hunter2","privateKey":%q,"passphrase":"MARKER-open sesame","hostKey":"SHA256:x"}}`, key)
	echo := "ssh: could not sign in with MARKER-hunter2, key " + key + " and passphrase MARKER-open sesame; line MARKER-AAAAB3NzaC1yc2EAAAADAQABAAABgQDx"
	for _, c := range []struct {
		name string
		path string
		body string
		err  error
		code int
		kind string
	}{
		{"an install, in an error the hub doesn't know", "/api/nodes", install, errors.New(echo), 500, "internal"},
		{"an install, in a sentence of its own", "/api/nodes", install, &Error{CodeNodeUnreachable, echo}, 502, "node_unreachable"},
		{"a removal, in an error the hub doesn't know", "/api/nodes/1/remove", remove, errors.New(echo), 500, "internal"},
		{"a removal, in a sentence of its own", "/api/nodes/1/remove", remove, &Error{CodeBadRequest, echo}, 400, "bad_request"},
	} {
		r, ops := opsRig(t, "")
		ops.setErr(c.err)
		code, _, body := r.page("POST", c.path, c.body)
		if code != c.code || !strings.Contains(string(body), `"code":"`+c.kind+`"`) {
			t.Errorf("%s: %d %s", c.name, code, body)
		}
		for _, secret := range []string{"MARKER-hunter2", "MARKER-open sesame", "MARKER-b3BlbnNz", "MARKER-AAAAB3Nza", "OPENSSH PRIVATE KEY"} {
			if bytes.Contains(body, []byte(secret)) || strings.Contains(r.log.String(), secret) {
				t.Errorf("%s: %q shows:\nanswer %s\nlog\n%s", c.name, secret, body, r.log)
			}
		}
		// what is left says what happened, with the secrets gone
		if c.kind != "internal" && !strings.Contains(string(body), "could not sign in") {
			t.Errorf("%s: the answer lost what was said: %s", c.name, body)
		}
		if c.kind == "internal" && !strings.Contains(r.log.String(), "could not sign in") {
			t.Errorf("%s: the log lost what was said:\n%s", c.name, r.log)
		}
		if !strings.Contains(string(body)+r.log.String(), "…") {
			t.Errorf("%s: nothing marks where a secret was taken out", c.name)
		}
	}
}

func TestHideInTakesSecretsOutInEveryFormTheyComeIn(t *testing.T) {
	key := "-----BEGIN KEY-----\nAAAA-line-one-of-the-key\nBBBB-line-two-of-the-key\n-----END KEY-----\n"
	err := hideIn(errors.New("a "+key+"b pw=hunter2 c AAAA-line-one-of-the-key d"), []string{"hunter2", key, "", "  "})
	for _, gone := range []string{"hunter2", "AAAA-line-one", "BBBB-line-two"} {
		if strings.Contains(err.Error(), gone) {
			t.Errorf("%q is still in %q", gone, err)
		}
	}
	if !strings.HasPrefix(err.Error(), "a ") || !strings.Contains(err.Error(), " c … d") {
		t.Errorf("err = %q", err)
	}
	// an error that says none of them is returned as it was, and a classified one keeps its code
	plain := errors.New("the disk is full")
	if got := hideIn(plain, []string{"hunter2"}); got != plain {
		t.Errorf("an error with no secret in it was changed to %v", got)
	}
	coded := hideIn(&Error{CodeBusy, "busy with hunter2"}, []string{"hunter2"})
	var e *Error
	if !errors.As(coded, &e) || e.Code != CodeBusy || e.Msg != "busy with …" {
		t.Errorf("coded = %#v", coded)
	}
	if hideIn(nil, []string{"x"}) != nil || hideIn(plain, nil) != plain {
		t.Error("nothing to hide")
	}
	// a secret that is a part of another doesn't leave the other in pieces
	if got := hideIn(errors.New("pass=abcdef"), []string{"abc", "abcdef"}); got.Error() != "pass=…" {
		t.Errorf("got %q", got)
	}
	// and one that is the ellipsis itself doesn't make it run on for ever
	if got := hideIn(errors.New("x…y"), []string{"…"}); got.Error() != "x…y" {
		t.Errorf("got %q", got)
	}
}

// Everything these calls answer, success or not, the page's or not, is free of
// what the hub holds and what the caller sent.
func TestNoAnswerOfANodeCallCarriesASecret(t *testing.T) {
	for _, b := range []string{"", base} {
		r, ops := opsRig(t, b)
		held := r.st.Read()
		if held.Nodes[0].RealityPrivateKey != privateKey || held.Nodes[0].SSHHostKey != hostKeyPin {
			t.Fatal("the test hub doesn't hold the secrets it is meant to hide")
		}
		all := append(secrets(), "MARKER-hunter2", "MARKER-KEYBODY", "MARKER-open")
		login := `"sshPort":22,"sshUser":"root","password":"MARKER-hunter2","privateKey":"MARKER-KEYBODY","passphrase":"MARKER-open","hostKey":"SHA256:x"`
		answers := map[string][]byte{}
		record := func(what string, body []byte) { answers[what] = body }
		for i, errToGive := range []error{nil, &Error{CodeNeedsLogin, "Send a login."}, errors.New("boom"), invalidErr()} {
			ops.setErr(errToGive)
			for _, c := range nodeCalls {
				body := c.body
				switch c.name {
				case "install":
					body = `{` + login + `,"name":"n","host":"h"}`
				case "remove":
					body = `{"login":{` + login + `}}`
				}
				_, _, out := r.page(c.method, b+c.path, body)
				record(fmt.Sprintf("%s #%d", c.name, i), out)
			}
		}
		// the refusals, which have the secret in the request and none in the answer
		for what, c := range map[string]struct{ method, path, body string }{
			"a bad node":       {"POST", "/api/nodes/99/remove", `{"login":{` + login + `}}`},
			"a field too many": {"POST", "/api/nodes", `{` + login + `,"name":"n","host":"h","extra":1}`},
			"a login, no JSON": {"POST", "/api/nodes", `{` + login},
			"no page header":   {"POST", "/api/nodes", `{` + login + `}`},
		} {
			h := map[string]string{UIHeader: "1"}
			if what == "no page header" {
				h = nil
			}
			_, _, out := r.send(c.method, b+c.path, c.body, h)
			record(what, out)
		}
		for what, body := range answers {
			for _, s := range all {
				if bytes.Contains(body, []byte(s)) {
					t.Errorf("base %q: %s carries %q: %s", b, what, s, body)
				}
			}
			var generic any
			if json.Unmarshal(body, &generic) == nil {
				walkKeys(generic, func(key string) {
					if secretish(key) {
						t.Errorf("base %q: %s has a key named %q", b, what, key)
					}
				})
			}
		}
		for _, s := range all {
			if strings.Contains(r.log.String(), s) {
				t.Errorf("base %q: the log carries %q:\n%s", b, s, r.log)
			}
		}
	}
}

func invalidErr() error { _, err := model.CleanName(""); return err }
