// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestSettingsFromTheEnvironment(t *testing.T) {
	got, err := fromEnv(env())
	want := settings{
		listen: ":8080", dataDir: "/data", basePath: "", addrs: netroles.Defaults(),
		bridgeBin: "/remote-nodes-bridge", bridgeUID: 65534, bridgeGID: 65534, bridgeDNS: []string{"1.1.1.1", "8.8.8.8"},
		factsPath: "/data/router/state",
	}
	want.licensesDir = "/licenses"
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %+v, %v\nwant       %+v", got, err, want)
	}

	got, err = fromEnv(env(
		"LISTEN_ADDR", " 127.0.0.1:9000 ", "DATA_DIR", "/var/lib/rn", "BASE_PATH", "/api/plugin/view/remote-nodes/",
		"ADDR_FOREIGN", "10.9.0.16", "ADDR_EXIT", "10.9.0.17", "ADDR_NODES", " 10.9.0.18 ",
		"BRIDGE_BIN", " /opt/bridge ", "BRIDGE_UID", "1000", "BRIDGE_GID", "1001", "BRIDGE_DNS", " 9.9.9.9 , 149.112.112.112:53 ,",
		"FACTS_PATH", "/run/router-state", "RELEASE_BASE_URL", "http://mirror.example:8000/rn/",
		"LICENSES_DIR", " /usr/share/licenses/rn ",
	))
	want = settings{
		listen: "127.0.0.1:9000", dataDir: "/var/lib/rn", basePath: "/api/plugin/view/remote-nodes",
		addrs: netroles.Set{
			Downloads: netroles.Addr{Name: "ADDR_FOREIGN", IP: netip.MustParseAddr("10.9.0.16")},
			Exit:      netroles.Addr{Name: "ADDR_EXIT", IP: netip.MustParseAddr("10.9.0.17")},
			Nodes:     netroles.Addr{Name: "ADDR_NODES", IP: netip.MustParseAddr("10.9.0.18")},
		},
		bridgeBin: "/opt/bridge", bridgeUID: 1000, bridgeGID: 1001, bridgeDNS: []string{"9.9.9.9", "149.112.112.112:53"},
		factsPath: "/run/router-state",
	}
	want.licensesDir = "/usr/share/licenses/rn"
	want.release.Base = "http://mirror.example:8000/rn"
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("set = %+v, %v\nwant  %+v", got, err, want)
	}

	// the facts follow the data folder unless they are told otherwise
	got, err = fromEnv(env("DATA_DIR", "/srv/rn"))
	if err != nil || got.factsPath != "/srv/rn/router/state" {
		t.Errorf("facts path = %q, %v", got.factsPath, err)
	}

	for name, c := range map[string]struct {
		e    func(string) string
		want string
	}{
		"no port in the listen address":  {env("LISTEN_ADDR", "localhost"), "LISTEN_ADDR"},
		"a base path with a query":       {env("BASE_PATH", "/a?b=c"), ""},
		"a base path with no slash":      {env("BASE_PATH", "plugin"), ""},
		"a base path going up":           {env("BASE_PATH", "/a/../b"), ""},
		"an address that is a name":      {env("ADDR_NODES", "nodes.example"), "ADDR_NODES"},
		"an address with a port":         {env("ADDR_EXIT", "10.0.0.1:80"), "ADDR_EXIT"},
		"the same exit and nodes":        {env("ADDR_EXIT", "10.1.1.1", "ADDR_NODES", "10.1.1.1"), "ADDR_EXIT"},
		"a user id that is a name":       {env("BRIDGE_UID", "nobody"), "BRIDGE_UID"},
		"a negative group id":            {env("BRIDGE_GID", "-1"), "BRIDGE_GID"},
		"a group id that is too big":     {env("BRIDGE_GID", "4294967296"), "BRIDGE_GID"},
		"a resolver that is a name":      {env("BRIDGE_DNS", "dns.example"), "BRIDGE_DNS"},
		"a resolver the exit refuses":    {env("BRIDGE_DNS", "1.1.1.1,192.168.1.1"), "192.168.0.0/16"},
		"a resolver of another family":   {env("BRIDGE_DNS", "2606:4700:4700::1111"), "family"},
		"an unspecified resolver":        {env("BRIDGE_DNS", "0.0.0.0"), "BRIDGE_DNS"},
		"too many resolvers":             {env("BRIDGE_DNS", "1.1.1.1,1.0.0.1,8.8.8.8,8.8.4.4,9.9.9.9,9.9.9.10,2.2.2.2,3.3.3.3,4.4.4.4"), "at most"},
		"a release address with a login": {env("RELEASE_BASE_URL", "https://user:pass@example.com/rn"), "RELEASE_BASE_URL"},
	} {
		_, err := fromEnv(c.e)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %q", name, err, c.want)
		}
		// an error never repeats a login
		if err != nil && strings.Contains(err.Error(), "pass") {
			t.Errorf("%s: the error repeats the password: %v", name, err)
		}
	}
}

