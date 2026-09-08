package accesslog

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestStoreTimestampOrderAndRingRetention(t *testing.T) {
	s := NewStore(3, 2)
	base := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	add := func(email string, seconds, id int) {
		s.Add(Entry{Email: email, Timestamp: base.Add(time.Duration(seconds) * time.Second), Port: id})
	}
	add("a", 90, 1)
	add("a", 10, 2)
	add("a", 50, 3)
	add("b", 50, 4)
	add("b", 20, 5)
	assertEntryIDs(t, s.GetAll(100), 1, 4, 3, 5, 2)
	assertEntryIDs(t, s.GetAll(3), 1, 4, 3)
	assertEntryIDs(t, s.GetByEmail("a", 100), 3, 2, 1)

	// The late record overwrites the oldest addition, even though that
	// overwritten record had the newest timestamp in the entire store.
	add("a", 5, 6)
	assertEntryIDs(t, s.GetAll(100), 4, 3, 5, 2, 6)
	assertEntryIDs(t, s.GetByEmail("a", 100), 6, 3, 2)
	assertStoreIndex(t, s)
}

func TestStoreEvictionUsesLastWriteAndRemovesAllIndexEntries(t *testing.T) {
	s := NewStore(3, 2)
	base := time.Now()
	for i := 1; i <= 4; i++ {
		s.Add(Entry{Email: "a", Port: i, Timestamp: base.Add(time.Duration(i) * time.Second)})
	}
	s.Add(Entry{Email: "b", Port: 5, Timestamp: base})
	s.Add(Entry{Email: "a", Port: 6, Timestamp: base}) // touch a
	s.GetByEmail("b", 1)                               // reads must not refresh the LRU
	s.Add(Entry{Email: "c", Port: 7, Timestamp: base})
	if got := s.GetByEmail("b", 10); got != nil {
		t.Fatalf("least recently written email remains: %+v", got)
	}
	assertEntryIDs(t, s.GetAll(100), 4, 3, 7, 6)
	assertStoreIndex(t, s)

	// Evict a wrapped, full ring and then recreate the same email. The old
	// heap nodes must neither leak into queries nor retain their ring.
	s.Add(Entry{Email: "b", Port: 8, Timestamp: base})
	assertEntryIDs(t, s.GetAll(100), 8, 7)
	s.Add(Entry{Email: "a", Port: 9, Timestamp: base})
	assertEntryIDs(t, s.GetAll(100), 9, 8)
	assertStoreIndex(t, s)
}

func TestStoreLimitsClearAndSnapshotIsolation(t *testing.T) {
	s := NewStore(0, 0)
	if s.maxPerEmail != 200 || s.maxEmails != 10000 {
		t.Fatal("non-positive capacities must use defaults")
	}
	if s.GetAll(1) != nil || s.Len() != 0 {
		t.Fatal("new store must be empty")
	}
	s.Add(Entry{Port: 1})
	if s.Len() != 0 {
		t.Fatal("entries without an email must be ignored")
	}
	for i := 1; i <= 120; i++ {
		s.Add(Entry{Email: "a", Port: i})
	}
	for _, limit := range []int{0, -1} {
		if got := s.GetAll(limit); len(got) != 100 || got[0].Port != 120 || got[99].Port != 21 {
			t.Fatalf("GetAll(%d) must default to the newest 100 entries", limit)
		}
		if got := s.GetByEmail("a", limit); got != nil {
			t.Fatalf("GetByEmail(%d) must preserve its empty-result behavior", limit)
		}
	}
	if got := s.GetAll(int(^uint(0) >> 1)); len(got) != 120 {
		t.Fatal("query allocation must be capped by retained history")
	}
	all, byEmail := s.GetAll(1), s.GetByEmail("a", 1)
	if cap(all) != len(all) {
		t.Fatal("small query retained a backing array larger than its result")
	}
	all[0].Email = "changed"
	byEmail[0].Port = -1
	if got := s.GetAll(1)[0]; got.Email != "a" || got.Port != 120 {
		t.Fatal("query results must not expose mutable ring slots")
	}
	s.Clear()
	if s.Len() != 0 || s.GetAll(100) != nil || s.GetByEmail("a", 10) != nil {
		t.Fatal("Clear retained data")
	}
	if s.recent != nil || s.order.Len() != 0 {
		t.Fatal("Clear must release the timestamp index and LRU")
	}
	s.Add(Entry{Email: "a", Port: 121})
	assertEntryIDs(t, s.GetAll(100), 121)
	assertStoreIndex(t, s)
}

