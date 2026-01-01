package telegram

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/tgctx"
	"gopkg.in/telebot.v3"
)

// titleCase capitalizes the first letter of a string
func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// HandleSettings shows settings categories as inline keyboard buttons
func (h *Handler) HandleSettings(c telebot.Context) error {
	if h.settingUC == nil {
		return c.Send("Settings module is not configured.")
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	grouped, err := h.settingUC.GetAll(ctx)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to load settings: %v", err))
	}

	if len(grouped) == 0 {
		return c.Send("⚙️ *Settings*\n\n_No settings found._", telebot.ModeMarkdown)
	}

	// Sort category names for consistent ordering
	categories := make([]string, 0, len(grouped))
	for cat := range grouped {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	// Build inline keyboard with one button per category
	kb := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, cat := range categories {
		count := len(grouped[cat])
		label := fmt.Sprintf("⚙️ %s (%d)", titleCase(cat), count)
		rows = append(rows, kb.Row(kb.Data(label, "settings_cat", cat)))
	}
	kb.Inline(rows...)

	return c.Send("⚙️ *Settings*\n\nSelect a category to view:", telebot.ModeMarkdown, kb)
}

// HandleSettingsCategory shows settings within a selected category
func (h *Handler) HandleSettingsCategory(c telebot.Context) error {
	if h.settingUC == nil {
		return c.Send("Settings module is not configured.")
	}

	category := c.Data()
	if category == "" {
		return c.Respond(&telebot.CallbackResponse{Text: "Invalid category"})
	}

	ctx, cancel := tgctx.FromTelebot(c)
	defer cancel()

	grouped, err := h.settingUC.GetAll(ctx)
	if err != nil {
		return c.Send(fmt.Sprintf("Failed to load settings: %v", err))
	}

	settings, ok := grouped[category]
	if !ok || len(settings) == 0 {
		return c.Send(fmt.Sprintf("⚙️ *%s Settings*\n\n_No settings in this category._", titleCase(category)), telebot.ModeMarkdown)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⚙️ *%s Settings*\n\n", titleCase(category)))

	for _, s := range settings {
		label := s.Label
		if label == "" {
			label = s.Key
		}

		value := s.Value
		if value == "" {
			value = "_empty_"
		}

		// Truncate long values for readability
		if len(value) > 80 {
			value = value[:77] + "..."
		}

		sb.WriteString(fmt.Sprintf("*%s*\n`%s`\n\n", label, value))
	}

	// Back button
	kb := &telebot.ReplyMarkup{}
	kb.Inline(
		kb.Row(kb.Data("🔙 Back to Categories", "settings_back")),
	)

	return c.Send(sb.String(), telebot.ModeMarkdown, kb)
}
