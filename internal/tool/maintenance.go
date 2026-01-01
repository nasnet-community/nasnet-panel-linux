package tool

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// ActionCleanDockerLogs truncates container log files to reclaim disk space.
func ActionCleanDockerLogs(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Clean Docker Logs")

	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		ui.PressAnyKey()
		return
	}

	containers := []string{DefaultContainerBackend, DefaultContainerDB}

	type logEntry struct {
		container string
		logPath   string
		size      string
		sizeBytes int64
	}

	var entries []logEntry
	var totalBytes int64

	for _, container := range containers {
		out, err := exec.Command("docker", "inspect", "--format={{.LogPath}}", container).Output()
		logPath := ""
		if err == nil {
			logPath = strings.TrimSpace(string(out))
		}

		var sizeStr string
		var sizeBytes int64

		if logPath != "" {
			fi, err := os.Stat(logPath)
			if err == nil {
				sizeBytes = fi.Size()
				totalBytes += sizeBytes
				sizeStr = bytesHuman(sizeBytes)
			} else {
				sizeStr = ui.StyleDim.Render("no log file")
			}
		} else {
			sizeStr = ui.StyleDim.Render("not running")
		}

		entries = append(entries, logEntry{
			container: container,
			logPath:   logPath,
			size:      sizeStr,
			sizeBytes: sizeBytes,
		})
	}

	rows := make([][]string, len(entries))
	for i, e := range entries {
		rows[i] = []string{e.container, e.size}
	}
	ui.Table([]string{"Container", "Log Size"}, rows)

	if totalBytes == 0 {
		ui.StepInfo("No logs to clean")
		ui.PressAnyKey()
		return
	}

	fmt.Println()
	confirmed, err := ui.Confirm("Truncate all container logs?")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	for _, e := range entries {
		if e.logPath == "" || e.sizeBytes == 0 {
			continue
		}
		truncCmd := exec.Command("sudo", "truncate", "-s", "0", e.logPath)
		if runErr := truncCmd.Run(); runErr != nil {
			ui.StepFail(fmt.Sprintf("Failed to truncate %s (may need sudo)", e.container))
		} else {
			ui.StepOk(fmt.Sprintf("Truncated logs for %s", e.container))
		}
	}

	fmt.Println()
	ui.StepOk(fmt.Sprintf("Space reclaimed: ~%s", bytesHuman(totalBytes)))
	ui.PressAnyKey()
}

// ActionCleanOldBackups removes old backup files keeping only the N most recent.
func ActionCleanOldBackups(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Clean Old Backups")

	if _, err := os.Stat(cfg.BackupDir); err != nil {
		ui.StepWarn("No backup directory found")
		ui.PressAnyKey()
		return
	}

	// Collect .sql and .db files
	var backups []string
	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil {
		ui.StepFail(fmt.Sprintf("Cannot read backup directory: %v", err))
		ui.PressAnyKey()
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".db") {
			backups = append(backups, filepath.Join(cfg.BackupDir, name))
		}
	}

	// Sort descending by filename (newest first when names contain timestamps)
	sort.Slice(backups, func(i, j int) bool {
		return filepath.Base(backups[i]) > filepath.Base(backups[j])
	})

	count := len(backups)
	if count == 0 {
		ui.StepInfo("No backups found")
		ui.PressAnyKey()
		return
	}

	ui.StepInfo(fmt.Sprintf("Found %d backup(s)", count))
	fmt.Println()

	keepStr, err := ui.InputStringDefault("How many backups to keep?", "10")
	if err != nil {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	keepStr = strings.TrimSpace(keepStr)
	keep, parseErr := strconv.Atoi(keepStr)
	if parseErr != nil || keep < 0 {
		ui.StepFail("Invalid number")
		ui.PressAnyKey()
		return
	}

	if count <= keep {
		ui.StepInfo(fmt.Sprintf("Only %d backup(s) exist, keeping all", count))
		ui.PressAnyKey()
		return
	}

	toDelete := count - keep
	fmt.Println()
	ui.StepInfo(fmt.Sprintf("Will delete %d oldest backup(s):", toDelete))

	for i := keep; i < count; i++ {
		fi, statErr := os.Stat(backups[i])
		sizeStr := ""
		if statErr == nil {
			sizeStr = fmt.Sprintf(" (%s)", bytesHuman(fi.Size()))
		}
		fmt.Printf("    %s %s%s\n", ui.StyleError.Render("✘"), filepath.Base(backups[i]), sizeStr)
	}

	fmt.Println()
	confirmed, err := ui.Confirm(fmt.Sprintf("Delete these %d backup(s)?", toDelete))
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	deleted := 0
	for i := keep; i < count; i++ {
		if removeErr := os.Remove(backups[i]); removeErr == nil {
			deleted++
		}
	}

	ui.StepOk(fmt.Sprintf("Deleted %d backup(s)", deleted))
	ui.PressAnyKey()
}

