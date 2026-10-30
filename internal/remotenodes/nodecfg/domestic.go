// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg

import (
	_ "embed"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
)

// The list is written from geoip.dat, which only a build with the real file
// has (make geofiles); CI's empty stub makes the generator refuse.
//
//go:generate go run ../../../cmd/gen-domestic -o domestic_cidrs.txt

//go:embed domestic_cidrs.txt
var domesticText string

var domesticOnce = sync.OnceValue(func() []string {
	var out []string
	for n, line := range strings.Split(domesticText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		// A bad line is a bad commit, and the tests catch it; not worth an
		// error every caller would have to handle.
		if p, err := netip.ParsePrefix(line); err != nil || p != p.Masked() {
			panic(fmt.Sprintf("nodecfg: domestic_cidrs.txt line %d: %q", n+1, line))
		}
		out = append(out, line)
	}
	return out
})

// DomesticCIDRs returns the domestic ranges the node sends out directly: IPv4
// then IPv6, sorted and aggregated. The file is parsed once.
func DomesticCIDRs() []string {
	return slices.Clone(domesticOnce())
}
