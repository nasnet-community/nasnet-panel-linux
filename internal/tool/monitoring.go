package tool

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

func ActionHealthCheck(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Health Check")

	webPort := os.Getenv("WEB_PORT")
	if webPort == "" {
		webPort = "3000"
	}

	var rows [][]string

	// Check Backend API
	apiURL := fmt.Sprintf("http://localhost:%s/health/ready", cfg.AppPort)
	apiStatus := checkHTTP(apiURL)
	rows = append(rows, []string{"Backend API", apiURL, apiStatus})

	// Check Frontend
	frontURL := fmt.Sprintf("http://localhost:%s", webPort)
	frontStatus := checkHTTP(frontURL)
	rows = append(rows, []string{"Frontend", frontURL, frontStatus})

	// Check Database (mode-aware, driver-aware)
	if cfg.IsSQLite() {
		if cfg.IsSystemd() {
			if _, err := os.Stat(cfg.DBPath); err == nil {
				dbStatus := ui.StyleSuccess.Render("● Ready")
				rows = append(rows, []string{"Database", fmt.Sprintf("SQLite (%s)", cfg.DBPath), dbStatus})
			} else {
				dbStatus := ui.StyleWarning.Render("● No DB file")
				rows = append(rows, []string{"Database", fmt.Sprintf("SQLite (%s)", cfg.DBPath), dbStatus})
			}
		} else {
			if isContainerRunning(DefaultContainerBackend) {
				dbStatus := ui.StyleSuccess.Render("● Ready")
				rows = append(rows, []string{"Database", "SQLite (embedded)", dbStatus})
			} else {
				dbStatus := ui.StyleError.Render("● Container down")
				rows = append(rows, []string{"Database", "SQLite (embedded)", dbStatus})
			}
		}
	} else {
		if cfg.IsSystemd() {
			cmd := exec.Command("pg_isready", "-h", cfg.DBHost, "-U", cfg.DBUser)
			cmd.Env = append(os.Environ(), "PGPASSWORD="+cfg.DBPassword)
			if cmd.Run() == nil {
				dbStatus := ui.StyleSuccess.Render("● Ready")
				rows = append(rows, []string{"Database", "PostgreSQL (local)", dbStatus})
			} else {
				dbStatus := ui.StyleError.Render("● Unreachable")
				rows = append(rows, []string{"Database", "PostgreSQL (local)", dbStatus})
			}
		} else {
			pgCmd := exec.Command("docker", "exec", DefaultContainerDB,
				"pg_isready", "-U", cfg.DBUser)
			if pgCmd.Run() == nil {
				dbStatus := ui.StyleSuccess.Render("● Ready")
				rows = append(rows, []string{"Database", DefaultContainerDB, dbStatus})
			} else {
				dbStatus := ui.StyleError.Render("● Unreachable")
				rows = append(rows, []string{"Database", DefaultContainerDB, dbStatus})
			}
		}
	}

	// Infrastructure status (mode-aware)
	if cfg.IsSystemd() {
		beStatus := systemctlIsActive(DefaultBackendService)
		rows = append(rows, []string{DefaultBackendService, "systemd", beStatus})

		feStatus := systemctlIsActive("nasnet-panel-frontend")
		rows = append(rows, []string{"nasnet-panel-frontend", "systemd", feStatus})
	} else {
		if IsDockerRunning() {
			rows = append(rows, []string{"Docker", "daemon", ui.StyleSuccess.Render("● Running")})
		} else {
			rows = append(rows, []string{"Docker", "daemon", ui.StyleError.Render("● Not running")})
		}
	}

	ui.Table([]string{"Service", "Endpoint", "Status"}, rows)
	ui.PressAnyKey()
}

