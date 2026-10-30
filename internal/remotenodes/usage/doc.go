// SPDX-License-Identifier: AGPL-3.0-or-later

// Package usage counts what the subscriptions use and keeps the numbers the
// page shows: the bytes of today and of the last thirty days, a node's live
// rate, who is online, and a short chart. The bytes come from the traffic each
// node's agent buffers.
//
// The history is usage.json in the data folder, beside state.json. It holds a
// pair of counts for each UTC day of the last 31, for every node and for
// every subscription on it, the bridge's lifetime totals, and for each node
// the mark: the timestamp of the newest bucket of the agent's buffer that the
// counts include. The router's flash wears with every write, so the file is
// written only when its bytes would change, by the store's atomic routine, and
// holds nothing that isn't a count or an id. A corrupt file is kept beside
// itself and the history starts empty; a file from a newer release is left
// exactly as it is. The hub never stops over a counter.
//
// How the agent buffers is what the counting has to follow. It adds what Xray
// counted into buckets, an hour wide unless its config says otherwise, and
// keeps them until the hub acknowledges them (seven days at most). A bucket
// holds bytes, not counters, but it grows while its hour runs, and an
// acknowledgement drops it whole: the next bytes go into a new bucket with
// the same timestamp. So a timestamp doesn't say whether the bytes under it
// have been counted, and a mark that skips every bucket "not newer" than the
// last one acknowledged would throw away the rest of that hour.
//
// So a bucket is counted only once a newer one exists. Nothing is added to it
// after that, so it is counted once, in full, and can be written and then
// acknowledged without losing bytes or meeting its timestamp again. The newest
// bucket is still growing: the page shows it, but it is in memory only and
// the agent still holds it. If the hub dies, it is counted from the agent's
// copy; nothing is lost and nothing twice.
//
// The order is what makes the counting exact. A poll adds each completed
// bucket to the totals once and moves the node's mark to the newest of them.
// Flush writes the totals and the mark in one file, and only then tells the
// agent to drop everything up to the mark. A crash before the write forgets the
// poll, and the agent still has the buckets. A crash, or an acknowledgement
// that fails, after the write leaves buckets at the agent that the file already
// counts: they are not newer than the mark, so the next poll drops them and
// asks again.
//
// The agent's records show nothing of a restart of the agent or of Xray, and
// need to show nothing: the agent keeps its baseline on disk and puts only
// the difference of Xray's counters into the buckets, taking a counter that
// went backwards as new. What can happen to the hub's view is that an agent
// loses its buffer, and then the newest bucket starts again below what was
// shown, which costs those bytes and nothing more.
package usage
