// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/nasnet-community/nasnet-panel-linux"

// listDeps returns every package the bridge program links, for a target.
func listDeps(t *testing.T, goos, goarch string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", modulePath+"/cmd/remote-nodes-bridge")
	cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0")
	if goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+goarch)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	return strings.Fields(string(out))
}

// The bridge carries the Xray core and nothing of the hub: not the agent's
// client or its SSH, not the database layer, not the panel's own Xray package
// or the geo data it embeds. Each of those would grow the binary on a router
// with 128 MB of flash, and none of them belongs in a process that handles
// the subscriptions' traffic.
func TestBridgeImportBudget(t *testing.T) {
	forbidden := []string{
		modulePath + "/pkg/agent",
		modulePath + "/pkg/xray",
		modulePath + "/pkg/geoip",
		modulePath + "/pkg/geofiles",
		modulePath + "/internal/node",
		"golang.org/x/crypto/ssh",
		"gorm.io/",
		"github.com/sagernet/",
		"google.golang.org/grpc/credentials/",
	}
	// Of this module's code the bridge links the spec and nothing else.
	allowed := map[string]bool{
		modulePath + "/cmd/remote-nodes-bridge":         true,
		modulePath + "/internal/remotenodes/bridgespec": true,
	}
	for _, target := range []struct{ goos, goarch string }{{}, {"linux", "arm64"}, {"linux", "amd64"}} {
		name := target.goos + "/" + target.goarch
		if target.goos == "" {
			name = "this machine"
		}
		t.Run(name, func(t *testing.T) {
			for _, dep := range listDeps(t, target.goos, target.goarch) {
				for _, bad := range forbidden {
					if strings.HasPrefix(dep, bad) {
						t.Errorf("the bridge links %s", dep)
					}
				}
				if strings.HasPrefix(dep, modulePath+"/") && !allowed[dep] {
					t.Errorf("the bridge links %s, which is this module's code but not the spec", dep)
				}
			}
		})
	}
}

// The binary's size was the reason to build the config by hand: xray-core's
// JSON loader (infra/conf) and the other front ends pull in nearly the whole
// core. They may be imported in tests, never in the program.
func TestBridgeHasNoConfigLoader(t *testing.T) {
	for _, dep := range listDeps(t, "linux", "arm64") {
		for _, bad := range []string{
			"github.com/xtls/xray-core/infra/",
			"github.com/xtls/xray-core/main/distro",
			"github.com/xtls/xray-core/main/commands",
			"github.com/xtls/xray-core/main/json",
			"github.com/xtls/xray-core/main/toml",
			"github.com/xtls/xray-core/main/yaml",
			"github.com/xtls/xray-core/app/commander",
			"github.com/xtls/xray-core/app/metrics",
		} {
			if strings.HasPrefix(dep, bad) {
				t.Errorf("the bridge links %s", dep)
			}
		}
	}
}
