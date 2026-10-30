// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// The hub links this package into a router container and mustn't carry the
// panel's Xray, gorm or the geoip data along. Test files may import what they
// like; the package itself may not.
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

	self := reflect.TypeOf(nodecfg.Input{}).PkgPath()
	var extra []string
	for _, p := range strings.Fields(string(out)) {
		if p != self {
			extra = append(extra, p)
		}
	}

	// the ones that would hurt, named so the failure says why it matters
	heavy := []string{
		"github.com/xtls/xray-core", "github.com/sagernet", "gvisor.dev", "gorm.io",
		"github.com/nasnet-community/nasnet-panel-linux/pkg/xray",
		"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip",
		"github.com/nasnet-community/nasnet-panel-linux/pkg/geofiles",
		"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain",
	}
	for _, p := range extra {
		for _, h := range heavy {
			if p == h || strings.HasPrefix(p, h+"/") {
				t.Errorf("nodecfg depends on %s, which the hub must not link", p)
			}
		}
	}
	if len(extra) > 0 {
		t.Errorf("nodecfg is standard library only, but its dependencies include %v", extra)
	}
}
