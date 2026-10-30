// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/nasnet-community/nasnet-panel-linux"

// The hub links the supervisor and the spec, so neither may bring the Xray
// core, or anything that does, into the hub: that is the hub's import budget
// (14 MB, against 19 MB for the bridge, which does carry the core). The
// supervisor itself needs the standard library and the spec, no more.
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
	allowed := map[string]bool{
		modulePath + "/internal/remotenodes/bridgectl":  true,
		modulePath + "/internal/remotenodes/bridgespec": true,
	}
	for _, target := range []struct{ goos, goarch string }{{}, {"linux", "arm64"}, {"linux", "amd64"}} {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}",
			modulePath+"/internal/remotenodes/bridgespec", modulePath+"/internal/remotenodes/bridgectl")
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
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			path, std, _ := strings.Cut(line, " ")
			for _, bad := range forbidden {
				if strings.HasPrefix(path, bad) {
					t.Errorf("%s: the hub would link %s", where, path)
				}
			}
			if std != "true" && !allowed[path] {
				t.Errorf("%s: %s is neither the standard library nor the spec", where, path)
			}
		}
	}
}
