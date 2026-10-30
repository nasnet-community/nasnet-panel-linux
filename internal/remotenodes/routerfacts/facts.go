// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"bytes"
	"errors"
	"io"
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

	VPNKind  string // wireguard, l2tp, openvpn or other; "" when the router named none
	VPNLabel string // the VPN's interface name

	// Whether the routing table behind each of the plugin's three source
	// addresses exists (see Routes).
	RouteForeign  bool
	RouteVPN      bool
	RouteDomestic bool

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
	case "route_foreign":
		ok = flag(&f.RouteForeign, val)
	case "route_vpn":
		ok = flag(&f.RouteVPN, val)
	case "route_domestic":
		ok = flag(&f.RouteDomestic, val)
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
