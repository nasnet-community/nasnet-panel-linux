package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
)

// BackupSender is what the backup runner needs from the Telegram transport.
// *telegram.Bot implements it. A nil sender disables delivery and Telegram
// alerts (dump + rotation still run) so scheduling works with the bot off.
type BackupSender interface {
	SendDocumentToChat(chatID int64, filePath, fileName, caption string) error
	NotifyAdmin(message string) error
}

// ErrBackupBusy reports that the shared backup/restore lock was held by
// someone else (a panel backup or restore). It means no attempt was made,
// so the scheduler gives the interval back instead of waiting it out.
var ErrBackupBusy = errors.New("another backup or restore operation is already in progress")

const (
	// Telegram Bot API document upload limit.
	maxTelegramUploadBytes = 50 * 1024 * 1024
	backupRunTimeout       = 15 * time.Minute
)

// BackupResult describes one backup cycle so callers can report the outcome.
// Skipped is non-empty when the dump succeeded but nothing was delivered
// (too large for Telegram, or the bot is disabled).
type BackupResult struct {
	PlainPath string
	EncName   string
	SizeBytes int64
	Delivered []int64
	Skipped   string
}

// ScheduledBackupRunner dumps the database on a panel-configured interval,
// rotates plain copies in the backup directory, and ships a gzip+age
// encrypted copy to a Telegram chat. All knobs live in the settings table
// (category "backup"); the last-run timestamp is persisted there too, so
// intervals missed while the process was down are caught up after restart.
type ScheduledBackupRunner struct {
	backupSvc        *BackupService
	settings         settingDomain.SettingRepository
	fallbackAdminIDs []int64

	// sender is swappable: the Telegram bot is built against
	// api.telegram.org and can appear long after boot (once the outbound
	// path is up), so delivery reads it per run instead of at construction.
	senderMu sync.RWMutex
	sender   BackupSender

	// running keeps the detached scheduled backups single-flight.
	running atomic.Bool

	// lastAttempt backs the due-check in memory as well as in the settings
	// table, so a failed timestamp write cannot turn into a dump per tick.
	mu          sync.Mutex
	lastAttempt time.Time
}

func NewScheduledBackupRunner(
	backupSvc *BackupService,
	settings settingDomain.SettingRepository,
	sender BackupSender,
	fallbackAdminIDs []int64,
) *ScheduledBackupRunner {
	return &ScheduledBackupRunner{
		backupSvc:        backupSvc,
		settings:         settings,
		sender:           sender,
		fallbackAdminIDs: fallbackAdminIDs,
	}
}

// SetSender wires (or clears) Telegram delivery after construction.
func (r *ScheduledBackupRunner) SetSender(sender BackupSender) {
	r.senderMu.Lock()
	r.sender = sender
	r.senderMu.Unlock()
}

// telegramSender returns the current sender, or nil when delivery is off.
func (r *ScheduledBackupRunner) telegramSender() BackupSender {
	r.senderMu.RLock()
	defer r.senderMu.RUnlock()
	return r.sender
}

