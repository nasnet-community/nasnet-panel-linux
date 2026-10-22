package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// MaxUpload caps an uploaded bundle.
	MaxUpload = 300 << 20
	uploadTTL = 24 * time.Hour
)

// Upload is a bundle waiting to be installed, as its signed bundle.json
// describes it.
type Upload struct {
	ID         string    `json:"upload_id"`
	Version    string    `json:"version"`
	Prerelease bool      `json:"prerelease"`
	Arch       string    `json:"arch"`
	MinFrom    string    `json:"min_from,omitempty"`
	Notes      string    `json:"notes"`
	Xray       string    `json:"xray"`
	Size       int64     `json:"size"`
	BuiltAt    time.Time `json:"built_at"`
	At         time.Time `json:"uploaded_at"`
}

var uploadIDRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (u *Usecase) uploadPath(id string) string {
	return filepath.Join(u.deps.Paths.Uploads(), id+".tar.gz")
}

// SaveUpload stores a bundle and reads its signed bundle.json. Every file
// is checked again when the update runs.
func (u *Usecase) SaveUpload(r io.Reader) (*Upload, error) {
	if err := os.MkdirAll(u.deps.Paths.Uploads(), 0o700); err != nil {
		return nil, err
	}
	id := newJobID()
	path := u.uploadPath(id)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(out, io.LimitReader(r, MaxUpload+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > MaxUpload {
		err = fmt.Errorf("the file is over %s", mb(MaxUpload))
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	b, err := peekBundle(path)
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	up := &Upload{ID: id, Version: b.Version, Prerelease: b.Prerelease, Arch: b.Arch, MinFrom: b.MinFrom, Notes: b.Notes,
		Xray: b.Xray, Size: n, BuiltAt: b.BuiltAt, At: u.now().UTC()}
	if err := u.checkUpload(up); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	data, _ := json.Marshal(up)
	if err := writeFileAtomic(strings.TrimSuffix(path, ".tar.gz")+".json", data, 0o600); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return up, nil
}

// checkUpload refuses a bundle this panel can't install, before anyone
// picks a time for it.
func (u *Usecase) checkUpload(up *Upload) error {
	return u.checkRelease(up.Version, up.MinFrom, up.Arch)
}

func (u *Usecase) upload(id string) (*Upload, error) {
	if !uploadIDRE.MatchString(id) {
		return nil, errors.New("no such upload")
	}
	data, err := os.ReadFile(filepath.Join(u.deps.Paths.Uploads(), id+".json"))
	if err != nil {
		return nil, errors.New("that upload is gone; upload the file again")
	}
	var up Upload
	if err := json.Unmarshal(data, &up); err != nil {
		return nil, err
	}
	if _, err := os.Stat(u.uploadPath(id)); err != nil {
		return nil, errors.New("that upload is gone; upload the file again")
	}
	return &up, nil
}

func (u *Usecase) dropUpload(id string) {
	if !uploadIDRE.MatchString(id) {
		return
	}
	_ = os.Remove(u.uploadPath(id))
	_ = os.Remove(filepath.Join(u.deps.Paths.Uploads(), id+".json"))
}

// pruneUploads drops uploads nobody installed within a day.
func (u *Usecase) pruneUploads(keep string) {
	entries, err := os.ReadDir(u.deps.Paths.Uploads())
	if err != nil {
		return
	}
	for _, e := range entries {
		id, _, _ := strings.Cut(e.Name(), ".")
		info, err := e.Info()
		if err != nil || id == keep || u.now().Sub(info.ModTime()) < uploadTTL {
			continue
		}
		_ = os.Remove(filepath.Join(u.deps.Paths.Uploads(), e.Name()))
	}
}
