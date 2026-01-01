package telegram

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	accountUC "github.com/nasnet-community/nasnet-panel-linux/internal/account/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/admin/usecase"
	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	provisioningRepo "github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/repository"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	subUC "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userRepo "github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
)

// Handler handles admin Telegram commands
type Handler struct {
	adminUC      usecase.AdminUsecase
	accountUC    accountUC.AccountUsecase
	subUC        subUC.SubscriptionUsecase
	userRepo     userRepo.UserRepository
	stateManager *conversation.StateManager
	bot          *telebot.Bot
	// Phase 5 dependencies
	auditUC   auditDomain.AuditLogUsecase
	settingUC settingDomain.SettingUsecase
	nodeUC    nodeUC.NodeUsecase
	provRepo  provisioningRepo.ProvisioningRepository
	backupSvc *usecase.BackupService
	db        *gorm.DB
}

// NewHandler creates a new admin handler
func NewHandler(
	adminUC usecase.AdminUsecase,
	accountUC accountUC.AccountUsecase,
	subUC subUC.SubscriptionUsecase,
	userRepo userRepo.UserRepository,
	stateManager *conversation.StateManager,
	bot *telebot.Bot,
	auditUC auditDomain.AuditLogUsecase,
	settingUC settingDomain.SettingUsecase,
	nodeUC nodeUC.NodeUsecase,
	provRepo provisioningRepo.ProvisioningRepository,
	backupSvc *usecase.BackupService,
	db *gorm.DB,
) *Handler {
	return &Handler{
		adminUC:      adminUC,
		accountUC:    accountUC,
		subUC:        subUC,
		userRepo:     userRepo,
		stateManager: stateManager,
		bot:          bot,
		auditUC:      auditUC,
		settingUC:    settingUC,
		nodeUC:       nodeUC,
		provRepo:     provRepo,
		backupSvc:    backupSvc,
		db:           db,
	}
}

// SetBot sets the bot instance (for circular dependency resolution)
func (h *Handler) SetBot(bot *telebot.Bot) {
	h.bot = bot
}

// === DASHBOARD ===

func (h *Handler) HandleAdmin(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	stats, _ := h.adminUC.GetDashboardStats(ctx)

	msg := fmt.Sprintf(`🔐 *Admin Panel*

👥 *Users:* %d (Active: %d)
📡 *Online Now:* %d
📋 *Subs:* %d`,
		stats.TotalUsers, stats.ActiveUsers,
		stats.OnlineUsers,
		stats.TotalSubscriptions)

	return c.Send(msg, telebot.ModeMarkdown, keyboards.AdminMenu())
}

func (h *Handler) HandleAdminHelp(c telebot.Context) error {
	utils.AnswerCallback(c)
	helpMsg := `📚 *Admin Quick Guide*

*Dashboard:*
• /admin - Open Admin Panel
• /stats - Detailed statistics

*Servers:*
• /nodes - Manage Xray servers

*Users:*
• /users - List all users
• /getuser ID - View specific user
• /ban ID - Ban user
• /unban ID - Unban user

*Subscriptions:*
• /subs - List all subscriptions
• /getsub ID - View subscription
• /assign\_sub SUB TG - Assign to user

*System:*
• /xray\_sync - Sync all nodes

*Manual:*
• /adduser - Add Xray user
• /getlink - Get config link`

	return c.Send(helpMsg, telebot.ModeMarkdown, keyboards.AdminMenu())
}

func (h *Handler) HandleStats(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	stats, _ := h.adminUC.GetDashboardStats(ctx)

	msg := fmt.Sprintf(`📊 *Detailed Statistics*

👥 *Users*
• Total: %d
• Active: %d
• Online: %d
• Banned: %d

📋 *Subscriptions*
• Total: %d
• Active: %d
• Expired: %d`,
		stats.TotalUsers, stats.ActiveUsers, stats.OnlineUsers, stats.BannedUsers,
		stats.TotalSubscriptions, stats.ActiveSubscriptions, stats.ExpiredSubscriptions)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("👤 View Online Users", "admin_online_users")),
	)

	return c.Send(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleXrayStatus(c telebot.Context) error {
	return c.Send("⚠️ Use /nodes or '🖥 Servers' to check status of specific nodes.")
}

func (h *Handler) HandleOnlineUsers(c telebot.Context) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	users, err := h.adminUC.GetOnlineUsers(ctx)
	if err != nil {
		return c.Send("❌ Error fetching online users: "+err.Error(), telebot.ModeMarkdown)
	}

	if len(users) == 0 {
		return c.Send("👤 *Online Users*\n\n_No users currently online._", telebot.ModeMarkdown)
	}

	msg := fmt.Sprintf("👤 *Online Users* (%d total)\n\n", len(users))

	// Show up to 50 users to avoid Telegram message limits
	displayLimit := 50
	if len(users) > displayLimit {
		for i := 0; i < displayLimit; i++ {
			msg += fmt.Sprintf("• `%s`\n", users[i])
		}
		msg += fmt.Sprintf("\n_...and %d more_", len(users)-displayLimit)
	} else {
		for _, email := range users {
			msg += fmt.Sprintf("• `%s`\n", email)
		}
	}

	return c.Send(msg, telebot.ModeMarkdown)
}

// HandleXraySync forces a reconciliation across ALL nodes
func (h *Handler) HandleXraySync(c telebot.Context) error {
	// Not a callback usually, but if called via button:
	utils.AnswerCallback(c, "Processing...")

	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	c.Send("🔄 Starting synchronization across all nodes...")

	stats, err := h.subUC.ReconcileUsers(ctx)
	if err != nil {
		return c.Send("❌ Sync Failed: " + err.Error())
	}

	msg := fmt.Sprintf("✅ *Sync Completed*\n\n"+
		"📋 *Total Active (DB):* %d\n"+
		"⚡ *Total Users (Xray):* %d\n"+
		"➕ *Missing Restored:* %d\n"+
		"🗑 *Ghosts Removed:* %d\n"+
		"⚠️ *Errors:* %d",
		stats.TotalDBUsers,
		stats.TotalXrayUsers,
		stats.MissingAdded,
		stats.GhostsRemoved,
		stats.Errors)

	return c.Send(msg, telebot.ModeMarkdown)
}

