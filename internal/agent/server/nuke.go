package server

import (
	"context"
	"fmt"
	"os"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
)

type phaseResult struct {
	ok      bool
	skipped bool
	err     error
	bytes   int64
	files   int
}

type phase struct {
	name    string
	applies func(req *pb.NukeRequest) bool
	run     func(ctx context.Context, root Root) phaseResult
	// timeout overrides the default 30s timeout. Zero means default.
	timeout time.Duration
}

type nukeRunner struct {
	phases []phase
	root   Root
}

// Run executes every applicable phase in order. emit (optional) is invoked
// after each phase with a NukePhaseResult for streaming callers; pass nil
// for unary callers.
func (r *nukeRunner) Run(ctx context.Context, req *pb.NukeRequest, emit func(*pb.NukePhaseResult)) (*pb.NukeReport, error) {
	start := time.Now()
	report := &pb.NukeReport{
		Mode:   req.Mode.String(),
		DryRun: req.DryRun,
		Phases: make([]*pb.NukePhaseResult, 0, len(r.phases)),
		Result: pb.NukeReport_NUKE_RESULT_SUCCESS,
	}

	anyFailure := false
	anySuccess := false

	for _, p := range r.phases {
		if !p.applies(req) {
			continue
		}

		phaseStart := time.Now()
		timeout := p.timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		phaseCtx, cancel := context.WithTimeout(ctx, timeout)
		res := runPhaseSafe(phaseCtx, p, r.root)
		cancel()

		out := &pb.NukePhaseResult{
			Phase:        p.name,
			Ok:           res.ok,
			Skipped:      res.skipped,
			DurationMs:   time.Since(phaseStart).Milliseconds(),
			BytesRemoved: res.bytes,
			FilesRemoved: int32(res.files),
		}
		if res.err != nil {
			out.Error = res.err.Error()
		}

		report.Phases = append(report.Phases, out)
		if emit != nil {
			emit(out)
		}

		if res.ok {
			anySuccess = true
		} else if !res.skipped {
			anyFailure = true
			logrus.WithField("phase", p.name).WithError(res.err).Warn("nuke phase failed")
		}
	}

	switch {
	case anyFailure && !anySuccess:
		report.Result = pb.NukeReport_NUKE_RESULT_FAILED
	case anyFailure:
		report.Result = pb.NukeReport_NUKE_RESULT_PARTIAL
	}
	report.TotalDurationMs = time.Since(start).Milliseconds()
	return report, nil
}

func runPhaseSafe(ctx context.Context, p phase, root Root) (res phaseResult) {
	defer func() {
		if r := recover(); r != nil {
			res = phaseResult{ok: false, err: fmt.Errorf("panic in phase %s: %v", p.name, r)}
		}
	}()
	return p.run(ctx, root)
}

// preFlight aborts if this node looks like a hub (refuse to self-wipe the
// control plane). Called from the Wipe/Nuke RPC handlers before running
// any phase.
func preFlight(root Root) error {
	// Basic check: is there a hub directory present?
	if _, err := os.Stat("/etc/nasnet-panel"); err == nil {
		return fmt.Errorf("pre-flight: /etc/nasnet-panel exists — refusing to run on a hub node")
	}
	// Hostname containing 'hub' is a softer signal but worth logging.
	if h, err := os.Hostname(); err == nil && (h == "hub" || h == "hub.local") {
		return fmt.Errorf("pre-flight: hostname %q matches a hub — refusing to run", h)
	}
	return nil
}

// --- Phase definitions (fixed order) ---

