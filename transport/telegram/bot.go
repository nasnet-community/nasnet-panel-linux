package telegram

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	adminTg "github.com/nasnet-community/nasnet-panel-linux/internal/admin/delivery/telegram"
	adminMiddleware "github.com/nasnet-community/nasnet-panel-linux/internal/admin/middleware"
	mntTg "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/delivery/telegram"
	nodeTg "github.com/nasnet-community/nasnet-panel-linux/internal/node/delivery/telegram"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	sniTg "github.com/nasnet-community/nasnet-panel-linux/internal/sni/delivery/telegram"
	subTg "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/delivery/telegram"
	userTg "github.com/nasnet-community/nasnet-panel-linux/internal/user/delivery/telegram"
	userUC "github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/keyboards"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	proxyPkg "github.com/nasnet-community/nasnet-panel-linux/pkg/proxy"
	"gopkg.in/telebot.v3"
)

// Bot creation retry policy. The initial telebot.NewBot call performs a
// synchronous getMe request which fails (often with EOF) when api.telegram.org
// is momentarily unreachable — common behind a flaky proxy. Once the bot is
// created, telebot's long poller self-heals from runtime network drops, so we
// only need to retry the initial handshake.
const (
	botInitMaxAttempts = 5
	botInitBaseDelay   = 2 * time.Second
	botInitMaxDelay    = 30 * time.Second
)

// stateHandler is a function that handles text input for a specific conversation state.
type stateHandler func(c telebot.Context) error

type Bot struct {
	bot                *telebot.Bot
	userHandler        *userTg.Handler
	subHandler         *subTg.Handler
	adminHandler       *adminTg.Handler
	nodeHandler        *nodeTg.Handler
	sniHandler         *sniTg.Handler
	maintenanceHandler *mntTg.Handler
	adminMiddleware    *adminMiddleware.AdminMiddleware
	stateManager       *conversation.StateManager
	userUsecase        userUC.UserUsecase
	settingUsecase     settingDomain.SettingUsecase
	adminIDs           []int64
	stopCh             chan struct{}
	enabled            atomic.Bool
	stateHandlers      map[conversation.ConversationState]stateHandler
}

func NewBot(
	cfg config.TelegramConfig,
	adminIDs []int64,
	userHandler *userTg.Handler,
	subHandler *subTg.Handler,
	adminHandler *adminTg.Handler,
	nodeHandler *nodeTg.Handler,
	sniHandler *sniTg.Handler,
	maintenanceHandler *mntTg.Handler,
	adminMW *adminMiddleware.AdminMiddleware,
	stateManager *conversation.StateManager,
	userUsecase userUC.UserUsecase,
	settingUsecase settingDomain.SettingUsecase,
	httpFactory *httpclient.Factory,
) (*Bot, error) {
	log := logger.GetLogger()

	var poller telebot.Poller
	if cfg.BotMode == "webhook" {
		log.Info("Using Webhook Poller")
		poller = &telebot.Webhook{
			Listen:   ":8081",
			Endpoint: &telebot.WebhookEndpoint{PublicURL: cfg.WebhookURL},
		}
	} else {
		log.Info("Using Long Poller")
		poller = &telebot.LongPoller{Timeout: 10 * time.Second}
	}

	// Outbound HTTP client selection — precedence:
	// 1. Global outbound proxy factory if proxy_use_telegram=true AND
	//    outbound_proxy_url is valid (LiveClient → live reload).
	// 2. Legacy telegram_proxy_* settings if telegram_proxy_enabled=true.
	// 3. nil (telebot uses its default direct client).
	var httpClient *http.Client
	if httpFactory != nil && httpFactory.IsProxyConfigured() {
		httpClient = httpFactory.LiveClient(httpclient.FeatureTelegram, 30*time.Second)
		log.Info("Using outbound-proxy factory for Telegram API")
	}
	if httpClient == nil && cfg.Proxy.Enabled {
		legacyCfg := proxyPkg.Config{
			Enabled:  cfg.Proxy.Enabled,
			Type:     cfg.Proxy.Type,
			Host:     cfg.Proxy.Host,
			Port:     cfg.Proxy.Port,
			Username: cfg.Proxy.Username,
			Password: cfg.Proxy.Password,
		}
		legacyClient, err := proxyPkg.NewHTTPClient(legacyCfg)
		if err != nil {
			log.WithError(err).Error("Failed to create legacy proxy HTTP client")
			return nil, err
		}
		if legacyClient != nil {
			httpClient = legacyClient
			log.WithField("proxy", fmt.Sprintf("%s:%d", cfg.Proxy.Host, cfg.Proxy.Port)).Info("Using legacy SOCKS5 proxy for Telegram API")
		}
	}

	// Global error handler
	pref := telebot.Settings{
		Token:  cfg.BotToken,
		Poller: poller,
		Client: httpClient, // nil when proxy disabled, uses default client
		OnError: func(err error, c telebot.Context) {
			if c != nil && c.Sender() != nil {
				log.WithField("user_id", c.Sender().ID).WithError(err).Error("Telegram handler error")
			} else {
				log.WithError(err).Error("Telegram error (no sender context)")
			}
		},
	}

	var b *telebot.Bot
	var err error
	for attempt := 1; ; attempt++ {
		b, err = telebot.NewBot(pref)
		if err == nil {
			break
		}
		if attempt >= botInitMaxAttempts {
			log.WithError(err).WithField("attempts", attempt).Error("Failed to create Telegram bot after retries")
			return nil, err
		}
		delay := botInitBaseDelay << (attempt - 1)
		if delay > botInitMaxDelay {
			delay = botInitMaxDelay
		}
		log.WithError(err).WithFields(map[string]interface{}{
			"attempt":     attempt,
			"max":         botInitMaxAttempts,
			"retry_after": delay.String(),
		}).Warn("Telegram bot init failed; retrying")
		time.Sleep(delay)
	}

	// Persist bot username so the sub panel can link to it
	if settingUsecase != nil && b.Me != nil && b.Me.Username != "" {
		_ = settingUsecase.UpdateMany(context.Background(), []*settingDomain.Setting{
			{Key: "telegram_bot_username", Value: b.Me.Username, Type: "string", Category: "telegram"},
		})
	}

	// Remove any stale webhook when using long polling mode,
	// otherwise Telegram ignores getUpdates calls.
	if cfg.BotMode != "webhook" {
		if err := b.RemoveWebhook(); err != nil {
			log.WithError(err).Warn("Failed to remove stale webhook")
		} else {
			log.Debug("Cleared any existing webhook for long polling")
		}
	}

	b.Use(AutoRespondMiddleware)

	bot := &Bot{
		bot:                b,
		adminIDs:           adminIDs,
		userHandler:        userHandler,
		subHandler:         subHandler,
		adminHandler:       adminHandler,
		nodeHandler:        nodeHandler,
		sniHandler:         sniHandler,
		maintenanceHandler: maintenanceHandler,
		adminMiddleware:    adminMW,
		stateManager:       stateManager,
		userUsecase:        userUsecase,
		settingUsecase:     settingUsecase,
		stopCh:             make(chan struct{}),
	}

	// Read initial bot-enabled state from settings
	bot.enabled.Store(true)
	if val, err := settingUsecase.GetByKey(context.Background(), "telegram_bot_enabled"); err == nil {
		bot.enabled.Store(val != "false")
	}

	b.Use(bot.BotEnabledMiddleware)

	bot.registerHandlers()

	return bot, nil
}

