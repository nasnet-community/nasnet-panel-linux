package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/process"
)

// fakeXray answers "version" with v, or fails it when v is empty; "run" idles.
func fakeXray(t *testing.T, dir, name, v string) string {
	t.Helper()
	version := `echo "Xray ` + v + ` (Xray, Penetrates Everything.)"`
	if v == "" {
		version = "exit 1"
	}
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\ncase \"$1\" in\nversion) " + version + " ;;\nrun) exec sleep 30 ;;\nesac\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func switchServer(t *testing.T, bin string) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := process.NewXrayManager(process.Config{BinaryPath: bin, ConfigPath: cfg})
	t.Cleanup(func() { _ = mgr.Stop(5 * time.Second) })
	return &Server{xrayMgr: mgr}
}

func TestSwitchXrayRestartsOnTheNewBuild(t *testing.T) {
	dir := t.TempDir()
	s := switchServer(t, fakeXray(t, dir, "old", "26.10.10"))
	if err := s.xrayMgr.Start(); err != nil {
		t.Fatal(err)
	}
	next := fakeXray(t, dir, "new", "26.11.1")

	v, err := s.SwitchXray(next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v, "26.11.1") {
		t.Fatalf("version = %q", v)
	}
	if s.xrayMgr.BinaryPath() != next || !s.xrayMgr.IsRunning() {
		t.Fatalf("path %s, running %v", s.xrayMgr.BinaryPath(), s.xrayMgr.IsRunning())
	}
}

func TestSwitchXrayKeepsTheOldBuildWhenTheNewOneFails(t *testing.T) {
	dir := t.TempDir()
	old := fakeXray(t, dir, "old", "26.10.10")
	s := switchServer(t, old)
	if err := s.xrayMgr.Start(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SwitchXray(fakeXray(t, dir, "broken", "")); err == nil {
		t.Fatal("a build with no version was accepted")
	}
	if s.xrayMgr.BinaryPath() != old || !s.xrayMgr.IsRunning() {
		t.Fatalf("path %s, running %v", s.xrayMgr.BinaryPath(), s.xrayMgr.IsRunning())
	}
}

func TestSwitchXrayLeavesAStoppedXrayStopped(t *testing.T) {
	dir := t.TempDir()
	s := switchServer(t, fakeXray(t, dir, "old", "26.10.10"))
	next := fakeXray(t, dir, "new", "26.11.1")

	if _, err := s.SwitchXray(next); err != nil {
		t.Fatal(err)
	}
	if s.xrayMgr.IsRunning() {
		t.Fatal("switching started a stopped xray")
	}
	if s.xrayMgr.BinaryPath() != next {
		t.Fatalf("path = %s", s.xrayMgr.BinaryPath())
	}
}
