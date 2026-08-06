package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// snapshotVersion guards against reading a snapshot written by a future build
const snapshotVersion = 1

// cloudInitDropIn disables cloud-init's network config. Written by the takeover,
// removed by a restore that predates it.
const cloudInitDropIn = "99-nasnet.cfg"

// Paths locates everything the network feature owns
type Paths struct {
	NetworkdDir        string
	NetplanDir         string
	NetplanDisabledDir string
	SysctlDir          string
	RTTablesDir        string
	CloudInitDir       string
	RunNetworkdDir     string // holds netplan's generated units.
	StateDir           string
}

func DefaultPaths() Paths {
	return Paths{
		NetworkdDir:        "/etc/systemd/network",
		NetplanDir:         "/etc/netplan",
		NetplanDisabledDir: "/etc/netplan.disabled",
		SysctlDir:          "/etc/sysctl.d",
		RTTablesDir:        "/etc/iproute2/rt_tables.d",
		CloudInitDir:       "/etc/cloud/cloud.cfg.d",
		RunNetworkdDir:     "/run/systemd/network",
		StateDir:           "/var/lib/nasnet",
	}
}

// Snapshot is everything needed to put the box back as it was
type Snapshot struct {
	Version int       `json:"version"`
	TakenAt time.Time `json:"taken_at"`

	NetworkdFiles map[string][]byte `json:"networkd_files"`
	NetplanFiles  map[string][]byte `json:"netplan_files"`
	SysctlFiles   map[string][]byte `json:"sysctl_files"`
	RTTablesFiles map[string][]byte `json:"rt_tables_files"`

	Rules  []Rule          `json:"rules"`
	Routes map[int][]Route `json:"routes"`
	Addrs  []Addr          `json:"addrs"`

	NftRuleset string `json:"nft_ruleset"`

	MaskedUnits       []string `json:"masked_units"`
	CloudInitDisabled bool     `json:"cloud_init_disabled"`
}

type Snapshotter struct {
	Backend Backend
	Nft     *nft.Manager
	Paths   Paths
}

// Capture reads current state
func (s *Snapshotter) Capture(ctx context.Context, tables []int) (*Snapshot, error) {
	snap := &Snapshot{
		Version: snapshotVersion,
		TakenAt: time.Now(),
		Routes:  map[int][]Route{},
	}

	var err error
	if snap.NetworkdFiles, err = readDirFiles(s.Paths.NetworkdDir); err != nil {
		return nil, fmt.Errorf("snapshot networkd: %w", err)
	}
	if snap.NetplanFiles, err = readDirFiles(s.Paths.NetplanDir); err != nil {
		return nil, fmt.Errorf("snapshot netplan: %w", err)
	}
	if snap.SysctlFiles, err = readDirFiles(s.Paths.SysctlDir); err != nil {
		return nil, fmt.Errorf("snapshot sysctl.d: %w", err)
	}
	if snap.RTTablesFiles, err = readDirFiles(s.Paths.RTTablesDir); err != nil {
		return nil, fmt.Errorf("snapshot rt_tables.d: %w", err)
	}

	if snap.Rules, err = s.Backend.RuleList(ctx); err != nil {
		return nil, fmt.Errorf("snapshot rules: %w", err)
	}
	for _, t := range tables {
		rs, err := s.Backend.RouteList(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("snapshot routes table %d: %w", t, err)
		}
		snap.Routes[t] = rs
	}
	if snap.Addrs, err = s.Backend.Addrs(ctx); err != nil {
		return nil, fmt.Errorf("snapshot addrs: %w", err)
	}

	if s.Nft != nil {
		snap.NftRuleset = s.Nft.Snapshot().Render()
	}
	snap.MaskedUnits = maskedUnits("NetworkManager.service", "NetworkManager-wait-online.service")
	if _, err := os.Stat(s.cloudInitPath()); err == nil {
		snap.CloudInitDisabled = true
	}

	return snap, nil
}

func (s *Snapshotter) cloudInitPath() string {
	return filepath.Join(s.Paths.CloudInitDir, cloudInitDropIn)
}