// BotEnabledMiddleware silently drops updates when the bot is disabled via settings.
func (b *Bot) BotEnabledMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	log := logger.GetLogger()
	return func(c telebot.Context) error {
		if !b.enabled.Load() {
			log.WithField("user_id", c.Sender().ID).Debug("Bot disabled, dropping update")
			return nil
		}
		return next(c)
	}
}

// AutoRespondMiddleware automatically answers callback queries if the handler didn't.
func AutoRespondMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	log := logger.GetLogger()
	return func(c telebot.Context) error {
		// Log incoming update
		if c.Callback() != nil {
			log.WithFields(map[string]interface{}{
				"user_id":  c.Sender().ID,
				"callback": c.Callback().Unique,
				"data":     c.Callback().Data,
			}).Debug("Received callback query")
		} else if c.Message() != nil {
			fields := map[string]interface{}{
				"user_id": c.Sender().ID,
			}
			if c.Message().Text != "" {
				text := c.Message().Text
				if len(text) > 50 {
					text = text[:50] + "..."
				}
				fields["text"] = text
			}
			if c.Message().Photo != nil {
				fields["has_photo"] = true
			}
			log.WithFields(fields).Debug("Received message")
		}

		err := next(c)

		if err != nil {
			log.WithField("user_id", c.Sender().ID).WithError(err).Debug("Handler returned error")
		}

		// If this was a callback interaction, try to respond to stop the spinner.
		// If the handler already responded, this might fail or be ignored, which is fine.
		if c.Callback() != nil {
			// We ignore the error here because the most common error is
			// "query is too old" or "query already answered", both of which are acceptable.
			_ = c.Respond()
		}

		return err
	}
}

func (b *Bot) GetBot() *telebot.Bot {
	return b.bot
}

// getUserLang fetches the user's preferred language, defaults to "en"
func (b *Bot) getUserLang(telegramID int64) string {
	log := logger.GetLogger()
	ctx := context.Background()
	dbUser, err := b.userUsecase.GetByTelegramID(ctx, telegramID)
	if err != nil {
		log.WithField("user_id", telegramID).WithError(err).Debug("Failed to fetch user language, defaulting to en")
		return "en"
	}
	if dbUser != nil {
		return dbUser.Language
	}
	return "en"
}

// getUserMenu returns the main menu with trial eligibility check for non-admin users
func (b *Bot) getUserMenu(userID int64) *telebot.ReplyMarkup {
	ctx := context.Background()
	dbUser, err := b.userUsecase.GetByTelegramID(ctx, userID)
	if err != nil || dbUser == nil {
		lang := b.getUserLang(userID)
		return keyboards.MainMenu(lang, false)
	}
	return b.userHandler.GetMainMenuWithTrialCheck(ctx, dbUser)
}

func (b *Bot) registerHandlers() {
	log := logger.GetLogger()
	log.Debug("Registering Telegram bot handlers")

	// Build state→handler map for text input routing
	b.setupStateHandlers()

	// User commands
	b.bot.Handle("/start", b.userHandler.HandleStart)
	b.bot.Handle("/profile", b.userHandler.HandleProfile)
	b.bot.Handle("/help", b.userHandler.HandleHelp)
	b.bot.Handle("/language", b.userHandler.HandleChangeLanguage)

	// Cancel command — resets conversation state from any active wizard
	b.bot.Handle("/cancel", b.handleCancel)

	// Subscription commands
	b.bot.Handle("/subscriptions", b.subHandler.HandleMySubscriptions)
	b.bot.Handle("/my_subscriptions", b.subHandler.HandleMySubscriptions)

	// Register text button handlers
	b.registerButtonHandlers()

	// Admin commands (protected by middleware)
	b.registerAdminCommands()

	// Manual User commands (protected by middleware inside register)
	b.registerManualUserCommands()
}

