package accesslog

import (
	"fmt"
	"sort"
	"testing"
	"time"
)

var benchmarkEntries []Entry

// Compare the indexed query with the previous production copy-and-sort
// algorithm on the same retained data. Run with:
// go test ./internal/agent/accesslog -run '^$' -bench BenchmarkStoreGetAll -benchmem
func BenchmarkStoreGetAll(b *testing.B) {
	for _, emails := range []int{50, 5000} {
		b.Run(fmt.Sprintf("retained=%d", emails*200), func(b *testing.B) {
			s := NewStore(200, emails)
			base := time.Unix(1700000000, 0)
			for user := 0; user < emails; user++ {
				email := fmt.Sprintf("user%d@example.com", user)
				for entry := 0; entry < 200; entry++ {
					s.Add(Entry{
						Email: email, Timestamp: base.Add(time.Duration(entry*emails+user) * time.Second),
						Domain: "example.com", Status: "accepted", SourceIP: "192.0.2.1", Port: 443,
					})
				}
			}
			b.Run("indexed", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchmarkEntries = s.GetAll(100)
				}
			})
			b.Run("previous_copy_sort", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchmarkEntries = previousGetAll(s, 100)
				}
			})
		})
	}
}

func previousGetAll(s *Store, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []Entry
	for _, buf := range s.buffers {
		all = append(all, buf.tail(buf.count)...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.After(all[j].Timestamp)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

func BenchmarkStoreAdd(b *testing.B) {
	for _, emails := range []int{1, 10000} {
		for _, access := range []string{"hot_email", "round_robin"} {
			b.Run(fmt.Sprintf("emails=%d/%s", emails, access), func(b *testing.B) {
				entries := make([]Entry, emails)
				for i := range entries {
					entries[i] = Entry{Email: fmt.Sprintf("user%d@example.com", i), Timestamp: time.Unix(1700000000, 0)}
				}
				b.Run("indexed", func(b *testing.B) {
					s := NewStore(1, emails)
					for _, entry := range entries {
						s.Add(entry)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						idx := 0
						if access == "round_robin" {
							idx = i % emails
						}
						s.Add(entries[idx])
					}
				})
				b.Run("previous_slice_lru", func(b *testing.B) {
					s := newPreviousAddStore(entries)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						idx := 0
						if access == "round_robin" {
							idx = i % emails
						}
						s.add(entries[idx])
					}
				})
			})
		}
	}
}

// Minimal copy of the previous Add path for a full one-entry-per-email store.
// The benchmark never creates or evicts emails, so only its old steady-state
// map lookup, slice LRU update, and ring overwrite are needed.
type previousAddStore struct {
	Store   // supplies the same RWMutex
	entries map[string]*Entry
	order   []string
}

func newPreviousAddStore(entries []Entry) *previousAddStore {
	s := &previousAddStore{entries: make(map[string]*Entry)}
	for _, entry := range entries {
		s.entries[entry.Email] = &Entry{}
		s.order = append(s.order, entry.Email)
	}
	return s
}

func (s *previousAddStore) add(entry Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := s.entries[entry.Email]
	for i, email := range s.order {
		if email == entry.Email {
			s.order = append(s.order[:i], s.order[i+1:]...)
			s.order = append(s.order, entry.Email)
			break
		}
	}
	*slot = entry
}
