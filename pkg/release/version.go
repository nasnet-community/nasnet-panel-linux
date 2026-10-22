// Package release reads the signed files a panel release ships and orders versions.
package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a release tag: vMAJOR.MINOR.PATCH with an optional prerelease.
type Version struct {
	Major, Minor, Patch int
	Pre                 []string
}

var (
	tagRE      = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	describeRE = regexp.MustCompile(`^\d+-g[0-9a-f]{7,}$`)
)

// Parse reads a tag. Git-describe output and "dev" aren't tags.
func Parse(s string) (Version, error) {
	m := tagRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("%q isn't a release tag", s)
	}
	v := Version{}
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.Pre = strings.Split(m[4], ".")
		// git describe looks like v1.0.0-14-g3fa9c1e; a release never does
		if len(v.Pre) == 1 && describeRE.MatchString(m[4]) {
			return Version{}, fmt.Errorf("%q is a git build, not a release", s)
		}
	}
	return v, nil
}

// MustParse is Parse for constants in tests and tools.
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Stable is a release without a prerelease part.
func (v Version) Stable() bool { return len(v.Pre) == 0 }

// Compare orders by semver precedence: -1, 0 or 1.
func (v Version) Compare(o Version) int {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d != 0 {
			return sign(d)
		}
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		if c := comparePre(v.Pre[i], o.Pre[i]); c != 0 {
			return c
		}
	}
	return sign(len(v.Pre) - len(o.Pre))
}

// Newer reports whether v sorts after o.
func (v Version) Newer(o Version) bool { return v.Compare(o) > 0 }

func comparePre(a, b string) int {
	ai, aErr := strconv.Atoi(a)
	bi, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return sign(ai - bi)
	case aErr == nil:
		return -1 // numeric identifiers sort first
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// Pick is the newest tag above running that the channel offers: stable only, or
// stable and prereleases with beta on. Turning beta off never offers a way back.
func Pick(tags []string, running Version, beta bool) (Version, bool) {
	var best Version
	found := false
	for _, t := range tags {
		v, err := Parse(t)
		if err != nil || (!beta && !v.Stable()) || !v.Newer(running) {
			continue
		}
		if !found || v.Newer(best) {
			best, found = v, true
		}
	}
	return best, found
}
