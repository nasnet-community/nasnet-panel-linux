package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// bundledAgentPath is the binary AutoUpdateAgent pushes to a platform.
func bundledAgentPath(osName, arch string) string {
	return filepath.Join(agentBinaryDir, fmt.Sprintf("nasnet-agent-%s-%s", strings.ToLower(osName), normalizeArch(strings.ToLower(arch))))
}

type fileSum struct {
	size int64
	mod  time.Time
	sum  string
}

var bundledSums struct {
	mu sync.Mutex
	m  map[string]fileSum
}

// bundledAgentSum is the SHA-256 of a shipped agent ("" when there is none).
// Pages poll this, so a file is hashed again only once it changes.
func bundledAgentSum(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	bundledSums.mu.Lock()
	defer bundledSums.mu.Unlock()
	if c, ok := bundledSums.m[path]; ok && c.size == fi.Size() && c.mod.Equal(fi.ModTime()) {
		return c.sum
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if bundledSums.m == nil {
		bundledSums.m = map[string]fileSum{}
	}
	bundledSums.m[path] = fileSum{size: fi.Size(), mod: fi.ModTime(), sum: sum}
	return sum
}

var describeTail = regexp.MustCompile(`-(\d+)-g[0-9a-f]+$`)

// describeOrder reads a version the way git describe writes it (v1.1.0,
// v1.2.0-rc1, v1.1.0-58-g1d8940b, any of them -dirty): the release, whether it
// is a pre-release, then how many commits past it. "dev" and the like aren't.
func describeOrder(v string) (o [5]int, ok bool) {
	v = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-dirty")
	if m := describeTail.FindStringSubmatch(v); m != nil {
		o[4], _ = strconv.Atoi(m[1])
		v = v[:len(v)-len(m[0])]
	}
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return o, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return o, false
		}
		o[i] = n
	}
	if pre == "" {
		o[3] = 1 // a release sorts after its pre-releases
	}
	return o, true
}

// agentBuildOlder reports a < b; versions it can't read are never older.
func agentBuildOlder(a, b string) bool {
	x, okA := describeOrder(a)
	y, okB := describeOrder(b)
	if !okA || !okB {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

// agentUpdateDue: the hub ships a build the node doesn't run, and not an older
// one. Builds decide, since dev builds can share a version; a node that never
// named its build goes by version.
func agentUpdateDue(running, runningBuild, bundled, bundledBuild string) bool {
	switch {
	case bundledBuild == "", running == "" && runningBuild == "":
		return false // nothing to install, or nothing to compare it with
	case agentBuildOlder(bundled, running):
		return false // that would be a downgrade
	case runningBuild != "":
		return !strings.EqualFold(runningBuild, bundledBuild)
	}
	return running != bundled
}

// agentUpdate is whether a node can take the agent this hub ships, with the
// build it runs and the one it would get.
func (u *nodeUsecase) agentUpdate(node *domain.Node, running string) (due bool, build, bundledBuild string) {
	if node.IsLocal() || node.IsLite() || node.LastHostInfo == nil {
		return false, "", ""
	}
	build = u.agentBuild(node.ID)
	bundledBuild = bundledAgentSum(bundledAgentPath(node.LastHostInfo.OS, node.LastHostInfo.Arch))
	return agentUpdateDue(running, build, bundledAgentVersion(), bundledBuild), build, bundledBuild
}

// AgentUpdates says, for each remote full node, whether the hub ships an agent it doesn't run.
func (u *nodeUsecase) AgentUpdates(ctx context.Context) (map[uint]bool, error) {
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uint]bool, len(nodes))
	for _, n := range nodes {
		if n.IsLocal() || n.IsLite() {
			continue
		}
		out[n.ID], _, _ = u.agentUpdate(n, u.runningAgentVersion(n))
	}
	return out, nil
}

// runningAgentVersion is the version the node last reported.
func (u *nodeUsecase) runningAgentVersion(node *domain.Node) string {
	if snap, _, ok := u.liveStats.get(node.ID); ok && snap.AgentVersion != "" {
		return snap.AgentVersion
	}
	u.versionCacheMu.Lock()
	defer u.versionCacheMu.Unlock()
	if v := u.versionCache[node.ID].agentVersion; v != "" {
		return v
	}
	return node.AgentVersion
}
