package keyboards

import (
	"fmt"
	"strconv"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	"gopkg.in/telebot.v3"
)

// BackBtn returns a "🔙 Back" inline button bound to the given unique + data.
func BackBtn(kb *telebot.ReplyMarkup, unique string, data ...string) telebot.Btn {
	return kb.Data("🔙 Back", unique, data...)
}

// BackRow wraps BackBtn in a single-button row, ready to drop into kb.Inline(...).
func BackRow(kb *telebot.ReplyMarkup, unique string, data ...string) telebot.Row {
	return kb.Row(BackBtn(kb, unique, data...))
}

// BackRowID is the uint convenience: formats the id as the callback data.
// Kills the `fmt.Sprintf("%d", id)` boilerplate at every back-button site.
func BackRowID(kb *telebot.ReplyMarkup, unique string, id uint) telebot.Row {
	return kb.Row(kb.Data("🔙 Back", unique, strconv.FormatUint(uint64(id), 10)))
}

// PageNav builds a pagination row "◀️ 📄 p/total ▶️", emitting arrows only when
// they would advance. The middle counter uses callback unique "noop" by
// convention (existing handlers register a no-op for it).
func PageNav(kb *telebot.ReplyMarkup, unique string, page, total int) telebot.Row {
	var btns []telebot.Btn
	if page > 1 {
		btns = append(btns, kb.Data("◀️", unique, strconv.Itoa(page-1)))
	}
	btns = append(btns, kb.Data(fmt.Sprintf("📄 %d/%d", page, total), "noop"))
	if page < total {
		btns = append(btns, kb.Data("▶️", unique, strconv.Itoa(page+1)))
	}
	return kb.Row(btns...)
}

// MainMenu returns the main menu keyboard for regular users.
// When showTrial is true, the "Get Free Trial" button is included.
func MainMenu(lang string, showTrial bool) *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{
		ResizeKeyboard: true,
	}

	rows := []telebot.Row{
		menu.Row(menu.Text(i18n.Get(lang, "BtnBuyVPN"))),
	}
	if showTrial {
		rows = append(rows, menu.Row(menu.Text(i18n.Get(lang, "BtnFreeTrial"))))
	}
	rows = append(rows,
		menu.Row(
			menu.Text(i18n.Get(lang, "BtnMySubs")),
			menu.Text(i18n.Get(lang, "BtnProfile")),
		),
		menu.Row(
			menu.Text(i18n.Get(lang, "BtnPayments")),
			menu.Text(i18n.Get(lang, "BtnSupport")),
		),
		menu.Row(
			menu.Text(i18n.Get(lang, "BtnGuide")),
			menu.Text(i18n.Get(lang, "BtnSettings")),
		),
	)

	menu.Reply(rows...)
	return menu
}

// AdminMenu returns the admin panel keyboard
func AdminMenu() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{
		ResizeKeyboard: true,
	}

	menu.Reply(
		menu.Row(
			menu.Text("📈 Admin Stats"),
			menu.Text("👥 Users"),
		),
		// New Row for Server Management
		menu.Row(
			menu.Text("🖥 Servers"),
			menu.Text("📋 Manage Plans"),
		),
		menu.Row(
			menu.Text("💳 Pending Payments"),
			menu.Text("📢 Broadcast"),
		),
		menu.Row(
			menu.Text("📋 Accounts"),
			menu.Text("🔐 TLS Certs"),
		),
		menu.Row(
			menu.Text("❓ Admin Help"),
		),
		menu.Row(
			menu.Text("🔙 User Menu"),
		),
	)

	return menu
}

// Cancel returns a cancel button keyboard
func Cancel() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{
		ResizeKeyboard: true,
	}

	menu.Reply(
		menu.Row(menu.Text("❌ Cancel")),
	)

	return menu
}

// CancelInline returns an inline cancel button (for use with c.Edit)
func CancelInline() *telebot.ReplyMarkup {
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("❌ Cancel", "cancel")),
	)
	return kb
}

// Confirm returns a confirm/cancel inline keyboard
func Confirm(confirmData, cancelData string) *telebot.ReplyMarkup {
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("✅ Confirm", confirmData),
			kb.Data("❌ Cancel", cancelData),
		),
	)
	return kb
}

// ProductTypes returns inline keyboard for product type selection
func ProductTypes() *telebot.ReplyMarkup {
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("🌐 Xray", "product_xray"),
			kb.Data("🔒 OpenVPN", "product_openvpn"),
		),
		kb.Row(
			kb.Data("⚡ WireGuard", "product_wireguard"),
		),
		kb.Row(
			kb.Data("❌ Cancel", "cancel"),
		),
	)
	return kb
}

// BackToMenu returns a back to menu inline button
func BackToMenu() *telebot.ReplyMarkup {
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔙 Back to Menu", "back_menu")),
	)
	return kb
}

// GetMenu returns AdminMenu for admins, MainMenu otherwise
func GetMenu(isAdmin bool, lang string, showTrial bool) *telebot.ReplyMarkup {
	if isAdmin {
		return AdminMenu()
	}
	return MainMenu(lang, showTrial)
}

// LanguageSelector returns inline keyboard for choosing language
func LanguageSelector() *telebot.ReplyMarkup {
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(
			kb.Data("🇬🇧 English", "lang_en"),
			kb.Data("🇮🇷 فارسی", "lang_fa"),
		),
	)
	return kb
}

// UserSettingsMenu returns inline keyboard for user settings
func UserSettingsMenu(userLang string) *telebot.ReplyMarkup {
	kb := &telebot.ReplyMarkup{}

	langBtnText := i18n.Get(userLang, "BtnChangeLang")

	kb.Inline(
		kb.Row(
			kb.Data(langBtnText, "change_lang"),
		),
		kb.Row(
			kb.Data(i18n.Get(userLang, "BtnCancel"), "cancel"),
		),
	)
	return kb
}
