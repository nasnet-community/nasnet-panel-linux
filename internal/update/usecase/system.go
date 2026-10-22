package usecase

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	panelUnit  = "nasnet-panel.service"
	runnerUnit = "nasnet-panel-update"
)

// System is what a job does to the host. Tests fake it.
type System interface {
	// StartRunner starts the runner unit that finishes the job.
	StartRunner(ctx context.Context, jobID string) error
	// UpdateSystem runs the release's nasnet-tool.sh update-system.
	UpdateSystem(ctx context.Context, script string) error
	DaemonReload(ctx context.Context) error
	Restart(ctx context.Context) error
	// Starts is the panel unit's restart count (NRestarts).
	Starts(ctx context.Context) (int, error)
}

func (u *Usecase) system() System {
	if u.deps.System != nil {
		return u.deps.System
	}
	return systemd{root: u.deps.Paths.Root}
}

type systemd struct{ root string }

// StartRunner runs "update apply --job" in a transient root unit, outside
// the panel's cgroup, so it lives through the restart and can roll back a
// binary that never starts.
func (s systemd) StartRunner(ctx context.Context, jobID string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return runCmd(ctx, "systemd-run", "--unit="+runnerUnit, "--collect", "--quiet",
		"-p", "WorkingDirectory="+s.root,
		"-p", "EnvironmentFile="+filepath.Join(s.root, ".env"),
		exe, "update", "apply", "--job", jobID)
}

func (s systemd) UpdateSystem(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "bash", script, "update-system")
	cmd.Dir = s.root
	// the runner is a bare unit: give the script the root shell it was written for
	cmd.Env = append(os.Environ(), "HOME=/root", "TERM=dumb")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nasnet-tool.sh update-system: %s", lastLine(out, err))
	}
	return nil
}

func (s systemd) DaemonReload(ctx context.Context) error {
	return runCmd(ctx, "systemctl", "daemon-reload")
}

func (s systemd) Restart(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return runCmd(ctx, "systemctl", "restart", panelUnit)
}

func (s systemd) Starts(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "show", "-p", "NRestarts", "--value", panelUnit).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func runCmd(ctx context.Context, name string, args ...string) error {
	if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", name, lastLine(out, err))
	}
	return nil
}
