package tool

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

// ─── WizardUpdate (git-based update) ─────────────────────────────────────────

// WizardUpdate performs a git-based update of nasnet-panel from the remote repository.
func WizardUpdate(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("nasnet-panel Update")

	// Check git availability.
	if _, err := exec.LookPath("git"); err != nil {
		ui.StepFail("git is not installed")
		ui.PressAnyKey()
		return
	}

	deployMode := ReadEnvValue("DEPLOY_MODE", cfg.EnvFile)
	if deployMode == "" {
		deployMode = "docker"
	}

	// For docker mode, validate compose is available.
	if deployMode == "docker" {
		if _, err := ComposeCmd(); err != nil {
			ui.StepFail("docker compose not found")
			ui.PressAnyKey()
			return
		}
	}

	// ── Current version ───────────────────────────────────────────────────
	ui.DrawHeader("Current Version")

	currentVersion := gitOutput(cfg.ProjectDir, "describe", "--tags", "--always", "--dirty")
	if currentVersion == "" {
		currentVersion = "unknown"
	}
	currentBranch := gitOutput(cfg.ProjectDir, "branch", "--show-current")
	if currentBranch == "" {
		currentBranch = "unknown"
	}
	currentCommit := gitOutput(cfg.ProjectDir, "rev-parse", "--short", "HEAD")
	if currentCommit == "" {
		currentCommit = "unknown"
	}

	ui.StepInfo("Version:  " + currentVersion)
	ui.StepInfo("Branch:   " + currentBranch)
	ui.StepInfo("Commit:   " + currentCommit)
	ui.StepInfo("Deploy:   " + deployMode)

	// ── Check for updates ─────────────────────────────────────────────────
	ui.DrawHeader("Checking for Updates")
	ui.StepInfo("Fetching from remote...")

	fetchCmd := exec.Command("git", "-C", cfg.ProjectDir, "fetch", "origin", currentBranch)
	if err := fetchCmd.Run(); err != nil {
		ui.StepFail("Failed to fetch updates")
		ui.PressAnyKey()
		return
	}

	localHash := gitOutput(cfg.ProjectDir, "rev-parse", "HEAD")
	remoteHash := gitOutput(cfg.ProjectDir, "rev-parse", "origin/"+currentBranch)

	needsPull := false
	rebuildOnly := false

	if localHash == remoteHash {
		// Git is up to date — check whether a rebuild is needed.
		needsRebuild := false
		if deployMode == "systemd" {
			backendBinary := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
			if _, err := os.Stat(backendBinary); err != nil {
				needsRebuild = true
			} else {
				// Check if any .go files are newer than the binary.
				goNewer := exec.Command("bash", "-c",
					fmt.Sprintf("find '%s' -name '*.go' -newer '%s' -print -quit 2>/dev/null | grep -q .",
						cfg.ProjectDir, backendBinary))
				if goNewer.Run() == nil {
					needsRebuild = true
				}
			}
		} else {
			// Docker mode: check if the app image exists.
			imgCmd := exec.Command("bash", "-c",
				fmt.Sprintf("docker compose -f '%s' images app --format '{{.ID}}' 2>/dev/null | grep -q .",
					cfg.ComposeFile))
			if imgCmd.Run() != nil {
				needsRebuild = true
			}
		}

		if needsRebuild {
			ui.StepInfo("Code is up to date but artifacts need rebuilding")
			fmt.Println()
			confirmed, err := ui.Confirm("Rebuild and redeploy?")
			if err != nil || !confirmed {
				ui.StepInfo("Cancelled")
				ui.PressAnyKey()
				return
			}
			rebuildOnly = true
		} else {
			ui.StepOk("Already up to date!")
			ui.PressAnyKey()
			return
		}
	} else {
		needsPull = true

		// Count commits behind.
		behindOut := gitOutput(cfg.ProjectDir, "rev-list", "--count", "HEAD..origin/"+currentBranch)
		if behindOut == "" {
			behindOut = "?"
		}
		ui.StepInfo(behindOut + " commit(s) behind origin/" + currentBranch)

		// Show upcoming commits.
		fmt.Println()
		ui.StepInfo("New commits:")
		logOut, _ := exec.Command("git", "-C", cfg.ProjectDir,
			"log", "--oneline", "HEAD..origin/"+currentBranch).Output()
		for _, line := range strings.Split(strings.TrimSpace(string(logOut)), "\n") {
			if line != "" {
				fmt.Printf("    %s\n", ui.StyleDim.Render(line))
			}
		}

		latestVersion := gitOutput(cfg.ProjectDir, "describe", "--tags", "--always", "origin/"+currentBranch)
		if latestVersion == "" {
			latestVersion = remoteHash
		}

		fmt.Println()
		fmt.Printf("  %s → %s\n",
			ui.StyleTitle.Render(currentVersion),
			ui.StyleSuccess.Render(latestVersion))
		fmt.Println()

		confirmed, err := ui.Confirm("Apply update?")
		if err != nil || !confirmed {
			ui.StepInfo("Cancelled")
			ui.PressAnyKey()
			return
		}
	}

	// ── Pre-update backup ─────────────────────────────────────────────────
	ui.DrawHeader("Pre-Update Backup")

	if backupErr := performPreUpdateBackup(cfg, deployMode, "pre_update"); backupErr != nil {
		ui.StepWarn("Backup failed — continuing anyway")
	}

	// ── Pull updates ──────────────────────────────────────────────────────
	if needsPull {
		ui.DrawHeader("Pulling Updates")

		pullCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && git pull --rebase origin '%s'", cfg.ProjectDir, currentBranch))
		if err := ui.RunLogged("Pulling changes", pullCmd); err != nil {
			ui.StepFail("git pull failed — you may have local changes")
			ui.StepInfo(fmt.Sprintf("Try: git stash && git pull --rebase origin %s && git stash pop", currentBranch))
			ui.PressAnyKey()
			return
		}
	}

	// ── Rebuild & Restart ─────────────────────────────────────────────────
	ui.DrawHeader("Rebuilding")

	_ = rebuildOnly // used for flow control above; rebuild proceeds the same way

	if deployMode == "docker" {
		buildCmd := WithBuildEnv(cfg.DockerCompose("build", "app"))
		if err := ui.RunLogged("Building backend image", buildCmd); err != nil {
			ui.StepFail("Backend build failed")
		}
		upCmd := cfg.DockerCompose("up", "-d", "--force-recreate")
		if err := ui.RunLogged("Recreating containers", upCmd); err != nil {
			ui.StepFail("Failed to restart services")
		}
	} else {
		buildCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && make build", cfg.ProjectDir))
		if err := ui.RunLogged("Building nasnet-panel", buildCmd); err != nil {
			ui.StepFail("Build failed")
			ui.PressAnyKey()
			return
		}

		agentCmd := exec.Command("bash", "-c",
			fmt.Sprintf("cd '%s' && make build-agent", cfg.ProjectDir))
		if err := ui.RunLogged("Building agent binaries", agentCmd); err != nil {
			ui.StepFail("Agent binary build failed")
		}

		fmt.Println()
		ui.StepInfo("Stopping services...")
		exec.Command("sudo", "systemctl", "stop", DefaultBackendService).Run() //nolint:errcheck
		ui.StepOk("Backend stopped")

		deployArtifacts(cfg)

		fmt.Println()
		ui.StepInfo("Starting services...")
		if startErr := exec.Command("sudo", "systemctl", "start", DefaultBackendService).Run(); startErr != nil {
			ui.StepFail("Backend start failed")
		} else {
			ui.StepOk("Backend started")
		}
	}

	// ── Post-update summary ───────────────────────────────────────────────
	fmt.Println()
	ui.DrawHeader("Update Complete")

	newVersion := gitOutput(cfg.ProjectDir, "describe", "--tags", "--always")
	if newVersion == "" {
		newVersion = "unknown"
	}

	fmt.Printf("  %s → %s\n",
		ui.StyleTitle.Render(currentVersion),
		ui.StyleSuccess.Render(newVersion))
	fmt.Println()

	if deployMode == "docker" {
		// Show docker compose ps status.
		psCmd := cfg.DockerCompose("ps", "--format", "table {{.Name}}\t{{.Status}}")
		psCmd.Stdout = os.Stdout
		psCmd.Stderr = os.Stderr
		psCmd.Run() //nolint:errcheck
	} else {
		svcStatus, _ := exec.Command("systemctl", "is-active", DefaultBackendService).Output()
		state := strings.TrimSpace(string(svcStatus))
		if state == "" {
			state = "unknown"
		}
		var stateRendered string
		if state == "active" {
			stateRendered = ui.StyleSuccess.Render("● active")
		} else {
			stateRendered = ui.StyleError.Render("● " + state)
		}
		ui.Table(
			[]string{"Service", "Status"},
			[][]string{{DefaultBackendService, stateRendered}},
		)
	}

	ui.PressAnyKey()
}

