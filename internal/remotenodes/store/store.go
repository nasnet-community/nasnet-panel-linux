// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store keeps what has to survive a restart in one JSON file,
// state.json under DATA_DIR.
//
// Live data does not belong here. Heartbeats, stats, logs and who is online
// change every few seconds, and the router's flash wears with every write, so
// the manager keeps them in memory and calls Update only for what is worth
// keeping, such as traffic totals every few minutes. Update writes only when
// the bytes would change, and Writes counts the writes so a soak test can
// read the rate off /health.
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

const FileName = "state.json"

// ErrCorrupt means state.json exists but can't be used. Open has kept a copy
// beside it and left the file itself alone.
var ErrCorrupt = errors.New("state file is corrupt")

type Store struct {
	path string
	ops  diskOps

	mu     sync.Mutex // one writer at a time; readers never wait for the disk
	enc    []byte     // the current state as it would be written
	cur    atomic.Pointer[model.State]
	writes atomic.Uint64
}

// Open loads dir/state.json, or starts empty when there is none; nothing is
// written until something changes. A file that can't be used is an error and
// is never replaced: a reset that nobody asked for would lose the CA, and
// with it every node.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tighten(dir)
	dropStrayTemps(dir)

	s := &Store{path: filepath.Join(dir, FileName), ops: osOps()}
	st := model.NewState()
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("store: %w", err)
	default:
		if st, err = model.Decode(b); err != nil {
			return nil, refuse(s.path, b, err)
		}
	}
	if s.enc, err = model.Encode(st); err != nil {
		return nil, err
	}
	s.cur.Store(&st)
	return s, nil
}

func (s *Store) Path() string { return s.path }

// Read is a snapshot the caller may change freely.
func (s *Store) Read() model.State { return s.cur.Load().Clone() }

// Writes is how many times the file has been written since Open.
func (s *Store) Writes() uint64 { return s.writes.Load() }

// Update runs fn on a copy of the state and keeps the result only if fn, the
// checks (the caps among them) and the write all succeed, so a failure leaves
// the state in memory and the file as they were. A result that would write
// the same bytes writes nothing.
func (s *Store) Update(fn func(*model.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := *s.cur.Load()
	next := before.Clone()
	if err := fn(&next); err != nil {
		return err
	}
	if err := next.CheckEdit(before); err != nil {
		return err
	}
	enc, err := model.Encode(next)
	if err != nil {
		return err
	}
	if bytes.Equal(enc, s.enc) {
		return nil
	}
	if err := writeAtomic(s.ops, s.path, enc); err != nil {
		return err
	}
	// a copy, so a closure that kept its pointer can't reach into what readers see
	published := next.Clone()
	s.cur.Store(&published)
	s.enc = enc
	s.writes.Add(1)
	return nil
}

// tighten takes group and other access off the folder: it holds the CA key
// and every link, and a mount point can arrive as 0755. Best effort, since
// the files inside are 0600 either way.
func tighten(dir string) {
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(dir, fi.Mode().Perm()&^0o077)
	}
}

// dropStrayTemps removes what a crash left between creating a temp file and
// renaming it.
func dropStrayTemps(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "."+FileName+"-") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// refuse is Open's answer to a file it can't use. A file from a newer release
// is fine as it is, so only the others get a copy.
func refuse(path string, b []byte, cause error) error {
	if errors.Is(cause, model.ErrNewer) {
		return fmt.Errorf("store: %s: %w", path, cause)
	}
	kept, err := keepCopy(path, b)
	if err != nil {
		return fmt.Errorf("%w: %s: %v (no copy could be kept: %v)", ErrCorrupt, path, cause, err)
	}
	return fmt.Errorf("%w: %s: %v (a copy is in %s; fix state.json or move it away to start empty)", ErrCorrupt, path, cause, kept)
}

// keepCopy saves b as state.json.corrupt-<UTC time>. If a copy with the same
// bytes is there already it stays the only one, so a container that restarts
// in a loop doesn't fill the flash with the same file.
func keepCopy(path string, b []byte) (string, error) {
	dir, prefix := filepath.Dir(path), filepath.Base(path)+".corrupt-"
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), prefix) {
			if old, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && bytes.Equal(old, b) {
				return filepath.Join(dir, e.Name()), nil
			}
		}
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for i := 0; i < 100; i++ {
		name := filepath.Join(dir, prefix+stamp)
		if i > 0 {
			name += "-" + strconv.Itoa(i) // two different files in one second
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(name)
			return "", err
		}
		return name, nil
	}
	return "", errors.New("too many copies of the same second")
}
