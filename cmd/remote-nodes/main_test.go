// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
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
	if err != nil || got != (settings{listen: ":8080", dataDir: "/data", basePath: ""}) {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	got, err = fromEnv(env("LISTEN_ADDR", " 127.0.0.1:9000 ", "DATA_DIR", "/var/lib/rn", "BASE_PATH", "/api/plugin/view/remote-nodes/"))
	want := settings{listen: "127.0.0.1:9000", dataDir: "/var/lib/rn", basePath: "/api/plugin/view/remote-nodes"}
	if err != nil || got != want {
		t.Fatalf("set = %+v, %v; want %+v", got, err, want)
	}
	for name, e := range map[string]func(string) string{
		"no port in the listen address": env("LISTEN_ADDR", "localhost"),
		"a base path with a query":      env("BASE_PATH", "/a?b=c"),
		"a base path with no slash":     env("BASE_PATH", "plugin"),
		"a base path going up":          env("BASE_PATH", "/a/../b"),
	} {
		if _, err := fromEnv(e); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// start runs the hub on a free port and returns its address and a way to stop it.
func start(t *testing.T, s settings) (addr string, stop func() error) {
	t.Helper()
	s.listen = "127.0.0.1:0"
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
	addr, stop := start(t, settings{dataDir: dir, basePath: base})

	for _, p := range []string{"/health", base + "/health"} {
		code, body := fetch(t, "http://"+addr+p, false)
		var h map[string]any
		if err := json.Unmarshal(body, &h); err != nil || code != 200 || h["ok"] != true || h["version"] != "dev" || h["commit"] != "unknown" {
			t.Errorf("GET %s = %d %s", p, code, body)
		}
	}
	for p, want := range map[string]string{"/api/nodes": "[]\n", base + "/api/subscriptions": "[]\n"} {
		if code, body := fetch(t, "http://"+addr+p, true); code != 200 || string(body) != want {
			t.Errorf("GET %s = %d %q", p, code, body)
		}
	}
	if code, body := fetch(t, "http://"+addr+base+"/", false); code != 200 || !strings.Contains(string(body), `<base href="`+base+`/">`) {
		t.Errorf("the page = %d %s", code, body)
	}

	// an idle hub has nothing to save
	if _, err := os.Stat(filepath.Join(dir, store.FileName)); !os.IsNotExist(err) {
		t.Errorf("state.json after only reads: %v", err)
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
}

func TestRefusesToStartOnAFileItCannotUse(t *testing.T) {
	dir := t.TempDir()
	junk := []byte("{broken")
	if err := os.WriteFile(filepath.Join(dir, store.FileName), junk, 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), settings{listen: "127.0.0.1:0", dataDir: dir}, nil)
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
	err = run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), settings{listen: ln.Addr().String(), dataDir: t.TempDir()}, nil)
	if err == nil {
		t.Fatal("started on a port that is taken")
	}
}
