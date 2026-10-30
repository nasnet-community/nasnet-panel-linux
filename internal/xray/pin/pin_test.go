package pin

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The panel links xray-core as a library, so go.mod has to name the commit
// the pinned release was tagged at. Left behind, it builds and tests against
// an Xray no node runs, and nothing else notices.
func TestGoModBuildsThePinnedCommit(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(Commit) {
		t.Fatalf("Commit %q is not a full commit hash", Commit)
	}
	mod, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*(?:require\s+)?github\.com/xtls/xray-core\s+(v\S+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod has no github.com/xtls/xray-core requirement")
	}
	want := "go get github.com/xtls/xray-core@" + Commit + " && go mod tidy"
	rev := regexp.MustCompile(`-([0-9a-f]{12})$`).FindSubmatch(m[1])
	if rev == nil {
		t.Fatalf("go.mod requires xray-core %s, not a pseudo-version of one commit; run: %s", m[1], want)
	}
	if !strings.HasPrefix(Commit, string(rev[1])) {
		t.Fatalf("go.mod builds xray-core at %s but Commit is %s (Version %s): bump Version, Commit and go.mod together; run: %s",
			rev[1], Commit, Version, want)
	}
}
