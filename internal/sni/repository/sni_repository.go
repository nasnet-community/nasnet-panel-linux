package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	"gorm.io/gorm"
)

// SNIRepository defines the interface for SNI persistence
type SNIRepository interface {
	Create(ctx context.Context, sni *domain.SNI) error
	FindAll(ctx context.Context) ([]*domain.SNI, error)
	FindByID(ctx context.Context, id uint) (*domain.SNI, error)
	FindByDomain(ctx context.Context, domainName string) (*domain.SNI, error)
	FindExpiring(ctx context.Context, withinDays int) ([]*domain.SNI, error) // Find certs expiring soon
	Update(ctx context.Context, sni *domain.SNI) error
	Delete(ctx context.Context, id uint) error

	// inbound_sni join table — mirrors which inbounds reference which SNI cert.
	LinkInbound(ctx context.Context, inboundID, sniID, nodeID uint) error
	UnlinkInbound(ctx context.Context, inboundID uint) error
	CountInbounds(ctx context.Context, sniID uint) (int64, error)
	ListNodeIDs(ctx context.Context, sniID uint) ([]uint, error)
	ListInboundIDs(ctx context.Context, sniID uint) ([]uint, error)
}

type sniRepository struct {
	db *gorm.DB
}

// NewSNIRepository creates a new SNI repository
func NewSNIRepository(db *gorm.DB) SNIRepository {
	return &sniRepository{db: db}
}

func (r *sniRepository) Create(ctx context.Context, sni *domain.SNI) error {
	return r.db.WithContext(ctx).Create(sni).Error
}

func (r *sniRepository) FindAll(ctx context.Context) ([]*domain.SNI, error) {
	var snis []*domain.SNI
	err := r.db.WithContext(ctx).Order("name ASC").Find(&snis).Error
	return snis, err
}

func (r *sniRepository) FindByID(ctx context.Context, id uint) (*domain.SNI, error) {
	var sni domain.SNI
	err := r.db.WithContext(ctx).First(&sni, id).Error
	if err != nil {
		return nil, err
	}
	return &sni, nil
}

func (r *sniRepository) FindByDomain(ctx context.Context, domainName string) (*domain.SNI, error) {
	var sni domain.SNI
	err := r.db.WithContext(ctx).Where("domain = ?", domainName).First(&sni).Error
	if err != nil {
		return nil, err
	}
	return &sni, nil
}

func (r *sniRepository) Update(ctx context.Context, sni *domain.SNI) error {
	return r.db.WithContext(ctx).Save(sni).Error
}

func (r *sniRepository) Delete(ctx context.Context, id uint) error {
	// Use Unscoped to hard delete because unique index on domain conflicts with soft deletion
	return r.db.WithContext(ctx).Unscoped().Delete(&domain.SNI{}, id).Error
}

func (r *sniRepository) FindExpiring(ctx context.Context, withinDays int) ([]*domain.SNI, error) {
	var snis []*domain.SNI
	targetTime := time.Now().AddDate(0, 0, withinDays)

	// Any cert with a known expiry inside the window — manual ones too. The
	// scheduler decides what to do with each (auto-renew vs. notify the admin).
	err := r.db.WithContext(ctx).
		Where("expires_at > ? AND expires_at <= ?", time.Time{}, targetTime).
		Find(&snis).Error
	return snis, err
}

func (r *sniRepository) LinkInbound(ctx context.Context, inboundID, sniID, nodeID uint) error {
	// An inbound serves exactly one managed cert at a time; upsert keeps the row unique.
	link := &domain.InboundSNI{InboundID: inboundID, SNIID: sniID, NodeID: nodeID}
	return r.db.WithContext(ctx).Save(link).Error
}

func (r *sniRepository) UnlinkInbound(ctx context.Context, inboundID uint) error {
	return r.db.WithContext(ctx).Where("inbound_id = ?", inboundID).Delete(&domain.InboundSNI{}).Error
}

func (r *sniRepository) CountInbounds(ctx context.Context, sniID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.InboundSNI{}).Where("sni_id = ?", sniID).Count(&n).Error
	return n, err
}

func (r *sniRepository) ListNodeIDs(ctx context.Context, sniID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&domain.InboundSNI{}).
		Where("sni_id = ?", sniID).Distinct().Pluck("node_id", &ids).Error
	return ids, err
}

func (r *sniRepository) ListInboundIDs(ctx context.Context, sniID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&domain.InboundSNI{}).
		Where("sni_id = ?", sniID).Pluck("inbound_id", &ids).Error
	return ids, err
}