func (h *Handler) HandleXrayInspect(c telebot.Context) error {
	return c.Send("⚠️ Use /nodes -> Inbounds to inspect details.")
}

// HandlePlanUsers lists users on a plan
func (h *Handler) HandleUsers(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	search := ""
	page := 1
	limit := 10

	if c.Callback() != nil {
		page, _ = strconv.Atoi(c.Data())
	} else if len(c.Args()) > 0 {
		search = strings.Join(c.Args(), " ")
	}

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	users, total, err := h.adminUC.ListUsers(ctx, search, "", "", "", offset, limit)
	if err != nil {
		return c.Send("❌ Error fetching users")
	}

	if len(users) == 0 && page == 1 {
		return c.Send("📭 No users found")
	}

	msg := fmt.Sprintf("👥 *Users* (Page %d, Total %d)\n\n", page, total)
	for _, user := range users {
		username := "@N/A"
		if user.Username != "" {
			username = "@" + user.Username
		}
		fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
		if fullName == "" {
			fullName = "No Name"
		}

		statusEmoji := "✅"
		if user.IsBanned {
			statusEmoji = "🚫"
		}

		msg += fmt.Sprintf("%s `%d`: %s (%s)\n", statusEmoji, user.TelegramID, utils.EscapeMarkdown(username), utils.EscapeMarkdown(fullName))
	}

	msg += "\nUse: `/getuser <chatid>` to manage."

	menu := &telebot.ReplyMarkup{}
	var navRow []telebot.Btn
	if page > 1 {
		navRow = append(navRow, menu.Data("⬅️ Prev", "admin_users_page", fmt.Sprintf("%d", page-1)))
	}
	if int64(offset+limit) < total {
		navRow = append(navRow, menu.Data("➡️ Next", "admin_users_page", fmt.Sprintf("%d", page+1)))
	}
	if len(navRow) > 0 {
		menu.Inline(menu.Row(navRow...))
	}

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, menu)
}

func (h *Handler) HandleGetUser(c telebot.Context) error {
	utils.AnswerCallback(c)
	var dbID uint
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if c.Callback() != nil {
		id, err := strconv.ParseUint(c.Data(), 10, 32)
		if err != nil {
			return utils.AnswerCallback(c, "Invalid data")
		}
		dbID = uint(id)
	} else {
		if len(c.Args()) < 1 {
			return c.Send("Usage: `/getuser <chat_id|username>`", telebot.ModeMarkdown)
		}

		input := c.Args()[0]
		var found bool

		// Try parsing as Chat ID first
		if chatID, err := strconv.ParseInt(input, 10, 64); err == nil {
			if user, err := h.userRepo.FindByTelegramID(ctx, chatID); err == nil {
				dbID = user.ID
				found = true
			}
		}

		// Try as Username
		if !found {
			username := strings.TrimPrefix(input, "@")
			if user, err := h.userRepo.FindByUsername(ctx, username); err == nil {
				dbID = user.ID
				found = true
			}
		}

		if !found {
			return c.Send("❌ User not found.")
		}
	}

	details, err := h.adminUC.GetUserDetails(ctx, dbID)
	if err != nil {
		return c.Send("❌ User details not found")
	}

	statusText := "Active"
	banBtnText := "🚫 Ban User"
	banUnique := "admin_user_ban"

	if details.IsBanned {
		statusText = "🚫 Banned"
		banBtnText = "✅ Unban User"
		banUnique = "admin_user_unban"
	}

	adminText := ""
	if details.IsAdmin {
		adminText = "👑 Admin"
	}

	msg := fmt.Sprintf(`👤 *User Details* (TG: `+"`%d`"+`)

👤 Username: @%s
📛 Name: %s %s
🚦 Status: %s %s

📊 *Stats:*
• Subs: %d (Active: %d)
• Joined: %s`,
		details.TelegramID,
		details.Username,
		details.FirstName, details.LastName,
		statusText, adminText,
		details.TotalSubscriptions, details.ActiveSubscriptions,
		details.CreatedAt)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("📋 Subscriptions", "admin_user_subs", fmt.Sprintf("%d", details.ID)),
		),
		kb.Row(
			kb.Data(banBtnText, banUnique, fmt.Sprintf("%d", details.ID)),
		),
		kb.Row(
			kb.Data("🔙 User List", "admin_users_page:1"),
		),
	)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleUserBanToggle(c telebot.Context) error {
	id := utils.CallbackID(c)
	isBan := c.Callback().Unique == "admin_user_ban"
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	var err error
	if isBan {
		err = h.adminUC.BanUser(ctx, id)
	} else {
		err = h.adminUC.UnbanUser(ctx, id)
	}

	if err != nil {
		return utils.AnswerCallback(c, "Error processing ban")
	}

	// Ack done via GetUser or logic above
	return h.HandleGetUser(c)
}

func (h *Handler) HandleSetDataWizard(c telebot.Context, targetSubID uint, gbAmount float64) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	bytes := int64(gbAmount * 1024 * 1024 * 1024)
	if err := h.adminUC.SetDataUsage(ctx, targetSubID, bytes); err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}
	return c.Send(fmt.Sprintf("✅ Set Data Usage to %.2f GB for Sub #%d", gbAmount, targetSubID), keyboards.AdminMenu())
}

func (h *Handler) HandleAdminExtendCustomValue(c telebot.Context, targetSubID uint, days int) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.ExtendSubscription(ctx, targetSubID, days); err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}
	return c.Send(fmt.Sprintf("✅ Extended Sub #%d by %d days", targetSubID, days), keyboards.AdminMenu())
}

