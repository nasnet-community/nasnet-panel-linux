// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg

import (
	"cmp"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// The generator's own tests cover how the list is made; this checks what was
// committed, and still runs where CI has no geoip.dat to regenerate it from.
func TestDomesticListIsCanonical(t *testing.T) {
	list := DomesticCIDRs()
	if len(list) < 1000 {
		t.Fatalf("%d domestic ranges; a truncated file?", len(list))
	}

	var prev netip.Prefix
	v4, v6 := 0, 0
	for i, s := range list {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("range %d %q: %v", i+1, s, err)
		}
		if p != p.Masked() || p.String() != s {
			t.Fatalf("range %d %q isn't in canonical form (%s)", i+1, s, p.Masked())
		}
		if p.Addr().Is4In6() {
			t.Fatalf("range %d %q is an IPv4-mapped IPv6 prefix", i+1, s)
		}
		if p.Addr().Is4() {
			v4++
		} else {
			v6++
		}
		if i > 0 {
			if cmp.Or(prev.Addr().Compare(p.Addr()), cmp.Compare(prev.Bits(), p.Bits())) >= 0 {
				t.Fatalf("%s comes after %s: not sorted", s, prev)
			}
			if prev.Overlaps(p) {
				t.Fatalf("%s overlaps %s: not aggregated", s, prev)
			}
			if prev.Bits() == p.Bits() && p.Bits() > 0 &&
				netip.PrefixFrom(prev.Addr(), p.Bits()-1).Masked() == netip.PrefixFrom(p.Addr(), p.Bits()-1).Masked() {
				t.Fatalf("%s and %s are halves of one prefix: not aggregated", prev, s)
			}
		}
		prev = p
	}
	if v4 == 0 || v6 == 0 {
		t.Fatalf("%d IPv4 and %d IPv6 ranges; the list should have both", v4, v6)
	}
}

// A domestic range inside a private one would never be reached: the private
// rule comes first.
func TestDomesticListAvoidsPrivateRanges(t *testing.T) {
	var private []netip.Prefix
	for _, s := range privateRanges {
		private = append(private, netip.MustParsePrefix(s))
	}
	for _, s := range DomesticCIDRs() {
		p := netip.MustParsePrefix(s)
		for _, q := range private {
			if p.Overlaps(q) {
				t.Errorf("domestic range %s overlaps private range %s", p, q)
			}
		}
	}
}

func TestDomesticCIDRsReturnsACopy(t *testing.T) {
	a := DomesticCIDRs()
	want := a[0]
	a[0] = "0.0.0.0/0"
	if got := DomesticCIDRs()[0]; got != want {
		t.Fatalf("a caller changed the shared list: first range is now %s, was %s", got, want)
	}
}

func TestDomesticTextHasNoStrayLines(t *testing.T) {
	for n, line := range strings.Split(domesticText, "\n") {
		if line != strings.TrimSpace(line) {
			t.Errorf("line %d has surrounding space: %q", n+1, line)
		}
	}
	if !strings.HasSuffix(domesticText, "\n") {
		t.Error("the file doesn't end with a newline")
	}
	if got, want := len(DomesticCIDRs()), len(slices.DeleteFunc(strings.Split(domesticText, "\n"), func(l string) bool {
		return l == "" || strings.HasPrefix(l, "#")
	})); got != want {
		t.Errorf("parsed %d ranges from %d lines", got, want)
	}
}
