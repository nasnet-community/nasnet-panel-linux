package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/oui"
)

// LANDeviceLabel is an operator-assigned name for a LAN device.
//
// Deliberately the only stored device state. Everything else in the device list
// is derived per request from the lease file, the neighbour table and the
// bridge FDB, so there is nothing that can go stale or accumulate.
type LANDeviceLabel struct {
	ID     uint `gorm:"primarykey" json:"id"`
	NodeID uint `gorm:"index;not null;default:1" json:"node_id"`

	// MAC is the canonical lowercase colon form. GORM names this column "mac".
	MAC   string `gorm:"uniqueIndex;not null" json:"mac"`
	Label string `gorm:"not null" json:"label"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// No DeletedAt, unlike its siblings: a soft-deleted row still occupies the
	// unique index on mac, so clearing a name would block ever setting it again.
	// Clearing here means removing.
}

// MaxDeviceLabelRunes counts runes, not bytes: a Persian label is legitimate
// and would otherwise get a third of the room an English one gets.
const MaxDeviceLabelRunes = 63

var (
	ErrInvalidMAC       = errors.New("not a MAC address")
	ErrRandomizedMAC    = errors.New("this device uses a randomized MAC address, which changes each time it joins, so a name would not stay attached to it")
	ErrLabelTooLong     = fmt.Errorf("a name may be at most %d characters", MaxDeviceLabelRunes)
	ErrLabelUnprintable = errors.New("a name may not contain control or text-direction characters")
)

// ValidateDeviceLabel normalizes the MAC and checks the label.
//
// The label is operator-supplied, so unlike a client's hostname it may be any
// script — Persian names are the point. Only characters that corrupt the
// rendering of everyone else's row are refused.
func ValidateDeviceLabel(mac, label string) (normMAC, normLabel string, err error) {
	normMAC = oui.Normalize(mac)
	if normMAC == "" {
		return "", "", ErrInvalidMAC
	}
	// A name on a MAC that will not exist next week is a name the operator
	// loses, plus an orphan row per rejoin. Refuse, and say why.
	if oui.IsRandomized(normMAC) {
		return "", "", ErrRandomizedMAC
	}

	normLabel = strings.TrimSpace(label)
	if normLabel == "" {
		return normMAC, "", nil // clearing the name
	}
	if !utf8.ValidString(normLabel) {
		return "", "", ErrLabelUnprintable
	}
	if utf8.RuneCountInString(normLabel) > MaxDeviceLabelRunes {
		return "", "", ErrLabelTooLong
	}
	for _, r := range normLabel {
		if unicode.IsControl(r) || isBidiOverride(r) {
			return "", "", ErrLabelUnprintable
		}
	}
	return normMAC, normLabel, nil
}

// isBidiOverride reports the explicit direction controls. Persian renders from
// its own characters; these only exist to make text display as something else.
func isBidiOverride(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}
