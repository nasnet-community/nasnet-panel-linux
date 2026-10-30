// SPDX-License-Identifier: AGPL-3.0-or-later

package pki

import (
	"net"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxCNRunes is the longest common name RFC 5280 allows. Go doesn't enforce
// it, but other tools that print or parse the subject do.
const maxCNRunes = 64

// The zero-width non-joiner and joiner. Persian is spelled with them.
const (
	zwnj = 0x200c
	zwj  = 0x200d
)

// commonName is node-<id>-<name>, the form the agent reads the id out of.
//
// The name is whatever the user typed, so it is made safe first: bytes that
// aren't UTF-8 go, and so do control characters, line and paragraph
// separators and the format characters (the bidi overrides among them, which
// could make a subject read differently from what it holds). The zero-width
// non-joiner and joiner stay, since Persian is spelled with them. The whole
// CN is cut to maxCNRunes characters, and a name with nothing left becomes
// "unnamed".
func commonName(id uint, name string) string {
	prefix := "node-" + strconv.FormatUint(uint64(id), 10) + "-"
	return prefix + cleanName(name, maxCNRunes-len(prefix))
}

func cleanName(name string, room int) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(name) && n < room; {
		r, size := utf8.DecodeRuneInString(name[i:])
		i += size
		if dropRune(r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	// a joiner at either end joins nothing
	trim := func(r rune) bool { return unicode.IsSpace(r) || r == zwnj || r == zwj }
	if s := strings.TrimFunc(b.String(), trim); s != "" {
		return s
	}
	return "unnamed"
}

func dropRune(r rune) bool {
	switch {
	case r == utf8.RuneError: // a stray byte decodes to this, and so does a real U+FFFD
		return true
	case r == zwnj || r == zwj:
		return false
	}
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}

// sans puts host where a TLS client looks for it. An IP address is matched
// against the IP SANs only, and a name against the DNS ones only, so each
// goes in its own.
func sans(host string) (ips []net.IP, dns []string, err error) {
	if a, perr := netip.ParseAddr(host); perr == nil {
		if a.Zone() != "" || a.IsUnspecified() {
			return nil, nil, ErrBadHost
		}
		return []net.IP{a.AsSlice()}, nil, nil
	}
	name := strings.ToLower(host)
	if !plainDNSName(name) {
		return nil, nil, ErrBadHost
	}
	return nil, []string{name}, nil
}

// plainDNSName is letters, digits and hyphens in dot-separated labels, with
// no trailing dot, so an internationalised name has to come as punycode. A
// last label of digits is a mistyped address (1.2.3, 256.1.1.1), not a name.
// It is the same rule the model applies to a node's host.
func plainDNSName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-') {
				return false
			}
		}
	}
	last := labels[len(labels)-1]
	for i := 0; i < len(last); i++ {
		if last[i] < '0' || last[i] > '9' {
			return true
		}
	}
	return false
}
