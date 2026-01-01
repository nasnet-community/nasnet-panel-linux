package telegram

import (
	"fmt"
	"strings"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// HandleAudit shows the last 10 audit log entries
func (h *Handler) HandleAudit(c telebot.Context) error {
	if h.auditUC == nil {
		return c.Send("Audit logging is not configured.")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	entries, _, err := h.auditUC.List(ctx, auditDomain.AuditListFilters{
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to fetch audit logs: %v", err))
	}

	if len(entries) == 0 {
		return c.Send("📋 *Audit Log*\n\n_No entries found._", telebot.ModeMarkdown)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *Audit Log* (last %d)\n\n", len(entries)))

	for _, e := range entries {
		ts := e.CreatedAt.Format("Jan 02 15:04")
		actor := e.ActorName
		if actor == "" {
			actor = fmt.Sprintf("ID:%d", e.ActorID)
		}
		action := e.Action

		line := fmt.Sprintf("`%s` | *%s* | %s", ts, actor, action)

		// Add entity info if present
		if e.EntityType != "" {
			line += fmt.Sprintf(" (%s #%d)", e.EntityType, e.EntityID)
		}

		sb.WriteString(line + "\n")
	}

	return c.Send(sb.String(), telebot.ModeMarkdown)
}
