// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// ErrEmpty is a report with nothing in it. The script never writes one,
	// so it is a file caught between being emptied and being filled.
	ErrEmpty = errors.New("the router's report is empty")

	// ErrUnsettled is a report that kept changing while it was read.
	ErrUnsettled = errors.New("the router's report kept changing while it was read")
)

const (
	// A file's time is only as fine as its filesystem keeps it, down to two
	// seconds on some. A change inside that window of the last time we read
	// can leave the same time and size, so a read this fresh isn't trusted
	// to stand for the file.
	racyWindow = 2 * time.Second

	// settleTries is how many times a read is tried again when the file
	// doesn't hold still.
	settleTries = 3
)

// Reader hands out the router's latest report. It looks at the file's time and
// size on every call and reads it again only when they changed, so it is cheap
// enough to call for every request.
type Reader struct {
	fsys fs.FS
	name string // in fsys
	path string // for messages

	now   func() time.Time
	pause time.Duration // between tries when the file doesn't hold still

	mu    sync.Mutex
	have  bool
	racy  bool // the read was too close to the file's time to count as settled
	mod   time.Time
	size  int64
	facts Facts
}

// NewReader reads the report at path, which is Path(dataDir) in the hub.
func NewReader(path string) *Reader {
	return newReader(os.DirFS(filepath.Dir(path)), filepath.Base(path), path)
}

func newReader(fsys fs.FS, name, path string) *Reader {
	return &Reader{fsys: fsys, name: name, path: path, now: time.Now, pause: 5 * time.Millisecond}
}

// Read is the latest report, or why there is none: a missing file satisfies
// errors.Is(err, fs.ErrNotExist). It never returns a report that was caught
// being rewritten.
func (r *Reader) Read() (Facts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fi, err := fs.Stat(r.fsys, r.name)
	if err != nil {
		r.have = false
		return Facts{}, err
	}
	if r.have && !r.racy && fi.Size() == r.size && fi.ModTime().Equal(r.mod) {
		return r.facts, nil
	}
	if fi.Size() > maxFile {
		r.have = false
		return Facts{}, ErrTooLarge
	}
	b, err := r.settle()
	if err != nil {
		r.have = false
		return Facts{}, err
	}
	// What was stat'd before the read is what the report is filed under. If
	// the file changed in between, the next call sees a different time or size
	// and reads it again.
	r.facts, r.have = parse(b), true
	r.mod, r.size = fi.ModTime(), fi.Size()
	r.racy = r.now().Sub(r.mod) < racyWindow
	return r.facts, nil
}

// Entry is the page's view of the router's report at time now. A report that
// can't be had gives an entry that is off, with no time to show and the cause
// in Reason.
func (r *Reader) Entry(now time.Time, minRouterOS string) Entry {
	f, err := r.Read()
	if err == nil {
		return f.Entry(now, minRouterOS)
	}
	e := Facts{}.Entry(now, minRouterOS)
	e.Stale = false // there is no report to be old
	e.Reason = r.explain(err)
	return e
}

func (r *Reader) explain(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("the router has not written its report (%s) yet", r.path)
	case errors.Is(err, ErrTooLarge):
		return fmt.Sprintf("the router's report (%s) is far larger than it should be", r.path)
	case errors.Is(err, ErrEmpty), errors.Is(err, ErrUnsettled):
		return fmt.Sprintf("%s (%s)", err, r.path)
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return fmt.Sprintf("the router's report (%s) can't be read: %v", r.path, err)
}

// settle returns the file's bytes once two reads in a row agree.
//
// The script rewrites the file in place, so a read can land between the
// emptying and the filling, or halfway through. Reading the file into memory
// and parsing that copy keeps the parse from changing under its feet, but the
// copy itself could still be half a report. Two reads that match, and aren't
// empty, are not.
func (r *Reader) settle() ([]byte, error) {
	var last []byte
	for try := 0; try < settleTries; try++ {
		if try > 0 {
			time.Sleep(r.pause)
		}
		a, err := r.slurp()
		if err != nil {
			return nil, err
		}
		b, err := r.slurp()
		if err != nil {
			return nil, err
		}
		if last = b; len(b) > 0 && bytes.Equal(a, b) {
			return b, nil
		}
	}
	if len(last) == 0 {
		return nil, ErrEmpty
	}
	return nil, ErrUnsettled
}

// slurp is one read of the whole file, held to maxFile.
func (r *Reader) slurp() ([]byte, error) {
	f, err := r.fsys.Open(r.name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, ErrTooLarge
	}
	return b, nil
}
