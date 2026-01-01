package telegram

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"

	mntUC "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/usecase"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	userUC "github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	wireguardUC "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

type Handler struct {
	subUsecase    usecase.SubscriptionUsecase
	userUsecase   userUC.UserUsecase
	stateManager  *conversation.StateManager
	settingUC     settingDomain.SettingUsecase
	baseURL       string
	maintenanceUC mntUC.Usecase
	deviceUC      wireguardUC.DeviceUsecase
}

func NewHandler(subUsecase usecase.SubscriptionUsecase, userUsecase userUC.UserUsecase, stateManager *conversation.StateManager, baseURL string, settingUC settingDomain.SettingUsecase, deviceUC wireguardUC.DeviceUsecase) *Handler {
	return &Handler{
		subUsecase:   subUsecase,
		userUsecase:  userUsecase,
		stateManager: stateManager,
		settingUC:    settingUC,
		baseURL:      baseURL,
		deviceUC:     deviceUC,
	}
}

// SetMaintenanceUC wires in the maintenance usecase after construction.
// Nil-safe: when unset, guards are no-ops.
func (h *Handler) SetMaintenanceUC(uc mntUC.Usecase) {
	h.maintenanceUC = uc
}

// maintenanceBlock returns true (and sends a notice) when maintenance is
// active for this subscription / user. Callers should `return nil` on true.
func (h *Handler) maintenanceBlock(c telebot.Context, ctx context.Context, subID *uint, lang string) bool {
	if h.maintenanceUC == nil {
		return false
	}
	status := h.maintenanceUC.Resolve(ctx, 0, subID, i18n.Get(lang, "MaintenanceNotice"))
	if !status.Active {
		return false
	}
	_ = c.Send(status.Message)
	return true
}

// getBaseURL reads app_base_url from DB settings, falling back to the startup config value.
func (h *Handler) getBaseURL() string {
	if h.settingUC != nil {
		if v, err := h.settingUC.GetByKey(context.Background(), "app_base_url"); err == nil && v != "" {
			return v
		}
	}
	return h.baseURL
}

// getUserLang fetches the user's preferred language, defaults to "en"
func (h *Handler) getUserLang(ctx context.Context, telegramID int64) string {
	dbUser, err := h.userUsecase.GetByTelegramID(ctx, telegramID)
	if err == nil && dbUser != nil {
		return dbUser.Language
	}
	return "en"
}

// verifiedSub holds the result of ownership verification.
type verifiedSub struct {
	Sub    *domain.Subscription
	DBUser *userDomain.User
	Lang   string
}

// getVerifiedSubscription fetches the subscription by ID, verifies the caller
// owns it, and returns the subscription, DB user, and resolved language.
// On any failure it sends an appropriate error message and returns a non-nil error.
func (h *Handler) getVerifiedSubscription(c telebot.Context, subID uint) (*verifiedSub, error) {
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	lang := h.getUserLang(ctx, c.Sender().ID)

	sub, err := h.subUsecase.GetByID(ctx, subID)
	if err != nil {
		c.Send(i18n.Get(lang, "SubNotFound"))
		return nil, err
	}

	dbUser, err := h.userUsecase.GetOrCreate(ctx, c.Sender().ID, c.Sender().Username, c.Sender().FirstName, c.Sender().LastName)
	if err != nil {
		c.Send(i18n.Get(lang, "ErrOccurred"))
		return nil, err
	}
	lang = dbUser.Language

	if sub.GetUserID() != dbUser.ID {
		c.Send(i18n.Get(lang, "AccessDenied"))
		return nil, fmt.Errorf("access denied")
	}

	return &verifiedSub{Sub: sub, DBUser: dbUser, Lang: lang}, nil
}

// generateQRCode generates a QR code PNG image in-memory and returns the bytes.
func generateQRCode(content string, size int) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, size)
}

