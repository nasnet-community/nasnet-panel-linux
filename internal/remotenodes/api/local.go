// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The router's own entry, "This router" on the page: an Xray inbound on the
// router that subscriptions connect to straight. It is not a node, so it has
// routes and views of its own; a subscription is switched on for it beside
// its nodes, and gets one more link.

// RouterID is what Live's per-node questions take for the router's own entry:
// SubscriptionOn(sub, RouterID) and Traffic(RouterID). No node has it.
const RouterID uint = 0

// What Local.State and Local.Reason say.
const (
	LocalOff       = "off"
	LocalStarting  = "starting"
	LocalWorking   = "working"
	LocalAttention = "attention"

	LocalChoosingTarget = "choosing_target" // starting: a Reality target is being looked for
	LocalWaitingRouter  = "waiting_router"  // starting: the router has not opened the port yet
	LocalEntryOff       = "entry_off"       // the router's entry gate is off, so nothing is carried
	LocalNoPort         = "no_port"         // the router never opened the port
	LocalNoAddress      = "no_address"      // there is no name to put in a link
	LocalBridgeDown     = "bridge_down"     // the bridge that holds the inbound isn't running
	LocalTargetFailed   = "target_failed"   // no Reality target works from the router
)

// Local is the router's own entry as the page reads it.
type Local struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`  // off, starting, working or attention
	Reason  string `json:"reason"` // why it is starting or needs attention, "" otherwise
	Detail  string `json:"detail"` // a sentence with more, when there is more to say
	// Host is the name its links carry: the one set by hand, else the router's
	// IP Cloud name, else "". HostSource says which: own, cloud or "".
	Host         string `json:"host"`
	HostSource   string `json:"hostSource"`
	OwnHost      string `json:"ownHost"`      // the name set by hand, "" when none
	CloudName    string `json:"cloudName"`    // the router's IP Cloud name
	PublicAddr   string `json:"publicAddr"`   // the router as IP Cloud sees it
	DomesticAddr string `json:"domesticAddr"` // the router's own address on the Domestic link
	// BehindNAT is set when those two differ: another router sits in front and
	// has to forward the port too.
	BehindNAT         bool      `json:"behindNat"`
	Port              *int      `json:"port"`            // on the Domestic link; null while the router has opened none
	HandshakeTarget   string    `json:"handshakeTarget"` // host:port the REALITY handshake borrows
	SNI               string    `json:"sni"`
	SNICustom         bool      `json:"sniCustom"` // false: the same as the Reality target's host
	PublicKey         string    `json:"publicKey"`
	ShortID           string    `json:"shortId"`
	SubscriptionCount int       `json:"subscriptionCount"` // switched on for it
	Usage             NodeUsage `json:"usage"`
	XrayVersion       string    `json:"xrayVersion"`
	UptimeSec         int64     `json:"uptimeSec"`
	// Events is only in the view of the entry itself, as a node's is.
	Events *[]NodeEvent `json:"events,omitempty"`
}

// LocalBrief is the entry in the page's config: enough to draw its place.
type LocalBrief struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
}

// LocalLive is the live part of Local, which the manager knows and the state
// doesn't. The zero value is an entry nothing is known of.
type LocalLive struct {
	State        string
	Reason       string
	Detail       string
	CloudName    string
	PublicAddr   string
	DomesticAddr string
	Port         int // 0: none
	UptimeSec    int64
	Usage        NodeUsage
}

// SubscriptionLocal is a subscription on the router's own entry: what a
// SubscriptionNode is for a node.
type SubscriptionLocal struct {
	Enabled     bool       `json:"enabled"`
	Online      *bool      `json:"online"` // null: the entry isn't answering, so unknown
	Connections *int       `json:"connections"`
	LastSeenAt  *time.Time `json:"lastSeenAt"`
	TodayUp     uint64     `json:"todayUp"`
	TodayDown   uint64     `json:"todayDown"`
	MonthUp     uint64     `json:"monthUp"`
	MonthDown   uint64     `json:"monthDown"`
}

// LocalSettings is what PATCH /api/local/settings may change. What isn't in it
// stays. An empty host goes back to the router's own name, an empty target
// lets the router choose one, and an empty SNI is the target's host.
type LocalSettings struct {
	Host            *string `json:"host"`
	HandshakeTarget *string `json:"handshakeTarget"`
	SNI             *string `json:"sni"`
}

// LocalOps is what the API asks of the code that runs the router's own entry.
// *manager.Manager is one. Each call that changes it answers with the entry as
// it is afterwards.
type LocalOps interface {
	// SetLocalEnabled switches the entry on or off. Off keeps its keys and the
	// subscriptions' switches, so on again gives the same links.
	SetLocalEnabled(ctx context.Context, enabled bool) (Local, error)
	ChangeLocalSettings(ctx context.Context, s LocalSettings) (Local, error)
	// NewLocalPort asks the router for another port, which changes every link.
	NewLocalPort(ctx context.Context) (Local, error)
	// RotateLocalKeys makes new REALITY keys, which changes every link.
	RotateLocalKeys(ctx context.Context) (Local, error)
	// CheckLocalTarget tries a Reality target from the router.
	CheckLocalTarget(ctx context.Context, target string) (HandshakeCheck, error)
	SetLocalSubscription(ctx context.Context, subID uint, enabled bool) (Subscription, error)
	LocalLink(subID uint) (Link, error)
	ReplaceLocalLink(ctx context.Context, subID uint) (Link, error)
}

// enabledHere is how many subscriptions are switched on for the entry.
func enabledHere(st model.State) int {
	n := 0
	for _, c := range st.LocalClients {
		if c.Enabled {
			n++
		}
	}
	return n
}

func localBrief(st model.State, live Live) LocalBrief {
	v := localView(st, live)
	return LocalBrief{Enabled: v.Enabled, State: v.State}
}

func localView(st model.State, live Live) Local {
	l := st.Local
	v := Local{
		Enabled:           l.Enabled,
		State:             LocalOff,
		OwnHost:           l.Host,
		HandshakeTarget:   l.RealityTarget,
		SNI:               l.RealitySNI,
		SNICustom:         l.RealitySNI != "" && l.RealitySNI != hostOf(l.RealityTarget),
		PublicKey:         l.RealityPublicKey,
		ShortID:           l.RealityShortID,
		SubscriptionCount: enabledHere(st),
	}
	if l.Enabled {
		// with nobody to say how it is doing, it is on its way and no more
		v.State = LocalStarting
	}
	if live != nil {
		ll := live.Local()
		if ll.State != "" {
			v.State, v.Reason, v.Detail = ll.State, ll.Reason, ll.Detail
		}
		v.CloudName, v.PublicAddr, v.DomesticAddr = ll.CloudName, ll.PublicAddr, ll.DomesticAddr
		v.BehindNAT = ll.PublicAddr != "" && ll.DomesticAddr != "" && ll.PublicAddr != ll.DomesticAddr
		if ll.Port > 0 {
			p := ll.Port
			v.Port = &p
		}
		v.UptimeSec, v.Usage = ll.UptimeSec, ll.Usage
		v.XrayVersion = live.Xray()
	}
	switch {
	case l.Host != "":
		v.Host, v.HostSource = l.Host, "own"
	case v.CloudName != "":
		v.Host, v.HostSource = v.CloudName, "cloud"
	}
	return v
}

// subscriptionLocalView is a subscription's row for the entry. connected and
// unknown are for the subscription's own word on whether it is online.
func subscriptionLocalView(st model.State, sub model.Subscription, live Live) (row SubscriptionLocal, connected, unknown bool) {
	c := st.FindLocalClient(sub.ID)
	row.Enabled = c != nil && c.Enabled
	if live == nil {
		return row, false, false
	}
	u := live.SubscriptionOn(sub.ID, RouterID)
	row.LastSeenAt = u.LastSeenAt
	row.TodayUp, row.TodayDown, row.MonthUp, row.MonthDown = u.TodayUp, u.TodayDown, u.MonthUp, u.MonthDown
	if row.Enabled {
		if u.Live {
			row.Online, row.Connections = &u.Online, &u.Connections
			connected = u.Online
		} else {
			unknown = true
		}
	}
	return row, connected, unknown
}

// localSubscriptionViews is the entry's list: every subscription, switched on
// for it or not, as a node's list has them.
func localSubscriptionViews(st model.State, live Live) []NodeSubscription {
	out := make([]NodeSubscription, 0, len(st.Subscriptions))
	for _, sub := range st.Subscriptions {
		c := st.FindLocalClient(sub.ID)
		row := NodeSubscription{ID: sub.ID, Name: sub.Name, Enabled: c != nil && c.Enabled, Stopped: sub.Stopped, TrafficLimit: sub.TrafficLimit, ExpiresAt: expiry(sub)}
		if live != nil {
			row.TrafficUsed = TrafficUsed(sub, live.SubscriptionLifetime(sub.ID))
			u := live.SubscriptionOn(sub.ID, RouterID)
			row.LastSeenAt = u.LastSeenAt
			row.TodayUp, row.TodayDown, row.MonthUp, row.MonthDown = u.TodayUp, u.TodayDown, u.MonthUp, u.MonthDown
			if row.Enabled && u.Live {
				row.Online, row.Connections = &u.Online, &u.Connections
			}
		}
		out = append(out, row)
	}
	return out
}

// ViewLocal is the entry as GET /api/local shows it, events and all, for the
// code that changes it and has to answer with the result.
func ViewLocal(st model.State, live Live) Local {
	v := localView(st, live)
	events := []NodeEvent{}
	if live != nil {
		if got := live.LocalEvents(); got != nil {
			events = got
		}
	}
	v.Events = &events
	return v
}

// localRoutes adds the entry's routes. What it is and how it is doing can
// always be read; the calls that change it are there when something can carry
// them out, and answer as any path that isn't otherwise.
func (a *API) localRoutes(route func(pattern string, h http.HandlerFunc)) {
	route("GET /api/local", a.local)
	route("GET /api/local/subscriptions", a.localSubscriptions)
	route("GET /api/local/traffic", a.localTraffic)
	if a.cfg.Local == nil {
		return
	}
	route("PUT /api/local", a.switchLocal)
	route("PATCH /api/local/settings", a.changeLocalSettings)
	route("POST /api/local/new-port", a.newLocalPort)
	route("POST /api/local/rotate-keys", a.rotateLocalKeys)
	route("POST /api/local/handshake-check", a.checkLocalTarget)
	route("PUT /api/subscriptions/{id}/local", a.switchLocalSubscription)
	route("GET /api/subscriptions/{id}/local/link", a.localLink)
	route("POST /api/subscriptions/{id}/local/replace-link", a.replaceLocalLink)
}

func (a *API) local(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ViewLocal(a.cfg.Store.Read(), a.cfg.Live))
}

func (a *API) localSubscriptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, localSubscriptionViews(a.cfg.Store.Read(), a.cfg.Live))
}

func (a *API) localTraffic(w http.ResponseWriter, _ *http.Request) {
	t := Traffic{StepSec: defaultStepSec}
	if a.cfg.Live != nil {
		t = a.cfg.Live.Traffic(RouterID)
		if t.StepSec == 0 {
			t.StepSec = defaultStepSec
		}
	}
	if t.Samples == nil {
		t.Samples = []TrafficSample{} // JSON gets [], never null
	}
	writeJSON(w, http.StatusOK, t)
}

// answerLocal writes what a call that changed the entry came back with.
func (a *API) answerLocal(w http.ResponseWriter, r *http.Request, v Local, err error) {
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) switchLocal(w http.ResponseWriter, r *http.Request) {
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
	v, err := a.cfg.Local.SetLocalEnabled(r.Context(), *req.Enabled)
	a.answerLocal(w, r, v, err)
}

func (a *API) changeLocalSettings(w http.ResponseWriter, r *http.Request) {
	var req LocalSettings
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.Host == nil && req.HandshakeTarget == nil && req.SNI == nil {
		a.fail(w, &Error{CodeBadRequest, "There is nothing to change."})
		return
	}
	v, err := a.cfg.Local.ChangeLocalSettings(r.Context(), req)
	a.answerLocal(w, r, v, err)
}

func (a *API) newLocalPort(w http.ResponseWriter, r *http.Request) {
	v, err := a.cfg.Local.NewLocalPort(r.Context())
	a.answerLocal(w, r, v, err)
}

func (a *API) rotateLocalKeys(w http.ResponseWriter, r *http.Request) {
	v, err := a.cfg.Local.RotateLocalKeys(r.Context())
	a.answerLocal(w, r, v, err)
}

func (a *API) checkLocalTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := strictJSON(w, r, maxSmallBody, &req); err != nil {
		a.fail(w, err)
		return
	}
	v, err := a.cfg.Local.CheckLocalTarget(r.Context(), req.Target)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) switchLocalSubscription(w http.ResponseWriter, r *http.Request) {
	sub, err := idPath(r, "id")
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
	v, err := a.cfg.Local.SetLocalSubscription(r.Context(), sub, *req.Enabled)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// localLink hands out a link, no-store and never logged, like a node's.
func (a *API) localLink(w http.ResponseWriter, r *http.Request) {
	sub, err := idPath(r, "id")
	if err != nil {
		a.fail(w, err)
		return
	}
	l, err := a.cfg.Local.LocalLink(sub)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) replaceLocalLink(w http.ResponseWriter, r *http.Request) {
	sub, err := idPath(r, "id")
	if err != nil {
		a.fail(w, err)
		return
	}
	l, err := a.cfg.Local.ReplaceLocalLink(r.Context(), sub)
	if err != nil {
		a.failOp(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