// defaultPhases returns the canonical phase list. Kept as a function so tests
// can call it independently of server wiring.
func defaultPhases(xrayStopFn func() error) []phase {
	return []phase{
		// Phase 1: stop_xray
		{
			name:    "stop_xray",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				if root.DryRun() || xrayStopFn == nil {
					return phaseResult{ok: true, skipped: true}
				}
				if err := xrayStopFn(); err != nil {
					return phaseResult{ok: false, err: err}
				}
				return phaseResult{ok: true}
			},
		},

		// Phase 2: wipe_xray
		{
			name:    "wipe_xray",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("systemctl", "disable", "--now", "xray")
				var totalBytes int64
				var totalFiles int
				for _, p := range []string{
					"/etc/xray", "/usr/local/share/xray", "/var/log/xray",
				} {
					b, f, _ := root.RemoveAllPath(p)
					totalBytes += b
					totalFiles += f
				}
				_, _ = root.RunCmd("systemctl", "daemon-reload")
				return phaseResult{ok: true, bytes: totalBytes, files: totalFiles}
			},
		},

		// Phase 3: wipe_wireguard
		{
			name:    "wipe_wireguard",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("systemctl", "stop", "wg-quick@wg0")
				_, _ = root.RunCmd("systemctl", "disable", "wg-quick@wg0")
				_, _ = root.RunCmd("apt-get", "remove", "-y", "wireguard", "wireguard-tools")
				b, f, _ := root.RemoveAllPath("/etc/wireguard")
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 4: wipe_xui
		{
			name:    "wipe_xui",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("systemctl", "stop", "x-ui")
				_, _ = root.RunCmd("systemctl", "disable", "x-ui")
				b1, f1, _ := root.RemoveAllPath("/usr/local/x-ui")
				b2, f2, _ := root.RemoveAllPath("/etc/x-ui")
				b3, f3, _ := root.RemoveAllPath("/etc/systemd/system/x-ui.service")
				b4, f4, _ := root.RemoveAllPath("/etc/systemd/system/x-ui.service.d")
				_, _ = root.RunCmd("systemctl", "daemon-reload")
				return phaseResult{ok: true, bytes: b1 + b2 + b3 + b4, files: f1 + f2 + f3 + f4}
			},
		},

		// Phase 5: flush_iptables
		{
			name:    "flush_iptables",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				for _, args := range [][]string{
					{"-F"}, {"-X"}, {"-Z"},
					{"-t", "nat", "-F"}, {"-t", "nat", "-X"},
					{"-t", "mangle", "-F"}, {"-t", "mangle", "-X"},
				} {
					_, _ = root.RunCmd("iptables", args...)
				}
				_, _ = root.RunCmd("conntrack", "-F")
				return phaseResult{ok: true}
			},
		},

		// Phase 6: clear_bash_history
		{
			name:    "clear_bash_history",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				var totalBytes int64
				var totalFiles int
				for _, g := range []string{"/root/.bash_history", "/home/*/.bash_history"} {
					b, f, _ := root.RemoveGlobPath(g)
					totalBytes += b
					totalFiles += f
				}
				return phaseResult{ok: true, bytes: totalBytes, files: totalFiles}
			},
		},

		// Phase 7: shred_ssh_host_keys (NUKE only)
		{
			name:    "shred_ssh_host_keys",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				b, f, _ := root.RemoveGlobPath("/etc/ssh/ssh_host_*")
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 8: wipe_known_hosts_and_authkeys
		{
			name:    "wipe_known_hosts_and_authkeys",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				var b int64
				var f int
				for _, g := range []string{
					"/root/.ssh/known_hosts", "/root/.ssh/authorized_keys",
					"/home/*/.ssh/known_hosts", "/home/*/.ssh/authorized_keys",
				} {
					bb, ff, _ := root.RemoveGlobPath(g)
					b += bb
					f += ff
				}
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 9: wipe_auth_logs
		{
			name:    "wipe_auth_logs",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				var b int64
				var f int
				for _, p := range []string{
					"/var/log/lastlog", "/var/log/wtmp", "/var/log/btmp", "/var/run/utmp",
					"/var/log/auth.log", "/var/log/secure",
				} {
					bb, ff, _ := root.ShredPath(p)
					b += bb
					f += ff
				}
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 10: wipe_journals_and_var_log
		{
			name:    "wipe_journals_and_var_log",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("journalctl", "--rotate")
				_, _ = root.RunCmd("journalctl", "--vacuum-time=1s")
				var b int64
				var f int
				for _, p := range []string{
					"/var/log/journal", "/var/log/apt", "/var/lib/apt/lists",
				} {
					bb, ff, _ := root.RemoveAllPath(p)
					b += bb
					f += ff
				}
				bb, ff, _ := root.RemoveGlobPath("/var/log/dpkg.log*")
				b += bb
				f += ff
				_, _ = root.RunCmd("systemctl", "restart", "systemd-journald")
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 11: wipe_tmp
		{
			name:    "wipe_tmp",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				b1, f1, _ := root.RemoveGlobPath("/tmp/*")
				b2, f2, _ := root.RemoveGlobPath("/var/tmp/*")
				return phaseResult{ok: true, bytes: b1 + b2, files: f1 + f2}
			},
		},

		// Phase 12: disable_audit
		{
			name:    "disable_audit",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("auditctl", "-D")
				_, _ = root.RunCmd("systemctl", "stop", "auditd")
				_, _ = root.RunCmd("systemctl", "disable", "auditd")
				return phaseResult{ok: true}
			},
		},

		// Phase 13: disable_coredumps
		{
			name:    "disable_coredumps",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("sysctl", "-w", "kernel.core_pattern=|/bin/false")
				// ulimit is a shell builtin, not a binary; skip from Go.
				return phaseResult{ok: true}
			},
		},

		// Phase 14: scrub_swap
		{
			name:    "scrub_swap",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			timeout: 60 * time.Second, // swapoff can be slow
			run: func(_ context.Context, root Root) phaseResult {
				if _, err := root.RunCmd("swapoff", "-a"); err != nil {
					return phaseResult{ok: false, err: err}
				}
				_, _ = root.RunCmd("swapon", "-a")
				return phaseResult{ok: true}
			},
		},

		// Phase 15: drop_caches
		{
			name:    "drop_caches",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE },
			run: func(_ context.Context, root Root) phaseResult {
				_, _ = root.RunCmd("sync")
				_, _ = root.RunCmd("sh", "-c", "echo 3 > /proc/sys/vm/drop_caches")
				return phaseResult{ok: true}
			},
		},

		// Phase 16: shred_root (opt-in toggle within NUKE mode)
		{
			name:    "shred_root",
			applies: func(r *pb.NukeRequest) bool { return r.Mode == pb.NukeMode_NUKE_MODE_NUKE && r.ShredRoot },
			timeout: 5 * time.Minute,
			run: func(_ context.Context, root Root) phaseResult {
				b, f, _ := root.RemoveAllPath("/root")
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 17: shred_tls_certs (must run before wipe_agent_state so parent still exists)
		{
			name:    "shred_tls_certs",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				b, f, _ := root.ShredGlobPath("/etc/nasnet-agent/tls/*")
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},

		// Phase 18: wipe_agent_state
		{
			name:    "wipe_agent_state",
			applies: func(_ *pb.NukeRequest) bool { return true },
			run: func(_ context.Context, root Root) phaseResult {
				var b int64
				var f int
				for _, p := range []string{
					"/etc/nasnet-agent", "/var/lib/nasnet-agent", "/var/log/nasnet-agent",
				} {
					bb, ff, _ := root.RemoveAllPath(p)
					b += bb
					f += ff
				}
				for _, g := range []string{"/tmp/nasnet-*", "/tmp/xray-*", "/tmp/agent-*"} {
					bb, ff, _ := root.RemoveGlobPath(g)
					b += bb
					f += ff
				}
				return phaseResult{ok: true, bytes: b, files: f}
			},
		},
	}
}
