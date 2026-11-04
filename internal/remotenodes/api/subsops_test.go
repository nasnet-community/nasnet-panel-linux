// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// subsUI is what the page sends with every call.
var subsUI = map[string]string{UIHeader: "1"}

// fakeSubs stands in for the manager. It records every call as text and
// answers with views and links of its own, or with the one error the test set.
type fakeSubs struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeSubs) note(format string, a ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
	return f.err
}

func (f *fakeSubs) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeSubs) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func viewOf(id uint, name string, on bool) Subscription {
	return Subscription{ID: id, Name: name, CreatedAt: now, Nodes: []SubscriptionNode{{NodeID: 1, Enabled: on}}}
}

func (f *fakeSubs) CreateSubscription(_ context.Context, name string, nodeIDs []uint, limits SubscriptionLimits) (Subscription, error) {
	note := fmt.Sprintf("create %q %v", name, nodeIDs)
	if limits != (SubscriptionLimits{}) {
		note += fmt.Sprintf(" limit=%d expires=%v", limits.TrafficLimit, !limits.ExpiresAt.IsZero())
	}
	if err := f.note("%s", note); err != nil {
		return Subscription{}, err
	}
	return viewOf(7, name, len(nodeIDs) > 0), nil
}

func (f *fakeSubs) UpdateSubscription(_ context.Context, id uint, patch SubscriptionPatch) (Subscription, error) {
	name := ""
	if patch.Name != nil {
		name = *patch.Name
	}
	note := fmt.Sprintf("update %d name=%q limit=%v/%v/%d expires=%v/%v/%s", id, name, patch.TrafficLimit.Set, patch.TrafficLimit.Null, patch.TrafficLimit.Value, patch.ExpiresAt.Set, patch.ExpiresAt.Null, patch.ExpiresAt.Value.UTC().Format(time.RFC3339))
	if patch.Name != nil && !patch.TrafficLimit.Set && !patch.ExpiresAt.Set {
		note = fmt.Sprintf("rename %d %q", id, name)
	}
	if err := f.note("%s", note); err != nil {
		return Subscription{}, err
	}
	return viewOf(id, name, true), nil
}

func (f *fakeSubs) ResetUsage(_ context.Context, id uint) (Subscription, error) {
	if err := f.note("reset %d", id); err != nil {
		return Subscription{}, err
	}
	return viewOf(id, "Sara", true), nil
}

func (f *fakeSubs) DeleteSubscription(_ context.Context, id uint) error {
	return f.note("delete %d", id)
}

func (f *fakeSubs) SetEnabled(_ context.Context, sub, node uint, on bool) (Subscription, error) {
	if err := f.note("switch %d %d %v", sub, node, on); err != nil {
		return Subscription{}, err
	}
	return viewOf(sub, "Sara", on), nil
}

func linkWith(node uint, id string) Link {
	return Link{NodeID: node, NodeName: "تهران", Vless: fmt.Sprintf("vless://%s@203.0.113.1:443?security=reality#%%D8%%B3%%D8%%A7", id)}
}

func (f *fakeSubs) Link(sub, node uint) (Link, error) {
	if err := f.note("link %d %d", sub, node); err != nil {
		return Link{}, err
	}
	return linkWith(node, uuidA), nil
}

func (f *fakeSubs) ReplaceLink(_ context.Context, sub, node uint) (Link, error) {
	if err := f.note("replace %d %d", sub, node); err != nil {
		return Link{}, err
	}
	return linkWith(node, uuidB), nil
}

func withSubs(s SubscriptionOps) func(*Config, *jobs.Engine) {
	return func(c *Config, _ *jobs.Engine) { c.Subs = s }
}

