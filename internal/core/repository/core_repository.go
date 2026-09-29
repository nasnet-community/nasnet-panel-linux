// Package repository provides persistence for the tunnel core subsystem:
// core_inbounds and core_users.
package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CoreRepository persists core_inbounds and core_users.
type CoreRepository interface {
	CreateInbound(ctx context.Context, in *domain.CoreInbound) error
	UpdateInbound(ctx context.Context, in *domain.CoreInbound) error
	FindInboundByID(ctx context.Context, id uint) (*domain.CoreInbound, error)
	DeleteInbound(ctx context.Context, id uint) error
	ListInboundsByNode(ctx context.Context, nodeID uint) ([]*domain.CoreInbound, error)
	// ListAllInbounds returns every core_inbound across all nodes (admin
	// cross-node overview). Preloads Users like ListInboundsByNode; Secret stays
	// json:"-", so no credential leaks through the list response.
	ListAllInbounds(ctx context.Context) ([]*domain.CoreInbound, error)

	CreateUser(ctx context.Context, u *domain.CoreUser) error
	UpdateUser(ctx context.Context, u *domain.CoreUser) error
	DeleteUser(ctx context.Context, id uint) error
	FindUserByID(ctx context.Context, id uint) (*domain.CoreUser, error)
	ListUsersByInbound(ctx context.Context, inboundID uint) ([]*domain.CoreUser, error)
	// FindUsersBySubscription returns the core_users provisioned for a
	// subscription (one per assigned core_inbound).
	FindUsersBySubscription(ctx context.Context, subID uint) ([]*domain.CoreUser, error)
	// FindUsersByEmail returns the core_users sharing an email identity. The
	// subscription's ConfigEmail is the correlation key used to reuse a user
	// on assignment and to bind subscription_id.
	FindUsersByEmail(ctx context.Context, email string) ([]*domain.CoreUser, error)

	// CoreUserIDsForSubOnNode returns the IDs of the subscription's core_users
	// whose core_inbound lives on nodeID. Used by the hub's per-node traffic fold
	// to attribute pure-core bytes to the right tunnel(s).
	CoreUserIDsForSubOnNode(ctx context.Context, subID, nodeID uint) ([]uint, error)
	// CoreUserIDsForSubOnNodeByProtocol narrows CoreUserIDsForSubOnNode to a single
	// core protocol, so the hub can attribute tagged per-protocol core bytes to the
	// exact tunnel when a sub has several core protocols on one node.
	CoreUserIDsForSubOnNodeByProtocol(ctx context.Context, subID, nodeID uint, protocol string) ([]uint, error)
	// AddCoreUserData atomically increments a core_user's DataUsed counter.
	AddCoreUserData(ctx context.Context, coreUserID uint, bytes int64) error
	// ListCoreTrafficRefsByNode loads attribution IDs and protocols without per-user queries.
	ListCoreTrafficRefsByNode(ctx context.Context, nodeID uint) ([]contract.CoreTrafficRef, error)
	AddCoreUserDataBatch(ctx context.Context, deltas []contract.CoreUsageDelta) error
	// ResetDataBySubscription zeroes DataUsed for all of a subscription's
	// core_users (kept in step with sub.DataUsed resets).
	ResetDataBySubscription(ctx context.Context, subID uint) error

	// DisabledProtocolsForNode returns the set of core protocols explicitly
	// disabled on the node (node_core_settings rows with enabled=false). Absent
	// protocols are enabled by default, so an empty map means "all enabled".
	DisabledProtocolsForNode(ctx context.Context, nodeID uint) (map[domain.CoreProtocol]bool, error)
	// SetNodeProtocolEnabled upserts the node's per-protocol enable flag.
	SetNodeProtocolEnabled(ctx context.Context, nodeID uint, proto domain.CoreProtocol, enabled bool) error
}

type coreRepository struct{ db *gorm.DB }

// NewCoreRepository returns a CoreRepository backed by db.
func NewCoreRepository(db *gorm.DB) CoreRepository { return &coreRepository{db: db} }

func (r *coreRepository) CreateInbound(ctx context.Context, in *domain.CoreInbound) error {
	return database.GetExecutor(r.db, ctx).Create(in).Error
}

func (r *coreRepository) UpdateInbound(ctx context.Context, in *domain.CoreInbound) error {
	return database.GetExecutor(r.db, ctx).Save(in).Error
}

func (r *coreRepository) FindInboundByID(ctx context.Context, id uint) (*domain.CoreInbound, error) {
	var in domain.CoreInbound
	if err := database.GetExecutor(r.db, ctx).First(&in, id).Error; err != nil {
		return nil, err
	}
	return &in, nil
}

// DeleteInbound removes the inbound and its users together. The FK cascades
// too; deleting the users first also covers databases whose FK predates the
// cascade.
func (r *coreRepository) DeleteInbound(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("core_inbound_id = ?", id).Delete(&domain.CoreUser{}).Error; err != nil {
			return err
		}
		return tx.Delete(&domain.CoreInbound{}, id).Error
	})
}

