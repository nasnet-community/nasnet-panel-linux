// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/ui"
)

const base = "/api/plugin/view/remote-nodes"

var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// what the model holds that a browser must never get
const (
	privateKey = "MARKER-REALITY-PRIVATE-KEY"
	hostKeyPin = "SHA256:MARKER-HOST-KEY-PIN"
	uuidA      = "6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5f"
	uuidB      = "0b9a8f7e-6d5c-4b3a-9281-706f5e4d3c2b"
	bridgeUUID = "3d2c1b0a-9f8e-4d7c-8b6a-5f4e3d2c1b0a"
)

// testCA is the CA the test hub holds, key and all.
var testCA = func() *pki.CA {
	ca, err := pki.NewCA(now)
	if err != nil {
		panic(err)
	}
	return ca
}()

// pemBody is the base64 of a PEM block without its lines, so that it is
// looked for as one run of text, and a header on its own is not mistaken for it.
func pemBody(b []byte) string {
	var body []string
	for _, line := range strings.Split(string(b), "\n") {
		if line != "" && !strings.HasPrefix(line, "-----") {
			body = append(body, line)
		}
	}
	return strings.Join(body, "")
}

// secrets is everything the test hub holds that no body may carry: the REALITY
// key, the pinned host key, the clients' ids, the bridge's, and the CA.
func secrets() []string {
	return []string{
		privateKey, hostKeyPin, uuidA, uuidB, bridgeUUID,
		"BEGIN EC PRIVATE KEY", "BEGIN CERTIFICATE",
		pemBody(testCA.KeyPEM()), pemBody(testCA.CertPEM()),
		// the same text a line at a time, which is how a PEM would be copied out of a log
		strings.Split(string(testCA.KeyPEM()), "\n")[1],
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type rig struct {
	t    *testing.T
	st   *store.Store
	srv  *httptest.Server
	log  *syncBuf
	jobs *jobs.Engine
}

func newRig(t *testing.T, basePath string) *rig { return newRigWith(t, basePath, nil) }

// newRigWith is newRig with a say in the config, once the job engine exists.
func newRigWith(t *testing.T, basePath string, mod func(*Config, *jobs.Engine)) *rig {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	index, err := ui.New(basePath)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, st: st, log: &syncBuf{}}
	logger := slog.New(slog.NewTextHandler(r.log, nil))
	r.jobs = jobs.New(jobs.Config{Now: func() time.Time { return now }, Log: logger, StepKey: testStepKey, ErrorCode: testErrorCode, ErrorInfo: testErrorInfo})
	cfg := Config{
		Store: st, Jobs: r.jobs, Version: "1.2.3", Commit: "abc1234", BasePath: basePath, Index: index,
		Log: logger, Now: func() time.Time { return now },
	}
	if mod != nil {
		mod(&cfg, r.jobs)
	}
	r.srv = httptest.NewServer(New(cfg))
	// An event stream ends with its job, and Close waits for the streams, so
	// the engine is closed first. Whatever is still connected is let go.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.jobs.Close(ctx); err != nil {
			t.Errorf("closing the job engine: %v", err)
		}
		r.srv.CloseClientConnections()
		r.srv.Close()
	})
	return r
}

// call makes a request with exactly the headers given.
func (r *rig) call(method, path string, headers map[string]string) (int, http.Header, []byte) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.srv.URL+path, nil)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		r.t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, body
}

// get is what the page does: a GET with its header.
func (r *rig) get(path string) []byte {
	r.t.Helper()
	code, _, body := r.call("GET", path, map[string]string{UIHeader: "1"})
	if code != http.StatusOK {
		r.t.Fatalf("GET %s = %d: %s", path, code, body)
	}
	return body
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("not JSON of the shape wanted: %v\n%s", err, body)
	}
	return v
}

