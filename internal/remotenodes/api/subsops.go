// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// Link is a subscription's way in at one node: the vless:// address to hand
// over. It carries the client's id, so it and the backup file are the only
// answers that hold a secret, and neither is ever logged.
type Link struct {
	NodeID   uint   `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Vless    string `json:"vless"`
}

// SubscriptionOps is what the API asks of the code that changes subscriptions
// and hands out their links. *manager.Manager is one. The views it returns are
// the ones the list shows, with the live parts merged in, so the page can put
// them where the old ones were. Its errors are the model's, the job engine's
// busy error and *Error for the few that need a sentence of their own.
type SubscriptionOps interface {
	CreateSubscription(ctx context.Context, name string, nodeIDs []uint, limits SubscriptionLimits) (Subscription, error)
	UpdateSubscription(ctx context.Context, id uint, patch SubscriptionPatch) (Subscription, error)
	// ResetUsage starts a subscription's traffic count again from zero.
	ResetUsage(ctx context.Context, id uint) (Subscription, error)
	DeleteSubscription(ctx context.Context, id uint) error
	SetEnabled(ctx context.Context, subID, nodeID uint, enabled bool) (Subscription, error)
	Link(subID, nodeID uint) (Link, error)
	ReplaceLink(ctx context.Context, subID, nodeID uint) (Link, error)
}

// SubscriptionLimits is what a subscription may use before it stops. The zero
// value is no limit and no end date.
type SubscriptionLimits struct {
	TrafficLimit uint64    // bytes, both ways on every node; 0 is no limit
	ExpiresAt    time.Time // zero is never
}

// Opt is a field of a patch that may be left out, null, or a value. A field
// that isn't in the body is the zero Opt, which changes nothing; null clears.
type Opt[T any] struct {
	Set   bool // the body has the field
	Null  bool // and it is null
	Value T
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(bytes.TrimSpace(b)) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

// SubscriptionPatch is what PATCH /api/subscriptions/{id} may change. What
// isn't in it stays.
type SubscriptionPatch struct {
	Name         *string        `json:"name"`
	TrafficLimit Opt[uint64]    `json:"trafficLimit"` // bytes; null removes the limit
	ExpiresAt    Opt[time.Time] `json:"expiresAt"`    // RFC 3339; null removes the end date
}

// maxSmallBody is the most a call that takes a few words may send.
const maxSmallBody = 64 << 10

// subscriptionRoutes adds the calls that change a subscription, when there is
// something to carry them out. Without it they are not there, and answer as
// any other path that isn't.
func (a *API) subscriptionRoutes(route func(pattern string, h http.HandlerFunc)) {
	if a.cfg.Subs == nil {
		return
	}
	route("POST /api/subscriptions", a.createSubscription)
	route("PATCH /api/subscriptions/{id}", a.updateSubscription)
	route("POST /api/subscriptions/{id}/reset-usage", a.resetSubscriptionUsage)
	route("DELETE /api/subscriptions/{id}", a.deleteSubscription)
	route("PUT /api/subscriptions/{id}/nodes/{nodeId}", a.switchSubscription)
	route("GET /api/subscriptions/{id}/nodes/{nodeId}/link", a.subscriptionLink)
	route("POST /api/subscriptions/{id}/nodes/{nodeId}/replace-link", a.replaceSubscriptionLink)
}

// idPath is the number a path holds under name. Anything else names nothing,
// which is the same answer as an id nobody has.
func idPath(r *http.Request, name string) (uint, error) {
	v := r.PathValue(name)
	id, err := strconv.ParseUint(v, 10, 32)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%w: %s %q", model.ErrNotFound, name, v)
	}
	return uint(id), nil
}

// idsPath is the subscription and the node a path names.
func idsPath(r *http.Request) (sub, node uint, err error) {
	if sub, err = idPath(r, "id"); err != nil {
		return 0, 0, err
	}
	node, err = idPath(r, "nodeId")
	return sub, node, err
}

var errBodyTooLarge = &Error{CodeBadRequest, "The request is too large."}

// strictJSON reads a request's body into dst. The body must be one JSON value
// of at most limit bytes, and have no field that dst doesn't. The reasons are
// plain sentences that never repeat what was sent, which may be a passphrase.
func strictJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) error {
	if r.ContentLength > limit {
		// The body is not read, so the connection can't carry another request.
		// Without this the server would first try to read it, up to 256 KiB,
		// and a client that never sends it would hold the answer back.
		w.Header().Set("Connection", "close")
		return errBodyTooLarge
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		// nothing but space may follow
		if _, err = dec.Token(); errors.Is(err, io.EOF) {
			return nil
		}
		if err == nil {
			err = errors.New("more than one value")
		}
	}
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return errBodyTooLarge
	case errors.Is(err, io.EOF):
		return &Error{CodeBadRequest, "The request needs a JSON body."}
	}
	return &Error{CodeBadRequest, "The request body is not what this call takes."}
}

// failOp answers an error from a call that changes something. A request that
// went away while it waited has no one to answer and nothing to report.
func (a *API) failOp(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	a.fail(w, err)
}

func (a *API) createSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string     `json:"name"`
		NodeIDs      *[]uint    `json:"nodeIds"`
		TrafficLimit *uint64    `json:"trafficLimit"`
		ExpiresAt    *time.Time `json:"expiresAt"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.NodeIDs == nil {
		a.fail(w, &Error{CodeBadRequest, "nodeIds must be a list of node ids."})
		return
	}
	var limits SubscriptionLimits
	if req.TrafficLimit != nil {
		limits.TrafficLimit = *req.TrafficLimit
	}
	if req.ExpiresAt != nil {
		limits.ExpiresAt = *req.ExpiresAt
	}
	v, err := a.cfg.Subs.CreateSubscription(r.Context(), req.Name, *req.NodeIDs, limits)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (a *API) updateSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := idPath(r, "id")
	if err != nil {
		a.fail(w, err)
		return
	}
	var patch SubscriptionPatch
	if err := strictJSON(w, r, maxSmallBody, &patch); err != nil {
		a.fail(w, err)
		return
	}
	if patch.Name == nil && !patch.TrafficLimit.Set && !patch.ExpiresAt.Set {
		a.fail(w, &Error{CodeBadRequest, "There is nothing to change."})
		return
	}
	v, err := a.cfg.Subs.UpdateSubscription(r.Context(), id, patch)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) resetSubscriptionUsage(w http.ResponseWriter, r *http.Request) {
	id, err := idPath(r, "id")
	if err != nil {
		a.fail(w, err)
		return
	}
	v, err := a.cfg.Subs.ResetUsage(r.Context(), id)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := idPath(r, "id")
	if err != nil {
		a.fail(w, err)
		return
	}
	if err := a.cfg.Subs.DeleteSubscription(r.Context(), id); err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{true})
}

func (a *API) switchSubscription(w http.ResponseWriter, r *http.Request) {
	sub, node, err := idsPath(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.Enabled == nil {
		a.fail(w, &Error{CodeBadRequest, "enabled must be true or false."})
		return
	}
	v, err := a.cfg.Subs.SetEnabled(r.Context(), sub, node, *req.Enabled)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// subscriptionLink hands out a link. The answer is no-store like every other,
// and nothing here logs it: a failure is logged without the link, since an
// error never holds one.
func (a *API) subscriptionLink(w http.ResponseWriter, r *http.Request) {
	sub, node, err := idsPath(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	l, err := a.cfg.Subs.Link(sub, node)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) replaceSubscriptionLink(w http.ResponseWriter, r *http.Request) {
	sub, node, err := idsPath(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	l, err := a.cfg.Subs.ReplaceLink(r.Context(), sub, node)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