// ─── ActionAutoUpdate (GitHub release update) ─────────────────────────────────

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// ActionAutoUpdate downloads and installs the latest GitHub release of nasnet-panel.
func ActionAutoUpdate(cfg *Config) {
	ui.ClearScreen()
	ui.DrawBox("nasnet-panel Auto-Update (GitHub Release)")

	// ── Detect current version ─────────────────────────────────────────────
	currentVersion := "unknown"
	versionFile := filepath.Join(cfg.InstallDir, ".version")
	if data, err := os.ReadFile(versionFile); err == nil {
		currentVersion = strings.TrimSpace(string(data))
	} else {
		backendBinary := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
		if _, err2 := os.Stat(backendBinary); err2 == nil {
			out, err3 := exec.Command(backendBinary, "--version").Output()
			if err3 == nil {
				// Parse "vX.Y.Z" from output.
				for _, field := range strings.Fields(string(out)) {
					if strings.HasPrefix(field, "v") && strings.Count(field, ".") >= 2 {
						currentVersion = field
						break
					}
				}
			}
		}
	}
	ui.StepInfo("Current version: " + currentVersion)

	// ── Fetch latest release ───────────────────────────────────────────────
	ui.DrawHeader("Checking GitHub Releases")

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", cfg.GithubRepo)
	rel, err := fetchGithubRelease(apiURL, cfg.GithubToken, wizardHTTPClient(cfg))
	if err != nil {
		ui.StepFail("Failed to fetch latest release from GitHub")
		ui.StepInfo("If this is a private repo, set GITHUB_TOKEN environment variable")
		ui.PressAnyKey()
		return
	}

	latestVersion := rel.TagName
	if latestVersion == "" {
		ui.StepFail("Could not parse release tag from GitHub API response")
		ui.PressAnyKey()
		return
	}
	ui.StepInfo("Latest release: " + latestVersion)

	// ── Compare versions ───────────────────────────────────────────────────
	if currentVersion == latestVersion {
		ui.StepOk("Already up to date!")
		ui.PressAnyKey()
		return
	}

	fmt.Println()
	fmt.Printf("  %s → %s\n",
		ui.StyleTitle.Render(currentVersion),
		ui.StyleSuccess.Render(latestVersion))
	fmt.Println()

	confirmed, err2 := ui.Confirm(fmt.Sprintf("Download and install %s?", latestVersion))
	if err2 != nil || !confirmed {
		ui.StepInfo("Cancelled")
		ui.PressAnyKey()
		return
	}

	// ── Detect architecture ────────────────────────────────────────────────
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		ui.StepFail("Unsupported architecture: " + runtime.GOARCH)
		ui.PressAnyKey()
		return
	}
	ui.StepInfo("Architecture: linux/" + arch)

	// ── Find asset URLs ────────────────────────────────────────────────────
	hubName := "nasnet-panel-linux-" + arch
	toolName := "nasnet-tool-linux-" + arch
	agentName := "nasnet-agent-linux-" + arch
	checksumsName := "checksums.txt"

	hubURL := findAssetURL(rel, hubName)
	toolURL := findAssetURL(rel, toolName)
	agentURL := findAssetURL(rel, agentName)
	checksumsURL := findAssetURL(rel, checksumsName)

	if hubURL == "" || agentURL == "" || checksumsURL == "" {
		ui.StepFail(fmt.Sprintf("Could not find all required assets for linux/%s in release %s",
			arch, latestVersion))
		ui.PressAnyKey()
		return
	}

	// ── Download assets ────────────────────────────────────────────────────
	ui.DrawHeader("Downloading Assets")

	tmpDir, err3 := os.MkdirTemp("", "nasnet-update-*")
	if err3 != nil {
		ui.StepFail("Failed to create temp directory: " + err3.Error())
		ui.PressAnyKey()
		return
	}
	defer os.RemoveAll(tmpDir)

	assetMap := map[string]string{
		hubName:       hubURL,
		agentName:     agentURL,
		checksumsName: checksumsURL,
	}
	// nasnet-tool is optional (may not exist in older releases)
	if toolURL != "" {
		assetMap[toolName] = toolURL
	}

	downloadOrder := []string{hubName, agentName, checksumsName}
	if toolURL != "" {
		downloadOrder = append(downloadOrder[:2], append([]string{toolName}, downloadOrder[2:]...)...)
	}

	dlFailed := false
	for _, name := range downloadOrder {
		url := assetMap[name]
		outPath := filepath.Join(tmpDir, name)

		curlArgs := []string{"-fL", "-o", outPath, url}
		if cfg.GithubToken != "" {
			curlArgs = append([]string{
				"-H", "Authorization: Bearer " + cfg.GithubToken,
				"-H", "Accept: application/octet-stream",
			}, curlArgs...)
		}

		dlCmd := exec.Command("curl", curlArgs...)
		if dlErr := ui.RunLogged("Downloading "+name, dlCmd); dlErr != nil {
			ui.StepFail("Failed to download " + name)
			dlFailed = true
		} else {
			ui.StepOk(name)
		}
	}

	if dlFailed {
		ui.StepFail("Some downloads failed — aborting")
		ui.PressAnyKey()
		return
	}

	// ── Verify checksums ───────────────────────────────────────────────────
	ui.DrawHeader("Verifying Checksums")

	checksumData, err4 := os.ReadFile(filepath.Join(tmpDir, checksumsName))
	if err4 != nil {
		ui.StepFail("Failed to read checksums file")
		ui.PressAnyKey()
		return
	}

	checksumOK := true
	for _, line := range strings.Split(string(checksumData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		expectedHash := fields[0]
		expectedFile := fields[1]

		downloadedPath := filepath.Join(tmpDir, expectedFile)
		if _, err5 := os.Stat(downloadedPath); err5 != nil {
			// File not in our download set; skip.
			continue
		}

		actualHash, hashErr := sha256File(downloadedPath)
		if hashErr != nil {
			ui.StepFail(fmt.Sprintf("%s — could not compute hash", expectedFile))
			checksumOK = false
			continue
		}
		if actualHash == expectedHash {
			ui.StepOk(expectedFile + " checksum OK")
		} else {
			ui.StepFail(expectedFile + " checksum MISMATCH")
			checksumOK = false
		}
	}

	if !checksumOK {
		ui.StepFail("Checksum verification failed — aborting")
		ui.PressAnyKey()
		return
	}

	// ── Pre-update backup ──────────────────────────────────────────────────
	ui.DrawHeader("Pre-Update Backup")

	if backupErr := performPreUpdateBackup(cfg, "systemd", "pre_autoupdate"); backupErr != nil {
		ui.StepWarn("Backup failed — continuing anyway")
	}

	// ── Stop services ──────────────────────────────────────────────────────
	ui.DrawHeader("Stopping Services")

	exec.Command("sudo", "systemctl", "stop", DefaultBackendService).Run() //nolint:errcheck
	ui.StepOk("Backend stopped")

	// ── Deploy binaries ────────────────────────────────────────────────────
	ui.DrawHeader("Deploying " + latestVersion)

	// Create directory structure.
	for _, dir := range []string{
		filepath.Join(cfg.InstallDir, "bin"),
		filepath.Join(cfg.InstallDir, "bin", "agent"),
		filepath.Join(cfg.InstallDir, "bin", "xray"),
		filepath.Join(cfg.InstallDir, "data", "backups"),
		filepath.Join(cfg.InstallDir, "data", "acme"),
	} {
		exec.Command("sudo", "mkdir", "-p", dir).Run() //nolint:errcheck
	}

	// Deploy hub binary.
	hubSrc := filepath.Join(tmpDir, hubName)
	hubDst := filepath.Join(cfg.InstallDir, "bin", "nasnet-panel")
	exec.Command("sudo", "cp", hubSrc, hubDst).Run()  //nolint:errcheck
	exec.Command("sudo", "chmod", "+x", hubDst).Run() //nolint:errcheck
	ui.StepOk("nasnet-panel binary deployed")

	// Deploy agent binary.
	agentSrc := filepath.Join(tmpDir, agentName)
	agentDst := filepath.Join(cfg.InstallDir, "bin", "agent", agentName)
	exec.Command("sudo", "cp", agentSrc, agentDst).Run() //nolint:errcheck
	exec.Command("sudo", "chmod", "+x", agentDst).Run()  //nolint:errcheck
	ui.StepOk("nasnet-agent binary deployed")

	// Deploy nasnet-tool binary (if present in release).
	toolSrc := filepath.Join(tmpDir, toolName)
	if _, err := os.Stat(toolSrc); err == nil {
		toolDst := filepath.Join(cfg.InstallDir, "bin", "nasnet-tool")
		exec.Command("sudo", "cp", toolSrc, toolDst).Run() //nolint:errcheck
		exec.Command("sudo", "chmod", "+x", toolDst).Run() //nolint:errcheck
		ui.StepOk("nasnet-tool binary deployed")
	}

	// Write version marker.
	teeCmd := exec.Command("sudo", "tee", versionFile)
	teeCmd.Stdin = strings.NewReader(latestVersion + "\n")
	teeCmd.Run() //nolint:errcheck

	// Fix ownership.
	runUser := os.Getenv("SUDO_USER")
	if runUser == "" {
		whoamiOut, _ := exec.Command("whoami").Output()
		runUser = strings.TrimSpace(string(whoamiOut))
	}
	if runUser != "" {
		exec.Command("sudo", "chown", "-R", runUser+":"+runUser, cfg.InstallDir).Run() //nolint:errcheck
	}

	// ── Start services ─────────────────────────────────────────────────────
	ui.DrawHeader("Starting Services")

	if startErr := exec.Command("sudo", "systemctl", "start", DefaultBackendService).Run(); startErr != nil {
		ui.StepFail("Backend start failed")
	} else {
		ui.StepOk("Backend started")
	}

	// ── Health check ───────────────────────────────────────────────────────
	appPort := cfg.AppPort
	if appPort == "" {
		appPort = "9761"
	}
	healthURL := fmt.Sprintf("http://127.0.0.1:%s/health", appPort)

	fmt.Println()
	ui.StepInfo("Waiting for health check...")

	healthy := false
	maxRetries := 15
	for i := 0; i < maxRetries; i++ {
		if checkErr := exec.Command("curl", "-sf", healthURL).Run(); checkErr == nil {
			healthy = true
			break
		}
		time.Sleep(2 * time.Second)
	}

	if healthy {
		ui.StepOk("Health check passed")
	} else {
		ui.StepWarn(fmt.Sprintf("Health check did not pass within %ds — service may still be starting",
			maxRetries*2))
	}

	// ── Summary ────────────────────────────────────────────────────────────
	fmt.Println()
	ui.DrawHeader("Update Complete")
	fmt.Printf("  %s → %s\n",
		ui.StyleTitle.Render(currentVersion),
		ui.StyleSuccess.Render(latestVersion))
	fmt.Println()

	svcStatus, _ := exec.Command("systemctl", "is-active", DefaultBackendService).Output()
	state := strings.TrimSpace(string(svcStatus))
	if state == "" {
		state = "inactive"
	}
	var stateRendered string
	if state == "active" {
		stateRendered = ui.StyleSuccess.Render("● active")
	} else {
		stateRendered = ui.StyleError.Render("● " + state)
	}
	ui.Table(
		[]string{"Service", "Status"},
		[][]string{{DefaultBackendService, stateRendered}},
	)

	ui.PressAnyKey()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// gitOutput runs a git command in the given directory and returns trimmed stdout.
func gitOutput(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fetchGithubRelease fetches and decodes the latest GitHub release JSON.
// If client is nil, a default 15s timeout client is used. Callers that want
// the request routed through the outbound SOCKS proxy should pass a client
// built via httpclient.Factory.
func fetchGithubRelease(apiURL, token string, client *http.Client) (*githubRelease, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if decErr := json.NewDecoder(resp.Body).Decode(&rel); decErr != nil {
		return nil, decErr
	}
	return &rel, nil
}

// findAssetURL returns the browser_download_url for the first asset whose name
// contains the given substring, or "" if not found.
func findAssetURL(rel *githubRelease, nameContains string) string {
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, nameContains) {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// sha256File computes the hex-encoded SHA-256 digest of a file.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err2 := io.Copy(h, f); err2 != nil {
		return "", err2
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// performPreUpdateBackup creates a database backup before an update.
// backupPrefix is used in the backup filename (e.g., "pre_update" or "pre_autoupdate").
// Returns non-nil only for unexpected failures; the caller may choose to ignore it.
func performPreUpdateBackup(cfg *Config, deployMode, backupPrefix string) error {
	dbRunning := false

	if cfg.IsSQLite() {
		if deployMode == "docker" {
			out, _ := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
			if strings.Contains(string(out), DefaultContainerBackend) {
				dbRunning = true
			}
		} else {
			if _, err := os.Stat(cfg.DBPath); err == nil {
				dbRunning = true
			}
		}
	} else {
		if deployMode == "docker" {
			out, _ := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
			if strings.Contains(string(out), DefaultContainerDB) {
				dbRunning = true
			}
		} else {
			if pgErr := exec.Command("systemctl", "is-active", "postgresql").Run(); pgErr == nil {
				dbRunning = true
			}
		}
	}

	if !dbRunning {
		ui.StepWarn("Database not running — skipping backup")
		return nil
	}

	if err := os.MkdirAll(cfg.BackupDir, 0750); err != nil {
		return err
	}

	ext := "sql"
	if cfg.IsSQLite() {
		ext = "db"
	}
	backupFile := fmt.Sprintf("%s_%s.%s",
		backupPrefix, time.Now().Format("20060102_150405"), ext)
	backupPath := filepath.Join(cfg.BackupDir, backupFile)

	if err := ui.RunLogged("Creating database backup",
		cfg.PGDumpToFile(backupPath)); err != nil {
		return err
	}
	ui.StepOk("Backup: " + backupFile)
	return nil
}
