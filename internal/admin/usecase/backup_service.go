package usecase

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
)

// BackupService encapsulates database backup and restore operations.
// A single instance should be shared across HTTP and Telegram delivery layers
// so the mutex protects against concurrent operations from any source.
type BackupService struct {
	db        *gorm.DB
	dbConfig  config.DatabaseConfig
	backupDir string
	mu        sync.Mutex
}

func NewBackupService(db *gorm.DB, dbConfig config.DatabaseConfig, backupDir string) *BackupService {
	svc := &BackupService{
		db:        db,
		dbConfig:  dbConfig,
		backupDir: backupDir,
	}
	svc.CleanupStaleTemps()
	return svc
}

// TryLock attempts to acquire the service-level mutex.
// Returns true if the lock was acquired.
func (s *BackupService) TryLock() bool { return s.mu.TryLock() }

// Unlock releases the service-level mutex.
func (s *BackupService) Unlock() { s.mu.Unlock() }

// BackupDir returns the configured backup directory.
func (s *BackupService) BackupDir() string { return s.backupDir }

// DropAndRecreateSchema drops all data and recreates the schema.
// PostgreSQL: DROP SCHEMA public CASCADE + CREATE SCHEMA public
// SQLite: iterate all tables and drop them
func (s *BackupService) DropAndRecreateSchema(ctx context.Context) error {
	if database.IsSQLite() {
		return s.dropAllTablesSQLite(ctx)
	}
	if err := s.db.WithContext(ctx).Exec("DROP SCHEMA public CASCADE").Error; err != nil {
		return fmt.Errorf("failed to drop schema: %w", err)
	}
	if err := s.db.WithContext(ctx).Exec("CREATE SCHEMA public").Error; err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	return nil
}

// dropAllTablesSQLite drops all user tables in the SQLite database.
func (s *BackupService) dropAllTablesSQLite(ctx context.Context) error {
	var tables []string
	if err := s.db.WithContext(ctx).Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("failed to list tables: %w", err)
	}
	log := logger.GetLogger()
	// Disable FK checks while dropping
	if err := s.db.WithContext(ctx).Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		log.WithError(err).Warn("[dropAllTablesSQLite] Failed to disable foreign key checks")
	}
	for _, table := range tables {
		if err := s.db.WithContext(ctx).Exec(fmt.Sprintf("DROP TABLE IF EXISTS \"%s\"", table)).Error; err != nil {
			s.db.WithContext(ctx).Exec("PRAGMA foreign_keys = ON")
			return fmt.Errorf("failed to drop table %s: %w", table, err)
		}
	}
	if err := s.db.WithContext(ctx).Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		log.WithError(err).Warn("[dropAllTablesSQLite] Failed to re-enable foreign key checks")
	}
	return nil
}

// RunPgDump executes pg_dump (PostgreSQL) or copies the DB file (SQLite)
// and writes the output to filePath.
func (s *BackupService) RunPgDump(ctx context.Context, filePath string) error {
	if database.IsSQLite() {
		return s.sqliteBackup(ctx, filePath)
	}
	return s.pgDump(ctx, filePath)
}

// AtomicRestore: PG runs DROP SCHEMA + restore in a single
// psql --single-transaction (rollback on any failure). SQLite replaces
// the DB file and schedules a process restart.
func (s *BackupService) AtomicRestore(ctx context.Context, sqlFilePath string) ([]byte, error) {
	if database.IsSQLite() {
		return nil, s.sqliteRestore(ctx, sqlFilePath)
	}
	return s.atomicPsqlRestore(ctx, sqlFilePath)
}

// CleanupStaleTemps removes leftover temporary upload files from the backup
// directory (e.g., from a previous crash during restore).
func (s *BackupService) CleanupStaleTemps() {
	if s.backupDir == "" {
		return
	}
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return
	}
	log := logger.GetLogger()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "upload_restore_") {
			path := filepath.Join(s.backupDir, e.Name())
			if err := os.Remove(path); err == nil {
				log.WithField("file", e.Name()).Info("Cleaned up stale temp file")
			}
		}
	}
}

// --- PostgreSQL implementation ---

func (s *BackupService) pgDump(ctx context.Context, filePath string) error {
	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		return fmt.Errorf("pg_dump binary not found: %w", err)
	}

	args := []string{
		"-h", s.dbConfig.Host,
		"-p", fmt.Sprint(s.dbConfig.Port),
		"-U", s.dbConfig.User,
		"-d", s.dbConfig.Name,
		"-f", filePath,
		"--no-owner",
		"--no-privileges",
	}

	cmd := exec.CommandContext(ctx, pgDump, args...)
	cmd.Env = s.buildPgEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_dump failed: %v — %s", err, string(output))
	}
	return nil
}