func ActionSystemInfo(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("System Info")

	var rows [][]string

	// Runtime info (mode-aware)
	if cfg.IsDocker() {
		dockerVer := runOutputTrimmed("docker", "version", "--format", "{{.Server.Version}}")
		if dockerVer == "" {
			dockerVer = "N/A"
		}
		composeVer := runOutputTrimmed("docker", "compose", "version", "--short")
		if composeVer == "" {
			composeVer = "N/A"
		}
		rows = append(rows, []string{"Deploy Mode", "Docker"})
		rows = append(rows, []string{"Docker", dockerVer})
		rows = append(rows, []string{"Docker Compose", composeVer})
	} else {
		goVer := runOutputTrimmed("go", "version")
		if goVer == "" {
			goVer = "N/A"
		} else {
			// "go version go1.25.6 linux/amd64" -> extract go1.25.6
			parts := strings.Fields(goVer)
			if len(parts) >= 3 {
				goVer = parts[2]
			}
		}
		nodeVer := runOutputTrimmed("node", "--version")
		if nodeVer == "" {
			nodeVer = "N/A"
		}
		rows = append(rows, []string{"Deploy Mode", "Systemd (bare-metal)"})
		rows = append(rows, []string{"Go", goVer})
		rows = append(rows, []string{"Node.js", nodeVer})
		rows = append(rows, []string{"Install Dir", cfg.InstallDir})
	}

	// Database info (driver-aware)
	if cfg.IsSQLite() {
		rows = append(rows, []string{"Database", "SQLite"})
		if cfg.IsSystemd() {
			if _, err := os.Stat(cfg.DBPath); err == nil {
				dbSize := runOutputTrimmed("du", "-h", cfg.DBPath)
				if dbSize != "" {
					dbSize = strings.Fields(dbSize)[0]
				} else {
					dbSize = "N/A"
				}
				rows = append(rows, []string{"Database Size", dbSize})
			}
		} else {
			out, err := exec.Command("docker", "exec", DefaultContainerBackend,
				"du", "-h", cfg.DBPath).Output()
			if err == nil {
				parts := strings.Fields(string(out))
				if len(parts) > 0 {
					rows = append(rows, []string{"Database Size", parts[0]})
				}
			} else {
				rows = append(rows, []string{"Database Size", "N/A"})
			}
		}
	} else {
		pgVerCmd := cfg.PSQLExec("SELECT version();")
		pgOut, err := pgVerCmd.Output()
		pgVer := "N/A"
		if err == nil {
			line := strings.TrimSpace(string(pgOut))
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				pgVer = parts[0] + " " + parts[1]
			} else if len(parts) > 0 {
				pgVer = parts[0]
			}
		}
		rows = append(rows, []string{"PostgreSQL", pgVer})

		dbSizeCmd := cfg.PSQLExec("SELECT pg_size_pretty(pg_database_size(current_database()));")
		dbSizeOut, err := dbSizeCmd.Output()
		dbSize := "N/A"
		if err == nil {
			dbSize = strings.TrimSpace(string(dbSizeOut))
		}
		rows = append(rows, []string{"Database Size", dbSize})
	}

	// Git info
	gitVer := runOutputInDir(cfg.ProjectDir, "git", "describe", "--tags", "--always", "--dirty")
	if gitVer == "" {
		gitVer = "N/A"
	}
	gitBranch := runOutputInDir(cfg.ProjectDir, "git", "branch", "--show-current")
	if gitBranch == "" {
		gitBranch = "N/A"
	}
	rows = append(rows, []string{"Git Version", fmt.Sprintf("%s (%s)", gitVer, gitBranch)})

	// System uptime
	uptimeOut := runOutputTrimmed("uptime")
	sysUptime := "N/A"
	if uptimeOut != "" {
		// Extract the "up ..." part
		if idx := strings.Index(uptimeOut, " up "); idx >= 0 {
			after := uptimeOut[idx+4:]
			if idx2 := strings.Index(after, ", load"); idx2 >= 0 {
				after = after[:idx2]
			}
			sysUptime = strings.TrimSpace(after)
		} else {
			sysUptime = uptimeOut
		}
	}
	rows = append(rows, []string{"System Uptime", sysUptime})

	ui.Table([]string{"Component", "Value"}, rows)
	ui.PressAnyKey()
}

