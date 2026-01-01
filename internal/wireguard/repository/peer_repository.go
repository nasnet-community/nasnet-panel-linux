package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

type WGPeerRepository interface {
	Create(ctx context.Context, p *domain.WGPeer) error
	Update(ctx context.Context, p *domain.WGPeer) error
	FindByID(ctx context.Context, id uint) (*domain.WGPeer, error)
	Delete(ctx context.Context, id uint) error

	ListBySubscription(ctx context.Context, subID uint) ([]*domain.WGPeer, error)
	CountActiveBySubscription(ctx context.Context, subID uint) (int64, error)
	ListActiveByInbound(ctx context.Context, inboundID uint) ([]*domain.WGPeer, error)
	ListUsedIPs(ctx context.Context, inboundID uint) ([]string, error)

	SetStatusBySubscription(ctx context.Context, subID uint, status domain.WGPeerStatus) error

	AddUsage(ctx context.Context, id uint, up, down int64) error
	TouchLastSeen(ctx context.Context, id uint, t time.Time) error
}

type wgPeerRepository struct{ db *gorm.DB }

func NewWGPeerRepository(db *gorm.DB) WGPeerRepository { return &wgPeerRepository{db: db} }

func (r *wgPeerRepository) Create(ctx context.Context, p *domain.WGPeer) error {
	return database.GetExecutor(r.db, ctx).Create(p).Error
}

func (r *wgPeerRepository) Update(ctx context.Context, p *domain.WGPeer) error {
	return database.GetExecutor(r.db, ctx).Save(p).Error
}

func (r *wgPeerRepository) FindByID(ctx context.Context, id uint) (*domain.WGPeer, error) {
	var p domain.WGPeer
	if err := database.GetExecutor(r.db, ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *wgPeerRepository) Delete(ctx context.Context, id uint) error {
	return database.GetExecutor(r.db, ctx).Delete(&domain.WGPeer{}, id).Error
}

func (r *wgPeerRepository) ListBySubscription(ctx context.Context, subID uint) ([]*domain.WGPeer, error) {
	var ps []*domain.WGPeer
	err := database.GetExecutor(r.db, ctx).Where("subscription_id = ?", subID).Order("id asc").Find(&ps).Error
	return ps, err
}

func (r *wgPeerRepository) CountActiveBySubscription(ctx context.Context, subID uint) (int64, error) {
	var n int64
	err := database.GetExecutor(r.db, ctx).Model(&domain.WGPeer{}).
		Where("subscription_id = ? AND status = ?", subID, domain.WGPeerStatusActive).Count(&n).Error
	return n, err
}

func (r *wgPeerRepository) ListActiveByInbound(ctx context.Context, inboundID uint) ([]*domain.WGPeer, error) {
	var ps []*domain.WGPeer
	err := database.GetExecutor(r.db, ctx).
		Where("inbound_id = ? AND status = ?", inboundID, domain.WGPeerStatusActive).
		Order("id asc").Find(&ps).Error
	return ps, err
}

func (r *wgPeerRepository) ListUsedIPs(ctx context.Context, inboundID uint) ([]string, error) {
	var ips []string
	err := database.GetExecutor(r.db, ctx).Model(&domain.WGPeer{}).
		Where("inbound_id = ?", inboundID).Pluck("assigned_ip", &ips).Error
	return ips, err
}

func (r *wgPeerRepository) SetStatusBySubscription(ctx context.Context, subID uint, status domain.WGPeerStatus) error {
	return database.GetExecutor(r.db, ctx).Model(&domain.WGPeer{}).
		Where("subscription_id = ?", subID).Update("status", status).Error
}

func (r *wgPeerRepository) AddUsage(ctx context.Context, id uint, up, down int64) error {
	return database.GetExecutor(r.db, ctx).Model(&domain.WGPeer{}).
		Where("id = ?", id).
		UpdateColumns(map[string]interface{}{
			"up_bytes":   gorm.Expr("up_bytes + ?", up),
			"down_bytes": gorm.Expr("down_bytes + ?", down),
		}).Error
}

func (r *wgPeerRepository) TouchLastSeen(ctx context.Context, id uint, t time.Time) error {
	return database.GetExecutor(r.db, ctx).Model(&domain.WGPeer{}).
		Where("id = ?", id).Update("last_seen", t).Error
}
