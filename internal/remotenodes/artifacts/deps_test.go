// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/nasnet-community/nasnet-panel-linux"

// The hub imports this package and runs in a router's container, where size
// counts. It takes the standard library and three light packages of the
// module (the release manifest, its signing keys and the Xray pin), so the Xray
// core, sing-box, the agent client, the panel's database and its geoip data
// can't come in through it.
func TestArtifactsImportOnlyWhatItIsAllowed(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	want := []string{
		module + "/internal/remotenodes/artifacts",
		module + "/internal/xray/pin",
		module + "/pkg/release",
		module + "/pkg/signing",
	}
	for _, goarch := range []string{"amd64", "arm64"} {
		cmd := exec.Command(gobin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".")
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			var stderr []byte
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = ee.Stderr
			}
			t.Fatalf("go list -deps: %v\n%s", err, stderr)
		}
		got := strings.Fields(string(out))
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("linux/%s: the package links %v, want only %v", goarch, got, want)
		}
	}
}
