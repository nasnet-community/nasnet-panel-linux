// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"strings"
	"testing"
)

func TestVersionsOrderTheWayRouterOSDoes(t *testing.T) {
	// a is older than b in every row, so each is checked both ways round
	for _, c := range [][2]string{
		{"7.9", "7.11"}, // numbers, not text: 9 comes before 11
		{"7.11", "7.11.1"},
		{"7.11.1", "7.11.2"},
		{"7.11.9", "7.12"},
		{"7.11", "7.24.5"},
		{"6.49.10", "7.11"},
		{"7.99", "8.0"},
		{"7.12beta3", "7.12beta10"}, // the number after beta, not its text
		{"7.12alpha2", "7.12beta1"},
		{"7.12beta3", "7.12rc1"},
		{"7.12rc1", "7.12rc2"},
		{"7.12rc2", "7.12"}, // a candidate comes before its release
		{"7.12beta3", "7.12"},
		{"7.11", "7.12beta1"}, // but after the release before it
		{"7.11.5", "7.12alpha1"},
		{"7.12rc1", "7.12.1"},
		{"7.12.1rc1", "7.12.1"},
		{"7.12", "7.12.1rc1"},
		{"7.24.5 (stable)", "7.24.6"},
		{"7.12rc1 (testing)", "7.12 (stable)"},
	} {
		older, newer := c[0], c[1]
		if got, err := CompareVersions(older, newer); err != nil || got != -1 {
			t.Errorf("Compare(%q, %q) = %d, %v; want -1", older, newer, got, err)
		}
		if got, err := CompareVersions(newer, older); err != nil || got != 1 {
			t.Errorf("Compare(%q, %q) = %d, %v; want 1", newer, older, got, err)
		}
		if AtLeast(older, newer) || !AtLeast(newer, older) {
			t.Errorf("AtLeast disagrees about %q and %q", older, newer)
		}
	}
}

func TestVersionsThatAreTheSame(t *testing.T) {
	for _, c := range [][2]string{
		{"7.11", "7.11"},
		{"7.11", "7.11.0"},
		{"7.11.0", "7.11.0.0"},
		{"7.24.5", "7.24.5 (stable)"},
		{"7.24.5", "  7.24.5\n"},
		{"7.12rc1", "7.12RC1"},
		{"7.12rc1", "7.12.0rc1"},
		{"7.12rc1", "7.12rc1 (testing)"},
		{"7.12beta3", "7.12beta3 (development)"},
		{"7.24.5 (long-term)", "7.24.5 (stable)"},
	} {
		if got, err := CompareVersions(c[0], c[1]); err != nil || got != 0 {
			t.Errorf("Compare(%q, %q) = %d, %v; want 0", c[0], c[1], got, err)
		}
		if !AtLeast(c[0], c[1]) || !AtLeast(c[1], c[0]) {
			t.Errorf("AtLeast says %q and %q differ", c[0], c[1])
		}
	}
}

func TestTheMinimumIsHeldTo(t *testing.T) {
	for have, want := range map[string]bool{
		"7.11":              true,
		"7.11.0":            true,
		"7.11.1":            true,
		"7.24.5":            true,
		"7.24.5 (stable)":   true,
		"7.12rc1":           true, // a candidate of the next release is past 7.11
		"7.12beta3":         true,
		"8.0":               true,
		"7.11rc1":           false, // a candidate of 7.11 is not 7.11
		"7.11beta2":         false,
		"7.10":              false,
		"7.10.2":            false,
		"7.9":               false,
		"6.49.10":           false,
		"6.49.10 (stable)":  false,
		"":                  false,
		"banana":            false,
		"7":                 false,
		"7.x":               false,
		"7.11 (stable) foo": false,
	} {
		if got := AtLeast(have, MinRouterOS); got != want {
			t.Errorf("AtLeast(%q, %q) = %v, want %v", have, MinRouterOS, got, want)
		}
	}
}

func TestGarbageIsNotAVersion(t *testing.T) {
	for _, s := range []string{
		"", " ", "\n", "7", "7.", ".7", "7..11", "7.11.", "7.11..1", ".",
		"banana", "v7.11", "7.x", "7.11x", "7.11-rc1", "7.11 rc1", "7.11rc", "7.11rc-1", "7.11rc1.2",
		"7.11beta", "7.11gamma1", "7.11 (stable", "7.11 stable)", "7.11 ()", "7.11 (Stable1)", "7.11 (stable) (testing)",
		"7.11\x007", "7,11", "7.1 1", "-7.11", "+7.11", "7.11.1.2.3",
		"99999999999999999999.1", "7." + strings.Repeat("9", 7), "7.11rc" + strings.Repeat("9", 7),
		"٧.١١", // other scripts' digits
	} {
		if _, err := CompareVersions(s, "7.11"); err == nil {
			t.Errorf("%q was taken for a version", s)
		}
		if _, err := CompareVersions("7.11", s); err == nil {
			t.Errorf("%q was taken for a version as the second one", s)
		}
		if AtLeast(s, "7.11") || AtLeast("99.0", s) {
			t.Errorf("AtLeast let %q through", s)
		}
	}
}
