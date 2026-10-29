package tool

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const resetHash = "$2a$10$YN/ZfhKoWOGMv98B.8KvTO.q6TzrGFve34zI9kEwTQ42lCcaobdC2"

// A stand-in panel binary that logs how it was run and exits with the given status.
func fakePanel(t *testing.T, installDir, message string, status int) string {
	t.Helper()
	log := filepath.Join(installDir, "calls")
	script := "#!/bin/sh\n" +
		`{ echo "args=$*"; echo "cwd=$(pwd)"; echo "env=$DB_DRIVER|$DB_PATH|$DB_PASSWORD"; echo "stdin=$(cat)"; } >> '` + log + "'\n" +
		"echo '" + message + "' >&2\n" +
		"exit " + strconv.Itoa(status) + "\n"
	if err := os.MkdirAll(filepath.Join(installDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "bin", "nasnet-panel"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestStoreAdminPasswordHashUsesPanelBinary(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nasnet_panel.db")
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	log := fakePanel(t, dir, "", 0)
	cfg := &Config{DeployMode: "systemd", DBDriver: "sqlite", DBPath: dbPath, InstallDir: dir}
	notInitialized, err := storeAdminPasswordHash(cfg, resetHash)
	if err != nil || notInitialized {
		t.Fatalf("got %v, %v", notInitialized, err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"args=set-admin-password-hash", "env=sqlite|" + dbPath + "|", "stdin=" + resetHash} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("missing %q in %s", want, calls)
		}
	}

	// Postgres credentials go in the environment, not on the command line.
	pg := &Config{DeployMode: "systemd", DBDriver: "postgres", DBPassword: "db-s3cret", InstallDir: dir}
	if _, err := storeAdminPasswordHash(pg, resetHash); err != nil {
		t.Fatal(err)
	}
	calls, _ = os.ReadFile(log)
	if !strings.Contains(string(calls), "env=postgres||db-s3cret") || strings.Contains(string(calls), "args=set-admin-password-hash db-s3cret") {
		t.Fatalf("postgres call: %s", calls)
	}
}

func TestStoreAdminPasswordHashReportsState(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{DeployMode: "systemd", DBDriver: "sqlite", DBPath: filepath.Join(dir, "missing.db"), InstallDir: dir}
	log := fakePanel(t, dir, "", 0)
	if notInitialized, err := storeAdminPasswordHash(cfg, resetHash); err != nil || !notInitialized {
		t.Fatalf("missing database: got %v, %v", notInitialized, err)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("panel binary must not run without a database")
	}

	cfg.DBDriver = "postgres"
	fakePanel(t, dir, "panel database is not initialized yet", 3)
	if notInitialized, err := storeAdminPasswordHash(cfg, resetHash); err != nil || !notInitialized {
		t.Fatalf("exit 3: got %v, %v", notInitialized, err)
	}
	fakePanel(t, dir, "Error: connect to database: refused", 1)
	if _, err := storeAdminPasswordHash(cfg, resetHash); err == nil || err.Error() != "Error: connect to database: refused" {
		t.Fatalf("failure: got %v", err)
	}
}

func TestSetEnvAdminPasswordHash(t *testing.T) {
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte("ADMIN_USERNAME=admin\nADMIN_PASSWORD_HASH='$2a$10$old'\nOTHER=kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setEnvAdminPasswordHash(env, resetHash); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(env)
	want := "ADMIN_USERNAME=admin\nADMIN_PASSWORD_HASH='" + resetHash + "'\nOTHER=kept\n"
	if string(got) != want {
		t.Fatalf("got %q", got)
	}
	if ReadEnvValue("ADMIN_PASSWORD_HASH", env) != resetHash {
		t.Fatal("hash does not round-trip")
	}
}

// Panel login is one admin; users.is_admin only marks Telegram bot admins.
func TestSecurityMenuHasNoCreateAdminUser(t *testing.T) {
	var labels []string
	for _, item := range securityMenu {
		labels = append(labels, item.label)
	}
	if strings.Join(labels, ",") != "Reset Admin Password" {
		t.Fatalf("security menu = %q", labels)
	}
}
