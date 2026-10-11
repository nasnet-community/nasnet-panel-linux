package mtprotonode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	"github.com/nasnet-community/mtproto-remote-plugin/hubnode"
	"github.com/nasnet-community/mtproto-remote-plugin/link"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops/relaytest"
	"github.com/nasnet-community/mtproto-remote-plugin/release"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
)

type apiRig struct {
	*relaytest.Rig
	h    *Handler
	r    *gin.Engine
	live *fakeLive
}

// fakeLive reports what the shared fake link says, in the manager's shape.
type fakeLive struct {
	rig     *relaytest.Rig
	snap    *hubnode.Snapshot // when set, the view's snapshot
	extra   View              // what the manager measures itself: users, throughput
	created time.Time
	feed    feed
}

func (f *fakeLive) View(id uint) (View, bool) {
	v, ok := f.rig.Links.View(id)
	if !ok {
		return View{}, false
	}
	out := f.extra
	if f.snap != nil {
		out.Snapshot = *f.snap
	}
	if v.Up && out.Snapshot.Link.Up == 0 {
		out.Snapshot.Link.Up = 1
	}
	return out, true
}

func (f *fakeLive) Domestic(addrs []string) *bool {
	return domestic(PrefixRanges([]string{"192.0.2.0/24"}), addrs)
}

func (f *fakeLive) Call(ctx context.Context, id uint, m string, req, resp any) error {
	return f.rig.Links.Call(ctx, id, m, req, resp)
}

func (f *fakeLive) Row(ctx context.Context, id uint) (*LiteNode, error) {
	r, err := f.rig.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var l LiteNode
	l.apply(*r)
	l.CreatedAt = f.created
	return &l, nil
}

func (f *fakeLive) Rows(ctx context.Context) ([]*LiteNode, error) {
	recs, err := f.rig.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*LiteNode, len(recs))
	for i, r := range recs {
		out[i] = &LiteNode{}
		out[i].apply(*r)
	}
	return out, nil
}

func (f *fakeLive) UpdateSettings(ctx context.Context, id uint, s Settings) error {
	r, err := f.rig.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.MaxStreams != nil {
		r.MaxStreams = *s.MaxStreams
	}
	if s.SSHPort != nil {
		r.SSHPort = *s.SSHPort
	}
	if s.SSHUser != nil {
		r.SSHUser = *s.SSHUser
	}
	return f.rig.Store.Save(ctx, r)
}

func (f *fakeLive) Events(_ uint, limit int) EventFeed {
	return EventFeed{Since: f.created, Events: f.feed.list(limit)}
}

func (f *fakeLive) Record(_ uint, ev Event) { f.feed.add(ev) }

func newAPI(t *testing.T) *apiRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rig := relaytest.NewRig()
	allow := &tgnet.Refresher{List: tgnet.New(nil)}
	if p, err := tgnet.PinnedPrefixes(); err == nil {
		allow.List = tgnet.New(p)
	}
	live := &fakeLive{rig: rig, created: time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)}
	h := NewHandler(rig.Svc, live, allow)
	e := gin.New()
	h.RegisterRoutes(e.Group("/api/v1"))
	return &apiRig{Rig: rig, h: h, r: e, live: live}
}

func (a *apiRig) do(method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.r.ServeHTTP(w, req)
	return w
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return e
}

func installBodyJSON() map[string]any {
	return map[string]any{
		"name": "VPS 1", "host": "203.0.113.9", "ssh_user": "root", "ssh_password": "hunter2-super-secret",
		"host_key": "SHA256:abc", "disguise": "mirror.example.ir",
	}
}

