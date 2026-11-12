// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// maxFile is as much of the report as is ever read. The script writes a dozen
// short lines, so a file near this is not its report, and the hub has no
// memory to spare on finding out.
const maxFile = 64 << 10

// maxUpdated is 2100-01-01. A later "updated" is a script counting
// milliseconds, not a time, and one far enough out wouldn't even encode as
// JSON.
const maxUpdated = 4102444800

var ErrTooLarge = errors.New("the router's report is too large")

// Path is where the router's script leaves its report under the plugin's
// data folder.
func Path(dataDir string) string { return filepath.Join(dataDir, "router", "state") }

// Size is a byte count the router may not have reported. The zero value says
// it didn't, which is not the same as having no bytes free.
type Size struct {
	Bytes uint64
	Known bool
}

// Facts is one report. It holds only values, so a copy is a deep copy.
type Facts struct {
	Domestic bool // a Domestic link is up
	VPN      bool // a VPN is up

	// DomesticAddr is the router's own IPv4 address on the Domestic link. It is
	// the zero Addr when the router gave none, which is what it does while the
	// link is down, and also when it gave something that isn't an address.
	// Nothing tells those apart, and neither is a change of address.
	DomesticAddr netip.Addr

	VPNKind  string // wireguard, l2tp, openvpn or other; "" when the router named none
	VPNLabel string // the VPN's interface name

	// Whether the routing table behind each of the plugin's three source
	// addresses exists (see Routes).
	RouteForeign  bool
	RouteVPN      bool
	RouteDomestic bool

	// What the router keeps for its own entry (see Asks). All of it is zero
	// while the entry is off, and with a script from before there was one.
	//
	// LocalPort is the port on the Domestic link the router forwards to the
	// entry, 0 while it has picked none. LocalRules is whether that forward is
	// in place now: the port is kept while the entry is off, so that switching
	// it on again gives the same links.
	LocalPort  uint16
	LocalRules bool
	// CloudName is the router's IP Cloud name, in lower case, and PublicAddr
	// its address as IP Cloud sees it. Either is empty when the router has
	// none, and also when it gave something that isn't one.
	CloudName  string
	PublicAddr netip.Addr

	ROS      string // the RouterOS version as reported, such as 7.24.5 or 7.12rc1
	FreeMem  Size
	FreeDisk Size

	// Updated is when the script wrote the report, by the router's clock. It
	// is zero if the report didn't say.
	Updated time.Time

	BadLines int // lines that were skipped
}

// Parse reads a report. A line it can't make sense of is skipped and counted
// in BadLines, and a key it doesn't know is ignored, so the error is only for
// a reader that fails or a report that is far too long.
func Parse(r io.Reader) (Facts, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxFile+1))
	if err != nil {
		return Facts{}, err
	}
	if len(b) > maxFile {
		return Facts{}, ErrTooLarge
	}
	return parse(b), nil
}

// parse works on a copy of the bytes, never on the file, so what it sees can't
// change halfway.
func parse(b []byte) Facts {
	var f Facts
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) // an editor's byte order mark
	for len(b) > 0 {
		var line []byte
		line, b, _ = bytes.Cut(b, []byte("\n"))
		// TrimSpace also takes the \r of a CRLF line ending
		f.line(string(bytes.TrimSpace(line)))
	}
	return f
}