func (r *rig) populate() {
	r.t.Helper()
	err := r.st.Update(func(s *model.State) error {
		n1, err := s.AddNode(model.Node{
			Name: "تهران", Host: "203.0.113.1", SSHPort: 22, SSHUser: "root", AgentPort: 9090, EntryPort: 443,
			State: model.NodeRegistered, CreatedAt: now, Arch: "arm64",
			RealityTarget: "www.example.com:443", RealitySNI: "www.example.com",
			RealityPublicKey: "PUBLICKEY", RealityShortID: "0123abcd", RealityPrivateKey: privateKey,
			AgentVersion: "1.3.0", XrayVersion: "26.10.21",
			CertNotBefore: now.AddDate(0, 0, -10), CertNotAfter: now.AddDate(0, 0, 355), SSHHostKey: hostKeyPin,
			BridgeUUID: bridgeUUID,
		})
		if err != nil {
			return err
		}
		s.CACert, s.CAKey = string(testCA.CertPEM()), string(testCA.KeyPEM())
		// a node whose install has not finished
		if _, err := s.AddNode(model.Node{Name: "second", Host: "node2.example.com", SSHPort: 2222, AgentPort: 9091, EntryPort: 8443, State: model.NodeInstalling, CreatedAt: now}); err != nil {
			return err
		}
		a, err := s.AddSubscription(model.Subscription{Name: "علی", CreatedAt: now, TrafficLimit: 50 << 30, ExpiresAt: now.AddDate(0, 0, 30)})
		if err != nil {
			return err
		}
		b, err := s.AddSubscription(model.Subscription{Name: "sara", CreatedAt: now})
		if err != nil {
			return err
		}
		s.Enablements = []model.Enablement{
			{SubscriptionID: a.ID, NodeID: n1.ID, ClientUUID: uuidA, Enabled: true},
			{SubscriptionID: b.ID, NodeID: n1.ID, ClientUUID: uuidB, Enabled: false},
		}
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

func TestHealthNeedsNoHeaderAndCountsWrites(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		paths := []string{"/health"}
		if b != "" {
			paths = append(paths, b+"/health")
		}
		for _, p := range paths {
			code, hdr, body := r.call("GET", p, nil)
			if code != http.StatusOK || !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
				t.Fatalf("base %q GET %s = %d %q", b, p, code, hdr.Get("Content-Type"))
			}
			h := decode[map[string]any](t, body)
			if h["ok"] != true || h["version"] != "1.2.3" || h["commit"] != "abc1234" || h["writes"] != float64(0) || len(h) != 4 {
				t.Fatalf("health = %s", body)
			}
		}
		r.populate()
		h := decode[map[string]any](t, func() []byte { _, _, b := r.call("GET", paths[len(paths)-1], nil); return b }())
		if h["writes"] != float64(1) {
			t.Fatalf("writes = %v after one change", h["writes"])
		}
	}
}

func TestAnEmptyHubAnswersWithZerosAndEmptyLists(t *testing.T) {
	for _, b := range []string{"", base} {
		r := newRig(t, b)
		cfg := decode[map[string]any](t, r.get(b+"/api/config"))
		want := map[string]any{"version": "1.2.3", "commit": "abc1234", "maxNodes": 3.0, "nodeCount": 0.0, "maxSubscriptions": 10.0, "subscriptionCount": 0.0, "xray": ""}
		for k, v := range want {
			if cfg[k] != v {
				t.Errorf("base %q config[%s] = %v, want %v", b, k, cfg[k], v)
			}
		}
		if backup := cfg["backup"].(map[string]any); backup["lastExportAt"] != nil {
			t.Errorf("lastExportAt = %v, want null", backup["lastExportAt"])
		}
		entry := cfg["entry"].(map[string]any)
		if entry["on"] != false || entry["domestic"] != "missing" || entry["bridge"] != "stopped" || entry["checkedAt"] != nil || entry["stale"] != false {
			t.Errorf("entry = %v", entry)
		}
		if vpn := entry["vpn"].(map[string]any); vpn["state"] != "missing" || vpn["kind"] != "" || vpn["label"] != "" {
			t.Errorf("entry.vpn = %v", vpn)
		}
		if routes, ok := entry["routes"].([]any); !ok || len(routes) != 0 {
			t.Errorf("entry.routes = %#v, want []", entry["routes"])
		}
		if got := string(r.get(b + "/api/nodes")); got != "[]\n" {
			t.Errorf("nodes = %q, want []", got)
		}
		if got := string(r.get(b + "/api/subscriptions")); got != "[]\n" {
			t.Errorf("subscriptions = %q, want []", got)
		}
	}
}

func sortedKeys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

func wantKeys(t *testing.T, what string, got map[string]any, want string) {
	t.Helper()
	if w := strings.Fields(want); !slices.Equal(sortedKeys(got), slices.Sorted(slices.Values(w))) {
		t.Errorf("%s has the keys %v, the contract says %v", what, sortedKeys(got), w)
	}
}

// The page is written against the contract, so a key that moves, goes or
// appears here is a break there.
func TestTheShapesAreTheContracts(t *testing.T) {
	r := newRig(t, "")
	r.populate()

	cfg := decode[map[string]any](t, r.get("/api/config"))
	wantKeys(t, "config", cfg, "version commit maxNodes nodeCount maxSubscriptions subscriptionCount entry backup xray")
	entry := cfg["entry"].(map[string]any)
	wantKeys(t, "entry", entry, "on domestic vpn bridge routerOS routes checkedAt stale")
	wantKeys(t, "entry.vpn", entry["vpn"].(map[string]any), "state kind label")
	wantKeys(t, "entry.routerOS", entry["routerOS"].(map[string]any), "version min ok")
	wantKeys(t, "backup", cfg["backup"].(map[string]any), "lastExportAt")

	nodes := decode[[]map[string]any](t, r.get("/api/nodes"))
	if len(nodes) != 2 {
		t.Fatalf("%d nodes", len(nodes))
	}
	for _, n := range nodes {
		wantKeys(t, "node", n, "id name host sshPort sshUser registered createdAt entryPort handshakeTarget sni sniCustom publicKey shortId arch agentVersion xrayVersion lastSeenAt subscriptionCount tunnel agent cert updates usage")
		wantKeys(t, "node.tunnel", n["tunnel"].(map[string]any), "state since rttMs streams carried bytesToSubs bytesFromSubs dialFailures reason")
		wantKeys(t, "node.cert", n["cert"].(map[string]any), "notAfter validDays renewsAt")
		wantKeys(t, "node.updates", n["updates"].(map[string]any), "agent xray agentVersion xrayVersion")
		wantKeys(t, "node.usage", n["usage"].(map[string]any), "live online connections rateUp rateDown todayUp todayDown monthUp monthDown updatedAt")
	}

	subs := decode[[]map[string]any](t, r.get("/api/subscriptions"))
	if len(subs) != 2 {
		t.Fatalf("%d subscriptions", len(subs))
	}
	for _, s := range subs {
		wantKeys(t, "subscription", s, "id name createdAt trafficLimit trafficUsed expiresAt stopped online lastSeenAt todayUp todayDown monthUp monthDown nodes")
		for _, n := range s["nodes"].([]any) {
			wantKeys(t, "subscription.nodes[]", n.(map[string]any), "nodeId enabled online connections lastSeenAt todayUp todayDown monthUp monthDown")
		}
	}
}

func TestNodesAndSubscriptionsCarryWhatTheModelHolds(t *testing.T) {
	r := newRig(t, "")
	r.populate()

	nodes := decode[[]Node](t, r.get("/api/nodes"))
	n := nodes[0]
	if n.ID != 1 || n.Name != "تهران" || n.Host != "203.0.113.1" || n.SSHPort != 22 || n.SSHUser != "root" || !n.Registered ||
		n.EntryPort != 443 || n.HandshakeTarget != "www.example.com:443" || n.SNI != "www.example.com" || n.SNICustom ||
		n.PublicKey != "PUBLICKEY" || n.ShortID != "0123abcd" || n.Arch != "arm64" ||
		n.AgentVersion != "1.3.0" || n.XrayVersion != "26.10.21" || n.SubscriptionCount != 1 || !n.CreatedAt.Equal(now) {
		t.Errorf("node 1 = %+v", n)
	}
	if n.Cert.ValidDays != 355 || n.Cert.NotAfter != now.AddDate(0, 0, 355).Format(time.RFC3339) ||
		n.Cert.RenewsAt != now.AddDate(0, 0, 355).Add(-30*24*time.Hour).Format(time.RFC3339) {
		t.Errorf("node 1 cert = %+v", n.Cert)
	}
	if n.Tunnel.State != "off" || n.Agent != nil || n.LastSeenAt != nil || n.Installing != nil || n.Events != nil {
		t.Errorf("node 1 live parts = %+v", n)
	}

	m := nodes[1]
	if m.Registered || m.SubscriptionCount != 0 || m.Cert.NotAfter != "" || m.Cert.RenewsAt != "" || m.Cert.ValidDays != 0 || m.HandshakeTarget != "" || m.SNICustom {
		t.Errorf("node 2 (still installing) = %+v", m)
	}

	subs := decode[[]Subscription](t, r.get("/api/subscriptions"))
	if len(subs) != 2 || subs[0].Name != "علی" || subs[1].Name != "sara" {
		t.Fatalf("subscriptions = %+v", subs)
	}
	// one entry per registered node: the one still installing is left out
	if len(subs[0].Nodes) != 1 || subs[0].Nodes[0].NodeID != 1 || !subs[0].Nodes[0].Enabled {
		t.Errorf("ali's nodes = %+v", subs[0].Nodes)
	}
	if len(subs[1].Nodes) != 1 || subs[1].Nodes[0].Enabled {
		t.Errorf("sara's nodes = %+v (switched off)", subs[1].Nodes)
	}
	if subs[0].Online != nil || subs[0].Nodes[0].Online != nil || subs[0].Nodes[0].Connections != nil {
		t.Error("what is unknown should be null")
	}

	cfg := decode[AppConfig](t, r.get("/api/config"))
	if cfg.NodeCount != 2 || cfg.SubscriptionCount != 2 {
		t.Errorf("counts = %d nodes, %d subscriptions", cfg.NodeCount, cfg.SubscriptionCount)
	}

	if err := r.st.Update(func(s *model.State) error { s.LastBackupAt = now; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := decode[AppConfig](t, r.get("/api/config")).Backup.LastExportAt; got == nil || !got.Equal(now) {
		t.Errorf("lastExportAt = %v", got)
	}
}

func TestACustomSNIIsMarked(t *testing.T) {
	r := newRig(t, "")
	r.populate()
	if err := r.st.Update(func(s *model.State) error { s.FindNode(1).RealitySNI = "other.example.com"; return nil }); err != nil {
		t.Fatal(err)
	}
	if n := decode[[]Node](t, r.get("/api/nodes"))[0]; !n.SNICustom || n.SNI != "other.example.com" {
		t.Errorf("node = %+v", n)
	}
}

func TestAnExpiredCertificateCountsDaysBelowZero(t *testing.T) {
	for _, c := range []struct {
		left time.Duration
		days int
	}{
		{355 * 24 * time.Hour, 355}, {36 * time.Hour, 1}, {time.Hour, 0}, {-time.Hour, -1}, {-25 * time.Hour, -2},
	} {
		cert := certView(model.Node{CertNotAfter: now.Add(c.left)}, now)
		if cert.ValidDays != c.days {
			t.Errorf("%v left: %d days, want %d", c.left, cert.ValidDays, c.days)
		}
	}
}

// secretish names a field that should never be in a body.
func secretish(name string) bool {
	name = strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(name))
	for _, bad := range []string{"privatekey", "uuid", "hostkey", "password", "passphrase", "secret", "token", "cakey", "credential"} {
		if strings.Contains(name, bad) {
			return true
		}
	}
	return false
}

func TestSecretishNamesAreRecognised(t *testing.T) {
	for _, n := range []string{"privateKey", "reality_private_key", "clientUuid", "client_uuid", "uuid", "hostKey", "ssh_host_key", "password", "passphrase", "secret", "token", "caKey"} {
		if !secretish(n) {
			t.Errorf("%q is not recognised as secret", n)
		}
	}
	for _, n := range []string{"publicKey", "shortId", "sshUser", "sshPort", "host", "handshakeTarget", "jobId"} {
		if secretish(n) {
			t.Errorf("%q is taken for a secret", n)
		}
	}
}

// jsonNames collects the JSON names a type can produce, through slices,
// pointers and nested structs.
func jsonNames(typ reflect.Type, path string, into map[string]string) {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ == reflect.TypeOf(time.Time{}) {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		into[path+"."+name] = name
		jsonNames(f.Type, path+"."+name, into)
	}
}

// Nothing in a view type may be named like a secret, whether or not a
// particular response happens to fill it.
func TestNoViewTypeHasASecretField(t *testing.T) {
	names := map[string]string{}
	for _, v := range []any{AppConfig{}, Node{}, Subscription{}, NodeSubscription{}, Traffic{}, jobs.Job{}, errorBody{}} {
		jsonNames(reflect.TypeOf(v), reflect.TypeOf(v).Name(), names)
	}
	if len(names) < 50 {
		t.Fatalf("only %d names found; the walk is broken", len(names))
	}
	for where, name := range names {
		if secretish(name) {
			t.Errorf("%s looks like a secret", where)
		}
	}
}

// And no body the page can read has such a key or such a value, at the root
// or under the base path, for a hub that holds secrets of every kind.
func TestNoSecretReachesABrowser(t *testing.T) {
	for _, b := range []string{"", base} {
		fake, bk := &fakeSubs{}, newFakeBackup()
		r := newRigWith(t, b, withOps(withSubs(fake), withBackup(bk)))
		r.populate()
		held := r.st.Read()
		if held.Nodes[0].RealityPrivateKey != privateKey || held.Nodes[0].SSHHostKey != hostKeyPin || held.Enablements[0].ClientUUID != uuidA ||
			held.Nodes[0].BridgeUUID != bridgeUUID || held.CACert == "" || held.CAKey == "" {
			t.Fatal("the test hub does not hold the secrets it is meant to hide")
		}

		bodies := map[string][]byte{}
		for _, p := range []string{
			"/api/config", "/api/nodes", "/api/subscriptions",
			"/api/nodes/1", "/api/nodes/2", "/api/nodes/1/subscriptions", "/api/nodes/1/traffic", "/api/jobs",
		} {
			bodies[b+p] = r.get(b + p)
		}
		_, _, bodies[b+"/health"] = r.call("GET", b+"/health", nil)
		_, _, bodies[b+"/api/nothing"] = r.call("GET", b+"/api/nothing", map[string]string{UIHeader: "1"})
		_, _, bodies[b+"/api/config (no header)"] = r.call("GET", b+"/api/config", nil)
		_, _, bodies[b+"/ (the page)"] = r.call("GET", b+"/", nil)
		// the calls that change a subscription, and the backup's, answer in the same way
		for what, body := range subscriptionBodies(r, fake, b) {
			bodies[what] = body
		}
		for what, body := range backupBodies(r, bk, b) {
			bodies[what] = body
		}

		if !strings.Contains(string(bodies[b+"/api/nodes"]), "PUBLICKEY") {
			t.Fatal("the public key should be shown: it is part of every link anyway")
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

func walkKeys(v any, visit func(string)) {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			visit(k)
			walkKeys(x, visit)
		}
	case []any:
		for _, x := range v {
			walkKeys(x, visit)
		}
	}
}

func TestAllRoutesAreAlsoUnderTheBasePath(t *testing.T) {
	r := newRig(t, base)
	r.populate()
	for _, p := range []string{"/api/config", "/api/nodes", "/api/subscriptions"} {
		if string(r.get(p)) != string(r.get(base+p)) {
			t.Errorf("%s differs under the base path", p)
		}
	}
}

func TestThePageIsServedWithoutTheHeader(t *testing.T) {
	r := newRig(t, base)
	for _, p := range []string{"/", base + "/", base} {
		code, hdr, body := r.call("GET", p, nil)
		if code != http.StatusOK || !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") ||
			!strings.Contains(string(body), `<base href="`+base+`/">`) {
			t.Errorf("GET %s = %d %q\n%s", p, code, hdr.Get("Content-Type"), body)
		}
	}
	r = newRig(t, "")
	if code, _, body := r.call("GET", "/", nil); code != http.StatusOK || !strings.Contains(string(body), `<base href="/">`) {
		t.Errorf("GET / = %d\n%s", code, body)
	}
}

func TestUnknownPathsAnswerInJSON(t *testing.T) {
	r := newRig(t, base)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/nothing"}, {"GET", base + "/api/nothing"}, {"GET", "/api"}, {"GET", "/nothing"},
		{"GET", base + "/nothing"}, {"POST", "/health"}, {"DELETE", "/"}, {"GET", "/api/nodes/1/nothing"},
	} {
		code, hdr, body := r.call(c.method, c.path, map[string]string{UIHeader: "1"})
		got := decode[map[string]any](t, body)
		if code != http.StatusNotFound || got["code"] != "not_found" || got["error"] == "" || len(got) != 2 ||
			!strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
			t.Errorf("%s %s = %d %s", c.method, c.path, code, body)
		}
	}
}

func TestResponsesAreNotCachedOrSniffed(t *testing.T) {
	r := newRig(t, "")
	for _, p := range []string{"/health", "/api/config", "/api/nothing"} {
		_, hdr, _ := r.call("GET", p, map[string]string{UIHeader: "1"})
		if hdr.Get("Cache-Control") != "no-store" || hdr.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: Cache-Control %q, nosniff %q", p, hdr.Get("Cache-Control"), hdr.Get("X-Content-Type-Options"))
		}
	}
}
