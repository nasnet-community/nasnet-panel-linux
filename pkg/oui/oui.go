// Package oui resolves a MAC address to its registered vendor, and answers the
// two structural questions about a MAC that a device list needs: what is its
// canonical form, and is it randomized.
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"net"
	"strings"
	"sync"
)

// The IEEE MA-L registry, compiled by pkg/oui/gen. Embedded unconditionally: a
// build tag would leave most builds with no vendor column, which reads as a
// broken lookup rather than a missing feature.
//
//go:embed oui.tsv.gz
var tableGz []byte

var (
	once  sync.Once
	table map[uint32]string
)

// Normalize returns the canonical lowercase colon form, or "" if s is not a
// 6-octet MAC. Every MAC we store or compare goes through here.
func Normalize(s string) string {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) != 6 {
		return ""
	}
	return hw.String()
}

// IsRandomized reports the locally-administered bit. Phones rotate these per
// network, so such a MAC names a session, not a device.
func IsRandomized(s string) bool {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) == 0 {
		return false
	}
	return hw[0]&0x02 != 0
}

// IsGroup reports the multicast bit. Group addresses are never devices, and the
// bridge FDB is full of them.
func IsGroup(s string) bool {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) == 0 {
		return false
	}
	return hw[0]&0x01 != 0
}

// Lookup returns the vendor registered for the MAC's 24-bit prefix.
//
// A randomized MAC never resolves: its prefix is locally chosen, so a hit would
// be coincidence reported as fact. MA-M and MA-S blocks are sub-allocations
// inside an MA-L /24, so those few resolve to the block holder rather than the
// device's real vendor.
func Lookup(s string) (string, bool) {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) != 6 || hw[0]&0x02 != 0 {
		return "", false
	}
	once.Do(load)
	v, ok := table[uint32(hw[0])<<16|uint32(hw[1])<<8|uint32(hw[2])]
	return v, ok
}

func load() {
	table = map[uint32]string{}
	zr, err := gzip.NewReader(bytes.NewReader(tableGz))
	if err != nil {
		return
	}
	defer zr.Close()

	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		prefix, name, ok := strings.Cut(sc.Text(), "\t")
		if !ok || len(prefix) != 6 {
			continue
		}
		var v uint32
		bad := false
		for i := 0; i < 6 && !bad; i++ {
			d := hexVal(prefix[i])
			if d < 0 {
				bad = true
				break
			}
			v = v<<4 | uint32(d)
		}
		if !bad {
			table[v] = name
		}
	}
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}
