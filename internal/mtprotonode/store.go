package mtprotonode

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nasnet-community/mtproto-remote-plugin/hubnode"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
)

var ErrNotFound = errors.New("MTProto node not found")

// Store persists lite-node rows.
type Store interface {
	Get(ctx context.Context, nodeID uint) (*LiteNode, error)
	// Insert adds a new node's row and never overwrites one.
	Insert(ctx context.Context, n *LiteNode) error
	// UpdateInstall rewrites only what an install, repair or key rotation sets.
	UpdateInstall(ctx context.Context, n *LiteNode) error
	Delete(ctx context.Context, nodeID uint) error
	// background writers only update: a report racing a delete must not bring the node back
	UpdateObserved(ctx context.Context, nodeID uint, agentVersion, telemtVersion string, seen time.Time) error
	UpdateDisguise(ctx context.Context, nodeID uint, domain string) error
	UpdateCreds(ctx context.Context, nodeID uint, pub, shortID, token, nextSeed string) error
	// UpdateSettings writes the fields s names; ErrNotFound when there is no row.
	UpdateSettings(ctx context.Context, nodeID uint, s Settings) error
	List(ctx context.Context) ([]*LiteNode, error)
	Baseline(ctx context.Context, nodeID uint) (map[string]hubnode.Usage, error)
	SaveBaseline(ctx context.Context, nodeID uint, cum map[string]hubnode.Usage) error
}

type gormStore struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

func (s *gormStore) Get(ctx context.Context, id uint) (*LiteNode, error) {
	var n LiteNode
	err := s.db.WithContext(ctx).First(&n, "node_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &n, err
}

func (s *gormStore) Insert(ctx context.Context, n *LiteNode) error {
	return s.db.WithContext(ctx).Create(n).Error
}

func (s *gormStore) UpdateInstall(ctx context.Context, n *LiteNode) error {
	res := s.db.WithContext(ctx).Model(&LiteNode{}).Where("node_id = ?", n.NodeID).Updates(map[string]any{
		"agent_id": n.AgentID, "hub_port": n.HubPort, "capture_port": n.CapturePort, "ssh_port": n.SSHPort,
		"ssh_user": n.SSHUser, "host_key": n.HostKey, "reality_pub": n.RealityPub, "short_id": n.ShortID,
		"token": n.Token, "next_seed": n.NextSeed, "keys_rotated_at": n.KeysRotatedAt, "arch": n.Arch,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *gormStore) UpdateObserved(ctx context.Context, id uint, agentVer, telemtVer string, seen time.Time) error {
	return s.db.WithContext(ctx).Model(&LiteNode{}).Where("node_id = ?", id).
		Updates(map[string]any{"agent_version": agentVer, "telemt_version": telemtVer, "last_seen_at": seen}).Error
}

func (s *gormStore) UpdateDisguise(ctx context.Context, id uint, domain string) error {
	return s.db.WithContext(ctx).Model(&LiteNode{}).Where("node_id = ?", id).Update("disguise", domain).Error
}

func (s *gormStore) UpdateCreds(ctx context.Context, id uint, pub, shortID, token, nextSeed string) error {
	return s.db.WithContext(ctx).Model(&LiteNode{}).Where("node_id = ?", id).
		Updates(map[string]any{"reality_pub": pub, "short_id": shortID, "token": token, "next_seed": nextSeed}).Error
}

func (s *gormStore) UpdateSettings(ctx context.Context, id uint, set Settings) error {
	cols := map[string]any{}
	if set.MaxStreams != nil {
		cols["max_streams"] = *set.MaxStreams
	}
	if set.SSHPort != nil {
		cols["ssh_port"] = *set.SSHPort
	}
	if set.SSHUser != nil {
		cols["ssh_user"] = *set.SSHUser
	}
	if set.ProxyStopped != nil {
		cols["proxy_stopped"] = *set.ProxyStopped
	}
	if len(cols) == 0 {
		_, err := s.Get(ctx, id)
		return err
	}
	// updated_at changes too, so a matched row always counts as affected
	res := s.db.WithContext(ctx).Model(&LiteNode{}).Where("node_id = ?", id).Updates(cols)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *gormStore) Delete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&UsageBaseline{}, "node_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&LiteNode{}, "node_id = ?", id).Error
	})
}

