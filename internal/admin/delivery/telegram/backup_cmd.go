package telegram

import (
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// HandleBackup runs the shared backup pipeline (plain dump + rotation +
// gzip, age-encrypted when a backup passphrase is set) and delivers the file
// to the backup chat plus the requesting admin.
func (h *Handler) HandleBackup(c telebot.Context) error {
	if h.backupRunner == nil {
		return c.Send("Backup is not configured.")
	}

	c.Send("🔄 Creating database backup...")

	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 15*time.Minute)
	defer cancel()

	res, err := h.backupRunner.RunNow(ctx, c.Chat().ID)
	if err != nil {
		return c.Send(fmt.Sprintf("Backup failed: %v", err))
	}
	// The dump succeeded but never left the server (too large for Telegram, or
	// the bot is disabled) — say so, otherwise the last thing the admin sees is
	// "Creating..." and it reads as a hang.
	if res.Skipped != "" {
		return c.Send("⚠️ Backup created but " + res.Skipped)
	}
	return nil
}