// subsCall makes a request with exactly the headers and body given. A body
// that is chunked has no length up front, as one from a client that streams.
func (r *rig) subsCall(method, path string, headers map[string]string, body []byte, chunked bool) (int, http.Header, []byte) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
		if chunked {
			rd = io.MultiReader(rd) // NewRequest can't tell how long it is
		}
	}
	req, err := http.NewRequest(method, r.srv.URL+path, rd)
	if err != nil {
		r.t.Fatal(err)
	}
	if chunked {
		req.ContentLength = -1
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

// do is a call as the page makes it: with its header, and a body if there is one.
func (r *rig) do(method, path, body string) (int, http.Header, []byte) {
	r.t.Helper()
	var b []byte
	if body != "" {
		b = []byte(body)
	}
	return r.subsCall(method, path, subsUI, b, false)
}

func wantError(t *testing.T, what string, code int, body []byte, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	got := decode[map[string]any](t, body)
	if code != wantStatus || got["code"] != wantCode || got["error"] == nil || got["error"] == "" {
		t.Errorf("%s = %d %s, want %d %s", what, code, body, wantStatus, wantCode)
	}
	return got
}

type subsReq struct{ method, path, body string }

// subsRoutes is every call this file adds, for subscription 3 and node 2.
func subsRoutes() []subsReq {
	return []subsReq{
		{"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[1,2]}`},
		{"PATCH", "/api/subscriptions/3", `{"name":"Sara"}`},
		{"DELETE", "/api/subscriptions/3", ""},
		{"PUT", "/api/subscriptions/3/nodes/2", `{"enabled":true}`},
		{"GET", "/api/subscriptions/3/nodes/2/link", ""},
		{"POST", "/api/subscriptions/3/nodes/2/replace-link", ""},
	}
}

// Without something to carry them out the routes are not there, and the page's
// header is still asked for before it is told so.
func TestWithoutSubscriptionOpsTheRoutesAreNotThere(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		for _, rt := range subsRoutes() {
			for _, prefix := range prefixes(b) {
				code, _, body := r.do(rt.method, prefix+rt.path, rt.body)
				wantError(t, rt.method+" "+prefix+rt.path, code, body, http.StatusNotFound, "not_found")
				code, _, body = r.subsCall(rt.method, prefix+rt.path, nil, []byte(rt.body), false)
				wantError(t, rt.method+" "+prefix+rt.path+" without the header", code, body, http.StatusForbidden, "forbidden")
			}
		}
	}
}

func TestEverySubscriptionCallNeedsThePagesHeaderAndSite(t *testing.T) {
	fake := &fakeSubs{}
	wantCalls := 0
	for _, b := range []string{"", base} {
		r := newRigWith(t, b, withSubs(fake))
		host := strings.TrimPrefix(r.srv.URL, "http://")
		for _, rt := range subsRoutes() {
			for _, prefix := range prefixes(b) {
				for name, hdr := range map[string]map[string]string{
					"no header":              nil,
					"an empty header":        {UIHeader: ""},
					"a header that is not 1": {UIHeader: "yes"},
				} {
					code, _, body := r.subsCall(rt.method, prefix+rt.path, hdr, []byte(rt.body), false)
					wantError(t, rt.method+" "+prefix+rt.path+" with "+name, code, body, http.StatusForbidden, "forbidden")
				}
				if rt.method == "GET" {
					continue
				}
				// a call that changes something has to come from this site as well
				for name, from := range map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"} {
					hdr := map[string]string{UIHeader: "1", name: from}
					code, _, body := r.subsCall(rt.method, prefix+rt.path, hdr, []byte(rt.body), false)
					wantError(t, rt.method+" "+prefix+rt.path+" from "+name+" "+from, code, body, http.StatusForbidden, "forbidden")
				}
				hdr := map[string]string{UIHeader: "1", "Origin": "http://" + host}
				if code, _, body := r.subsCall(rt.method, prefix+rt.path, hdr, []byte(rt.body), false); code >= 400 {
					t.Errorf("%s %s from its own site = %d %s", rt.method, prefix+rt.path, code, body)
				}
				wantCalls++
			}
		}
	}
	// every refusal stopped short of the manager, and only the calls from this site went through
	if got := fake.called(); len(got) != wantCalls {
		t.Errorf("the manager was asked %d times, want %d: %v", len(got), wantCalls, got)
	}
}

