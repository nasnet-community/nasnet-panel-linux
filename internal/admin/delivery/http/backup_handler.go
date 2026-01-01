package http

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	adminUC "github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/audit"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/maintenance"
)

var backupFilenameRe = regexp.MustCompile(`^(backup|pre_restore)_\d{8}_\d{6}(_\d{4})?\.sql$`)

const maxUploadSize = 500 << 20 // 500 MB

// BackupHandler handles database backup endpoints
type BackupHandler struct {
	backupSvc *adminUC.BackupService
	auditUC   auditDomain.AuditLogUsecase
	settingUC settingDomain.SettingUsecase
}

// BackupInfo describes a single backup file
type BackupInfo struct {
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	SizeHuman string    `json:"size_human"`
	CreatedAt time.Time `json:"created_at"`
}

// RestoreResult is returned after a successful restore
type RestoreResult struct {
	Message         string `json:"message"`
	SafetyBackup    string `json:"safety_backup"`
	RequiresRestart bool   `json:"requires_restart"`
}

func NewBackupHandler(backupSvc *adminUC.BackupService, auditUC auditDomain.AuditLogUsecase, settingUC settingDomain.SettingUsecase) *BackupHandler {
	return &BackupHandler{
		backupSvc: backupSvc,
		auditUC:   auditUC,
		settingUC: settingUC,
	}
}

func (h *BackupHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/admin/backups")
	{
		g.POST("", h.CreateBackup)
		g.GET("", h.ListBackups)
		g.GET("/:filename/download", h.DownloadBackup)
		g.DELETE("/:filename", h.DeleteBackup)
		g.POST("/restore", h.RestoreBackup)
		g.POST("/:filename/restore", h.RestoreFromExisting)
	}
}

// CreateBackup runs pg_dump and returns backup info.
func (h *BackupHandler) CreateBackup(c *gin.Context) {
	if !h.backupSvc.TryLock() {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "Another backup or restore operation is already in progress",
		})
		return
	}
	defer h.backupSvc.Unlock()

	backupDir := h.backupSvc.BackupDir()
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to create backup directory: %v", err),
		})
		return
	}

	now := time.Now()
	filename := fmt.Sprintf("backup_%s_%04d.sql", now.Format("20060102_150405"), now.Nanosecond()/100000)
	filePath := filepath.Join(backupDir, filename)

	if err := h.backupSvc.RunPgDump(c.Request.Context(), filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "backup created but failed to stat file",
		})
		return
	}

	h.logAudit(c, auditDomain.AuditDataBackup, filename)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": BackupInfo{
			Filename:  filename,
			Size:      info.Size(),
			SizeHuman: humanSize(info.Size()),
			CreatedAt: info.ModTime(),
		},
	})
}

// ListBackups returns all backup files in the backup directory.
func (h *BackupHandler) ListBackups(c *gin.Context) {
	backupDir := h.backupSvc.BackupDir()
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"success": true, "data": []BackupInfo{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to read backup directory: %v", err),
		})
		return
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if entry.IsDir() || !backupFilenameRe.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupInfo{
			Filename:  entry.Name(),
			Size:      info.Size(),
			SizeHuman: humanSize(info.Size()),
			CreatedAt: info.ModTime(),
		})
	}

	// Sort newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	if backups == nil {
		backups = []BackupInfo{}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": backups})
}

// DownloadBackup streams a backup file to the client.
func (h *BackupHandler) DownloadBackup(c *gin.Context) {
	filename := filepath.Base(c.Param("filename"))
	if !backupFilenameRe.MatchString(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid filename"})
		return
	}

	filePath := filepath.Join(h.backupSvc.BackupDir(), filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "backup file not found"})
		return
	}

	h.logAudit(c, auditDomain.AuditBackupDownload, filename)
	c.FileAttachment(filePath, filename)
}

// DeleteBackup removes a backup file from disk.
func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	filename := filepath.Base(c.Param("filename"))
	if !backupFilenameRe.MatchString(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid filename"})
		return
	}

	filePath := filepath.Join(h.backupSvc.BackupDir(), filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "backup file not found"})
		return
	}

	if err := os.Remove(filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to delete backup: %v", err),
		})
		return
	}

	h.logAudit(c, auditDomain.AuditBackupDelete, filename)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "backup deleted"})
}

