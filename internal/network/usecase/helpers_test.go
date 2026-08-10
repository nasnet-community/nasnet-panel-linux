package usecase

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func testPaths(t *testing.T) system.Paths {
	t.Helper()
	root := t.TempDir()
	p := system.Paths{
		NetworkdDir:        filepath.Join(root, "etc/systemd/network"),
		NetworkdConfDir:    filepath.Join(root, "etc/systemd/networkd.conf.d"),
		NetplanDir:         filepath.Join(root, "etc/netplan"),
		NetplanDisabledDir: filepath.Join(root, "etc/netplan.disabled"),
		SysctlDir:          filepath.Join(root, "etc/sysctl.d"),
		RTTablesDir:        filepath.Join(root, "etc/iproute2/rt_tables.d"),
		// Must be set, or Restore deletes relative to the package dir.
		CloudInitDir: filepath.Join(root, "etc/cloud/cloud.cfg.d"),
		StateDir:     filepath.Join(root, "var/lib/nasnet"),
	}
	for _, d := range []string{p.NetworkdDir, p.NetworkdConfDir, p.NetplanDir, p.SysctlDir,
		p.RTTablesDir, p.CloudInitDir, p.StateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// uplinksWithKeys is twoUplinks() with the stable keys PortForward rows point at.
func uplinksWithKeys() []Uplink {
	us := twoUplinks()
	us[0].Key = "aa:bb:cc:dd:ee:01"
	us[1].Key = "aa:bb:cc:dd:ee:02"
	return us
}

func newTestSnapshotter(t *testing.T, p system.Paths) *system.Snapshotter {
	t.Helper()
	return &system.Snapshotter{
		Backend: system.NewFakeBackend(),
		Nft:     nft.NewManager(&nft.FakeApplier{}),
		Paths:   p,
	}
}