// sendQRPhoto generates a QR code locally, wraps it as a telebot.Photo, and sends it.
func (h *Handler) sendQRPhoto(c telebot.Context, content, caption string, kb *telebot.ReplyMarkup) error {
	pngBytes, err := generateQRCode(content, 300)
	if err != nil {
		return c.Send(caption, telebot.ModeMarkdown, kb)
	}

	photo := &telebot.Photo{
		File:    telebot.FromReader(bytes.NewReader(pngBytes)),
		Caption: caption,
	}
	return c.Send(photo, telebot.ModeMarkdown, kb)
}

// configLines splits a multi-config blob into its non-empty, trimmed lines.
func configLines(config string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(config), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// qrAlbumMax is Telegram's hard cap on media-group size.
const qrAlbumMax = 10

// sendQRAlbums sends one QR photo per config as Telegram media-group album(s),
// chunked to Telegram's 10-item limit. Each photo is captioned with its config
// index and a tap-to-copy link. Media groups can't carry inline keyboards, so
// the caller is responsible for sending navigation buttons afterwards.
func (h *Handler) sendQRAlbums(c telebot.Context, configs []string, lang string) error {
	for start := 0; start < len(configs); start += qrAlbumMax {
		end := start + qrAlbumMax
		if end > len(configs) {
			end = len(configs)
		}

		var album telebot.Album
		for i := start; i < end; i++ {
			pngBytes, err := generateQRCode(configs[i], 300)
			if err != nil {
				continue // link too long to encode — skip its QR
			}
			album = append(album, &telebot.Photo{
				File:    telebot.FromReader(bytes.NewReader(pngBytes)),
				Caption: i18n.Get(lang, "QRCaptionN", i+1, configs[i]),
			})
		}

		switch len(album) {
		case 0:
			continue
		case 1:
			// A one-item media group is invalid; send it as a lone photo.
			if err := c.Send(album[0], telebot.ModeMarkdown); err != nil {
				return err
			}
		default:
			if err := c.SendAlbum(album, telebot.ModeMarkdown); err != nil {
				return err
			}
		}
	}
	return nil
}

// HandleTrialRequest processes the Free Trial button click
func (h *Handler) botSend(c telebot.Context, what interface{}, opts ...interface{}) (*telebot.Message, error) {
	return c.Bot().Send(c.Recipient(), what, opts...)
}

func (h *Handler) botEdit(c telebot.Context, msg *telebot.Message, what interface{}, opts ...interface{}) error {
	_, err := c.Bot().Edit(msg, what, opts...)
	return err
}

// sendConfigToUser handles delivering the config.
func (h *Handler) sendConfigToUser(c telebot.Context, sub *domain.Subscription, lang string) error {
	fileName := fmt.Sprintf("sub_%d%s", sub.ID, sub.FileExt)
	if sub.FileExt == "" {
		fileName = fmt.Sprintf("sub_%d.txt", sub.ID)
	}

	fileCaption := i18n.Get(lang, "ConfigFile", sub.ProductType)

	file := &telebot.Document{
		File:     telebot.FromReader(bytes.NewReader([]byte(sub.ConfigData))),
		FileName: fileName,
		Caption:  fileCaption,
	}
	c.Send(file, telebot.ModeMarkdown)

	// Handle QR/link display

	kb := &telebot.ReplyMarkup{}

	caption := i18n.Get(lang, "SubKeyQR", sub.SubLink)
	return h.sendQRPhoto(c, sub.SubLink, caption, kb)
}

// HandleMySubscriptions shows the Dashboard List
func (h *Handler) HandleMySubscriptions(c telebot.Context) error {
	// FIX: Acknowledge just in case middleware missed it or for faster response
	utils.AnswerCallback(c)

	user := c.Sender()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	dbUser, err := h.userUsecase.GetOrCreate(
		ctx,
		user.ID,
		user.Username,
		user.FirstName,
		user.LastName,
	)
	if err != nil {
		lang := h.getUserLang(ctx, user.ID)
		return c.Send(i18n.Get(lang, "ErrGeneral"), keyboards.MainMenu(lang, false))
	}

	lang := dbUser.Language

	offset := 0
	limit := 5
	page := 1

	if c.Callback() != nil {
		data := c.Callback().Data
		// Callback data is just the page number (e.g., "2"), not prefixed
		if data != "" {
			page, _ = strconv.Atoi(data)
			if page < 1 {
				page = 1
			}
			offset = (page - 1) * limit
		}
	}

	subs, err := h.subUsecase.ListByUserID(ctx, dbUser.ID, offset, limit)
	if err != nil {
		return c.Send(i18n.Get(lang, "ErrFetchSubs"))
	}

	if len(subs) == 0 && page == 1 {
		return c.Send(i18n.Get(lang, "NoSubsYet"), telebot.ModeMarkdown)
	}

	msg := i18n.Get(lang, "MySubsTitle", page)

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	for i, sub := range subs {
		displayName := sub.GetDisplayName()
		statusEmoji := getStatusEmoji(sub.Status)

		var expiryText string
		if sub.Status == domain.SubscriptionStatusActive {
			timeText, isTimeRemaining := formatTimeRemaining(sub, lang)
			if isTimeRemaining {
				expiryText = i18n.Get(lang, "TimeLeft", timeText)
			} else {
				expiryText = timeText
			}
		} else {
			expiryText = getStatusText(sub.Status, lang)
		}

		effectiveLimit := sub.GetEffectiveDataLimit()
		progressBar := generateProgressBar(sub.DataUsed, effectiveLimit)
		dataText := formatDataUsageShort(sub.DataUsed, effectiveLimit)

		msg += fmt.Sprintf(
			"*%d. %s* %s\n"+
				"   %s %s\n"+
				"   `%s` %s\n\n",
			offset+i+1, utils.EscapeMarkdown(displayName), getProductEmoji(sub.ProductType),
			statusEmoji, expiryText,
			progressBar, dataText,
		)

		btnLabel := fmt.Sprintf("%d. %s", offset+i+1, displayName)
		rows = append(rows, kb.Row(kb.Data(btnLabel, "sub_select", fmt.Sprintf("%d", sub.ID))))
	}

	var navRow []telebot.Btn
	if page > 1 {
		navRow = append(navRow, kb.Data(i18n.Get(lang, "BtnPrev"), "sub_list", fmt.Sprintf("%d", page-1)))
	}
	if len(subs) == limit {
		navRow = append(navRow, kb.Data(i18n.Get(lang, "BtnNext"), "sub_list", fmt.Sprintf("%d", page+1)))
	}
	if len(navRow) > 0 {
		rows = append(rows, kb.Row(navRow...))
	}

	rows = append(rows, kb.Row(kb.Data(i18n.Get(lang, "BtnRefreshList"), "sub_list", fmt.Sprintf("%d", page))))

	kb.Inline(rows...)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleSubscriptionDetail(c telebot.Context, subID uint) error {
	utils.AnswerCallback(c)

	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	sub, lang := v.Sub, v.Lang

	// Sync usage — use a short-lived context so slow nodes don't block the UI
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer syncCancel()
	h.subUsecase.SyncUsageFromXray(syncCtx, subID)

	// Re-fetch to get updated usage
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	if freshSub, fetchErr := h.subUsecase.GetByID(ctx, subID); fetchErr == nil {
		sub = freshSub
	}

	displayName := sub.GetDisplayName()
	statusEmoji := getStatusEmoji(sub.Status)

	planName := displayName

	// Expiry: a real date, or "Unlimited" when there's no end date.
	// (Old code left this blank, so the message showed "Expires:" with no value.)
	expires := i18n.Get(lang, "UnlimitedData")
	if sub.EndDate != nil {
		expires = formatDate(*sub.EndDate, lang)
	}

	effectiveLimit := sub.GetEffectiveDataLimit()
	usagePercent := sub.GetDataUsagePercentage()

	var remainingText string
	if effectiveLimit > 0 {
		remaining := effectiveLimit - sub.DataUsed
		if remaining < 0 {
			remaining = 0
		}
		remainingText = formatBytes(remaining)
	} else {
		remainingText = i18n.Get(lang, "UnlimitedData")
	}

	timeRemainingText, _ := formatTimeRemaining(sub, lang)

	// Online line from last activity (usage sync updates it, WG included).
	onlineLine := i18n.Get(lang, "WgNotConnected")
	if sub.LastActiveAt != nil {
		if time.Since(*sub.LastActiveAt) < 3*time.Minute {
			onlineLine = i18n.Get(lang, "StatusOnline")
		} else {
			onlineLine = i18n.Get(lang, "LastActive", formatDate(*sub.LastActiveAt, lang))
		}
	}

	// WG server count gates the Manage-devices button (devices = WG peers).
	wgCount := 0
	if h.deviceUC != nil {
		if servers, _ := h.deviceUC.ListServers(ctx, sub.ID); servers != nil {
			wgCount = len(servers)
		}
	}

	// Displayed "Servers" count is the total reachable config endpoints:
	// xray inbounds (from plan + attached accounts, WG-excluded) plus WG servers.
	// ListServers alone only counts WireGuard, so an xray-only plan showed 0.
	serverCount := wgCount
	if cfgServers, _ := h.subUsecase.GetSubscriptionServers(ctx, sub.ID); cfgServers != nil {
		serverCount += len(cfgServers)
	}

	msg := i18n.Get(lang, "SubDetails",
		utils.EscapeMarkdown(displayName),
		statusEmoji, getStatusText(sub.Status, lang),
		utils.EscapeMarkdown(planName),
		onlineLine,
		formatBytes(sub.DataUsed), formatBytesLimit(effectiveLimit, lang),
		generateProgressBar(sub.DataUsed, effectiveLimit), usagePercent,
		remainingText,
		expires,
		timeRemainingText,
		serverCount,
		formatDate(sub.CreatedAt, lang),
	)

	kb := &telebot.ReplyMarkup{}

	if sub.Status == domain.SubscriptionStatusActive || sub.Status == domain.SubscriptionStatusPaused || sub.Status == domain.SubscriptionStatusTrafficExhausted {
		rows := []telebot.Row{
			kb.Row(
				kb.Data(i18n.Get(lang, "BtnGetConfig"), "sub_config", fmt.Sprintf("%d", sub.ID)),
				kb.Data(i18n.Get(lang, "BtnQRCode"), "sub_qr", fmt.Sprintf("%d", sub.ID)),
			),
			kb.Row(
				kb.Data(i18n.Get(lang, "BtnSubLink"), "sub_link", fmt.Sprintf("%d", sub.ID)),
				kb.Data(i18n.Get(lang, "BtnRegenLink"), "sub_link_regen", fmt.Sprintf("%d", sub.ID)),
			),
			kb.Row(
				kb.Data(i18n.Get(lang, "BtnRename"), "sub_rename_ask", fmt.Sprintf("%d", sub.ID)),
				kb.Data(i18n.Get(lang, "BtnRegenKey"), "sub_regen_ask", fmt.Sprintf("%d", sub.ID)),
			),
		}

		if wgCount > 0 {
			rows = append(rows, kb.Row(kb.Data("⚡ Manage devices", "sub_devices", fmt.Sprintf("%d", sub.ID))))
		}
		rows = append(rows, kb.Row(kb.Data(i18n.Get(lang, "BtnBackToList"), "sub_list")))
		kb.Inline(rows...)
	} else {
		// Expired or cancelled
		kb.Inline(
			kb.Row(
				kb.Data(i18n.Get(lang, "BtnBackToList"), "sub_list"),
			),
		)
	}

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// HandleGetConfig retrieves and sends config in Markdown format (copyable text).
// replyWithBack shows msg with a single Back-to-detail button, editing the
// current message when invoked from a callback (so we don't stack messages).
func (h *Handler) replyWithBack(c telebot.Context, msg, lang string, subID uint) error {
	// Plain text (no Markdown) — these are error strings that may embed
	// err.Error() with characters that would break Markdown parsing.
	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data(i18n.Get(lang, "BtnBack"), "sub_select", fmt.Sprintf("%d", subID))))
	return utils.EditOrSend(c, msg, kb)
}

// countryFlag converts a 2-letter country code to a flag emoji (🌐 fallback).
func countryFlag(cc string) string {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
		return "🌐"
	}
	return string(rune(0x1F1E6)+rune(cc[0]-'A')) + string(rune(0x1F1E6)+rune(cc[1]-'A'))
}

