package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	subdomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	tg "github.com/nasnet-community/nasnet-panel-linux/pkg/telegram"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/utils"
	"gopkg.in/telebot.v3"
)

type Handler struct {
	userUsecase usecase.UserUsecase
	subLinker   SubLinker
	settingUC   settingDomain.SettingUsecase
	linkSecret  string
}

func NewHandler(userUsecase usecase.UserUsecase, subLinker SubLinker, settingUC settingDomain.SettingUsecase, linkSecret string) *Handler {
	return &Handler{
		userUsecase: userUsecase,
		subLinker:   subLinker,
		settingUC:   settingUC,
		linkSecret:  linkSecret,
	}
}

func (h *Handler) HandleStart(c telebot.Context) error {
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
		return c.Send(i18n.Get("en", "ErrGeneral")) // fallback language
	}

	// Deep-link subscription binding: the sub panel's "Connect Telegram" button
	// opens /start with a signed lk_ token. Bind this chat as the sub's
	// notification target before showing the welcome.
	if payload := strings.TrimSpace(c.Message().Payload); payload != "" {
		if msg := h.handleLinkPayload(ctx, payload, user.ID, dbUser.Language); msg != "" {
			_ = c.Send(msg, telebot.ModeMarkdown)
		}
	}

	// First time Language selection
	if time.Since(dbUser.CreatedAt) < 5*time.Second {
		welcomeMsg := "Welcome! Please select your language 👇\n\nبه ربات ما خوش آمدید! لطفا زبان خود را انتخاب کنید 👇"
		return c.Send(welcomeMsg, telebot.ModeMarkdown, keyboards.LanguageSelector())
	}

	welcomeMsg := i18n.Get(dbUser.Language, "WelcomeMsg", dbUser.FirstName)

	return c.Send(welcomeMsg, telebot.ModeMarkdown, h.GetMainMenuWithTrialCheck(ctx, dbUser))
}

// SubLinker is the minimal subscription access HandleStart needs to bind a
// subscription to a Telegram chat via a deep-link token.
type SubLinker interface {
	GetByID(ctx context.Context, id uint) (*subdomain.Subscription, error)
	UpdateTelegramChatIDByConfigID(ctx context.Context, configID string, chatID int64) error
}

// handleLinkPayload binds the sender's chat to a subscription when /start
// carries a signed deep-link token. Returns a localized message to send, or ""
// to stay silent (payload was not a link token).
func (h *Handler) handleLinkPayload(ctx context.Context, payload string, chatID int64, lang string) string {
	if h.subLinker == nil || h.linkSecret == "" {
		return ""
	}
	subID, err := tg.ParseLinkToken(payload, h.linkSecret)
	if err != nil {
		if err == tg.ErrLinkTokenExpired {
			return i18n.Get(lang, "TgLinkExpired")
		}
		return "" // not our token / invalid — stay silent
	}
	sub, err := h.subLinker.GetByID(ctx, uint(subID))
	if err != nil {
		return i18n.Get(lang, "TgLinkExpired")
	}
	linkKey := sub.LinkKey
	if linkKey == "" {
		linkKey = sub.ConfigID
	}
	if err := h.subLinker.UpdateTelegramChatIDByConfigID(ctx, linkKey, chatID); err != nil {
		return i18n.Get(lang, "ErrGeneral")
	}
	return i18n.Get(lang, "TgLinkSuccess")
}

func (h *Handler) HandleProfile(c telebot.Context) error {
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
		return c.Send(i18n.Get("en", "ErrGeneral"))
	}

	// Build status
	status := i18n.Get(dbUser.Language, "StatusActive")
	if dbUser.IsBanned {
		status = i18n.Get(dbUser.Language, "StatusBanned")
	}
	if dbUser.IsAdmin {
		status = i18n.Get(dbUser.Language, "StatusAdmin")
	}

	profileMsg := i18n.GetMD(dbUser.Language, "ProfileMsg",
		dbUser.TelegramID,
		dbUser.Username,
		dbUser.FirstName,
		dbUser.LastName,
		status,
		i18n.GetLangName(dbUser.Language),
		formatDateOnly(dbUser.CreatedAt, dbUser.Language),
	)

	// Add change language inline button
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data(i18n.Get(dbUser.Language, "BtnChangeLang"), "change_lang")),
	)

	return c.Send(profileMsg, telebot.ModeMarkdown, kb)
}

