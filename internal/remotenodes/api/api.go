// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api is the hub's HTTP API.
//
// The model holds secrets (the REALITY private key, client UUIDs, the pinned
// host key), so nothing in this package marshals a model type: views.go maps
// them to the shapes the page reads, field by field, and a test walks every
// GET body for anything that looks like a secret.
package api

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// Store is what the API uses of the state.
type Store interface {
	Read() model.State
	Writes() uint64
}

type Config struct {
	Store    Store
	Jobs     Jobs // where jobs are read from; nil means there are none
	Live     Live // what the manager knows of the nodes and the router; nil means nothing is running
	Version  string
	Commit   string
	BasePath string       // "" or "/a/b", already cleaned by ui.BasePath
	Index    http.Handler // the web page; nil serves none
	Licenses fs.FS        // the licence files GET /licenses shows, read on each request; nil serves none
	Log      *slog.Logger
	Now      func() time.Time
	// JobPing is how long a job's event stream may sit quiet before it sends a
	// comment, which keeps a proxy from closing it. Zero means 15 seconds.
	JobPing time.Duration

	// Subs changes subscriptions and hands out their links. With nil those
	// routes are left out, and answer as any path that isn't there.
	Subs SubscriptionOps
	// Backup exports the state and imports it again, with the same rule for nil.
	Backup BackupOps
}

type API struct{ cfg Config }

// New serves every route at / and, when there is a base path, under it too:
// the router's proxy may or may not strip its prefix. Routes under /api are
// only for requests from the page (see allowed), except a job's event stream,
// which an EventSource reads (see guardedStream); /health and /licenses are open.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.JobPing <= 0 {
		cfg.JobPing = defaultJobPing
	}
	a := &API{cfg: cfg}
	mux := http.NewServeMux()
	handle := func(pattern string, h http.HandlerFunc) {
		method, path, _ := strings.Cut(pattern, " ")
		mux.HandleFunc(method+" "+path, h)
		if cfg.BasePath != "" {
			mux.HandleFunc(method+" "+cfg.BasePath+path, h)
		}
	}
	route := func(pattern string, h http.HandlerFunc) {
		if _, path, _ := strings.Cut(pattern, " "); strings.HasPrefix(path, "/api/") {
			h = a.guarded(h)
		}
		handle(pattern, h)
	}
	route("GET /health", a.health)
	route("GET /licenses", a.licenses) // not under /api, so open like the page
	route("GET /api/config", a.config)
	route("GET /api/jobs", a.listJobs)
	route("GET /api/jobs/{id}", a.getJob)
	handle("GET /api/jobs/{id}/events", a.guardedStream(a.jobEvents))
	route("GET /api/nodes", a.nodes)
	route("GET /api/nodes/{id}", a.node)
	route("GET /api/nodes/{id}/subscriptions", a.nodeSubscriptions)
	route("GET /api/nodes/{id}/traffic", a.nodeTraffic)
	route("GET /api/subscriptions", a.subscriptions)
	a.subscriptionRoutes(route)
	a.backupRoutes(route)
	mux.HandleFunc("/", a.noRoute)
	if cfg.Index != nil {
		mux.Handle("GET /{$}", cfg.Index)
		if cfg.BasePath != "" {
			mux.Handle("GET "+cfg.BasePath+"/{$}", cfg.Index)
			mux.Handle("GET "+cfg.BasePath, cfg.Index)
		}
	}
	return mux
}

func (a *API) noRoute(w http.ResponseWriter, r *http.Request) {
	if a.looksLikeAPI(r.URL.Path) && !a.allowed(w, r) {
		return
	}
	a.fail(w, &Error{CodeNotFound, "There is nothing at this address."})
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Writes  uint64 `json:"writes"`
	}{true, a.cfg.Version, a.cfg.Commit, a.cfg.Store.Writes()})
}

func (a *API) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, configView(a.cfg, a.cfg.Store.Read()))
}

func (a *API) nodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, nodeViews(a.cfg.Store.Read(), a.cfg.Now(), a.cfg.Live))
}

func (a *API) subscriptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, subscriptionViews(a.cfg.Store.Read(), a.cfg.Live))
}

// nodeParam is the node a path names, with the state it was found in. An id
// that isn't a number names no node, the same as one nobody has.
func (a *API) nodeParam(r *http.Request) (model.State, model.Node, error) {
	st := a.cfg.Store.Read()
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err == nil {
		if n := st.FindNode(uint(id)); n != nil {
			return st, *n, nil
		}
	}
	return st, model.Node{}, fmt.Errorf("%w: node %q", model.ErrNotFound, r.PathValue("id"))
}

// node is one node with its recent events, which the list leaves out.
func (a *API) node(w http.ResponseWriter, r *http.Request) {
	st, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	events := []NodeEvent{}
	if a.cfg.Live != nil {
		if got := a.cfg.Live.NodeEvents(n.ID); got != nil {
			events = got
		}
	}
	v := nodeView(n, enabledCounts(st)[n.ID], a.cfg.Now(), a.cfg.Live)
	v.Events = &events
	writeJSON(w, http.StatusOK, v)
}

func (a *API) nodeSubscriptions(w http.ResponseWriter, r *http.Request) {
	st, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nodeSubscriptionViews(st, n.ID, a.cfg.Live))
}

// defaultStepSec is the spacing of the chart's samples when there is no
// manager to say: the collector's own step.
const defaultStepSec = 5

func (a *API) nodeTraffic(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	t := Traffic{StepSec: defaultStepSec}
	if a.cfg.Live != nil {
		t = a.cfg.Live.Traffic(n.ID)
		if t.StepSec == 0 {
			t.StepSec = defaultStepSec
		}
	}
	if t.Samples == nil {
		t.Samples = []TrafficSample{} // JSON gets [], never null
	}
	writeJSON(w, http.StatusOK, t)
}