// HandleGetConfig is the config entry point: >1 server opens the Servers menu,
// otherwise it shows all config lines directly.
func (h *Handler) HandleGetConfig(c telebot.Context, subID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	sub, lang := v.Sub, v.Lang
	utils.AnswerCallback(c)

	if sub.Status != domain.SubscriptionStatusActive {
		return h.replyWithBack(c, i18n.Get(lang, "SubNotActive"), lang, sub.ID)
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	servers, _ := h.subUsecase.GetSubscriptionServers(ctx, sub.ID)
	if len(servers) <= 1 {
		return h.HandleGetConfigAll(c, subID)
	}

	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for i, s := range servers {
		dot := "⚪"
		if s.Online {
			dot = "🟢"
		}
		rows = append(rows, kb.Row(kb.Data(
			fmt.Sprintf("%s %s %s", countryFlag(s.CountryCode), s.Name, dot),
			"srv_cfg", fmt.Sprintf("%d", sub.ID), fmt.Sprintf("%d", i))))
	}
	rows = append(rows, kb.Row(kb.Data(i18n.Get(lang, "BtnAllConfigs"), "sub_config_all", fmt.Sprintf("%d", sub.ID))))
	rows = append(rows, kb.Row(kb.Data(i18n.Get(lang, "BtnBack"), "sub_select", fmt.Sprintf("%d", sub.ID))))
	kb.Inline(rows...)

	msg := i18n.Get(lang, "ServersTitle", len(servers))
	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// HandleGetConfigAll shows every server's config line as copyable blocks.
func (h *Handler) HandleGetConfigAll(c telebot.Context, subID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	sub, lang := v.Sub, v.Lang

	utils.AnswerCallback(c, i18n.Get(lang, "GeneratingConfig"))

	if sub.Status != domain.SubscriptionStatusActive {
		return h.replyWithBack(c, i18n.Get(lang, "SubNotActive"), lang, sub.ID)
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Generate fresh config to include latest active nodes
	configResult, err := h.subUsecase.GetSubscriptionConfig(ctx, sub.GetLinkKey())
	if err != nil {
		return h.replyWithBack(c, i18n.Get(lang, "ErrGenerateConfig"), lang, sub.ID)
	}

	// Format configs as Markdown with copyable code blocks
	var msgBuilder strings.Builder
	msgBuilder.WriteString(i18n.Get(lang, "ConfigTitle"))

	for i, line := range configLines(configResult.Config) {
		msgBuilder.WriteString(i18n.Get(lang, "ConfigN", i+1))
		msgBuilder.WriteString(fmt.Sprintf("`%s`\n\n", line))
	}

	msgBuilder.WriteString(i18n.Get(lang, "ConfigTapCopy"))

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(i18n.Get(lang, "BtnShowQR"), "sub_qr", fmt.Sprintf("%d", sub.ID)),
			kb.Data(i18n.Get(lang, "BtnBack"), "sub_select", fmt.Sprintf("%d", sub.ID)),
		),
	)

	return utils.EditOrSend(c, msgBuilder.String(), telebot.ModeMarkdown, kb)
}

// HandleGetQR generates and sends QR code for the subscription.
func (h *Handler) HandleGetQR(c telebot.Context, subID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	sub, lang := v.Sub, v.Lang

	utils.AnswerCallback(c, i18n.Get(lang, "GeneratingQR"))

	if sub.Status != domain.SubscriptionStatusActive {
		return h.replyWithBack(c, i18n.Get(lang, "SubNotActive"), lang, sub.ID)
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	// Generate fresh config to include latest active nodes
	configResult, err := h.subUsecase.GetSubscriptionConfig(ctx, sub.GetLinkKey())
	if err != nil {
		return h.replyWithBack(c, i18n.Get(lang, "ErrGenerateConfig"), lang, sub.ID)
	}

	configs := configLines(configResult.Config)
	if len(configs) == 0 {
		return h.replyWithBack(c, i18n.Get(lang, "NoConfigAvailable"), lang, sub.ID)
	}

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(i18n.Get(lang, "BtnGetConfig"), "sub_config", fmt.Sprintf("%d", sub.ID)),
			kb.Data(i18n.Get(lang, "BtnBack"), "sub_select", fmt.Sprintf("%d", sub.ID)),
		),
	)

	// Single config → one photo with the nav keyboard attached.
	if len(configs) == 1 {
		caption := i18n.Get(lang, "QRCode", configs[0])
		return h.sendQRPhoto(c, configs[0], caption, kb)
	}

	// Multiple configs → one QR per config. Telegram media groups can't carry
	// inline keyboards, so send the QR album(s) first, then the nav buttons.
	if err := h.sendQRAlbums(c, configs, lang); err != nil {
		caption := i18n.Get(lang, "QRCode", configs[0])
		return h.sendQRPhoto(c, configs[0], caption, kb)
	}
	return c.Send(i18n.Get(lang, "QRAlbumNav"), telebot.ModeMarkdown, kb)
}

