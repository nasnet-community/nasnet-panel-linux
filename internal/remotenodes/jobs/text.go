// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"strings"
	"unicode/utf8"
)

// text is s as a job may show it. The caller holds the engine's lock.
func (j *job) text(s string, max int) string { return clip(s, max) }

// clip trims s and cuts it to at most max runes, ending in an ellipsis when it
// had to cut. Bytes that are not UTF-8 become U+FFFD first, so a cut can't
// land inside a character and a page never gets text it can't decode.
func clip(s string, max int) string {
	s = strings.ToValidUTF8(strings.TrimSpace(s), "\uFFFD")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	end := 0
	for n := 0; n < max-1; n++ {
		_, w := utf8.DecodeRuneInString(s[end:])
		end += w
	}
	return s[:end] + "…"
}
