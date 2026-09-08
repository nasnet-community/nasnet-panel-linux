package accesslog

import (
	"container/heap"
	"container/list"
	"sync"
)

// Store is a thread-safe, bounded in-memory store for parsed access log entries,
// keyed by user email. Each email gets its own insertion-order ring buffer.
type Store struct {
	mu          sync.RWMutex
	buffers     map[string]*ringBuffer
	order       list.List // LRU emails, most recently written at the back
	recent      entryHeap
	sequence    uint64
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
		maxPerEmail: maxPerEmail,
		maxEmails:   maxEmails,
	}
}

// Add stores an entry in the per-email ring buffer. Updating the email LRU is
// O(1); maintaining the timestamp index is O(log retained entries).
// Email eviction removes at most maxPerEmail entries from the index.
func (s *Store) Add(entry Entry) {
	if entry.Email == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	buf, ok := s.buffers[entry.Email]
	if !ok {
		if len(s.buffers) >= s.maxEmails {
			oldest := s.order.Front()
			email := oldest.Value.(string)
			evicted := s.buffers[email]
			// Remove index entries immediately: a lazy heap would retain old
			// log strings and grow without bound as users are evicted.
			for i := 0; i < evicted.count; i++ {
				heap.Remove(&s.recent, evicted.data[i].heapIndex)
			}
			delete(s.buffers, email)
			s.order.Remove(oldest)
		}
		buf = newRingBuffer(s.maxPerEmail)
		buf.lru = s.order.PushBack(entry.Email)
		s.buffers[entry.Email] = buf
	} else {
		s.order.MoveToBack(buf.lru)
	}

	s.sequence++
	slot, overwritten := buf.write(entry, s.sequence)
	if overwritten {
		heap.Fix(&s.recent, slot.heapIndex)
	} else {
		heap.Push(&s.recent, slot)
	}
}

// GetByEmail returns the last-added entries for an email, newest addition first.
// Retention and ordering follow arrival order, including late log entries.
func (s *Store) GetByEmail(email string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	buf, ok := s.buffers[email]
	if !ok {
		return nil
	}
	return buf.tail(limit)
}

// GetAll returns the most recent retained entries by timestamp, newest first.
// Equal timestamps are ordered by most recent addition. A heap frontier visits
// only the requested entries and their children, so small reads do not scan or
// copy the full history while blocking ingestion: O(limit log limit) work and
// O(limit) result/frontier memory, independent of the retained history size.
func (s *Store) GetAll(limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	if limit > len(s.recent) {
		limit = len(s.recent)
	}
	if limit == 0 {
		return nil
	}

	result := make([]Entry, 0, limit)
	frontier := entryFrontier{entries: s.recent, indices: make([]int, 1, limit)}
	for len(result) < limit {
		idx := frontier.pop()
		result = append(result, s.recent[idx].Entry)
		if len(result) == limit {
			break
		}
		left := 2*idx + 1
		if left < len(s.recent) {
			frontier.push(left)
		}
		if right := left + 1; right < len(s.recent) {
			frontier.push(right)
		}
	}
	return result
}

// Len returns the total number of retained entries across all emails.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.recent)
}

// Clear removes all stored entries and their indexes.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffers = make(map[string]*ringBuffer)
	s.order.Init()
	s.recent = nil
	s.sequence = 0
}

// indexedEntry lives in a fixed ring slot. There is exactly one global heap
// pointer per occupied slot; overwrites fix its position without allocating.
type indexedEntry struct {
	Entry
	sequence  uint64
	heapIndex int
}

func (e *indexedEntry) newer(other *indexedEntry) bool {
	if e.Timestamp.Equal(other.Timestamp) {
		return e.sequence > other.sequence
	}
	return e.Timestamp.After(other.Timestamp)
}

// entryHeap is a max-heap ordered by timestamp and addition sequence. Positions
// let ring overwrites and email eviction update it without scanning the index.
type entryHeap []*indexedEntry

func (h entryHeap) Len() int           { return len(h) }
func (h entryHeap) Less(i, j int) bool { return h[i].newer(h[j]) }
func (h entryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}
func (h *entryHeap) Push(value any) {
	entry := value.(*indexedEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}
func (h *entryHeap) Pop() any {
	last := len(*h) - 1
	entry := (*h)[last]
	(*h)[last] = nil // release references to evicted rings
	*h = (*h)[:last]
	entry.heapIndex = -1
	return entry
}

// entryFrontier is a query-local heap of positions in the global heap. Its
// children can only be older than its parent, so expanding the newest candidate
// gives the next globally newest result, even with out-of-order log timestamps.
// Typed operations avoid boxing integer positions into heap.Interface on reads.
type entryFrontier struct {
	entries entryHeap
	indices []int
}

func (f *entryFrontier) push(idx int) {
	f.indices = append(f.indices, idx)
	child := len(f.indices) - 1
	for child > 0 {
		parent := (child - 1) / 2
		if !f.entries[idx].newer(f.entries[f.indices[parent]]) {
			break
		}
		f.indices[child] = f.indices[parent]
		child = parent
	}
	f.indices[child] = idx
}

func (f *entryFrontier) pop() int {
	result := f.indices[0]
	last := f.indices[len(f.indices)-1]
	f.indices = f.indices[:len(f.indices)-1]
	if len(f.indices) == 0 {
		return result
	}
	parent := 0
	for {
		child := 2*parent + 1
		if child >= len(f.indices) {
			break
		}
		if right := child + 1; right < len(f.indices) && f.entries[f.indices[right]].newer(f.entries[f.indices[child]]) {
			child = right
		}
		if !f.entries[f.indices[child]].newer(f.entries[last]) {
			break
		}
		f.indices[parent] = f.indices[child]
		parent = child
	}
	f.indices[parent] = last
	return result
}

// ringBuffer retains the last size additions for an email, independently of
// their timestamps. Its backing slots never move while the email is retained.
type ringBuffer struct {
	data  []indexedEntry
	lru   *list.Element
	head  int
	count int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{data: make([]indexedEntry, size)}
}

func (rb *ringBuffer) write(e Entry, sequence uint64) (*indexedEntry, bool) {
	slot := &rb.data[rb.head]
	overwritten := rb.count == len(rb.data)
	slot.Entry = e
	slot.sequence = sequence
	rb.head = (rb.head + 1) % len(rb.data)
	if !overwritten {
		rb.count++
	}
	return slot, overwritten
}

// tail returns the last n additions, newest first.
func (rb *ringBuffer) tail(n int) []Entry {
	if n <= 0 || rb.count == 0 {
		return nil
	}
	if n > rb.count {
		n = rb.count
	}
	result := make([]Entry, n)
	for i := 0; i < n; i++ {
		idx := (rb.head - 1 - i + len(rb.data)) % len(rb.data)
		result[i] = rb.data[idx].Entry
	}
	return result
}