// HandleServerConfig sends one server's QR + tap-to-copy link (per-server view).
func (h *Handler) HandleServerConfig(c telebot.Context, subID, idx uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	sub, lang := v.Sub, v.Lang
	utils.AnswerCallback(c, i18n.Get(lang, "GeneratingQR"))

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	servers, err := h.subUsecase.GetSubscriptionServers(ctx, sub.ID)
	if err != nil || int(idx) >= len(servers) {
		return h.HandleGetConfig(c, subID) // stale index → re-render the menu
	}
	s := servers[idx]
	if s.Link == "" {
		return h.replyWithBack(c, i18n.Get(lang, "ErrGenerateConfig"), lang, sub.ID)
	}

	status := "⚪"
	if s.Online {
		status = "🟢"
	}
	caption := fmt.Sprintf("%s *%s* · %s · %s\n\n`%s`",
		countryFlag(s.CountryCode), utils.EscapeMarkdown(s.Name), s.Protocol, status, s.Link)
	return h.sendQRPhoto(c, s.Link, caption, nil)
}

// HandleSubLink shows the HTTP subscription URL for V2Ray clients
func (h *Handler) HandleSubLink(c telebot.Context, subID uint) error {
	utils.AnswerCallback(c)

	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	sub, lang := v.Sub, v.Lang

	if sub.Status != domain.SubscriptionStatusActive {
		return h.replyWithBack(c, i18n.Get(lang, "SubNotActive"), lang, sub.ID)
	}

	// Construct subscription URL
	baseURL := h.getBaseURL()
	if baseURL == "" {
		return h.replyWithBack(c, i18n.Get(lang, "SubLinkNotConfigured"), lang, sub.ID)
	}

	subURL := fmt.Sprintf("%s/sub/%s", baseURL, sub.GetLinkKey())

	msg := i18n.Get(lang, "SubLinkTitle", subURL)

	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data(i18n.Get(lang, "BtnBack"), "sub_select", fmt.Sprintf("%d", sub.ID))),
	)

	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// === Renaming Logic ===