func (b *Bot) registerButtonHandlers() {
	// Main menu buttons - Register for both languages
	b.bot.Handle(i18n.Get("en", "BtnMySubs"), b.subHandler.HandleMySubscriptions)
	b.bot.Handle(i18n.Get("fa", "BtnMySubs"), b.subHandler.HandleMySubscriptions)

	b.bot.Handle(i18n.Get("en", "BtnProfile"), b.userHandler.HandleProfile)
	b.bot.Handle(i18n.Get("fa", "BtnProfile"), b.userHandler.HandleProfile)

	b.bot.Handle(i18n.Get("en", "BtnSupport"), b.userHandler.HandleSupport)
	b.bot.Handle(i18n.Get("fa", "BtnSupport"), b.userHandler.HandleSupport)

	b.bot.Handle(i18n.Get("en", "BtnHelp"), b.userHandler.HandleHelp)
	b.bot.Handle(i18n.Get("fa", "BtnHelp"), b.userHandler.HandleHelp)

	b.bot.Handle(i18n.Get("en", "BtnGuide"), b.userHandler.HandleHelp)
	b.bot.Handle(i18n.Get("fa", "BtnGuide"), b.userHandler.HandleHelp)

	b.bot.Handle(i18n.Get("en", "BtnSettings"), b.userHandler.HandleSettings)
	b.bot.Handle(i18n.Get("fa", "BtnSettings"), b.userHandler.HandleSettings)

	// Admin menu buttons
	mw := b.adminMiddleware.RequireAdmin
	h := b.adminHandler
	nh := b.nodeHandler

	b.bot.Handle("📈 Admin Stats", mw(h.HandleStats))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_online_users"}, mw(h.HandleOnlineUsers))
	b.bot.Handle("👥 Users", mw(h.HandleUsers))
	b.bot.Handle("🖥 Servers", mw(nh.HandleNodes))
	b.bot.Handle("📢 Broadcast", mw(b.handleBroadcastStart))
	b.bot.Handle("❓ Admin Help", mw(h.HandleAdminHelp))
	b.bot.Handle("🔐 TLS Certs", mw(b.sniHandler.HandleSNIList))
	b.bot.Handle("🔙 User Menu", b.handleBackToUserMenu)

	// SNI Certificate Management buttons
	sh := b.sniHandler
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_sync"}, mw(sh.HandleSNISync))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_list"}, mw(sh.HandleSNIList))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_add"}, mw(sh.HandleSNIAdd))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_mode_content"}, mw(sh.HandleSNIModeContent))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_mode_path"}, mw(sh.HandleSNIModePath))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_view"}, mw(sh.HandleSNIView))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_delete_ask"}, mw(sh.HandleSNIDeleteAsk))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_delete"}, mw(sh.HandleSNIDelete))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_edit_name"}, mw(sh.HandleSNIEditName))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_edit_domain"}, mw(sh.HandleSNIEditDomain))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_edit_cert"}, mw(sh.HandleSNIEditCert))

	// Admin Subscription Management
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_manage_sub"}, mw(h.HandleManageSub))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_set_data"}, mw(h.HandleAdminSetDataLimit))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_add_data"}, mw(h.HandleAdminAddData))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_reset_data"}, mw(h.HandleAdminResetData))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_reset_confirm"}, mw(h.HandleAdminResetDataConfirm))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_set_expiry"}, mw(h.HandleAdminSetEndDate))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_quick_extend"}, mw(h.HandleAdminQuickExtendOptions))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_extend_7d"}, mw(h.HandleAdminExtend7Days))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_extend_14d"}, mw(h.HandleAdminExtend14Days))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_extend_30d"}, mw(h.HandleAdminExtend30Days))

	// ACME Certificate Issuance callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_add_manual"}, mw(sh.HandleSNIAddManual))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_issue_start"}, mw(sh.HandleIssueStart))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_issue_http01"}, mw(sh.HandleIssueHTTP01Start))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_issue_dns01"}, mw(sh.HandleIssueDNS01Start))
	b.bot.Handle(&telebot.InlineButton{Unique: "sni_dns01_verify"}, mw(sh.HandleDNS01Verify))

	// Language Handlers
	b.bot.Handle(&telebot.InlineButton{Unique: "lang_en"}, b.userHandler.HandleLanguageSelection)
	b.bot.Handle(&telebot.InlineButton{Unique: "lang_fa"}, b.userHandler.HandleLanguageSelection)
	b.bot.Handle(&telebot.InlineButton{Unique: "change_lang"}, b.userHandler.HandleChangeLanguage)

	// Cancel button
	b.bot.Handle("❌ Cancel", b.handleCancel)

	// Handle all text input for conversations
	b.bot.Handle(telebot.OnText, b.handleTextInput)

}

func (b *Bot) handleTextInput(c telebot.Context) error {
	log := logger.GetLogger()
	userID := c.Sender().ID
	session := b.stateManager.GetSession(userID)

	if session.JustExpired {
		session.JustExpired = false
		log.WithField("user_id", userID).Debug("Session expired, sending reset message")
		lang := b.getUserLang(userID)
		return c.Send(i18n.Get(lang, "SessionExpiredStart"), b.getUserMenu(userID))
	}

	state := session.State
	if state == conversation.StateIdle {
		log.WithField("user_id", userID).Debug("Text input ignored, state is idle")
		return nil
	}

	log.WithFields(map[string]interface{}{
		"user_id": userID,
		"state":   state,
	}).Debug("Routing text input")

	// Look up handler in the state→handler map
	if handler, ok := b.stateHandlers[state]; ok {
		return handler(c)
	}

	// State exists but has no text handler (likely callback-driven)
	log.WithFields(map[string]interface{}{
		"user_id": userID,
		"state":   state,
	}).Debug("No text handler for state, ignoring input")
	return nil
}