func (a *apiRig) install(t *testing.T) string {
	t.Helper()
	a.Links.SetUp(1, true)
	w := a.do("POST", "/mtproto/nodes", installBodyJSON())
	if w.Code != http.StatusAccepted {
		t.Fatalf("install: %d %s", w.Code, w.Body)
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	json.Unmarshal(decode(t, w).Data, &out)
	if v := a.Wait(t, out.JobID); v.State != relayops.JobOK {
		t.Fatalf("job = %+v", v)
	}
	return out.JobID
}

func TestProbeReturnsTheFingerprintAndRejectsNonsense(t *testing.T) {
	a := newAPI(t)
	w := a.do("POST", "/mtproto/probe", map[string]any{"host": "203.0.113.9"})
	var out struct{ Fingerprint string }
	json.Unmarshal(decode(t, w).Data, &out)
	if w.Code != 200 || out.Fingerprint != "SHA256:abc" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := a.do("POST", "/mtproto/probe", map[string]any{"host": "a b;c"}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad host: %d", w.Code)
	}
}

func TestInstallThroughTheAPIAndNoSecretInAnyResponse(t *testing.T) {
	a := newAPI(t)
	jid := a.install(t)

	var bodies []string
	for _, path := range []string{"/mtproto/jobs/" + jid, "/mtproto/nodes/1", "/mtproto/allowlist"} {
		w := a.do("GET", path, nil)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		bodies = append(bodies, w.Body.String())
	}
	row, _ := a.Store.Get(context.Background(), 1)
	for _, b := range bodies {
		for _, secret := range []string{"hunter2-super-secret", row.Token, row.RealityPub, row.ShortID} {
			if strings.Contains(b, secret) {
				t.Fatalf("a secret leaked into %s", b)
			}
		}
	}
	var v NodeView
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1", nil)).Data, &v)
	if v.NodeID != 1 || v.Disguise != "mirror.example.ir" || v.HubPort != 8443 || v.Arch != "amd64" || !v.Running || v.Link == nil || v.Link.Up != 1 {
		t.Fatalf("view = %+v", v)
	}
}

// A bad cover site comes back with the request and its test; a failed login says why.
func TestABadCoverSiteIsRefusedBeforeTheInstall(t *testing.T) {
	a := newAPI(t)
	a.SSH.Cover = &ctrl.DisguiseResult{TLS13: true, X25519: true, Addrs: []string{"192.0.2.7"}, Err: "the site does not offer h2"}
	w := a.do("POST", "/mtproto/nodes", installBodyJSON())
	e := decode(t, w)
	var out struct {
		Cover ctrl.DisguiseResult `json:"cover"`
	}
	json.Unmarshal(e.Data, &out)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(e.Error, "mirror.example.ir does not work as a cover site") ||
		out.Cover.OK || !out.Cover.TLS13 || out.Cover.H2 || out.Cover.Err != "the site does not offer h2" || len(out.Cover.Addrs) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(a.SSH.Installed) != 0 || strings.Contains(w.Body.String(), "hunter2-super-secret") {
		t.Fatalf("installs = %d, body %s", len(a.SSH.Installed), w.Body)
	}

	a.SSH.Cover = nil
	a.SSH.CoverErr = errors.New("installer: login: ssh: unable to authenticate")
	w = a.do("POST", "/mtproto/nodes", installBodyJSON())
	if w.Code != http.StatusBadGateway || !strings.Contains(decode(t, w).Error, "unable to authenticate") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestInstallValidationAndUnknownThings(t *testing.T) {
	a := newAPI(t)
	bad := installBodyJSON()
	bad["disguise"] = "not a domain"
	w := a.do("POST", "/mtproto/nodes", bad)
	if w.Code != http.StatusBadRequest || !strings.Contains(decode(t, w).Error, "domain") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := a.do("POST", "/mtproto/nodes", "not an object"); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: %d", w.Code)
	}
	for _, path := range []string{"/mtproto/jobs/nope", "/mtproto/nodes/99"} {
		if w := a.do("GET", path, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	if w := a.do("GET", "/mtproto/nodes/abc", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", w.Code)
	}
}

func TestStreamEndsWithTheFinalState(t *testing.T) {
	a := newAPI(t)
	jid := a.install(t)
	w := a.do("GET", "/mtproto/jobs/"+jid+"/stream", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(body, "event: progress") || !strings.Contains(body, `"state":"ok"`) {
		t.Fatalf("%d %q", w.Code, body)
	}
	if w := a.do("GET", "/mtproto/jobs/nope/stream", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown job stream: %d", w.Code)
	}
}

func TestDeleteNeedsALoginWhenTheLinkIsDown(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	a.Links.SetUp(1, false)
	if w := a.do("POST", "/mtproto/nodes/1/remove", map[string]any{}); w.Code != http.StatusConflict {
		t.Fatalf("no login: %d %s", w.Code, w.Body)
	}
	w := a.do("POST", "/mtproto/nodes/1/remove", map[string]any{"force": true})
	if w.Code != http.StatusAccepted {
		t.Fatalf("force: %d %s", w.Code, w.Body)
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	json.Unmarshal(decode(t, w).Data, &out)
	if v := a.Wait(t, out.JobID); v.State != relayops.JobOK {
		t.Fatalf("job = %+v", v)
	}
}

func TestOperationsRefuseWhenTheyCannotRun(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	if w := a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "kernel"}); w.Code != http.StatusBadRequest {
		t.Errorf("bad kind: %d", w.Code)
	}
	a.Links.SetUp(1, false)
	if w := a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "agent"}); w.Code != http.StatusConflict {
		t.Errorf("link down update: %d", w.Code)
	}
	if w := a.do("POST", "/mtproto/nodes/1/rotate", nil); w.Code != http.StatusConflict {
		t.Errorf("link down rotate: %d", w.Code)
	}
}

func TestControlPassThroughs(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	a.Links.Mu.Lock()
	a.Links.Respond = func(m string, resp any) {
		switch m {
		case ctrl.MLogsTail:
			*(resp.(*ctrl.LogsResponse)) = ctrl.LogsResponse{Lines: []string{"client [ip] connected"}}
		case ctrl.MDisguiseCheck:
			*(resp.(*ctrl.DisguiseResult)) = ctrl.DisguiseResult{OK: true, TLS13: true, X25519: true, H2: true, Addrs: []string{"192.0.2.4", "198.51.100.7"}}
		}
	}
	a.Links.Mu.Unlock()

	w := a.do("GET", "/mtproto/nodes/1/logs?lines=50", nil)
	var logs ctrl.LogsResponse
	json.Unmarshal(decode(t, w).Data, &logs)
	if w.Code != 200 || len(logs.Lines) != 1 {
		t.Fatalf("logs: %d %s", w.Code, w.Body)
	}
	w = a.do("POST", "/mtproto/nodes/1/disguise-check", map[string]any{"domain": "other.example.ir"})
	var res ctrl.DisguiseResult
	json.Unmarshal(decode(t, w).Data, &res)
	if w.Code != 200 || !res.OK {
		t.Fatalf("check: %d %s", w.Code, w.Body)
	}
	// one address outside the local ranges: a warning, the check still passes
	if res.Domestic == nil || *res.Domestic {
		t.Fatalf("domestic = %v, want false", res.Domestic)
	}
	if w := a.do("POST", "/mtproto/nodes/1/disguise-check", map[string]any{"domain": "no spaces allowed"}); w.Code != http.StatusBadRequest {
		t.Errorf("bad domain: %d", w.Code)
	}
	if w := a.do("POST", "/mtproto/nodes/1/restart", nil); w.Code != 200 {
		t.Errorf("restart: %d", w.Code)
	}
	// an agent-side refusal surfaces as a gateway error carrying its text
	a.Links.Mu.Lock()
	a.Links.CallErr = &ctrl.Error{Code: "bad_state", Msg: "port 0"}
	a.Links.Mu.Unlock()
	w = a.do("POST", "/mtproto/nodes/1/restart", nil)
	if w.Code != http.StatusBadGateway || !strings.Contains(decode(t, w).Error, "port 0") {
		t.Errorf("agent error: %d %s", w.Code, w.Body)
	}
}

func TestAllowlistEndpointsReportTheListAndWhereItRefreshesFrom(t *testing.T) {
	a := newAPI(t)
	a.h.allow.URL = func() string { return tgnet.DefaultURL }
	var out map[string]any
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/allowlist", nil)).Data, &out)
	if out["placeholder"] != false || out["prefixes"].(float64) < 1 {
		t.Fatalf("allowlist = %v", out)
	}
	if out["url"] != tgnet.DefaultURL {
		t.Errorf("url = %v, want Telegram's list", out["url"])
	}
	// refresh turned off: a refresh is a quiet no-op, and the UI is told
	a.h.allow.URL = func() string { return "" }
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/allowlist", nil)).Data, &out)
	if out["url"] != "" {
		t.Errorf("url with the refresh off = %v", out["url"])
	}
	if w := a.do("POST", "/mtproto/allowlist/refresh", nil); w.Code != 200 {
		t.Fatalf("refresh: %d %s", w.Code, w.Body)
	}
}

func TestNodeViewCarriesSkewAndTheLastCoverCheck(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	no := false
	a.live.snap = &hubnode.Snapshot{
		Link: link.Status{Conns: []link.ConnStatus{
			{Name: "domestic-0", Up: true, Skew: 75 * time.Second},
			{Name: "domestic-1", Up: true, Skew: -1500 * time.Millisecond},
		}},
		Disguise:   &ctrl.DisguiseResult{OK: true, Addrs: []string{"198.51.100.7"}, Domestic: &no},
		DisguiseAt: at,
	}
	var v struct {
		Link struct {
			Conns []struct {
				Name   string `json:"name"`
				SkewMS int64  `json:"skew_ms"`
			} `json:"conns"`
		} `json:"link"`
		Cover *struct {
			OK        bool      `json:"ok"`
			Domestic  *bool     `json:"domestic"`
			CheckedAt time.Time `json:"checked_at"`
		} `json:"disguise_check"`
	}
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1", nil)).Data, &v)
	if len(v.Link.Conns) != 2 || v.Link.Conns[0].SkewMS != 75000 || v.Link.Conns[1].SkewMS != -1500 {
		t.Fatalf("conns = %+v", v.Link.Conns)
	}
	if v.Cover == nil || !v.Cover.OK || v.Cover.Domestic == nil || *v.Cover.Domestic || !v.Cover.CheckedAt.Equal(at) {
		t.Fatalf("cover check = %+v", v.Cover)
	}
}

func TestASecondJobOnABusyNodeIsAConflict(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	a.Links.SetUp(1, true)
	hold := make(chan struct{})
	a.Links.Mu.Lock()
	a.Links.Respond = func(m string, _ any) {
		if m == ctrl.MKeysRotate {
			<-hold
		}
	}
	a.Links.Mu.Unlock()
	w := a.do("POST", "/mtproto/nodes/1/rotate", nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("first rotate: %d %s", w.Code, w.Body)
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	json.Unmarshal(decode(t, w).Data, &out)
	for i := 0; a.Links.CallsOf(ctrl.MKeysRotate) == 0 && i < 500; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	w = a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "telemt"})
	if w.Code != http.StatusConflict {
		t.Fatalf("a second job: %d %s", w.Code, w.Body)
	}
	if msg := decode(t, w).Error; !strings.Contains(msg, "already running on this node") || strings.HasPrefix(msg, "relay:") {
		t.Fatalf("message = %q", msg)
	}
	close(hold)
	a.Wait(t, out.JobID)
}