// RestoreBackup restores the database from an uploaded .sql file.
func (h *BackupHandler) RestoreBackup(c *gin.Context) {
	if !h.backupSvc.TryLock() {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "Another backup or restore operation is already in progress",
		})
		return
	}
	defer h.backupSvc.Unlock()

	// Limit upload size
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)

	// Receive uploaded file
	file, header, err := c.Request.FormFile("backup_file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "backup_file is required",
		})
		return
	}
	defer file.Close()

	if !strings.HasSuffix(header.Filename, ".sql") {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "only .sql files are accepted",
		})
		return
	}

	// Save uploaded file to temp path
	backupDir := h.backupSvc.BackupDir()
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to create backup directory: %v", err),
		})
		return
	}

	tempPath := filepath.Join(backupDir, fmt.Sprintf("upload_restore_%d.sql", time.Now().UnixNano()))
	out, err := os.Create(tempPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to save uploaded file: %v", err),
		})
		return
	}

	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		os.Remove(tempPath)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to write uploaded file: %v", err),
		})
		return
	}
	out.Close()

	// Cleanup temp file when done
	defer os.Remove(tempPath)

	// Validate SQL content
	if err := h.validateSQLFile(tempPath); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	h.executeRestore(c, tempPath, header.Filename)
}

// RestoreFromExisting restores the database from a backup already on disk.
func (h *BackupHandler) RestoreFromExisting(c *gin.Context) {
	if !h.backupSvc.TryLock() {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "Another backup or restore operation is already in progress",
		})
		return
	}
	defer h.backupSvc.Unlock()

	filename := filepath.Base(c.Param("filename"))
	if !backupFilenameRe.MatchString(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid filename"})
		return
	}

	filePath := filepath.Join(h.backupSvc.BackupDir(), filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "backup file not found"})
		return
	}

	h.executeRestore(c, filePath, filename)
}

// executeRestore contains the shared restore logic for both upload and
// restore-from-existing flows.
func (h *BackupHandler) executeRestore(c *gin.Context, sqlFilePath, displayName string) {
	log := logger.GetLogger()
	ctx := c.Request.Context()
	backupDir := h.backupSvc.BackupDir()

	// Audit: restore start
	h.logAudit(c, auditDomain.AuditRestoreStart, displayName)

	// Enable maintenance mode — all other endpoints return 503. For a
	// successful SQLite restore we keep maintenance ON until the scheduled
	// process restart (the pool is closed and any request hitting it would
	// fail with "sql: database is closed" during the ~2s SIGTERM window).
	maintenance.Enable("Database restore in progress")
	keepMaintenance := false
	defer func() {
		if !keepMaintenance {
			maintenance.Disable()
		}
	}()

	// Create safety backup before destructive operation
	now := time.Now()
	safetyFilename := fmt.Sprintf("pre_restore_%s_%04d.sql", now.Format("20060102_150405"), now.Nanosecond()/100000)
	safetyPath := filepath.Join(backupDir, safetyFilename)

	if err := h.backupSvc.RunPgDump(ctx, safetyPath); err != nil {
		h.logAudit(c, auditDomain.AuditRestoreFailed, fmt.Sprintf("safety backup failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("failed to create safety backup before restore: %v", err),
		})
		return
	}

	// Execute atomic restore (PG: single transaction, SQLite: file replace)
	output, err := h.backupSvc.AtomicRestore(ctx, sqlFilePath)
	if err != nil {
		errMsg := fmt.Sprintf("restore failed: %v", err)
		if len(output) > 0 {
			errMsg += " — " + string(output)
		}
		log.WithError(err).WithField("output", string(output)).Error("Restore failed")
		h.logAudit(c, auditDomain.AuditRestoreFailed, errMsg)

		// Auto-recover from safety backup (PostgreSQL only). SQLite leaves
		// the live DB untouched on pre-rename failure; post-rename failure
		// is unrecoverable in-process — operator uses safetyPath manually.
		if database.IsPostgres() {
			log.Info("Attempting auto-recovery from safety backup")
			recoveryOutput, recoveryErr := h.backupSvc.AtomicRestore(ctx, safetyPath)
			if recoveryErr != nil {
				log.WithError(recoveryErr).WithField("output", string(recoveryOutput)).Error("Auto-recovery from safety backup also failed")
				c.JSON(http.StatusInternalServerError, gin.H{
					"success":       false,
					"error":         fmt.Sprintf("restore failed AND auto-recovery failed: %v — safety backup available on disk: %s", recoveryErr, safetyFilename),
					"safety_backup": safetyFilename,
				})
				return
			}
			log.Info("Auto-recovered from safety backup after failed restore")
			c.JSON(http.StatusInternalServerError, gin.H{
				"success":       false,
				"error":         fmt.Sprintf("restore failed: %v — database was automatically recovered from safety backup", err),
				"recovered":     true,
				"safety_backup": safetyFilename,
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success":       false,
			"error":         errMsg,
			"safety_backup": safetyFilename,
		})
		return
	}

	// Re-apply deployment-specific settings (URLs, ports, tokens) from the
	// current .env so the restored backup doesn't break the panel.
	if database.IsPostgres() {
		// PostgreSQL: DB is still open, reseed directly.
		if err := h.settingUC.ReseedEnvSettings(ctx); err != nil {
			log.WithError(err).Warn("Failed to reseed env settings after restore")
		}
	} else {
		// SQLite: DB connections are closed and a restart is pending.
		// Write a marker file so the next startup reseeds automatically.
		markerPath := filepath.Join(backupDir, ".reseed_after_restore")
		if err := os.WriteFile(markerPath, []byte("1"), 0600); err != nil {
			log.WithError(err).Warn("Failed to write reseed marker file")
		}
		// Hold maintenance mode open across the SIGTERM window so requests
		// don't hit the closed pool between handler return and restart.
		keepMaintenance = true
	}

	h.logAudit(c, auditDomain.AuditRestoreComplete, fmt.Sprintf("restored from %s, safety: %s", displayName, safetyFilename))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": RestoreResult{
			Message:         "Database restored successfully",
			SafetyBackup:    safetyFilename,
			RequiresRestart: true,
		},
	})
}

