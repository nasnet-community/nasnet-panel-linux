// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"errors"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

var (
	// ErrNotEmpty is Restore's refusal to write over an install that has nodes
	// or subscriptions. A restore replaces everything, so it is for a fresh
	// install.
	ErrNotEmpty = errors.New("this install already has nodes or subscriptions")

	// ErrBadSnapshot is bytes Restore can't use as a state. The cause is
	// wrapped with it, so a snapshot from a newer release still matches
	// model.ErrNewer.
	ErrBadSnapshot = errors.New("the snapshot can't be restored")
)

// Snapshot is the state as state.json holds it, secrets included, for a
// backup. It is the caller's own copy and doesn't wait for a write.
func (s *Store) Snapshot() ([]byte, error) {
	return model.Encode(*s.cur.Load())
}

// Restore puts the state in data, which is what Snapshot returned, in place of
// an empty install's. It goes through Update, so the caps, the atomic write
// and the write counter are the same as for any other change, and the
// install can't stop being empty between the check and the write.
//
// The id counters never go back: an install that has handed out ids and then
// been emptied keeps them, so an id that is in a certificate somewhere isn't
// given to another node.
func (s *Store) Restore(data []byte) error {
	restored, err := model.Decode(data)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadSnapshot, err)
	}
	return s.Update(func(st *model.State) error {
		if len(st.Nodes) > 0 || len(st.Subscriptions) > 0 {
			return ErrNotEmpty
		}
		restored.NextNodeID = max(restored.NextNodeID, st.NextNodeID)
		restored.NextSubscriptionID = max(restored.NextSubscriptionID, st.NextSubscriptionID)
		*st = restored
		return nil
	})
}
