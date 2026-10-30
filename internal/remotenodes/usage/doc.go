// SPDX-License-Identifier: AGPL-3.0-or-later

// Package usage keeps what the subscriptions have used, and the numbers the
// page shows from it: the bytes of today and of the last thirty days, a node's
// live rate, who is online, and a short chart.
//
// The history is usage.json in the data folder, beside state.json. It holds a
// pair of counts for each UTC day of the last 31, for every node and for
// every subscription on it, the bridge's lifetime totals, and for each node
// the mark: the timestamp of the newest bucket of the agent's buffer that the
// counts include. The router's flash wears with every write, so the file is
// written only when its bytes would change, by the store's atomic routine, and
// holds nothing that isn't a count or an id.
//
// A corrupt file is kept beside itself and the history starts empty, and a
// file from a newer release is left exactly as it is, for the hub never to
// stop over a counter.
package usage
