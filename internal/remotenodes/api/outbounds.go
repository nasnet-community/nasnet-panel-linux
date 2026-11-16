// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// Outbounds are servers a node, or the router's own entry, can leave through
// instead of the router's VPN. The page reads a share link into the fields
// below; the hub takes only the fields, and checks them.

// What an outbound's state says.
const (
	OutboundIdle    = "idle"    // nothing leaves through it
	OutboundUnknown = "unknown" // something does, and the bridge isn't running or hasn't checked it yet
	OutboundUp      = "up"
	OutboundDown    = "down"
)

// OutboundSettings is how to reach an outbound's server, as a request gives
// it. A view has the same fields but the id and the password, which are its
// login and never leave the hub.
type OutboundSettings struct {
	Protocol    string   `json:"protocol"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	ID          string   `json:"id"`
	Password    string   `json:"password"`
	Username    string   `json:"username"`
	Method      string   `json:"method"`
	Flow        string   `json:"flow"`
	Network     string   `json:"network"`
	Path        string   `json:"path"`
	HostHeader  string   `json:"hostHeader"`
	Mode        string   `json:"mode"`
	Security    string   `json:"security"`
	ServerName  string   `json:"serverName"`
	Fingerprint string   `json:"fingerprint"`
	ALPN        []string `json:"alpn"`
	PublicKey   string   `json:"publicKey"`
	ShortID     string   `json:"shortId"`
	SpiderX     string   `json:"spiderX"`
}

// Model is the settings as the state keeps them.
func (s OutboundSettings) Model() model.OutboundSettings {
	return model.OutboundSettings{
		Protocol: s.Protocol, Host: s.Host, Port: s.Port, ID: s.ID, Password: s.Password, Username: s.Username,
		Method: s.Method, Flow: s.Flow, Network: s.Network, Path: s.Path, HostHeader: s.HostHeader, Mode: s.Mode,
		Security: s.Security, ServerName: s.ServerName, Fingerprint: s.Fingerprint, ALPN: slices.Clone(s.ALPN),
		PublicKey: s.PublicKey, ShortID: s.ShortID, SpiderX: s.SpiderX,
	}
}

// String leaves the login out, like Login's.
func (s OutboundSettings) String() string {
	return fmt.Sprintf("%s to %s:%d over %s/%s (id %s, password %s)", s.Protocol, s.Host, s.Port, s.Network, s.Security, hidden(s.ID), hidden(s.Password))
}

func (s OutboundSettings) GoString() string { return s.String() }

func (s OutboundSettings) LogValue() slog.Value { return slog.StringValue(s.String()) }

// OutboundCreate is a new outbound.
type OutboundCreate struct {
	Name     string           `json:"name"`
	Fallback bool             `json:"fallback"`
	Settings OutboundSettings `json:"settings"`
}

// OutboundPatch is what PATCH /api/outbounds/{id} may change. What isn't in it
// stays. Settings, when it is there, is whole and takes the place of all of
// them, the login too.
type OutboundPatch struct {
	Name     *string           `json:"name"`
	Fallback *bool             `json:"fallback"`
	Settings *OutboundSettings `json:"settings"`
}

// Outbound is an outbound as the page reads it.
type Outbound struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Fallback    bool      `json:"fallback"`
	CreatedAt   time.Time `json:"createdAt"`
	Protocol    string    `json:"protocol"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Username    string    `json:"username"`
	Method      string    `json:"method"`
	Flow        string    `json:"flow"`
	Network     string    `json:"network"` // always filled: tcp when none was given
	Path        string    `json:"path"`
	HostHeader  string    `json:"hostHeader"`
	Mode        string    `json:"mode"`
	Security    string    `json:"security"` // always filled: none when none was given
	ServerName  string    `json:"serverName"`
	Fingerprint string    `json:"fingerprint"`
	ALPN        []string  `json:"alpn"`
	PublicKey   string    `json:"publicKey"`
	ShortID     string    `json:"shortId"`
	SpiderX     string    `json:"spiderX"`
	// SecretHint is the first and last four characters of an id, to tell one
	// from another. A password has none: the page shows dots.
	SecretHint string        `json:"secretHint"`
	UsedBy     OutboundUsers `json:"usedBy"`
	State      OutboundState `json:"state"`
}

// OutboundUsers is what names an outbound as its way out: nodes by id, and
// This router.
type OutboundUsers struct {
	Nodes  []uint `json:"nodes"`
	Router bool   `json:"router"`
}

