package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/tool/ui"
)

const (
	requiredGoVersion = "1.25"
	requiredNodeMajor = 22
)

// PrereqsDocker checks that Docker and Docker Compose are installed and running.
// If Docker is missing, it offers to install it. Returns true when all
// prerequisites are satisfied.
func PrereqsDocker(cfg *Config) bool {
	ui.DrawHeader("Prerequisites — Docker Mode")

	// ── Docker ────────────────────────────────────────────────────────────
	dockerOk := false
	if _, err := exec.LookPath("docker"); err == nil && IsDockerRunning() {
		ver, _ := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
		ui.StepOk("Docker " + strings.TrimSpace(string(ver)))
		dockerOk = true
	} else {
		ui.StepFail("Docker not found or not running")
		install, err := ui.Confirm("Install Docker now?")
		if err == nil && install {
			if installDocker() {
				dockerOk = true
			}
		}
	}

	if !dockerOk {
		ui.StepFail("Docker is required — cannot continue")
		return false
	}

	// ── Docker Compose ────────────────────────────────────────────────────
	if _, err := ComposeCmd(); err != nil {
		ui.StepFail("Docker Compose not found — cannot continue")
		ui.StepInfo("Install: https://docs.docker.com/compose/install/")
		return false
	}
	composeVer, _ := exec.Command("docker", "compose", "version", "--short").Output()
	ui.StepOk("Docker Compose " + strings.TrimSpace(string(composeVer)))

	// ── openssl ───────────────────────────────────────────────────────────
	if _, err := exec.LookPath("openssl"); err == nil {
		ui.StepOk("openssl")
	} else {
		ui.StepWarn("openssl not found — will use fallback for secret generation")
	}

	// ── htpasswd ──────────────────────────────────────────────────────────
	if _, err := exec.LookPath("htpasswd"); err == nil {
		ui.StepOk("htpasswd")
	} else {
		if IsDockerRunning() {
			ui.StepWarn("htpasswd not found — will use Docker fallback for bcrypt")
		} else {
			ui.StepFail("htpasswd not found and Docker not available for fallback")
			ui.StepInfo("Install: apt install apache2-utils")
			return false
		}
	}

	// ── git ───────────────────────────────────────────────────────────────
	if _, err := exec.LookPath("git"); err == nil {
		ui.StepOk("git")
	} else {
		ui.StepWarn("git not found — updates will not work")
	}

	return true
}

// installDocker downloads and runs the official Docker install script, then
// enables the Docker service and adds the current user to the docker group.
func installDocker() bool {
	cmd := exec.Command("bash", "-c", "curl -fsSL https://get.docker.com | sh")
	if err := ui.RunLogged("Installing Docker", cmd); err != nil {
		ui.StepFail("Docker installation failed: " + err.Error())
		return false
	}

	// Enable + start docker
	exec.Command("sudo", "systemctl", "enable", "docker", "--now").Run() //nolint:errcheck

	// Add current user to docker group
	user := os.Getenv("SUDO_USER")
	if user == "" {
		user = os.Getenv("USER")
	}
	if user != "" {
		exec.Command("sudo", "usermod", "-aG", "docker", user).Run() //nolint:errcheck
	}

	// Verify docker is now running
	for i := 0; i < 5; i++ {
		if IsDockerRunning() {
			ver, _ := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
			ui.StepOk("Docker " + strings.TrimSpace(string(ver)) + " installed")
			return true
		}
		time.Sleep(2 * time.Second)
	}

	ui.StepFail("Docker installed but daemon did not start — please reboot or run: sudo systemctl start docker")
	return false
}

