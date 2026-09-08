package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Load declarations only; bootstrap's privileged main entrypoint is never run.
func offlineInstallerFunctions(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../scripts/offline-install.sh")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	main := strings.Index(source, "# ── Main ")
	if main < 0 {
		t.Fatal("offline installer main boundary not found")
	}
	return source[:main]
}

func TestOfflineInstallCancellationDoesNotDeploy(t *testing.T) {
	root := t.TempDir()
	script := offlineInstallerFunctions(t) + `
INSTALL_DIR="$1/panel"
PGSQL_INSTALL_DIR="$1/pgsql"
BUNDLE_VERSION=test
BUNDLE_ARCH=amd64
install_postgresql() { echo "unexpected PostgreSQL installation" >&2; exit 91; }
deploy_artifacts() { echo "unexpected deployment" >&2; exit 92; }
do_install
`
	cmd := exec.Command("bash", "-c", script, "offline-install-test", root)
	cmd.Stdin = strings.NewReader("n\n")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "no files were deployed") {
		t.Fatalf("cancel output = %s, error = %v", output, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancellation changed directory: %v, %v", entries, err)
	}
}

func TestOfflineInstallRetryPreservesPostgresCluster(t *testing.T) {
	root := t.TempDir()
	pgDir := filepath.Join(root, "pgsql")
	if err := os.MkdirAll(filepath.Join(pgDir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pgDir, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(pgDir, "data", "postgresql.conf")
	if err := os.WriteFile(config, []byte("existing database settings\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pgDir, "bin", "pg_ctl"), []byte("#!/usr/bin/env bash\nprintf 'PostgreSQL 17.9\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	script := offlineInstallerFunctions(t) + `
PGSQL_INSTALL_DIR="$1/pgsql"
install_postgresql
`
	output, err := exec.Command("bash", "-c", script, "offline-install-test", root).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Keeping existing PostgreSQL") {
		t.Fatalf("retry output = %s, error = %v", output, err)
	}
	data, err := os.ReadFile(config)
	if err != nil || string(data) != "existing database settings\n" {
		t.Fatalf("existing database config = %q, error = %v", data, err)
	}
}

func TestOfflineInstallDoesNotOverwriteDamagedRuntimeWithExistingData(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "pgsql", "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	script := offlineInstallerFunctions(t) + `
PGSQL_INSTALL_DIR="$1/pgsql"
install_postgresql
`
	output, err := exec.Command("bash", "-c", script, "offline-install-test", root).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "data directory has been preserved") {
		t.Fatalf("invalid-runtime output = %s, error = %v", output, err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("existing data directory was lost: %v", err)
	}
}
