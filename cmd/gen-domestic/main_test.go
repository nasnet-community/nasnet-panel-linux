// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
)

const listPath = "../../internal/remotenodes/nodecfg/domestic_cidrs.txt"

func prefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func strs(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

func TestAggregate(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"single", []string{"10.0.0.0/24"}, []string{"10.0.0.0/24"}},
		{"unsorted", []string{"10.2.0.0/16", "10.0.0.0/16"}, []string{"10.0.0.0/16", "10.2.0.0/16"}},
		{"duplicate", []string{"10.0.0.0/24", "10.0.0.0/24"}, []string{"10.0.0.0/24"}},
		{"covered", []string{"10.0.0.64/26", "10.0.0.0/24", "10.0.0.0/25"}, []string{"10.0.0.0/24"}},
		{"host bits", []string{"10.0.0.77/24"}, []string{"10.0.0.0/24"}},
		{"siblings merge", []string{"10.0.0.0/25", "10.0.0.128/25"}, []string{"10.0.0.0/24"}},
		{"merge cascades", []string{
			"10.0.0.0/26", "10.0.0.64/26", "10.0.0.128/26", "10.0.0.192/26",
		}, []string{"10.0.0.0/24"}},
		{"cousins stay apart", []string{"10.0.0.64/26", "10.0.0.128/26"}, []string{"10.0.0.64/26", "10.0.0.128/26"}},
		{"gap stays", []string{"10.0.0.0/26", "10.0.0.128/26"}, []string{"10.0.0.0/26", "10.0.0.128/26"}},
		{"everything", []string{"0.0.0.0/1", "128.0.0.0/1"}, []string{"0.0.0.0/0"}},
		{"ipv6", []string{"2001:db8::/33", "2001:db8:8000::/33"}, []string{"2001:db8::/32"}},
		{"v4 before v6", []string{"2001:db8::/32", "10.0.0.0/8"}, []string{"10.0.0.0/8", "2001:db8::/32"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strs(aggregate(prefixes(c.in...)))
			if !slices.Equal(got, c.want) {
				t.Fatalf("aggregate(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Whatever goes in, the same addresses come out and no shorter list could do
// it: the result is sorted, doesn't overlap and has no pair left to merge. The
// space is 256 addresses (the last byte), small enough that random prefixes
// overlap and pair up often and coverage can be checked address by address.
func TestAggregateKeepsCoverage(t *testing.T) {
	spaces := []struct {
		name string
		base netip.Addr // the space's first address
		bits int        // its prefix length
	}{
		{"v4", netip.MustParseAddr("10.1.2.0"), 24},
		{"v6", netip.MustParseAddr("2001:db8::"), 120},
	}
	const size = 1 << 8
	rng := rand.New(rand.NewPCG(1, 2))

	for _, sp := range spaces {
		t.Run(sp.name, func(t *testing.T) {
			bitlen := sp.base.BitLen()
			// first and last index a prefix of the space covers
			span := func(p netip.Prefix) (first, last int) {
				b := p.Addr().AsSlice()
				first = int(b[len(b)-1])
				return first, first + 1<<(bitlen-p.Bits()) - 1
			}
			covered := func(ps []netip.Prefix) []bool {
				c := make([]bool, size)
				for _, p := range ps {
					first, last := span(p)
					for i := first; i <= last; i++ {
						c[i] = true
					}
				}
				return c
			}

			shrunk := 0
			for trial := 0; trial < 500; trial++ {
				var in []netip.Prefix
				for i, n := 0, 1+rng.IntN(40); i < n; i++ {
					raw := sp.base.AsSlice()
					raw[len(raw)-1] = byte(rng.IntN(size))
					addr, _ := netip.AddrFromSlice(raw)
					in = append(in, netip.PrefixFrom(addr, sp.bits+rng.IntN(bitlen-sp.bits+1)).Masked())
				}

				out := aggregate(in)
				if !slices.Equal(covered(in), covered(out)) {
					t.Fatalf("coverage changed: %v -> %v", in, out)
				}
				for i := 1; i < len(out); i++ {
					_, prevLast := span(out[i-1])
					first, _ := span(out[i])
					if first <= prevLast {
						t.Fatalf("%v then %v: not in order or overlapping (from %v)", out[i-1], out[i], in)
					}
					if siblings(out[i-1], out[i]) {
						t.Fatalf("%v and %v could still merge (from %v)", out[i-1], out[i], in)
					}
				}
				if len(out) < len(in) {
					shrunk++
				}
			}
			if shrunk < 100 {
				t.Fatalf("only %d of 500 trials had anything to aggregate; the generator isn't testing much", shrunk)
			}
		})
	}
}

func TestRender(t *testing.T) {
	got, err := render([]string{"2001:db8::/33", "8.8.8.128/25", "8.8.8.0/25", "2001:db8:8000::/33"})
	if err != nil {
		t.Fatal(err)
	}
	want := header + "8.8.8.0/24\n2001:db8::/32\n"
	if string(got) != want {
		t.Fatalf("render:\n%s\nwant:\n%s", got, want)
	}

	for _, bad := range []string{"10.0.0.0", "10.0.0.0/33", "::ffff:10.0.0.0/104", "nonsense"} {
		if _, err := render([]string{bad}); err == nil {
			t.Errorf("render accepted %q", bad)
		}
	}
}

func TestRenderLeavesOutPrivateRanges(t *testing.T) {
	got, err := render([]string{"192.168.204.48/28", "8.8.8.0/24", "10.202.10.10/31", "fe80::/64", "2001:db8::/32", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	if want := header + "8.8.8.0/24\n2001:db8::/32\n"; string(got) != want {
		t.Fatalf("render:\n%s\nwant:\n%s", got, want)
	}

	// a range that holds private addresses is bad data, not something to trim
	for _, wide := range []string{"8.0.0.0/5", "0.0.0.0/0", "::/0", "192.0.0.0/8"} {
		if _, err := render([]string{wide}); err == nil || !strings.Contains(err.Error(), "private range") {
			t.Errorf("render(%q) = %v, want an error about the private range", wide, err)
		}
	}
}

// The committed list has to be what the generator writes from the geoip.dat in
// this build. Without the real file (CI stubs it) there is nothing to compare.
func TestCommittedListIsFresh(t *testing.T) {
	want, err := generate()
	if errors.Is(err, geoip.ErrNoEmbeddedGeoIP) {
		t.Skip("no real geoip.dat in this build, so the list can't be regenerated here")
	}
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is out of date (%d lines on disk, %d from geoip.dat); run go generate in internal/remotenodes/nodecfg",
			listPath, strings.Count(string(got), "\n"), strings.Count(string(want), "\n"))
	}
}