// atomicPsqlRestore: DROP SCHEMAs + restore in one psql transaction
// (PG's transactional DDL → full rollback on any failure). Drops every
// user-created schema, not just public.
const dropAllSchemasSQL = `DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name NOT IN ('pg_catalog','information_schema','pg_toast')
      AND schema_name NOT LIKE 'pg_temp_%'
      AND schema_name NOT LIKE 'pg_toast_temp_%'
  LOOP
    EXECUTE 'DROP SCHEMA IF EXISTS ' || quote_ident(r.schema_name) || ' CASCADE';
  END LOOP;
END $$;
CREATE SCHEMA IF NOT EXISTS public;`

func (s *BackupService) atomicPsqlRestore(ctx context.Context, sqlFilePath string) ([]byte, error) {
	psqlPath, err := exec.LookPath("psql")
	if err != nil {
		return nil, fmt.Errorf("psql binary not found: %w", err)
	}

	args := []string{
		"-h", s.dbConfig.Host,
		"-p", fmt.Sprint(s.dbConfig.Port),
		"-U", s.dbConfig.User,
		"-d", s.dbConfig.Name,
		"--single-transaction",
		"--set", "ON_ERROR_STOP=on",
		"-c", dropAllSchemasSQL,
		"-f", sqlFilePath,
	}

	cmd := exec.CommandContext(ctx, psqlPath, args...)
	cmd.Env = s.buildPgEnv()

	return cmd.CombinedOutput()
}

// buildPgEnv returns the current environment with PGPASSWORD set.
func (s *BackupService) buildPgEnv() []string {
	return append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", s.dbConfig.Password))
}

// --- SQLite implementation ---

// sqliteBackup creates a consistent, compacted backup via VACUUM INTO.
// VACUUM INTO takes an internal read lock, writes only live pages to the
// destination (dropping freelist pages), and produces an atomic snapshot —
// so the backup file reflects actual row count rather than the on-disk size
// of the live database (which retains freed pages until VACUUM).
func (s *BackupService) sqliteBackup(ctx context.Context, filePath string) error {
	log := logger.GetLogger()

	// VACUUM INTO fails if the target already exists.
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear backup target: %w", err)
	}

	if err := s.db.WithContext(ctx).Exec("VACUUM INTO ?", filePath).Error; err != nil {
		return fmt.Errorf("VACUUM INTO failed: %w", err)
	}

	log.WithField("path", filePath).Info("SQLite backup created")
	return nil
}

// sqliteRestore: copy-to-tmp + fsync + atomic rename. Pre-rename
// failures leave the live DB intact. Process restart required after
// rename (repos hold the old *gorm.DB pointer).
func (s *BackupService) sqliteRestore(_ context.Context, backupPath string) error {
	log := logger.GetLogger()
	dstPath := s.dbConfig.Path
	dstDir := filepath.Dir(dstPath)
	tmpPath := dstPath + ".restore.tmp"

	// Drop any stale tmp from a prior aborted restore.
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear stale restore tmp: %w", err)
	}

	// Copy + fsync while live DB still open; failure leaves it untouched.
	if err := copyFileSync(backupPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy to tmp failed (live DB unchanged): %w", err)
	}

	// fsync dir so the tmp inode is durable before rename.
	if err := syncDir(dstDir); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync dir failed (live DB unchanged): %w", err)
	}

	// Close live pool; app can't serve DB requests until process restart.
	sqlDB, err := s.db.DB()
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		log.WithError(err).Warn("[sqliteRestore] Error closing database connections")
	}

	// Atomic rename: on failure the old inode is still in place.
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename tmp to live failed: %w", err)
	}

	// Remove stale WAL/SHM sidecars; SQLite recreates them on first write.
	_ = os.Remove(dstPath + "-wal")
	_ = os.Remove(dstPath + "-shm")

	// Persist the rename + sidecar removals.
	_ = syncDir(dstDir)

	log.WithField("path", dstPath).Info("SQLite database restored, scheduling restart")
	s.scheduleRestart()
	return nil
}

// copyFileSync copies src to dst, fsyncs dst, then closes it. dst is created
// with O_TRUNC so a stale tmp from a previous run is overwritten cleanly.
func copyFileSync(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("copy: %w", err)
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return fmt.Errorf("fsync file: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

// syncDir fsyncs a directory so that entry-level operations (create, rename,
// unlink) become durable before returning.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// scheduleRestart sends SIGTERM to the current process after a brief delay,
// allowing the HTTP response to be sent before shutdown begins.
func (s *BackupService) scheduleRestart() {
	go func() {
		time.Sleep(2 * time.Second)
		p, _ := os.FindProcess(os.Getpid())
		p.Signal(syscall.SIGTERM)
	}()
}
