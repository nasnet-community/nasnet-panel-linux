// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
)

// stepConstants are the string constants named Step... in the Go files of a
// package, by name, which is how the steps of agentops and of this package are
// declared. Files of tests are left out.
func stepConstants(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if name := f.Name(); !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, f.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if !strings.HasPrefix(id.Name, "Step") || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatal(err)
						}
						out[id.Name] = v
					}
				}
			}
		}
	}
	return out
}

// A step added without a key would show in English on the page whatever the
// language is. Every step constant of agentops and of this package has one, so
// adding a constant without a line in stepKeys fails here.
func TestEveryStepConstantHasAKey(t *testing.T) {
	names := map[string]bool{}
	for dir, least := range map[string]int{"../agentops": 16, ".": 4} {
		found := stepConstants(t, dir)
		if len(found) < least {
			t.Fatalf("found %d step constants in %s, expected at least %d: %v", len(found), dir, least, found)
		}
		for ident, name := range found {
			names[name] = true
			if StepKey(name) == "" {
				t.Errorf("%s (%q) has no key: add it to stepKeys", ident, name)
			}
		}
	}
	// and a key for a name that no constant has is a leftover
	for name := range stepKeys {
		if !names[name] {
			t.Errorf("stepKeys has %q, which no step constant is called", name)
		}
	}
}

func TestStepKeysAreSnakeCaseAndNeverShared(t *testing.T) {
	shape := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	byKey := map[string]string{}
	for name, key := range stepKeys {
		if !shape.MatchString(key) {
			t.Errorf("the key of %q is %q, which is not lower-case words joined by underscores", name, key)
		}
		if other, taken := byKey[key]; taken {
			t.Errorf("%q and %q share the key %q", other, name, key)
		}
		byKey[key] = name
	}
	if StepKey("Check the server") != "check_server" || StepKey("no such step") != "" {
		t.Errorf("StepKey gives %q and %q", StepKey("Check the server"), StepKey("no such step"))
	}
}

// Every plan is made of named steps, so a plan that lists a name of its own
// (say, a literal) is caught too.
func TestEveryPlannedStepHasAKey(t *testing.T) {
	for name, plan := range map[string][]string{
		"install":          agentops.InstallPlan(),
		"uninstall":        agentops.UninstallPlan(),
		"provision":        agentops.ProvisionPlan(),
		"update the agent": agentops.UpdateAgentPlan(),
		"update xray":      agentops.UpdateXrayPlan(),
		"the whole add":    installSteps(),
		"remove":           removeSteps(false),
		"forget":           removeSteps(true),
	} {
		if len(plan) == 0 {
			t.Errorf("%s: an empty plan", name)
		}
		for _, step := range plan {
			if StepKey(step) == "" {
				t.Errorf("%s: step %q has no key", name, step)
			}
		}
	}
}

// keyGuard is the job engine's StepKey in a test: the real one, which also
// notes each step it had no key for. A test that starts a job then fails for a
// step that was added without one, whichever way the step was reached.
type keyGuard struct {
	mu      sync.Mutex
	missing []string
}

func (g *keyGuard) key(name string) string {
	k := StepKey(name)
	if k == "" {
		g.mu.Lock()
		g.missing = append(g.missing, name)
		g.mu.Unlock()
	}
	return k
}

// guardKeys makes the guard for a test and has it checked when the test ends.
func guardKeys(t *testing.T) func(string) string {
	t.Helper()
	g := &keyGuard{}
	t.Cleanup(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if len(g.missing) > 0 {
			slices.Sort(g.missing)
			t.Errorf("job steps with no key (add them to stepKeys): %q", slices.Compact(g.missing))
		}
	})
	return g.key
}

func TestTheGuardNotesAStepWithNoKey(t *testing.T) {
	g := &keyGuard{}
	if g.key("Check the server") != "check_server" || len(g.missing) != 0 {
		t.Fatalf("a step with a key was noted: %v", g.missing)
	}
	if g.key("A step nobody gave a key") != "" || len(g.missing) != 1 {
		t.Fatalf("a step with no key was not noted: %v", g.missing)
	}
}