// The module's errors carry its package name; the panel's operator never sees "relay:".
func TestErrorsReadWithoutThePackagePrefix(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	w := a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "kernel"})
	if got := decode(t, w).Error; w.Code != http.StatusBadRequest || got != "invalid request: kind must be agent or telemt" {
		t.Errorf("bad kind: %d %q", w.Code, got)
	}
	a.Links.Mu.Lock()
	a.Links.Fail = func(m string, _ int) error {
		if m == ctrl.MStatus {
			return errors.New("i/o timeout")
		}
		return nil
	}
	a.Links.Mu.Unlock()
	w = a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "agent"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	json.Unmarshal(decode(t, w).Data, &out)
	a.Wait(t, out.JobID)
	var v relayops.JobView
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/jobs/"+out.JobID, nil)).Data, &v)
	if v.State != relayops.JobFailed || strings.Contains(v.Error, "relay:") || !strings.Contains(v.Error, "i/o timeout") {
		t.Errorf("job error = %q", v.Error)
	}
	a.Links.SetUp(1, false)
	w = a.do("POST", "/mtproto/nodes/1/rotate", nil)
	if got := decode(t, w).Error; w.Code != http.StatusConflict || got != "the node's link is down" {
		t.Errorf("link down: %d %q", w.Code, got)
	}
	w = a.do("POST", "/mtproto/nodes/1/remove", map[string]any{})
	if got := decode(t, w).Error; w.Code != http.StatusConflict || !strings.HasPrefix(got, "the node is unreachable") {
		t.Errorf("no login: %d %q", w.Code, got)
	}
}

