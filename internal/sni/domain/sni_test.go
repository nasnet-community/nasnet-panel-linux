package domain

import (
	"strings"
	"testing"
)

func TestSNI_GetALPNList(t *testing.T) {
	tests := []struct {
		name string
		alpn string
		want []string
	}{
		{"empty falls back to defaults", "", []string{"h2", "http/1.1"}},
		{"two values", "h2,http/1.1", []string{"h2", "http/1.1"}},
		{"single value", "h3", []string{"h3"}},
		{"trailing comma ignored", "h2,", []string{"h2"}},
		{"empty segments skipped", "a,,b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&SNI{ALPN: tt.alpn}).GetALPNList()
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSNI_MaskPrivateKey(t *testing.T) {
	if got := (&SNI{PrivateKey: "too-short"}).MaskPrivateKey(); got != "***INVALID***" {
		t.Errorf("short key = %q, want ***INVALID***", got)
	}

	key := strings.Repeat("A", 60) + strings.Repeat("B", 60) // 120 chars
	got := (&SNI{PrivateKey: key}).MaskPrivateKey()
	if !strings.HasPrefix(got, key[:50]) || !strings.HasSuffix(got, key[len(key)-30:]) {
		t.Errorf("masked key missing head/tail: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("masked key should contain redaction marker: %q", got)
	}
}
