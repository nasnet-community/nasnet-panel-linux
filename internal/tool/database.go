package tool

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

func ActionDBBackup(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Create Database Backup")

	if err := os.MkdirAll(cfg.BackupDir, 0755); err != nil {
		ui.StepFail("Failed to create backup directory: " + err.Error())
		ui.PressAnyKey()
		return
	}

	ext := "sql"
	spinnerMsg := "Running pg_dump"
	if cfg.IsSQLite() {
		ext = "db"
		spinnerMsg = "Copying SQLite database"
	}

	filename := fmt.Sprintf("backup_%s.%s", time.Now().Format("20060102_150405"), ext)
	fpath := filepath.Join(cfg.BackupDir, filename)

	ui.StepInfo("Creating backup: " + filename)

	if err := ui.Spinner(spinnerMsg, cfg.PGDumpToFile(fpath)); err != nil {
		_ = os.Remove(fpath)
		ui.StepFail("Backup failed")
	} else {
		size := fileSizeHuman(fpath)
		ui.StepOk(fmt.Sprintf("Backup created: %s (%s)", filename, size))
	}
	ui.PressAnyKey()
}

func ActionDBRestore(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Restore Database")

	backups := listBackupFiles(cfg.BackupDir)
	if len(backups) == 0 {
		ui.StepWarn("No backups found in " + cfg.BackupDir)
		ui.PressAnyKey()
		return
	}

	choice, err := ui.Menu("Select backup to restore", backups)
	if err != nil || choice < 0 {
		return
	}

	selected := backups[choice]

	ui.ClearScreen()
	ui.DrawHeader("Restore: " + selected)

	confirmed, err := ui.ConfirmDangerous("This will DROP the current database and restore from backup.", "RESTORE")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// Create safety backup first
	if err := os.MkdirAll(cfg.BackupDir, 0755); err != nil {
		ui.StepFail("Failed to create backup directory: " + err.Error())
		ui.PressAnyKey()
		return
	}

	safetyExt := "sql"
	if cfg.IsSQLite() {
		safetyExt = "db"
	}
	safety := fmt.Sprintf("pre_restore_%s.%s", time.Now().Format("20060102_150405"), safetyExt)
	safetyPath := filepath.Join(cfg.BackupDir, safety)

	ui.StepInfo("Creating safety backup...")
	if err := ui.Spinner("Safety backup", cfg.PGDumpToFile(safetyPath)); err != nil {
		ui.StepFail("Safety backup failed - aborting restore")
		ui.PressAnyKey()
		return
	}
	ui.StepOk("Safety backup: " + safety)

	selectedPath := filepath.Join(cfg.BackupDir, selected)

	if cfg.IsSQLite() {
		ui.StepInfo("Restoring from " + selected + "...")
		if err := restoreSQLite(cfg, selectedPath); err != nil {
			ui.StepFail("Restore failed - safety backup: " + safety)
		} else {
			ui.StepOk("Database restored successfully")
			ui.StepInfo("Safety backup available: " + safety)
			fmt.Println()
			ui.StepWarn("You may need to restart the backend for changes to take effect")
		}
	} else {
		ui.StepInfo("Dropping schema...")
		dropCmd := cfg.PSQLExec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
		if err := dropCmd.Run(); err != nil {
			ui.StepFail("Schema reset failed - safety backup: " + safety)
			ui.PressAnyKey()
			return
		}
		ui.StepOk("Schema reset")

		ui.StepInfo("Restoring from " + selected + "...")
		if err := ui.Spinner("Restoring database", psqlRestoreFromFile(cfg, selectedPath)); err != nil {
			ui.StepFail("Restore failed - safety backup: " + safety)
		} else {
			ui.StepOk("Database restored successfully")
			ui.StepInfo("Safety backup available: " + safety)
			fmt.Println()
			ui.StepWarn("You may need to restart the backend for changes to take effect")
		}
	}
	ui.PressAnyKey()
}