// HandleBan bans a user via command
// Usage: /ban <user_id>
func (h *Handler) HandleBan(c telebot.Context) error {
	if len(c.Args()) < 1 {
		return c.Send("Usage: `/ban <user_id>`", telebot.ModeMarkdown)
	}
	userID, err := strconv.ParseUint(c.Args()[0], 10, 32)
	if err != nil {
		return c.Send("❌ Invalid user ID")
	}
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.BanUser(ctx, uint(userID)); err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}
	return c.Send(fmt.Sprintf("🚫 User #%d has been banned", userID))
}

// HandleUnban unbans a user via command
// Usage: /unban <user_id>
func (h *Handler) HandleUnban(c telebot.Context) error {
	if len(c.Args()) < 1 {
		return c.Send("Usage: `/unban <user_id>`", telebot.ModeMarkdown)
	}
	userID, err := strconv.ParseUint(c.Args()[0], 10, 32)
	if err != nil {
		return c.Send("❌ Invalid user ID")
	}
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.UnbanUser(ctx, uint(userID)); err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}
	return c.Send(fmt.Sprintf("✅ User #%d has been unbanned", userID))
}

func (h *Handler) HandleMakeAdmin(c telebot.Context) error {
	if len(c.Args()) < 1 {
		return c.Send("Usage: /makeadmin <user_id>")
	}
	id, _ := strconv.ParseUint(c.Args()[0], 10, 32)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	h.adminUC.SetAdmin(ctx, uint(id), true)
	return c.Send("✅ Promoted")
}
func (h *Handler) HandleRemoveAdmin(c telebot.Context) error {
	if len(c.Args()) < 1 {
		return c.Send("Usage: /removeadmin <user_id>")
	}
	id, _ := strconv.ParseUint(c.Args()[0], 10, 32)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	h.adminUC.SetAdmin(ctx, uint(id), false)
	return c.Send("✅ Demoted")
}

// === PLAN MANAGEMENT ===

