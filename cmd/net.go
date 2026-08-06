package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	"github.com/spf13/cobra"
)

func newNetCmd() *cobra.Command {
	net := &cobra.Command{
		Use:   "net",
		Short: "Network administration",
	}

	var ifExpired bool
	rollback := &cobra.Command{
		Use:   "rollback",
		Short: "Revert the last network apply",
		Long: `Revert the last network apply from its snapshot.

With --if-expired this exits 0 immediately unless /var/lib/nasnet/net-pending.json
holds an unconfirmed deadline that has passed. That is how nasnet-netrollback.timer
calls it, every 10 seconds, so the no-op path must stay cheap.

This lives outside the panel deliberately: a bad network apply is most likely to
break the panel itself, and a dead-man held by the panel process would die with
it. The same check runs at boot before the reconciler, so a reboot mid-apply
reverts too.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runNetRollback(ifExpired)
		},
	}
	rollback.Flags().BoolVar(&ifExpired, "if-expired", false,
		"exit 0 unless an unconfirmed apply deadline has passed")

	net.AddCommand(rollback)
	return net
}

func statePaths() system.Paths {
	p := system.DefaultPaths()
	if d := os.Getenv("NASNET_STATE_DIR"); d != "" {
		p.StateDir = d
	}
	return p
}

func runNetRollback(ifExpired bool) error {
	paths := statePaths()

	// No marker means nothing to do. Never open the DB to find that out.
	m, err := system.ReadMarker(paths)
	if err != nil {
		return fmt.Errorf("read marker: %w", err)
	}
	if m == nil {
		return nil
	}
	if ifExpired && !m.Expired(time.Now()) {
		return nil
	}

	cfg := config.Load()
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	backend, err := system.NewNetlinkBackend()
	if err != nil {
		return fmt.Errorf("netlink backend: %w", err)
	}

	applier := &system.Applier{
		Snap: &system.Snapshotter{
			Backend: backend,
			Nft:     nft.NewManager(nft.NewCmdApplier("")),
			Paths:   paths,
		},
		Repo:   repository.NewApplyRepository(db),
		Paths:  paths,
		Reload: system.ReloadNetworkd,
	}

	did, err := applier.Rollback(context.Background(), ifExpired)
	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	if did {
		fmt.Fprintf(os.Stderr, "nasnet: reverted network apply %d (confirm window expired)\n", m.PlanID)
	}
	return nil
}