func intp(n int) *int { return &n }

// a busy node: two connections up of three, the proxy held, users moving traffic
func (a *apiRig) busy() {
	st := &ctrl.Status{AgentVersion: "0.9.0"}
	st.Telemt.Held = "no users"
	a.live.snap = &hubnode.Snapshot{
		Agent: st, Healthy: true,
		Link: link.Status{Up: 2, Streams: 12, Conns: []link.ConnStatus{
			{Name: "domestic-0", Up: true, RTT: 40 * time.Millisecond},
			{Name: "domestic-1", Up: true, RTT: 60 * time.Millisecond},
			{Name: "domestic2-0", LastErr: "i/o timeout"},
		}},
	}
	a.live.extra = View{Users: intp(3), ActiveUsers: intp(1), Throughput: &Throughput{UpBps: 100, DownBps: 200}}
}

func TestTheNodeListHasEveryMTProtoNodeRunningOrNot(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	a.busy()
	a.Store.Save(context.Background(), &relayops.Record{NodeID: 2, AgentID: "agent-2", MaxStreams: 3000, AgentVersion: "0.8.0", Disguise: "b.example.com"})

	w := a.do("GET", "/mtproto/nodes", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var list []map[string]any
	if err := json.Unmarshal(decode(t, w).Data, &list); err != nil || len(list) != 2 {
		t.Fatalf("list = %s (%v)", decode(t, w).Data, err)
	}
	one, two := list[0], list[1]
	want := map[string]any{
		"node_id": 1.0, "running": true, "healthy": true, "link_up": 2.0, "link_conns": 3.0, "rtt_ms": 50.0,
		"streams": 12.0, "max_streams": 5000.0, "proxy_running": false, "proxy_held": "no users",
		"users": 3.0, "active_users": 1.0, "up_bps": 100.0, "down_bps": 200.0, "agent_version": "0.9.0", "latest_agent": release.AgentVersion,
		"agent_update": true, // it names no build, so the panel's is offered
	}
	for k, v := range want {
		if one[k] != v {
			t.Errorf("node 1 %s = %v, want %v", k, one[k], v)
		}
	}
	if _, ok := one["crash_loop"]; ok {
		t.Error("crash_loop is sent while false")
	}
	if len(one) != len(want) {
		t.Errorf("node 1 has keys %v", keysOf(one))
	}
	stopped := map[string]any{
		"node_id": 2.0, "running": false, "healthy": false, "link_up": 0.0, "link_conns": 0.0, "streams": 0.0, "max_streams": 3000.0,
		"proxy_running": false, "up_bps": 0.0, "down_bps": 0.0, "agent_version": "0.8.0", "latest_agent": release.AgentVersion,
		"agent_update": false, // nothing to compare until it reports
	}
	for k, v := range stopped {
		if two[k] != v {
			t.Errorf("node 2 %s = %v, want %v", k, two[k], v)
		}
	}
	if len(two) != len(stopped) {
		t.Errorf("a stopped node has keys %v", keysOf(two))
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestTheNodeViewCarriesUsersThroughputAndTheLatestReleases(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	var idle map[string]any
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1", nil)).Data, &idle)
	for _, k := range []string{"users", "active_users", "throughput"} {
		if _, ok := idle[k]; ok {
			t.Errorf("%s is sent before it is known", k)
		}
	}
	a.busy()
	var v struct {
		InstalledAt *time.Time `json:"installed_at"`
		Users       *int       `json:"users"`
		ActiveUsers *int       `json:"active_users"`
		Throughput  *struct {
			Up   uint64 `json:"up_bps"`
			Down uint64 `json:"down_bps"`
		} `json:"throughput"`
		Latest *struct {
			Agent  string `json:"agent"`
			Telemt string `json:"telemt"`
		} `json:"latest"`
	}
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1", nil)).Data, &v)
	if v.InstalledAt == nil || !v.InstalledAt.Equal(a.live.created) {
		t.Errorf("installed_at = %v", v.InstalledAt)
	}
	if v.Users == nil || *v.Users != 3 || v.ActiveUsers == nil || *v.ActiveUsers != 1 {
		t.Errorf("users = %v active = %v", v.Users, v.ActiveUsers)
	}
	if v.Throughput == nil || v.Throughput.Up != 100 || v.Throughput.Down != 200 {
		t.Errorf("throughput = %+v", v.Throughput)
	}
	if v.Latest == nil || v.Latest.Agent != release.AgentVersion || v.Latest.Telemt != release.TelemtVersion {
		t.Errorf("latest = %+v", v.Latest)
	}
}

func TestTheEventsFeedAndItsLimit(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	waitEvent(t, a, func(ev Event) bool { return ev.Subject == "install" }, "the install's own entry")
	base := time.Now()
	for i := 0; i < 60; i++ {
		a.live.feed.add(Event{At: base.Add(time.Duration(i) * time.Second), Kind: kindApplied, Level: levelInfo, Detail: fmt.Sprint(i)})
	}
	var out EventFeed
	w := a.do("GET", "/mtproto/nodes/1/events", nil)
	json.Unmarshal(decode(t, w).Data, &out)
	if w.Code != 200 || len(out.Events) != 50 || out.Events[0].Detail != "59" || !out.Since.Equal(a.live.created) {
		t.Fatalf("%d: %d events, newest %+v, since %v", w.Code, len(out.Events), out.Events, out.Since)
	}
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1/events?limit=200", nil)).Data, &out)
	if len(out.Events) != 61 {
		t.Fatalf("limit=200 gave %d events, want all 61", len(out.Events))
	}
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1/events?limit=1", nil)).Data, &out)
	if len(out.Events) != 1 || out.Events[0].Detail != "59" {
		t.Fatalf("limit=1 gave %+v", out.Events)
	}
	for _, q := range []string{"0", "201", "-1", "abc"} {
		if w := a.do("GET", "/mtproto/nodes/1/events?limit="+q, nil); w.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: %d", q, w.Code)
		}
	}
	if w := a.do("GET", "/mtproto/nodes/99/events", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown node: %d", w.Code)
	}
	// a node with nothing recorded is an empty list, not null
	a.Store.Save(context.Background(), &relayops.Record{NodeID: 2})
	a.live.feed = feed{}
	if body := a.do("GET", "/mtproto/nodes/2/events", nil).Body.String(); !strings.Contains(body, `"events":[]`) {
		t.Fatalf("empty feed = %s", body)
	}
}

func waitEvent(t *testing.T, a *apiRig, match func(Event) bool, what string) Event {
	t.Helper()
	var got Event
	waitFor(t, func() bool {
		for _, ev := range a.live.feed.list(feedCap) {
			if match(ev) {
				got = ev
				return true
			}
		}
		return false
	}, what)
	return got
}

func TestFinishedJobsLandInTheFeed(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	ev := waitEvent(t, a, func(ev Event) bool { return ev.Subject == "install" }, "the install's entry")
	if ev.Kind != kindJobOK || ev.Level != levelInfo {
		t.Fatalf("install = %+v", ev)
	}

	w := a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "telemt"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	ev = waitEvent(t, a, func(ev Event) bool { return ev.Subject == "update" }, "the proxy update's entry")
	if ev.Kind != kindJobOK || ev.Detail != "proxy" {
		t.Fatalf("proxy update = %+v", ev)
	}

	a.Links.Mu.Lock()
	a.Links.Fail = func(m string, _ int) error {
		if m == ctrl.MStatus {
			return errors.New("i/o timeout")
		}
		return nil
	}
	a.Links.Mu.Unlock()
	if w := a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": "agent"}); w.Code != http.StatusAccepted {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	ev = waitEvent(t, a, func(ev Event) bool { return ev.Kind == kindJobFailed }, "the failed update's entry")
	if ev.Subject != "update" || ev.Level != levelError || !strings.HasPrefix(ev.Detail, "agent: ") || !strings.Contains(ev.Detail, "i/o timeout") || strings.Contains(ev.Detail, "relay") {
		t.Fatalf("failed update = %+v", ev)
	}
	a.Links.Mu.Lock()
	a.Links.Fail = nil
	a.Links.Mu.Unlock()

	if w := a.do("POST", "/mtproto/nodes/1/rotate", nil); w.Code != http.StatusAccepted {
		t.Fatalf("rotate: %d %s", w.Code, w.Body)
	}
	waitEvent(t, a, func(ev Event) bool { return ev.Subject == "rotate" }, "the rotation's entry")
}

func TestSettingsValidation(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	bad := []any{
		map[string]any{},
		map[string]any{"name": "not a setting"},
		map[string]any{"max_streams": 99},
		map[string]any{"max_streams": 5001},
		map[string]any{"ssh_port": 0},
		map[string]any{"ssh_port": 65536},
		map[string]any{"ssh_user": ""},
		map[string]any{"ssh_user": "has space"},
		map[string]any{"ssh_user": "1root"},
		map[string]any{"ssh_user": "root;rm"},
		map[string]any{"ssh_user": strings.Repeat("a", 33)},
		map[string]any{"max_streams": "lots"},
		"not an object",
	}
	for _, b := range bad {
		w := a.do("PATCH", "/mtproto/nodes/1", b)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%v: %d %s", b, w.Code, w.Body)
			continue
		}
		if msg := decode(t, w).Error; msg == "" || strings.Contains(msg, "relay") {
			t.Errorf("%v: message %q", b, msg)
		}
	}
	req := httptest.NewRequest("PATCH", "/api/v1/mtproto/nodes/1", nil)
	w := httptest.NewRecorder()
	a.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("no body: %d", w.Code)
	}
	if w := a.do("PATCH", "/mtproto/nodes/99", map[string]any{"ssh_port": 2222}); w.Code != http.StatusNotFound {
		t.Errorf("unknown node: %d %s", w.Code, w.Body)
	}
	if w := a.do("PATCH", "/mtproto/nodes/abc", map[string]any{"ssh_port": 2222}); w.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", w.Code)
	}

	w = a.do("PATCH", "/mtproto/nodes/1", map[string]any{"ssh_port": 2222, "ssh_user": "Deploy.user-1", "max_streams": 5000})
	var v NodeView
	json.Unmarshal(decode(t, w).Data, &v)
	if w.Code != 200 || v.SSHPort != 2222 || v.SSHUser != "Deploy.user-1" || v.MaxStreams != 5000 || !v.Running {
		t.Fatalf("%d %+v", w.Code, v)
	}
	if r, _ := a.Store.Get(context.Background(), 1); r.SSHPort != 2222 || r.SSHUser != "Deploy.user-1" {
		t.Fatalf("stored %+v", r)
	}
}