func (r *coreRepository) ListInboundsByNode(ctx context.Context, nodeID uint) ([]*domain.CoreInbound, error) {
	var list []*domain.CoreInbound
	// Preload Users so the panel's card grid can show member counts/avatars and a
	// node-wide user total without an N+1 fetch. CoreUser.Secret has json:"-", so
	// the credential never leaks through the list response.
	err := database.GetExecutor(r.db, ctx).
		Preload("Users").
		Where("node_id = ?", nodeID).Order("id asc").Find(&list).Error
	return list, err
}

func (r *coreRepository) ListAllInbounds(ctx context.Context) ([]*domain.CoreInbound, error) {
	var list []*domain.CoreInbound
	err := database.GetExecutor(r.db, ctx).
		Preload("Users").
		Order("node_id asc, id asc").Find(&list).Error
	return list, err
}

func (r *coreRepository) CreateUser(ctx context.Context, u *domain.CoreUser) error {
	return database.GetExecutor(r.db, ctx).Create(u).Error
}

func (r *coreRepository) UpdateUser(ctx context.Context, u *domain.CoreUser) error {
	return database.GetExecutor(r.db, ctx).Save(u).Error
}

func (r *coreRepository) DeleteUser(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Delete(&domain.CoreUser{}, id).Error
}

func (r *coreRepository) FindUserByID(ctx context.Context, id uint) (*domain.CoreUser, error) {
	var u domain.CoreUser
	if err := database.GetExecutor(r.db, ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *coreRepository) ListUsersByInbound(ctx context.Context, inboundID uint) ([]*domain.CoreUser, error) {
	var list []*domain.CoreUser
	err := database.GetExecutor(r.db, ctx).
		Where("core_inbound_id = ?", inboundID).Order("id asc").Find(&list).Error
	return list, err
}

func (r *coreRepository) FindUsersBySubscription(ctx context.Context, subID uint) ([]*domain.CoreUser, error) {
	var list []*domain.CoreUser
	err := database.GetExecutor(r.db, ctx).
		Where("subscription_id = ?", subID).Order("id asc").Find(&list).Error
	return list, err
}

func (r *coreRepository) FindUsersByEmail(ctx context.Context, email string) ([]*domain.CoreUser, error) {
	var list []*domain.CoreUser
	err := database.GetExecutor(r.db, ctx).
		Where("email = ?", email).Order("id asc").Find(&list).Error
	return list, err
}

func (r *coreRepository) CoreUserIDsForSubOnNode(ctx context.Context, subID, nodeID uint) ([]uint, error) {
	var ids []uint
	err := database.GetExecutor(r.db, ctx).
		Model(&domain.CoreUser{}).
		Joins("JOIN core_inbounds ON core_inbounds.id = core_users.core_inbound_id").
		Where("core_users.subscription_id = ? AND core_inbounds.node_id = ?", subID, nodeID).
		Pluck("core_users.id", &ids).Error
	return ids, err
}

func (r *coreRepository) CoreUserIDsForSubOnNodeByProtocol(ctx context.Context, subID, nodeID uint, protocol string) ([]uint, error) {
	var ids []uint
	err := database.GetExecutor(r.db, ctx).
		Model(&domain.CoreUser{}).
		Joins("JOIN core_inbounds ON core_inbounds.id = core_users.core_inbound_id").
		Where("core_users.subscription_id = ? AND core_inbounds.node_id = ? AND core_inbounds.protocol = ?", subID, nodeID, protocol).
		Pluck("core_users.id", &ids).Error
	return ids, err
}

func (r *coreRepository) AddCoreUserData(ctx context.Context, coreUserID uint, bytes int64) error {
	if bytes == 0 {
		return nil
	}
	return database.GetExecutor(r.db, ctx).
		Model(&domain.CoreUser{}).
		Where("id = ?", coreUserID).
		Update("data_used", gorm.Expr("data_used + ?", bytes)).Error
}

func (r *coreRepository) ResetDataBySubscription(ctx context.Context, subID uint) error {
	return database.GetExecutor(r.db, ctx).
		Model(&domain.CoreUser{}).
		Where("subscription_id = ?", subID).
		Update("data_used", 0).Error
}

func (r *coreRepository) DisabledProtocolsForNode(ctx context.Context, nodeID uint) (map[domain.CoreProtocol]bool, error) {
	var rows []domain.NodeCoreSetting
	err := database.GetExecutor(r.db, ctx).
		Where("node_id = ? AND enabled = ?", nodeID, false).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[domain.CoreProtocol]bool, len(rows))
	for _, row := range rows {
		out[row.Protocol] = true
	}
	return out, nil
}

func (r *coreRepository) SetNodeProtocolEnabled(ctx context.Context, nodeID uint, proto domain.CoreProtocol, enabled bool) error {
	row := domain.NodeCoreSetting{NodeID: nodeID, Protocol: proto, Enabled: enabled}
	return database.GetExecutor(r.db, ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "node_id"}, {Name: "protocol"}},
			DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
		}).
		Create(&row).Error
}
