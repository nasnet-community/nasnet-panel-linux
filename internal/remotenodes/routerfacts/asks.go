// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The files the hub leaves for the router's script, in the folder of its
// report. The script looks at them on every run, once a minute.
const (
	// askLocal holds "on" while the router should forward a port on its
	// Domestic link to the router's own entry. Absent means it should not.
	askLocal = "local"
	// askHost holds the name the entry's links carry when one was set by hand,
	// so the script can count its address among those a device at home reaches
	// the entry by. Absent means the router's own name is used.
	askHost = "host"
	// askRequest holds one thing for the script to do once; it removes the
	// file when it has. "new-port" is the only one.
	askRequest = "request"

	requestNewPort = "new-port"
)

// Asks writes what the hub wants of the router. A file is written only when
// what it holds would change, to spare the flash, and it is replaced whole,
// so the script never reads half of one.
type Asks struct {
	dir string
	mu  sync.Mutex
}

// NewAsks leaves its files beside the report of the same data folder.
func NewAsks(dataDir string) *Asks { return &Asks{dir: filepath.Dir(Path(dataDir))} }

// SetLocal says whether the router should forward a port to its own entry.
func (a *Asks) SetLocal(on bool) error {
	if on {
		return a.put(askLocal, "on")
	}
	return a.put(askLocal, "")
}

// SetHost leaves the name the entry's links carry, or takes it away with "".
func (a *Asks) SetHost(host string) error {
	if strings.ContainsAny(host, "\r\n") {
		return fmt.Errorf("routerfacts: a host name can't hold a line break")
	}
	return a.put(askHost, host)
}

// NewPort asks the router to pick another port for the entry. Asking again
// before it has is the same request.
func (a *Asks) NewPort() error { return a.put(askRequest, requestNewPort) }

// put makes the file hold one line, or removes it for an empty one.
func (a *Asks) put(name, line string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	path := filepath.Join(a.dir, name)
	if line == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("routerfacts: %w", err)
		}
		return nil
	}
	want := []byte(line + "\n")
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return nil
	}
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return fmt.Errorf("routerfacts: %w", err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, want, 0o644); err != nil {
		return fmt.Errorf("routerfacts: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("routerfacts: %w", err)
	}
	return nil
}
