package domain

import "testing"

// Bit i corresponds to the i-th boolean argument, LSB first.
func TestAlertFlagsFromBooleans(t *testing.T) {
	tests := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"none", AlertFlagsFromBooleans(false, false, false, false, false, false, false, false, false, false, false), 0},
		{"first bit", AlertFlagsFromBooleans(true, false, false, false, false, false, false, false, false, false, false), 1},
		{"last bit", AlertFlagsFromBooleans(false, false, false, false, false, false, false, false, false, false, true), 1 << 10},
		{"two bits", AlertFlagsFromBooleans(false, true, false, false, false, false, false, false, true, false, false), 1<<1 | 1<<8},
		{"all bits", AlertFlagsFromBooleans(true, true, true, true, true, true, true, true, true, true, true), 1<<11 - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("flags = %d, want %d", tt.got, tt.want)
			}
		})
	}
}
