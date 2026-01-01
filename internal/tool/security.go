package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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

	// Update .env file: single-quote the hash to prevent bash $-expansion
	if _, err := os.Stat(cfg.EnvFile); err == nil {
		data, err := os.ReadFile(cfg.EnvFile)
		if err != nil {
			ui.StepFail("Failed to read .env: " + err.Error())
			ui.PressAnyKey()
			return
		}

		lines := strings.Split(string(data), "\n")
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, "ADMIN_PASSWORD_HASH=") {
				lines[i] = fmt.Sprintf("ADMIN_PASSWORD_HASH='%s'", hash)
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, fmt.Sprintf("ADMIN_PASSWORD_HASH='%s'", hash))
		}

		// Remove trailing empty lines that may have accumulated, then rejoin
		newContent := strings.Join(lines, "\n")
		if err := os.WriteFile(cfg.EnvFile, []byte(newContent), 0600); err != nil {
			ui.StepFail("Failed to write .env: " + err.Error())
			ui.PressAnyKey()
			return
		}
		if found {
			ui.StepOk("Updated ADMIN_PASSWORD_HASH in .env")
		} else {
			ui.StepOk("Added ADMIN_PASSWORD_HASH to .env")
		}
	}

	// Sync .env to install directory for systemd
	if err := cfg.SyncEnvToInstallDir(); err != nil {
		ui.StepWarn("Failed to sync .env to install dir: " + err.Error())
	}

	// Update database if accessible
	dbAccessible := isDBAccessible(cfg)
	if dbAccessible {
		// Single-quote escape: replace ' with ''
		escapedHash := strings.ReplaceAll(hash, "'", "''")

		// Get admin username from .env
		adminUser := ReadEnvValue("ADMIN_USERNAME", cfg.EnvFile)
		if adminUser == "" {
			adminUser = "admin"
		}

		escapedUser := strings.ReplaceAll(adminUser, "'", "''")
		sql := fmt.Sprintf("UPDATE admin_credentials SET password_hash = '%s' WHERE username = '%s';",
			escapedHash, escapedUser)
		cmd := cfg.DBExec(sql)
		if err := cmd.Run(); err != nil {
			ui.StepWarn("Could not update database (table may not exist yet)")
		} else {
			ui.StepOk("Updated password in database")
		}
	}

	fmt.Println()
	ui.StepWarn("Restart the backend for changes to take effect")
	ui.PressAnyKey()
}

func ActionCreateAdminUser(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Create Admin User")

	if !isDBAccessible(cfg) {
		ui.StepFail("Database is not accessible")
		ui.PressAnyKey()
		return
	}

	username, err := ui.InputString("Username", "admin")
	if err != nil {
		return
	}
	if username == "" {
		ui.StepFail("Username is required")
		ui.PressAnyKey()
		return
	}

	telegramIDStr, err := ui.InputStringDefault("Telegram ID (or 0 for none)", "0")
	if err != nil {
		return
	}
	if telegramIDStr == "" {
		telegramIDStr = "0"
	}

	// Validate numeric
	for _, ch := range telegramIDStr {
		if ch < '0' || ch > '9' {
			ui.StepFail("Telegram ID must be a number")
			ui.PressAnyKey()
			return
		}
	}

	// If telegram_id is 0, use negative nanosecond timestamp as placeholder
	dbTelegramID := telegramIDStr
	if telegramIDStr == "0" {
		nanos := time.Now().UnixNano()
		// Truncate to 15 digits to match bash head -c 15 on date +%s%N
		nanosStr := fmt.Sprintf("%d", nanos)
		if len(nanosStr) > 15 {
			nanosStr = nanosStr[:15]
		}
		dbTelegramID = "-" + nanosStr
	}

	ui.StepInfo(fmt.Sprintf("Creating admin user: %s (telegram_id: %s)", username, telegramIDStr))

	var sql string
	if cfg.IsSQLite() {
		sql = fmt.Sprintf(
			"INSERT INTO users (telegram_id, username, first_name, is_admin, created_at, updated_at) "+
				"VALUES (%s, '%s', '%s', 1, datetime('now'), datetime('now')) "+
				"ON CONFLICT DO NOTHING;",
			dbTelegramID,
			strings.ReplaceAll(username, "'", "''"),
			strings.ReplaceAll(username, "'", "''"),
		)
	} else {
		sql = fmt.Sprintf(
			"INSERT INTO users (telegram_id, username, first_name, is_admin, created_at, updated_at) "+
				"VALUES (%s, '%s', '%s', true, NOW(), NOW()) "+
				"ON CONFLICT DO NOTHING RETURNING id;",
			dbTelegramID,
			strings.ReplaceAll(username, "'", "''"),
			strings.ReplaceAll(username, "'", "''"),
		)
	}

	cmd := cfg.DBExec(sql)
	out, err := cmd.Output()
	result := strings.TrimSpace(string(out))

	if err != nil {
		ui.StepFail("Failed to create user: " + err.Error())
	} else if cfg.IsSQLite() {
		// SQLite ON CONFLICT DO NOTHING does not return rows; treat as success
		ui.StepOk("Admin user created: " + username)
	} else if result != "" && isNumeric(result) {
		ui.StepOk("Admin user created with ID: " + result)
	} else if result == "" {
		// ON CONFLICT DO NOTHING triggered — user already exists
		ui.StepWarn("User already exists (conflict on insert)")
	} else {
		ui.StepFail("Failed to create user: " + result)
	}

	ui.PressAnyKey()
}

// ── helpers ──────────────────────────────────────────────────────────────────

// isDBAccessible checks if the database is reachable based on deploy mode and driver.
func isDBAccessible(cfg *Config) bool {
	if cfg.IsSQLite() {
		if cfg.IsSystemd() {
			_, err := os.Stat(cfg.DBPath)
			return err == nil
		}
		return isContainerRunning(DefaultContainerBackend)
	}
	// PostgreSQL
	if cfg.IsSystemd() {
		cmd := pgIsReadyCmdFor(cfg)
		return cmd.Run() == nil
	}
	return isContainerRunning(DefaultContainerDB)
}

func pgIsReadyCmdFor(cfg *Config) *exec.Cmd {
	cmd := exec.Command("pg_isready", "-h", cfg.DBHost, "-U", cfg.DBUser)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.DBPassword)
	return cmd
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
