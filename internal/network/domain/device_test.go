package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateDeviceLabel_NormalizesTheMAC(t *testing.T) {
	for _, in := range []string{"B8:27:EB:AA:BB:01", "b8-27-eb-aa-bb-01", "b827.ebaa.bb01"} {
		mac, _, err := ValidateDeviceLabel(in, "nas")
		if err != nil || mac != "b8:27:eb:aa:bb:01" {
			t.Errorf("ValidateDeviceLabel(%q) = %q, %v", in, mac, err)
		}
	}
}

// Decision 20: refusing at the source beats reaping orphans afterwards, and the
// refusal teaches the operator why their phone keeps reappearing.
func TestValidateDeviceLabel_RefusesRandomizedMACs(t *testing.T) {
	_, _, err := ValidateDeviceLabel("b2:27:eb:aa:bb:02", "my phone")
	if !errors.Is(err, ErrRandomizedMAC) {
		t.Errorf("err = %v, want ErrRandomizedMAC", err)
	}
}

func TestValidateDeviceLabel_RejectsMalformedMACs(t *testing.T) {
	for _, in := range []string{"", "nonsense", "b8:27:eb"} {
		if _, _, err := ValidateDeviceLabel(in, "x"); !errors.Is(err, ErrInvalidMAC) {
			t.Errorf("ValidateDeviceLabel(%q) err = %v, want ErrInvalidMAC", in, err)
		}
	}
}

// Operator-supplied, so any script is legitimate — unlike a client's hostname.
func TestValidateDeviceLabel_AllowsUnicode(t *testing.T) {
	for _, in := range []string{"مودم اتاق", "NAS — office", "プリンタ"} {
		_, got, err := ValidateDeviceLabel("b8:27:eb:aa:bb:01", in)
		if err != nil || got != in {
			t.Errorf("ValidateDeviceLabel(_, %q) = %q, %v", in, got, err)
		}
	}
}

func TestValidateDeviceLabel_TrimsAndClears(t *testing.T) {
	_, got, err := ValidateDeviceLabel("b8:27:eb:aa:bb:01", "  nas  ")
	if err != nil || got != "nas" {
		t.Errorf("got %q, %v", got, err)
	}
	// Blank is how a name is removed, not an error.
	_, got, err = ValidateDeviceLabel("b8:27:eb:aa:bb:01", "   ")
	if err != nil || got != "" {
		t.Errorf("blank label = %q, %v; want a clear", got, err)
	}
}

func TestValidateDeviceLabel_RejectsUnprintable(t *testing.T) {
	for _, in := range []string{"a\x00b", "a\nb", "a\tb", "admin‮", "x⁦y"} {
		if _, _, err := ValidateDeviceLabel("b8:27:eb:aa:bb:01", in); !errors.Is(err, ErrLabelUnprintable) {
			t.Errorf("ValidateDeviceLabel(_, %q) err = %v, want ErrLabelUnprintable", in, err)
		}
	}
}

// Runes, not bytes: a Persian name would otherwise get a third of the room.
func TestValidateDeviceLabel_LengthIsCountedInRunes(t *testing.T) {
	if _, _, err := ValidateDeviceLabel("b8:27:eb:aa:bb:01",
		strings.Repeat("ا", MaxDeviceLabelRunes)); err != nil {
		t.Errorf("a %d-rune Persian label was rejected: %v", MaxDeviceLabelRunes, err)
	}
	if _, _, err := ValidateDeviceLabel("b8:27:eb:aa:bb:01",
		strings.Repeat("a", MaxDeviceLabelRunes+1)); !errors.Is(err, ErrLabelTooLong) {
		t.Error("an over-length label was accepted")
	}
}
