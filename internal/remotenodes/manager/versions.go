// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"regexp"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// dotted is a version number: digits and dots, with one dot at least, and
// maybe the v that a tag has.
var dotted = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)+$`)

// xrayVersionOf is the version number in what a node says its Xray is. The
// agent reports the first line of `xray version`, "Xray 26.10.21 (Xray,
// Penetrates Everything.) 1b1647c (go1.26.1 linux/amd64)", which is too long
// for the state file and has more in it than the version. A bare "26.10.21"
// or "v26.10.21" is taken as it is. It is "" when there is no version in it,
// as when the agent couldn't run xray and said "unknown".
func xrayVersionOf(s string) string {
	for _, f := range strings.Fields(s) {
		if dotted.MatchString(f) {
			return strings.TrimPrefix(f, "v")
		}
	}
	return ""
}

// agentVersionOf is an agent's version as it is kept: without a leading v, so
// that it reads the same as the release it came from, and as plain text that
// is short enough for the state file, which refuses anything else.
func agentVersionOf(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1 && s[0] == 'v' && s[1] >= '0' && s[1] <= '9' {
		s = s[1:]
	}
	var b strings.Builder
	for i := 0; i < len(s) && b.Len() < 64; i++ {
		if c := s[i]; c >= 0x20 && c <= 0x7e {
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// xrayState is how a node's Xray stands against the one this plugin installs.
// The bridge and the node's portal have to run the same build: a reverse
// tunnel promises nothing across versions.
type xrayState int

const (
	xrayUnknown  xrayState = iota // the node hasn't said, or said something that isn't a version
	xrayOK                        // it is the pin
	xrayMismatch                  // it is another
)

func (s xrayState) String() string {
	switch s {
	case xrayOK:
		return "ok"
	case xrayMismatch:
		return "mismatch"
	}
	return "unknown"
}

// checkXray compares a version, as xrayVersionOf made it, with the pin.
func checkXray(version string) xrayState {
	switch version {
	case "":
		return xrayUnknown
	case pin.Version:
		return xrayOK
	}
	return xrayMismatch
}
