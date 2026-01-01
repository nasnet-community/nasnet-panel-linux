package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// BuildEnv resolves VERSION/COMMIT/BUILD_TIME from git (when available)
// and returns them as KEY=value entries to append to an exec.Cmd.Env.
// Compose interpolates ${VERSION}/${COMMIT}/${BUILD_TIME} in build.args
// from this environment.
func BuildEnv() []string {
	version := gitProvenance("describe", "--tags", "--always", "--dirty")
	if version == "" {
		version = "dev"
	}
	commit := gitProvenance("rev-parse", "--short", "HEAD")
	if commit == "" {
		commit = "unknown"
	}
	buildTime := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	return []string{
		"VERSION=" + version,
		"COMMIT=" + commit,
		"BUILD_TIME=" + buildTime,
	}
}

// WithBuildEnv prepends os.Environ() and appends BuildEnv() to the
// command's environment so compose interpolation sees the build args.
func WithBuildEnv(cmd *exec.Cmd) *exec.Cmd {
	cmd.Env = append(os.Environ(), BuildEnv()...)
	return cmd
}

func gitProvenance(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// containerAppUID is the UID/GID baked into the Dockerfile for the appuser.
// Host bind mounts must be readable/writable by this UID.
const containerAppUID = 1000

// PrepareDockerBindMounts creates the host directories that compose bind-mounts
// into the container (data/backups) with ownership matching the container's
// non-root appuser. Idempotent. Failures fall back to chmod 0777 so the
// container can still write — chown requires root.
func (c *Config) PrepareDockerBindMounts() error {
	dirs := []string{
		c.ProjectDir + "/data/backups",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
		if err := os.Chown(d, containerAppUID, containerAppUID); err != nil {
			// Not root — fall back to world-writable so appuser can still write.
			if chmodErr := os.Chmod(d, 0o777); chmodErr != nil {
				return fmt.Errorf("chown/chmod %s: %w", d, chmodErr)
			}
		}
	}
	return nil
}

func ComposeCmd() (string, error) {
	if err := exec.Command("docker", "compose", "version").Run(); err == nil {
		return "docker compose", nil
	}
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return "docker-compose", nil
	}
	return "", fmt.Errorf("docker compose not found")
}

func IsDockerRunning() bool {
	return exec.Command("docker", "info").Run() == nil
}

func RequireDocker() error {
	if !IsDockerRunning() {
		return fmt.Errorf("Docker is not running")
	}
	if _, err := ComposeCmd(); err != nil {
		return err
	}
	return nil
}

func (c *Config) DockerCompose(args ...string) *exec.Cmd {
	composeArgs := []string{"compose", "-f", c.ComposeFile}
	if c.IsSQLite() {
		composeArgs = append(composeArgs, "-f", c.SQLiteComposeFile)
	}
	composeArgs = append(composeArgs, "--project-directory", c.ProjectDir)
	composeArgs = append(composeArgs, args...)
	cmd := exec.Command("docker", composeArgs...)
	cmd.Dir = c.ProjectDir
	return cmd
}

func (c *Config) Systemctl(args ...string) *exec.Cmd {
	return exec.Command("sudo", append([]string{"systemctl"}, args...)...)
}

func (c *Config) PSQLExec(sql string) *exec.Cmd {
	if c.IsDocker() {
		return exec.Command("docker", "exec", "-i", DefaultContainerDB,
			"psql", "-U", c.DBUser, "-d", c.DBName, "-c", sql)
	}
	cmd := exec.Command("psql", "-h", c.DBHost, "-U", c.DBUser, "-d", c.DBName, "-c", sql)
	cmd.Env = append(cmd.Environ(), "PGPASSWORD="+c.DBPassword)
	return cmd
}

func (c *Config) SQLiteExec(sql string) *exec.Cmd {
	if c.IsDocker() {
		return exec.Command("docker", "exec", "-i", DefaultContainerBackend,
			"sqlite3", c.DBPath, sql)
	}
	return exec.Command("sqlite3", c.DBPath, sql)
}

func (c *Config) DBExec(sql string) *exec.Cmd {
	if c.IsSQLite() {
		return c.SQLiteExec(sql)
	}
	return c.PSQLExec(sql)
}

func (c *Config) PGDumpToFile(filepath string) *exec.Cmd {
	if c.IsSQLite() {
		if c.IsDocker() {
			return exec.Command("docker", "cp",
				fmt.Sprintf("%s:%s", DefaultContainerBackend, c.DBPath), filepath)
		}
		return exec.Command("cp", c.DBPath, filepath)
	}
	if c.IsDocker() {
		return exec.Command("bash", "-c",
			fmt.Sprintf("docker exec %q pg_dump -U %q -d %q --no-owner --no-privileges > %q",
				DefaultContainerDB, c.DBUser, c.DBName, filepath))
	}
	return exec.Command("bash", "-c",
		fmt.Sprintf("PGPASSWORD=%q pg_dump -h %q -U %q -d %q --no-owner --no-privileges > %q",
			c.DBPassword, c.DBHost, c.DBUser, c.DBName, filepath))
}

func (c *Config) DBShellCmd() *exec.Cmd {
	if c.IsSQLite() {
		if c.IsDocker() {
			return exec.Command("docker", "exec", "-it", DefaultContainerBackend,
				"sqlite3", c.DBPath)
		}
		return exec.Command("sqlite3", c.DBPath)
	}
	if c.IsDocker() {
		return exec.Command("docker", "exec", "-it", DefaultContainerDB,
			"psql", "-U", c.DBUser, "-d", c.DBName)
	}
	cmd := exec.Command("psql", "-h", c.DBHost, "-U", c.DBUser, "-d", c.DBName)
	cmd.Env = append(cmd.Environ(), "PGPASSWORD="+c.DBPassword)
	return cmd
}
