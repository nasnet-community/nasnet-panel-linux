// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"fmt"
	"strconv"
	"strings"
)

// MinRouterOS is the oldest RouterOS the plugin runs on: the first with every
// container setting it uses (a veth with several addresses, mount lists, and
// the memory limits). It is the same number as the manifest's minRouterOS.
const MinRouterOS = "7.20"

// The order of what comes before a release, lowest first. A release is 0.
const (
	stageAlpha = iota - 3
	stageBeta
	stageRC
)

// version is a RouterOS version such as 7.24.5, 7.12rc1 or 7.12beta3.
type version struct {
	nums  []int // 7.24.5 is [7 24 5]
	stage int   // 0 for the release, negative before it
	pre   int   // the 1 of rc1
}

// CompareVersions says whether RouterOS version a is older than b (-1), the
// same (0) or newer (1). A release candidate or a beta comes before the
// release it leads up to, and 7.11 is the same as 7.11.0. It refuses anything
// that isn't a version, so a garbled report can't pass for a new enough one.
func CompareVersions(a, b string) (int, error) {
	va, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < max(len(va.nums), len(vb.nums)); i++ {
		if c := cmp3(va.num(i), vb.num(i)); c != 0 {
			return c, nil
		}
	}
	if c := cmp3(va.stage, vb.stage); c != 0 {
		return c, nil
	}
	return cmp3(va.pre, vb.pre), nil
}

// AtLeast is whether have is need or newer. Either one being unreadable makes
// it false.
func AtLeast(have, need string) bool {
	c, err := CompareVersions(have, need)
	return err == nil && c >= 0
}

func (v version) num(i int) int {
	if i < len(v.nums) {
		return v.nums[i]
	}
	return 0
}

func cmp3(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func parseVersion(s string) (version, error) {
	bad := fmt.Errorf("routerfacts: %q is not a RouterOS version", s)
	f := strings.Fields(s)
	// /system resource prints the release channel after the number, as in
	// "7.24.5 (stable)"
	if len(f) == 2 && channel(f[1]) {
		f = f[:1]
	}
	if len(f) != 1 {
		return version{}, bad
	}
	s = f[0]

	// the dotted numbers first; what follows them is the rc1 or beta3
	isNum := func(r rune) bool { return r == '.' || r >= '0' && r <= '9' }
	nums, tag := s, ""
	if i := strings.IndexFunc(s, func(r rune) bool { return !isNum(r) }); i >= 0 {
		nums, tag = s[:i], s[i:]
	}

	parts := strings.Split(nums, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return version{}, bad
	}
	var v version
	for _, p := range parts {
		n, ok := number(p)
		if !ok {
			return version{}, bad
		}
		v.nums = append(v.nums, n)
	}
	if tag == "" {
		return v, nil
	}
	name := strings.TrimRightFunc(tag, func(r rune) bool { return r >= '0' && r <= '9' })
	n, ok := number(tag[len(name):])
	if !ok {
		return version{}, bad
	}
	switch strings.ToLower(name) {
	case "alpha":
		v.stage = stageAlpha
	case "beta":
		v.stage = stageBeta
	case "rc":
		v.stage = stageRC
	default:
		return version{}, bad
	}
	v.pre = n
	return v, nil
}

// number reads a run of digits that is neither empty nor long enough to
// overflow.
func number(s string) (int, bool) {
	if s == "" || len(s) > 6 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// channel is the "(stable)" of "7.24.5 (stable)": one word in brackets.
func channel(s string) bool {
	if len(s) < 3 || s[0] != '(' || s[len(s)-1] != ')' {
		return false
	}
	for _, r := range s[1 : len(s)-1] {
		if !(r >= 'a' && r <= 'z' || r == '-') {
			return false
		}
	}
	return true
}
