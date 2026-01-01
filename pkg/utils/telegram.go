package utils

import (
	"strconv"
	"strings"

	"gopkg.in/telebot.v3"
)

// AnswerCallback stops the loading spinner on a button. Safe to call on non-callback contexts.
func AnswerCallback(c telebot.Context, text ...string) error {
	if c.Callback() == nil {
		return nil
	}

	resp := &telebot.CallbackResponse{}
	if len(text) > 0 {
		resp.Text = text[0]
		resp.ShowAlert = false // toast by default; caller can use AnswerCallbackWithAlert for a popup
	}

	return c.Respond(resp)
}

// AnswerCallbackWithAlert is a specific helper for showing popup alerts
func AnswerCallbackWithAlert(c telebot.Context, text string) error {
	if c.Callback() == nil {
		return nil
	}

	return c.Respond(&telebot.CallbackResponse{
		Text:      text,
		ShowAlert: true,
	})
}

// EscapeMarkdown escapes special characters for Telegram Markdown mode.
// Characters escaped: _ * ` [ <
func EscapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"`", "\\`",
		"[", "\\[",
		"<", "\\<",
	)
	return replacer.Replace(text)
}

// CallbackID parses c.Data() as a uint32. Returns 0 on parse error — matches
// the dominant `id, _ := strconv.ParseUint(c.Data(), 10, 32)` pattern.
func CallbackID(c telebot.Context) uint {
	v, _ := strconv.ParseUint(c.Data(), 10, 32)
	return uint(v)
}

// EditOrSend edits the current callback message when invoked from a callback,
// otherwise sends a new message. opts pass straight through to telebot
// (parse mode, reply markup, etc.).
func EditOrSend(c telebot.Context, what interface{}, opts ...interface{}) error {
	if c.Callback() != nil {
		return c.Edit(what, opts...)
	}
	return c.Send(what, opts...)
}

// SplitCallback splits c.Data() by sep and requires at least `want` parts.
// On failure it answers the callback with "Invalid data" and returns ok=false.
func SplitCallback(c telebot.Context, sep string, want int) ([]string, bool) {
	parts := strings.Split(c.Data(), sep)
	if len(parts) < want {
		_ = AnswerCallback(c, "Invalid data")
		return nil, false
	}
	return parts, true
}
