package traffic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const fileStoreVersion = 2

type fileStoreData struct {
	Version  int                `json:"version"`
	Buckets  []*TrafficBucket   `json:"buckets"`
	Baseline *XrayStatsSnapshot `json:"baseline,omitempty"`
}

// FileStore wraps a MemoryStore with periodic JSON flush to disk.
type FileStore struct {
	mem      *MemoryStore
	path     string
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewFileStore creates a disk-persisted traffic store.
// It loads existing data from path on startup and flushes periodically.
func NewFileStore(path string, bucketDuration, retention, flushInterval time.Duration) (*FileStore, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	mem := NewMemoryStore(bucketDuration, retention)

	fs := &FileStore{
		mem:      mem,
		path:     path,
		interval: flushInterval,
		stopCh:   make(chan struct{}),
	}

	fs.loadFromDisk()

	// Start periodic flush
	fs.wg.Add(1)
	go fs.flushLoop()

	return fs, nil
}

func (fs *FileStore) loadFromDisk() {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if !os.IsNotExist(err) {
			logrus.WithError(err).Warn("[traffic] Failed to read store file, starting fresh")
		}
		return
	}

	var stored fileStoreData
	if err := json.Unmarshal(data, &stored); err != nil {
		logrus.WithError(err).Warn("[traffic] Corrupt store file, starting fresh")
		return
	}

	// Versions 1 and 2 share the bucket schema. v2 additionally persists a
	// baseline snapshot used by the collector for delta computation.
	if stored.Version != 1 && stored.Version != fileStoreVersion {
		logrus.Warnf("[traffic] Store file version %d unsupported, starting fresh", stored.Version)
		return
	}

	fs.mem.mu.Lock()
	defer fs.mem.mu.Unlock()
	for _, b := range stored.Buckets {
		if b.UserUplink == nil {
			b.UserUplink = make(map[string]int64)
		}
		if b.UserDownlink == nil {
			b.UserDownlink = make(map[string]int64)
		}
		if b.InboundUplink == nil {
			b.InboundUplink = make(map[string]int64)
		}
		if b.InboundDownlink == nil {
			b.InboundDownlink = make(map[string]int64)
		}
		if b.OutboundUplink == nil {
			b.OutboundUplink = make(map[string]int64)
		}
		if b.OutboundDownlink == nil {
			b.OutboundDownlink = make(map[string]int64)
		}
		fs.mem.buckets[b.Timestamp] = b
	}
	if stored.Baseline != nil {
		fs.mem.baseline = stored.Baseline
	}

	logrus.Infof("[traffic] Loaded %d buckets from disk (v%d)", len(stored.Buckets), stored.Version)
}

func (fs *FileStore) flush() {
	buckets := fs.mem.GetAll()
	baseline := fs.mem.GetBaseline()

	stored := fileStoreData{
		Version:  fileStoreVersion,
		Buckets:  buckets,
		Baseline: baseline,
	}

	data, err := json.Marshal(stored)
	if err != nil {
		logrus.WithError(err).Warn("[traffic] Failed to marshal store data")
		return
	}

	// Atomic write: tmp + rename
	tmpPath := fs.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		logrus.WithError(err).Warn("[traffic] Failed to write temp store file")
		return
	}

	if err := os.Rename(tmpPath, fs.path); err != nil {
		logrus.WithError(err).Warn("[traffic] Failed to rename store file")
	}
}

func (fs *FileStore) flushLoop() {
	defer fs.wg.Done()
	ticker := time.NewTicker(fs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fs.flush()
		case <-fs.stopCh:
			return
		}
	}
}

func (fs *FileStore) Accumulate(snapshot *XrayStatsSnapshot, now time.Time) {
	fs.mem.Accumulate(snapshot, now)
}

func (fs *FileStore) GetAll() []*TrafficBucket {
	return fs.mem.GetAll()
}

func (fs *FileStore) Drain(throughTime int64) {
	fs.mem.Drain(throughTime)
	// Flush immediately after drain to persist the removal
	fs.flush()
}

func (fs *FileStore) GetBaseline() *XrayStatsSnapshot {
	return fs.mem.GetBaseline()
}

func (fs *FileStore) SetBaseline(snapshot *XrayStatsSnapshot) {
	fs.mem.SetBaseline(snapshot)
}

func (fs *FileStore) Close() error {
	close(fs.stopCh)
	fs.wg.Wait()
	// Final flush
	fs.flush()
	return nil
}

// Aggregate delegates to the underlying MemoryStore.
func (fs *FileStore) Aggregate() *XrayStatsSnapshot {
	return fs.mem.Aggregate()
}

// DrainAll delegates to the underlying MemoryStore and flushes.
func (fs *FileStore) DrainAll() *XrayStatsSnapshot {
	agg := fs.mem.DrainAll()
	fs.flush()
	return agg
}