func (h *Handler) HandleRenameAsk(c telebot.Context, subID uint) error {
	utils.AnswerCallback(c)

	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	_ = v.Sub
	lang := v.Lang

	userID := c.Sender().ID
	h.stateManager.StartConversation(userID, conversation.StateRenameSubscription)
	h.stateManager.SetData(userID, "rename_sub_id", int(subID))
	msg := i18n.Get(lang, "RenameTitle", subID)
	return c.Send(msg, telebot.ModeMarkdown, keyboards.Cancel())
}

func (h *Handler) HandleRenameSave(c telebot.Context) error {
	userID := c.Sender().ID
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	lang := h.getUserLang(ctx, userID)
	newName := strings.TrimSpace(c.Text())

	if utf8.RuneCountInString(newName) > 20 {
		return c.Send(i18n.Get(lang, "NameTooLong"))
	}
	if len(newName) < 1 {
		return c.Send(i18n.Get(lang, "NameEmpty"))
	}

	subIDInt := h.stateManager.GetIntData(userID, "rename_sub_id")
	if subIDInt == 0 {
		h.stateManager.ResetSession(userID)
		return c.Send(i18n.Get(lang, "SessionError"))
	}
	subIDUint := uint(subIDInt)

	// Verify ownership before allowing rename
	sub, err := h.subUsecase.GetByID(ctx, subIDUint)
	if err != nil {
		h.stateManager.ResetSession(userID)
		return c.Send(i18n.Get(lang, "SubNotFound"))
	}
	dbUser, err := h.userUsecase.GetByTelegramID(ctx, userID)
	if err != nil || dbUser == nil || sub.GetUserID() != dbUser.ID {
		h.stateManager.ResetSession(userID)
		return c.Send(i18n.Get(lang, "AccessDenied"))
	}

	if err := h.subUsecase.RenameSubscription(ctx, subIDUint, newName); err != nil {
		return c.Send(i18n.Get(lang, "FailedUpdateName"))
	}
	h.stateManager.ResetSession(userID)
	c.Send(i18n.Get(lang, "RenamedTo", utils.EscapeMarkdown(newName)), telebot.ModeMarkdown)
	return h.HandleSubscriptionDetail(c, subIDUint)
}

