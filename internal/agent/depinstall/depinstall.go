// Package depinstall is the agent-side engine that probes for, and installs,
// the binaries the tunnel cores need (internal/coredeps). It runs on the node
// itself, in the agent on remote nodes and in process on the built-in node.
//
// Installs are slow, so an install runs as a background job: StartInstall
// returns a job id at once and Progress reports step states and new log lines.
package depinstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/coredeps"
)

// InstallRequest is the decoded input to StartInstall.
type InstallRequest struct {
	Cores []coredeps.Protocol
	// Uninstall reverses the operation: remove the cores' binaries instead of
	// installing them.
	Uninstall bool
}

// Manager owns the install jobs and probe logic for one node agent.
type Manager struct {
	client *http.Client
	now    func() time.Time

	mu   sync.Mutex
	jobs map[string]*Job
}

// New builds a Manager that downloads through client (nil = a plain client).
// On the built-in node the panel hands in its egress-aware client, so the
// download follows the proxy settings and, in router mode, the foreign uplinks.
func New(client *http.Client) *Manager {
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}
	return &Manager{client: client, now: time.Now, jobs: make(map[string]*Job)}
}

// Probe evaluates each requested protocol's manifest against the live host. An
// empty cores slice probes every protocol.
func (m *Manager) Probe(cores []coredeps.Protocol) []CoreReport {
	if len(cores) == 0 {
		cores = coredeps.Protocols()
	}
	reports := make([]CoreReport, 0, len(cores))
	for _, p := range cores {
		deps := coredeps.For(p)
		rep := CoreReport{CoreType: string(p), Ready: true, Deps: make([]coredeps.ProbeResult, 0, len(deps))}
		for _, d := range deps {
			res := probeOne(d)
			if !res.Present && !d.Optional {
				rep.Ready = false
			}
			rep.Deps = append(rep.Deps, res)
		}
		reports = append(reports, rep)
	}
	return reports
}

// CoreReport is the probe outcome for one protocol.
type CoreReport struct {
	CoreType string                 `json:"core_type"`
	Ready    bool                   `json:"ready"`
	Deps     []coredeps.ProbeResult `json:"deps"`
}

func probeOne(d coredeps.Dependency) coredeps.ProbeResult {
	res := coredeps.ProbeResult{Name: d.Name, Kind: d.Kind, Optional: d.Optional}
	path, ok := d.Probe.Resolve()
	res.Present = ok
	if ok {
		res.Path = path
		res.Version = d.Probe.Version()
	}
	return res
}

// StartInstall creates and launches a background install job, returning its id.
func (m *Manager) StartInstall(req InstallRequest) string {
	job := newJob(newJobID(), req, m.now())

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()

	go m.run(context.Background(), job)
	return job.ID
}

// Progress returns a snapshot of a job's state and any log lines with index
// >= sinceCursor. ok is false for an unknown job id.
func (m *Manager) Progress(jobID string, sinceCursor int) (Snapshot, bool) {
	m.mu.Lock()
	job := m.jobs[jobID]
	m.mu.Unlock()
	if job == nil {
		return Snapshot{}, false
	}
	return job.snapshot(sinceCursor), true
}

func newJobID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "dep-" + hex.EncodeToString(b[:])
}