func (f *Facts) line(s string) {
	if s == "" || s[0] == '#' {
		return
	}
	key, val, found := strings.Cut(s, "=")
	key, val = strings.TrimSpace(key), strings.TrimSpace(val)
	if !found || key == "" {
		f.BadLines++
		return
	}
	// A value that doesn't fit its key leaves what an earlier line said, so a
	// bad line is skipped as if it weren't there.
	ok := true
	switch key {
	case "domestic":
		ok = flag(&f.Domestic, val)
	case "vpn":
		ok = flag(&f.VPN, val)
	case "domestic_addr":
		// unlike the keys around it, a bad value doesn't keep an earlier
		// line's: the router said something about its address, and it
		// wasn't one, so the address is not known
		ok = address(&f.DomesticAddr, val)
	case "route_foreign":
		ok = flag(&f.RouteForeign, val)
	case "route_vpn":
		ok = flag(&f.RouteVPN, val)
	case "route_domestic":
		ok = flag(&f.RouteDomestic, val)
	case "local_port":
		ok = port(&f.LocalPort, val)
	case "local_rules":
		ok = flag(&f.LocalRules, val)
	case "cloud_name":
		ok = hostName(&f.CloudName, val)
	case "public_addr":
		ok = address(&f.PublicAddr, val)
	case "vpn_kind":
		f.VPNKind = kind(val)
	case "vpn_label":
		f.VPNLabel = plain(val, 64)
	case "ros":
		f.ROS = plain(val, 32)
	case "free_mem":
		ok = size(&f.FreeMem, val)
	case "free_disk":
		ok = size(&f.FreeDisk, val)
	case "updated":
		ok = unix(&f.Updated, val)
	}
	if !ok {
		f.BadLines++
	}
}

// flag takes 1 or 0 and nothing else. yes and true are a script's mistake, so
// they are refused rather than guessed at.
func flag(dst *bool, v string) bool {
	switch v {
	case "1":
		*dst = true
	case "0":
		*dst = false
	default:
		return false
	}
	return true
}

// address takes the IPv4 address of one of the router's own links, in dotted
// decimal and nothing else: no mask, no zone, no leading zeros, no IPv6. Nor
// is one that no link holds an address: 0.0.0.0, loopback, link-local,
// multicast or the broadcast. An empty value is the script saying it has none,
// which is a good line. Whatever fails leaves the address unknown.
func address(dst *netip.Addr, v string) bool {
	*dst = netip.Addr{}
	if v == "" {
		return true
	}
	a, err := netip.ParseAddr(v)
	if err != nil || !a.Is4() || !a.IsGlobalUnicast() {
		return false
	}
	*dst = a
	return true
}

// port takes a port in decimal, or nothing or 0 for none. Like an address, a
// value that fails leaves none: a link must not be made from a port the
// router didn't really give.
func port(dst *uint16, v string) bool {
	*dst = 0
	if v == "" {
		return true
	}
	n, err := strconv.ParseUint(v, 10, 16)
	if err != nil || (len(v) > 1 && v[0] == '0') {
		return false
	}
	*dst = uint16(n)
	return true
}

// hostName takes a DNS name the way a link can carry one: labels of letters,
// digits and hyphens, at least two of them, 253 characters at most, and not an
// address. It comes out in lower case. An empty value is the router saying it
// has no name, which is a good line; whatever fails leaves none.
func hostName(dst *string, v string) bool {
	*dst = ""
	if v == "" {
		return true
	}
	v = strings.ToLower(strings.TrimSuffix(v, "."))
	if len(v) > 253 || !strings.Contains(v, ".") {
		return false
	}
	if _, err := netip.ParseAddr(v); err == nil {
		return false
	}
	for _, label := range strings.Split(v, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			if c := label[i]; !('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-') {
				return false
			}
		}
	}
	*dst = v
	return true
}

// kind keeps the three kinds the page knows by name and files any other VPN
// under "other".
func kind(v string) string {
	switch v = strings.ToLower(v); v {
	case "", "wireguard", "l2tp", "openvpn":
		return v
	}
	return "other"
}

func size(dst *Size, v string) bool {
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return false
	}
	*dst = Size{Bytes: n, Known: true}
	return true
}

func unix(dst *time.Time, v string) bool {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > maxUpdated {
		return false
	}
	*dst = time.Unix(n, 0).UTC()
	return true
}

// plain is text the page will show: valid UTF-8, no control characters, at
// most n characters.
func plain(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return strings.TrimSpace(s)
}