// The resolvers are checked against the exit's family: an IPv6 exit takes
// IPv6 resolvers, which the defaults are not.
func TestBridgeDNSFollowsTheExitsFamily(t *testing.T) {
	got, err := fromEnv(env("ADDR_EXIT", "fd00::17", "ADDR_NODES", "fd00::18", "BRIDGE_DNS", "2606:4700:4700::1111,[2001:4860:4860::8888]:53"))
	if err != nil || len(got.bridgeDNS) != 2 {
		t.Fatalf("%+v, %v", got.bridgeDNS, err)
	}
	if _, err := fromEnv(env("ADDR_EXIT", "fd00::17", "ADDR_NODES", "fd00::18")); err == nil || !strings.Contains(err.Error(), "family") {
		t.Errorf("the default resolvers with an IPv6 exit: %v", err)
	}
}

// testSettings are what a hub on a free port starts from: the environment
// parsed the way main parses it, so that the defaults are the real ones.
func testSettings(t *testing.T, dir, base string, kv ...string) settings {
	t.Helper()
	s, err := fromEnv(env(append([]string{"DATA_DIR", dir, "BASE_PATH", base, "LISTEN_ADDR", "127.0.0.1:0"}, kv...)...))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// start runs the hub on a free port and returns its address and a way to stop it.
func start(t *testing.T, s settings) (addr string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), s, func(a net.Addr) { ready <- a })
	}()
	select {
	case a := <-ready:
		addr = a.String()
	case err := <-done:
		t.Fatalf("the hub stopped at once: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the hub never listened")
	}
	stopped := false
	stop = func() error {
		stopped = true
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("the hub did not stop")
			return nil
		}
	}
	t.Cleanup(func() {
		if !stopped {
			stop()
		}
	})
	return addr, stop
}