// === Regenerate Logic ===
func (h *Handler) HandleRegenerateAsk(c telebot.Context, subID uint) error {
	utils.AnswerCallback(c)
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()
	lang := h.getUserLang(ctx, c.Sender().ID)

	msg := i18n.Get(lang, "RegenWarning")
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data(i18n.Get(lang, "BtnYesRegenerate"), "sub_regen_confirm", fmt.Sprintf("%d", subID)),
			kb.Data(i18n.Get(lang, "BtnCancel"), "sub_select", fmt.Sprintf("%d", subID)),
		),
	)
	return c.Edit(msg, telebot.ModeMarkdown, kb)
}

func (h *Handler) HandleRegenerateConfirm(c telebot.Context, subID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	lang := v.Lang

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if h.maintenanceBlock(c, ctx, &subID, lang) {
		return nil
	}

	utils.AnswerCallback(c, i18n.Get(lang, "Regenerating"))
	c.Edit(i18n.Get(lang, "Processing"), telebot.ModeMarkdown)

	newSub, err := h.subUsecase.RegenerateUUID(ctx, subID)
	if err != nil {
		return c.Edit(i18n.Get(lang, "RegenFailed", err.Error()))
	}
	c.Send(i18n.Get(lang, "KeyRegenerated"))
	return h.HandleGetConfig(c, newSub.ID)
}

