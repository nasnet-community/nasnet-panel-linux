// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manager is the live side of the hub: the link to each node, its
// heartbeat and stats, the events the page shows, the router's entry gate and
// the bridge process.
//
// All of that stays in memory. The router keeps its data on flash, which wears
// with every write, so only what has to survive a restart goes through
// store.Update (the CA, and a node's versions when they change), and traffic
// totals no more often than every few minutes.
//
// One Manager does everything, with its methods grouped by concern: nodes.go
// keeps a loop for each node, entry.go reads the router's report, domestic.go
// restarts a node's Xray when the router's address changes, bridge.go
// builds the bridge's spec and runs the supervisor, tunnel.go says what each
// node's tunnel is doing and live.go answers the API. subs.go changes what the
// subscriptions ask of the nodes and backup.go exports the state and restores
// it. The operations on a node, which add it, change its settings, update it,
// renew its certificate and take it out, are install.go, settings.go,
// updates.go, certs.go and remove.go; the ones that take a while run as jobs,
// over the Ops that ops.go describes. Everything it depends on is a small
// interface defined in deps.go, so a test stands in with a fake.
//
// A node that Sync has been told of gets a loop of its own: connect, poll every
// few seconds, give the connection up after three polls with no answer and
// connect again. The loops, the router's entry gate, the bridge, the hourly
// look at the release and the daily look at the certificates run between Run
// and Close.
package manager