// MaybeRun starts a scheduled backup when enabled and due, then returns
// immediately: the cycle runs on its own goroutine because a large dump plus
// upload takes minutes, and the reconcile group it is called from also owns
// cert renewals and retention cleanups.
func (r *ScheduledBackupRunner) MaybeRun(ctx context.Context) {
	if r.settingValue(ctx, "backup_enabled") != "true" {
		return
	}
	intervalHours, err := strconv.Atoi(r.settingValue(ctx, "backup_interval_hours"))
	if err != nil || intervalHours < 1 {
		intervalHours = 24
	}
	interval := time.Duration(intervalHours) * time.Hour

	r.mu.Lock()
	tooSoon := !r.lastAttempt.IsZero() && time.Since(r.lastAttempt) < interval
	r.mu.Unlock()
	if tooSoon {
		return
	}

	prevRaw := r.settingValue(ctx, "backup_last_run_at")
	if prevRaw != "" {
		if last, parseErr := time.Parse(time.RFC3339, prevRaw); parseErr == nil &&
			time.Since(last) < interval {
			return
		}
	}

	if !r.running.CompareAndSwap(false, true) {
		return
	}

	// Record the attempt before running — a persistently failing backup then
	// alerts once per interval instead of once per reconcile tick. If the
	// write fails (disk full, read-only FS) the stored timestamp never
	// advances, so fall back to the in-memory guard; otherwise the due-check
	// stays purely settings-driven and clearing the timestamp forces a run.
	now := time.Now()
	if err := r.persistLastRun(ctx, now); err != nil {
		r.mu.Lock()
		r.lastAttempt = now
		r.mu.Unlock()
	}

	go func() {
		defer r.running.Store(false)
		log := logger.GetLogger()

		runCtx, cancel := context.WithTimeout(ctx, backupRunTimeout)
		defer cancel()

		res, err := r.RunNow(runCtx)
		if !errors.Is(err, ErrBackupBusy) {
			r.writeLastResult(context.WithoutCancel(ctx), now, res, err)
		}
		switch {
		case errors.Is(err, ErrBackupBusy):
			// Not a real attempt: hand the interval back so the next tick retries.
			r.rollbackLastRun(ctx, prevRaw)
			log.Warn("Scheduled backup skipped: a backup or restore was already running")
		case err != nil:
			log.WithError(err).Error("Scheduled backup failed")
			r.alert(fmt.Sprintf("⚠️ Scheduled backup failed: %v\nNext attempt in %dh.", err, intervalHours))
		case res.Skipped != "":
			log.WithField("reason", res.Skipped).Warn("Scheduled backup created but not delivered")
		default:
			log.WithFields(map[string]interface{}{
				"file":      res.EncName,
				"size":      res.SizeBytes,
				"delivered": len(res.Delivered),
			}).Info("Scheduled backup delivered")
		}
	}()
}

// RunNow performs one full backup cycle: plain dump into the backup dir,
// local rotation, gzip+age encryption, Telegram delivery to the backup chat
// plus extraChats (e.g. the admin who invoked /backup). The plain dump stays
// on disk either way; only the packed copy leaves the server. Without a
// backup passphrase the packed copy is a plain gzip, sent with a warning.
func (r *ScheduledBackupRunner) RunNow(ctx context.Context, extraChats ...int64) (BackupResult, error) {
	log := logger.GetLogger()
	var res BackupResult

	passphrase := r.settingValue(ctx, "backup_passphrase")
	if passphrase == "" {
		log.Warn("Backup passphrase is not set (Settings → Backup); this backup is unencrypted")
	}

	// Resolve the destination before dumping: a misconfigured or empty
	// recipient set must fail loudly rather than produce a backup that
	// silently goes nowhere.
	recipients, err := r.recipients(ctx, extraChats)
	if err != nil {
		return res, err
	}
	sender := r.telegramSender()
	if sender != nil && len(recipients) == 0 {
		return res, fmt.Errorf("no backup recipients configured: set a Backup Chat ID (Settings → Backup) or ADMIN_IDS")
	}

	plainPath, encPath, size, err := r.dumpAndPack(ctx, passphrase)
	if err != nil {
		return res, err
	}
	defer os.Remove(encPath)

	res.PlainPath = plainPath
	res.EncName = filepath.Base(encPath)
	res.SizeBytes = size
	sizeMB := float64(size) / 1024 / 1024

	if size > maxTelegramUploadBytes {
		res.Skipped = fmt.Sprintf("%s is %.2f MB, over Telegram's 50 MB upload limit, so it was NOT sent. The plain dump is kept at %s.",
			res.EncName, sizeMB, plainPath)
		r.alert("⚠️ Backup " + res.Skipped)
		return res, nil
	}
	if sender == nil {
		res.Skipped = "the Telegram bot is disabled, so nothing was sent."
		log.Warn("Backup created but the Telegram bot is disabled; nothing sent")
		return res, nil
	}

	caption := fmt.Sprintf("💾 Database backup\nFile: %s\nSize: %.2f MB\nDecrypt: age -d %s | gunzip > %s",
		res.EncName, sizeMB, res.EncName, filepath.Base(plainPath))
	if passphrase == "" {
		caption = fmt.Sprintf("⚠️ Unencrypted — set a backup passphrase in Settings → Backup.\n💾 Database backup\nFile: %s\nSize: %.2f MB\nDecompress: gunzip -c %s > %s",
			res.EncName, sizeMB, res.EncName, filepath.Base(plainPath))
	}

	var failed []int64
	for _, chatID := range recipients {
		if err := r.sendWithRetry(ctx, chatID, encPath, res.EncName, caption); err != nil {
			log.WithError(err).WithField("chat_id", chatID).Error("Failed to deliver backup")
			failed = append(failed, chatID)
			continue
		}
		res.Delivered = append(res.Delivered, chatID)
	}
	if len(failed) > 0 {
		r.alert(fmt.Sprintf("⚠️ Backup %s was created but delivery failed for chat(s) %v. The plain dump is kept at %s.",
			res.EncName, failed, plainPath))
		return res, fmt.Errorf("delivery failed for %d chat(s)", len(failed))
	}
	return res, nil
}