func (h *Handler) HandleUserSubscriptions(c telebot.Context) error {
	utils.AnswerCallback(c)
	userID, err := strconv.ParseUint(c.Data(), 10, 32)
	if err != nil {
		return c.Send("Invalid user ID")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	user, _ := h.userRepo.FindByID(ctx, uint(userID))
	telegramID := int64(0)
	if user != nil {
		telegramID = user.TelegramID
	}

	subs, err := h.adminUC.GetSubscriptionsByUser(ctx, uint(userID))
	if err != nil {
		return c.Edit("❌ Error")
	}

	if len(subs) == 0 {
		return c.Edit(fmt.Sprintf("👤 User (TG: %d) has no subscriptions", telegramID))
	}

	msg := fmt.Sprintf("📋 *Subscriptions for User* (TG: `%d`)", telegramID)
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, sub := range subs {
		status := "🟢"
		if sub.Status != "active" {
			status = "🔴"
		}
		rows = append(rows, kb.Row(kb.Data(fmt.Sprintf("%s #%d | %s", status, sub.ID, sub.ProductType), "admin_manage_sub", fmt.Sprintf("%d", sub.ID))))
	}
	kb.Inline(rows...)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleManageSubscription(c telebot.Context) error {
	utils.AnswerCallback(c)
	subID, err := strconv.ParseUint(c.Data(), 10, 32)
	if err != nil {
		return c.Send("Invalid subscription ID")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	sub, err := h.adminUC.GetSubscription(ctx, uint(subID))
	if err != nil {
		return c.Edit("❌ Not found")
	}

	progressBar := generateProgressBar(sub.DataUsed, sub.DataLimit)
	statusIcon := "🟢"
	switch sub.Status {
	case subDomain.SubscriptionStatusPaused:
		statusIcon = "⏸"
	case subDomain.SubscriptionStatusExpired:
		statusIcon = "⏰"
	case subDomain.SubscriptionStatusCancelled:
		statusIcon = "❌"
	}

	// Get online session count
	onlineSessions, _ := h.adminUC.GetUserOnlineSessions(ctx, sub.ConfigEmail)
	onlineText := "0"
	if onlineSessions > 0 {
		onlineText = fmt.Sprintf("🟢 %d", onlineSessions)
	}

	accountName := sub.ConfigEmail
	if accountName == "" {
		accountName = "No Account"
	}

	planName := sub.Label
	if planName == "" {
		planName = "N/A"
	}
	endDateStr := "N/A"
	if sub.EndDate != nil {
		endDateStr = sub.EndDate.Format("2006-01-02 15:04")
	}

	msg := fmt.Sprintf("📋 *Sub #%d Details*\n"+
		"━━━━━━━━━━━━━━━━\n"+
		"👤 *Account:* `%s`\n"+
		"👤 *User:* `%d`\n"+
		"📦 *Plan:* %s\n"+
		"📡 *Type:* %s\n"+
		"🚦 *Status:* %s %s\n"+
		"🌐 *Online Sessions:* %s\n\n"+
		"⏱ *Expires:* %s\n"+
		"📊 *Data Usage:*\n"+
		"`[%s]`\n%s / %s",
		sub.ID, accountName, sub.GetUserID(), planName, sub.ProductType,
		statusIcon, strings.ToUpper(string(sub.Status)),
		onlineText,
		endDateStr,
		progressBar,
		formatBytes(sub.DataUsed), formatBytesLimit(sub.DataLimit))

	idStr := fmt.Sprintf("%d", sub.ID)
	kb := &telebot.ReplyMarkup{}

	var statusBtn telebot.Btn
	if sub.Status == subDomain.SubscriptionStatusPaused {
		statusBtn = kb.Data("▶️ Resume", "admin_sub_resume", idStr)
	} else {
		statusBtn = kb.Data("⏸ Pause", "admin_sub_pause", idStr)
	}
	revokeBtn := kb.Data("🚫 Revoke", "admin_sub_revoke_ask", idStr)
	row1 := kb.Row(statusBtn, revokeBtn)

	row1b := kb.Row(kb.Data("🗑 Delete Permanently", "admin_sub_delete_ask", idStr))

	row2 := kb.Row(
		kb.Data("+7d", "admin_sub_ext", idStr+":7"),
		kb.Data("+30d", "admin_sub_ext", idStr+":30"),
		kb.Data("📅 Custom", "admin_sub_ext_custom", idStr),
	)

	row3 := kb.Row(
		kb.Data("🔄 Reset Data", "admin_sub_reset_data", idStr),
		kb.Data("✏️ Set Data", "admin_sub_setdata", idStr),
	)

	row4 := kb.Row(
		kb.Data("🔑 Regen Key", "admin_sub_regen_ask", idStr),
		kb.Data("👁 Get Link", "admin_sub_link", idStr),
	)

	row5 := kb.Row(kb.Data("🔙 Back to User", "admin_user_subs", fmt.Sprintf("%d", sub.GetUserID())))

	kb.Inline(row1, row1b, row2, row3, row4, row5)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleSubscriptionAction(c telebot.Context) error {
	action := c.Callback().Unique
	payload := c.Data()
	idStr := payload
	if strings.Contains(payload, ":") {
		parts := strings.Split(payload, ":")
		idStr = parts[0]
	}
	subID, _ := strconv.ParseUint(idStr, 10, 32)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	switch action {
	case "admin_sub_pause":
		h.adminUC.PauseSubscription(ctx, uint(subID))
		utils.AnswerCallback(c, "Subscription Paused")
		return h.HandleManageSubscription(c)
	case "admin_sub_resume":
		h.adminUC.ResumeSubscription(ctx, uint(subID))
		utils.AnswerCallback(c, "Subscription Resumed")
		return h.HandleManageSubscription(c)
	case "admin_sub_revoke_ask":
		utils.AnswerCallback(c)
		kb := &telebot.ReplyMarkup{}
		kb.Inline(kb.Row(
			kb.Data("✅ Yes, Revoke", "admin_sub_revoke_confirm", idStr),
			kb.Data("❌ Cancel", "admin_manage_sub", idStr),
		))
		return c.Edit("⚠️ *Are you sure?*\nThis will stop the user's connection.", telebot.ModeMarkdown, kb)
	case "admin_sub_revoke_confirm":
		h.adminUC.RevokeSubscription(ctx, uint(subID))
		utils.AnswerCallback(c, "Subscription Revoked")
		return h.HandleManageSubscription(c)
	case "admin_sub_reset_data":
		h.adminUC.ResetDataUsage(ctx, uint(subID))
		utils.AnswerCallback(c, "Data Usage Reset")
		return h.HandleManageSubscription(c)
	case "admin_sub_ext":
		parts := strings.Split(payload, ":")
		if len(parts) < 2 {
			// FIX: Respond on error
			return utils.AnswerCallback(c, "Error: Invalid Data")
		}
		days, _ := strconv.Atoi(parts[1])
		h.adminUC.ExtendSubscription(ctx, uint(subID), days)
		utils.AnswerCallback(c, "Extended")
		return h.HandleManageSubscription(c)
	case "admin_sub_link":
		utils.AnswerCallback(c)
		link, _ := h.adminUC.GetSubscriptionLink(ctx, uint(subID))
		c.Send(fmt.Sprintf("🔗 *Link #%d*\n\n`%s`", subID, link), telebot.ModeMarkdown)
		return nil
	case "admin_sub_regen_ask":
		utils.AnswerCallback(c)
		kb := &telebot.ReplyMarkup{}
		kb.Inline(kb.Row(
			kb.Data("✅ Yes, Regenerate", "admin_sub_regen_confirm", idStr),
			kb.Data("❌ Cancel", "admin_manage_sub", idStr),
		))
		return c.Edit("⚠️ *Regenerate Key?*", telebot.ModeMarkdown, kb)
	case "admin_sub_regen_confirm":
		h.adminUC.RegenerateSubscriptionKey(ctx, uint(subID), "")
		utils.AnswerCallback(c, "Key Regenerated")
		link, _ := h.adminUC.GetSubscriptionLink(ctx, uint(subID))
		c.Send(fmt.Sprintf("🔑 *New Key for #%d*\n\n`%s`", subID, link), telebot.ModeMarkdown)
		return h.HandleManageSubscription(c)
	}

	// Default Fallback
	utils.AnswerCallback(c)
	return h.HandleManageSubscription(c)
}

func (h *Handler) HandleExtend(c telebot.Context) error    { return nil }
func (h *Handler) HandleRevoke(c telebot.Context) error    { return nil }
func (h *Handler) HandleResetData(c telebot.Context) error { return nil }

// Payment Management
func (h *Handler) HandleBroadcastWithMessage(c telebot.Context, msg string) error {
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	res, _ := h.adminUC.BroadcastMessage(ctx, h.bot, msg, false)
	return c.Send(fmt.Sprintf("📢 Sent: %d, Failed: %d", res.Sent, res.Failed), keyboards.AdminMenu())
}

func (h *Handler) HandleBroadcastActive(c telebot.Context) error {
	msg := strings.Join(c.Args(), " ")
	if strings.TrimSpace(msg) == "" {
		return c.Send("Usage: /broadcast_active <message>")
	}
	ctx, cancel := tgctx.FromTelebotWithTimeout(c, 2*time.Minute)
	defer cancel()

	h.adminUC.BroadcastMessage(ctx, h.bot, msg, true)
	return c.Send("📢 Broadcast sent")
}

func getProductEmoji(pt product.ProductType) string {
	switch pt {
	case product.ProductTypeXray:
		return "🌐"
	case product.ProductTypeOpenVPN:
		return "🔒"
	case product.ProductTypeWireGuard:
		return "⚡"
	default:
		return "📦"
	}
}

func formatBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatBytesLimit(bytes int64) string {
	if bytes == 0 {
		return "Unlimited"
	}
	return formatBytes(bytes)
}

// Removed escapeMarkdown - use utils.EscapeMarkdown instead

func generateProgressBar(used, limit int64) string {
	const barLength = 10
	if limit == 0 {
		return "♾️♾️♾️♾️♾️♾️♾️♾️♾️♾️"
	}
	percent := float64(used) / float64(limit)
	if percent > 1 {
		percent = 1
	}
	filled := int(math.Round(percent * float64(barLength)))
	empty := barLength - filled
	return strings.Repeat("▓", filled) + strings.Repeat("░", empty)
}

// === NEW ADMIN HANDLERS ===

// HandleDeleteSubscriptionAsk shows confirmation for permanent deletion
func (h *Handler) HandleDeleteSubscriptionAsk(c telebot.Context) error {
	utils.AnswerCallback(c)
	idStr := c.Data()
	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(
		kb.Data("⚠️ Yes, Delete Forever", "admin_sub_delete_confirm", idStr),
		kb.Data("❌ Cancel", "admin_manage_sub", idStr),
	))
	return c.Edit("🗑 *Permanently Delete Subscription?*\n\n⚠️ This action *cannot be undone*.\nThe subscription will be removed from the database entirely.", telebot.ModeMarkdown, kb)
}

// HandleDeleteSubscriptionConfirm permanently deletes the subscription
func (h *Handler) HandleDeleteSubscriptionConfirm(c telebot.Context) error {
	subID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.DeleteSubscription(ctx, subID); err != nil {
		utils.AnswerCallbackWithAlert(c, "Error: "+err.Error())
		return nil
	}

	utils.AnswerCallback(c, "Subscription Deleted")
	return c.Edit(fmt.Sprintf("✅ *Subscription #%d Deleted*\n\nThe subscription has been permanently removed.", subID), telebot.ModeMarkdown)
}

// HandleQuickGetSub shows a subscription's management sheet via /getsub command.
func (h *Handler) HandleQuickGetSub(c telebot.Context) error {
	if len(c.Args()) < 1 {
		return c.Send("Usage: `/getsub <subscription_id>`", telebot.ModeMarkdown)
	}
	subID, err := strconv.ParseUint(c.Args()[0], 10, 32)
	if err != nil {
		return c.Send("❌ Invalid subscription ID")
	}

	// Set the data for HandleManageSubscription
	c.Set("callback_data", fmt.Sprintf("%d", subID))

	// Create a synthetic callback context by sending a new message
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	sub, err := h.adminUC.GetSubscription(ctx, uint(subID))
	if err != nil {
		return c.Send("❌ Subscription not found")
	}

	progressBar := generateProgressBar(sub.DataUsed, sub.DataLimit)
	statusIcon := "🟢"
	switch sub.Status {
	case subDomain.SubscriptionStatusPaused:
		statusIcon = "⏸"
	case subDomain.SubscriptionStatusExpired:
		statusIcon = "⏰"
	case subDomain.SubscriptionStatusCancelled:
		statusIcon = "❌"
	}

	onlineSessions, _ := h.adminUC.GetUserOnlineSessions(ctx, sub.ConfigEmail)
	onlineText := "0"
	if onlineSessions > 0 {
		onlineText = fmt.Sprintf("🟢 %d", onlineSessions)
	}

	planName := sub.Label
	if planName == "" {
		planName = "Unknown"
	}

	endDateStr := "N/A"
	if sub.EndDate != nil {
		endDateStr = sub.EndDate.Format("2006-01-02 15:04")
	}

	accountName := sub.ConfigEmail
	if accountName == "" {
		accountName = "No Account"
	}

	msg := fmt.Sprintf("📋 *Sub #%d Details*\n"+
		"━━━━━━━━━━━━━━━━\n"+
		"👤 *Account:* `%s`\n"+
		"👤 *User:* `%d`\n"+
		"📦 *Plan:* %s\n"+
		"📡 *Type:* %s\n"+
		"🚦 *Status:* %s %s\n"+
		"🌐 *Online Sessions:* %s\n\n"+
		"⏱ *Expires:* %s\n"+
		"📊 *Data Usage:*\n"+
		"`[%s]`\n%s / %s",
		sub.ID, accountName, sub.GetUserID(), planName, sub.ProductType,
		statusIcon, strings.ToUpper(string(sub.Status)),
		onlineText,
		endDateStr,
		progressBar,
		formatBytes(sub.DataUsed), formatBytesLimit(sub.DataLimit))

	idStr := fmt.Sprintf("%d", sub.ID)
	kb := &telebot.ReplyMarkup{}

	var statusBtn telebot.Btn
	if sub.Status == subDomain.SubscriptionStatusPaused {
		statusBtn = kb.Data("▶️ Resume", "admin_sub_resume", idStr)
	} else {
		statusBtn = kb.Data("⏸ Pause", "admin_sub_pause", idStr)
	}
	revokeBtn := kb.Data("🚫 Revoke", "admin_sub_revoke_ask", idStr)
	row1 := kb.Row(statusBtn, revokeBtn)
	row1b := kb.Row(kb.Data("🗑 Delete Permanently", "admin_sub_delete_ask", idStr))
	row2 := kb.Row(
		kb.Data("+7d", "admin_sub_ext", idStr+":7"),
		kb.Data("+30d", "admin_sub_ext", idStr+":30"),
		kb.Data("📅 Custom", "admin_sub_ext_custom", idStr),
	)
	row3 := kb.Row(
		kb.Data("🔄 Reset Data", "admin_sub_reset_data", idStr),
		kb.Data("✏️ Set Data", "admin_sub_setdata", idStr),
	)
	row4 := kb.Row(
		kb.Data("🔑 Regen Key", "admin_sub_regen_ask", idStr),
		kb.Data("👁 Get Link", "admin_sub_link", idStr),
	)
	row5 := kb.Row(kb.Data("🔙 Back to User", "admin_user_subs", fmt.Sprintf("%d", sub.GetUserID())))

	kb.Inline(row1, row1b, row2, row3, row4, row5)
	return c.Send(msg, telebot.ModeMarkdown, kb)
}

// HandleListAllSubs lists all subscriptions with rich UI and pagination
func (h *Handler) HandleListAllSubs(c telebot.Context) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	page := 1
	limit := 8

	if c.Callback() != nil && c.Data() != "" {
		page, _ = strconv.Atoi(c.Data())
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	// Get total count for pagination display
	stats, _ := h.adminUC.GetDashboardStats(ctx)
	total := stats.TotalSubscriptions

	subs, err := h.adminUC.ListAllSubscriptions(ctx, "", offset, limit)
	if err != nil {
		return c.Send("❌ Error fetching subscriptions")
	}

	if len(subs) == 0 && page == 1 {
		return c.Send("📭 No subscriptions found")
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))
	if totalPages < 1 {
		totalPages = 1
	}

	msg := fmt.Sprintf("📋 *All Subscriptions*\n"+
		"━━━━━━━━━━━━━━━━\n"+
		"📊 Total: %d | 📄 Page %d of %d\n\n", total, page, totalPages)

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for _, sub := range subs {
		// Status emoji
		statusIcon := "🟢"
		switch sub.Status {
		case subDomain.SubscriptionStatusExpired:
			statusIcon = "🔴"
		case subDomain.SubscriptionStatusPaused:
			statusIcon = "⏸"
		case subDomain.SubscriptionStatusCancelled:
			statusIcon = "❌"
		case subDomain.SubscriptionStatusPending:
			statusIcon = "⏳"
		}

		// Plan name
		planName := sub.Label
		if planName == "" {
			planName = "?"
		}

		// Days remaining/expired text
		var expiryText string
		if sub.EndDate != nil {
			remaining := sub.TimeRemainingFormatted()
			daysLeft := sub.DaysRemaining() // Keep for color logic if needed, but text uses formatted

			if daysLeft < 0 {
				expiryText = fmt.Sprintf("⏰ Exp %dd ago", -daysLeft)
			} else if daysLeft == 0 && !strings.Contains(remaining, "hour") && !strings.Contains(remaining, "min") {
				expiryText = "⚠️ Today"
			} else if daysLeft <= 3 && daysLeft >= 0 {
				expiryText = fmt.Sprintf("⚠️ %s", remaining)
			} else {
				expiryText = fmt.Sprintf("⏳ %s", remaining)
			}
		}

		// Data usage progress bar

		dataText := fmt.Sprintf("%s / %s", formatBytes(sub.DataUsed), formatBytesLimit(sub.DataLimit))

		// User info
		userInfo := fmt.Sprintf("U%d", sub.GetUserID())
		userDisplay := fmt.Sprintf("TG:%d", sub.GetUserID()) // Fallback
		if sub.User != nil {
			if sub.User.Username != "" {
				userInfo = "@" + sub.User.Username
				userDisplay = "@" + sub.User.Username
			} else {
				userDisplay = fmt.Sprintf("TG:%d", sub.User.TelegramID)
			}
		}

		// Account Name (ConfigEmail)
		accountName := sub.ConfigEmail
		if accountName == "" {
			accountName = "No Account"
		}

		// Calculate Usage Percent
		var usagePercent int64
		if sub.DataLimit > 0 {
			usagePercent = (sub.DataUsed * 100) / sub.DataLimit
		}

		// Build message entry (Card Style)
		msg += fmt.Sprintf(
			"➖➖➖➖➖➖➖➖➖➖\n"+
				"%s *%s*\n"+
				"🆔 #%d | 👤 %s\n"+
				"📦 %s\n"+
				"📊 %s (%d%%) | %s\n",
			statusIcon, utils.EscapeMarkdown(accountName),
			sub.ID, utils.EscapeMarkdown(userInfo),
			utils.EscapeMarkdown(planName),
			dataText, usagePercent, expiryText,
		)

		// Button label
		// The /subs command must list subscriptions of all users and the button must write Sub id and the user's username or its chat id if doesn't exists
		btnLabel := fmt.Sprintf("%s #%d %s", statusIcon, sub.ID, userDisplay)
		if len(btnLabel) > 40 {
			btnLabel = btnLabel[:37] + "..."
		}
		rows = append(rows, kb.Row(kb.Data(btnLabel, "admin_manage_sub", fmt.Sprintf("%d", sub.ID))))
	}

	// Pagination navigation
	var navRow []telebot.Btn
	if page > 1 {
		navRow = append(navRow, kb.Data("⏮ 1", "admin_subs_page", "1"))
		navRow = append(navRow, kb.Data("◀️", "admin_subs_page", fmt.Sprintf("%d", page-1)))
	}
	navRow = append(navRow, kb.Data(fmt.Sprintf("📄 %d/%d", page, totalPages), "noop"))
	if page < totalPages {
		navRow = append(navRow, kb.Data("▶️", "admin_subs_page", fmt.Sprintf("%d", page+1)))
		navRow = append(navRow, kb.Data(fmt.Sprintf("⏭ %d", totalPages), "admin_subs_page", fmt.Sprintf("%d", totalPages)))
	}
	if len(navRow) > 0 {
		rows = append(rows, kb.Row(navRow...))
	}

	rows = append(rows, kb.Row(kb.Data("🔙 Admin Menu", "back_admin")))
	kb.Inline(rows...)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// HandleAssignSub assigns an existing subscription to a Telegram user.
func (h *Handler) HandleAssignSub(c telebot.Context) error {
	if len(c.Args()) < 2 {
		return c.Send("Usage: `/assign_sub <sub_id> <telegram_id>`\n\n"+
			"Assigns a subscription to a Telegram user.", telebot.ModeMarkdown)
	}

	subID, err := strconv.ParseUint(c.Args()[0], 10, 32)
	if err != nil {
		return c.Send("❌ Invalid subscription ID")
	}

	telegramID, err := strconv.ParseInt(c.Args()[1], 10, 64)
	if err != nil {
		return c.Send("❌ Invalid Telegram ID")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Verify subscription exists
	sub, err := h.adminUC.GetSubscription(ctx, uint(subID))
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Subscription #%d not found", subID))
	}

	// Verify user exists
	user, err := h.userRepo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ User with Telegram ID `%d` not found.\nThey must `/start` the bot first.", telegramID), telebot.ModeMarkdown)
	}

	// Assign subscription to user
	if err := h.subUC.AssignToUser(ctx, uint(subID), user.ID); err != nil {
		return c.Send("❌ Failed to assign: " + err.Error())
	}

	// Build success message with details
	username := user.Username
	if username == "" {
		username = user.FirstName
	}
	if username == "" {
		username = "Unknown"
	}

	statusEmoji := "🟢"
	switch sub.Status {
	case subDomain.SubscriptionStatusExpired:
		statusEmoji = "🔴"
	case subDomain.SubscriptionStatusPaused:
		statusEmoji = "⏸"
	case subDomain.SubscriptionStatusCancelled:
		statusEmoji = "❌"
	}

	expiryText := "Never"
	if sub.EndDate != nil {
		expiryText = sub.EndDate.Format("Jan 02, 2006")
	}

	msg := fmt.Sprintf("✅ *Subscription Assigned*\n"+
		"━━━━━━━━━━━━━━━━\n"+
		"📦 *Sub #%d* → @%s (ID: `%d`)\n"+
		"📧 Email: `%s`\n"+
		"⏳ Expires: %s\n"+
		"📊 Status: %s %s\n\n"+
		"The user will now see this subscription in their *📋 My Subscriptions* menu.",
		sub.ID, utils.EscapeMarkdown(username), user.TelegramID,
		sub.ConfigEmail,
		expiryText,
		statusEmoji, func() string {
			s := string(sub.Status)
			if len(s) > 0 {
				return strings.ToUpper(s[:1]) + s[1:]
			}
			return s
		}())

	return c.Send(msg, telebot.ModeMarkdown, keyboards.AdminMenu())
}

// === SUBSCRIPTION DATA & EXPIRY MANAGEMENT ===

// HandleManageSub is the entry point from list view
func (h *Handler) HandleManageSub(c telebot.Context) error {
	utils.AnswerCallback(c)
	subID := utils.CallbackID(c)
	return h.handleManageSub(c, subID)
}

// handleManageSub is a helper to show the admin manage subscription menu
func (h *Handler) handleManageSub(c telebot.Context, subID uint) error {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	sub, err := h.adminUC.GetSubscription(ctx, subID)
	if err != nil {
		return c.Send("❌ Subscription not found")
	}
	return h.sendSubscriptionManagementMenu(c, sub)
}

func (h *Handler) HandleAdminSetDataLimit(c telebot.Context) error {
	utils.AnswerCallback(c)
	subID := utils.CallbackID(c)

	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, conversation.StateAdminSetDataLimitValue)
	h.stateManager.SetData(userID, "sub_id", int(subID))

	return c.Send("📊 *Set Custom Data Limit*\n\nEnter the new limit in **GB** (e.g. `50.5`).\nEnter `0` to remove custom limit and reset to plan default.", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAdminSetDataLimitValue(c telebot.Context) error {
	userID := c.Sender().ID
	subID := uint(h.stateManager.GetIntData(userID, "sub_id"))

	input := c.Text()
	limit, err := strconv.ParseFloat(input, 64)
	if err != nil || limit < 0 {
		return c.Send("❌ Invalid format. Please enter a positive number (GB).")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	var limitPtr *float64
	if limit > 0 {
		limitPtr = &limit
	}

	if err := h.adminUC.SetSubscriptionDataLimit(ctx, subID, limitPtr); err != nil {
		return c.Send("❌ Failed to update data limit: " + err.Error())
	}

	h.stateManager.ResetSession(userID)

	msg := "✅ *Data Limit Updated*\n\n"
	if limitPtr == nil {
		msg += "Reset to plan default."
	} else {
		msg += fmt.Sprintf("New Limit: %.2f GB", limit)
	}

	c.Send(msg, telebot.ModeMarkdown)
	return h.handleManageSub(c, subID)
}

func (h *Handler) HandleAdminAddData(c telebot.Context) error {
	utils.AnswerCallback(c)
	subID := utils.CallbackID(c)

	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, conversation.StateAdminAddDataValue)
	h.stateManager.SetData(userID, "sub_id", int(subID))

	return c.Send("➕ *Add Data*\n\nEnter amount in **GB** to add to the current limit (e.g. `10`).", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAdminAddDataValue(c telebot.Context) error {
	userID := c.Sender().ID
	subID := uint(h.stateManager.GetIntData(userID, "sub_id"))

	input := c.Text()
	amount, err := strconv.ParseFloat(input, 64)
	if err != nil || amount <= 0 {
		return c.Send("❌ Invalid format. Please enter a positive number (GB).")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.AddSubscriptionData(ctx, subID, amount); err != nil {
		return c.Send("❌ Failed to add data: " + err.Error())
	}

	h.stateManager.ResetSession(userID)
	c.Send(fmt.Sprintf("✅ Added %.2f GB to subscription.", amount), telebot.ModeMarkdown)
	return h.handleManageSub(c, subID)
}

func (h *Handler) HandleAdminResetData(c telebot.Context) error {
	subID := utils.CallbackID(c)

	msg := "🔄 *Reset Data Usage?*\n\nThis will set `DataUsed` to 0. The user will have their full data limit available again."
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("✅ Yes, Reset", "admin_sub_reset_confirm", fmt.Sprintf("%d", subID)),
			kb.Data("❌ Cancel", "admin_manage_sub", fmt.Sprintf("%d", subID)),
		),
	)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleAdminResetDataConfirm(c telebot.Context) error {
	utils.AnswerCallback(c, "Resetting...")
	subID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.ResetSubscriptionData(ctx, subID); err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}
	return h.handleManageSub(c, subID)
}

func (h *Handler) HandleAdminSetEndDate(c telebot.Context) error {
	utils.AnswerCallback(c)
	subID := utils.CallbackID(c)

	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, conversation.StateAdminSetEndDateValue)
	h.stateManager.SetData(userID, "sub_id", int(subID))

	return c.Send("📅 *Set Custom Expiry Date*\n\nEnter date in format `YYYY-MM-DD` (e.g. `2025-12-31`).\nEnter `0` to remove custom date and use calculation based on plan duration.", telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleAdminSetEndDateValue(c telebot.Context) error {
	userID := c.Sender().ID
	subID := uint(h.stateManager.GetIntData(userID, "sub_id"))

	input := c.Text()
	var datePtr *time.Time

	if input != "0" {
		parsed, err := time.Parse("2006-01-02", input)
		if err != nil {
			return c.Send("❌ Invalid date format. Use YYYY-MM-DD.")
		}
		parsed = parsed.Add(23*time.Hour + 59*time.Minute)
		datePtr = &parsed
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if _, err := h.adminUC.SetSubscriptionEndDate(ctx, subID, datePtr, false); err != nil {
		return c.Send("❌ Failed to set end date: " + err.Error())
	}

	h.stateManager.ResetSession(userID)

	msg := "✅ *Expiry Date Updated*\n\n"
	if datePtr == nil {
		msg += "Reset to plan default."
	} else {
		msg += "New Date: " + datePtr.Format("Jan 02, 2006")
	}

	c.Send(msg, telebot.ModeMarkdown)
	return h.handleManageSub(c, subID)
}

func (h *Handler) HandleAdminQuickExtendOptions(c telebot.Context) error {
	utils.AnswerCallback(c)
	subID := c.Data()

	msg := "📅 *Quick Extend*\nSelect duration to extend:"
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("+7 Days", "admin_extend_7d", subID),
			kb.Data("+14 Days", "admin_extend_14d", subID),
			kb.Data("+30 Days", "admin_extend_30d", subID),
		),
		keyboards.BackRow(kb, "admin_manage_sub", subID),
	)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleAdminExtend7Days(c telebot.Context) error {
	return h.extendSub(c, 7)
}

func (h *Handler) HandleAdminExtend14Days(c telebot.Context) error {
	return h.extendSub(c, 14)
}

func (h *Handler) HandleAdminExtend30Days(c telebot.Context) error {
	return h.extendSub(c, 30)
}

func (h *Handler) extendSub(c telebot.Context, days int) error {
	utils.AnswerCallback(c, fmt.Sprintf("Extending by %d days...", days))
	subID := utils.CallbackID(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if err := h.adminUC.ExtendSubscription(ctx, subID, days); err != nil {
		return c.Send("❌ Failed: " + err.Error())
	}
	return h.handleManageSub(c, subID)
}

func (h *Handler) sendSubscriptionManagementMenu(c telebot.Context, sub *subDomain.Subscription) error {
	statusIcon := "🟢"
	if sub.Status == subDomain.SubscriptionStatusExpired {
		statusIcon = "🔴"
	}

	effectiveLimit := sub.GetEffectiveDataLimit()
	dataLimitInfo := formatBytesLimit(effectiveLimit)
	if sub.IsDataLimitCustom {
		dataLimitInfo += " (Custom)"
	}

	expiryDate := "N/A"
	if sub.EndDate != nil {
		expiryDate = sub.EndDate.Format("Jan 02, 2006")
	}
	if sub.IsEndDateCustom {
		expiryDate += " (Custom)"
	}

	usagePercent := sub.GetDataUsagePercentage()
	progressBar := generateProgressBar(sub.DataUsed, effectiveLimit)

	userInfo := "N/A"
	if sub.User != nil {
		userInfo = fmt.Sprintf("%s (@%s)", sub.User.FirstName, sub.User.Username)
	}

	planName := sub.Label
	if planName == "" {
		planName = "N/A"
	}

	msg := fmt.Sprintf(
		"⚙️ *Manage Subscription #%d*\n"+
			"━━━━━━━━━━━━━━━━\n"+
			"👤 *User:* %s\n"+
			"📦 *Plan:* %s\n"+
			"🚦 *Status:* %s %s\n\n"+
			"━━━━━━━ *DATA* ━━━━━━━\n"+
			"📊 %s / %s\n"+
			"%s %.0f%%\n\n"+
			"━━━━━━━ *EXPIRY* ━━━━━━━\n"+
			"📅 %s\n"+
			"⏳ %s Left\n"+
			"━━━━━━━━━━━━━━━━",
		sub.ID, utils.EscapeMarkdown(userInfo),
		planName,
		statusIcon, strings.ToUpper(string(sub.Status)),
		formatBytes(sub.DataUsed), dataLimitInfo,
		progressBar, usagePercent,
		expiryDate,
		sub.TimeRemainingFormatted(),
	)

	kb := &telebot.ReplyMarkup{}

	kb.Inline(
		kb.Row(
			kb.Data("📊 Set Limit", "admin_sub_set_data", fmt.Sprintf("%d", sub.ID)),
			kb.Data("➕ Add Data", "admin_sub_add_data", fmt.Sprintf("%d", sub.ID)),
		),
		kb.Row(
			kb.Data("📅 Set Expiry", "admin_sub_set_expiry", fmt.Sprintf("%d", sub.ID)),
			kb.Data("🔄 Reset Data", "admin_sub_reset_data", fmt.Sprintf("%d", sub.ID)),
		),
		kb.Row(
			kb.Data("⏫ Quick Extend", "admin_sub_quick_extend", fmt.Sprintf("%d", sub.ID)),
		),
		kb.Row(
			kb.Data("⏸ Pause", "admin_sub_pause", fmt.Sprintf("%d", sub.ID)),
			kb.Data("▶️ Resume", "admin_sub_resume", fmt.Sprintf("%d", sub.ID)),
		),
		kb.Row(
			kb.Data("❌ Revoke", "admin_sub_revoke", fmt.Sprintf("%d", sub.ID)),
		),
		kb.Row(
			kb.Data("🔙 Back to List", "admin_subs_page", "1"),
		),
	)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}
