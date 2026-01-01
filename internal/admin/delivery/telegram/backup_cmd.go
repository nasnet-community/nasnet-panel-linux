package telegram

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// HandleBackup creates a database backup and sends it as a document
func (h *Handler) HandleBackup(c telebot.Context) error {
	if h.backupSvc == nil {
		return c.Send("Backup is not configured (missing database config).")
	}

	// Acquire service-level lock (shared with HTTP handler)
	if !h.backupSvc.TryLock() {
		return c.Send("Another backup or restore operation is already in progress.")
	}
	defer h.backupSvc.Unlock()

	// Notify the user that backup is in progress
	c.Send("🔄 Creating database backup...")

	// Use the shared backup directory so Telegram backups appear in history.
	// Fall back to temp directory if no backup dir is configured.
	backupDir := h.backupSvc.BackupDir()
	keepFile := backupDir != ""
	if !keepFile {
		backupDir = os.TempDir()
	}
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return c.Send(fmt.Sprintf("Failed to create backup directory: %v", err))
	}

	now := time.Now()
	filename := fmt.Sprintf("backup_%s_%04d.sql", now.Format("20060102_150405"), now.Nanosecond()/100000)
	filePath := filepath.Join(backupDir, filename)

	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 5*time.Minute)
	defer cancel()

	if err := h.backupSvc.RunPgDump(ctx, filePath); err != nil {
		return c.Send(fmt.Sprintf("pg\\_dump failed: %v", err), telebot.ModeMarkdown)
	}
	// Only remove if using temp dir; otherwise keep in backup history
	if !keepFile {
		defer os.Remove(filePath)
	}

	// Get file info
	info, err := os.Stat(filePath)
	if err != nil {
		return c.Send("Backup created but failed to read file info.")
	}

	sizeMB := float64(info.Size()) / 1024 / 1024

	doc := &telebot.Document{
		File:     telebot.FromDisk(filePath),
		FileName: filename,
		Caption:  fmt.Sprintf("💾 Database Backup\nFile: %s\nSize: %.2f MB", filename, sizeMB),
	}

	return c.Send(doc)
}
