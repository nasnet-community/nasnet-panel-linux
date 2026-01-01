package telegram

import "testing"

// configLines must return every non-empty config line, not just the first.
// Regression guard: the QR handler used to keep only the first line, so
// multi-config subscriptions showed a QR code for Config 1 only.
func TestConfigLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "two configs",
			in:   "vless://config-1\nvless://config-2",
			want: []string{"vless://config-1", "vless://config-2"},
		},
		{
			name: "blank and whitespace lines skipped, surviving lines trimmed",
			in:   "\n  vless://a  \n\n\tvless://b\n   \n",
			want: []string{"vless://a", "vless://b"},
		},
		{
			name: "single config",
			in:   "vless://only",
			want: []string{"vless://only"},
		},
		{
			name: "empty",
			in:   "   \n\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configLines(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("configLines(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("configLines(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}
