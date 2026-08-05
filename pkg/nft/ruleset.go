package nft

// Package nft owns `table inet nasnet` — the entire firewall and mangle surface this panel installs.

import (
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

const (
	TableFamily = "inet"
	TableName   = "nasnet"
)

// Pin records that flows arriving on IfName belong to uplink Index, so their
// replies leave by the same uplink. Index is an uplink index.
type Pin struct {
	IfName string
	Index  uint32
}

// Ruleset is the complete desired state of the owned table. Zero value renders
// an empty table, which is a valid "everything off" state
type Ruleset struct {
	// Connmark enables the masked save/restore pair that download shaping,
	// forwarded reply-path pinning and the DNAT ingress pin all depend on.
	Connmark bool

	// IngressPins stamp the arrival uplink into ct mark. Requires Connmark.
	IngressPins []Pin
}

// Render returns a complete `nft -f -` script that atomically replaces the owned table.
func (r Ruleset) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "table %s %s\n", TableFamily, TableName)
	fmt.Fprintf(&b, "delete table %s %s\n", TableFamily, TableName)
	fmt.Fprintf(&b, "table %s %s {\n", TableFamily, TableName)

	if body := r.renderManglePre(); body != "" {
		b.WriteString(body)
	}
	if r.Connmark {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString(r.renderManglePost())
	}

	b.WriteString("}\n")
	return b.String()
}

// renderManglePre emits the prerouting mangle chain at priority -150: after
// conntrack (-200), so ct state and ct mark are available, and before dstnat
// (-100), so iifname is still the real ingress device.
func (r Ruleset) renderManglePre() string {
	if !r.Connmark && len(r.IngressPins) == 0 {
		return ""
	}
	all := netmark.Hex(netmark.MaskAll)
	keep := netmark.Hex(^netmark.MaskPin) // clear the pin field, keep the rest

	var b strings.Builder
	b.WriteString("\tchain mangle_pre {\n")
	b.WriteString("\t\ttype filter hook prerouting priority mangle; policy accept;\n")

	for _, p := range r.IngressPins {
		fmt.Fprintf(&b, "\t\tiifname %q ct state new ct mark set ct mark and %s or %s\n",
			p.IfName, keep, netmark.Hex(netmark.PinMark(p.Index)))
	}
	if r.Connmark {
		fmt.Fprintf(&b, "\t\tct mark != 0x0 meta mark set ct mark and %s\n", all)
	}

	b.WriteString("\t}\n")
	return b.String()
}

// renderManglePost writes the working skb mark back to conntrack.
func (r Ruleset) renderManglePost() string {
	all := netmark.Hex(netmark.MaskAll)
	var b strings.Builder
	b.WriteString("\tchain mangle_post {\n")
	b.WriteString("\t\ttype filter hook postrouting priority mangle; policy accept;\n")
	fmt.Fprintf(&b, "\t\tmeta mark and %s != 0x0 ct mark set meta mark and %s\n", all, all)
	b.WriteString("\t}\n")
	return b.String()
}
