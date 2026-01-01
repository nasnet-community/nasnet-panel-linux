package usecase

import "testing"

func TestNormalizeArch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"amd64", "amd64"},
		{"AMD64", "amd64"},
		{"x86_64", "amd64"},
		{"X86_64", "amd64"},
		{"arm64", "arm64"},
		{"ARM64", "arm64"},
		{"aarch64", "arm64"},
		{"AArch64", "arm64"},
		{"", "amd64"},
		{"unknown", "amd64"},
		{"  amd64  ", "amd64"},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := normalizeArch(c.in)
			if got != c.want {
				t.Fatalf("normalizeArch(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