func ActionDBWipe(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Wipe Database")

	fmt.Printf("  %s\n", ui.StyleError.Bold(true).Render("This will permanently destroy ALL data!"))
	if cfg.IsSQLite() {
		fmt.Printf("  %s\n", ui.StyleDim.Render("This deletes the SQLite database file."))
	} else {
		fmt.Printf("  %s\n", ui.StyleDim.Render("This drops and recreates the public schema."))
	}
	fmt.Printf("  %s\n", ui.StyleDim.Render("The application will recreate tables on restart."))
	fmt.Println()

	confirmed, err := ui.ConfirmDangerous("Permanently wipe all database data?", "WIPE-ALL-DATA")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// Safety backup
	if err := os.MkdirAll(cfg.BackupDir, 0755); err != nil {
		ui.StepFail("Failed to create backup directory: " + err.Error())
		ui.PressAnyKey()
		return
	}

	safetyExt := "sql"
	if cfg.IsSQLite() {
		safetyExt = "db"
	}
	safety := fmt.Sprintf("pre_wipe_%s.%s", time.Now().Format("20060102_150405"), safetyExt)
	safetyPath := filepath.Join(cfg.BackupDir, safety)

	ui.StepInfo("Creating safety backup before wipe...")
	if err := ui.Spinner("Safety backup", cfg.PGDumpToFile(safetyPath)); err != nil {
		ui.StepFail("Safety backup failed - aborting")
		ui.PressAnyKey()
		return
	}
	ui.StepOk("Safety backup: " + safety)

	ui.StepInfo("Wiping database...")
	if cfg.IsSQLite() {
		if err := wipeSQLite(cfg); err != nil {
			ui.StepFail("Wipe failed: " + err.Error())
		} else {
			ui.StepOk("Database wiped")
			ui.StepInfo("Safety backup: " + safety)
			fmt.Println()
			ui.StepWarn("Restart the backend to recreate tables")
		}
	} else {
		dropCmd := cfg.PSQLExec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
		if err := dropCmd.Run(); err != nil {
			ui.StepFail("Wipe failed")
		} else {
			ui.StepOk("Database wiped")
			ui.StepInfo("Safety backup: " + safety)
			fmt.Println()
			ui.StepWarn("Restart the backend to recreate tables")
		}
	}
	ui.PressAnyKey()
}

func ActionDBShell(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Database Shell")

	if cfg.IsSQLite() {
		fmt.Printf("  %s\n\n", ui.StyleDim.Render("Entering sqlite3 session... Type .quit to exit"))
	} else {
		fmt.Printf("  %s\n\n", ui.StyleDim.Render(`Entering psql session... Type \q to exit`))
	}

	cmd := cfg.DBShellCmd()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	ui.PressAnyKey()
}

func ActionDBListBackups(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Database Backups")

	if _, err := os.Stat(cfg.BackupDir); os.IsNotExist(err) {
		ui.StepWarn("Backup directory does not exist: " + cfg.BackupDir)
		ui.PressAnyKey()
		return
	}

	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil {
		ui.StepFail("Failed to read backup directory: " + err.Error())
		ui.PressAnyKey()
		return
	}

	type backupEntry struct {
		name  string
		size  int64
		mtime time.Time
	}

	var bkups []backupEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") && !strings.HasSuffix(name, ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bkups = append(bkups, backupEntry{
			name:  name,
			size:  info.Size(),
			mtime: info.ModTime(),
		})
	}

	// Sort by name descending (newest first)
	sort.Slice(bkups, func(i, j int) bool {
		return bkups[i].name > bkups[j].name
	})

	if len(bkups) == 0 {
		ui.StepWarn("No backups found")
		ui.PressAnyKey()
		return
	}

	var rows [][]string
	var totalSize int64
	for _, b := range bkups {
		rows = append(rows, []string{
			b.name,
			bytesHuman(b.size),
			b.mtime.Format("2006-01-02 15:04"),
		})
		totalSize += b.size
	}

	ui.Table([]string{"Filename", "Size", "Date"}, rows)
	fmt.Println()
	ui.StepInfo(fmt.Sprintf("%d backup(s), total: %s", len(bkups), bytesHuman(totalSize)))

	ui.PressAnyKey()
}

// ── helpers ──────────────────────────────────────────────────────────────────

// listBackupFiles returns backup filenames sorted descending (newest first).
func listBackupFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".sql") || strings.HasSuffix(n, ".db") {
			names = append(names, n)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

// psqlRestoreFromFile returns a command that pipes the file into psql.
func psqlRestoreFromFile(cfg *Config, fpath string) *exec.Cmd {
	if cfg.IsDocker() {
		return exec.Command("bash", "-c",
			fmt.Sprintf("cat %q | docker exec -i %s psql -U %s -d %s --single-transaction -q",
				fpath, DefaultContainerDB, cfg.DBUser, cfg.DBName))
	}
	return exec.Command("bash", "-c",
		fmt.Sprintf("PGPASSWORD='%s' psql -h %s -U %s -d %s --single-transaction -q < %q",
			cfg.DBPassword, cfg.DBHost, cfg.DBUser, cfg.DBName, fpath))
}

// restoreSQLite copies a backup file over the SQLite database (no spinner, small file).
func restoreSQLite(cfg *Config, src string) error {
	if cfg.IsDocker() {
		cmd := exec.Command("docker", "cp", src,
			fmt.Sprintf("%s:%s", DefaultContainerBackend, cfg.DBPath))
		return cmd.Run()
	}
	return copyFile(src, cfg.DBPath)
}

// wipeSQLite removes the SQLite database file.
func wipeSQLite(cfg *Config) error {
	if cfg.IsDocker() {
		cmd := exec.Command("docker", "exec", DefaultContainerBackend, "rm", "-f", cfg.DBPath)
		return cmd.Run()
	}
	return os.Remove(cfg.DBPath)
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fileSizeHuman returns a human-readable file size string.
func fileSizeHuman(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return bytesHuman(info.Size())
}

// bytesHuman is in wizard_helpers.go
