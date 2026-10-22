package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	settingRepo "github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	updateUC "github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

func newUpdateCmd() *cobra.Command {
	update := &cobra.Command{
		Use:   "update",
		Short: "Check for, install and roll back panel releases",
	}
	quiet := func(cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.SilenceUsage = true
		}
	}

	var asJSON bool
	check := &cobra.Command{
		Use:   "check",
		Short: "Look for a newer release on the update channel",
		RunE: func(_ *cobra.Command, _ []string) error {
			u, err := cliUpdater()
			if err != nil {
				return err
			}
			res, err := u.CheckNow(context.Background())
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(res)
			}
			switch {
			case res.Error != "":
				return fmt.Errorf("check failed: %s", res.Error)
			case res.Available:
				fmt.Printf("%s is available (running %s).\n", res.Latest.Version, Version)
			case res.Blocked != "":
				fmt.Println(res.Blocked)
			default:
				fmt.Printf("Up to date (%s).\n", Version)
			}
			return nil
		},
	}
	check.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")

	status := &cobra.Command{
		Use:   "status",
		Short: "Show the version, the last check and the current job",
		RunE: func(_ *cobra.Command, _ []string) error {
			u, err := cliUpdater()
			if err != nil {
				return err
			}
			st, err := u.Status(context.Background())
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(st)
			}
			fmt.Printf("Running %s (%s install, %s channel).\n", st.Current, st.Kind.Name, st.Channel)
			if st.Kind.Why != "" {
				fmt.Printf("%s Run: %s\n", st.Kind.Why, st.Kind.Command)
			}
			if st.Available != nil {
				fmt.Printf("%s is available.\n", st.Available.Version)
			}
			if st.Job != nil {
				fmt.Printf("Last job: %s %s → %s, %s %s\n", st.Job.Kind, st.Job.From, st.Job.To, st.Job.Phase, st.Job.Result)
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "print the status as JSON")

	quiet(check, status)
	update.AddCommand(check, status)
	return update
}

// cliUpdater builds the updater the way the panel does, from the install
// root's .env and settings.
func cliUpdater() (*updateUC.Usecase, error) {
	root := chdirInstallRoot()
	cfg := config.Load()
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	settings := settingRepo.NewSettingRepository(db)

	f := httpclient.NewFactory()
	f.SetRouterMode(cfg.Router.Enabled, httpclient.EgressDomestic)
	ctx := context.Background()
	var proxyURL string
	if s, err := settings.GetByKey(ctx, "outbound_proxy_url"); err == nil {
		proxyURL = s.Value
	}
	enabled := map[httpclient.Feature]bool{}
	if s, err := settings.GetByKey(ctx, "proxy_use_"+string(httpclient.FeaturePanelUpdate)); err == nil {
		enabled[httpclient.FeaturePanelUpdate] = s.Value == "true"
	}
	f.Update(httpclient.Config{ProxyURL: proxyURL, Enabled: enabled})

	return updateUC.New(updateUC.Deps{
		Paths:    updateUC.Paths{Root: root},
		Version:  Version,
		Settings: settings,
		DB:       db,
		HTTP:     f,
	}), nil
}

// chdirInstallRoot moves to the install root when this binary sits in its
// bin/ next to a .env, so the CLI works from any directory.
func chdirInstallRoot() string {
	wd, _ := os.Getwd()
	exe, err := os.Executable()
	if err != nil {
		return wd
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	root := filepath.Dir(filepath.Dir(exe))
	if filepath.Base(filepath.Dir(exe)) != "bin" {
		return wd
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); err != nil {
		return wd
	}
	if err := os.Chdir(root); err != nil {
		return wd
	}
	return root
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
