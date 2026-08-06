package repository

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"gorm.io/gorm"
)

// GroupRepository persists WAN policy groups and their members
type GroupRepository interface {
	List(ctx context.Context) ([]domain.WANGroup, error)
	GetByName(ctx context.Context, name string) (*domain.WANGroup, error)
	Members(ctx context.Context, groupID uint) ([]domain.WANGroupMember, error)
	EnsureDefaults(ctx context.Context) error
	SetMember(ctx context.Context, groupID, interfaceID uint, priority int, uplinkIndex uint32) error
}

type groupRepository struct {
	db *gorm.DB
}

func NewGroupRepository(db *gorm.DB) GroupRepository {
	return &groupRepository{db: db}
}

func (r *groupRepository) List(ctx context.Context) ([]domain.WANGroup, error) {
	var out []domain.WANGroup
	err := r.db.WithContext(ctx).Order("group_index ASC").Find(&out).Error
	return out, err
}

func (r *groupRepository) GetByName(ctx context.Context, name string) (*domain.WANGroup, error) {
	var g domain.WANGroup
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *groupRepository) Members(ctx context.Context, groupID uint) ([]domain.WANGroupMember, error) {
	var out []domain.WANGroupMember
	err := r.db.WithContext(ctx).Where("group_id = ?", groupID).
		Order("priority ASC").Find(&out).Error
	return out, err
}

// EnsureDefaults creates the two stage-1 groups if absent. Runs at every boot,
// so it has to be idempotent.
func (r *groupRepository) EnsureDefaults(ctx context.Context) error {
	defaults := []domain.WANGroup{
		{NodeID: 1, Name: "domestic", GroupIndex: netmark.GroupDomestic,
			RuleBase: 110, RuleBlackhole: 149, Policy: domain.PolicyFailover},
		{NodeID: 1, Name: "foreign", GroupIndex: netmark.GroupForeign,
			RuleBase: 150, RuleBlackhole: 199, Policy: domain.PolicyFailover},
	}
	for i := range defaults {
		g := defaults[i]
		err := r.db.WithContext(ctx).
			Where("node_id = ? AND name = ?", g.NodeID, g.Name).
			FirstOrCreate(&g).Error
		if err != nil {
			return fmt.Errorf("ensure group %q: %w", g.Name, err)
		}
	}
	return nil
}

// SetMember binds one interface to a group, replacing any prior binding for
// that interface in that group.
func (r *groupRepository) SetMember(ctx context.Context, groupID, interfaceID uint,
	priority int, uplinkIndex uint32) error {
	m := domain.WANGroupMember{
		GroupID: groupID, InterfaceID: interfaceID,
		Priority: priority, UplinkIndex: uplinkIndex,
	}
	var existing domain.WANGroupMember
	err := r.db.WithContext(ctx).
		Where("group_id = ? AND interface_id = ?", groupID, interfaceID).
		First(&existing).Error
	if err == nil {
		return r.db.WithContext(ctx).Model(&existing).
			Updates(map[string]any{"priority": priority, "uplink_index": uplinkIndex}).Error
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
