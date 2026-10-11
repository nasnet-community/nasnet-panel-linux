package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	settingRepo "github.com/nasnet-community/nasnet-panel-linux/internal/setting/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// nasnet-tool tells "never started" apart from a failure by this exit code.
const exitDatabaseNotInitialized = 3

var errDatabaseNotInitialized = errors.New("panel database is not initialized yet")

var setAdminPasswordHashCmd = &cobra.Command{
	Use:   "set-admin-password-hash",
	Short: "Store a bcrypt admin password hash read from stdin; takes effect at the next login",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// Keep stdout/stderr for the result; the tool shows stderr on failure.
		logrus.SetLevel(logrus.ErrorLevel)
		logger.Init("error", "text")
		hash, err := readAdminPasswordHash(cmd.InOrStdin())
		if err == nil {
			cfg := config.Load()
			err = storeAdminPasswordHash(context.Background(), &cfg.Database, hash)
		}
		switch {
		case errors.Is(err, errDatabaseNotInitialized):
			fmt.Fprintln(os.Stderr, err)
			os.Exit(exitDatabaseNotInitialized)
		case err != nil:
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(setAdminPasswordHashCmd)
}

// Read the hash from stdin so it never shows up in the process list.
func readAdminPasswordHash(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	hash := strings.TrimSpace(line)
	if err := (config.AdminConfig{Username: "admin", PasswordHash: hash}).Validate(); err != nil {
		return "", errors.New("stdin must contain one complete bcrypt hash")
	}
	return hash, nil
}

func storeAdminPasswordHash(ctx context.Context, cfg *config.DatabaseConfig, hash string) error {
	// Opening a missing SQLite path would create an empty database owned by the caller.
	if strings.EqualFold(cfg.Driver, "sqlite") {
		if _, err := os.Stat(cfg.Path); errors.Is(err, os.ErrNotExist) {
			return errDatabaseNotInitialized
		}
	}
	db, err := database.Connect(cfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	if !db.Migrator().HasTable(&settingDomain.Setting{}) {
		return errDatabaseNotInitialized
	}
	setting := settingDomain.AdminPasswordHashSetting(hash)
	if err := settingRepo.NewSettingRepository(db).UpdateMany(ctx, []*settingDomain.Setting{setting}); err != nil {
		return fmt.Errorf("save admin password: %w", err)
	}
	return nil
}
