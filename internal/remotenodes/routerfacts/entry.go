// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"fmt"
	"strings"
	"time"
)

// StaleAfter is how old a report may get before the page warns about it. The
// script runs every five minutes, so this is three missed runs.
const StaleAfter = 15 * time.Minute

// The plugin's three source addresses on the router. Each is routed by its
// address alone; Routes says where each one goes.
const (
	AddrDownloads = "192.168.50.16"
	AddrExit      = "192.168.50.17"
	AddrNodes     = "192.168.50.18"
)

// The words an Entry uses for a link.
const (
	LinkOnline  = "online"
	LinkOffline = "offline"
	LinkMissing = "missing"
)

// The words an Entry uses for the bridge.
const (
	BridgeRunning = "running"
	BridgeWaiting = "waiting"
	BridgeStopped = "stopped"
)

// Entry is what the page shows of the router: whether the entry can carry
// traffic and, if not, which piece is missing. Its JSON is the entry in the
// app config.
type Entry struct {
	On        bool       `json:"on"` // a Domestic link and a VPN are up, the routes are ready and RouterOS is new enough
	Domestic  string     `json:"domestic"`
	VPN       VPN        `json:"vpn"`
	Bridge    string     `json:"bridge"` // the manager fills this in; this package knows nothing of the process
	RouterOS  RouterOS   `json:"routerOS"`
	Routes    []Route    `json:"routes"`
	CheckedAt *time.Time `json:"checkedAt"` // when the router's script wrote the report; null if there is none to go by
	Stale     bool       `json:"stale"`     // the report is too old to be trusted as current

	// Reason is why On is false, in a sentence for the log and the error the
	// API answers with. The page works it out from the fields above, so it
	// stays out of the JSON.
	Reason string `json:"-"`
}

type VPN struct {
	State string `json:"state"` // online, offline or missing
	Kind  string `json:"kind"`  // wireguard, l2tp, openvpn, other or ""
	Label string `json:"label"`
}

type RouterOS struct {
	Version string `json:"version"`
	Min     string `json:"min"`
	OK      bool   `json:"ok"`
}

type Route struct {
	Address string `json:"address"`
	UsedFor string `json:"usedFor"` // downloads, entry-exit or nodes
	Via     string `json:"via"`     // foreign, vpn or domestic
	Ready   bool   `json:"ready"`
}

// Routes are the rows of the routes table on the Settings page. A row is ready
// when the router has the routing table its address is sent to.
func (f Facts) Routes() []Route {
	return []Route{
		{AddrDownloads, "downloads", "foreign", f.RouteForeign},
		{AddrExit, "entry-exit", "vpn", f.RouteVPN},
		{AddrNodes, "nodes", "domestic", f.RouteDomestic},
	}
}

// Entry is the page's view of this report at time now. The entry is on only
// when a Domestic link and a VPN are both up, all three routes are ready and
// RouterOS is at least minRouterOS. How old the report is doesn't decide
// that: the router's own rules fail closed, and the page warns about a stale
// report instead.
func (f Facts) Entry(now time.Time, minRouterOS string) Entry {
	// The script gives one up-or-down flag per link, which can't tell "no such
	// link" from "the link is down". Two more things say the router has the
	// link at all: the routing table that goes with it (the wizard only makes
	// it when it sets the link up) and, for the VPN, the name and kind the
	// script found on its interface. A link that is up is online whatever
	// else is missing. One that is down is offline if either sign is there
	// and missing if neither is.
	domestic := linkState(f.Domestic, f.RouteDomestic)
	vpn := linkState(f.VPN, f.RouteVPN || f.VPNKind != "" || f.VPNLabel != "")

	routes := f.Routes()
	routesReady := true
	for _, r := range routes {
		routesReady = routesReady && r.Ready
	}
	ros := RouterOS{Version: f.ROS, Min: minRouterOS, OK: AtLeast(f.ROS, minRouterOS)}

	e := Entry{
		On:       domestic == LinkOnline && vpn == LinkOnline && routesReady && ros.OK,
		Domestic: domestic,
		VPN:      VPN{State: vpn, Kind: f.VPNKind, Label: f.VPNLabel},
		Bridge:   BridgeStopped,
		RouterOS: ros,
		Routes:   routes,
	}
	if !f.Updated.IsZero() {
		at := f.Updated.UTC()
		e.CheckedAt = &at
	}
	// A report with no time can't be vouched for. One from the future can't
	// either: the router and the hub share a clock, so it means the script got
	// the time wrong, and counting it fresh would hide a dead script until the
	// clock caught up.
	age := now.Sub(f.Updated)
	e.Stale = f.Updated.IsZero() || age > StaleAfter || age < -StaleAfter
	if !e.On {
		e.Reason = f.why(minRouterOS, domestic, vpn, ros.OK)
	}
	return e
}

func linkState(up, exists bool) string {
	switch {
	case up:
		return LinkOnline
	case exists:
		return LinkOffline
	}
	return LinkMissing
}

// why lists what keeps the entry off. A route that isn't ready is left out
// when its link is down, since that already explains it.
func (f Facts) why(minRouterOS, domestic, vpn string, rosOK bool) string {
	var parts []string
	switch {
	case rosOK:
	case f.ROS == "":
		parts = append(parts, "the router did not report its RouterOS version")
	default:
		if c, err := CompareVersions(f.ROS, minRouterOS); err != nil {
			parts = append(parts, fmt.Sprintf("RouterOS %q can't be compared with the %s this plugin needs", f.ROS, minRouterOS))
		} else if c < 0 {
			parts = append(parts, fmt.Sprintf("RouterOS %s is older than the %s this plugin needs", f.ROS, minRouterOS))
		}
	}
	switch domestic {
	case LinkOffline:
		parts = append(parts, "the Domestic link is down")
	case LinkMissing:
		parts = append(parts, "the router has no Domestic link")
	}
	switch vpn {
	case LinkOffline:
		parts = append(parts, "the VPN is down")
	case LinkMissing:
		parts = append(parts, "the router has no VPN")
	}
	for _, r := range f.Routes() {
		if r.Ready || r.Via == "domestic" && domestic != LinkOnline || r.Via == "vpn" && vpn != LinkOnline {
			continue
		}
		parts = append(parts, fmt.Sprintf("the router's route for %s (%s) is not ready", r.UsedFor, r.Address))
	}
	return strings.Join(parts, "; ")
}
