package mtprotonode

import "net/netip"

// Ranges answers whether an address is inside the panel's local address ranges.
type Ranges interface {
	Contains(netip.Addr) bool
}

type prefixRanges []netip.Prefix

func (p prefixRanges) Contains(a netip.Addr) bool {
	a = a.Unmap()
	for _, pf := range p {
		if pf.Contains(a) {
			return true
		}
	}
	return false
}

// PrefixRanges is nil when nothing parses: an empty set would put every site outside.
func PrefixRanges(lists ...[]string) Ranges {
	var out prefixRanges
	for _, l := range lists {
		for _, s := range l {
			if p, err := netip.ParsePrefix(s); err == nil {
				out = append(out, p.Masked())
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// domestic is false once one address is outside: a warning, not a block.
func domestic(r Ranges, addrs []string) *bool {
	if r == nil {
		return nil
	}
	known, in := false, true
	for _, s := range addrs {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		known = true
		if !r.Contains(a) {
			in = false
			break
		}
	}
	if !known {
		return nil
	}
	return &in
}
