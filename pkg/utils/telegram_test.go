package utils

import (
	"strings"
	"testing"
)

// Telegram Markdown is broken by unescaped _ * ` [ < — escaping these
// prevents user input from accidentally re-styling or truncating a message.
func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"under_score", "under\\_score"},
		{"*bold*", "\\*bold\\*"},
		{"`code`", "\\`code\\`"},
		{"[link]", "\\[link]"},
		{"<tag>", "\\<tag>"},
		{"_*`[<", "\\_\\*\\`\\[\\<"},
	}
	for _, tt := range tests {
		if got := EscapeMarkdown(tt.in); got != tt.want {
			t.Errorf("EscapeMarkdown(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEscapeMarkdown_LeavesOtherCharsAlone(t *testing.T) {
	got := EscapeMarkdown("hello world.,!?")
	if strings.ContainsRune(got, '\\') {
		t.Errorf("non-special chars should not be escaped, got %q", got)
	}
}
