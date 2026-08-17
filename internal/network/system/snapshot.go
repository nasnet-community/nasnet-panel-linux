package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
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
	NetworkdConfDir    string // networkd.conf.d, where we tell it to leave our rules alone
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
		NetworkdConfDir:    "/etc/systemd/networkd.conf.d",
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

	NetworkdFiles     map[string][]byte `json:"networkd_files"`
	NetworkdConfFiles map[string][]byte `json:"networkd_conf_files"`
	NetplanFiles      map[string][]byte `json:"netplan_files"`
	SysctlFiles       map[string][]byte `json:"sysctl_files"`
	RTTablesFiles     map[string][]byte `json:"rt_tables_files"`

	// Modes, kept separate so a snapshot written by an older build still loads.
	// netplan refuses to be world-readable, so 0644 everywhere is not safe.
	NetworkdModes     map[string]uint32 `json:"networkd_modes,omitempty"`
	NetworkdConfModes map[string]uint32 `json:"networkd_conf_modes,omitempty"`
	NetplanModes      map[string]uint32 `json:"netplan_modes,omitempty"`
	SysctlModes       map[string]uint32 `json:"sysctl_modes,omitempty"`
	RTTablesModes     map[string]uint32 `json:"rt_tables_modes,omitempty"`

	Rules  []Rule          `json:"rules"`
	Routes map[int][]Route `json:"routes"`
	Addrs  []Addr          `json:"addrs"`

	NftRuleset string `json:"nft_ruleset"`
	// NftState is what a rollback restores. Kept alongside the rendered text and
	// omitempty, so an older build's snapshot still loads mid-apply.
	NftState *nft.Ruleset `json:"nft_state,omitempty"`

	MaskedUnits       []string `json:"masked_units"`
	CloudInitDisabled bool     `json:"cloud_init_disabled"`

	// LANConfig lives in the database, which no other field here covers. Without
	// it a reverted LAN change leaves the row disagreeing with the files.
	LANConfig *domain.LANConfig `json:"lan_config,omitempty"`

	// VPNProfile is whatever was active, nil for none. VPNCaptured tells that
	// apart from an older snapshot, where nil would read as "tear it down".
	VPNCaptured bool               `json:"vpn_captured,omitempty"`
	VPNProfile  *domain.VPNProfile `json:"vpn_profile,omitempty"`
	// Config is json:"-" on the model, so it rides here or not at all.
	VPNConfig string `json:"vpn_config,omitempty"`
}

type Snapshotter struct {
	Backend Backend
	Nft     *nft.Manager
	Paths   Paths
	// Restart is swapped in tests. A reload re-reads the files but leaves a link
	// running under the one it was moved to, so a restore that changed a
	// .network file has to restart.
	Restart func(context.Context) error
	// CaptureLAN and RestoreLAN reach the database without this package knowing
	// about repositories. Both nil is fine: older snapshots have no LAN either.
	CaptureLAN func(context.Context) (*domain.LANConfig, error)
	RestoreLAN func(context.Context, *domain.LANConfig) error
	// Same for the tunnel. RestoreVPN gets nil for "none was active", which has
	// to tear the device down.
	CaptureVPN func(context.Context) (*domain.VPNProfile, error)
	RestoreVPN func(context.Context, *domain.VPNProfile) error
}

