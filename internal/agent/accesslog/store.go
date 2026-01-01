package accesslog

import (
	"sort"
	"sync"
)

// Store is a thread-safe, bounded in-memory store for parsed access log entries,
// keyed by user email. Each email gets its own ring buffer.
type Store struct {
	mu          sync.RWMutex
	buffers     map[string]*ringBuffer
	order       []string // LRU order of emails (most recent at end)
	maxPerEmail int
	maxEmails   int
}

// NewStore creates a new access log store.
// maxPerEmail is the max entries kept per email (default 200).
// maxEmails is the max distinct emails tracked (default 10000).
func NewStore(maxPerEmail, maxEmails int) *Store {
	if maxPerEmail <= 0 {
		maxPerEmail = 200
	}
	if maxEmails <= 0 {
		maxEmails = 10000
	}
	return &Store{
		buffers:     make(map[string]*ringBuffer),
		order:       make([]string, 0),
		maxPerEmail: maxPerEmail,
		maxEmails:   maxEmails,
	}
}

// Add stores an entry in the per-email ring buffer.
func (s *Store) Add(entry Entry) {
	if entry.Email == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	buf, ok := s.buffers[entry.Email]
	if !ok {
		// Evict oldest email if at capacity
		if len(s.buffers) >= s.maxEmails && len(s.order) > 0 {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.buffers, oldest)
		}
		buf = newRingBuffer(s.maxPerEmail)
		s.buffers[entry.Email] = buf
		s.order = append(s.order, entry.Email)
	} else {
		// Move email to end of LRU
		s.touchLocked(entry.Email)
	}

	buf.write(entry)
}

// GetByEmail returns the most recent entries for a specific email, newest first.
func (s *Store) GetByEmail(email string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	buf, ok := s.buffers[email]
	if !ok {
		return nil
	}
	return buf.tail(limit)
}

// GetAll returns the most recent entries across all emails, newest first.
func (s *Store) GetAll(limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	// Collect all entries then sort by timestamp descending
	var all []Entry
	for _, buf := range s.buffers {
		all = append(all, buf.all()...)
	}

	// Sort descending by timestamp
	sortEntriesDesc(all)

	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// Len returns the total number of entries across all emails.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, buf := range s.buffers {
		total += buf.count
	}
	return total
}

// Clear removes all stored entries.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffers = make(map[string]*ringBuffer)
	s.order = s.order[:0]
}

// touchLocked moves email to the end of the LRU order. Must hold s.mu.
func (s *Store) touchLocked(email string) {
	for i, e := range s.order {
		if e == email {
			s.order = append(s.order[:i], s.order[i+1:]...)
			s.order = append(s.order, email)
			return
		}
	}
}

// sortEntriesDesc sorts entries by timestamp descending (newest first).
func sortEntriesDesc(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
}

// ringBuffer is a fixed-size circular buffer of Entry values.
type ringBuffer struct {
	data  []Entry
	size  int
	head  int
	count int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		data: make([]Entry, size),
		size: size,
	}
}

func (rb *ringBuffer) write(e Entry) {
	rb.data[rb.head] = e
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// tail returns the last n entries, newest first.
func (rb *ringBuffer) tail(n int) []Entry {
	if n <= 0 || rb.count == 0 {
		return nil
	}
	if n > rb.count {
		n = rb.count
	}
	result := make([]Entry, n)
	for i := 0; i < n; i++ {
		idx := (rb.head - 1 - i + rb.size) % rb.size
		result[i] = rb.data[idx]
	}
	return result
}

// all returns all entries, newest first.
func (rb *ringBuffer) all() []Entry {
	return rb.tail(rb.count)
}