// setupStateHandlers builds the state→handler map for text input routing.
// States not in this map are callback-driven and don't need text handlers.
func (b *Bot) setupStateHandlers() {
	b.stateHandlers = map[conversation.ConversationState]stateHandler{
		// Add Node states
		conversation.StateAddNodeName:       b.nodeHandler.HandleAddNodeName,
		conversation.StateAddNodeIP:         b.nodeHandler.HandleAddNodeIP,
		conversation.StateAddNodeCountry:    b.nodeHandler.HandleAddNodeCountry,
		conversation.StateAddNodeDatacenter: b.nodeHandler.HandleAddNodeDatacenter,
		conversation.StateAddNodeAPIPort:    b.nodeHandler.HandleAddNodeAPIPort,

		// Inbound Basic Info
		conversation.StateEditInboundRemark:     b.nodeHandler.HandleRemarkInput,
		conversation.StateEditInboundAddress:    b.nodeHandler.HandleAddressInput,
		conversation.StateEditInboundPort:       b.nodeHandler.HandlePortInput,
		conversation.StateEditInboundLinkFormat: b.nodeHandler.HandleLinkFormatInput,

		// Inbound Transport & Network
		conversation.StateEditInboundNetwork: b.nodeHandler.HandleNetworkInput,
		conversation.StateEditInboundPath:    b.nodeHandler.HandlePathInput,
		conversation.StateEditInboundHost:    b.nodeHandler.HandleHostInput,
		conversation.StateEditInboundMode:    b.nodeHandler.HandleModeInput,
		conversation.StateEditInboundService: b.nodeHandler.HandleServiceInput, // was missing

		// Inbound Security
		conversation.StateEditInboundSecurity: b.nodeHandler.HandleSecurityInput,
		conversation.StateEditInboundSNI:      b.nodeHandler.HandleInboundSNIInput,
		conversation.StateEditInboundALPN:     b.nodeHandler.HandleInboundALPNInput,

		// Add Inbound states
		conversation.StateAddInboundTag:        b.nodeHandler.HandleAddInboundTag,
		conversation.StateAddInboundProtocol:   b.nodeHandler.HandleAddInboundProtocol,
		conversation.StateAddInboundPort:       b.nodeHandler.HandleAddInboundPort,
		conversation.StateAddInboundRemark:     b.nodeHandler.HandleAddInboundRemark,
		conversation.StateAddInboundFormat:     b.nodeHandler.HandleAddInboundFormat,
		conversation.StateAddInboundRealitySNI: b.nodeHandler.HandleAddInboundRealitySNI,
		conversation.StateAddInboundTLSSNI:     b.nodeHandler.HandleAddInboundTLSSNI,
		conversation.StateAddInboundTLSALPN:    b.nodeHandler.HandleAddInboundTLSALPN,

		// Advanced JSON Editing states
		conversation.StateEditInboundAdvancedTLS:       func(c telebot.Context) error { return b.nodeHandler.HandleAdvancedJSONInput(c, "tls") },
		conversation.StateEditInboundAdvancedTransport: func(c telebot.Context) error { return b.nodeHandler.HandleAdvancedJSONInput(c, "transport") },
		conversation.StateEditInboundAdvancedSniffing:  func(c telebot.Context) error { return b.nodeHandler.HandleAdvancedJSONInput(c, "sniffing") },

		// SNI Certificate Management states
		conversation.StateAddSNIName:     b.sniHandler.HandleAddSNIName,
		conversation.StateAddSNIDomain:   b.sniHandler.HandleAddSNIDomain,
		conversation.StateAddSNICert:     b.sniHandler.HandleAddSNICert,
		conversation.StateAddSNIKey:      b.sniHandler.HandleAddSNIKey,
		conversation.StateAddSNIALPN:     b.sniHandler.HandleAddSNIALPN,
		conversation.StateAddSNICertPath: b.sniHandler.HandleAddSNICertPath,
		conversation.StateAddSNIKeyPath:  b.sniHandler.HandleAddSNIKeyPath,
		conversation.StateEditSNIName:    b.sniHandler.HandleEditSNIName,
		conversation.StateEditSNIDomain:  b.sniHandler.HandleEditSNIDomain,
		conversation.StateEditSNICert:    b.sniHandler.HandleEditSNICert,
		conversation.StateEditSNIKey:     b.sniHandler.HandleEditSNIKey,

		// ACME Issuance states
		conversation.StateIssueCertDomain: b.sniHandler.HandleIssueCertDomain,
		conversation.StateIssueCertName:   b.sniHandler.HandleIssueCertName,

		// Admin Data states
		conversation.StateAdminSetDataValue: b.handleAdminSetDataValue,
		conversation.StateAdminExtendCustom: b.handleAdminExtendCustom,

		// Admin Subscription Management
		conversation.StateAdminSetDataLimitValue: b.adminHandler.HandleAdminSetDataLimitValue,
		conversation.StateAdminAddDataValue:      b.adminHandler.HandleAdminAddDataValue,
		conversation.StateAdminSetEndDateValue:   b.adminHandler.HandleAdminSetEndDateValue,

		// Broadcast
		conversation.StateBroadcastMessage: b.handleBroadcastMessage,

		// Subscription Rename
		conversation.StateRenameSubscription: b.subHandler.HandleRenameSave,

		// Add Outbound states
		conversation.StateAddOutboundTag:        b.nodeHandler.HandleAddOutboundTag,
		conversation.StateAddOutboundAddress:    b.nodeHandler.HandleAddOutboundAddress,
		conversation.StateAddOutboundPort:       b.nodeHandler.HandleAddOutboundPort,
		conversation.StateAddOutboundUUID:       b.nodeHandler.HandleAddOutboundUUID,
		conversation.StateAddOutboundUsername:   b.nodeHandler.HandleAddOutboundUsername,
		conversation.StateAddOutboundPassword:   b.nodeHandler.HandleAddOutboundPassword,
		conversation.StateAddOutboundTLSSNI:     b.nodeHandler.HandleAddOutboundTLSSNI,
		conversation.StateAddOutboundRemark:     b.nodeHandler.HandleAddOutboundRemark,
		conversation.StateAddOutboundImportLink: b.nodeHandler.HandleAddOutboundImportLink,

		// Edit Outbound states
		conversation.StateEditOutboundRemark:   b.nodeHandler.HandleEditOutboundRemarkInput,
		conversation.StateEditOutboundAddress:  b.nodeHandler.HandleEditOutboundAddressInput,
		conversation.StateEditOutboundPort:     b.nodeHandler.HandleEditOutboundPortInput,
		conversation.StateEditOutboundUUID:     b.nodeHandler.HandleEditOutboundUUIDInput,
		conversation.StateEditOutboundUsername: b.nodeHandler.HandleEditOutboundUsernameInput,
		conversation.StateEditOutboundPassword: b.nodeHandler.HandleEditOutboundPasswordInput,
		conversation.StateEditOutboundFlow:     b.nodeHandler.HandleEditOutboundFlowInput,
		conversation.StateEditOutboundMethod:   b.nodeHandler.HandleEditOutboundMethodInput,

		// Advanced Outbound States
		conversation.StateEditOutboundLevel: func(c telebot.Context) error {
			return b.nodeHandler.HandleEditOutboundGenericInput(c, conversation.StateEditOutboundLevel)
		},
		conversation.StateEditOutboundEmail: func(c telebot.Context) error {
			return b.nodeHandler.HandleEditOutboundGenericInput(c, conversation.StateEditOutboundEmail)
		},
		conversation.StateEditOutboundInterface: func(c telebot.Context) error {
			return b.nodeHandler.HandleEditOutboundGenericInput(c, conversation.StateEditOutboundInterface)
		},
		conversation.StateEditOutboundMark: func(c telebot.Context) error {
			return b.nodeHandler.HandleEditOutboundGenericInput(c, conversation.StateEditOutboundMark)
		},
		conversation.StateEditOutboundEncryption: b.nodeHandler.HandleEditOutboundEncryptionInput,

		// Add Routing Rule states
		conversation.StateAddRuleTag:     b.nodeHandler.HandleAddRuleTag,
		conversation.StateAddRuleTarget:  b.nodeHandler.HandleAddRuleTarget,
		conversation.StateAddRuleDomains: b.nodeHandler.HandleAddRuleDomains,
		conversation.StateAddRuleGeoIP:   b.nodeHandler.HandleAddRuleGeoIP,
		conversation.StateAddRuleRemark:  b.nodeHandler.HandleAddRuleRemark,

		// Edit Routing Rule states
		conversation.StateEditRuleRemark:  b.nodeHandler.HandleEditRuleRemarkInput,
		conversation.StateEditRuleGeoIP:   b.nodeHandler.HandleEditRuleGeoIPInput,
		conversation.StateEditRulePorts:   b.nodeHandler.HandleEditRulePortsInput,
		conversation.StateEditRuleDomains: b.nodeHandler.HandleEditRuleDomainsInput,
		conversation.StateEditRuleIPs:     b.nodeHandler.HandleEditRuleIPsInput,
		conversation.StateEditRuleUsers:   b.nodeHandler.HandleEditRuleUsersInput,

		// Manual User Management
		conversation.StateManualAddUserEmail: b.handleManualAddUserEmail,
		conversation.StateManualGetLinkEmail: b.handleManualGetLinkEmail,
		conversation.StateManualGetLinkUUID:  b.handleManualGetLinkUUID,
	}
}

