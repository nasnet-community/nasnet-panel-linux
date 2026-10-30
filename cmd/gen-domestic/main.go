// SPDX-License-Identifier: AGPL-3.0-or-later

// Command gen-domestic writes the domestic address list that
// internal/remotenodes/nodecfg embeds, from the geoip.dat built into this
// binary (make geofiles fetches the real one). Run it with go generate in the
// nodecfg directory.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
)

// country is the geoip.dat list the LAN split already uses for "domestic".
const country = "IR"

const header = `# Domestic address ranges, one per line: IPv4 then IPv6, sorted and aggregated, no private ranges.
# Written by cmd/gen-domestic from the embedded geoip.dat; run go generate to refresh.
`

func main() {
	out := flag.String("o", "", "write the list to this file instead of stdout")
	flag.Parse()

	data, err := generate()
	if errors.Is(err, geoip.ErrNoEmbeddedGeoIP) {
		err = errors.New("this build has no geoip.dat to read, run make geofiles first")
	}
	if err == nil && *out != "" {
		err = os.WriteFile(*out, data, 0o644)
	} else if err == nil {
		_, err = os.Stdout.Write(data)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-domestic:", err)
		os.Exit(1)
	}
}

// generate renders the list from the embedded geoip data.
func generate() ([]byte, error) {
	set, err := geoip.EmbeddedCIDRSet(country)
	if err != nil {
		return nil, err
	}
	return render(slices.Concat(set.V4, set.V6))
}

func render(ranges []string) ([]byte, error) {
	prefixes := make([]netip.Prefix, 0, len(ranges))
	for _, s := range ranges {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("bad range %q: %w", s, err)
		}
		if p.Addr().Is4In6() {
			// geoip turns these into IPv4 already; one here means the data is off
			return nil, fmt.Errorf("range %q is an IPv4-mapped IPv6 prefix", s)
		}
		prefixes = append(prefixes, p.Masked())
	}
	public, err := outsidePrivate(prefixes)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(header)
	for _, p := range aggregate(public) {
		b.WriteString(p.String())
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// outsidePrivate drops the ranges that lie inside the private set. geoip.dat
// has a few (internal addresses of Iranian networks); the node blocks those
// before it reads this list, so keeping them would only mislead. A range that
// swallows part of the private set is another matter and stops the run.
func outsidePrivate(ps []netip.Prefix) ([]netip.Prefix, error) {
	var private []netip.Prefix
	for _, s := range nodecfg.PrivateCIDRs() {
		private = append(private, netip.MustParsePrefix(s))
	}
	var out []netip.Prefix
next:
	for _, p := range ps {
		for _, q := range private {
			switch {
			case q.Bits() <= p.Bits() && q.Contains(p.Addr()):
				continue next
			case p.Overlaps(q):
				return nil, fmt.Errorf("range %s covers the private range %s", p, q)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// aggregate returns the fewest prefixes that cover exactly what ps covers,
// IPv4 first, each family in address order.
func aggregate(ps []netip.Prefix) []netip.Prefix {
	sorted := make([]netip.Prefix, len(ps))
	for i, p := range ps {
		sorted[i] = p.Masked()
	}
	// A prefix sorts ahead of everything inside it, so one pass sees every
	// covered range right after what covers it.
	slices.SortFunc(sorted, func(a, b netip.Prefix) int {
		return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
	})

	var out []netip.Prefix
	for _, p := range sorted {
		if n := len(out); n > 0 && out[n-1].Bits() <= p.Bits() && out[n-1].Contains(p.Addr()) {
			continue
		}
		out = append(out, p)
		// Two halves of one parent become the parent, and that may complete
		// the parent's own pair.
		for len(out) >= 2 && siblings(out[len(out)-2], out[len(out)-1]) {
			last := out[len(out)-1]
			out = append(out[:len(out)-2], netip.PrefixFrom(last.Addr(), last.Bits()-1).Masked())
		}
	}
	return out
}

func siblings(a, b netip.Prefix) bool {
	if a.Bits() != b.Bits() || a.Bits() == 0 || a == b {
		return false
	}
	return netip.PrefixFrom(a.Addr(), a.Bits()-1).Masked() == netip.PrefixFrom(b.Addr(), b.Bits()-1).Masked()
}
