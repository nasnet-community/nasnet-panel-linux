package telegram

import (
	"context"
	"strings"

	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	"gopkg.in/telebot.v3"
)

type Handler struct {
	uc mntUC.Usecase
}

func NewHandler(uc mntUC.Usecase) *Handler {
	return &Handler{uc: uc}
}

// HandleMaintenance parses "/maintenance on [message]" or "/maintenance off".
// Intended to be wrapped in adminMiddleware.RequireAdmin at registration time.
// Bot command never triggers a user broadcast — use the web UI for that.
func (h *Handler) HandleMaintenance(c telebot.Context) error {
	args := c.Args()
	if len(args) == 0 {
		return c.Send("Usage: /maintenance on [message] | off")
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "on":
		msg := strings.TrimSpace(strings.Join(args[1:], " "))
		if err := h.uc.SetGlobal(context.Background(), true, msg, false); err != nil {
			return c.Send("Failed: " + err.Error())
		}
		return c.Send("✅ Maintenance ON. Users will see the notice on next write action.")
	case "off":
		if err := h.uc.SetGlobal(context.Background(), false, "", false); err != nil {
			return c.Send("Failed: " + err.Error())
		}
		return c.Send("✅ Maintenance OFF.")
	default:
		return c.Send("Usage: /maintenance on [message] | off")
	}
}
