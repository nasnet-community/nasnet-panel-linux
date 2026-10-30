// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manager is the live side of the hub: the link to each node, its
// heartbeat and stats, the events the page shows, and the bridge process.
//
// All of that stays in memory. The router keeps its data on flash, which wears
// with every write, so only what has to survive a restart goes through
// store.Update, and traffic totals no more often than every few minutes.
package manager
