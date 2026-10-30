// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The hub runs on a router with little memory and flash, and links this
// package. It talks to nodes over gRPC and SSH, and must not carry the Xray
// core, sing-box, gvisor, the panel's database or its Xray layer and geoip
// data along. Nor may it link the agent's own code: that is what package
// agentcheck is for, which holds the tests that run the real agent.

const module = "github.com/nasnet-community/nasnet-panel-linux"

type ban struct {
	why   string
	match func(importPath string) bool
	// insurance: nothing in the module links it today, so the canary below
	// doesn't ask for a live match.
	insurance bool
}

func under(prefix string) func(string) bool {
	return func(p string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }
}

func fragment(s string) func(string) bool {
	return func(p string) bool { return strings.Contains(p, s) }
}

var bans = []ban{
	{"xray-core", fragment("xtls/xray-core"), false},
	{"sing-box", fragment("sagernet"), false},
	{"gvisor", fragment("gvisor"), true},
	{"gorm", fragment("gorm"), false},
	{"pkg/xray, the panel's Xray layer", under(module + "/pkg/xray"), false},
	{"pkg/geoip, which embeds the geoip data", under(module + "/pkg/geoip"), false},
	{"internal/node/domain, which brings in gorm", under(module + "/internal/node/domain"), false},
	{"the agent itself", under(module + "/internal/agent"), false},
}

// graph is the import graph go list reports: package to what it imports.
type graph map[string][]string

// chain is the shortest way from root to a package the ban matches, or nil.
func (g graph) chain(root string, b ban) []string {
	prev := map[string]string{root: ""}
	queue := []string{root}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if b.match(p) {
			var path []string
			for ; p != ""; p = prev[p] {
				path = append([]string{p}, path...)
			}
			return path
		}
		for _, imp := range g[p] {
			if _, seen := prev[imp]; !seen {
				prev[imp] = p
				queue = append(queue, imp)
			}
		}
	}
	return nil
}

func parseGraph(out string) graph {
	g := graph{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			g[f[0]] = f[1:]
		}
	}
	return g
}

// deps is what go list -deps says pkg links for a Linux build, which is what
// the image holds. -e keeps it going past a package that can't be built here.
func deps(t *testing.T, goarch, pkg string) graph {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool on PATH")
	}
	out, err := exec.Command(gobin, "env", "GOMOD").Output()
	gomod := strings.TrimSpace(string(out))
	if err != nil || gomod == "" || gomod == os.DevNull {
		t.Skip("not inside a Go module")
	}
	cmd := exec.Command(gobin, "list", "-e", "-deps", "-f", `{{.ImportPath}} {{join .Imports " "}}`, pkg)
	cmd.Dir = filepath.Dir(gomod)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
	if out, err = cmd.Output(); err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, stderr)
	}
	return parseGraph(string(out))
}

func TestAgentopsStaysLight(t *testing.T) {
	self := module + "/internal/remotenodes/agentops"
	for _, goarch := range []string{"amd64", "arm64"} {
		g := deps(t, goarch, self)
		if _, ok := g[self]; !ok {
			t.Fatalf("agentops is not in its own dependency list: the module path in this test is wrong")
		}
		for _, b := range bans {
			if c := g.chain(self, b); c != nil {
				t.Errorf("linux/%s: agentops links %s:\n\t%s", goarch, b.why, strings.Join(c, "\n\t-> "))
			}
		}
		// the stand-in agent is for tests, so it stays out of what the hub links
		if c := g.chain(self, ban{why: "fakeagent", match: under(self + "/fakeagent")}); c != nil {
			t.Errorf("linux/%s: agentops links the test agent: %v", goarch, c)
		}
	}
}

func TestTheChainShowsHowAPackageGotIn(t *testing.T) {
	g := graph{
		"hub":                       {"net/http", "hub/api"},
		"hub/api":                   {"hub/store", "helper"},
		"helper":                    {"example.com/x/gorm/driver"},
		"hub/store":                 {"os"},
		"example.com/x/gorm/driver": {"database/sql"},
	}
	gorm := ban{why: "gorm", match: fragment("gorm")}
	want := "hub -> hub/api -> helper -> example.com/x/gorm/driver"
	if got := strings.Join(g.chain("hub", gorm), " -> "); got != want {
		t.Fatalf("chain = %q, want %q", got, want)
	}
	if c := g.chain("hub", ban{why: "nothing", match: fragment("sagernet")}); c != nil {
		t.Fatalf("a chain to nothing: %v", c)
	}
	if c := g.chain("hub/store", gorm); c != nil {
		t.Fatalf("a chain from a package that doesn't reach it: %v", c)
	}
	if !under("a/b")("a/b") || !under("a/b")("a/b/c") || under("a/b")("a/bc") || under("a/b")("a") {
		t.Fatal("under matches the wrong paths")
	}
}

// A ban that matches nothing real would let anything through. So each one is
// held against the agent and the panel's Xray layer, which do carry what they
// name.
func TestEveryBanMatchesSomethingReal(t *testing.T) {
	if testing.Short() {
		t.Skip("lists the dependencies of the heaviest packages")
	}
	heavy := map[string]graph{}
	for _, pkg := range []string{module + "/internal/agent/server", module + "/pkg/xray", module + "/pkg/geoip", module + "/internal/node/domain"} {
		heavy[pkg] = deps(t, "amd64", pkg)
	}
	for _, b := range bans {
		found := false
		for pkg, g := range heavy {
			found = found || g.chain(pkg, b) != nil
		}
		if !found && !b.insurance {
			t.Errorf("the ban on %s matches nothing in the heaviest packages: its pattern is probably out of date", b.why)
		}
	}
}
