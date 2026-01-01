package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Root: filesystem abstraction for Nuke phases. Translates absolute
// node-perspective paths to its configured base ("/" prod, t.TempDir() in tests).
type Root interface {
	// RemoveAllPath removes a path and everything under it, returning the
	// total bytes and file count removed (or, in dry-run mode, what would
	// have been removed).
	RemoveAllPath(absPath string) (bytes int64, files int, err error)

	// RemoveGlobPath expands a glob rooted at the node-absolute path and
	// removes each match.
	RemoveGlobPath(absGlob string) (bytes int64, files int, err error)

	// ShredPath overwrites and unlinks a file using `shred -u`. Falls back
	// to RemoveAllPath if `shred` is not installed.
	ShredPath(absPath string) (bytes int64, files int, err error)

	// ShredGlobPath expands a glob rooted at the node-absolute path and shreds
	// each match (like ShredPath) rather than plain-unlinking.
	ShredGlobPath(absGlob string) (bytes int64, files int, err error)

	// RunCmd executes a shell command (used by phases that invoke
	// systemctl / iptables / journalctl / apt / shred on directories /
	// swapoff / sysctl / etc.). In dry-run mode this is a no-op that
	// returns ("", nil).
	RunCmd(name string, args ...string) (combinedOutput string, err error)

	// DryRun returns whether this Root is in dry-run mode.
	DryRun() bool

	// WithDryRun returns a copy of the Root with dry-run mode toggled.
	WithDryRun(on bool) Root
}

// NewRootAt returns a Root that maps all paths under base. Pass "/" for
// production, t.TempDir() for tests.
func NewRootAt(base string) Root {
	return &realRoot{base: base}
}

type realRoot struct {
	base   string
	dryRun bool
}

func (r *realRoot) WithDryRun(on bool) Root {
	clone := *r
	clone.dryRun = on
	return &clone
}

func (r *realRoot) DryRun() bool { return r.dryRun }

func (r *realRoot) resolve(abs string) string {
	if r.base == "/" || r.base == "" {
		return abs
	}
	return filepath.Join(r.base, abs)
}

func (r *realRoot) RemoveAllPath(absPath string) (int64, int, error) {
	target := r.resolve(absPath)
	bytes, files, err := measureTree(target)
	if err != nil || files == 0 {
		return bytes, files, err
	}
	if r.dryRun {
		return bytes, files, nil
	}
	return bytes, files, os.RemoveAll(target)
}

func (r *realRoot) RemoveGlobPath(absGlob string) (int64, int, error) {
	pattern := r.resolve(absGlob)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, 0, err
	}
	var totalBytes int64
	totalFiles := 0
	var errs []error
	for _, m := range matches {
		b, f, mErr := measureTree(m)
		if mErr != nil {
			errs = append(errs, mErr)
			continue
		}
		if f == 0 {
			continue
		}
		if !r.dryRun {
			if rmErr := os.RemoveAll(m); rmErr != nil {
				errs = append(errs, rmErr)
				continue // do NOT increment counts for this match
			}
		}
		totalBytes += b
		totalFiles += f
	}
	return totalBytes, totalFiles, errors.Join(errs...)
}

func (r *realRoot) ShredPath(absPath string) (int64, int, error) {
	target := r.resolve(absPath)
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	bytes := info.Size()
	if r.dryRun {
		return bytes, 1, nil
	}
	if _, err := exec.LookPath("shred"); err == nil {
		if err := exec.Command("shred", "-fu", "-n", "1", target).Run(); err == nil {
			return bytes, 1, nil
		}
	}
	return bytes, 1, os.Remove(target)
}

func (r *realRoot) ShredGlobPath(absGlob string) (int64, int, error) {
	pattern := r.resolve(absGlob)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, 0, err
	}
	var totalBytes int64
	totalFiles := 0
	var errs []error
	for _, m := range matches {
		// Convert the resolved (base-prefixed) path back to a node-absolute
		// path so that ShredPath can resolve it again correctly.
		absFromNode := m
		if r.base != "/" && r.base != "" {
			if strings.HasPrefix(m, r.base) {
				absFromNode = strings.TrimPrefix(m, r.base)
				if absFromNode == "" {
					absFromNode = "/"
				}
			}
		}
		b, f, shredErr := r.ShredPath(absFromNode)
		if shredErr != nil {
			errs = append(errs, shredErr)
			continue
		}
		totalBytes += b
		totalFiles += f
	}
	return totalBytes, totalFiles, errors.Join(errs...)
}

func (r *realRoot) RunCmd(name string, args ...string) (string, error) {
	if r.dryRun {
		return "[dry-run] " + name + " " + strings.Join(args, " "), nil
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// measureTree walks target and returns total bytes + file count.
// Missing paths are not an error — they return (0, 0, nil).
func measureTree(target string) (int64, int, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if !info.IsDir() {
		return info.Size(), 1, nil
	}
	var bytes int64
	files := 0
	_ = filepath.Walk(target, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil || fi == nil {
			return nil
		}
		if !fi.IsDir() {
			bytes += fi.Size()
			files++
		}
		return nil
	})
	return bytes, files, nil
}