// HandleRegenerateLink rotates the /sub/ share URL (link key). Installed apps
// keep working (config/UUID unchanged); only the old share link stops resolving.
func (h *Handler) HandleRegenerateLink(c telebot.Context, subID uint) error {
	v, err := h.getVerifiedSubscription(c, subID)
	if err != nil {
		return nil
	}
	lang := v.Lang
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	if h.maintenanceBlock(c, ctx, &subID, lang) {
		return nil
	}

	utils.AnswerCallback(c, i18n.Get(lang, "Regenerating"))
	if _, err := h.subUsecase.RegenerateSubscriptionKey(ctx, subID, ""); err != nil {
		return h.replyWithBack(c, i18n.Get(lang, "RegenFailed", err.Error()), lang, subID)
	}
	link, err := h.subUsecase.GetSubscriptionLink(ctx, subID)
	if err != nil {
		return h.replyWithBack(c, i18n.Get(lang, "ErrGeneral"), lang, subID)
	}
	msg := i18n.Get(lang, "SubLinkRegenerated", link)
	kb := &telebot.ReplyMarkup{}
	kb.Inline(kb.Row(kb.Data(i18n.Get(lang, "BtnBack"), "sub_select", fmt.Sprintf("%d", subID))))
	return utils.EditOrSend(c, msg, telebot.ModeMarkdown, kb)
}

