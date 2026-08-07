package repository

import (
	"context"
	"errors"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// InterfaceRepository persists NIC roles and observed facts.
type InterfaceRepository interface {
	List(ctx context.Context) ([]domain.NetworkInterface, error)
	GetByKey(ctx context.Context, key string) (*domain.NetworkInterface, error)
	GetByRole(ctx context.Context, role domain.InterfaceRole) ([]domain.NetworkInterface, error)
	GetBySlot(ctx context.Context, slot domain.UplinkSlot) (*domain.NetworkInterface, error)
	Upsert(ctx context.Context, in *domain.NetworkInterface) error
	MarkAbsent(ctx context.Context, presentKeys []string) error
	SetRoleTx(ctx context.Context, tx *gorm.DB, id uint, role domain.InterfaceRole, slot domain.UplinkSlot) error
	SetHealth(ctx context.Context, id uint, healthy bool) error
	SetLearnedGateway(ctx context.Context, id uint, gateway string) error
	DB() *gorm.DB
}

type interfaceRepository struct {
	db *gorm.DB
}

func NewInterfaceRepository(db *gorm.DB) InterfaceRepository {
	return &interfaceRepository{db: db}
}

func (r *interfaceRepository) List(ctx context.Context) ([]domain.NetworkInterface, error) {
	var out []domain.NetworkInterface
	err := r.db.WithContext(ctx).Order("if_name ASC").Find(&out).Error
	return out, err
}

func (r *interfaceRepository) GetByKey(ctx context.Context, key string) (*domain.NetworkInterface, error) {
	var in domain.NetworkInterface
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&in).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

func (r *interfaceRepository) GetByRole(ctx context.Context, role domain.InterfaceRole) ([]domain.NetworkInterface, error) {
	var out []domain.NetworkInterface
	err := r.db.WithContext(ctx).Where("role = ?", role).Order("if_name ASC").Find(&out).Error
	return out, err
}

func (r *interfaceRepository) GetBySlot(ctx context.Context, slot domain.UplinkSlot) (*domain.NetworkInterface, error) {
	var in domain.NetworkInterface
	if err := r.db.WithContext(ctx).Where("slot = ?", slot).First(&in).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

// Upsert writes by Key, not by ID: enumeration rediscovers a device every boot
// and must land on the existing row so its role survives.
func (r *interfaceRepository) Upsert(ctx context.Context, in *domain.NetworkInterface) error {
	var existing domain.NetworkInterface
	err := r.db.WithContext(ctx).Where("key = ? AND node_id = ?", in.Key, nodeID(in.NodeID)).
		First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if in.NodeID == 0 {
			in.NodeID = 1
		}
		return r.db.WithContext(ctx).Create(in).Error
	case err != nil:
		return err
	}

	// Refresh observed facts only. Role, Slot, Label, Method and the static
	// address are operator intent and must never be overwritten by a rescan.
	now := time.Now()
	updates := map[string]any{
		"if_name":           in.IfName,
		"key_kind":          in.KeyKind,
		"perm_mac":          in.PermMAC,
		"id_path":           in.IDPath,
		"source":            in.Source,
		"source_confidence": in.SourceConfidence,
		"present":           true,
		"last_seen_at":      &now,
	}
	if err := r.db.WithContext(ctx).Model(&existing).Updates(updates).Error; err != nil {
		return err
	}
	in.ID = existing.ID
	in.Role, in.Slot, in.Label = existing.Role, existing.Slot, existing.Label
	return nil
}

// MarkAbsent flips every row whose key is not in presentKeys to Present=false
// and stamps LastSeenAt. Rows are never deleted: a dongle keeps its role.
func (r *interfaceRepository) MarkAbsent(ctx context.Context, presentKeys []string) error {
	now := time.Now()
	q := r.db.WithContext(ctx).Model(&domain.NetworkInterface{}).Where("present = ?", true)
	if len(presentKeys) > 0 {
		q = q.Where("key NOT IN ?", presentKeys)
	}
	return q.Updates(map[string]any{"present": false, "last_seen_at": &now}).Error
}

// SetRoleTx assigns a role inside the caller's transaction. A role change
// regenerates .network files, nft rules and xray bindings together; a partial
// application must roll back.
func (r *interfaceRepository) SetRoleTx(ctx context.Context, tx *gorm.DB, id uint,
	role domain.InterfaceRole, slot domain.UplinkSlot) error {
	if tx == nil {
		tx = r.db
	}
	return tx.WithContext(ctx).Model(&domain.NetworkInterface{}).Where("id = ?", id).
		Updates(map[string]any{"role": role, "slot": slot}).Error
}

func (r *interfaceRepository) SetHealth(ctx context.Context, id uint, healthy bool) error {
	return r.db.WithContext(ctx).Model(&domain.NetworkInterface{}).Where("id = ?", id).
		Update("healthy", healthy).Error
}

func (r *interfaceRepository) SetLearnedGateway(ctx context.Context, id uint, gateway string) error {
	return r.db.WithContext(ctx).Model(&domain.NetworkInterface{}).Where("id = ?", id).
		Update("learned_gateway", gateway).Error
}

// DB exposes the handle so the usecase can open one transaction spanning a role
// change and its eviction.
func (r *interfaceRepository) DB() *gorm.DB { return r.db }

func nodeID(v uint) uint {
	if v == 0 {
		return 1
	}
	return v
}
