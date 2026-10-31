// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"context"
	"net/http"
	"runtime/debug"
	"sync"
)

// Holder keeps one job's downloads in memory, so that the second node of an
// architecture gets what the first one fetched and the file is downloaded once.
// Nothing goes to disk. Call Release when the job is over.
//
// The agent is looked up by release as well as architecture, so a release
// published in the middle of a job is fetched for the nodes after it, and the
// ones that came before keep the one they had.
type Holder struct {
	c   *http.Client
	src Source
	f   fetcher

	// gate lets one download run at a time, which holds the memory to one
	// file in flight, and a caller waiting on it can still give up.
	gate chan struct{}
	mu   sync.Mutex // guards held
	held map[heldKey]any
}

type heldKey struct{ kind, arch, version string }

func NewHolder(c *http.Client, src Source) *Holder {
	return &Holder{c: c, src: src, f: defaults(), gate: make(chan struct{}, 1), held: map[heldKey]any{}}
}

// Agent is FetchAgent, except that an agent already held is handed back. The
// small release.json is read every time to learn which release is current;
// the agent itself is not.
func (h *Holder) Agent(ctx context.Context, arch string) (*Agent, error) {
	e, err := h.f.entry(ctx, h.c, h.src, arch)
	if err != nil {
		return nil, err
	}
	v, err := h.once(ctx, heldKey{"agent", arch, e.version}, func() (any, error) {
		return h.f.downloadAgent(ctx, h.c, h.src, e)
	})
	if err != nil {
		return nil, err
	}
	return v.(*Agent), nil
}

// LatestAgent is the version of the agent the release carries for arch,
// such as "v1.3.1", read from release.json and its signature alone. No agent
// is downloaded and nothing is held, so it is cheap enough to ask now and
// then, which is how the hub finds out that a node is behind.
//
// The checks are Agent's: a release that isn't signed by a trusted key
// (ErrManifest), is older than MinAgentVersion (ErrAgentTooOld) or has no
// gzipped agent for arch (ErrNoGzAgents) is an error here too, since the hub
// couldn't push it.
func (h *Holder) LatestAgent(ctx context.Context, arch string) (string, error) {
	e, err := h.f.entry(ctx, h.c, h.src, arch)
	if err != nil {
		return "", err
	}
	return e.version, nil
}

// Xray is FetchXray, except that a build already held is handed back.
func (h *Holder) Xray(ctx context.Context, arch string) (*Xray, error) {
	if err := checkArch(arch); err != nil {
		return nil, err
	}
	v, err := h.once(ctx, heldKey{"xray", arch, h.f.pin.version}, func() (any, error) {
		return h.f.xray(ctx, h.c, arch)
	})
	if err != nil {
		return nil, err
	}
	return v.(*Xray), nil
}

// once returns what is held under k, or runs load and holds its result. A
// failure holds nothing, so the next call tries again.
func (h *Holder) once(ctx context.Context, k heldKey, load func() (any, error)) (any, error) {
	if v, ok := h.get(k); ok {
		return v, nil
	}
	select {
	case h.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-h.gate }()
	if v, ok := h.get(k); ok { // another caller fetched it while this one waited
		return v, nil
	}
	v, err := load()
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.held[k] = v
	h.mu.Unlock()
	return v, nil
}

func (h *Holder) get(k heldKey) (any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.held[k]
	return v, ok
}

// Release lets go of everything held and hands the memory back to the system,
// since a router has little to spare and the next job is not soon.
func (h *Holder) Release() {
	h.mu.Lock()
	had := len(h.held) > 0
	h.held = map[heldKey]any{}
	h.mu.Unlock()
	if had {
		debug.FreeOSMemory()
	}
}
