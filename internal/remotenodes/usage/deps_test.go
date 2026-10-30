// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The hub links this package into a router container. The agent's client types
// are fine; the Xray core, the panel's database and the geoip data are not.
// Test files may import what they like; the package may not.
func TestPackageHasNoHeavyDependencies(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	list := exec.Command(goTool, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".")
	var stderr bytes.Buffer
	list.Stderr = &stderr
	out, err := list.Output()
	if err != nil {
		t.Skipf("can't ask the go tool for dependencies: %v\n%s", err, stderr.Bytes())
	}

	// the ones that would hurt, named so the failure says why it matters
	heavy := []string{
		"github.com/xtls/xray-core", "github.com/sagernet", "gvisor.dev", "gorm.io",
		"github.com/nasnet-community/nasnet-panel-linux/pkg/xray",
		"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip",
		"github.com/nasnet-community/nasnet-panel-linux/pkg/geofiles",
		"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain",
	}
	for _, p := range strings.Fields(string(out)) {
		for _, h := range heavy {
			if p == h || strings.HasPrefix(p, h+"/") {
				t.Errorf("the usage package depends on %s, which the hub must not link", p)
			}
		}
	}
}
