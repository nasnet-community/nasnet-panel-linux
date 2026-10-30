// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api is the hub's HTTP API.
//
// The model holds secrets (the REALITY private key, client UUIDs, the pinned
// host key), so nothing in this package marshals a model type: views.go maps
// them to the shapes the page reads, field by field, and a test walks every
// GET body for anything that looks like a secret.
package api

import (
	"log/slog"
	"net/http"
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
	Version  string
	Commit   string
	BasePath string       // "" or "/a/b", already cleaned by ui.BasePath
	Index    http.Handler // the web page; nil serves none
	Log      *slog.Logger
	Now      func() time.Time
}

type API struct{ cfg Config }

// New serves every route at / and, when there is a base path, under it too:
// the router's proxy may or may not strip its prefix. Routes under /api are
// only for requests from the page (see allowed); /health is open.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	a := &API{cfg: cfg}
	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		method, path, _ := strings.Cut(pattern, " ")
		if strings.HasPrefix(path, "/api/") {
			h = a.guarded(h)
		}
		mux.HandleFunc(method+" "+path, h)
		if cfg.BasePath != "" {
			mux.HandleFunc(method+" "+cfg.BasePath+path, h)
		}
	}
	route("GET /health", a.health)
	route("GET /api/config", a.config)
	route("GET /api/nodes", a.nodes)
	route("GET /api/subscriptions", a.subscriptions)
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
	writeJSON(w, http.StatusOK, nodeViews(a.cfg.Store.Read(), a.cfg.Now()))
}

func (a *API) subscriptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, subscriptionViews(a.cfg.Store.Read()))
}
