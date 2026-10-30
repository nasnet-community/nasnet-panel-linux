// SPDX-License-Identifier: AGPL-3.0-or-later

package pki

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/nasnet-community/nasnet-panel-linux"

// The hub imports this package and runs in a router's container, where size
// counts. It takes the standard library and nothing else, so the Xray core,
// sing-box, the panel's database and its geoip data can't come in through it.
func TestPKIImportsOnlyTheStandardLibrary(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
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
		if want := []string{module + "/internal/remotenodes/pki"}; !slices.Equal(got, want) {
			t.Errorf("linux/%s: the package links %v, want only %v", goarch, got, want)
		}
	}
}
