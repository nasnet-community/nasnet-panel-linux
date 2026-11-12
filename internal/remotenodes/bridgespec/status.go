// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import "time"

// StatusPath is where the bridge serves its Status as JSON.
const StatusPath = "/status"

// ClientsPath is where the hub puts the entry's clients while the bridge runs,
// on the status page's address, with the spec's token in TokenHeader.
const (
	ClientsPath = "/local/clients"
	TokenHeader = "X-Bridge-Token"
)

// ClientsRequest is the whole set of clients the entry should have. Sending
// the same set again changes nothing, so the hub can send it whenever it is
// unsure what the bridge holds.
type ClientsRequest struct {
	Clients []Client `json:"clients"`
}

// ClientsResult is what the bridge did to get there, by email and sorted.
type ClientsResult struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	// Replaced kept their email and got another id.
	Replaced []string `json:"replaced"`
	// Closed is how many open connections were ended: those of the clients
	// removed and replaced, which Xray would have let run to their end.
	Closed int `json:"closed"`
}

// Status is what the bridge's status page returns. The hub reads it through
// the supervisor, so it never needs the Xray core's own API types.
type Status struct {
	// Running is true while the bridge serves traffic; it turns false for the
	// moment the bridge is shutting down.
	Running bool      `json:"running"`
	Started time.Time `json:"started"`
	// Xray is the version of the core the bridge was built with. Nodes run
	// the same pin, so the hub compares the two.
	Xray  string       `json:"xray"`
	Nodes []NodeStatus `json:"nodes"`
	// Local is the entry on the router itself, nil when the spec has none.
	Local *LocalStatus `json:"local,omitempty"`
}

// LocalStatus is the router's own entry.
type LocalStatus struct {
	// Clients are the subscriptions the entry has had since the bridge
	// started, by email: those on it now and those taken off, whose counts the
	// hub may not have read yet.
	Clients []ClientStatus `json:"clients"`
}

// ClientStatus is one subscription on the router's entry.
type ClientStatus struct {
	Email string `json:"email"`
	// On is whether it may connect now.
	On bool `json:"on"`
	// BytesUp and BytesDown count what it has moved since the bridge started,
	// from where it sits: up goes toward the internet, down comes back. Taking
	// it off and putting it back does not start them over.
	BytesUp   uint64 `json:"bytes_up"`
	BytesDown uint64 `json:"bytes_down"`
	// Connections is how many of its connections to the entry carry something
	// right now, and Addrs is where they come from, each address once.
	Connections int      `json:"connections"`
	Addrs       []string `json:"addrs,omitempty"`
	// LastSeen is when it last opened something through the entry, zero if it
	// hasn't since the bridge started.
	LastSeen time.Time `json:"last_seen,omitzero"`
}

// NodeStatus is one node's tunnel.
type NodeStatus struct {
	Tag string `json:"tag"`
	// Up means the bridge holds a connection to the portal that has lasted a
	// few seconds and still hears from it. Since is when Up last changed, or
	// when the bridge started if it never has.
	Up    bool      `json:"up"`
	Since time.Time `json:"since"`
	// BytesUp and BytesDown count the subscriptions' traffic from where they
	// sit: up goes toward the internet, down comes back.
	BytesUp   uint64 `json:"bytes_up"`
	BytesDown uint64 `json:"bytes_down"`
	// Connections is the number of TCP connections open through the tunnel
	// right now. UDP flows aren't counted.
	Connections int `json:"connections"`
}