// validateSQLFile reads the first 4KB of the file and checks that it looks
// like a valid SQL backup for the current database driver.
func (h *BackupHandler) validateSQLFile(filePath string) error {
	probe, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to read uploaded file")
	}
	buf := make([]byte, 4096)
	n, _ := probe.Read(buf)
	probe.Close()
	header4k := string(buf[:n])

	// Reject binary pg_dump format (custom/directory/tar)
	if strings.HasPrefix(header4k, "PGDMP") {
		return fmt.Errorf("binary pg_dump format (custom/tar) is not supported — please use plain SQL format")
	}

	if database.IsSQLite() {
		// SQLite backups are raw database files — check magic bytes
		if strings.HasPrefix(header4k, "SQLite format 3") {
			return nil
		}
		return fmt.Errorf("file does not appear to be a valid SQLite database")
	}

	// PostgreSQL: require the pg_dump header comment
	if !strings.Contains(header4k, "-- PostgreSQL database dump") {
		return fmt.Errorf("file does not appear to be a valid PostgreSQL dump (missing pg_dump header)")
	}

	// Reject dangerous standalone statements
	upper := strings.ToUpper(header4k)
	for _, pattern := range []string{"DROP DATABASE", "DROP ROLE", "CREATE ROLE", "ALTER ROLE"} {
		if strings.Contains(upper, pattern) {
			return fmt.Errorf("file contains disallowed statement: %s", pattern)
		}
	}

	return nil
}

// logAudit logs a backup/restore action to the audit trail.
func (h *BackupHandler) logAudit(c *gin.Context, action auditDomain.AuditAction, newVal string) {
	if h.auditUC == nil {
		return
	}
	ac := audit.FromGinContext(c)
	h.auditUC.Log(c.Request.Context(), &auditDomain.AuditLog{
		Action:     string(action),
		ActorID:    ac.ActorID,
		ActorName:  ac.ActorName,
		EntityType: "backup",
		NewValues:  newVal,
		IPAddress:  ac.IPAddress,
		RequestID:  ac.RequestID,
		Source:     "http",
	})
}

// humanSize formats a byte count into a human-readable string.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