func fetch(t *testing.T, url string, ui bool) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if ui {
		req.Header.Set("X-RemoteNodes-UI", "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestServesAndStopsCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	base := "/api/plugin/view/remote-nodes"
	addr, stop := start(t, testSettings(t, dir, base))

	for _, p := range []string{"/health", base + "/health"} {
		code, body := fetch(t, "http://"+addr+p, false)
		var h map[string]any
		if err := json.Unmarshal(body, &h); err != nil || code != 200 || h["ok"] != true || h["version"] != "dev" || h["commit"] != "unknown" {
			t.Errorf("GET %s = %d %s", p, code, body)
		}
	}
	for p, want := range map[string]string{"/api/nodes": "[]\n", base + "/api/subscriptions": "[]\n", "/api/jobs": "[]\n"} {
		if code, body := fetch(t, "http://"+addr+p, true); code != 200 || string(body) != want {
			t.Errorf("GET %s = %d %q", p, code, body)
		}
	}
	if code, body := fetch(t, "http://"+addr+"/api/nodes/1", true); code != 404 || !strings.Contains(string(body), "not_found") {
		t.Errorf("GET /api/nodes/1 = %d %s", code, body)
	}
	if code, body := fetch(t, "http://"+addr+base+"/", false); code != 200 || !strings.Contains(string(body), `<base href="`+base+`/">`) {
		t.Errorf("the page = %d %s", code, body)
	}

	// the first start made the CA, and that is the one write there was
	var h struct{ Writes int }
	_, body := fetch(t, "http://"+addr+"/health", false)
	if err := json.Unmarshal(body, &h); err != nil || h.Writes != 1 {
		t.Errorf("health = %s", body)
	}
	state, err := os.ReadFile(filepath.Join(dir, store.FileName))
	if err != nil || !bytes.Contains(state, []byte(`"ca_cert"`)) || !bytes.Contains(state, []byte(`"ca_key"`)) {
		t.Errorf("state.json after the first start: %v\n%s", err, state)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("data dir: %v, %v", fi, err)
	}

	if err := stop(); err != nil {
		t.Fatalf("shutting down: %v", err)
	}
	if _, err := http.Get("http://" + addr + "/health"); err == nil {
		t.Error("the hub still answers after it stopped")
	}

	// a second start keeps the CA it made, and writes nothing
	addr, stop = start(t, testSettings(t, dir, base))
	if after, _ := os.ReadFile(filepath.Join(dir, store.FileName)); !bytes.Equal(after, state) {
		t.Error("the second start changed the state file")
	}
	_, body = fetch(t, "http://"+addr+"/health", false)
	if err := json.Unmarshal(body, &h); err != nil || h.Writes != 0 {
		t.Errorf("health after the second start = %s", body)
	}
	if err := stop(); err != nil {
		t.Fatalf("shutting down: %v", err)
	}
}

// The licence files are read from where LICENSES_DIR says, by the hub as it
// runs. The page's Notices link opens /licenses with no header, under the base
// path or not, and a machine without the image's files gets a plain not found.
func TestServesTheLicensesFromLicensesDir(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"LICENSE": "AGPL text\n", "NOTICES": "Go and its modules\n", "web-notices.txt": "Preact and lucide-react\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := "/api/plugin/view/remote-nodes"
	addr, _ := start(t, testSettings(t, t.TempDir(), base, "LICENSES_DIR", dir))
	for _, p := range []string{"/licenses", base + "/licenses"} {
		for _, ui := range []bool{false, true} {
			code, body := fetch(t, "http://"+addr+p, ui)
			text := string(body)
			a, b, c := strings.Index(text, "AGPL text"), strings.Index(text, "Go and its modules"), strings.Index(text, "Preact and lucide-react")
			if code != 200 || a < 0 || b < a || c < b {
				t.Errorf("GET %s (page header %v) = %d, want the three files in order:\n%s", p, ui, code, body)
			}
		}
	}

	addr, _ = start(t, testSettings(t, t.TempDir(), "", "LICENSES_DIR", filepath.Join(dir, "gone")))
	code, body := fetch(t, "http://"+addr+"/licenses", false)
	var got struct{ Code string }
	if err := json.Unmarshal(body, &got); err != nil || code != 404 || got.Code != "not_found" {
		t.Errorf("GET /licenses with no such directory = %d %s", code, body)
	}

	if s, err := fromEnv(env("LICENSES_DIR", "   ")); err != nil || s.licensesDir != "/licenses" {
		t.Errorf("a blank LICENSES_DIR = %q, %v, want the default", s.licensesDir, err)
	}
}

// A hub with nothing set up still answers: no nodes, no report from the
// router, no bridge program, none of the three addresses on this machine.
func TestAHubWithNothingConfiguredShowsTheEntryAsMissingFacts(t *testing.T) {
	addr, stop := start(t, testSettings(t, t.TempDir(), "", "BRIDGE_BIN", filepath.Join(t.TempDir(), "no-such-bridge")))
	defer stop()
	code, body := fetch(t, "http://"+addr+"/api/config", true)
	var cfg struct {
		Xray  string
		Entry struct {
			On       bool
			Domestic string
			VPN      struct{ State, Kind, Label string }
			Bridge   string
			RouterOS struct {
				Version, Min string
				OK           bool
			}
			Routes    []struct{ Ready bool }
			CheckedAt *string
			Stale     bool
		}
	}
	if err := json.Unmarshal(body, &cfg); err != nil || code != 200 {
		t.Fatalf("GET /api/config = %d %s", code, body)
	}
	e := cfg.Entry
	if e.On || e.Domestic != "missing" || e.VPN.State != "missing" || e.Bridge != "stopped" || e.CheckedAt != nil || e.Stale || e.RouterOS.Min != "7.20" || e.RouterOS.OK || len(e.Routes) != 3 {
		t.Errorf("entry = %+v", e)
	}
	if cfg.Xray != pin.Version {
		t.Errorf("xray = %q, want the pin %q", cfg.Xray, pin.Version)
	}
}

// The router's report is read from where FACTS_PATH says, and what it says
// shows on the page.
func TestTheRoutersReportIsReadFromFactsPath(t *testing.T) {
	report := filepath.Join(t.TempDir(), "state")
	text := fmt.Sprintf("domestic=1\nvpn=1\nvpn_kind=wireguard\nvpn_label=wg-vpn\nroute_foreign=1\nroute_vpn=1\nroute_domestic=1\nros=7.24.5\nupdated=%d\n", time.Now().Unix())
	if err := os.WriteFile(report, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	addr, stop := start(t, testSettings(t, t.TempDir(), "", "FACTS_PATH", report))
	defer stop()
	_, body := fetch(t, "http://"+addr+"/api/config", true)
	var cfg struct {
		Entry struct {
			On     bool
			VPN    struct{ Kind, Label string }
			Bridge string
		}
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	// both links are up, and with no node to carry the bridge is waiting
	if !cfg.Entry.On || cfg.Entry.VPN.Kind != "wireguard" || cfg.Entry.VPN.Label != "wg-vpn" || cfg.Entry.Bridge != "waiting" {
		t.Errorf("entry = %s", body)
	}
}

// What /health counts is the writes of both files.
func TestHealthCountsTheStateFileAndTheUsageFile(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	coll, err := usage.Open(usage.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	c := counted{st, coll}
	if c.Writes() != 0 {
		t.Fatalf("writes = %d", c.Writes())
	}
	if err := st.Update(func(s *model.State) error { s.LastBackupAt = time.Now(); return nil }); err != nil {
		t.Fatal(err)
	}
	if c.Writes() != 1 || c.Writes() != st.Writes()+coll.Writes() {
		t.Errorf("writes = %d after one write of the state file", c.Writes())
	}
	if len(c.Read().Nodes) != 0 {
		t.Error("the counted store doesn't read through")
	}
}

// Everything is stopped, and in the order that keeps the tunnels carrying as
// long as the hub does. A stage that fails doesn't keep the others from
// stopping, and each failure is said.
func TestStopsInOrderAndNoFailureStopsTheRest(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	var order []string
	stage := func(name string, err error) stage {
		return stage{name, func(context.Context) error { order = append(order, name); return err }}
	}
	err := stopInOrder(context.Background(), log,
		stage("requests and jobs", nil), stage("manager", errors.New("took too long")), stage("usage counters", nil), stage("bridge", errors.New("would not die")))
	if got := strings.Join(order, ","); got != "requests and jobs,manager,usage counters,bridge" {
		t.Errorf("stopped in the order %s", got)
	}
	if err == nil || !strings.Contains(err.Error(), "manager: took too long") || !strings.Contains(err.Error(), "bridge: would not die") {
		t.Errorf("err = %v", err)
	}
	if strings.Count(buf.String(), "stopping failed") != 2 {
		t.Errorf("log:\n%s", &buf)
	}
	if err := stopInOrder(context.Background(), log, stage("a", nil), stage("b", nil)); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestRefusesToStartOnAFileItCannotUse(t *testing.T) {
	dir := t.TempDir()
	junk := []byte("{broken")
	if err := os.WriteFile(filepath.Join(dir, store.FileName), junk, 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), testSettings(t, dir, ""), nil)
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err = %v, want one saying the state file is corrupt", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, store.FileName)); string(got) != string(junk) {
		t.Fatal("the file was touched")
	}
}

func TestAPortInUseIsAnError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	taken := testSettings(t, t.TempDir(), "")
	taken.listen = ln.Addr().String()
	err = run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), taken, nil)
	if err == nil {
		t.Fatal("started on a port that is taken")
	}
}
