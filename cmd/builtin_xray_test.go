package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

type fakeSettings map[string]string

func (f fakeSettings) GetByKey(_ context.Context, key string) (string, error) {
	return f[key], nil
}

type fakeSettingRepo map[string]*settingDomain.Setting

func (f fakeSettingRepo) GetByKey(_ context.Context, key string) (*settingDomain.Setting, error) {
	if s, ok := f[key]; ok {
		return s, nil
	}
	return nil, errors.New("record not found")
}

func (f fakeSettingRepo) Update(_ context.Context, s *settingDomain.Setting) error {
	f[s.Key] = s
	return nil
}

// cacheBuild puts a verified build of version in the cache under the cwd.
func cacheBuild(t *testing.T, version string) string {
	t.Helper()
	data := append([]byte{0x7f, 'E', 'L', 'F'}, version...)
	p := filepath.Join("bin", "xray", "v"+version, "xray-linux-"+runtime.GOARCH)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := os.WriteFile(p, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".sha256", []byte(hex.EncodeToString(sum[:])), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func TestTheBuiltInNodeRunsThePickedCachedBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	want := cacheBuild(t, "26.11.1")
	got := builtinXrayPath(context.Background(), fakeSettings{settingDomain.BuiltinXrayKey: "26.11.1"}, "/usr/local/bin/xray")
	if got != want {
		t.Fatalf("path = %s, want %s", got, want)
	}
}

func TestAPickedBuildMissingFromTheCacheFallsBackToTheInstalledOne(t *testing.T) {
	t.Chdir(t.TempDir())
	cacheBuild(t, "26.9.30")
	got := builtinXrayPath(context.Background(), fakeSettings{settingDomain.BuiltinXrayKey: "26.11.1"}, "/usr/local/bin/xray")
	if got != "/usr/local/bin/xray" {
		t.Fatalf("path = %s", got)
	}
	// nothing picked and the tested build isn't cached either
	if got := builtinXrayPath(context.Background(), fakeSettings{}, "/usr/local/bin/xray"); got != "/usr/local/bin/xray" {
		t.Fatalf("path = %s", got)
	}
}

func fakeInstalledXray(t *testing.T, version string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "xray")
	script := "#!/bin/sh\necho \"Xray " + version + " (Xray, Penetrates Everything.) abc123 (go1.27 linux/amd64)\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnInstalledXrayNewerThanTheTestedOneIsKept(t *testing.T) {
	for _, printed := range []string{"99.1.0", "v99.1.0"} {
		repo := fakeSettingRepo{}
		adoptInstalledXray(context.Background(), repo, fakeInstalledXray(t, printed))
		if s := repo[settingDomain.BuiltinXrayKey]; s == nil || s.Value != "99.1.0" || s.Category != "admin" {
			t.Fatalf("%s: row = %+v", printed, s)
		}
	}
}

func TestAdoptLeavesTestedOrOlderBuildsAndExistingRowsAlone(t *testing.T) {
	for _, v := range []string{pin.Version, "1.8.4"} {
		repo := fakeSettingRepo{}
		adoptInstalledXray(context.Background(), repo, fakeInstalledXray(t, v))
		if len(repo) != 0 {
			t.Fatalf("%s: wrote %+v", v, repo[settingDomain.BuiltinXrayKey])
		}
	}
	repo := fakeSettingRepo{settingDomain.BuiltinXrayKey: settingDomain.BuiltinXraySetting("")}
	adoptInstalledXray(context.Background(), repo, fakeInstalledXray(t, "99.1.0"))
	if repo[settingDomain.BuiltinXrayKey].Value != "" {
		t.Fatal("following the panel was overwritten")
	}
}
