// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/nasnet-community/nasnet-panel-linux"

// The hub links the engine, so it may not bring the Xray core, or anything
// that does, into the hub: that is the hub's import budget. The engine itself
// needs the standard library and nothing else.
func TestImportBudget(t *testing.T) {
	forbidden := []string{
		"github.com/xtls/xray-core",
		"github.com/sagernet/",
		"gvisor.dev/",
		"github.com/google/gvisor",
		"gorm.io/",
		modulePath + "/pkg/xray",
		modulePath + "/pkg/geoip",
		modulePath + "/pkg/agent",
		modulePath + "/internal/node/domain",
		"golang.org/x/crypto/ssh",
	}
	self := modulePath + "/internal/remotenodes/jobs"
	for _, target := range []struct{ goos, goarch string }{{}, {"linux", "arm64"}, {"linux", "amd64"}} {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", self)
		cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0")
		if target.goos != "" {
			cmd.Env = append(cmd.Env, "GOOS="+target.goos, "GOARCH="+target.goarch)
		}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}
		where := target.goos + "/" + target.goarch
		if target.goos == "" {
			where = "this machine"
		}
		found := false
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			path, std, _ := strings.Cut(line, " ")
			found = found || path == self
			for _, bad := range forbidden {
				if strings.HasPrefix(path, bad) {
					t.Errorf("%s: the hub would link %s", where, path)
				}
			}
			if std != "true" && path != self {
				t.Errorf("%s: %s is not the standard library", where, path)
			}
		}
		if !found {
			t.Fatalf("%s: the package is not in its own dependency list, so the module path in this test is wrong", where)
		}
	}
}
