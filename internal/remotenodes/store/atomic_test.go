// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

var errDisk = errors.New("the disk is on fire")

// fakeFile is a real temp file with calls that can be made to fail.
type fakeFile struct {
	tempFile
	chmod func(os.FileMode) error
	write func(p []byte) (int, error)
	sync  func() error
}

func (f *fakeFile) Chmod(m os.FileMode) error {
	if f.chmod != nil {
		return f.chmod(m)
	}
	return f.tempFile.Chmod(m)
}

func (f *fakeFile) Write(p []byte) (int, error) {
	if f.write != nil {
		return f.write(p)
	}
	return f.tempFile.Write(p)
}

func (f *fakeFile) Sync() error {
	if f.sync != nil {
		return f.sync()
	}
	return f.tempFile.Sync()
}

// breakTemp makes the store's saves go through a temp file that mod breaks.
func breakTemp(s *Store, mod func(*fakeFile)) {
	real := s.ops.createTemp
	s.ops.createTemp = func(dir, pattern string) (tempFile, error) {
		f, err := real(dir, pattern)
		if err != nil {
			return nil, err
		}
		ff := &fakeFile{tempFile: f}
		mod(ff)
		return ff, nil
	}
}

// survives checks that a failed save left the old file, memory and counter as
// they were, and no temp file behind.
func survives(t *testing.T, s *Store, run func() error) {
	t.Helper()
	dir := filepath.Dir(s.path)
	file, state, writes := readFile(t, s.path), s.Read(), s.Writes()

	err := run()
	if !errors.Is(err, errDisk) {
		t.Fatalf("err = %v, want the disk's error", err)
	}
	if string(readFile(t, s.path)) != string(file) {
		t.Fatal("the old file was changed")
	}
	if !same(t, s.Read(), state) {
		t.Fatal("the state in memory moved although the write failed")
	}
	if s.Writes() != writes {
		t.Fatalf("writes went from %d to %d for a failed write", writes, s.Writes())
	}
	if got := names(t, dir); len(got) != 1 || got[0] != FileName {
		t.Fatalf("the folder holds %v after a failed write", got)
	}
}

func TestAFailedSaveLeavesTheOldFile(t *testing.T) {
	rename := func(s *Store) { s.ops.rename = func(string, string) error { return errDisk } }
	cases := map[string]func(*Store){
		"rename": rename,
		"create": func(s *Store) {
			s.ops.createTemp = func(string, string) (tempFile, error) { return nil, errDisk }
		},
		"chmod": func(s *Store) {
			breakTemp(s, func(f *fakeFile) { f.chmod = func(os.FileMode) error { return errDisk } })
		},
		"sync": func(s *Store) { breakTemp(s, func(f *fakeFile) { f.sync = func() error { return errDisk } }) },
		"write": func(s *Store) {
			breakTemp(s, func(f *fakeFile) { f.write = func([]byte) (int, error) { return 0, errDisk } })
		},
		"half a write": func(s *Store) {
			// the disk fills up part-way through
			breakTemp(s, func(f *fakeFile) {
				f.write = func(p []byte) (int, error) {
					n, _ := f.tempFile.Write(p[:len(p)/2])
					return n, errDisk
				}
			})
		},
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			s := open(t, t.TempDir())
			must(t, s.Update(addNode("203.0.113.1")))
			breakIt(s)
			survives(t, s, func() error {
				return s.Update(func(st *model.State) error { st.Nodes[0].Name = "never saved"; return nil })
			})

			// and the store works again once the disk does
			s.ops = osOps()
			must(t, s.Update(func(st *model.State) error { st.Nodes[0].Name = "saved"; return nil }))
			if got := open(t, filepath.Dir(s.path)).Read().Nodes[0].Name; got != "saved" {
				t.Fatalf("name on disk = %q", got)
			}
		})
	}
}

func TestAFailedFirstSaveLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	s.ops.rename = func(string, string) error { return errDisk }
	if err := s.Update(addNode("203.0.113.1")); !errors.Is(err, errDisk) {
		t.Fatalf("err = %v", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("the folder holds %v after a failed first write", got)
	}
	if len(s.Read().Nodes) != 0 || s.Writes() != 0 {
		t.Fatal("a failed first write moved the store")
	}
}

func TestAFailedFolderSyncDoesNotFailTheUpdate(t *testing.T) {
	// by then the rename has happened: the new file is the file
	dir := t.TempDir()
	s := open(t, dir)
	s.ops.syncDir = func(string) error { return errDisk }
	must(t, s.Update(addNode("203.0.113.1")))
	if s.Writes() != 1 || len(s.Read().Nodes) != 1 {
		t.Fatalf("writes = %d, nodes = %d", s.Writes(), len(s.Read().Nodes))
	}
	if got := open(t, dir).Read().Nodes; len(got) != 1 {
		t.Fatalf("the file holds %d nodes", len(got))
	}
}

func TestTheFolderIsSyncedAfterTheRename(t *testing.T) {
	var calls []string
	s := open(t, t.TempDir())
	s.ops.rename = func(a, b string) error { calls = append(calls, "rename"); return os.Rename(a, b) }
	s.ops.syncDir = func(string) error { calls = append(calls, "syncDir"); return nil }
	breakTemp(s, func(f *fakeFile) {
		f.sync = func() error { calls = append(calls, "sync"); return f.tempFile.Sync() }
	})
	must(t, s.Update(addNode("203.0.113.1")))
	if got := strings.Join(calls, ","); got != "sync,rename,syncDir" {
		t.Fatalf("order of calls: %s, want the file synced, then renamed, then the folder synced", got)
	}
}

func TestARealUnwritableFolderLeavesTheOldFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	dir := t.TempDir()
	s := open(t, dir)
	must(t, s.Update(addNode("203.0.113.1")))
	file := readFile(t, s.path)

	must(t, os.Chmod(dir, 0o500))
	defer os.Chmod(dir, 0o700)
	err := s.Update(func(st *model.State) error { st.Nodes[0].Name = "never saved"; return nil })
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v, want a permission error", err)
	}
	must(t, os.Chmod(dir, 0o700))
	if string(readFile(t, s.path)) != string(file) {
		t.Fatal("the old file was changed")
	}
	if got := s.Read().Nodes[0].Name; got == "never saved" {
		t.Fatal("memory kept a change that was never saved")
	}
}
