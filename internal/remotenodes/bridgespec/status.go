// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import "time"

// StatusPath is where the bridge serves its Status as JSON.
const StatusPath = "/status"

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
