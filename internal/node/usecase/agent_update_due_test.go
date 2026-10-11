package usecase

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

func TestAgentBuildOlderReadsGitDescribe(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		older bool
	}{
		{"v1.1.0", "v1.2.0", true},
		{"v1.1.0-58-g1d8940b", "v1.1.0-60-gabc1234", true},
		{"v1.1.0-60-gabc1234", "v1.1.0-58-g1d8940b", false},
		{"v1.1.0", "v1.1.0-1-gabc1234", true},
		{"v1.2.0-rc1", "v1.2.0", true},
		{"v1.1.0-58-g1d8940b-dirty", "v1.1.0-58-g1d8940b", false}, // the same release
		{"v1.1.0-58-g1d8940b", "v1.1.0-58-g1d8940b-dirty", false},
		{"1.1.0", "v1.1.0", false},
		{"dev", "v1.1.0", false}, // unreadable is never older
		{"v1.1.0", "dev", false},
		{"", "v1.1.0", false},
	} {
		if got := agentBuildOlder(c.a, c.b); got != c.older {
			t.Errorf("%q < %q = %v, want %v", c.a, c.b, got, c.older)
		}
	}
}

func TestAgentUpdateDueGoesByBuild(t *testing.T) {
	for _, c := range []struct {
		name                                 string
		running, runningBuild, bundled, ship string
		due                                  bool
	}{
		{"same build", "v1.1.0-58-g1", "aaa", "v1.1.0-58-g1", "aaa", false},
		{"another build of one version", "v1.1.0-58-g1", "aaa", "v1.1.0-58-g1", "bbb", true},
		{"newer release shipped", "v1.1.0", "aaa", "v1.2.0", "bbb", true},
		{"node is newer than the panel", "v1.2.0", "aaa", "v1.1.0", "bbb", false},
		{"nothing shipped for its platform", "v1.1.0", "aaa", "v1.2.0", "", false},
		{"no report yet", "", "", "v1.2.0", "bbb", false},
		{"no build named: by version", "v1.1.0", "", "v1.2.0", "bbb", true},
		{"no build named, same version", "v1.2.0", "", "v1.2.0", "bbb", false},
		{"build case does not matter", "dev", "ABC", "dev", "abc", false},
	} {
		if got := agentUpdateDue(c.running, c.runningBuild, c.bundled, c.ship); got != c.due {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.due)
		}
	}
}

type buildClient struct {
	agent.NodeClient
	version, build string
}

func (c *buildClient) GetVersion(context.Context) (*agent.VersionInfo, error) {
	return &agent.VersionInfo{AgentVersion: c.version}, nil
}
func (c *buildClient) GetSelfChecksum(context.Context) (*agent.ChecksumResult, error) {
	return &agent.ChecksumResult{Checksum: c.build}, nil
}

// The overview compares the binary a node runs with the one shipped for its platform.
func TestANodeIsDueOnlyForAnotherShippedBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(agentBinaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ship := func(body string) string {
		if err := os.WriteFile(filepath.Join(agentBinaryDir, "nasnet-agent-linux-amd64"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return bundledAgentSum(bundledAgentPath("linux", "x86_64"))
	}
	os.WriteFile(filepath.Join(agentBinaryDir, "VERSION"), []byte("v1.1.0-58-g1d8940b\n"), 0o644)
	bundledVersion.mu.Lock()
	bundledVersion.readAt = time.Time{}
	bundledVersion.mu.Unlock()
	t.Cleanup(func() {
		bundledVersion.mu.Lock()
		bundledVersion.readAt = time.Time{}
		bundledVersion.mu.Unlock()
	})

	first := ship("build one")
	if first == "" {
		t.Fatal("the shipped build has no hash")
	}
	u := &nodeUsecase{}
	node := &domain.Node{ID: 7, LastHostInfo: &domain.HostInfo{OS: "linux", Arch: "x86_64"}}
	u.agentVersions(context.Background(), &buildClient{version: "v1.1.0-58-g1d8940b", build: first}, node.ID, true)
	if due, build, shipped := u.agentUpdate(node, "v1.1.0-58-g1d8940b"); due || build != first || shipped != first {
		t.Fatalf("same build: due=%v build=%s shipped=%s", due, build, shipped)
	}

	// a rebuild at the same version is an update
	second := ship("build two, a bit longer")
	if due, _, shipped := u.agentUpdate(node, "v1.1.0-58-g1d8940b"); !due || shipped != second {
		t.Fatalf("rebuild: due=%v shipped=%s", due, shipped)
	}

	// once it is pushed the same update isn't offered again, and the next sweep asks the node
	u.notePushedAgent(node.ID, second)
	if due, _, _ := u.agentUpdate(node, "v1.1.0-58-g1d8940b"); due {
		t.Fatal("still due right after the update")
	}
	u.agentVersions(context.Background(), &buildClient{version: "v1.1.0-58-g1d8940b", build: second}, node.ID, false)
	if due, build, _ := u.agentUpdate(node, "v1.1.0-58-g1d8940b"); due || build != second {
		t.Fatalf("after the sweep: due=%v build=%s", due, build)
	}

	// no host info, no platform: nothing to offer
	if due, _, _ := u.agentUpdate(&domain.Node{ID: 8}, "v1.0.0"); due {
		t.Fatal("offered a build for an unknown platform")
	}
}
