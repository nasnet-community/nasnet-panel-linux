// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// nodeOpRoutes adds the calls that add, change and remove nodes, when there is
// something to carry them out. Without it they are not there, and answer as any
// other path that isn't. Every one is under /api, so every one is for the page
// only.
//
// A call that takes a body reads it with strictJSON: one JSON object of at most
// maxSmallBody bytes, with no field the contract doesn't have. The calls that
// take none (checking for updates, renewing a certificate, rotating the keys)
// don't read one.
func (a *API) nodeOpRoutes(route func(pattern string, h http.HandlerFunc)) {
	if a.cfg.NodeOps == nil {
		return
	}
	route("POST /api/probe", a.opProbe)
	route("POST /api/nodes", a.opInstall)
	route("PATCH /api/nodes/{id}", a.opRename)
	route("PATCH /api/nodes/{id}/settings", a.opSettings)
	route("POST /api/nodes/{id}/handshake-check", a.opHandshake)
	route("POST /api/nodes/{id}/check-updates", a.opCheckUpdates)
	route("POST /api/nodes/{id}/update", a.opUpdate)
	route("POST /api/nodes/{id}/renew-cert", a.opRenewCert)
	route("POST /api/nodes/{id}/rotate-keys", a.opRotateKeys)
	route("POST /api/nodes/{id}/remove", a.opRemove)
}

// minLine is how short a line of a key may be and still be hidden by itself: a
// shorter one would blot out ordinary words.
const minLine = 8

// failSecret answers err for a request that carried a login. The manager and
// agentops take care that their messages hold none, and this is the last line:
// whatever of the login an error's text repeats is taken out before the text
// is sent or logged.
func (a *API) failSecret(w http.ResponseWriter, r *http.Request, err error, secrets ...string) {
	a.failOp(w, r, hideIn(err, secrets))
}

// hideIn is err with every secret replaced by an ellipsis in the text it would
// show, in whatever form that text has it: whole, trimmed, or a line of a key.
// An error that doesn't repeat any is returned as it is.
func hideIn(err error, secrets []string) error {
	var words []string
	for _, s := range secrets {
		if strings.TrimSpace(s) == "" {
			continue
		}
		words = append(words, s, strings.TrimSpace(s))
		for _, line := range strings.Split(s, "\n") {
			if line = strings.TrimSpace(line); utf8.RuneCountInString(line) >= minLine {
				words = append(words, line)
			}
		}
	}
	if len(words) == 0 || err == nil {
		return err
	}
	// the longer first, or a line of a key would leave the rest of the key behind
	slices.SortStableFunc(words, func(a, b string) int { return len(b) - len(a) })
	clean := func(s string) string {
		for _, w := range words {
			s = strings.ReplaceAll(s, w, "…")
		}
		return s
	}
	if e := classify(err); e != nil {
		if msg := clean(e.Msg); msg != e.Msg {
			return &Error{e.Code, msg}
		}
		return err
	}
	if msg := clean(err.Error()); msg != err.Error() {
		return errors.New(msg)
	}
	return err
}

// secrets are the parts of a login that were given.
func (l Login) secrets() []string {
	var out []string
	for _, s := range []string{l.Password, l.PrivateKey, l.Passphrase} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func started(w http.ResponseWriter, jobID string) {
	writeJSON(w, http.StatusAccepted, Started{JobID: jobID})
}

// respondNode answers with the node as it stands now, which is what a call that
// changed it or its updates gives back.
func (a *API) respondNode(w http.ResponseWriter, id uint) {
	st := a.cfg.Store.Read()
	n := st.FindNode(id)
	if n == nil {
		a.fail(w, errNoNode)
		return
	}
	writeJSON(w, http.StatusOK, a.nodeDetail(st, *n))
}

var errNoNode = &Error{CodeNotFound, "There is no such node or subscription."}

func (a *API) opProbe(w http.ResponseWriter, r *http.Request) {
	var req ProbeRequest
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	res, err := a.cfg.NodeOps.Probe(r.Context(), req)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) opInstall(w http.ResponseWriter, r *http.Request) {
	var req InstallRequest
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	id, err := a.cfg.NodeOps.AddNode(r.Context(), req)
	if err != nil {
		a.failSecret(w, r, err, req.Login.secrets()...)
		return
	}
	started(w, id)
}

// A call that is for a node asks first whether the node is there, so that a
// path that names none is not found whatever else is wrong with the request.

func (a *API) opRename(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if err := a.cfg.NodeOps.RenameNode(r.Context(), n.ID, req.Name); err != nil {
		a.failOp(w, r, err)
		return
	}
	a.respondNode(w, n.ID)
}

func (a *API) opSettings(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req SettingsRequest
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	id, err := a.cfg.NodeOps.ChangeSettings(r.Context(), n.ID, req)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	started(w, id)
}

func (a *API) opHandshake(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req struct {
		Target string `json:"target"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	res, err := a.cfg.NodeOps.CheckHandshake(r.Context(), n.ID, req.Target)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) opCheckUpdates(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if err := a.cfg.NodeOps.CheckUpdates(r.Context(), n.ID); err != nil {
		a.failOp(w, r, err)
		return
	}
	a.respondNode(w, n.ID)
}

func (a *API) opUpdate(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	id, err := a.cfg.NodeOps.UpdateNode(r.Context(), n.ID, req.Kind)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	started(w, id)
}

func (a *API) opRenewCert(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	id, err := a.cfg.NodeOps.RenewCert(r.Context(), n.ID)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	started(w, id)
}

func (a *API) opRotateKeys(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	id, err := a.cfg.NodeOps.RotateKeys(r.Context(), n.ID)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	started(w, id)
}

func (a *API) opRemove(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req RemoveRequest
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	var secrets []string
	if req.Login != nil {
		secrets = req.Login.secrets()
	}
	id, err := a.cfg.NodeOps.RemoveNode(r.Context(), n.ID, req)
	if err != nil {
		a.failSecret(w, r, err, secrets...)
		return
	}
	started(w, id)
}
