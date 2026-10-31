// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// hidden stands where a secret was.
const hidden = "…"

// minLine is how short a line of a multi-line secret may be and still be
// hidden by itself: a shorter one would blot out ordinary words.
const minLine = 8

// hider takes secrets out of text.
type hider struct{ words []string } // longest first

// newHider makes a hider for a job's secrets, or nil if there is nothing to
// hide. Blank ones are skipped: there is nothing in them to protect, and
// hiding them would turn every space into an ellipsis.
//
// An error may quote a secret whole, trimmed of its last newline, or, for a
// key, by the line, so each of those is hidden. A secret that is part of
// another goes after it, or the longer one would be left in pieces.
func newHider(secrets []string) *hider {
	var words []string
	add := func(w string) {
		if w != "" && !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	for _, s := range secrets {
		if strings.TrimSpace(s) == "" {
			continue
		}
		add(s)
		add(strings.TrimSpace(s))
		if strings.Contains(s, "\n") {
			for _, line := range strings.Split(s, "\n") {
				if line = strings.TrimSpace(line); utf8.RuneCountInString(line) >= minLine {
					add(line)
				}
			}
		}
	}
	if len(words) == 0 {
		return nil
	}
	slices.SortStableFunc(words, func(a, b string) int { return len(b) - len(a) })
	return &hider{words}
}

// Hide takes the secrets out of s the way a job's texts have them taken out,
// for text that goes somewhere else, such as a record of how a job failed.
func Hide(s string, secrets []string) string {
	if h := newHider(secrets); h != nil {
		return h.hide(s)
	}
	return s
}

func (h *hider) hide(s string) string {
	for _, w := range h.words {
		s = strings.ReplaceAll(s, w, hidden)
	}
	return s
}

// text is s as a job may show it: the secrets taken out first, so that the
// cut can't leave half of one at the end. The caller holds the engine's lock.
func (j *job) text(s string, max int) string {
	if j.hide != nil {
		s = j.hide.hide(s)
	}
	return clip(s, max)
}

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