func (b *Bot) handleCancel(c telebot.Context) error {
	log := logger.GetLogger()
	userID := c.Sender().ID
	log.WithField("user_id", userID).Debug("User cancelled conversation")
	b.stateManager.ResetSession(userID)
	lang := b.getUserLang(userID)

	if b.adminMiddleware.IsAdmin(userID) {
		return c.Send(i18n.Get(lang, "Cancelled"), keyboards.AdminMenu())
	}
	return c.Send(i18n.Get(lang, "Cancelled"), b.getUserMenu(userID))
}

func (b *Bot) handleBackToUserMenu(c telebot.Context) error {
	userID := c.Sender().ID
	b.stateManager.ResetSession(userID)
	lang := b.getUserLang(userID)
	return c.Send(i18n.Get(lang, "MainMenuText"), b.getUserMenu(userID))
}

// === Admin Helpers ===
func (b *Bot) handleAdminSetDataStart(c telebot.Context) error {
	parts := strings.Split(c.Data(), ":")
	if len(parts) < 2 {
		return nil
	}
	b.stateManager.StartConversation(c.Sender().ID, conversation.StateAdminSetDataValue)
	b.stateManager.SetData(c.Sender().ID, "target_sub_id", parts[1])
	return c.Send(fmt.Sprintf("✏️ *Set Data GB for Sub #%s*", parts[1]), telebot.ModeMarkdown, keyboards.Cancel())
}
func (b *Bot) handleAdminSetDataValue(c telebot.Context) error {
	gb, err := strconv.ParseFloat(c.Text(), 64)
	if err != nil || gb < 0 {
		return c.Send("❌ Invalid value. Please enter data limit in GB (e.g. 50):", keyboards.Cancel())
	}
	targetID, _ := strconv.Atoi(b.stateManager.GetStringData(c.Sender().ID, "target_sub_id"))
	b.stateManager.ResetSession(c.Sender().ID)
	return b.adminHandler.HandleSetDataWizard(c, uint(targetID), gb)
}
func (b *Bot) handleAdminExtendCustomStart(c telebot.Context) error {
	parts := strings.Split(c.Data(), ":")
	if len(parts) < 2 {
		return nil
	}
	b.stateManager.StartConversation(c.Sender().ID, conversation.StateAdminExtendCustom)
	b.stateManager.SetData(c.Sender().ID, "target_sub_id", parts[1])
	return c.Send(fmt.Sprintf("📅 *Extend Sub #%s (Days)*", parts[1]), telebot.ModeMarkdown, keyboards.Cancel())
}
func (b *Bot) handleAdminExtendCustom(c telebot.Context) error {
	days, err := strconv.Atoi(c.Text())
	if err != nil || days <= 0 {
		return c.Send("❌ Invalid value. Please enter a positive number of days (e.g. 30):", keyboards.Cancel())
	}
	targetID, _ := strconv.Atoi(b.stateManager.GetStringData(c.Sender().ID, "target_sub_id"))
	b.stateManager.ResetSession(c.Sender().ID)
	return b.adminHandler.HandleAdminExtendCustomValue(c, uint(targetID), days)
}
func (b *Bot) handleBroadcastStart(c telebot.Context) error {
	b.stateManager.StartConversation(c.Sender().ID, conversation.StateBroadcastMessage)
	return c.Send("📢 *Broadcast Message*", telebot.ModeMarkdown, keyboards.Cancel())
}
func (b *Bot) handleBroadcastMessage(c telebot.Context) error {
	msg := c.Text()
	b.stateManager.ResetSession(c.Sender().ID)
	return b.adminHandler.HandleBroadcastWithMessage(c, msg)
}

// handleEmojiID echoes the custom_emoji_id of any animated emoji in the message
// (or the replied-to message), so they can be wired into bot messages. Admin-only.
func (b *Bot) handleEmojiID(c telebot.Context) error {
	m := c.Message()
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	add := func(ents []telebot.MessageEntity) {
		for _, e := range ents {
			if e.Type == telebot.EntityCustomEmoji && e.CustomEmoji != "" && !seen[e.CustomEmoji] {
				seen[e.CustomEmoji] = true
				ids = append(ids, e.CustomEmoji)
			}
		}
	}
	add(m.Entities)
	add(m.CaptionEntities)
	if m.ReplyTo != nil {
		add(m.ReplyTo.Entities)
		add(m.ReplyTo.CaptionEntities)
	}
	if len(ids) == 0 {
		return c.Send("Send /emojiid with custom (animated) emoji in the same message, or reply to a message that has them.")
	}

	alt := map[string]string{}
	if stickers, err := b.bot.CustomEmojiStickers(ids); err == nil {
		for _, s := range stickers {
			alt[s.CustomEmoji] = s.Emoji
		}
	}

	var sb strings.Builder
	for _, id := range ids {
		a := alt[id]
		if a == "" {
			a = "?"
		}
		sb.WriteString(fmt.Sprintf("%s  →  %s\n", a, id))
	}
	return c.Send(sb.String())
}

