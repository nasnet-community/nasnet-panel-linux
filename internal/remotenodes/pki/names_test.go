// SPDX-License-Identifier: AGPL-3.0-or-later

package pki

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
)

// u spells characters by number, so that none of the hostile ones sits in
// this file as an invisible character.
func u(cps ...rune) string { return string(cps) }

const (
	rlo = 0x202e // right-to-left override
	nbs = 0x00a0 // no-break space
)

func TestCommonNames(t *testing.T) {
	long := strings.Repeat("x", 1000)
	cases := []struct{ name, in, want string }{
		{"plain", "Frankfurt", "node-3-Frankfurt"},
		{"spaces inside stay", "my node 2", "node-3-my node 2"},
		{"Persian", "تهران", "node-3-تهران"},
		{"Persian with a zero-width non-joiner", "می" + u(zwnj) + "خواهم", "node-3-می" + u(zwnj) + "خواهم"},
		{"Persian beside Latin", "سرور آلمان - Hetzner", "node-3-سرور آلمان - Hetzner"},
		{"a zero-width joiner", "ک" + u(zwj) + "ا", "node-3-ک" + u(zwj) + "ا"},
		{"digits in Persian script", "سرور ۲", "node-3-سرور ۲"},
		{"control characters go", "a\x00b\tc\nd\re\x7ff\x1b[0m", "node-3-abcdef[0m"},
		{"C1 controls go", "a" + u(0x85) + "b" + u(0x9f) + "c", "node-3-abc"},
		{"line and paragraph separators go", "a" + u(0x2028) + "b" + u(0x2029) + "c", "node-3-abc"},
		{"bidi overrides go", "evil" + u(rlo) + "txt.exe", "node-3-eviltxt.exe"},
		{"bidi embeddings and isolates go", "a" + u(0x202a, 'b', 0x202b, 'c', 0x202c, 'd', 0x202d, 'e', 0x2066, 'f', 0x2067, 'g', 0x2068, 'h', 0x2069, 'i'), "node-3-abcdefghi"},
		{"direction marks go", "a" + u(0x200e) + "b" + u(0x200f) + "c", "node-3-abc"},
		{"byte order mark, soft hyphen and zero-width space go", "a" + u(0xfeff) + "b" + u(0xad) + "c" + u(0x200b) + "d", "node-3-abcd"},
		{"bytes that are not UTF-8 go", "ok\xff\xfe!\xc3", "node-3-ok!"},
		{"a real replacement character goes too", "a" + u(0xfffd) + "b", "node-3-ab"},
		{"edges are trimmed", "  \t ali " + u(nbs), "node-3-ali"},
		{"joiners at the edges are trimmed", u(zwnj, zwj) + "ali" + u(zwnj), "node-3-ali"},
		{"nothing left", "\x00\x01" + u(rlo) + " " + u(zwnj), "node-3-unnamed"},
		{"empty", "", "node-3-unnamed"},
		{"a name that looks like an id", "7-evil", "node-3-7-evil"},
		{"a name that looks like the whole thing", "node-9-x", "node-3-node-9-x"},
		{"punctuation a DN would escape", `a,b+c="d";e\f<g>#h`, `node-3-a,b+c="d";e\f<g>#h`},
		{"cut at the limit", long, "node-3-" + strings.Repeat("x", 57)},
	}
	for _, c := range cases {
		got := commonName(3, c.in)
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if !utf8.ValidString(got) || utf8.RuneCountInString(got) > maxCNRunes {
			t.Errorf("%s: %q is not valid UTF-8 within %d characters", c.name, got, maxCNRunes)
		}
		// whatever the name was, the agent still reads the same id from it
		if id, ok := certs.NodeIDFromCommonName(got); !ok || id != 3 {
			t.Errorf("%s: the agent reads %d, %v from %q", c.name, id, ok, got)
		}
	}
}

func TestTheCapCountsCharactersNotBytes(t *testing.T) {
	for _, id := range []uint{1, 4294967295} {
		cn := commonName(id, strings.Repeat("سلام", 100))
		if n := utf8.RuneCountInString(cn); n != maxCNRunes {
			t.Errorf("id %d: %d characters, want %d", id, n, maxCNRunes)
		}
		if !utf8.ValidString(cn) || !strings.HasPrefix(cn, "node-") {
			t.Errorf("id %d: %q", id, cn)
		}
	}
	// characters that are dropped don't use up the room
	in := strings.Repeat("\x00", 500) + strings.Repeat("a", 500)
	if got, want := commonName(1, in), "node-1-"+strings.Repeat("a", 57); got != want {
		t.Errorf("padding of controls: %q", got)
	}
	// and a huge hostile name is dealt with without being copied whole
	if got := commonName(1, strings.Repeat(u(rlo), 1<<20)); got != "node-1-unnamed" {
		t.Errorf("a megabyte of overrides: %q", got)
	}
}

// What the issued certificate holds is what commonName says, for every one
// of those names, and it parses back to the same text.
func TestHostileNamesSurviveInACertificate(t *testing.T) {
	ca := newTestCA(t, t0)
	for _, name := range []string{
		"تهران", "می" + u(zwnj) + "خواهم", "a\x00b", "evil" + u(rlo) + "txt", "bad\xffbytes", "\x00\x01", "", strings.Repeat("ب", 500),
		"line\nbreak", "CN=admin,O=evil", "node-1-fake",
	} {
		_, leaf := issueNode(t, ca, 5, name, "203.0.113.7")
		if want := commonName(5, name); leaf.Subject.CommonName != want {
			t.Errorf("%q: the certificate says %q, want %q", name, leaf.Subject.CommonName, want)
		}
		if id, ok := certs.NodeIDFromCommonName(leaf.Subject.CommonName); !ok || id != 5 {
			t.Errorf("%q: the agent reads %d, %v", name, id, ok)
		}
		if len(leaf.Subject.Organization) != 1 || leaf.Subject.Organization[0] != org {
			t.Errorf("%q: organization %v", name, leaf.Subject.Organization)
		}
	}
}

func TestPlainDNSName(t *testing.T) {
	for _, ok := range []string{"a", "a.b", "node-1.example.com", "xn--bcher-kva.de", "a1.b2", "x.1y", strings.Repeat("a", 63), "1a"} {
		if !plainDNSName(ok) {
			t.Errorf("%q is refused", ok)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "1.2.3", "256.1.1.1", "a.b.123", "A.com", "-a", "a-", "a_b", "a b", "a.", ".a", "a..b", "é.com", strings.Repeat("a", 64), strings.Repeat("a.", 127) + "a"} {
		if plainDNSName(bad) {
			t.Errorf("%q is taken", bad)
		}
	}
}
