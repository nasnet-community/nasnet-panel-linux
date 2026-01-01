package repository

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/gorm"
)

// AgentCertificateRepository defines the interface for certificate storage
type AgentCertificateRepository interface {
	Create(ctx context.Context, cert *domain.AgentCertificate) error
	GetByID(ctx context.Context, id uint) (*domain.AgentCertificate, error)
	GetBySerialNumber(ctx context.Context, serialNumber string) (*domain.AgentCertificate, error)
	GetByNodeID(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error)
	GetCA(ctx context.Context) (*domain.AgentCertificate, error)
	GetMaster(ctx context.Context) (*domain.AgentCertificate, error)
	ListAll(ctx context.Context) ([]*domain.AgentCertificate, error)
	ListByType(ctx context.Context, certType string) ([]*domain.AgentCertificate, error)
	ListExpiringSoon(ctx context.Context, days int) ([]*domain.AgentCertificate, error)
	Update(ctx context.Context, cert *domain.AgentCertificate) error
	Revoke(ctx context.Context, id uint) error
	ListRevokedSerialNumbers(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, id uint) error
	DeleteAll(ctx context.Context) error // Deletes all certificates (for CA reinitialization)
}

type agentCertificateRepository struct {
	db *gorm.DB
}

// NewAgentCertificateRepository creates a new certificate repository
func NewAgentCertificateRepository(db *gorm.DB) AgentCertificateRepository {
	return &agentCertificateRepository{db: db}
}

func (r *agentCertificateRepository) Create(ctx context.Context, cert *domain.AgentCertificate) error {
	return r.db.WithContext(ctx).Create(cert).Error
}

func (r *agentCertificateRepository) GetByID(ctx context.Context, id uint) (*domain.AgentCertificate, error) {
	var cert domain.AgentCertificate
	if err := r.db.WithContext(ctx).First(&cert, id).Error; err != nil {
		return nil, err
	}
	return &cert, nil
}

func (r *agentCertificateRepository) GetBySerialNumber(ctx context.Context, serialNumber string) (*domain.AgentCertificate, error) {
	var cert domain.AgentCertificate
	if err := r.db.WithContext(ctx).Where("serial_number = ?", serialNumber).First(&cert).Error; err != nil {
		return nil, err
	}
	return &cert, nil
}

func (r *agentCertificateRepository) GetByNodeID(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error) {
	var cert domain.AgentCertificate
	if err := r.db.WithContext(ctx).
		Where("node_id = ? AND type = ? AND is_revoked = false", nodeID, domain.CertTypeAgent).
		Order("created_at DESC").
		First(&cert).Error; err != nil {
		return nil, err
	}
	return &cert, nil
}

func (r *agentCertificateRepository) GetCA(ctx context.Context) (*domain.AgentCertificate, error) {
	var cert domain.AgentCertificate
	if err := r.db.WithContext(ctx).
		Where("type = ? AND is_revoked = false", domain.CertTypeCA).
		Order("created_at DESC").
		First(&cert).Error; err != nil {
		return nil, err
	}
	return &cert, nil
}

func (r *agentCertificateRepository) GetMaster(ctx context.Context) (*domain.AgentCertificate, error) {
	var cert domain.AgentCertificate
	if err := r.db.WithContext(ctx).
		Where("type = ? AND is_revoked = false", domain.CertTypeMaster).
		Order("created_at DESC").
		First(&cert).Error; err != nil {
		return nil, err
	}
	return &cert, nil
}

func (r *agentCertificateRepository) ListAll(ctx context.Context) ([]*domain.AgentCertificate, error) {
	var certs []*domain.AgentCertificate
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&certs).Error; err != nil {
		return nil, err
	}
	return certs, nil
}

func (r *agentCertificateRepository) ListByType(ctx context.Context, certType string) ([]*domain.AgentCertificate, error) {
	var certs []*domain.AgentCertificate
	if err := r.db.WithContext(ctx).
		Where("type = ?", certType).
		Order("created_at DESC").
		Find(&certs).Error; err != nil {
		return nil, err
	}
	return certs, nil
}

func (r *agentCertificateRepository) ListExpiringSoon(ctx context.Context, days int) ([]*domain.AgentCertificate, error) {
	var certs []*domain.AgentCertificate
	threshold := time.Now().AddDate(0, 0, days)
	if err := r.db.WithContext(ctx).
		Where("not_after < ? AND is_revoked = false", threshold).
		Order("not_after ASC").
		Find(&certs).Error; err != nil {
		return nil, err
	}
	return certs, nil
}

func (r *agentCertificateRepository) Update(ctx context.Context, cert *domain.AgentCertificate) error {
	return r.db.WithContext(ctx).Save(cert).Error
}

func (r *agentCertificateRepository) Revoke(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&domain.AgentCertificate{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"is_revoked": true,
			"revoked_at": &now,
		}).Error
}

func (r *agentCertificateRepository) ListRevokedSerialNumbers(ctx context.Context) ([]string, error) {
	var serials []string
	if err := r.db.WithContext(ctx).
		Model(&domain.AgentCertificate{}).
		Where("is_revoked = true").
		Pluck("serial_number", &serials).Error; err != nil {
		return nil, err
	}
	return serials, nil
}

func (r *agentCertificateRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.AgentCertificate{}, id).Error
}

// DeleteAll removes all certificates (for CA reinitialization)
func (r *agentCertificateRepository) DeleteAll(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec("DELETE FROM agent_certificates").Error
}
