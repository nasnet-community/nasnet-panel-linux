package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	adminUC "github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	settingRepo "github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	updateDomain "github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
	updateUC "github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"
	xrayUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/xray/usecase"
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
			if st.Rollback != nil {
				fmt.Printf("Roll back to %s possible until %s.\n", st.Rollback.Version, st.Rollback.Until.Local().Format("Jan 2 15:04"))
			}
			if st.Job != nil {
				fmt.Printf("Last job: %s %s → %s, %s %s\n", st.Job.Kind, st.Job.From, st.Job.To, st.Job.Phase, st.Job.Result)
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "print the status as JSON")

	var version, file, jobID string
	var yes bool
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Install the newest release, a --version, or an offline bundle --file",
		Long: `Install a release the way Settings → Updates does: checks, a database
backup, a swap and a trial boot that puts the old version back if the new
one doesn't come up healthy within 2 minutes.

--job is for the update runner unit the panel starts; don't pass it by hand.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			if jobID != "" {
				// the runner needs nothing but files and systemctl, not even the database
				runner := updateUC.New(updateUC.Deps{Paths: updateUC.Paths{Root: chdirInstallRoot()}, Version: Version})
				return runner.RunJob(ctx, jobID)
			}
			u, err := cliUpdater()
			if err != nil {
				return err
			}
			req := updateUC.Request{Version: version}
			switch {
			case file != "":
				f, err := os.Open(file)
				if err != nil {
					return err
				}
				up, err := u.SaveUpload(f)
				f.Close()
				if err != nil {
					return err
				}
				req = updateUC.Request{UploadID: up.ID}
				version = up.Version
			case version == "":
				res, err := u.CheckNow(ctx)
				if err != nil {
					return err
				}
				if res.Error != "" {
					return fmt.Errorf("check failed: %s", res.Error)
				}
				if !res.Available {
					if res.Blocked != "" {
						return errors.New(res.Blocked)
					}
					fmt.Printf("Up to date (%s).\n", Version)
					return nil
				}
				version = res.Latest.Version
				req.Version = version
			}
			if !yes && !confirm(fmt.Sprintf("Update %s to %s? The panel restarts and is down for about a minute.", Version, version)) {
				return nil
			}
			job, err := u.Apply(ctx, req)
			if err != nil {
				return err
			}
			return followJob(u, job.ID)
		},
	}
	apply.Flags().StringVar(&version, "version", "", "the release to install, e.g. v1.1.0")
	apply.Flags().StringVar(&file, "file", "", "an offline bundle (.tar.gz) to install")
	apply.Flags().StringVar(&jobID, "job", "", "finish this job (the runner unit)")
	apply.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask")
	_ = apply.Flags().MarkHidden("job")

	rollback := &cobra.Command{
		Use:   "rollback",
		Short: "Go back to the version before the last update, within 7 days",
		Long: `Put the previous version's binaries and system files back. The database is
left as it is; the pre-update backup is in Backups for a manual restore.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			u, err := cliUpdater()
			if err != nil {
				return err
			}
			st, err := u.Status(context.Background())
			if err != nil {
				return err
			}
			if st.Rollback == nil {
				return errors.New("there's no earlier version to roll back to")
			}
			if !yes && !confirm(fmt.Sprintf("Roll back %s to %s? The database stays as it is.", Version, st.Rollback.Version)) {
				return nil
			}
			job, err := u.Rollback(context.Background())
			if err != nil {
				return err
			}
			return followJob(u, job.ID)
		},
	}
	rollback.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask")

	selftest := &cobra.Command{
		Use:    "selftest",
		Short:  "Check this binary can run the install it sits in",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return updateSelftest()
		},
	}

	quiet(check, status, apply, rollback, selftest)
	update.AddCommand(check, status, apply, rollback, selftest)
	return update
}

// cliUpdater builds the updater the way the panel does, from the install
// root's .env and settings. It never announces; the panel does.
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

	health := fmt.Sprintf("127.0.0.1:%d", cfg.App.Port)
	return updateUC.New(updateUC.Deps{
		Paths:       updateUC.Paths{Root: root},
		Version:     Version,
		Settings:    settings,
		DB:          db,
		HTTP:        f,
		Backups:     adminUC.NewBackupService(db, cfg.Database, cfg.App.BackupDir),
		DBSize:      settingRepo.NewStatusQueries(db).DBSize,
		NetPending:  netApplyPending,
		XrayRunning: func() bool { return updateUC.PanelXrayRunning(context.Background(), health) },
		XrayCache:   xrayUsecase.NewBinaryManager(xrayUsecase.CacheDir, f),
		BuiltinXray: func(ctx context.Context) string {
			if s, err := settings.GetByKey(ctx, settingDomain.BuiltinXrayKey); err == nil {
				return s.Value
			}
			return ""
		},
		HealthAddr: health,
	}), nil
}

// followJob prints each step as it changes until the job ends.
func followJob(u *updateUC.Usecase, id string) error {
	seen := map[string]string{}
	for {
		job, ok := u.Job(id)
		if !ok {
			return errors.New("the job is gone")
		}
		for _, s := range job.Steps {
			line := s.Status + " " + s.Detail
			if seen[s.ID] == line || s.Status == updateDomain.StepWaiting || s.Status == updateDomain.StepSkipped {
				continue
			}
			seen[s.ID] = line
			mark := map[string]string{updateDomain.StepRunning: "…", updateDomain.StepDone: "✓", updateDomain.StepFailed: "✗"}[s.Status]
			if s.Detail != "" {
				fmt.Printf("%s %s · %s\n", mark, s.Label, s.Detail)
			} else {
				fmt.Printf("%s %s\n", mark, s.Label)
			}
		}
		if job.Finished() {
			switch {
			case job.Result == updateDomain.ResultOK && job.Kind == updateDomain.KindRollback:
				fmt.Printf("Rolled back to %s.\n", job.To)
			case job.Result == updateDomain.ResultOK:
				fmt.Printf("Updated to %s.\n", job.To)
			default:
				return errors.New(job.Error)
			}
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func confirm(q string) bool {
	fmt.Printf("%s [y/N] ", q)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

// updateSelftest is run on a new binary before it's swapped in: it must
// read this install's .env and database.
func updateSelftest() error {
	cfg := config.Load()
	if strings.EqualFold(cfg.Database.Driver, "sqlite") {
		if _, err := os.Stat(cfg.Database.Path); err != nil {
			return fmt.Errorf("the database file %s is missing", cfg.Database.Path)
		}
	}
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int64
	if err := db.WithContext(ctx).Table("settings").Count(&n).Error; err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	fmt.Println("ok")
	return nil
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