// dumpAndPack holds the shared backup/restore lock for the dump, rotation
// and packing (gzip+age, or plain gzip without a passphrase) only. Delivery runs outside it: uploads plus retry backoff
// would otherwise make the panel's backup endpoints answer 409 for minutes.
// The encrypted copy is written next to the dump rather than into TempDir,
// which on many hosts is a small tmpfs (so, RAM).
func (r *ScheduledBackupRunner) dumpAndPack(ctx context.Context, passphrase string) (string, string, int64, error) {
	if !r.backupSvc.TryLock() {
		return "", "", 0, ErrBackupBusy
	}
	defer r.backupSvc.Unlock()

	backupDir := r.backupSvc.BackupDir()
	if backupDir == "" {
		backupDir = os.TempDir()
	}
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return "", "", 0, fmt.Errorf("create backup directory: %w", err)
	}

	now := time.Now()
	plainName := fmt.Sprintf("backup_%s_%04d.sql", now.Format("20060102_150405"), now.Nanosecond()/100000)
	plainPath := filepath.Join(backupDir, plainName)

	if err := r.backupSvc.RunPgDump(ctx, plainPath); err != nil {
		// pg_dump creates its output file before it can fail, and a truncated
		// dump matches the panel's restorable-backup pattern. Drop it.
		if rmErr := os.Remove(plainPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logger.GetLogger().WithError(rmErr).WithField("file", plainName).
				Warn("Failed to remove partial backup after a failed dump")
		}
		return "", "", 0, fmt.Errorf("database dump: %w", err)
	}

	if keep, convErr := strconv.Atoi(r.settingValue(ctx, "backup_retention_count")); convErr == nil {
		r.rotatePlainBackups(backupDir, keep)
	}

	var encPath string
	if passphrase == "" {
		encPath = filepath.Join(backupDir, plainName+".gz")
		if err := GzipBackupFile(plainPath, encPath); err != nil {
			os.Remove(encPath)
			return "", "", 0, fmt.Errorf("compress backup: %w", err)
		}
	} else {
		encPath = filepath.Join(backupDir, plainName+".gz.age")
		if err := EncryptBackupFile(plainPath, encPath, passphrase); err != nil {
			return "", "", 0, fmt.Errorf("encrypt backup: %w", err)
		}
	}

	info, err := os.Stat(encPath)
	if err != nil {
		os.Remove(encPath)
		return "", "", 0, fmt.Errorf("stat encrypted backup: %w", err)
	}
	return plainPath, encPath, info.Size(), nil
}

// recipients resolves the configured backup chat and merges extraChats,
// deduplicated, order-preserving. An unparseable chat ID is an error rather
// than a fallback: the operator pointed backups at a specific chat, and
// quietly sending the database somewhere else is worse than not sending it.
func (r *ScheduledBackupRunner) recipients(ctx context.Context, extraChats []int64) ([]int64, error) {
	var ids []int64
	if raw := strings.TrimSpace(r.settingValue(ctx, "backup_chat_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("backup chat ID %q is not a valid Telegram chat ID", raw)
		}
		ids = append(ids, id)
	} else {
		ids = append(ids, r.fallbackAdminIDs...)
	}

	seen := make(map[int64]bool, len(ids)+len(extraChats))
	var out []int64
	for _, id := range append(ids, extraChats...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// sendWithRetry attempts delivery up to 3 times (immediate, +10s, +30s).
func (r *ScheduledBackupRunner) sendWithRetry(ctx context.Context, chatID int64, path, name, caption string) error {
	backoffs := []time.Duration{0, 10 * time.Second, 30 * time.Second}
	var lastErr error
	for _, wait := range backoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		sender := r.telegramSender()
		if sender == nil {
			return errors.New("the Telegram bot is not available")
		}
		if lastErr = sender.SendDocumentToChat(chatID, path, name, caption); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// rotatePlainBackups deletes the oldest backup_*.sql files beyond keep.
// The timestamped names sort chronologically, so lexicographic order works.
// Runs after the new dump is written, so the newest file counts toward keep.
// Manual panel backups share this naming and are rotated along with the rest.
func (r *ScheduledBackupRunner) rotatePlainBackups(dir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "backup_") && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	log := logger.GetLogger()
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			log.WithError(err).WithField("file", name).Warn("Failed to rotate old backup")
		} else {
			log.WithField("file", name).Info("Rotated old backup")
		}
	}
}