func (b *Bot) registerAdminCommands() {
	mw := b.adminMiddleware.RequireAdmin
	h := b.adminHandler
	nh := b.nodeHandler

	b.bot.Handle("/admin", b.wrapAdminWithMenu(h.HandleAdmin))
	b.bot.Handle("/nodes", mw(nh.HandleNodes))
	b.bot.Handle("/getuser", mw(h.HandleGetUser))
	b.bot.Handle("/getsub", mw(h.HandleQuickGetSub))
	b.bot.Handle("/subs", mw(h.HandleListAllSubs))
	b.bot.Handle("/emojiid", mw(b.handleEmojiID))

	// Additional admin commands referenced in help
	b.bot.Handle("/stats", mw(h.HandleStats))
	b.bot.Handle("/xray_sync", mw(h.HandleXraySync))
	b.bot.Handle("/users", mw(h.HandleUsers))
	b.bot.Handle("/assign_sub", mw(h.HandleAssignSub))

	// Phase 5: New admin commands
	b.bot.Handle("/system", mw(h.HandleSystem))
	b.bot.Handle("/audit", mw(h.HandleAudit))
	b.bot.Handle("/export", mw(h.HandleExport))
	b.bot.Handle("/backup", mw(h.HandleBackup))
	b.bot.Handle("/settings", mw(h.HandleSettings))

	if b.maintenanceHandler != nil {
		b.bot.Handle("/maintenance", mw(b.maintenanceHandler.HandleMaintenance))
	}

	// Settings category callback and back button
	b.bot.Handle(&telebot.InlineButton{Unique: "settings_cat"}, mw(h.HandleSettingsCategory))
	b.bot.Handle(&telebot.InlineButton{Unique: "settings_back"}, mw(h.HandleSettings))

	// Node Callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_nodes"}, mw(nh.HandleNodes))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_view"}, mw(nh.HandleNodeView))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_add"}, mw(nh.HandleAddNodeStart))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_delete_ask"}, mw(nh.HandleDeleteNodeAsk))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_inbounds"}, mw(nh.HandleInbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_discover"}, mw(nh.HandleDiscoverInbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_sync"}, mw(nh.HandleSyncInbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_export"}, mw(nh.HandleExportConfig))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_stats"}, mw(nh.HandleNodeStats))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_view"}, mw(nh.HandleInboundView))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_add"}, mw(nh.HandleAddInboundStart))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_delete"}, mw(nh.HandleDeleteInbound))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_stats"}, mw(nh.HandleInboundStats))

	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_edit"}, mw(nh.HandleEditInbound))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_resync"}, mw(nh.HandleResyncInbound))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_edit_basic"}, mw(nh.HandleEditBasicInfo))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_edit_network"}, mw(nh.HandleEditNetworkSettings))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_edit_security"}, mw(nh.HandleEditSecuritySettings))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_edit_link"}, mw(nh.HandleSetLinkFormat))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_apply_sni"}, mw(nh.HandleApplySNI))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_advanced"}, mw(nh.HandleAdvancedSettings))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_adv_tls"}, mw(nh.HandleAdvancedTLS))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_adv_transport"}, mw(nh.HandleAdvancedTransport))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_adv_sniffing"}, mw(nh.HandleAdvancedSniffing))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_remark"}, mw(nh.HandleSetRemark))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_address"}, mw(nh.HandleSetAddress))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_port"}, mw(nh.HandleSetPort))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_network"}, mw(nh.HandleSetNetwork))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_path"}, mw(nh.HandleSetPath))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_host"}, mw(nh.HandleSetHost))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_security"}, mw(nh.HandleSetSecurity))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_sni"}, mw(nh.HandleSetSNI))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_fp"}, mw(nh.HandleSetFingerprint))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_set_fp_val"}, mw(nh.HandleSetFingerprintValue))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_mode"}, mw(nh.HandleSetMode))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_inbound_set_certs"}, mw(nh.HandleSetCerts))

	// Add Inbound Wizard - Network & XHTTP Callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_net_tcp"}, mw(nh.HandleInboundNetworkCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_net_ws"}, mw(nh.HandleInboundNetworkCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_net_grpc"}, mw(nh.HandleInboundNetworkCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_net_xhttp"}, mw(nh.HandleInboundNetworkCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_xhttp_packet-up"}, mw(nh.HandleXHTTPModeCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_xhttp_stream-up"}, mw(nh.HandleXHTTPModeCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_xhttp_stream-one"}, mw(nh.HandleXHTTPModeCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_xhttp_auto"}, mw(nh.HandleXHTTPModeCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_sec_reality"}, mw(nh.HandleInboundSecurityCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_sec_none"}, mw(nh.HandleInboundSecurityCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_sec_tls"}, mw(nh.HandleInboundSecurityCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_use_sni"}, mw(nh.HandleUseSNI))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_tls_manual"}, mw(nh.HandleTLSManual))
	b.bot.Handle(&telebot.InlineButton{Unique: "inbound_use_format"}, mw(nh.HandleUseSuggestedFormat))

	// Outbound Callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_outbounds"}, mw(nh.HandleOutbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_view"}, mw(nh.HandleOutboundView))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_add"}, mw(nh.HandleAddOutboundStart))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_delete"}, mw(nh.HandleDeleteOutbound))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_import"}, mw(nh.HandleAddOutboundImport))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_export"}, mw(nh.HandleOutboundExportLink))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_test"}, mw(nh.HandleOutboundTestConnectivity))

	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_discover"}, mw(nh.HandleDiscoverOutbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_sync"}, mw(nh.HandleSyncOutbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_edit"}, mw(nh.HandleOutboundEdit))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_remark"}, mw(nh.HandleEditOutboundRemark))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_address"}, mw(nh.HandleEditOutboundAddress))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_port"}, mw(nh.HandleEditOutboundPort))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_protocol"}, mw(nh.HandleEditOutboundProtocol))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_proto_save"}, mw(nh.HandleEditOutboundProtocolSave))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_uuid"}, mw(nh.HandleEditOutboundUUID))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_username"}, mw(nh.HandleEditOutboundUsername))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_password"}, mw(nh.HandleEditOutboundPassword))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_flow"}, mw(nh.HandleEditOutboundFlow))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_method"}, mw(nh.HandleEditOutboundMethod))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_network"}, mw(nh.HandleEditOutboundNetwork))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_net_save"}, mw(nh.HandleEditOutboundNetworkSave))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_security"}, mw(nh.HandleEditOutboundSecurity))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_sec_save"}, mw(nh.HandleEditOutboundSecuritySave))

	// Advanced Outbound Callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_edit_advanced"}, mw(nh.HandleOutboundEditAdvanced))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_outbound_edit_sockopt"}, mw(nh.HandleOutboundEditSockopt))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_level"}, mw(nh.HandleEditOutboundLevel))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_email"}, mw(nh.HandleEditOutboundEmail))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_encryption"}, mw(nh.HandleEditOutboundEncryption))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_ivcheck"}, mw(nh.HandleEditOutboundIVCheck))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_uot"}, mw(nh.HandleEditOutboundUoT))

	// Sockopt
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_interface"}, mw(nh.HandleEditOutboundSockoptInterface))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_outbound_mark"}, mw(nh.HandleEditOutboundSockoptMark))
	b.bot.Handle(&telebot.InlineButton{Unique: "toggle_outbound_tfo"}, mw(nh.HandleToggleOutboundTFO))
	b.bot.Handle(&telebot.InlineButton{Unique: "toggle_outbound_mptcp"}, mw(nh.HandleToggleOutboundMPTCP))
	b.bot.Handle(&telebot.InlineButton{Unique: "set_outbound_tproxy"}, mw(nh.HandleSetOutboundTProxy))

	// Outbound Wizard Callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_freedom"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_blackhole"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_vless"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_vmess"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_trojan"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_socks"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_http"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_proto_shadowsocks"}, mw(nh.HandleOutboundProtocolCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_ss_method"}, mw(nh.HandleAddOutboundMethod))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_sec_none"}, mw(nh.HandleOutboundSecurityCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_sec_tls"}, mw(nh.HandleOutboundSecurityCallback))
	b.bot.Handle(&telebot.InlineButton{Unique: "outbound_sec_reality"}, mw(nh.HandleOutboundSecurityCallback))

	// Routing Rule Callbacks
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_node_routing"}, mw(nh.HandleRoutingRules))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_rule_view"}, mw(nh.HandleRoutingRuleView))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_rule_add"}, mw(nh.HandleAddRuleStart))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_rule_delete"}, mw(nh.HandleDeleteRoutingRule))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_rule_sync"}, mw(nh.HandleSyncRoutingRules))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_rule_edit"}, mw(nh.HandleRuleEdit))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_remark"}, mw(nh.HandleEditRuleRemark))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_toggle"}, mw(nh.HandleToggleRule))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_priority"}, mw(nh.HandleEditRulePriority))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_priority_set"}, mw(nh.HandleEditRulePrioritySet))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_geoip"}, mw(nh.HandleEditRuleGeoIP))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_ports"}, mw(nh.HandleEditRulePorts))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_networks"}, mw(nh.HandleEditRuleNetworks))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_net_toggle"}, mw(nh.HandleEditRuleNetworkToggle))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_net_set"}, mw(nh.HandleEditRuleNetworkSet))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_protocol"}, mw(nh.HandleEditRuleProtocol))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_proto_toggle"}, mw(nh.HandleEditRuleProtocolToggle))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_proto_clear"}, mw(nh.HandleEditRuleProtocolClear))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_target"}, mw(nh.HandleEditRuleTarget))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_target_save"}, mw(nh.HandleEditRuleTargetSave))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_tag"}, mw(nh.HandleEditRuleTag))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_domains"}, mw(nh.HandleEditRuleDomains))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_ips"}, mw(nh.HandleEditRuleIPs))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_inbounds"}, mw(nh.HandleEditRuleInbounds))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_inbound_toggle"}, mw(nh.HandleEditRuleInboundToggle))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_inbound_clear"}, mw(nh.HandleEditRuleInboundClear))
	b.bot.Handle(&telebot.InlineButton{Unique: "edit_rule_users"}, mw(nh.HandleEditRuleUsers))

	b.bot.Handle(&telebot.InlineButton{Unique: "back_admin"}, mw(h.HandleAdmin))

	// User Management
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_users_page"}, mw(h.HandleUsers))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_user_subs"}, mw(h.HandleUserSubscriptions))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_user_ban"}, mw(h.HandleUserBanToggle))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_user_unban"}, mw(h.HandleUserBanToggle))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_getuser"}, mw(h.HandleGetUser))

	b.bot.Handle(&telebot.InlineButton{Unique: "noop"}, func(c telebot.Context) error {
		return c.Respond()
	})

	// Sub Management
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_manage_sub"}, mw(h.HandleManageSubscription))

	// Actions routed to Handler
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_pause"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_resume"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_revoke_ask"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_revoke_confirm"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_reset_data"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_ext"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_link"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_regen_ask"}, mw(h.HandleSubscriptionAction))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_regen_confirm"}, mw(h.HandleSubscriptionAction))

	// Actions routed to Wizard (Bot)
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_setdata"}, mw(b.handleAdminSetDataStart))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_ext_custom"}, mw(b.handleAdminExtendCustomStart))

	// Subscription Delete
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_delete_ask"}, mw(h.HandleDeleteSubscriptionAsk))
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_sub_delete_confirm"}, mw(h.HandleDeleteSubscriptionConfirm))

	// All Subscriptions List Pagination
	b.bot.Handle(&telebot.InlineButton{Unique: "admin_subs_page"}, mw(h.HandleListAllSubs))

	b.registerCallbackHandlers()
}

