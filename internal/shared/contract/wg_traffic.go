package contract

import "time"

// WGUsageDelta is a peer's incremental traffic and most recent activity time.
// A zero LastSeen leaves the stored activity time unchanged.
type WGUsageDelta struct {
	PeerID   uint
	Upload   int64
	Download int64
	LastSeen time.Time
}