func (r *ScheduledBackupRunner) settingValue(ctx context.Context, key string) string {
	s, err := r.settings.GetByKey(ctx, key)
	if err != nil || s == nil {
		return ""
	}
	return s.Value
}

func (r *ScheduledBackupRunner) persistLastRun(ctx context.Context, t time.Time) error {
	return r.writeLastRun(ctx, t.UTC().Format(time.RFC3339))
}

// rollbackLastRun restores the previous timestamp after an attempt that never
// happened, and clears the in-memory guard so the next tick retries.
func (r *ScheduledBackupRunner) rollbackLastRun(ctx context.Context, prevRaw string) {
	rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = r.writeLastRun(rbCtx, prevRaw)
	r.mu.Lock()
	r.lastAttempt = time.Time{}
	r.mu.Unlock()
}

func (r *ScheduledBackupRunner) writeLastRun(ctx context.Context, value string) error {
	s, err := r.settings.GetByKey(ctx, "backup_last_run_at")
	if err != nil || s == nil {
		s = &settingDomain.Setting{
			Key:         "backup_last_run_at",
			Type:        "string",
			Category:    "backup",
			Description: "RFC3339 timestamp of the last scheduled backup attempt. Set automatically; not user-editable.",
			Label:       "Last Scheduled Backup",
		}
	}
	s.Value = value
	if err := r.settings.Update(ctx, s); err != nil {
		logger.GetLogger().WithError(err).Warn("Failed to persist backup last-run timestamp")
		return err
	}
	return nil
}

// writeLastResult keeps the outcome of the last scheduled run for the
// settings page. It lives in the admin category: not editable, not exported.
func (r *ScheduledBackupRunner) writeLastResult(ctx context.Context, at time.Time, res BackupResult, runErr error) {
	out := struct {
		At        time.Time `json:"at"`
		OK        bool      `json:"ok"`
		Sent      bool      `json:"sent"`
		SizeBytes int64     `json:"size_bytes,omitempty"`
		Reason    string    `json:"reason,omitempty"`
	}{At: at.UTC(), OK: runErr == nil && res.Skipped == "", Sent: len(res.Delivered) > 0, SizeBytes: res.SizeBytes}
	switch {
	case runErr != nil:
		out.Reason = runErr.Error()
	case res.SizeBytes > maxTelegramUploadBytes:
		out.Reason = "too_big"
	case res.Skipped != "":
		out.Reason = "bot_off"
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return
	}
	s, err := r.settings.GetByKey(ctx, "backup_last_result")
	if err != nil || s == nil {
		s = &settingDomain.Setting{
			Key:         "backup_last_result",
			Type:        "json",
			Category:    "admin",
			Description: "Outcome of the last scheduled backup. Set automatically.",
			Label:       "Last Scheduled Backup Result",
		}
	}
	s.Value = string(raw)
	if err := r.settings.Update(ctx, s); err != nil {
		logger.GetLogger().WithError(err).Warn("Failed to persist backup result")
	}
}

// alert notifies the admins. NotifyAdmin sends with Markdown parse mode and
// every one of these messages carries a backup filename full of underscores,
// which Telegram rejects as an unbalanced italic entity — so escape first.
func (r *ScheduledBackupRunner) alert(message string) {
	sender := r.telegramSender()
	if sender == nil {
		logger.GetLogger().Warn("Backup alert (bot disabled): " + message)
		return
	}
	if err := sender.NotifyAdmin(utils.EscapeMarkdown(message)); err != nil {
		logger.GetLogger().WithError(err).Error("Failed to send backup alert to admins")
	}
}