func TestStoreMatchesReferenceUnderOutOfOrderWrites(t *testing.T) {
	for _, capacity := range []struct{ entries, emails int }{{1, 1}, {1, 5}, {7, 1}, {7, 5}, {200, 20}} {
		t.Run(fmt.Sprintf("entries%d_emails%d", capacity.entries, capacity.emails), func(t *testing.T) {
			s := NewStore(capacity.entries, capacity.emails)
			model := newReferenceStore(capacity.entries, capacity.emails)
			rng := rand.New(rand.NewSource(718))
			base := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
			for step := 1; step <= 5000; step++ {
				if step%997 == 0 {
					s.Clear()
					model = newReferenceStore(capacity.entries, capacity.emails)
				}
				email := fmt.Sprintf("user%d", rng.Intn(capacity.emails+2))
				// A small timestamp range deliberately exercises frequent ties
				// and backwards timestamps while rings wrap and users churn.
				entry := Entry{Email: email, Timestamp: base.Add(time.Duration(rng.Intn(30)) * time.Second), Port: step}
				s.Add(entry)
				model.add(entry)
				limit := rng.Intn(50) - 5
				if got, want := s.GetAll(limit), model.all(limit); !reflect.DeepEqual(got, want) {
					t.Fatalf("step %d, GetAll(%d):\ngot  %+v\nwant %+v", step, limit, got, want)
				}
				if got, want := s.GetByEmail(email, limit), model.byEmail(email, limit); !reflect.DeepEqual(got, want) {
					t.Fatalf("step %d, GetByEmail(%q, %d):\ngot  %+v\nwant %+v", step, email, limit, got, want)
				}
				if step%31 == 0 {
					assertStoreIndex(t, s)
				}
			}
			assertStoreIndex(t, s)
		})
	}
}

func TestStoreConcurrentReadsWritesAndClear(t *testing.T) {
	s := NewStore(13, 11)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				s.Add(Entry{Email: fmt.Sprintf("user%d", (i+worker)%17), Port: worker*2000 + i, Timestamp: time.Unix(int64(i%31), 0)})
			}
		}(worker)
	}
	for worker := 0; worker < 3; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				entries := s.GetAll(31)
				if len(entries) > 31 || !sort.SliceIsSorted(entries, func(i, j int) bool {
					return entries[i].Timestamp.After(entries[j].Timestamp)
				}) {
					t.Errorf("concurrent query returned too many or unsorted entries")
					return
				}
				if s.Len() > 13*11 || len(s.GetByEmail("user1", 100)) > 13 {
					t.Errorf("concurrent store exceeded retention bounds")
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.Clear()
		}
	}()
	wg.Wait()
	assertStoreIndex(t, s)
}

func assertEntryIDs(t *testing.T, entries []Entry, want ...int) {
	t.Helper()
	got := make([]int, len(entries))
	for i, entry := range entries {
		got[i] = entry.Port
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entry IDs = %v, want %v", got, want)
	}
}

func assertStoreIndex(t *testing.T, s *Store) {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.buffers) != s.order.Len() || len(s.buffers) > s.maxEmails {
		t.Fatal("email LRU and buffer count disagree or exceed capacity")
	}
	seen := make(map[*indexedEntry]bool)
	for _, buf := range s.buffers {
		if buf.count > s.maxPerEmail {
			t.Fatal("ring exceeded per-email capacity")
		}
		for i := 0; i < buf.count; i++ {
			slot := &buf.data[i]
			seen[slot] = true
			if slot.heapIndex < 0 || slot.heapIndex >= len(s.recent) || s.recent[slot.heapIndex] != slot {
				t.Fatal("ring slot missing from timestamp index")
			}
		}
	}
	if len(seen) != len(s.recent) {
		t.Fatal("timestamp index retained evicted or duplicate entries")
	}
	for i, entry := range s.recent {
		if !seen[entry] || entry.heapIndex != i {
			t.Fatal("timestamp index position is stale")
		}
		if i > 0 && entry.newer(s.recent[(i-1)/2]) {
			t.Fatal("timestamp index violates heap ordering")
		}
	}
	for _, slot := range s.recent[len(s.recent):cap(s.recent)] {
		if slot != nil {
			t.Fatal("unused index capacity still retains an evicted ring")
		}
	}
}

type referenceEntry struct {
	Entry
	sequence int
}

type referenceStore struct {
	buffers                    map[string][]referenceEntry
	order                      []string
	perEmail, emails, sequence int
}

func newReferenceStore(perEmail, emails int) *referenceStore {
	return &referenceStore{buffers: make(map[string][]referenceEntry), perEmail: perEmail, emails: emails}
}

func (m *referenceStore) add(entry Entry) {
	if _, ok := m.buffers[entry.Email]; ok {
		for i, email := range m.order {
			if email == entry.Email {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	} else if len(m.buffers) == m.emails {
		delete(m.buffers, m.order[0])
		m.order = m.order[1:]
	}
	m.order = append(m.order, entry.Email)
	m.sequence++
	rows := append(m.buffers[entry.Email], referenceEntry{Entry: entry, sequence: m.sequence})
	if len(rows) > m.perEmail {
		rows = rows[1:]
	}
	m.buffers[entry.Email] = rows
}

func (m *referenceStore) all(limit int) []Entry {
	if limit <= 0 {
		limit = 100
	}
	var rows []referenceEntry
	for _, entries := range m.buffers {
		rows = append(rows, entries...)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Timestamp.Equal(rows[j].Timestamp) {
			return rows[i].sequence > rows[j].sequence
		}
		return rows[i].Timestamp.After(rows[j].Timestamp)
	})
	if limit > len(rows) {
		limit = len(rows)
	}
	var result []Entry
	for _, row := range rows[:limit] {
		result = append(result, row.Entry)
	}
	return result
}

func (m *referenceStore) byEmail(email string, limit int) []Entry {
	rows := m.buffers[email]
	var result []Entry
	for i := len(rows) - 1; i >= 0 && len(result) < limit; i-- {
		result = append(result, rows[i].Entry)
	}
	return result
}