// PrereqsSystemd checks that all bare-metal (systemd) prerequisites are met:
// Go >= 1.25, Node.js >= 22, pnpm, and the selected database engine.
// If anything is missing, it offers to install it. Returns true when all
// prerequisites are satisfied.
func PrereqsSystemd(cfg *Config, dbDriver string) bool {
	// ── Offline mode: only verify deployed artifacts ──────────────────────
	if cfg.OfflineMode {
		return prereqsSystemdOffline(cfg, dbDriver)
	}

	ui.DrawHeader("Prerequisites — Systemd (Bare-Metal) Mode")

	if runtime.GOOS != "linux" {
		ui.StepFail("Systemd mode is only supported on Linux")
		return false
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		ui.StepFail("systemctl not found — systemd is not available on this system")
		return false
	}

	if _, err := exec.LookPath("apt-get"); err != nil {
		ui.StepFail("apt-get not found — only Debian/Ubuntu is supported for automatic setup")
		ui.StepInfo("For other distros, install dependencies manually and use Docker mode")
		return false
	}

	// ── apt update ────────────────────────────────────────────────────────
	if err := ui.RunLogged("Running apt update", exec.Command("sudo", "apt-get", "update")); err != nil {
		ui.StepFail("apt update failed")
		return false
	}

	// ── System packages ───────────────────────────────────────────────────
	systemPkgs := []string{"git", "openssl", "apache2-utils", "ca-certificates", "wget", "curl", "build-essential"}
	var toInstall []string
	for _, pkg := range systemPkgs {
		if err := exec.Command("dpkg", "-s", pkg).Run(); err == nil {
			ui.StepOk(pkg)
		} else {
			toInstall = append(toInstall, pkg)
		}
	}
	if len(toInstall) > 0 {
		installArgs := append([]string{"apt-get", "install", "-y"}, toInstall...)
		if err := ui.RunLogged("Installing system packages: "+strings.Join(toInstall, " "),
			exec.Command("sudo", installArgs...)); err != nil {
			ui.StepFail("Failed to install system packages")
			return false
		}
	}

	fmt.Println()

	// ── Go ────────────────────────────────────────────────────────────────
	if !ensureGo() {
		return false
	}

	fmt.Println()

	// ── Node.js ───────────────────────────────────────────────────────────
	if !ensureNode() {
		return false
	}

	// ── pnpm ──────────────────────────────────────────────────────────────
	if !ensurePnpm() {
		return false
	}

	fmt.Println()

	// ── Database ──────────────────────────────────────────────────────────
	if dbDriver == "sqlite" {
		ui.StepOk("SQLite selected — no separate database to install")
		exec.Command("sudo", "mkdir", "-p", cfg.InstallDir+"/data").Run() //nolint:errcheck
	} else {
		if !ensurePostgres() {
			return false
		}
	}

	fmt.Println()
	ui.StepOk("All prerequisites installed")
	return true
}

// prereqsSystemdOffline verifies that the necessary artifacts exist for
// offline (bundle) deployments without attempting any downloads.
func prereqsSystemdOffline(cfg *Config, dbDriver string) bool {
	ui.DrawHeader("Prerequisites — Offline Mode")

	if runtime.GOOS != "linux" {
		ui.StepFail("Only Linux is supported")
		return false
	}
	ui.StepOk("Linux")

	if _, err := exec.LookPath("systemctl"); err != nil {
		ui.StepFail("systemctl not found — systemd is required")
		return false
	}
	ui.StepOk("systemd")

	binary := cfg.InstallDir + "/bin/nasnet-panel"
	if info, err := os.Stat(binary); err != nil || !isExecutable(info) {
		ui.StepFail("nasnet-panel binary not found at " + binary)
		return false
	}
	ui.StepOk("nasnet-panel binary")

	if dbDriver != "sqlite" {
		if _, err := exec.LookPath("psql"); err != nil {
			ui.StepFail("psql not found — PostgreSQL is required for non-SQLite mode")
			return false
		}
		ui.StepOk("PostgreSQL client (psql)")
	} else {
		ui.StepOk("SQLite — no database server needed")
	}

	fmt.Println()
	ui.StepOk("All prerequisites verified (offline mode)")
	return true
}

func isExecutable(info os.FileInfo) bool {
	return info != nil && info.Mode()&0111 != 0
}

