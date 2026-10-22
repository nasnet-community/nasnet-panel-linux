package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/nasnet-community/nasnet-panel-linux/internal/update/domain"
)

// ErrBusy means another update or rollback holds the lock.
var ErrBusy = errors.New("another update is running")

// Paths are the updater's files, all under the install root.
type Paths struct {
	Root string
}

func (p Paths) Dir() string        { return filepath.Join(p.Root, "data", "update") }
func (p Paths) StateFile() string  { return filepath.Join(p.Dir(), "state.json") }
func (p Paths) LockFile() string   { return filepath.Join(p.Dir(), "update.lock") }
func (p Paths) Downloads() string  { return filepath.Join(p.Dir(), "downloads") }
func (p Paths) Staging() string    { return filepath.Join(p.Dir(), "staging") }
func (p Paths) Uploads() string    { return filepath.Join(p.Dir(), "uploads") }
func (p Paths) Prev() string       { return filepath.Join(p.Dir(), "prev") }
func (p Paths) Binary() string     { return filepath.Join(p.Root, "bin", "nasnet-panel") }
func (p Paths) PrevBinary() string { return p.Binary() + ".prev" }

// Store keeps the current job in state.json. The panel and the runner both
// write it, one after the other.
type Store struct {
	paths Paths
	mu    sync.Mutex
}

func NewStore(paths Paths) *Store { return &Store{paths: paths} }

// Load returns nil when no job ever ran.
func (s *Store) Load() (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (*domain.Job, error) {
	data, err := os.ReadFile(s.paths.StateFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j domain.Job
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("%s is unreadable: %w", s.paths.StateFile(), err)
	}
	return &j, nil
}

func (s *Store) Save(j *domain.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(j)
}

func (s *Store) save(j *domain.Job) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.paths.Dir(), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(s.paths.StateFile(), data, 0o600)
}

// Update loads the job, lets fn change it and saves it.
func (s *Store) Update(fn func(j *domain.Job)) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.load()
	if err != nil || j == nil {
		return j, err
	}
	fn(j)
	return j, s.save(j)
}

// TryLock takes the update lock. The kernel drops it if the holder dies.
func (s *Store) TryLock() (func(), error) {
	if err := os.MkdirAll(s.paths.Dir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.paths.LockFile(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, ErrBusy
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}

// writeFileAtomic writes next to path and renames over it, so readers see
// the old file or the new one, never half of it.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone already once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
