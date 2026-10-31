// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"strings"
	"unicode/utf8"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
)

// The kinds of event on a node's list, as the API contract has them. The page
// words each itself. This slice reports the first six; the later ones come
// from the code that renews, rotates and switches, through Event.
const (
	EventTunnelUp        = "tunnel_up"
	EventTunnelDown      = "tunnel_down"
	EventNodeUnreachable = "node_unreachable"
	EventNodeBack        = "node_back"
	EventAgentUpdated    = "agent_updated"
	EventXrayChanged     = "xray_changed"
	EventCertRenewed     = "cert_renewed"
	EventSubscriptionOn  = "subscription_on"
	EventSubscriptionOff = "subscription_off"
	EventKeysRotated     = "keys_rotated"
)

const (
	// eventsKeep is how many events a node keeps; the oldest go first.
	eventsKeep = 50
	// maxDetail is the longest an event's text is kept, in characters.
	maxDetail = 200
	maxKind   = 32
)

// eventRing is a node's list, oldest first, and never more than eventsKeep.
type eventRing struct{ list []api.NodeEvent }

func (r *eventRing) add(e api.NodeEvent) {
	r.list = append(r.list, e)
	if over := len(r.list) - eventsKeep; over > 0 {
		r.list = append(r.list[:0:0], r.list[over:]...) // a copy, so the dropped ones can be collected
	}
}

// newestFirst is a copy of the list the way the page reads it. Never nil.
func (r *eventRing) newestFirst() []api.NodeEvent {
	out := make([]api.NodeEvent, len(r.list))
	for i, e := range r.list {
		out[len(r.list)-1-i] = e
	}
	return out
}

// oneLine is text for the list: one line, not too long. An error from the
// network can run to several.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxDetail {
		s = string([]rune(s)[:maxDetail-1]) + "…"
	}
	return s
}

// Event adds an event to a node's list, stamped now. It is how what no poll
// can see gets there: a renewed certificate, rotated keys, a subscription
// switched on or off. An event for a node that is not in the state, or with no
// kind, is dropped.
func (m *Manager) Event(nodeID uint, kind, detail string) {
	if kind == "" || !m.inState(nodeID) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventLocked(nodeID, kind, detail)
}

// eventLocked adds to the list of a node, which the caller knows to exist.
func (m *Manager) eventLocked(nodeID uint, kind, detail string) {
	if len(kind) > maxKind {
		kind = kind[:maxKind]
	}
	m.recordLocked(nodeID).events.add(api.NodeEvent{At: m.now().UTC(), Kind: kind, Detail: oneLine(detail)})
}

// NodeEvents is a node's recent events, newest first. Never nil.
func (m *Manager) NodeEvents(id uint) []api.NodeEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l := m.nodes[id]; l != nil {
		return l.events.newestFirst()
	}
	return []api.NodeEvent{}
}