// ensureGo checks whether Go >= requiredGoVersion is installed. If not, it
// resolves the latest patch release from the Go download API and installs it.
func ensureGo() bool {
	if out, err := exec.Command("go", "version").Output(); err == nil {
		// "go version go1.25.3 linux/amd64"
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			goVer := strings.TrimPrefix(fields[2], "go")
			parts := strings.SplitN(goVer, ".", 3)
			if len(parts) >= 2 {
				installed := parts[0] + "." + parts[1]
				if installed == requiredGoVersion || goVersionAtLeast(goVer, requiredGoVersion) {
					ui.StepOk("Go " + fields[2])
					return true
				}
				ui.StepWarn(fmt.Sprintf("Go %s found but %s.x required", fields[2], requiredGoVersion))
			}
		}
	}

	ui.StepInfo(fmt.Sprintf("Installing Go %s...", requiredGoVersion))

	// Resolve latest patch version from the Go download API
	goFull, err := resolveLatestGo(requiredGoVersion)
	if err != nil || goFull == "" {
		ui.StepFail(fmt.Sprintf("Failed to resolve latest Go %s.x patch version", requiredGoVersion))
		ui.StepInfo("Download manually: https://go.dev/dl/")
		return false
	}
	ui.StepInfo("Resolved " + goFull)

	arch := hostArch()
	tarball := goFull + ".linux-" + arch + ".tar.gz"
	url := "https://go.dev/dl/" + tarball
	tmp := "/tmp/" + tarball

	if err := ui.RunLogged("Downloading "+goFull,
		exec.Command("curl", "-fSL", "-o", tmp, url)); err != nil {
		ui.StepFail("Failed to download Go from " + url)
		ui.StepInfo("Download manually: https://go.dev/dl/")
		return false
	}

	exec.Command("sudo", "rm", "-rf", "/usr/local/go").Run() //nolint:errcheck
	if err := ui.RunLogged("Extracting Go to /usr/local/go",
		exec.Command("sudo", "tar", "-C", "/usr/local", "-xzf", tmp)); err != nil {
		ui.StepFail("Failed to extract Go")
		return false
	}
	os.Remove(tmp) //nolint:errcheck

	// Ensure Go is on PATH for current process
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/usr/local/go/bin:"+oldPath) //nolint:errcheck

	// Persist PATH addition
	profileLine := `export PATH="/usr/local/go/bin:$PATH"`
	profileFile := "/etc/profile.d/golang.sh"
	if data, readErr := os.ReadFile(profileFile); readErr != nil || !strings.Contains(string(data), "/usr/local/go/bin") {
		writeCmd := exec.Command("sudo", "tee", profileFile)
		writeCmd.Stdin = strings.NewReader(profileLine + "\n")
		writeCmd.Run() //nolint:errcheck
		ui.StepOk("Go PATH added to " + profileFile)
	}

	if out, err := exec.Command("go", "version").Output(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			ui.StepOk("Go " + fields[2] + " ready")
		}
	}
	return true
}

// resolveLatestGo calls the Go download API to find the latest patch release
// for the given major.minor (e.g. "1.25"). Honors OUTBOUND_PROXY_URL env
// var to route through a SOCKS5 proxy (wizard runs as a separate process
// and cannot read DB settings reliably; env is the integration point).
func resolveLatestGo(minorVer string) (string, error) {
	client := wizardHTTPClient(nil)
	resp, err := client.Get("https://go.dev/dl/?mode=json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var releases []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", err
	}

	prefix := "go" + minorVer + "."
	for _, r := range releases {
		if strings.HasPrefix(r.Version, prefix) {
			return r.Version, nil
		}
	}
	return "", fmt.Errorf("no release found for Go %s", minorVer)
}

// goVersionAtLeast returns true if installed (e.g. "1.25.3") is >= required (e.g. "1.25").
func goVersionAtLeast(installed, required string) bool {
	iParts := strings.Split(installed, ".")
	rParts := strings.Split(required, ".")
	for i := 0; i < len(rParts); i++ {
		if i >= len(iParts) {
			return false
		}
		iN, _ := strconv.Atoi(iParts[i])
		rN, _ := strconv.Atoi(rParts[i])
		if iN > rN {
			return true
		}
		if iN < rN {
			return false
		}
	}
	return true
}

