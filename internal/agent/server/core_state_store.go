package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
)

// coreStateStore persists the last hub-pushed CoreStatePayload per core_inbound
// to disk so a remote agent can re-Apply its tunnel cores after a
// restart/reboot. The agent has no database, and each core keeps its listener or
// daemon only in memory — so without this durable record a reboot leaves every
// core dead until the hub happens to re-push. This mirrors how Xray recovers
// from its on-disk config file. The built-in node has none: the panel's push
// paths reconcile it on start.
type coreStateStore struct {
	dir string // <coresDataDir>/_state — sibling of the per-type inbound dirs
}

func newCoreStateStore(root string) *coreStateStore {
	return &coreStateStore{dir: filepath.Join(root, "_state")}
}

// safeStateID rejects inbound IDs that could escape the state dir. Hub inbound
// IDs are decimal strings, so anything with a path separator or a dot-segment is
// unexpected and refused rather than sanitized.
func safeStateID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("core state: unsafe inbound id %q", id)
	}
	return nil
}

func (s *coreStateStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// Save records the desired state for one inbound (protojson, mode 0600). The
// payload carries user secrets/PKI — the same at-rest exposure class as the
// rendered configs already under coresDataDir — so the dir is forced to 0700.
func (s *coreStateStore) Save(p *pb.CoreStatePayload) error {
	id := p.GetInboundId()
	if err := safeStateID(id); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("core state: mkdir: %w", err)
	}
	// MkdirAll honors umask; force 0700 so payload secrets aren't group/other readable.
	_ = os.Chmod(s.dir, 0o700)
	data, err := protojson.Marshal(p)
	if err != nil {
		return fmt.Errorf("core state: marshal id=%s: %w", id, err)
	}
	if err := os.WriteFile(s.path(id), data, 0o600); err != nil {
		return fmt.Errorf("core state: write id=%s: %w", id, err)
	}
	return nil
}

// Remove deletes the persisted state for one inbound. An absent file is not an
// error (idempotent) so a teardown push for an inbound we never persisted is fine.
func (s *coreStateStore) Remove(id string) error {
	if err := safeStateID(id); err != nil {
		return err
	}
	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("core state: remove id=%s: %w", id, err)
	}
	return nil
}

// LoadAll reads every persisted payload. Corrupt/unreadable files are skipped
// (logged) so one bad file can't block recovery of the rest. A missing dir
// (nothing ever persisted) returns an empty slice, not an error.
func (s *coreStateStore) LoadAll() ([]*pb.CoreStatePayload, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("core state: readdir: %w", err)
	}
	var out []*pb.CoreStatePayload
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			logrus.WithError(err).WithField("file", full).Warn("core state: skip unreadable file")
			continue
		}
		p := &pb.CoreStatePayload{}
		if err := protojson.Unmarshal(data, p); err != nil {
			logrus.WithError(err).WithField("file", full).Warn("core state: skip corrupt file")
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
