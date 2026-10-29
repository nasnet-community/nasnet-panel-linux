package tool

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

func ActionResetAdminPassword(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Reset Admin Password")

	password, err := ui.InputPassword("New password")
	if err != nil {
		return
	}
	if len(password) < 6 {
		ui.StepFail("Password must be at least 6 characters")
		ui.PressAnyKey()
		return
	}

	confirm, err := ui.InputPassword("Confirm password")
	if err != nil {
		return
	}
	if password != confirm {
		ui.StepFail("Passwords do not match")
		ui.PressAnyKey()
		return
	}

	hash, err := GenBcrypt(password)
	if err != nil {
		ui.StepFail("Failed to generate bcrypt hash: " + err.Error())
		ui.PressAnyKey()
		return
	}

	notInitialized, err := storeAdminPasswordHash(cfg, hash)
	if err != nil {
		ui.StepFail("Could not save the new password in the panel database: " + err.Error())
		ui.StepInfo("The password was not changed")
		ui.PressAnyKey()
		return
	}
	envErr := setEnvAdminPasswordHash(cfg.EnvFile, hash)
	if envErr == nil {
		envErr = cfg.SyncEnvToInstallDir()
	}
	if envErr != nil && notInitialized {
		ui.StepFail("Could not write ADMIN_PASSWORD_HASH to .env: " + envErr.Error())
		ui.PressAnyKey()
		return
	} else if envErr != nil {
		ui.StepWarn("Could not update ADMIN_PASSWORD_HASH in .env; the panel uses the new password anyway")
	}

	fmt.Println()
	if notInitialized {
		ui.StepOk("Password saved. The panel database is not created yet; the panel uses it when it starts")
	} else {
		ui.StepOk("Password changed. It works now, no restart needed")
	}
	ui.PressAnyKey()
}

// Login reads the admin_password_hash setting on every request and .env only at
// startup, so the panel binary (which owns that schema) writes the setting.
// notInitialized means the panel never started: .env alone is then enough.
func storeAdminPasswordHash(cfg *Config, hash string) (notInitialized bool, err error) {
	var cmd *exec.Cmd
	if cfg.IsDocker() {
		cmd = exec.Command("docker", "exec", "-i", DefaultContainerBackend, "./main", "set-admin-password-hash")
	} else {
		dbEnv := []string{"DB_DRIVER=" + cfg.DBDriver}
		if cfg.IsSQLite() {
			if _, err := os.Stat(cfg.DBPath); errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			dbEnv = append(dbEnv, "DB_PATH="+cfg.DBPath)
		} else {
			dbEnv = append(dbEnv, "DB_HOST="+cfg.DBHost, "DB_PORT="+cfg.DBPort, "DB_USER="+cfg.DBUser,
				"DB_PASSWORD="+cfg.DBPassword, "DB_NAME="+cfg.DBName, "DB_SSL_MODE="+getEnvDefault("DB_SSL_MODE", "disable"))
		}
		bin := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
		if cfg.IsSQLite() && os.Geteuid() != 0 && syscall.Access(cfg.DBPath, 0x2) != nil {
			// The service user owns the database file.
			cmd = exec.Command("sudo", append(append([]string{"env"}, dbEnv...), bin, "set-admin-password-hash")...)
		} else {
			cmd = exec.Command(bin, "set-admin-password-hash")
			cmd.Env = append(os.Environ(), dbEnv...)
		}
		cmd.Dir = cfg.InstallDir
	}
	cmd.Stdin = strings.NewReader(hash + "\n")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		return true, nil
	}
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			lines := strings.Split(msg, "\n")
			return false, errors.New(lines[len(lines)-1])
		}
		return false, err
	}
	return false, nil
}

// Single quotes stop `source` and Compose from expanding the hash's $ signs.
func setEnvAdminPasswordHash(envFile, hash string) error {
	data, err := os.ReadFile(envFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	line := fmt.Sprintf("ADMIN_PASSWORD_HASH='%s'", hash)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, "ADMIN_PASSWORD_HASH=") {
			lines[i] = line
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, line)
	}
	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}