// ensureNode checks whether Node.js >= requiredNodeMajor is installed.
// If not, it installs via the NodeSource APT repository.
func ensureNode() bool {
	if out, err := exec.Command("node", "--version").Output(); err == nil {
		ver := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
		parts := strings.SplitN(ver, ".", 2)
		if major, convErr := strconv.Atoi(parts[0]); convErr == nil && major >= requiredNodeMajor {
			ui.StepOk("Node.js v" + ver)
			return true
		}
		ui.StepWarn(fmt.Sprintf("Node.js v%s found but v%d.x required", ver, requiredNodeMajor))
	}

	ui.StepInfo(fmt.Sprintf("Installing Node.js %d.x...", requiredNodeMajor))

	setupScript := fmt.Sprintf("curl -fsSL https://deb.nodesource.com/setup_%d.x | sudo -E bash -", requiredNodeMajor)
	if err := ui.RunLogged("Setting up NodeSource repository",
		exec.Command("bash", "-c", setupScript)); err != nil {
		ui.StepFail("Failed to add NodeSource repository")
		return false
	}

	if err := ui.RunLogged("Installing Node.js",
		exec.Command("sudo", "apt-get", "install", "-y", "nodejs")); err != nil {
		ui.StepFail("Failed to install Node.js")
		return false
	}

	if out, err := exec.Command("node", "--version").Output(); err == nil {
		ui.StepOk("Node.js " + strings.TrimSpace(string(out)) + " installed")
	}
	return true
}

// ensurePnpm checks whether pnpm is installed and installs it via npm if missing.
func ensurePnpm() bool {
	if out, err := exec.Command("pnpm", "--version").Output(); err == nil {
		ui.StepOk("pnpm " + strings.TrimSpace(string(out)))
		return true
	}

	ui.StepInfo("Installing pnpm...")
	if err := ui.RunLogged("Installing pnpm",
		exec.Command("sudo", "npm", "install", "-g", "pnpm")); err != nil {
		ui.StepFail("Failed to install pnpm")
		return false
	}

	if out, err := exec.Command("pnpm", "--version").Output(); err == nil {
		ui.StepOk("pnpm " + strings.TrimSpace(string(out)) + " installed")
	}
	return true
}

// ensurePostgres checks whether PostgreSQL is installed and running.
// If not, it installs PostgreSQL from the official APT repository.
func ensurePostgres() bool {
	psqlOk := false

	if _, err := exec.LookPath("psql"); err == nil {
		// Check if service is running
		if exec.Command("systemctl", "is-active", "postgresql").Run() == nil {
			ver, _ := exec.Command("psql", "--version").Output()
			fields := strings.Fields(string(ver))
			pgVer := ""
			if len(fields) >= 3 {
				pgVer = " " + fields[2]
			}
			ui.StepOk("PostgreSQL" + pgVer + " (running)")
			psqlOk = true
		} else {
			ui.StepWarn("PostgreSQL installed but not running — starting...")
			if exec.Command("sudo", "systemctl", "enable", "postgresql", "--now").Run() == nil {
				ui.StepOk("PostgreSQL started")
				psqlOk = true
			} else {
				ui.StepFail("Failed to start PostgreSQL")
			}
		}
	}

	if psqlOk {
		return true
	}

	ui.StepInfo("Installing PostgreSQL...")

	// Add official PostgreSQL APT repository if not present
	if _, err := os.Stat("/etc/apt/sources.list.d/pgdg.list"); err != nil {
		ui.StepInfo("Adding PostgreSQL APT repository...")
		addRepoScript := `curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /usr/share/keyrings/postgresql-keyring.gpg 2>/dev/null &&
echo "deb [signed-by=/usr/share/keyrings/postgresql-keyring.gpg] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list >/dev/null &&
sudo apt-get update`
		if err := ui.RunLogged("Adding PostgreSQL APT repository",
			exec.Command("bash", "-c", addRepoScript)); err != nil {
			ui.StepWarn("Could not add official repo — will use distro version")
		}
	}

	if err := ui.RunLogged("Installing PostgreSQL",
		exec.Command("sudo", "apt-get", "install", "-y", "postgresql", "postgresql-client")); err != nil {
		ui.StepFail("Failed to install PostgreSQL")
		return false
	}
	ui.StepOk("PostgreSQL installed")

	if err := exec.Command("sudo", "systemctl", "enable", "postgresql", "--now").Run(); err != nil {
		ui.StepFail("Failed to start PostgreSQL")
		return false
	}
	ui.StepOk("PostgreSQL enabled and started")
	return true
}

// hostArch maps the OS architecture reported by Go to the strings used in Go
// download tarballs (amd64, arm64, etc.).
func hostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	default:
		return "amd64"
	}
}