// A new stream limit is saved, taken by the running node and pushed without waiting for a reconcile.
func TestANewStreamLimitReachesTheAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newEnv(t)
	mg := e.manager(t)
	mg.push(t, "alice")
	records := NewRecordStore(mg.st)
	svc := relayops.NewService(relayops.ServiceConfig{Store: records, Links: mg.m.Links()})
	h := NewHandler(svc, mg.m, &tgnet.Refresher{List: tgnet.New(nil)})
	r := gin.New()
	h.RegisterRoutes(r.Group("/api/v1"))
	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PATCH", "/api/v1/mtproto/nodes/1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	before := e.agent.stateCount()
	if w := patch(`{"ssh_port":2200}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	time.Sleep(300 * time.Millisecond)
	if n := e.agent.stateCount(); n != before {
		t.Fatalf("an SSH change pushed %d states", n-before)
	}

	w := patch(`{"max_streams":2500}`)
	var v NodeView
	json.Unmarshal(decode(t, w).Data, &v)
	if w.Code != 200 || v.MaxStreams != 2500 || v.SSHPort != 2200 {
		t.Fatalf("%d %+v", w.Code, v)
	}
	waitFor(t, func() bool { return e.agent.lastState().MaxStreams == 2500 }, "the new limit at the agent")
	if st := e.agent.lastState(); len(st.Users) != 1 || st.Users[0].Name != "alice" || st.Rev != Rev(st) {
		t.Fatalf("the agent got %+v", st)
	}
	if row, _ := mg.st.Get(context.Background(), 1); row.MaxStreams != 2500 || row.SSHPort != 2200 {
		t.Fatalf("stored %+v", row)
	}
	// the next reconcile keeps it
	mg.push(t, "alice", "bob")
	if st := e.agent.lastState(); st.MaxStreams != 2500 || len(st.Users) != 2 {
		t.Fatalf("a later push sent %+v", st)
	}
	// the drift view agrees once the push lands
	waitFor(t, func() bool { v, _ := mg.m.View(1); return v.Snapshot.Healthy }, "the node to be in sync")
}

func TestLogsHideProxySecrets(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	secret := "ee" + strings.Repeat("47ab", 8) + "7777772e636c6f7564666c6172652e636f6d"
	a.Links.Mu.Lock()
	a.Links.Respond = func(m string, resp any) {
		if m == ctrl.MLogsTail {
			*(resp.(*ctrl.LogsResponse)) = ctrl.LogsResponse{Lines: []string{
				"MAESTRO: EE-TLS: tg://proxy?server=[ip]&port=443&secret=" + secret,
				"user alice SECRET: " + secret,
				"client [ip] connected",
			}}
		}
	}
	a.Links.Mu.Unlock()

	w := a.do("GET", "/mtproto/nodes/1/logs?lines=50", nil)
	var logs ctrl.LogsResponse
	json.Unmarshal(decode(t, w).Data, &logs)
	if w.Code != 200 || len(logs.Lines) != 3 {
		t.Fatalf("logs: %d %s", w.Code, w.Body)
	}
	for _, l := range logs.Lines {
		if strings.Contains(l, secret) || strings.Contains(l, secret[2:20]) {
			t.Errorf("a secret reached the log view: %q", l)
		}
	}
	if logs.Lines[0] != "MAESTRO: EE-TLS: tg://proxy?server=[ip]&port=443&secret=[hidden]" {
		t.Errorf("link line = %q", logs.Lines[0])
	}
	if logs.Lines[2] != "client [ip] connected" {
		t.Errorf("an ordinary line changed: %q", logs.Lines[2])
	}
}

// A node on exactly what the panel installs is told so, and an update is refused.
func TestAnUpToDateNodeIsNotOfferedAnUpdate(t *testing.T) {
	a := newAPI(t)
	a.install(t)
	same := strings.Repeat("a", 64) // what relaytest.Bins hands out
	st := &ctrl.Status{AgentVersion: release.AgentVersion, AgentBuild: same, TelemtVersion: release.TelemtVersion}
	a.live.snap = &hubnode.Snapshot{Agent: st, Link: link.Status{Up: 1}}
	a.Links.Mu.Lock()
	a.Links.AgentVersion, a.Links.AgentBuild, a.Links.TelemtVer = st.AgentVersion, st.AgentBuild, st.TelemtVersion
	a.Links.Mu.Unlock()

	var v struct {
		Latest  Releases `json:"latest"`
		Updates Updates  `json:"updates"`
	}
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1", nil)).Data, &v)
	if v.Updates.Agent || v.Updates.Telemt || v.Latest.AgentBuild != same {
		t.Fatalf("view = %+v", v)
	}
	for _, kind := range []string{"agent", "telemt"} {
		w := a.do("POST", "/mtproto/nodes/1/update", map[string]any{"kind": kind})
		if got := decode(t, w).Error; w.Code != http.StatusConflict || got != "this node already runs that version" {
			t.Errorf("%s: %d %q", kind, w.Code, got)
		}
	}

	// another build of the same version is offered
	st.AgentBuild = strings.Repeat("b", 64)
	json.Unmarshal(decode(t, a.do("GET", "/mtproto/nodes/1", nil)).Data, &v)
	if !v.Updates.Agent || v.Updates.Telemt {
		t.Fatalf("another build: %+v", v.Updates)
	}
}

// Stop is saved and pushed with the users still listed; a later push keeps it, and Start undoes it.
func TestStopKeepsTheProxyOffUntilStarted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newEnv(t)
	mg := e.manager(t)
	mg.push(t, "alice")
	records := NewRecordStore(mg.st)
	svc := relayops.NewService(relayops.ServiceConfig{Store: records, Links: mg.m.Links()})
	h := NewHandler(svc, mg.m, &tgnet.Refresher{List: tgnet.New(nil)})
	r := gin.New()
	h.RegisterRoutes(r.Group("/api/v1"))
	post := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/mtproto/nodes/1/"+path, nil))
		return w
	}

	w := post("stop")
	var v NodeView
	json.Unmarshal(decode(t, w).Data, &v)
	if w.Code != 200 || !v.ProxyStopped {
		t.Fatalf("stop: %d %+v", w.Code, v)
	}
	waitFor(t, func() bool { return e.agent.lastState().Stopped }, "the stop at the agent")
	if st := e.agent.lastState(); len(st.Users) != 1 || st.Rev != Rev(st) {
		t.Fatalf("the agent got %+v", st)
	}
	if row, _ := mg.st.Get(context.Background(), 1); !row.ProxyStopped {
		t.Fatal("the stop was not saved")
	}
	// a new user doesn't bring it back
	mg.push(t, "alice", "bob")
	if st := e.agent.lastState(); !st.Stopped || len(st.Users) != 2 {
		t.Fatalf("a later push sent %+v", st)
	}
	if w := post("restart"); w.Code != http.StatusConflict || decode(t, w).Error != "the proxy is stopped: start it instead" {
		t.Fatalf("restart while stopped: %d %s", w.Code, w.Body)
	}

	if w := post("start"); w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	waitFor(t, func() bool { return !e.agent.lastState().Stopped }, "the start at the agent")
	if row, _ := mg.st.Get(context.Background(), 1); row.ProxyStopped {
		t.Fatal("the start was not saved")
	}
}

// Over the real manager and link: a port nobody answers on moves the panel back, one the agent answers on sticks.
func TestTheLinkPortMovesOnlyWhereTheAgentAnswers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := newEnv(t)
	mg := e.manager(t)
	mg.push(t, "alice")
	records := NewRecordStore(mg.st)
	svc := relayops.NewService(relayops.ServiceConfig{Store: records, Links: mg.m.Links(), LinkWait: 3 * time.Second})
	h := NewHandler(svc, mg.m, &tgnet.Refresher{List: tgnet.New(nil)})
	r := gin.New()
	h.RegisterRoutes(r.Group("/api/v1"))
	move := func(port int) relayops.JobView {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/v1/mtproto/nodes/1/link-port", strings.NewReader(fmt.Sprintf(`{"port":%d}`, port)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out struct {
			JobID string `json:"job_id"`
		}
		json.Unmarshal(decode(t, w).Data, &out)
		if w.Code != http.StatusAccepted {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if v, _ := svc.Job(out.JobID); v.State != relayops.JobRunning {
				return v
			}
		}
		t.Fatal("the job never finished")
		return relayops.JobView{}
	}
	port := func() int {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		defer ln.Close()
		return ln.Addr().(*net.TCPAddr).Port
	}
	up := func() bool { v, ok := mg.m.View(1); return ok && v.Snapshot.Link.Up > 0 }
	waitFor(t, up, "the first link")

	dead := port()
	v := move(dead)
	if v.State != relayops.JobFailed || !strings.Contains(v.Error, fmt.Sprintf("could not reach the node on port %d", dead)) {
		t.Fatalf("job = %+v", v)
	}
	if row, _ := mg.st.Get(context.Background(), 1); row.HubPort != e.lnPort {
		t.Fatalf("the panel is on %d, want %d", row.HubPort, e.lnPort)
	}
	waitFor(t, up, "the link back on the old port")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e.serveOn(ln)
	moved := ln.Addr().(*net.TCPAddr).Port
	if v := move(moved); v.State != relayops.JobOK {
		t.Fatalf("job = %+v", v)
	}
	if row, _ := mg.st.Get(context.Background(), 1); row.HubPort != moved {
		t.Fatalf("the panel is on %d, want %d", row.HubPort, moved)
	}
	got := e.agent.linkPorts()
	want := []ctrl.LinkPort{{Port: dead}, {Abort: true}, {Port: moved}, {Finish: true}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("asked the agent %+v, want %+v", got, want)
	}
}
