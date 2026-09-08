package tool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeInstaller(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nasnet-tool.sh")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallerCommandFindsLocalScript(t *testing.T) {
	for _, location := range []string{"project", "beside binary", "above bin directory", "installed"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			cfg := &Config{ProjectDir: filepath.Join(root, "project with spaces"), InstallDir: filepath.Join(root, "installed")}
			executable := filepath.Join(root, "release", "bin", "nasnet-tool")
			dir := map[string]string{
				"project": cfg.ProjectDir, "beside binary": filepath.Dir(executable),
				"above bin directory": filepath.Dir(filepath.Dir(executable)), "installed": cfg.InstallDir,
			}[location]
			script := writeInstaller(t, dir, "printf '%s' \"$1\"\n")
			cmd, err := installerCommand(cfg, executable)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cmd.Args[1:], []string{script, "install"}) || cmd.Dir != dir {
				t.Fatalf("command = %v, directory = %s", cmd.Args, cmd.Dir)
			}
			output, err := cmd.Output()
			if err != nil || string(output) != "install" {
				t.Fatalf("delegation output = %q, error = %v", output, err)
			}
		})
	}
}

func TestInstallerCommandOfflineUsesInstalledScript(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{ProjectDir: filepath.Join(root, "project"), InstallDir: filepath.Join(root, "installed"), OfflineMode: true}
	writeInstaller(t, cfg.ProjectDir, "exit 99\n")
	script := writeInstaller(t, cfg.InstallDir, "printf '%s' \"$1\"\n")
	cmd, err := installerCommand(cfg, filepath.Join(root, "nasnet-tool"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[1] != script {
		t.Fatalf("selected %v, want installed bundle %s", cmd.Args, script)
	}
	output, err := cmd.Output()
	if err != nil || string(output) != "--offline" {
		t.Fatalf("offline delegation output = %q, error = %v", output, err)
	}
}

func TestInstallerCommandMissingScriptDoesNotDownload(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{ProjectDir: filepath.Join(root, "project"), InstallDir: filepath.Join(root, "installed")}
	cmd, err := installerCommand(cfg, filepath.Join(root, "bin", "nasnet-tool"))
	if err == nil || cmd != nil || !strings.Contains(err.Error(), "same release") {
		t.Fatalf("command = %v, error = %v", cmd, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing-script lookup changed directory: %v, %v", entries, err)
	}
}

func TestWizardInstallPropagatesFailedShellExit(t *testing.T) {
	dir := t.TempDir()
	writeInstaller(t, dir, "exit 23\n")
	err := WizardInstall(&Config{ProjectDir: dir})
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("error = %v; want delegated exit 23", err)
	}
}

func TestWizardInstallRefreshesSavedConfigWhenReadinessFails(t *testing.T) {
	dir := t.TempDir()
	writeInstaller(t, dir, `cat > .env <<'ENV'
DEPLOY_MODE=systemd
DB_DRIVER=sqlite
DB_PATH=/saved/panel.db
APP_PORT=3456
ACME_ENABLED=true
ENV
exit 23
`)
	cfg := &Config{ProjectDir: dir, InstallDir: dir, DeployMode: "docker", DBDriver: "postgres"}
	err := WizardInstall(cfg)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("error = %v; want delegated exit 23", err)
	}
	if !cfg.IsSystemd() || !cfg.IsSQLite() || cfg.DBPath != "/saved/panel.db" || cfg.AppPort != "3456" || !cfg.ACMEEnabled || cfg.EnvFile != filepath.Join(dir, ".env") {
		t.Fatalf("menu kept stale configuration after failed readiness: %+v", cfg)
	}
}

func TestDockerComposeIncludesSelectedOverrides(t *testing.T) {
	for _, database := range []string{"postgres", "sqlite"} {
		for _, acme := range []bool{false, true} {
			t.Run(database+"/acme="+map[bool]string{false: "false", true: "true"}[acme], func(t *testing.T) {
				cfg := &Config{ProjectDir: "/project", ComposeFile: "/project/docker-compose.yml",
					SQLiteComposeFile: "/project/docker-compose.sqlite.yml", DBDriver: database, ACMEEnabled: acme}
				args := []string{"compose", "-f", cfg.ComposeFile}
				if database == "sqlite" {
					args = append(args, "-f", cfg.SQLiteComposeFile)
				}
				if acme {
					args = append(args, "-f", "/project/docker-compose.acme.yml")
				}
				args = append(args, "--project-directory", cfg.ProjectDir, "up", "-d")
				if got := cfg.DockerCompose("up", "-d").Args[1:]; !reflect.DeepEqual(got, args) {
					t.Fatalf("args = %v, want %v", got, args)
				}
			})
		}
	}
}

func TestDockerComposeReadsChangedACMESetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	cfg := &Config{ProjectDir: "/project", ComposeFile: "base.yml", EnvFile: path, ACMEEnabled: true}
	for _, value := range []string{"false", "true"} {
		if err := os.WriteFile(path, []byte("ACME_ENABLED="+value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		hasOverride := strings.Contains(strings.Join(cfg.DockerCompose("up").Args, " "), "docker-compose.acme.yml")
		if hasOverride != (value == "true") {
			t.Fatalf("ACME_ENABLED=%s: override included = %t", value, hasOverride)
		}
	}
}

func TestRefreshInstalledConfigTracksStandaloneSelections(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "installed")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(installDir, ".env")
	if err := os.WriteFile(envFile, []byte("DEPLOY_MODE=systemd\nDB_DRIVER=sqlite\nDB_PATH=/saved/panel.db\nAPP_PORT=3456\nACME_ENABLED=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ProjectDir: root, InstallDir: installDir, DeployMode: "docker", DBDriver: "postgres"}
	refreshInstalledConfig(cfg, root)
	if cfg.ProjectDir != installDir || cfg.EnvFile != envFile || !cfg.IsSystemd() || !cfg.IsSQLite() || cfg.DBPath != "/saved/panel.db" || cfg.AppPort != "3456" || !cfg.ACMEEnabled {
		t.Fatalf("stale configuration after install: %+v", cfg)
	}
}

func TestReconfigureWriterPreservesInstalledDatabaseAndTLS(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{ProjectDir: dir, InstallDir: dir, EnvFile: filepath.Join(dir, ".env")}
	existing := "DB_USER=nasnet_panel\nDB_PASSWORD=saved-password\nDB_NAME=existing_panel\nDB_HOST=db.internal\nDB_PORT=5433\nDB_SSL_MODE=verify-full\nPGSQL_SERVICE_NAME=postgresql\nTLS_CERT_FILE=/certs/panel.pem\nTLS_KEY_FILE=/certs/key.pem\n"
	if err := os.WriteFile(cfg.EnvFile, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	wv := &wizardVars{DBDriver: "postgres", DBPassword: "saved-password", AppBaseURL: "https://panel.example:9761", AcmeEnabled: "false"}
	wizardWriteEnv(cfg, wv, "systemd", "reconfigure test")
	for key, value := range map[string]string{
		"DB_USER": "nasnet_panel", "DB_PASSWORD": "saved-password", "DB_NAME": "existing_panel",
		"DB_HOST": "db.internal", "DB_PORT": "5433", "PGSQL_SERVICE_NAME": "postgresql",
		"DB_SSL_MODE":   "verify-full",
		"TLS_CERT_FILE": "/certs/panel.pem", "TLS_KEY_FILE": "/certs/key.pem",
	} {
		if got := ReadEnvValue(key, cfg.EnvFile); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestReconfigureWriterPreservesSQLiteDatabasePath(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{ProjectDir: dir, InstallDir: dir, EnvFile: filepath.Join(dir, ".env")}
	if err := os.WriteFile(cfg.EnvFile, []byte("DB_DRIVER=sqlite\nDB_PATH=/mnt/storage/existing.db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wizardWriteEnv(cfg, &wizardVars{DBDriver: "sqlite"}, "systemd", "reconfigure test")
	if got := ReadEnvValue("DB_PATH", cfg.EnvFile); got != "/mnt/storage/existing.db" {
		t.Fatalf("DB_PATH = %q; want existing database path", got)
	}
}
