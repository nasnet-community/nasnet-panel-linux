package telegram

import (
	"fmt"
	"strconv"

	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

type Handler struct {
	accountUC usecase.AccountUsecase
}

func NewHandler(accountUC usecase.AccountUsecase) *Handler {
	return &Handler{accountUC: accountUC}
}

// HandleAccountList shows all accounts (paginated)
func (h *Handler) HandleAccountList(c telebot.Context) error {
	return h.HandleAccountListPage(c, 0)
}

// HandleAccountListPage shows accounts with pagination
func (h *Handler) HandleAccountListPage(c telebot.Context, page int) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	const pageSize = 8 // Items per page

	// Get total count
	total, err := h.accountUC.CountAccounts(ctx, repository.AccountFilter{})
	if err != nil {
		return c.Send("❌ Error: " + err.Error())
	}

	if total == 0 {
		kb := &telebot.ReplyMarkup{}
		kb.Inline(keyboards.BackRow(kb, "back_admin"))
		return c.Send("📋 *No accounts found.*", telebot.ModeMarkdown, kb)
	}

	// Calculate pagination
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	offset := page * pageSize
	filter := repository.AccountFilter{
		Offset: offset,
		Limit:  pageSize,
	}
	accounts, err := h.accountUC.ListAllAccounts(ctx, filter)
	if err != nil {
		return c.Send("❌ Error: " + err.Error())
	}

	// Build message
	msg := fmt.Sprintf("📋 *Xray Accounts* (%d total)\n", total)
	msg += fmt.Sprintf("📄 Page %d of %d\n\n", page+1, totalPages)
	msg += "Select an account to view details:"

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, acc := range accounts {
		status := "🟢"
		if acc.Status != domain.AccountStatusActive {
			status = "🔴"
		}

		nodeName := "Unknown"
		if acc.Inbound != nil && acc.Inbound.Node != nil {
			nodeName = acc.Inbound.Node.Name
		}

		// Truncate email if too long
		email := acc.Email
		if len(email) > 20 {
			email = email[:17] + "..."
		}

		label := fmt.Sprintf("%s %s (%s)", status, email, nodeName)
		rows = append(rows, kb.Row(kb.Data(label, "account_view", fmt.Sprintf("%d", acc.ID))))
	}

	// Pagination controls
	var navRow telebot.Row
	if totalPages > 1 {
		if page > 0 {
			navRow = append(navRow, kb.Data("◀️ Prev", "account_page", fmt.Sprintf("%d", page-1)))
		}
		navRow = append(navRow, kb.Data(fmt.Sprintf("📄 %d/%d", page+1, totalPages), "noop"))
		if page < totalPages-1 {
			navRow = append(navRow, kb.Data("Next ▶️", "account_page", fmt.Sprintf("%d", page+1)))
		}
		rows = append(rows, navRow)
	}

	rows = append(rows, keyboards.BackRow(kb, "back_admin"))
	kb.Inline(rows...)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// HandleAccountPage handles pagination navigation
func (h *Handler) HandleAccountPage(c telebot.Context) error {
	page, _ := strconv.Atoi(c.Data())
	return h.HandleAccountListPage(c, page)
}

// HandleAccountView shows details for a specific account
func (h *Handler) HandleAccountView(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id, err := strconv.ParseUint(c.Data(), 10, 32)
	if err != nil {
		return c.Edit("❌ Invalid account ID")
	}

	acc, err := h.accountUC.GetAccount(ctx, uint(id))
	if err != nil {
		return c.Edit("❌ Account not found")
	}

	status := "🟢 Active"
	if acc.Status == domain.AccountStatusDisabled {
		status = "🔴 Disabled"
	} else if acc.Status == domain.AccountStatusExpired {
		status = "⚪ Expired"
	}

	nodeName := "Unknown"
	inboundTag := "Unknown"
	if acc.Inbound != nil {
		inboundTag = acc.Inbound.Tag
		if acc.Inbound.Node != nil {
			nodeName = acc.Inbound.Node.Name
		}
	}

	source := string(acc.Source)
	subInfo := "-"
	if acc.SubscriptionID != nil {
		subInfo = fmt.Sprintf("#%d", *acc.SubscriptionID)
	}

	msg := fmt.Sprintf("👤 *Account Details*\n━━━━━━━━━━━━━━━━\n\n"+
		"📧 *Email:* `%s`\n"+
		"🆔 *UUID:* `%s`\n"+
		"📊 *Status:* %s\n\n"+
		"🖥 *Node:* %s\n"+
		"📡 *Inbound:* `%s`\n\n"+
		"📌 *Source:* %s\n"+
		"🔗 *Subscription:* %s\n\n"+
		"📈 *Data Used:* %s",
		acc.Email, acc.UUID, status,
		nodeName, inboundTag,
		source, subInfo,
		formatBytes(acc.DataUsed))

	kb := &telebot.ReplyMarkup{}

	var actionRow telebot.Row
	if acc.Status == domain.AccountStatusActive {
		actionRow = kb.Row(kb.Data("⏸ Disable", "account_disable", fmt.Sprintf("%d", acc.ID)))
	} else {
		actionRow = kb.Row(kb.Data("▶️ Enable", "account_enable", fmt.Sprintf("%d", acc.ID)))
	}

	kb.Inline(
		kb.Row(kb.Data("🔗 Generate Link", "account_link", fmt.Sprintf("%d", acc.ID))),
		actionRow,
		kb.Row(kb.Data("🗑 Delete", "account_delete_ask", fmt.Sprintf("%d", acc.ID))),
		keyboards.BackRow(kb, "account_list"),
	)

	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

// HandleAccountLink generates and displays the config link
func (h *Handler) HandleAccountLink(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	id := utils.CallbackID(c)
	link, err := h.accountUC.GenerateAccountLink(ctx, id)
	if err != nil {
		return c.Edit("❌ Error generating link: " + err.Error())
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(keyboards.BackRow(kb, "account_view", c.Data()))

	return c.Edit(fmt.Sprintf("🔗 *Config Link:*\n\n`%s`", link), telebot.ModeMarkdown, kb)
}

// HandleAccountDisable disables an account
func (h *Handler) HandleAccountDisable(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id := utils.CallbackID(c)

	if err := h.accountUC.DisableAccount(ctx, id); err != nil {
		return utils.AnswerCallbackWithAlert(c, "Failed to disable: "+err.Error())
	}

	utils.AnswerCallback(c, "Account disabled")
	return h.HandleAccountView(c)
}

// HandleAccountEnable enables an account
func (h *Handler) HandleAccountEnable(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id := utils.CallbackID(c)

	if err := h.accountUC.EnableAccount(ctx, id); err != nil {
		return utils.AnswerCallbackWithAlert(c, "Failed to enable: "+err.Error())
	}

	utils.AnswerCallback(c, "Account enabled")
	return h.HandleAccountView(c)
}

// HandleAccountDeleteAsk shows confirmation
func (h *Handler) HandleAccountDeleteAsk(c telebot.Context) error {
	utils.AnswerCallback(c)
	id := c.Data()

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("✅ Yes, Delete", "account_delete", id)),
		kb.Row(kb.Data("❌ Cancel", "account_view", id)),
	)

	return c.Edit("⚠️ *Are you sure you want to delete this account?*\n\nThis will remove the user from Xray and cannot be undone.", telebot.ModeMarkdown, kb)
}

// HandleAccountDelete deletes an account
func (h *Handler) HandleAccountDelete(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	id := utils.CallbackID(c)

	if err := h.accountUC.DeleteAccount(ctx, id); err != nil {
		return utils.AnswerCallbackWithAlert(c, "Failed to delete: "+err.Error())
	}

	utils.AnswerCallback(c, "Account deleted")
	return h.HandleAccountList(c)
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// RegisterHandlers registers account-related handlers on bot
func RegisterHandlers(bot *telebot.Bot, h *Handler, adminMW func(telebot.HandlerFunc) telebot.HandlerFunc) {
	// Text button
	bot.Handle("📋 Accounts", adminMW(h.HandleAccountList))

	// Inline callbacks
	bot.Handle(&telebot.InlineButton{Unique: "account_list"}, adminMW(h.HandleAccountList))
	bot.Handle(&telebot.InlineButton{Unique: "account_page"}, adminMW(h.HandleAccountPage))
	bot.Handle(&telebot.InlineButton{Unique: "account_view"}, adminMW(h.HandleAccountView))
	bot.Handle(&telebot.InlineButton{Unique: "account_link"}, adminMW(h.HandleAccountLink))
	bot.Handle(&telebot.InlineButton{Unique: "account_disable"}, adminMW(h.HandleAccountDisable))
	bot.Handle(&telebot.InlineButton{Unique: "account_enable"}, adminMW(h.HandleAccountEnable))
	bot.Handle(&telebot.InlineButton{Unique: "account_delete_ask"}, adminMW(h.HandleAccountDeleteAsk))
	bot.Handle(&telebot.InlineButton{Unique: "account_delete"}, adminMW(h.HandleAccountDelete))

	// NOOP handler for page info button
	bot.Handle(&telebot.InlineButton{Unique: "noop"}, func(c telebot.Context) error {
		return c.Respond()
	})
}