func ActionDiskUsage(cfg *Config) {
	ui.ClearScreen()
	ui.DrawHeader("Disk Usage")

	var rows [][]string

	if cfg.IsSystemd() {
		// Install directory
		if _, err := os.Stat(cfg.InstallDir); err == nil {
			installSize := duSH(cfg.InstallDir)
			rows = append(rows, []string{cfg.InstallDir + "/", installSize})
		}

		// Binary
		binPath := cfg.InstallDir + "/bin/nasnet-panel"
		if _, err := os.Stat(binPath); err == nil {
			binSize := duH(binPath)
			rows = append(rows, []string{"Backend binary", binSize})
		}

		// Web panel
		webPanel := cfg.InstallDir + "/web-panel"
		if _, err := os.Stat(webPanel); err == nil {
			wpSize := duSH(webPanel)
			rows = append(rows, []string{"Web panel", wpSize})
		}

		// Database
		if cfg.IsSQLite() {
			if _, err := os.Stat(cfg.DBPath); err == nil {
				sqliteSize := duH(cfg.DBPath)
				rows = append(rows, []string{"SQLite database", sqliteSize})
			}
		} else {
			pgData := runOutputTrimmed("sudo", "du", "-sh", "/var/lib/postgresql")
			if pgData != "" {
				parts := strings.Fields(pgData)
				if len(parts) > 0 {
					rows = append(rows, []string{"PostgreSQL data", parts[0]})
				}
			}
		}
	} else {
		// Docker: data directory
		dataDir := cfg.ProjectDir + "/data"
		if _, err := os.Stat(dataDir); err == nil {
			dataSize := duSH(dataDir)
			rows = append(rows, []string{"data/", dataSize})
		}
	}

	// Backups (works for both)
	if _, err := os.Stat(cfg.BackupDir); err == nil {
		backupSize := duSH(cfg.BackupDir)
		// Count backup files
		entries, _ := os.ReadDir(cfg.BackupDir)
		count := 0
		for _, e := range entries {
			if !e.IsDir() {
				name := e.Name()
				if strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".db") {
					count++
				}
			}
		}
		rows = append(rows, []string{"Backups", fmt.Sprintf("%s (%d files)", backupSize, count)})
	}

	if cfg.IsDocker() {
		// Docker volumes
		volPg := dockerVolumeSize("nasnet.*postgres_data")
		if volPg != "" {
			rows = append(rows, []string{"Docker: postgres_data", volPg})
		}

		volAcme := dockerVolumeSize("nasnet.*acme_data")
		if volAcme != "" {
			rows = append(rows, []string{"Docker: acme_data", volAcme})
		}

		// Docker images (nasnet related)
		imgOut, err := exec.Command("docker", "images",
			"--filter", "reference=*nasnet*",
			"--format", "{{.Repository}}:{{.Tag}} {{.Size}}").Output()
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(imgOut)), "\n") {
				if line == "" {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					rows = append(rows, []string{"Image: " + parts[0], parts[1]})
				}
			}
		}

		// Docker total disk usage
		dfOut, err := exec.Command("docker", "system", "df",
			"--format", "{{.Type}}\t{{.Size}}").Output()
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(dfOut)), "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 2)
				if len(parts) == 2 {
					rows = append(rows, []string{"Docker " + parts[0], parts[1]})
				}
			}
		}
	}

	if len(rows) > 0 {
		ui.Table([]string{"Path / Resource", "Size"}, rows)
	} else {
		ui.StepWarn("Could not determine disk usage")
	}
	ui.PressAnyKey()
}

// ── helpers ──────────────────────────────────────────────────────────────────

func checkHTTP(url string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err == nil {
		resp.Body.Close()
		return ui.StyleSuccess.Render("● Healthy")
	}
	return ui.StyleError.Render("● Unreachable")
}

func isContainerRunning(name string) bool {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func systemctlIsActive(service string) string {
	out, err := exec.Command("systemctl", "is-active", service).Output()
	status := strings.TrimSpace(string(out))
	if err == nil && status == "active" {
		return ui.StyleSuccess.Render("● Running")
	}
	if status == "" {
		status = "inactive"
	}
	return ui.StyleError.Render("● " + status)
}

func runOutputTrimmed(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runOutputInDir(dir, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func duSH(path string) string {
	out, err := exec.Command("du", "-sh", path).Output()
	if err != nil {
		return "N/A"
	}
	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		return parts[0]
	}
	return "N/A"
}

func duH(path string) string {
	out, err := exec.Command("du", "-h", path).Output()
	if err != nil {
		return "N/A"
	}
	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		return parts[0]
	}
	return "N/A"
}

func dockerVolumeSize(patterns ...string) string {
	out, err := exec.Command("docker", "system", "df", "-v").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		for _, pat := range patterns {
			if matchesGlob(line, pat) {
				parts := strings.Fields(line)
				if len(parts) >= 4 {
					return parts[3]
				}
			}
		}
	}
	return ""
}

func matchesGlob(s, pattern string) bool {
	// Simple substring match for our purposes
	return strings.Contains(s, strings.ReplaceAll(pattern, ".*", ""))
}