// === Helpers ===
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

func formatDataUsageShort(used, limit int64) string {
	if limit == 0 {
		return formatBytes(used)
	}
	return fmt.Sprintf("%s / %s", formatBytes(used), formatBytes(limit))
}

func getStatusEmoji(status domain.SubscriptionStatus) string {
	switch status {
	case domain.SubscriptionStatusPending:
		return "⏳"
	case domain.SubscriptionStatusActive:
		return "🟢"
	case domain.SubscriptionStatusPaused:
		return "⏸"
	case domain.SubscriptionStatusExpired:
		return "🔴"
	case domain.SubscriptionStatusCancelled:
		return "❌"
	case domain.SubscriptionStatusTrafficExhausted:
		return "🟠"
	default:
		return "❓"
	}
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
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatBytesLimit(limit int64, lang string) string {
	if limit == 0 {
		return i18n.Get(lang, "DataLimitUnlimited")
	}
	return formatBytes(limit)
}

func formatTimeRemaining(sub *domain.Subscription, lang string) (string, bool) {
	endDate := sub.GetEffectiveEndDate()
	if endDate == nil {
		return i18n.Get(lang, "TimeUnlimited"), false
	}
	duration := time.Until(*endDate)
	if duration < 0 {
		return i18n.Get(lang, "TimeExpired"), false
	}

	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24

	if days > 0 {
		if hours > 0 {
			return i18n.Get(lang, "TimeDaysHours", days, hours), true
		}
		return i18n.Get(lang, "TimeDays", days), true
	}

	if hours > 0 {
		return i18n.Get(lang, "TimeHours", hours), true
	}

	minutes := int(duration.Minutes()) % 60
	if minutes > 0 {
		return i18n.Get(lang, "TimeMinutes", minutes), true
	}

	return i18n.Get(lang, "TimeLessThanMin"), true
}

func getStatusText(status domain.SubscriptionStatus, lang string) string {
	switch status {
	case domain.SubscriptionStatusActive:
		return i18n.Get(lang, "StatusTextActive")
	case domain.SubscriptionStatusExpired:
		return i18n.Get(lang, "StatusTextExpired")
	case domain.SubscriptionStatusPending:
		return i18n.Get(lang, "StatusTextPending")
	case domain.SubscriptionStatusCancelled:
		return i18n.Get(lang, "StatusTextCancelled")
	case domain.SubscriptionStatusPaused:
		return i18n.Get(lang, "StatusTextPaused")
	case domain.SubscriptionStatusTrafficExhausted:
		return i18n.Get(lang, "StatusTextTrafficExhausted")
	default:
		return string(status)
	}
}

func formatDate(t time.Time, lang string) string {
	if lang == i18n.LangFA {
		return t.Format("2006/01/02, 15:04")
	}
	return t.Format("Jan 02, 15:04")
}

func formatDateOnly(t time.Time, lang string) string {
	if lang == i18n.LangFA {
		return t.Format("2006/01/02")
	}
	return t.Format("Jan 02, 2006")
}

// parseIDGB parses "id" or "id:gb" callback data used by the metered steppers.
func parseIDGB(data string) (uint, float64) {
	parts := strings.Split(strings.TrimSpace(data), ":")
	id, _ := strconv.ParseUint(parts[0], 10, 32)
	var gb float64
	if len(parts) > 1 {
		gb, _ = strconv.ParseFloat(parts[1], 64)
	}
	return uint(id), gb
}