func (s *Snapshotter) Save(snap *Snapshot, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("snapshot dir: %w", err)
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	// Write-then-rename: a truncated snapshot is worse than none, because the
	// dead-man would restore it.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return os.Rename(tmp, path)
}

func LoadSnapshot(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	if snap.Version != snapshotVersion {
		return nil, fmt.Errorf("snapshot version %d, this build understands %d",
			snap.Version, snapshotVersion)
	}
	return &snap, nil
}

// Restore is the exact inverse of an apply
func (s *Snapshotter) Restore(ctx context.Context, snap *Snapshot) error {
	var errs []string
	restoreDir := func(dir string, want map[string][]byte) {
		if err := writeDirExactly(dir, want); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dir, err))
		}
	}
	restoreDir(s.Paths.NetworkdDir, snap.NetworkdFiles)
	restoreDir(s.Paths.NetplanDir, snap.NetplanFiles)
	restoreDir(s.Paths.SysctlDir, snap.SysctlFiles)
	restoreDir(s.Paths.RTTablesDir, snap.RTTablesFiles)

	// The owned table goes away entirely — one command, idempotent, which is
	// exactly why everything lives in one table.
	if s.Nft != nil {
		if err := s.Nft.Teardown(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("nft teardown: %v", err))
		}
	}

	current, err := s.Backend.RuleList(ctx)
	if err != nil {
		errs = append(errs, fmt.Sprintf("rule list: %v", err))
	}
	for _, r := range current {
		if r.Pref == 0 || r.Pref >= 32766 {
			continue // stock local/main/default rules
		}
		if containsRule(snap.Rules, r) {
			continue
		}
		if err := s.Backend.RuleDel(ctx, r); err != nil {
			errs = append(errs, fmt.Sprintf("rule del pref %d: %v", r.Pref, err))
		}
	}
	for _, r := range snap.Rules {
		if r.Pref == 0 || r.Pref >= 32766 {
			continue
		}
		if err := s.Backend.RuleAdd(ctx, r); err != nil {
			errs = append(errs, fmt.Sprintf("rule add pref %d: %v", r.Pref, err))
		}
	}

	for table, want := range snap.Routes {
		have, err := s.Backend.RouteList(ctx, table)
		if err != nil {
			errs = append(errs, fmt.Sprintf("route list %d: %v", table, err))
			continue
		}
		for _, r := range have {
			if err := s.Backend.RouteDel(ctx, r); err != nil {
				errs = append(errs, fmt.Sprintf("route del %d %s: %v", table, r.Dest, err))
			}
		}
		for _, r := range want {
			if err := s.Backend.RouteReplace(ctx, r); err != nil {
				errs = append(errs, fmt.Sprintf("route replace %d %s: %v", table, r.Dest, err))
			}
		}
	}

	for _, unit := range snap.MaskedUnits {
		_ = exec.CommandContext(ctx, "systemctl", "mask", unit).Run()
	}
	if !snap.CloudInitDisabled {
		_ = os.Remove(s.cloudInitPath())
	}

	if len(errs) > 0 {
		return fmt.Errorf("restore completed with errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// readDirFiles returns basename -> content for every regular file in dir
func readDirFiles(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = b
	}
	return out, nil
}

// writeDirExactly makes dir's contents equal want.
func writeDirExactly(dir string, want map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, keep := want[e.Name()]; !keep {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	for name, content := range want {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func containsRule(rs []Rule, r Rule) bool {
	for _, x := range rs {
		if x.Equal(r) {
			return true
		}
	}
	return false
}

// maskedUnits returns which of the named units are currently masked.
func maskedUnits(units ...string) []string {
	var out []string
	for _, u := range units {
		b, _ := exec.Command("systemctl", "is-enabled", u).Output()
		if unitIsMasked(string(b)) {
			out = append(out, u)
		}
	}
	return out
}

// unitIsMasked reads `systemctl is-enabled` output
func unitIsMasked(state string) bool {
	switch strings.TrimSpace(state) {
	case "masked", "masked-runtime":
		return true
	}
	return false
}
