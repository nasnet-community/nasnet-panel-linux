// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import (
	"os/exec"
	"strings"
	"testing"
)

// The hub imports this package to build specs and read the bridge's status.
// It must bring nothing with it: the standard library and itself.
func TestImportsStdlibOnly(t *testing.T) {
	for _, target := range []struct{ goos, goarch string }{{}, {"linux", "arm64"}, {"linux", "amd64"}} {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}",
			"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec")
		cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0")
		if target.goos != "" {
			cmd.Env = append(cmd.Env, "GOOS="+target.goos, "GOARCH="+target.goarch)
		}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			path, std, _ := strings.Cut(line, " ")
			if std != "true" && !strings.HasSuffix(path, "/internal/remotenodes/bridgespec") {
				t.Errorf("%s/%s: the spec package imports %s", target.goos, target.goarch, path)
			}
		}
	}
}