// HandleHelp shows help message with keyboard
func (h *Handler) HandleHelp(c telebot.Context) error {
	user := c.Sender()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	dbUser, err := h.userUsecase.GetByTelegramID(ctx, user.ID)
	lang := "en"
	if err == nil && dbUser != nil {
		lang = dbUser.Language
	}

	helpMsg := i18n.Get(lang, "HelpMsg")
	return c.Send(helpMsg, telebot.ModeMarkdown)
}

// HandleSupport displays support contact information
func (h *Handler) HandleSupport(c telebot.Context) error {
	user := c.Sender()
	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	dbUser, err := h.userUsecase.GetByTelegramID(ctx, user.ID)
	lang := "en"
	if err == nil && dbUser != nil {
		lang = dbUser.Language
	}

	contact := ""
	if h.settingUC != nil {
		if v, err := h.settingUC.GetByKey(ctx, "support_contact"); err == nil && strings.TrimSpace(v) != "" {
			contact = v
		}
	}

	supportMsg := i18n.GetMD(lang, "SupportMsg", contact)
	return c.Send(supportMsg, telebot.ModeMarkdown)
}

// GetMainMenuWithTrialCheck returns the main menu keyboard
func (h *Handler) GetMainMenuWithTrialCheck(ctx context.Context, user *domain.User) *telebot.ReplyMarkup {
	return keyboards.MainMenu(user.Language, false)
}

func formatDateOnly(t time.Time, lang string) string {
	if lang == i18n.LangFA {
		return t.Format("2006/01/02")
	}
	return t.Format("Jan 02, 2006")
}

// HandleLanguageSelection processes the inline language selection
func (h *Handler) HandleLanguageSelection(c telebot.Context) error {
	utils.AnswerCallback(c)
	lang := "en"
	if c.Callback().Unique == "lang_fa" {
		lang = "fa"
	}

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
		return c.Send(i18n.Get(lang, "ErrGeneral"), keyboards.MainMenu(lang, false))
	}

	err = h.userUsecase.UpdateLanguage(ctx, dbUser.ID, lang)
	if err != nil {
		return c.Send(i18n.Get(lang, "ErrGeneral"), keyboards.MainMenu(lang, false))
	}
	dbUser.Language = lang

	msg := i18n.Get(lang, "WelcomeMsg", dbUser.FirstName)
	_ = c.Delete()
	return c.Send(msg, telebot.ModeMarkdown, h.GetMainMenuWithTrialCheck(ctx, dbUser))
}

// HandleChangeLanguage prompt
func (h *Handler) HandleChangeLanguage(c telebot.Context) error {
	utils.AnswerCallback(c)

	var lang string
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
	if err == nil {
		lang = dbUser.Language
	} else {
		lang = "en"
	}

	msg := i18n.Get(lang, "ChangeLanguage")
	return c.Send(msg, telebot.ModeMarkdown, keyboards.LanguageSelector())
}

// HandleSettings displays the user settings panel
func (h *Handler) HandleSettings(c telebot.Context) error {
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
	lang := "en"
	if err == nil && dbUser != nil {
		lang = dbUser.Language
	}

	title := i18n.Get(lang, "SettingsTitle")
	desc := i18n.Get(lang, "SettingsDesc", i18n.GetLangName(lang))

	msg := fmt.Sprintf("%s\n%s", title, desc)
	return c.Send(msg, telebot.ModeMarkdown, keyboards.UserSettingsMenu(lang))
}
