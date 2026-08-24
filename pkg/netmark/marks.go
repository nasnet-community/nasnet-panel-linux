// Package netmark is the single home for the 32-bit firewall mark word this
// panel puts on packets. Three subsystems share it, so the layout lives here
// and nowhere else.
//
//	0x0000FFFF  bandwidth tier    pkg/bandwidth Tier.Mark
//	0x00FF0000  group selector    1 = domestic, 2 = foreign, 0x10|i = foreign via uplink i
//	0x0F000000  ingress pin       uplink index — NOT a group
//	0xF0000000  reserved          must stay zero (see MaskAll)
package netmark

import "fmt"

// Field masks. Together they tile the whole word.
const (
	MaskTier     uint32 = 0x0000FFFF
	MaskGroup    uint32 = 0x00FF0000
	MaskPin      uint32 = 0x0F000000
	MaskReserved uint32 = 0xF0000000

	// MaskAll is the only mask permitted in a conntrack save/restore or a
	// `tc ... fw` filter handle
	MaskAll uint32 = MaskTier | MaskGroup | MaskPin

	shiftGroup = 16
	shiftPin   = 24
)

// Group indices. Stage 1 ships exactly these two.
const (
	GroupDomestic uint32 = 1
	GroupForeign  uint32 = 2
)

// PinProbe marks the health probe's own sockets. Uplinks count up from 1.
const PinProbe uint32 = 15

// Foreign-via groups take 0x10–0x1F, low nibble naming the uplink. A via mark
// routes into that WAN's slice of the tunnel pool, never the WAN itself.
const groupForeignViaBase uint32 = 0x10

func GroupForeignVia(i uint32) uint32 { return groupForeignViaBase | (i & 0x0F) }

func IsGroupForeignVia(g uint32) bool {
	return g&^uint32(0x0F) == groupForeignViaBase && g&0x0F != 0
}

// 0 means g is not a via group.
func GroupViaUplink(g uint32) uint32 {
	if !IsGroupForeignVia(g) {
		return 0
	}
	return g & 0x0F
}

// GroupMark returns a mark word carrying only group index g.
func GroupMark(g uint32) uint32 { return (g << shiftGroup) & MaskGroup }

// PinMark returns a mark word carrying only ingress-pin uplink index i.
func PinMark(i uint32) uint32 { return (i << shiftPin) & MaskPin }

// WithGroup replaces the group field of mark with g, leaving tier and pin.
func WithGroup(mark, g uint32) uint32 { return (mark &^ MaskGroup) | GroupMark(g) }

// WithPin replaces the ingress-pin field of mark with i.
func WithPin(mark, i uint32) uint32 { return (mark &^ MaskPin) | PinMark(i) }

// Group extracts the group index; 0 means unclassified.
func Group(mark uint32) uint32 { return (mark & MaskGroup) >> shiftGroup }

// Pin extracts the ingress-pin uplink index; 0 means unpinned.
func Pin(mark uint32) uint32 { return (mark & MaskPin) >> shiftPin }

// Tier extracts the bandwidth-tier value (the rate in Mbit).
func Tier(mark uint32) uint32 { return mark & MaskTier }

// Hex formats a mark or mask the way nft, ip rule and tc expect it.
func Hex(v uint32) string { return fmt.Sprintf("0x%x", v) }
