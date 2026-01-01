package middleware

import (
	"testing"
)

func TestExtractSubUUID(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/sub/abc-123", "abc-123"},
		{"/sub/abc-123/renew", "abc-123"},
		{"/api/v1/public/sub/xyz", "xyz"},
		{"/api/v1/public/sub/xyz/telegram-chat-id", "xyz"},
		{"/api/v1/admin/maintenance/global", ""},
		{"/health", ""},
		{"/sub/", ""},
	}
	for _, tc := range cases {
		got := extractSubUUID(tc.path)
		if got != tc.want {
			t.Errorf("extractSubUUID(%q) = %q; want %q", tc.path, got, tc.want)
		}
	}
}
