package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/nasnet-community/nasnet-panel-linux/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// DriverName holds the active database driver ("postgres" or "sqlite").
var DriverName string

// IsPostgres returns true when the active driver is PostgreSQL.
func IsPostgres() bool { return DriverName == "postgres" }

// IsSQLite returns true when the active driver is SQLite.
func IsSQLite() bool { return DriverName == "sqlite" }

func Connect(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	log := logger.GetLogger()

	driver := strings.ToLower(cfg.Driver)
	if driver == "" {
		driver = "postgres"
	}
	DriverName = driver

	var dialector gorm.Dialector

	switch driver {
	case "sqlite":
		// Ensure the parent directory exists
		dir := filepath.Dir(cfg.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create SQLite directory %s: %w", dir, err)
		}
		// Enable WAL mode, foreign keys, and busy timeout via pragmas in the DSN
		dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", cfg.Path)
		dialector = sqlite.Open(dsn)
		log.WithField("path", cfg.Path).Info("Using SQLite database")

	default: // "postgres"
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
		)
		dialector = postgres.Open(dsn)
		log.Info("Using PostgreSQL database")
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		log.WithError(err).Error("Failed to connect to database")
		return nil, err
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		log.WithError(err).Warn("Failed to get sql.DB for pool configuration")
	} else if driver == "sqlite" {
		// SQLite only supports one writer at a time. Using a single
		// connection serialises all access and eliminates SQLITE_BUSY
		// errors from concurrent goroutines contending for the write lock.
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(0) // don't close the single connection
		sqlDB.SetConnMaxIdleTime(0)
	} else {
		maxOpen := cfg.MaxOpenConns
		if maxOpen <= 0 {
			maxOpen = 25
		}
		maxIdle := cfg.MaxIdleConns
		if maxIdle <= 0 {
			maxIdle = 10
		}
		lifetime := cfg.ConnMaxLifetime
		if lifetime <= 0 {
			lifetime = 5
		}
		idleTime := cfg.ConnMaxIdleTime
		if idleTime <= 0 {
			idleTime = 3
		}
		sqlDB.SetMaxOpenConns(maxOpen)
		sqlDB.SetMaxIdleConns(maxIdle)
		sqlDB.SetConnMaxLifetime(time.Duration(lifetime) * time.Minute)
		sqlDB.SetConnMaxIdleTime(time.Duration(idleTime) * time.Minute)
		log.WithFields(map[string]interface{}{
			"max_open": maxOpen, "max_idle": maxIdle,
			"lifetime_min": lifetime, "idle_min": idleTime,
		}).Info("Database connection pool configured")
	}

	DB = db
	log.Info("Database connection established")
	return db, nil
}

func AutoMigrate(db *gorm.DB, models ...interface{}) error {
	log := logger.GetLogger()

	if err := db.AutoMigrate(models...); err != nil {
		log.WithError(err).Error("Failed to run auto migrations")
		return err
	}

	log.Info("Database migrations completed")
	return nil
}
