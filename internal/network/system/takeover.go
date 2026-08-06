package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TakeoverOps make nasnet the sole owner of the network
func TakeoverOps(p Paths) []Op {
	cloudInit := filepath.Join(p.CloudInitDir, cloudInitDropIn)

	return []Op{
		{
			Desc: fmt.Sprintf("move netplan configuration aside (%s -> %s)", p.NetplanDir, p.NetplanDisabledDir),
			Do: func(context.Context) error {
				return moveYAML(p.NetplanDir, p.NetplanDisabledDir)
			},
			Undo: func(context.Context) error {
				return moveYAML(p.NetplanDisabledDir, p.NetplanDir)
			},
		},
		{
			// networkctl reload won't remove these, and a leftover
			// 10-netplan-*.network keeps winning the basename race.
			Desc: fmt.Sprintf("remove netplan's generated files from %s", p.RunNetworkdDir),
			Do: func(context.Context) error {
				matches, err := filepath.Glob(filepath.Join(p.RunNetworkdDir, "*netplan*"))
				if err != nil {
					return err
				}
				for _, m := range matches {
					if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("remove %s: %w", m, err)
					}
				}
				return nil
			},
		},
		{
			Desc: "disable cloud-init network configuration",
			Do: func(context.Context) error {
				if err := os.MkdirAll(p.CloudInitDir, 0o755); err != nil {
					return err
				}
				return os.WriteFile(cloudInit, []byte("network: {config: disabled}\n"), 0o644)
			},
			Undo: func(context.Context) error {
				if err := os.Remove(cloudInit); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			},
		},
		{
			Desc: "mask NetworkManager and NetworkManager-wait-online",
			Do: func(ctx context.Context) error {
				return systemctl(ctx, "mask",
					"NetworkManager", "NetworkManager-wait-online.service")
			},
			Undo: func(ctx context.Context) error {
				return systemctl(ctx, "unmask",
					"NetworkManager", "NetworkManager-wait-online.service")
			},
		},
	}
}

// TakeoverDone reports whether the netplan directory is empty of YAML
func TakeoverDone(p Paths) bool {
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		matches, err := filepath.Glob(filepath.Join(p.NetplanDir, pattern))
		if err != nil || len(matches) > 0 {
			return false
		}
	}
	return true
}

// moveYAML relocates every *.yaml/*.yml from src to dst. Empty src -> success
func moveYAML(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dst, err)
	}
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		matches, err := filepath.Glob(filepath.Join(src, pattern))
		if err != nil {
			return err
		}
		for _, m := range matches {
			target := filepath.Join(dst, filepath.Base(m))
			if err := os.Rename(m, target); err != nil {
				// Rename fails across filesystems; copy+remove instead.
				b, rerr := os.ReadFile(m)
				if rerr != nil {
					return fmt.Errorf("read %s: %w", m, rerr)
				}
				if werr := os.WriteFile(target, b, 0o644); werr != nil {
					return fmt.Errorf("write %s: %w", target, werr)
				}
				if rmerr := os.Remove(m); rmerr != nil {
					return fmt.Errorf("remove %s: %w", m, rmerr)
				}
			}
		}
	}
	return nil
}

// systemctl runs one verb over several units
func systemctl(ctx context.Context, verb string, units ...string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	args := append([]string{verb}, units...)
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "not loaded") || strings.Contains(s, "does not exist") {
			return nil
		}
		return fmt.Errorf("systemctl %s: %w (output: %s)", verb, err, strings.TrimSpace(s))
	}
	return nil
}
