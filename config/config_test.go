package config

import "testing"

func TestCleanBasePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"/", ""},
		{"/panel", "/panel"},
		{"panel", "/panel"},
		{"/panel/", "/panel"},
		{"//panel//sub//", "/panel/sub"},
		{"/panel/../admin", "/admin"},
	}
	for _, tt := range tests {
		got := CleanBasePath(tt.input)
		if got != tt.expected {
			t.Errorf("CleanBasePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