// OutboundState is how an outbound is doing, as the bridge's check last found
// it. The manager fills it in; the zero value is one nothing is known of.
type OutboundState struct {
	Status string `json:"status"` // idle, unknown, up or down
	// DelayMs is how long the last check took, when it is up; Error why it
	// failed, when it is down, in the core's words or the hub's.
	DelayMs int64  `json:"delayMs"`
	Error   string `json:"error"`
	// OnFallback is set while it is down and what would leave through it
	// leaves through the router's VPN instead.
	OnFallback bool       `json:"onFallback"`
	LastTryAt  *time.Time `json:"lastTryAt"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	// The bytes that went through it since the bridge started.
	BytesUp   uint64 `json:"bytesUp"`
	BytesDown uint64 `json:"bytesDown"`
}

// TestResult is how a try of an outbound went: a web request through it.
type TestResult struct {
	OK      bool   `json:"ok"`
	DelayMs int64  `json:"delayMs,omitempty"`
	Status  int    `json:"status,omitempty"` // the request's HTTP status
	Error   string `json:"error,omitempty"`
}

// OutboundCheck is how the bridge watches the outbounds: a request to URL
// through each, every Seconds. An empty URL is DefaultURL.
type OutboundCheck struct {
	URL        string `json:"url"`
	Seconds    int    `json:"seconds"`
	DefaultURL string `json:"defaultUrl"`
}

// OutboundList is GET /api/outbounds.
type OutboundList struct {
	Outbounds []Outbound    `json:"outbounds"`
	Limit     int           `json:"limit"`
	Check     OutboundCheck `json:"check"`
}

// OutboundOps is what the API asks of the code that keeps the outbounds and
// sends nodes through them. *manager.Manager is one. Its errors are the
// model's and *Error, as SubscriptionOps' are.
type OutboundOps interface {
	CreateOutbound(ctx context.Context, req OutboundCreate) (Outbound, error)
	ChangeOutbound(ctx context.Context, id uint, patch OutboundPatch) (Outbound, error)
	// RemoveOutbound refuses one that something leaves through.
	RemoveOutbound(ctx context.Context, id uint) error
	// TestOutbound and TestOutboundSettings try one, saved or not. Where
	// there is no bridge program to try it with, the error is test_unavailable.
	TestOutbound(ctx context.Context, id uint) (TestResult, error)
	TestOutboundSettings(ctx context.Context, s OutboundSettings) (TestResult, error)
	SetOutboundCheck(ctx context.Context, url string) (OutboundCheck, error)
	// SetNodeRoute and SetLocalRoute say what a node, or the router's own
	// entry, leaves through: an outbound by id, or 0 for the router's VPN.
	SetNodeRoute(ctx context.Context, nodeID, outboundID uint) error
	SetLocalRoute(ctx context.Context, outboundID uint) (Local, error)
}

// ErrNoOutbound is the answer for an outbound nobody has.
var ErrNoOutbound = &Error{CodeNotFound, "There is no such outbound."}

// secretHint is what an outbound's view shows of its login.
func secretHint(s model.OutboundSettings) string {
	if len(s.ID) < 8 {
		return ""
	}
	return s.ID[:4] + "…" + s.ID[len(s.ID)-4:]
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ViewOutbound is one outbound as the list shows it, for the code that
// changes one and answers with the result. It reports false for an id the
// state doesn't hold.
func ViewOutbound(st model.State, id uint, live Live) (Outbound, bool) {
	o := st.FindOutbound(id)
	if o == nil {
		return Outbound{}, false
	}
	return outboundView(st, *o, live), true
}

func outboundView(st model.State, o model.Outbound, live Live) Outbound {
	s := o.Settings
	v := Outbound{
		ID: o.ID, Name: o.Name, Fallback: o.Fallback, CreatedAt: o.CreatedAt.UTC(),
		Protocol: s.Protocol, Host: s.Host, Port: s.Port, Username: s.Username, Method: s.Method, Flow: s.Flow,
		Network: orDefault(s.Network, bridgespec.NetTCP), Path: s.Path, HostHeader: s.HostHeader, Mode: s.Mode,
		Security: orDefault(s.Security, bridgespec.SecNone), ServerName: s.ServerName, Fingerprint: s.Fingerprint,
		ALPN: slices.Clone(s.ALPN), PublicKey: s.PublicKey, ShortID: s.ShortID, SpiderX: s.SpiderX,
		SecretHint: secretHint(s),
		State:      OutboundState{Status: OutboundIdle},
	}
	if v.ALPN == nil {
		v.ALPN = []string{} // JSON gets [], never null
	}
	v.UsedBy.Nodes, v.UsedBy.Router = st.LeavingThrough(o.ID)
	if v.UsedBy.Nodes == nil {
		v.UsedBy.Nodes = []uint{}
	}
	// with nobody to say how it is doing, one in use is not known to work
	if st.CarriedThrough(o.ID) {
		v.State.Status = OutboundUnknown
	}
	if live != nil {
		if ls := live.OutboundLive(o.ID); ls.Status != "" {
			v.State = ls
		}
	}
	return v
}

// ViewOutbounds is GET /api/outbounds.
func ViewOutbounds(st model.State, live Live) OutboundList {
	out := OutboundList{Outbounds: make([]Outbound, 0, len(st.Outbounds)), Limit: model.MaxOutbounds, Check: CheckOf(st)}
	for _, o := range st.Outbounds {
		out.Outbounds = append(out.Outbounds, outboundView(st, o, live))
	}
	return out
}

// CheckOf is how the state has the bridge watch the outbounds.
func CheckOf(st model.State) OutboundCheck {
	return OutboundCheck{URL: st.ProbeURL, Seconds: bridgespec.DefaultProbeSeconds, DefaultURL: bridgespec.DefaultProbeURL}
}

// outboundRoutes adds the outbounds' routes. The list can always be read; the
// calls that change something are there when something can carry them out,
// and answer as any path that isn't otherwise.
func (a *API) outboundRoutes(route func(pattern string, h http.HandlerFunc)) {
	route("GET /api/outbounds", a.outbounds)
	if a.cfg.Outbounds == nil {
		return
	}
	route("POST /api/outbounds", a.createOutbound)
	route("POST /api/outbounds/test", a.testOutboundSettings)
	route("PUT /api/outbounds/check", a.setOutboundCheck)
	route("PATCH /api/outbounds/{id}", a.changeOutbound)
	route("DELETE /api/outbounds/{id}", a.removeOutbound)
	route("POST /api/outbounds/{id}/test", a.testOutbound)
	route("PUT /api/nodes/{id}/route", a.setNodeRoute)
	route("PUT /api/local/route", a.setLocalRoute)
}

func (a *API) outbounds(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ViewOutbounds(a.cfg.Store.Read(), a.cfg.Live))
}

// outboundParam is the outbound a path names. Like a node's, it is asked for
// before the body is read, so that a path that names none is not found
// whatever else is wrong with the request.
func (a *API) outboundParam(r *http.Request) (uint, error) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err != nil || id == 0 {
		return 0, ErrNoOutbound
	}
	st := a.cfg.Store.Read()
	if st.FindOutbound(uint(id)) == nil {
		return 0, ErrNoOutbound
	}
	return uint(id), nil
}

func (a *API) createOutbound(w http.ResponseWriter, r *http.Request) {
	var req OutboundCreate
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	v, err := a.cfg.Outbounds.CreateOutbound(r.Context(), req)
	if err != nil {
		a.failSecret(w, r, err, req.Settings.ID, req.Settings.Password)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (a *API) changeOutbound(w http.ResponseWriter, r *http.Request) {
	id, err := a.outboundParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var patch OutboundPatch
	if err := strictJSON(w, r, maxSmallBody, &patch); err != nil {
		a.fail(w, err)
		return
	}
	if patch.Name == nil && patch.Fallback == nil && patch.Settings == nil {
		a.fail(w, &Error{CodeBadRequest, "There is nothing to change."})
		return
	}
	v, err := a.cfg.Outbounds.ChangeOutbound(r.Context(), id, patch)
	if err != nil {
		var secrets []string
		if patch.Settings != nil {
			secrets = []string{patch.Settings.ID, patch.Settings.Password}
		}
		a.failSecret(w, r, err, secrets...)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) removeOutbound(w http.ResponseWriter, r *http.Request) {
	id, err := a.outboundParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if err := a.cfg.Outbounds.RemoveOutbound(r.Context(), id); err != nil {
		a.failOp(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) testOutbound(w http.ResponseWriter, r *http.Request) {
	id, err := a.outboundParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	res, err := a.cfg.Outbounds.TestOutbound(r.Context(), id)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) testOutboundSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Settings *OutboundSettings `json:"settings"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.Settings == nil {
		a.fail(w, &Error{CodeBadRequest, "settings is required."})
		return
	}
	res, err := a.cfg.Outbounds.TestOutboundSettings(r.Context(), *req.Settings)
	if err != nil {
		a.failSecret(w, r, err, req.Settings.ID, req.Settings.Password)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) setOutboundCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL *string `json:"url"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.URL == nil {
		a.fail(w, &Error{CodeBadRequest, `url must be an address, or "" for the default.`})
		return
	}
	v, err := a.cfg.Outbounds.SetOutboundCheck(r.Context(), *req.URL)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// routeBody is what the two route calls take.
type routeBody struct {
	OutboundID *uint `json:"outboundId"`
}

var errNoRoute = &Error{CodeBadRequest, "outboundId must be the id of an outbound, or 0 for the router's VPN."}

func (a *API) setNodeRoute(w http.ResponseWriter, r *http.Request) {
	_, n, err := a.nodeParam(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var req routeBody
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.OutboundID == nil {
		a.fail(w, errNoRoute)
		return
	}
	if err := a.cfg.Outbounds.SetNodeRoute(r.Context(), n.ID, *req.OutboundID); err != nil {
		a.failOp(w, r, err)
		return
	}
	a.respondNode(w, n.ID)
}

func (a *API) setLocalRoute(w http.ResponseWriter, r *http.Request) {
	var req routeBody
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.OutboundID == nil {
		a.fail(w, errNoRoute)
		return
	}
	v, err := a.cfg.Outbounds.SetLocalRoute(r.Context(), *req.OutboundID)
	a.answerLocal(w, r, v, err)
}
