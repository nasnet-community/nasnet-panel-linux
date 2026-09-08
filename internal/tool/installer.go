package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// WizardInstall shares the reviewed shell workflow with the one-line and
// offline installers. Keeping a single implementation prevents the Go menu
// from silently using different database, TLS, or service-start behavior.
func WizardInstall(cfg *Config) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate nasnet-tool: %w", err)
	}
	cmd, err := installerCommand(cfg, executable)
	if err != nil {
		ui.StepFail(err.Error())
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	// Readiness can fail after configuration and services have been deployed.
	// Keep subsequent menu operations pointed at that actual installation.
	refreshInstalledConfig(cfg, cmd.Dir)
	if err != nil {
		return fmt.Errorf("installation did not complete: %w", err)
	}
	return nil
}

// The interactive Go menu can continue after the child exits. Read the saved
// selection so subsequent operations target the database and deployment just
// installed instead of the menu's pre-install defaults.
func refreshInstalledConfig(cfg *Config, scriptDir string) {
	projectDir := scriptDir
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err != nil {
		projectDir = cfg.InstallDir
	}
	envFile := filepath.Join(projectDir, ".env")
	if projectDir == "" {
		return
	}
	if _, err := os.Stat(envFile); err != nil {
		return // The user may have cancelled before writing configuration.
	}
	cfg.ProjectDir = projectDir
	cfg.EnvFile = envFile
	cfg.BackupDir = filepath.Join(projectDir, "data", "backups")
	cfg.ComposeFile = filepath.Join(projectDir, "docker-compose.yml")
	cfg.SQLiteComposeFile = filepath.Join(projectDir, "docker-compose.sqlite.yml")
	cfg.ACMEComposeFile = filepath.Join(projectDir, "docker-compose.acme.yml")
	for key, dest := range map[string]*string{
		"DEPLOY_MODE": &cfg.DeployMode, "DB_DRIVER": &cfg.DBDriver,
		"DB_USER": &cfg.DBUser, "DB_PASSWORD": &cfg.DBPassword,
		"DB_NAME": &cfg.DBName, "DB_HOST": &cfg.DBHost,
		"DB_PORT": &cfg.DBPort, "DB_PATH": &cfg.DBPath, "APP_PORT": &cfg.AppPort,
	} {
		if value := ReadEnvValue(key, envFile); value != "" {
			*dest = value
		}
	}
	cfg.ACMEEnabled = ReadEnvValue("ACME_ENABLED", envFile) == "true"
	if _, err := os.Stat(filepath.Join(cfg.InstallDir, ".bundle-manifest")); err == nil {
		cfg.OfflineMode = true
	}
}

// installerCommand only resolves scripts already present on disk. A standalone
// Go binary must be accompanied by the installer from the same release; never
// download and execute an unverified script as an implicit fallback.
func installerCommand(cfg *Config, executable string) (*exec.Cmd, error) {
	if cfg == nil {
		return nil, fmt.Errorf("installation configuration is missing")
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	binDir := filepath.Dir(executable)
	dirs := []string{cfg.ProjectDir, binDir, filepath.Dir(binDir), cfg.InstallDir}
	if cfg.OfflineMode {
		// The installed bundle is authoritative when the menu is launched from
		// an unrelated working directory.
		dirs = []string{cfg.InstallDir, binDir, filepath.Dir(binDir), cfg.ProjectDir}
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		script, err := filepath.Abs(filepath.Join(dir, "nasnet-tool.sh"))
		if err != nil {
			continue
		}
		info, err := os.Stat(script)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		arg := "install"
		if cfg.OfflineMode {
			arg = "--offline"
		}
		cmd := exec.Command("bash", script, arg)
		cmd.Dir = filepath.Dir(script)
		cmd.Env = os.Environ()
		return cmd, nil
	}
	return nil, fmt.Errorf("nasnet-tool.sh was not found; place the installer from the same release beside nasnet-tool, or run nasnet-tool.sh from the repository or installed bundle")
}
