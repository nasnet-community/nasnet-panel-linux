// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgectl

import (
	"strings"
	"sync"
)

// ring keeps the last bytes written to it, in memory and no more than size of
// them. The bridge's output lands here instead of on flash, and the hub
// shows it when something goes wrong.
type ring struct {
	mu   sync.Mutex
	buf  []byte
	next int  // where the next byte goes
	full bool // the buffer has wrapped, so the oldest bytes are gone
}

func newRing(size int) *ring { return &ring{buf: make([]byte, size)} }

// Write never fails and never blocks on a full buffer: it overwrites the oldest bytes.
func (r *ring) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	size := len(r.buf)
	if len(p) >= size {
		copy(r.buf, p[len(p)-size:])
		r.next, r.full = 0, true
		return len(p), nil
	}
	n := copy(r.buf[r.next:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
		r.full = true
	}
	r.next = (r.next + len(p)) % size
	if r.next == 0 {
		r.full = true
	}
	return len(p), nil
}

// snapshot is everything the ring holds, oldest first, and whether it has
// wrapped, which means the oldest bytes are gone.
func (r *ring) snapshot() (b []byte, wrapped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]byte(nil), r.buf[:r.next]...), false
	}
	return append(append([]byte(nil), r.buf[r.next:]...), r.buf[:r.next]...), true
}

func (r *ring) bytes() []byte {
	b, _ := r.snapshot()
	return b
}

// String is what the ring holds, oldest first. Once it has wrapped the
// first line is probably cut, so it is dropped.
func (r *ring) String() string {
	b, wrapped := r.snapshot()
	if wrapped {
		if _, rest, ok := strings.Cut(string(b), "\n"); ok {
			return rest
		}
	}
	return string(b)
}