func (s *gormStore) List(ctx context.Context) ([]*LiteNode, error) {
	var out []*LiteNode
	return out, s.db.WithContext(ctx).Order("node_id").Find(&out).Error
}

func (s *gormStore) Baseline(ctx context.Context, id uint) (map[string]hubnode.Usage, error) {
	var rows []UsageBaseline
	if err := s.db.WithContext(ctx).Find(&rows, "node_id = ?", id).Error; err != nil {
		return nil, err
	}
	out := make(map[string]hubnode.Usage, len(rows))
	for _, r := range rows {
		out[r.Name] = hubnode.Usage{Up: r.Up, Down: r.Down}
	}
	return out, nil
}

func (s *gormStore) SaveBaseline(ctx context.Context, id uint, cum map[string]hubnode.Usage) error {
	rows := make([]UsageBaseline, 0, len(cum))
	for name, u := range cum {
		// one name the column rejects would roll back the whole booking, every poll
		if storableName(name) {
			rows = append(rows, UsageBaseline{NodeID: id, Name: name, Up: u.Up, Down: u.Down})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	// inside the booking's transaction when there is one, so both land or neither
	db := database.GetExecutor(s.db, ctx)
	// a late report for a node that has just been deleted must not leave orphans
	var n int64
	if err := db.Model(&LiteNode{}).Where("node_id = ?", id).Count(&n).Error; err != nil || n == 0 {
		return err
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"up", "down"}),
	}).CreateInBatches(rows, 200).Error
}

// maxNameLen is UsageBaseline.Name's column size, in bytes so it fits any database.
const maxNameLen = 190

func storableName(name string) bool {
	return name != "" && len(name) <= maxNameLen && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}

// RecordStore saves jobs' stale records without touching what the manager owns;
// a repair's cover site comes in through the manager's AdoptCover instead.
type RecordStore struct {
	s          Store
	mu         sync.Mutex
	installing map[uint]bool
	unlist     func(ctx context.Context, nodeID uint) error
}

// BeforeDelete runs ahead of a record's delete: a failure keeps the record, so the removal can be retried.
func (r *RecordStore) BeforeDelete(f func(ctx context.Context, nodeID uint) error) { r.unlist = f }

func NewRecordStore(s Store) *RecordStore { return &RecordStore{s: s, installing: map[uint]bool{}} }

// Installing lets an install's first save add the node's row.
func (r *RecordStore) Installing(nodeID uint) {
	r.mu.Lock()
	r.installing[nodeID] = true
	r.mu.Unlock()
}

func (r *RecordStore) Get(ctx context.Context, id uint) (*relayops.Record, error) {
	l, err := r.s.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, relayops.ErrNotFound
		}
		return nil, err
	}
	rec := l.record()
	return &rec, nil
}

func (r *RecordStore) Save(ctx context.Context, rec *relayops.Record) error {
	var l LiteNode
	l.apply(*rec)
	err := r.s.UpdateInstall(ctx, &l)
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	r.mu.Lock()
	fresh := r.installing[rec.NodeID]
	delete(r.installing, rec.NodeID)
	r.mu.Unlock()
	if !fresh {
		return relayops.ErrNotFound // removed while the job ran
	}
	return r.s.Insert(ctx, &l)
}

func (r *RecordStore) Delete(ctx context.Context, id uint) error {
	if r.unlist != nil {
		if err := r.unlist(ctx, id); err != nil {
			return err
		}
	}
	r.mu.Lock()
	delete(r.installing, id)
	r.mu.Unlock()
	return r.s.Delete(ctx, id)
}