func (b *Bot) wrapAdminWithMenu(handler telebot.HandlerFunc) telebot.HandlerFunc {
	return b.adminMiddleware.RequireAdmin(func(c telebot.Context) error {
		if err := handler(c); err != nil {
			return err
		}
		lang := b.getUserLang(c.Sender().ID)
		return c.Send(i18n.Get(lang, "AdminPanelText"), keyboards.AdminMenu())
	})
}

func (b *Bot) registerCallbackHandlers() {
	// Subscription Management
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_select"}, func(c telebot.Context) error {
		return b.subHandler.HandleSubscriptionDetail(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_config"}, func(c telebot.Context) error {
		return b.subHandler.HandleGetConfig(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_config_all"}, func(c telebot.Context) error {
		return b.subHandler.HandleGetConfigAll(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "srv_cfg"}, func(c telebot.Context) error {
		a := c.Args()
		if len(a) < 2 {
			return nil
		}
		sid, _ := strconv.ParseUint(a[0], 10, 32)
		idx, _ := strconv.ParseUint(a[1], 10, 32)
		return b.subHandler.HandleServerConfig(c, uint(sid), uint(idx))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_qr"}, func(c telebot.Context) error {
		return b.subHandler.HandleGetQR(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_link"}, func(c telebot.Context) error {
		return b.subHandler.HandleSubLink(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_link_regen"}, func(c telebot.Context) error {
		return b.subHandler.HandleRegenerateLink(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_list"}, b.subHandler.HandleMySubscriptions)

	// WireGuard device management
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_devices"}, func(c telebot.Context) error {
		return b.subHandler.HandleDevices(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "wg_dev_addpick"}, func(c telebot.Context) error {
		return b.subHandler.HandleAddDevicePicker(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "wg_dev_add"}, func(c telebot.Context) error {
		a := c.Args()
		if len(a) < 1 {
			return nil
		}
		sid, _ := strconv.ParseUint(a[0], 10, 32)
		var iid uint64
		if len(a) > 1 {
			iid, _ = strconv.ParseUint(a[1], 10, 32)
		}
		return b.subHandler.HandleAddDevice(c, uint(sid), uint(iid))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "wg_dev_rotate"}, func(c telebot.Context) error {
		a := c.Args()
		if len(a) < 2 {
			return nil
		}
		sid, _ := strconv.ParseUint(a[0], 10, 32)
		did, _ := strconv.ParseUint(a[1], 10, 32)
		return b.subHandler.HandleRotateDevice(c, uint(sid), uint(did))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "wg_dev_remove"}, func(c telebot.Context) error {
		a := c.Args()
		if len(a) < 2 {
			return nil
		}
		sid, _ := strconv.ParseUint(a[0], 10, 32)
		did, _ := strconv.ParseUint(a[1], 10, 32)
		return b.subHandler.HandleRemoveDevice(c, uint(sid), uint(did))
	})

	// Subscription Actions (Rename, Regenerate)
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_rename_ask"}, func(c telebot.Context) error {
		return b.subHandler.HandleRenameAsk(c, parseID(c.Data()))
	})
	// Regenerate Flow
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_regen_ask"}, func(c telebot.Context) error {
		return b.subHandler.HandleRegenerateAsk(c, parseID(c.Data()))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "sub_regen_confirm"}, func(c telebot.Context) error {
		return b.subHandler.HandleRegenerateConfirm(c, parseID(c.Data()))
	})

	// Utility
	b.bot.Handle(&telebot.InlineButton{Unique: "cancel"}, func(c telebot.Context) error {
		userID := c.Sender().ID
		b.stateManager.ResetSession(userID)
		lang := b.getUserLang(userID)
		c.Respond(&telebot.CallbackResponse{Text: i18n.Get(lang, "Cancelled")})
		if b.adminMiddleware.IsAdmin(userID) {
			return c.Send(i18n.Get(lang, "Cancelled"), keyboards.AdminMenu())
		}
		return c.Send(i18n.Get(lang, "Cancelled"), b.getUserMenu(userID))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "back_menu"}, func(c telebot.Context) error {
		userID := c.Sender().ID
		c.Respond()
		lang := b.getUserLang(userID)
		if b.adminMiddleware.IsAdmin(userID) {
			return c.Send(i18n.Get(lang, "AdminMenuText"), keyboards.AdminMenu())
		}
		return c.Send(i18n.Get(lang, "MainMenuText"), b.getUserMenu(userID))
	})
	b.bot.Handle(&telebot.InlineButton{Unique: "delete_msg"}, func(c telebot.Context) error {
		c.Respond()
		return c.Delete()
	})
}

func parseID(data string) uint {
	var id uint
	if n, _ := fmt.Sscanf(data, "%d", &id); n != 1 || id == 0 {
		return 0
	}
	return id
}

func (b *Bot) Start() {
	log := logger.GetLogger()
	log.WithField("admin_ids", b.adminIDs).Info("Telegram bot starting...")
	go func() {
		cleanupTicker := time.NewTicker(5 * time.Minute)
		settingTicker := time.NewTicker(30 * time.Second)
		defer cleanupTicker.Stop()
		defer settingTicker.Stop()
		for {
			select {
			case <-cleanupTicker.C:
				b.stateManager.CleanupExpired()
			case <-settingTicker.C:
				if val, err := b.settingUsecase.GetByKey(context.Background(), "telegram_bot_enabled"); err == nil {
					b.enabled.Store(val != "false")
				}
			case <-b.stopCh:
				return
			}
		}
	}()
	b.bot.Start()
}

func (b *Bot) Stop() {
	log := logger.GetLogger()
	log.Info("Telegram bot stopping...")
	close(b.stopCh)
	b.bot.Stop()
	log.Info("Telegram bot stopped")
}

// NotifyAdmin sends a message to all configured admins
func (b *Bot) NotifyAdmin(message string) error {
	log := logger.GetLogger()
	// Send to all admins
	failed := 0
	for _, adminID := range b.adminIDs {
		// Telebot Send requires a Recipient. Check if we have one or create ad-hoc.
		// telebot.Chat satisfies Recipient
		chat := &telebot.Chat{ID: adminID}
		_, err := b.bot.Send(chat, message, telebot.ModeMarkdown)
		if err != nil {
			log.Errorf("Failed to notify admin %d: %v", adminID, err)
			failed++
		}
	}

	if failed == len(b.adminIDs) && len(b.adminIDs) > 0 {
		return fmt.Errorf("failed to notify any admins")
	}
	return nil
}

func (b *Bot) registerManualUserCommands() {
	mw := b.adminMiddleware.RequireAdmin
	h := b.adminHandler

	b.bot.Handle("/adduser", mw(h.HandleManualAddUserStart))
	b.bot.Handle("/getlink", mw(h.HandleManualGetLinkStart))

	// Callbacks for Add User
	b.bot.Handle(&telebot.InlineButton{Unique: "manual_node_add"}, mw(h.HandleManualAddUserNodeSelect))
	b.bot.Handle(&telebot.InlineButton{Unique: "manual_inbound_add"}, mw(b.handleManualInboundAddSelect))

	// Callbacks for Get Link
	b.bot.Handle(&telebot.InlineButton{Unique: "manual_node_link"}, mw(h.HandleManualGetLinkNodeSelect))
	b.bot.Handle(&telebot.InlineButton{Unique: "manual_inbound_link"}, mw(b.handleManualInboundLinkSelect))
}

func (b *Bot) handleManualInboundAddSelect(c telebot.Context) error {
	parts := strings.Split(c.Data(), ":")
	if len(parts) < 2 {
		return nil
	}
	nodeID, inboundTag := parts[0], parts[1]
	userID := c.Sender().ID

	b.stateManager.StartConversation(userID, conversation.StateManualAddUserEmail)
	b.stateManager.SetData(userID, "manual_node_id", nodeID)
	b.stateManager.SetData(userID, "manual_inbound_tag", inboundTag)

	return b.adminHandler.HandleManualAddUserAskEmail(c, nodeID, inboundTag)
}

func (b *Bot) handleManualAddUserEmail(c telebot.Context) error {
	userID := c.Sender().ID
	email := c.Text()

	nodeID := b.stateManager.GetStringData(userID, "manual_node_id")
	inboundTag := b.stateManager.GetStringData(userID, "manual_inbound_tag")
	b.stateManager.ResetSession(userID)

	return b.adminHandler.HandleManualAddUserExecute(c, nodeID, inboundTag, email)
}

func (b *Bot) handleManualInboundLinkSelect(c telebot.Context) error {
	parts := strings.Split(c.Data(), ":")
	if len(parts) < 2 {
		return nil
	}
	nodeID, inboundTag := parts[0], parts[1]
	userID := c.Sender().ID

	b.stateManager.StartConversation(userID, conversation.StateManualGetLinkEmail)
	b.stateManager.SetData(userID, "manual_node_id", nodeID)
	b.stateManager.SetData(userID, "manual_inbound_tag", inboundTag)

	return b.adminHandler.HandleManualGetLinkAskEmail(c, inboundTag)
}

func (b *Bot) handleManualGetLinkEmail(c telebot.Context) error {
	userID := c.Sender().ID
	email := c.Text()

	b.stateManager.SetData(userID, "manual_email", email)
	b.stateManager.SetState(userID, conversation.StateManualGetLinkUUID)

	return b.adminHandler.HandleManualGetLinkAskUUID(c, email)
}

func (b *Bot) handleManualGetLinkUUID(c telebot.Context) error {
	userID := c.Sender().ID
	uuid := c.Text()

	nodeID := b.stateManager.GetStringData(userID, "manual_node_id")
	inboundTag := b.stateManager.GetStringData(userID, "manual_inbound_tag")
	email := b.stateManager.GetStringData(userID, "manual_email")
	b.stateManager.ResetSession(userID)

	return b.adminHandler.HandleManualGetLinkExecute(c, nodeID, inboundTag, email, uuid)
}
