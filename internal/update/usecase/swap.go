package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// SystemFiles are the files outside the install root that an update's
// nasnet-tool.sh rewrites. The runner saves them first.
var SystemFiles = []string{
	"/etc/systemd/system/nasnet-panel.service",
	"/etc/systemd/system/nasnet-netrollback.service",
	"/etc/systemd/system/nasnet-netrollback.timer",
	"/usr/local/bin/nasnet",
}

// fileSet is a group of live files and the copies saved before they
// changed: what a revert or a rollback puts back.
type fileSet struct {
	Version    string      `json:"version"`     // the version these copies belong to
	ReplacedBy string      `json:"replaced_by"` // the version that replaced them
	At         time.Time   `json:"at"`
	Files      []savedFile `json:"files"`
}

type savedFile struct {
	Live    string `json:"live"`
	Saved   string `json:"saved,omitempty"`
	Existed bool   `json:"existed"` // false: the update added it, putting back removes it
}

// save copies each live file into dir before anything touches it.
func (s *fileSet) save(dir string, lives ...string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, live := range lives {
		f := savedFile{Live: live}
		if _, err := os.Stat(live); err == nil {
			f.Existed = true
			f.Saved = filepath.Join(dir, fmt.Sprintf("%02d-%s", len(s.Files), filepath.Base(live)))
			if err := copyFile(live, f.Saved); err != nil {
				return fmt.Errorf("update: save %s: %w", live, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		s.Files = append(s.Files, f)
	}
	return nil
}

// saveAt saves one live file to a path of its own.
func (s *fileSet) saveAt(live, saved string) error {
	f := savedFile{Live: live}
	if _, err := os.Stat(live); err == nil {
		f.Existed, f.Saved = true, saved
		if err := copyFile(live, saved); err != nil {
			return fmt.Errorf("update: save %s: %w", live, err)
		}
	}
	s.Files = append(s.Files, f)
	return nil
}

// putBack restores every saved file, last first, and removes the ones that
// didn't exist. It carries on past errors so as much as possible is back.
func (s *fileSet) putBack() error {
	var errs []error
	for i := len(s.Files) - 1; i >= 0; i-- {
		f := s.Files[i]
		var err error
		if f.Existed {
			err = installFile(f.Saved, f.Live)
		} else if rerr := os.Remove(f.Live); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			err = rerr
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Live, err))
		}
	}
	return errors.Join(errs...)
}

func (s *fileSet) write(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o600)
}

func readFileSet(path string) (*fileSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s fileSet
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// installFile puts src at live in one rename, so nothing ever runs half a
// file: the root netrollback timer runs bin/nasnet-panel every 10 seconds.
func installFile(src, live string) error {
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		return err
	}
	tmp := live + ".new"
	if err := copyFile(src, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, live)
}

// copyFile copies with src's mode, through a temp file and a rename.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
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
	return os.Rename(tmp.Name(), dst)
}
