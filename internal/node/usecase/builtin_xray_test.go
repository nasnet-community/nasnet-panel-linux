package usecase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	agentconfig "github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

type fakeXrayCache struct {
	builds map[string]string // version → binary
	failed bool
}

func (f *fakeXrayCache) GetChecksum(string, string) (string, error) { return "", nil }

func (f *fakeXrayCache) EnsureCached(version, _ string) error {
	if f.failed {
		return errors.New("github is unreachable")
	}
	return nil
}

func (f *fakeXrayCache) VerifiedPath(version, _ string) (string, error) {
	if p, ok := f.builds[version]; ok {
		return p, nil
	}
	return "", errors.New("not cached")
}

type savedSettings struct {
	settingDomain.SettingUsecase
	rows []*settingDomain.Setting
	ctxs []context.Context
}

func (s *savedSettings) UpdateMany(ctx context.Context, rows []*settingDomain.Setting) error {
	s.rows = append(s.rows, rows...)
	s.ctxs = append(s.ctxs, ctx)
	return nil
}

func scriptXray(t *testing.T, dir, name, version string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\n[ \"$1\" = version ] && echo \"Xray " + version + " (Xray, Penetrates Everything.)\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func builtinNode(t *testing.T) (*nodeUsecase, *agentserver.Server, *fakeXrayCache, *savedSettings, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := agentconfig.DefaultConfig()
	cfg.TLS.Enabled = false
	cfg.Process.ServiceMode = false
	cfg.Xray.BinaryPath = scriptXray(t, dir, "installed", "26.9.30")
	cfg.Xray.ConfigPath = filepath.Join(dir, "config.json")
	cfg.Cores.DataDir = filepath.Join(dir, "cores")
	srv, err := agentserver.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cache := &fakeXrayCache{builds: map[string]string{
		"26.11.1":   scriptXray(t, dir, "new", "26.11.1"),
		pin.Version: scriptXray(t, dir, "tested", pin.Version),
	}}
	settings := &savedSettings{}
	return &nodeUsecase{xrayBM: cache, embeddedSrv: srv, settingUC: settings}, srv, cache, settings, cfg.Xray.BinaryPath
}

func TestPickingAnXrayForTheBuiltInNodeRunsTheCachedBuildAndKeepsThePick(t *testing.T) {
	u, srv, cache, settings, _ := builtinNode(t)

	if err := u.switchBuiltinXray(context.Background(), 1, "26.11.1"); err != nil {
		t.Fatal(err)
	}
	if got := srv.XrayBinaryPath(); got != cache.builds["26.11.1"] {
		t.Fatalf("xray runs %s", got)
	}
	if len(settings.rows) != 1 || settings.rows[0].Key != settingDomain.BuiltinXrayKey || settings.rows[0].Value != "26.11.1" {
		t.Fatalf("saved %+v", settings.rows)
	}
	if !settingDomain.ProtectedWriteAllowed(settings.ctxs[0]) {
		t.Fatal("the pick was saved through the generic settings path")
	}

	// the tested version means follow the panel again
	if err := u.switchBuiltinXray(context.Background(), 1, pin.Version); err != nil {
		t.Fatal(err)
	}
	if settings.rows[1].Value != "" {
		t.Fatalf("tested version saved as %q", settings.rows[1].Value)
	}
}

func TestAFailedDownloadLeavesTheBuiltInXrayAndThePickAlone(t *testing.T) {
	u, srv, cache, settings, installed := builtinNode(t)
	cache.failed = true

	if err := u.switchBuiltinXray(context.Background(), 1, "26.11.1"); err == nil {
		t.Fatal("a failed download reported success")
	}
	if srv.XrayBinaryPath() != installed || len(settings.rows) != 0 {
		t.Fatalf("xray runs %s, saved %+v", srv.XrayBinaryPath(), settings.rows)
	}
}