func TestTheSubscriptionCallsPassWhatThePageSentAndAnswerWithWhatTheyGot(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := &fakeSubs{}
		r := newRigWith(t, b, withSubs(fake))
		var want []string
		for _, prefix := range prefixes(b) {
			p := prefix + "/api/subscriptions"

			code, hdr, body := r.do("POST", p, `{"name":"علی","nodeIds":[2,1]}`)
			if code != http.StatusCreated || !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
				t.Fatalf("POST %s = %d %q %s", p, code, hdr.Get("Content-Type"), body)
			}
			if v := decode[Subscription](t, body); v.ID != 7 || v.Name != "علی" || len(v.Nodes) != 1 || !v.Nodes[0].Enabled {
				t.Errorf("created view = %+v", v)
			}
			// a subscription on no node at all is asked for with an empty list
			if code, _, body := r.do("POST", p, `{"name":"Sara","nodeIds":[]}`); code != http.StatusCreated {
				t.Errorf("an empty node list = %d %s", code, body)
			}

			code, _, body = r.do("PATCH", p+"/3", `{"name":" Sara "}`)
			if v := decode[Subscription](t, body); code != http.StatusOK || v.ID != 3 || v.Name != " Sara " {
				t.Errorf("PATCH = %d %s", code, body)
			}

			code, _, body = r.do("DELETE", p+"/3", "")
			if got := string(body); code != http.StatusOK || got != `{"ok":true}`+"\n" {
				t.Errorf("DELETE = %d %q", code, got)
			}

			code, _, body = r.do("PUT", p+"/3/nodes/2", `{"enabled":false}`)
			if v := decode[Subscription](t, body); code != http.StatusOK || v.ID != 3 || v.Nodes[0].Enabled {
				t.Errorf("PUT off = %d %s", code, body)
			}
			code, _, body = r.do("PUT", p+"/3/nodes/2", `{"enabled":true}`)
			if v := decode[Subscription](t, body); code != http.StatusOK || !v.Nodes[0].Enabled {
				t.Errorf("PUT on = %d %s", code, body)
			}

			code, _, body = r.do("GET", p+"/3/nodes/2/link", "")
			l := decode[Link](t, body)
			if code != http.StatusOK || l.NodeID != 2 || l.NodeName != "تهران" || !strings.Contains(l.Vless, uuidA) {
				t.Errorf("GET link = %d %s", code, body)
			}
			code, _, body = r.do("POST", p+"/3/nodes/2/replace-link", "")
			l = decode[Link](t, body)
			if code != http.StatusOK || l.NodeID != 2 || !strings.Contains(l.Vless, uuidB) {
				t.Errorf("POST replace-link = %d %s", code, body)
			}
			want = append(want, `create "علی" [2 1]`, `create "Sara" []`, `rename 3 " Sara "`, "delete 3",
				"switch 3 2 false", "switch 3 2 true", "link 3 2", "replace 3 2")
		}
		if got := fake.called(); !slices.Equal(got, want) {
			t.Errorf("base %q: the manager was asked\n%s\nwant\n%s", b, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// Whatever else is wrong with a body, nothing reaches the manager, and what
// was sent is not sent back.
func TestABodyThatIsNotWhatTheCallTakesIsRefusedBeforeTheManager(t *testing.T) {
	fake := &fakeSubs{}
	r := newRigWith(t, "", withSubs(fake))
	for name, c := range map[string]subsReq{
		"create: no body":                 {"POST", "/api/subscriptions", ""},
		"create: an empty object":         {"POST", "/api/subscriptions", `{}`},
		"create: no node list":            {"POST", "/api/subscriptions", `{"name":"Sara"}`},
		"create: a null node list":        {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":null}`},
		"create: nodes as a string":       {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":"1"}`},
		"create: node ids as strings":     {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":["1"]}`},
		"create: a negative node id":      {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[-1]}`},
		"create: a fractional node id":    {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[1.5]}`},
		"create: a name that is a number": {"POST", "/api/subscriptions", `{"name":5,"nodeIds":[]}`},
		"create: a field it doesn't take": {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[],"admin":true}`},
		"create: two values":              {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[]}{"name":"Ali","nodeIds":[]}`},
		"create: junk after the value":    {"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[]} x`},
		"create: a list for an object":    {"POST", "/api/subscriptions", `[{"name":"Sara","nodeIds":[]}]`},
		"create: cut off":                 {"POST", "/api/subscriptions", `{"name":"Sara","nodeI`},
		"create: not JSON":                {"POST", "/api/subscriptions", `name=Sara`},
		"rename: no body":                 {"PATCH", "/api/subscriptions/3", ""},
		"rename: a field it doesn't take": {"PATCH", "/api/subscriptions/3", `{"name":"Sara","id":9}`},
		"rename: a name that is a list":   {"PATCH", "/api/subscriptions/3", `{"name":["Sara"]}`},
		"switch: no body":                 {"PUT", "/api/subscriptions/3/nodes/2", ""},
		"switch: no enabled":              {"PUT", "/api/subscriptions/3/nodes/2", `{}`},
		"switch: a null enabled":          {"PUT", "/api/subscriptions/3/nodes/2", `{"enabled":null}`},
		"switch: a string enabled":        {"PUT", "/api/subscriptions/3/nodes/2", `{"enabled":"true"}`},
		"switch: a number enabled":        {"PUT", "/api/subscriptions/3/nodes/2", `{"enabled":1}`},
		"switch: another field too":       {"PUT", "/api/subscriptions/3/nodes/2", `{"enabled":true,"uuid":"x"}`},
	} {
		code, _, body := r.do(c.method, c.path, c.body)
		got := wantError(t, name, code, body, http.StatusBadRequest, "bad_request")
		for _, sent := range []string{"Sara", "admin", "uuid", "Ali"} {
			if strings.Contains(c.body, sent) && strings.Contains(fmt.Sprint(got["error"]), sent) {
				t.Errorf("%s: the error repeats %q from the body: %v", name, sent, got["error"])
			}
		}
	}
	if got := fake.called(); len(got) != 0 {
		t.Errorf("the manager was asked %v", got)
	}
}

func TestTheBodyLimitIs64KiBWhetherOrNotTheLengthIsKnown(t *testing.T) {
	fake := &fakeSubs{}
	r := newRigWith(t, "", withSubs(fake))
	// a body of exactly the limit is taken, and one byte more is not
	const frame = `{"name":"","nodeIds":[]}`
	exact := `{"name":"` + strings.Repeat("a", maxSmallBody-len(frame)) + `","nodeIds":[]}`
	if len(exact) != maxSmallBody {
		t.Fatalf("test body is %d bytes", len(exact))
	}
	over := `{"name":"` + strings.Repeat("a", maxSmallBody-len(frame)+1) + `","nodeIds":[]}`
	for _, chunked := range []bool{false, true} {
		code, _, body := r.subsCall("POST", "/api/subscriptions", subsUI, []byte(exact), chunked)
		if code != http.StatusCreated {
			t.Errorf("chunked=%v: a body of %d bytes = %d %.80s", chunked, len(exact), code, body)
		}
		code, _, body = r.subsCall("POST", "/api/subscriptions", subsUI, []byte(over), chunked)
		got := wantError(t, fmt.Sprint("chunked=", chunked, ": a body one byte over"), code, body, http.StatusBadRequest, "bad_request")
		if !strings.Contains(fmt.Sprint(got["error"]), "too large") {
			t.Errorf("chunked=%v: the reason is %v", chunked, got["error"])
		}
	}
	if got := fake.called(); len(got) != 2 {
		t.Errorf("the manager was asked %d times, want only for the two that fit", len(got))
	}
}

// announceTooMuch sends the head of a request whose Content-Length is over the
// limit, and the start of its body, and then nothing: the answer has to come
// without the rest.
func announceTooMuch(t *testing.T, r *rig, path string, length int) (int, []byte) {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(r.srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: hub\r\n%s: 1\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n{\"", path, UIHeader, length)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no answer to a body that announced %d bytes and never sent them: %v", length, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// A body that says it is over the limit is refused as soon as its head is
// read, so it can't hold a connection open for a body that never comes.
func TestABodyThatAnnouncesTooMuchIsRefusedWithoutWaitingForIt(t *testing.T) {
	fake := &fakeSubs{}
	r := newRigWith(t, "", withSubs(fake))
	code, body := announceTooMuch(t, r, "/api/subscriptions", maxSmallBody+1)
	got := wantError(t, "an announced body over the limit", code, body, http.StatusBadRequest, "bad_request")
	if !strings.Contains(fmt.Sprint(got["error"]), "too large") {
		t.Errorf("the reason is %v", got["error"])
	}
	if len(fake.called()) != 0 {
		t.Error("the manager was asked")
	}
}

// An id that isn't a number names nothing, as for a node.
func TestIdsInAPathAreChecked(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := &fakeSubs{}
		r := newRigWith(t, b, withSubs(fake))
		for _, bad := range []string{"abc", "0", "-1", "1.5", "99999999999999", "1e3", "%20", "0x2", "٣"} {
			for _, rt := range []subsReq{
				{"PATCH", "/api/subscriptions/" + bad, `{"name":"Sara"}`},
				{"DELETE", "/api/subscriptions/" + bad, ""},
				{"PUT", "/api/subscriptions/" + bad + "/nodes/2", `{"enabled":true}`},
				{"PUT", "/api/subscriptions/3/nodes/" + bad, `{"enabled":true}`},
				{"GET", "/api/subscriptions/" + bad + "/nodes/2/link", ""},
				{"GET", "/api/subscriptions/3/nodes/" + bad + "/link", ""},
				{"POST", "/api/subscriptions/" + bad + "/nodes/2/replace-link", ""},
				{"POST", "/api/subscriptions/3/nodes/" + bad + "/replace-link", ""},
			} {
				code, _, body := r.do(rt.method, b+rt.path, rt.body)
				got := wantError(t, rt.method+" "+b+rt.path, code, body, http.StatusNotFound, "not_found")
				if len(got) != 2 {
					t.Errorf("%s %s: the body is %s", rt.method, rt.path, body)
				}
			}
		}
		if got := fake.called(); len(got) != 0 {
			t.Errorf("an id that is not one reached the manager: %v", got)
		}
	}
}

func TestTheManagersRefusalsKeepTheContractsCodes(t *testing.T) {
	_, subLimit, taken, invalid := errorsFromTheModel(t)
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"the limit", subLimit, 409, CodeSubscriptionLimit},
		{"a name that is taken", taken, 409, CodeNameTaken},
		{"a name that is no good", invalid, 400, CodeBadRequest},
		{"nobody by that id", fmt.Errorf("%w: subscription 3", model.ErrNotFound), 404, CodeNotFound},
		{"a node being installed", &jobs.BusyError{NodeID: 2, JobID: "job-9"}, 409, CodeBusy},
		{"a plain sentence of its own", &Error{CodeNotFound, "This subscription is switched off on that node, so it has no link there."}, 404, CodeNotFound},
		{"a node that can't be asked", &Error{CodeNodeUnreachable, "The node can't be reached, so its link can't be replaced now."}, 502, CodeNodeUnreachable},
		{"the hub's own fault", errors.New("disk exploded: /data/state.json"), 500, CodeInternal},
	}
	for _, c := range cases {
		fake := &fakeSubs{err: c.err}
		r := newRigWith(t, "", withSubs(fake))
		for _, rt := range subsRoutes() {
			code, _, body := r.do(rt.method, rt.path, rt.body)
			got := wantError(t, c.name+": "+rt.method+" "+rt.path, code, body, c.status, c.code)
			if c.code == CodeInternal && strings.Contains(string(body), "exploded") {
				t.Errorf("an error of the hub's own is shown to the page: %s", body)
			}
			if c.name == "a plain sentence of its own" && !strings.Contains(fmt.Sprint(got["error"]), "switched off") {
				t.Errorf("the sentence was lost: %s", body)
			}
		}
		if c.code == CodeInternal && !strings.Contains(r.log.String(), "exploded") {
			t.Error("the hub's own fault was not logged")
		}
	}
}

// A request that went away while the manager waited gets nothing, and the hub
// logs no failure for it.
func TestARequestThatWentAwayIsNotAFailure(t *testing.T) {
	fake := &fakeSubs{err: context.Canceled}
	r := newRigWith(t, "", withSubs(fake))
	var logged syncBuf
	h := New(Config{Store: r.st, Subs: fake, Log: slog.New(slog.NewTextHandler(&logged, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, rt := range []subsReq{
		{"POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[1]}`},
		{"PUT", "/api/subscriptions/3/nodes/2", `{"enabled":true}`},
		{"POST", "/api/subscriptions/3/nodes/2/replace-link", ""},
	} {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body)).WithContext(ctx)
		req.Header.Set(UIHeader, "1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Body.Len() != 0 {
			t.Errorf("%s %s answered %q", rt.method, rt.path, rec.Body.String())
		}
	}
	if logged.String() != "" {
		t.Errorf("a request that went away was logged:\n%s", logged.String())
	}
	// the same error with the request still there is the hub's own fault
	code, _, body := r.do("POST", "/api/subscriptions", `{"name":"Sara","nodeIds":[1]}`)
	wantError(t, "a cancellation nobody asked for", code, body, http.StatusInternalServerError, CodeInternal)
}

// The links are the one answer that holds a secret. They are never cached or
// sniffed, and nothing is logged of them, whether the call worked or not.
func TestALinkIsNeverCachedOrLogged(t *testing.T) {
	fake := &fakeSubs{}
	r := newRigWith(t, "", withSubs(fake))
	for _, rt := range []subsReq{
		{"GET", "/api/subscriptions/3/nodes/2/link", ""},
		{"POST", "/api/subscriptions/3/nodes/2/replace-link", ""},
	} {
		code, hdr, body := r.do(rt.method, rt.path, rt.body)
		if code != http.StatusOK || hdr.Get("Cache-Control") != "no-store" || hdr.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
			t.Errorf("%s %s = %d with Cache-Control %q, nosniff %q, type %q", rt.method, rt.path, code, hdr.Get("Cache-Control"), hdr.Get("X-Content-Type-Options"), hdr.Get("Content-Type"))
		}
		if !bytes.Contains(body, []byte("vless://")) {
			t.Errorf("%s %s gave no link: %s", rt.method, rt.path, body)
		}
	}
	// a failure while a link is being made is logged without it
	fake.fail(errors.New("the hub is broken"))
	r.do("GET", "/api/subscriptions/3/nodes/2/link", "")
	r.do("POST", "/api/subscriptions/3/nodes/2/replace-link", "")
	if !strings.Contains(r.log.String(), "the hub is broken") {
		t.Error("the failure was not logged")
	}
	for _, s := range []string{uuidA, uuidB, "vless://", "security=reality"} {
		if strings.Contains(r.log.String(), s) {
			t.Errorf("the log has %q in it:\n%s", s, r.log.String())
		}
	}
}

// subscriptionBodies are the answers of every subscription call that is not a
// link, to calls that work, to calls the manager refuses with errors that
// hold a secret, and to one that is not from the page. The two that are links
// carry a client id on purpose.
func subscriptionBodies(r *rig, fake *fakeSubs, b string) map[string][]byte {
	bodies := map[string][]byte{}
	record := func(what string, code int, body []byte) { bodies[fmt.Sprint(b, " ", what, " ", code)] = body }
	call := func(what string) {
		for _, rt := range subsRoutes() {
			if strings.HasSuffix(rt.path, "/link") || strings.HasSuffix(rt.path, "/replace-link") {
				continue
			}
			code, _, body := r.do(rt.method, b+rt.path, rt.body)
			record(rt.method+" "+rt.path+what, code, body)
		}
	}
	fake.fail(nil)
	call("")
	for _, bad := range []error{errors.New("boom " + privateKey + " " + uuidA), model.ErrNotFound, model.ErrNameTaken} {
		fake.fail(bad)
		call(" failing")
	}
	fake.fail(nil)
	code, _, body := r.subsCall("POST", b+"/api/subscriptions", nil, []byte(`{"name":"x","nodeIds":[]}`), false)
	record("without the header", code, body)
	return bodies
}

func TestNoSecretReachesABrowserFromTheSubscriptionCalls(t *testing.T) {
	for _, b := range []string{"", base} {
		fake := &fakeSubs{}
		r := newRigWith(t, b, withSubs(fake))
		r.populate() // the hub holds every kind of secret the walk looks for
		bodies := subscriptionBodies(r, fake, b)
		if len(bodies) < 16 {
			t.Errorf("only %d bodies were looked at", len(bodies))
		}
		for what, body := range bodies {
			for _, s := range secrets() {
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