// Capture reads current state
func (s *Snapshotter) Capture(ctx context.Context, tables []int) (*Snapshot, error) {
	snap := &Snapshot{
		Version: snapshotVersion,
		TakenAt: time.Now(),
		Routes:  map[int][]Route{},
	}

	var err error
	if snap.NetworkdFiles, snap.NetworkdModes, err = readDirFiles(s.Paths.NetworkdDir); err != nil {
		return nil, fmt.Errorf("snapshot networkd: %w", err)
	}
	if snap.NetworkdConfFiles, snap.NetworkdConfModes, err = readDirFiles(s.Paths.NetworkdConfDir); err != nil {
		return nil, fmt.Errorf("snapshot networkd.conf.d: %w", err)
	}
	if snap.NetplanFiles, snap.NetplanModes, err = readDirFiles(s.Paths.NetplanDir); err != nil {
		return nil, fmt.Errorf("snapshot netplan: %w", err)
	}
	if snap.SysctlFiles, snap.SysctlModes, err = readDirFiles(s.Paths.SysctlDir); err != nil {
		return nil, fmt.Errorf("snapshot sysctl.d: %w", err)
	}
	if snap.RTTablesFiles, snap.RTTablesModes, err = readDirFiles(s.Paths.RTTablesDir); err != nil {
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
		rs := s.Nft.Snapshot()
		snap.NftRuleset = rs.Render()
		snap.NftState = &rs
	}
	// Fail rather than look like an older snapshot: a rollback trusts this.
	if s.CaptureLAN != nil {
		cfg, err := s.CaptureLAN(ctx)
		if err != nil {
			return nil, fmt.Errorf("capture the LAN row: %w", err)
		}
		snap.LANConfig = cfg
	}
	if s.CaptureVPN != nil {
		p, err := s.CaptureVPN(ctx)
		if err != nil {
			return nil, fmt.Errorf("capture the active VPN profile: %w", err)
		}
		snap.VPNProfile, snap.VPNCaptured = p, true
		if p != nil {
			snap.VPNConfig = p.Config
		}
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
	// Put the config back where the model's json:"-" dropped it.
	if snap.VPNProfile != nil && snap.VPNConfig != "" {
		snap.VPNProfile.Config = snap.VPNConfig
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
	// linkFilesChanged tracks the dirs that decide which .network governs a
	// link. Reloading is enough to break that binding but not to restore it.
	var linkFilesChanged bool
	restoreDir := func(dir string, want map[string][]byte, modes map[string]uint32) {
		changed, err := writeDirExactly(dir, want, modes)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dir, err))
		}
		if changed {
			linkFilesChanged = true
		}
	}
	restoreDir(s.Paths.NetworkdDir, snap.NetworkdFiles, snap.NetworkdModes)
	restoreDir(s.Paths.NetworkdConfDir, snap.NetworkdConfFiles, snap.NetworkdConfModes)
	restoreDir(s.Paths.NetplanDir, snap.NetplanFiles, snap.NetplanModes)
	restoreDir(s.Paths.SysctlDir, snap.SysctlFiles, snap.SysctlModes)
	restoreDir(s.Paths.RTTablesDir, snap.RTTablesFiles, snap.RTTablesModes)

	// The takeover deleted netplan's generated units; putting the YAML back does
	// not recreate them, and networkd would come up owning nothing at all.
	if len(snap.NetplanFiles) > 0 {
		if err := netplanGenerate(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("netplan generate: %v", err))
		}
	}

	if snap.LANConfig != nil && s.RestoreLAN != nil {
		if err := s.RestoreLAN(ctx, snap.LANConfig); err != nil {
			errs = append(errs, fmt.Sprintf("restore the LAN row: %v", err))
		}
	}

	// nil is a real value here, so only act when the snapshot recorded one.
	if snap.VPNCaptured && s.RestoreVPN != nil {
		if err := s.RestoreVPN(ctx, snap.VPNProfile); err != nil {
			errs = append(errs, fmt.Sprintf("restore the tunnel: %v", err))
		}
	}

	// A reload re-reads the files but leaves a link running under the one it was
	// moved to, and never recreates a .netdev. Only a restart puts both back.
	if linkFilesChanged {
		restart := s.Restart
		if restart == nil {
			restart = RestartNetworkd
		}
		if err := restart(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("restart networkd: %v", err))
		}
	}

	// Put the table back as it was. Tear it down only when there was nothing
	// there: before the takeover, or a snapshot that never recorded the state.
	if s.Nft != nil {
		switch {
		case snap.NftState != nil && !snap.NftState.IsZero():
			if err := s.Nft.Replace(ctx, *snap.NftState); err != nil {
				errs = append(errs, fmt.Sprintf("nft restore: %v", err))
			}
		default:
			if err := s.Nft.Teardown(ctx); err != nil {
				errs = append(errs, fmt.Sprintf("nft teardown: %v", err))
			}
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
		for _, r := range restoreOrder(want) {
			if err := s.Backend.RouteReplace(ctx, r); err != nil {
				errs = append(errs, fmt.Sprintf("route replace %d %s: %v", table, r.Dest, err))
			}
		}
	}

	for _, unit := range snap.MaskedUnits {
		_ = exec.CommandContext(ctx, "systemctl", "mask", unit).Run()
	}
	nowMasked := maskedUnits("NetworkManager.service", "NetworkManager-wait-online.service")
	for _, unit := range unitsToUnmask(nowMasked, snap.MaskedUnits) {
		_ = exec.CommandContext(ctx, "systemctl", "unmask", unit).Run()
	}
	if !snap.CloudInitDisabled {
		_ = os.Remove(s.cloudInitPath())
	}

	if len(errs) > 0 {
		return fmt.Errorf("restore completed with errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// readDirFiles returns basename -> content for every regular file in dir.
// An unset path means the caller does not manage that directory.
func readDirFiles(dir string) (map[string][]byte, map[string]uint32, error) {
	out, modes := map[string][]byte{}, map[string]uint32{}
	if dir == "" {
		return out, modes, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, modes, nil
		}
		return nil, nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, err
		}
		out[e.Name()] = b
		if info, err := e.Info(); err == nil {
			modes[e.Name()] = uint32(info.Mode().Perm())
		}
	}
	return out, modes, nil
}

// writeDirExactly makes dir's contents equal want.
func writeDirExactly(dir string, want map[string][]byte, modes map[string]uint32) (bool, error) {
	if dir == "" {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	changed := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, keep := want[e.Name()]; !keep {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return false, err
			}
			changed = true
		}
	}
	for name, content := range want {
		mode := os.FileMode(0o644)
		if m, ok := modes[name]; ok {
			mode = os.FileMode(m)
		}
		path := filepath.Join(dir, name)
		if old, err := os.ReadFile(path); err != nil || !bytes.Equal(old, content) {
			changed = true
		}
		if err := os.WriteFile(path, content, mode); err != nil {
			return false, err
		}
		// WriteFile only applies the mode when it creates the file.
		if err := os.Chmod(path, mode); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// restoreOrder puts connected routes first: a default's gateway must be on-link
// or the kernel answers EINVAL, and snapshots list defaults first. Stable.
func restoreOrder(routes []Route) []Route {
	out := append([]Route(nil), routes...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Gateway == "" && out[j].Gateway != ""
	})
	return out
}

// unitsToUnmask lists what the takeover masked and the snapshot did not, so a
// revert leaves NetworkManager exactly as it found it. Without this it stays
// masked and nothing owns the links.
func unitsToUnmask(nowMasked, before []string) []string {
	was := map[string]bool{}
	for _, u := range before {
		was[u] = true
	}
	var out []string
	for _, u := range nowMasked {
		if !was[u] {
			out = append(out, u)
		}
	}
	return out
}

// netplanGenerate rebuilds /run/systemd/network from the restored YAML.
func netplanGenerate(ctx context.Context) error {
	err := exec.CommandContext(ctx, "netplan", "generate").Run()
	if errors.Is(err, exec.ErrNotFound) {
		return nil // not a netplan box
	}
	return err
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