// ActionDockerPrune removes unused Docker resources.
func ActionDockerPrune(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Docker Prune")

	if err := RequireDocker(); err != nil {
		ui.StepFail(err.Error())
		ui.PressAnyKey()
		return
	}

	ui.StepInfo("Reclaimable space estimate:")
	fmt.Println()
	dfOut, _ := exec.Command("docker", "system", "df").Output()
	fmt.Print(string(dfOut))

	fmt.Println()
	confirmed, err := ui.Confirm("Run docker system prune (removes unused images, networks, build cache)?")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	fmt.Println()
	pruneCmd := exec.Command("docker", "system", "prune", "-f")
	if spinErr := ui.Spinner("Pruning Docker", pruneCmd); spinErr != nil {
		ui.StepFail("Prune failed")
	} else {
		ui.StepOk("Docker prune complete")
	}

	fmt.Println()
	ui.StepInfo("Updated disk usage:")
	dfOut2, _ := exec.Command("docker", "system", "df").Output()
	fmt.Print(string(dfOut2))

	ui.PressAnyKey()
}

// ActionCleanJournalLogs vacuums systemd journal logs to keep the last 7 days.
func ActionCleanJournalLogs(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Clean Journal Logs")

	if _, err := exec.LookPath("journalctl"); err != nil {
		ui.StepFail("journalctl not found")
		ui.PressAnyKey()
		return
	}

	// Show total journal usage
	diskOut, _ := exec.Command("journalctl", "--disk-usage").Output()
	if len(diskOut) > 0 {
		fields := strings.Fields(string(diskOut))
		total := "unknown"
		if len(fields) > 0 {
			total = fields[len(fields)-1]
		}
		ui.StepInfo(fmt.Sprintf("Total journal usage: %s", total))
	}

	// Show per-service usage in a table
	beOut, _ := exec.Command("journalctl", "-u", DefaultBackendService, "--disk-usage").Output()
	beSize := "N/A"
	if len(beOut) > 0 {
		fields := strings.Fields(string(beOut))
		if len(fields) > 0 {
			beSize = fields[len(fields)-1]
		}
	}

	fmt.Println()
	ui.Table(
		[]string{"Service", "Log Size"},
		[][]string{{DefaultBackendService, beSize}},
	)

	fmt.Println()
	confirmed, err := ui.Confirm("Vacuum journal logs (keep last 7 days)?")
	if err != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	vacCmd := exec.Command("sudo", "journalctl", "--vacuum-time=7d")
	if runErr := vacCmd.Run(); runErr != nil {
		ui.StepFail("Failed to vacuum journal logs")
	} else {
		ui.StepOk("Journal logs vacuumed (kept last 7 days)")
	}

	// Show new usage
	newOut, _ := exec.Command("journalctl", "--disk-usage").Output()
	if len(newOut) > 0 {
		fields := strings.Fields(string(newOut))
		newSize := "unknown"
		if len(fields) > 0 {
			newSize = fields[len(fields)-1]
		}
		ui.StepInfo(fmt.Sprintf("Journal usage now: %s", newSize))
	}

	ui.PressAnyKey()
}

// humanBytes is an alias for bytesHuman in wizard_helpers.go
