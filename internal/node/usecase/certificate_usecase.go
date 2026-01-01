package usecase

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/acme"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// CertificateUsecase handles public / ACME certificate management (inbound TLS).
type CertificateUsecase interface {
	RevokeCertificate(ctx context.Context, id uint) error
	ListRevokedSerialNumbers(ctx context.Context) ([]string, error)
	SetOnRevokeCallback(fn func(ctx context.Context))
	ListCertificates(ctx context.Context) ([]*domain.AgentCertificate, error)
	ListExpiringSoon(ctx context.Context, days int) ([]*domain.AgentCertificate, error)
	IssuePublicCert(ctx context.Context, domain string) (*domain.AgentCertificate, error)
	StartDNSChallenge(ctx context.Context, domain string) (*acme.DNS01Challenge, error)
	CompleteDNSChallenge(ctx context.Context, domain string) (*domain.AgentCertificate, error)
	GetCertificate(ctx context.Context, id uint) (*domain.AgentCertificate, error)
	RenewCertificate(ctx context.Context, id uint) (*domain.AgentCertificate, error)
	DeleteCertificate(ctx context.Context, id uint) error
	ToggleAutoRenew(ctx context.Context, id uint, enabled bool) error
}

type certificateUsecase struct {
	certRepo repository.AgentCertificateRepository
	nodeRepo repository.NodeRepository
	acmeMgr  *acme.CertManager
	onRevoke func(ctx context.Context) // Called after a certificate is revoked (e.g., to push denylist)
}

// NewCertificateUsecase creates a new certificate usecase
func NewCertificateUsecase(
	certRepo repository.AgentCertificateRepository,
	nodeRepo repository.NodeRepository,
	acmeMgr *acme.CertManager,
) CertificateUsecase {
	return &certificateUsecase{
		certRepo: certRepo,
		nodeRepo: nodeRepo,
		acmeMgr:  acmeMgr,
	}
}

// RevokeCertificate marks a certificate as revoked and triggers denylist push
func (u *certificateUsecase) RevokeCertificate(ctx context.Context, id uint) error {
	if err := u.certRepo.Revoke(ctx, id); err != nil {
		return err
	}

	// Trigger denylist push (async so it doesn't block the revocation response)
	if u.onRevoke != nil {
		go u.onRevoke(context.Background())
	}

	return nil
}

// SetOnRevokeCallback sets a callback that fires after a certificate is revoked
func (u *certificateUsecase) SetOnRevokeCallback(fn func(ctx context.Context)) {
	u.onRevoke = fn
}

// ListRevokedSerialNumbers returns serial numbers of all revoked certificates
func (u *certificateUsecase) ListRevokedSerialNumbers(ctx context.Context) ([]string, error) {
	return u.certRepo.ListRevokedSerialNumbers(ctx)
}

// ListCertificates returns all certificates
func (u *certificateUsecase) ListCertificates(ctx context.Context) ([]*domain.AgentCertificate, error) {
	return u.certRepo.ListAll(ctx)
}

// ListExpiringSoon returns certificates expiring within the given days
func (u *certificateUsecase) ListExpiringSoon(ctx context.Context, days int) ([]*domain.AgentCertificate, error) {
	return u.certRepo.ListExpiringSoon(ctx, days)
}

// IssuePublicCert issues a public certificate using HTTP-01 challenge
func (u *certificateUsecase) IssuePublicCert(ctx context.Context, domainName string) (*domain.AgentCertificate, error) {
	if u.acmeMgr == nil {
		return nil, fmt.Errorf("ACME manager not configured")
	}

	log := logger.GetLogger()

	// Reuse an existing valid cert for this domain if it has plenty of life left
	allCerts, err := u.certRepo.ListAll(ctx)
	if err == nil {
		for _, cert := range allCerts {
			if cert.Type == domain.CertTypePublic && cert.CommonName == domainName && cert.IsValid() {
				if cert.DaysUntilExpiry() > 30 {
					return cert, nil
				}
				// Expiring soon — revoke before issuing a replacement
				if err := u.RevokeCertificate(ctx, cert.ID); err != nil {
					log.WithError(err).Warn("[IssuePublicCert] Failed to revoke expiring cert for domain")
				}
			}
		}
	}

	log.WithField("domain", domainName).Info("[IssuePublicCert] Starting ACME issuance")

	result, err := u.acmeMgr.IssueWithHTTP01(ctx, domainName)
	if err != nil {
		return nil, fmt.Errorf("failed to issue certificate: %w", err)
	}

	serialNumber, err := parseCertSerialNumber(result.Certificate)
	if err != nil {
		return nil, fmt.Errorf("invalid certificate generated: %w", err)
	}

	cert := &domain.AgentCertificate{
		Type:         domain.CertTypePublic,
		CommonName:   domainName,
		SerialNumber: serialNumber,
		Certificate:  result.Certificate,
		PrivateKey:   result.PrivateKey,
		NotBefore:    result.NotBefore,
		NotAfter:     result.ExpiresAt,
	}

	if err := u.certRepo.Create(ctx, cert); err != nil {
		return nil, fmt.Errorf("failed to save public certificate: %w", err)
	}

	log.WithField("domain", domainName).Info("[IssuePublicCert] Successfully issued public certificate")
	return cert, nil
}

// StartDNSChallenge initiates a DNS-01 authorization
func (u *certificateUsecase) StartDNSChallenge(ctx context.Context, domainName string) (*acme.DNS01Challenge, error) {
	if u.acmeMgr == nil {
		return nil, fmt.Errorf("ACME manager not configured")
	}
	return u.acmeMgr.GetDNS01Challenge(ctx, domainName)
}

// CompleteDNSChallenge completes a DNS-01 authorization and issuance
func (u *certificateUsecase) CompleteDNSChallenge(ctx context.Context, domainName string) (*domain.AgentCertificate, error) {
	if u.acmeMgr == nil {
		return nil, fmt.Errorf("ACME manager not configured")
	}

	log := logger.GetLogger()
	log.WithField("domain", domainName).Info("[CompleteDNSChallenge] Completing DNS-01 challenge")

	result, err := u.acmeMgr.CompleteDNS01(ctx, domainName)
	if err != nil {
		return nil, fmt.Errorf("verification failed: %w", err)
	}

	serialNumber, err := parseCertSerialNumber(result.Certificate)
	if err != nil {
		return nil, fmt.Errorf("invalid certificate generated: %w", err)
	}

	cert := &domain.AgentCertificate{
		Type:         domain.CertTypePublic,
		CommonName:   domainName,
		SerialNumber: serialNumber,
		Certificate:  result.Certificate,
		PrivateKey:   result.PrivateKey,
		NotBefore:    result.NotBefore,
		NotAfter:     result.ExpiresAt,
	}

	if err := u.certRepo.Create(ctx, cert); err != nil {
		return nil, fmt.Errorf("failed to save public certificate: %w", err)
	}

	log.WithField("domain", domainName).Info("[CompleteDNSChallenge] Successfully issued public certificate via DNS-01")
	return cert, nil
}

// GetCertificate returns a certificate by ID
func (u *certificateUsecase) GetCertificate(ctx context.Context, id uint) (*domain.AgentCertificate, error) {
	return u.certRepo.GetByID(ctx, id)
}

// parseCertSerialNumber extracts the serial number from a PEM certificate
func parseCertSerialNumber(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	return cert.SerialNumber.String(), nil
}

// RenewCertificate reissues a public certificate for the same domain
func (u *certificateUsecase) RenewCertificate(ctx context.Context, id uint) (*domain.AgentCertificate, error) {
	log := logger.GetLogger()

	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("certificate not found: %w", err)
	}

	if cert.Type != domain.CertTypePublic {
		return nil, fmt.Errorf("only public certificates can be renewed")
	}

	// Revoke the old one first to keep the denylist clean, then re-issue.
	if !cert.IsRevoked {
		if err := u.RevokeCertificate(ctx, cert.ID); err != nil {
			log.WithError(err).Warn("[RenewCertificate] Failed to revoke old public cert")
		}
	}
	// HTTP-01 only here. DNS-01 renewal runs through the wizard UI
	return u.IssuePublicCert(ctx, cert.CommonName)
}

// ToggleAutoRenew sets the auto-renew flag on a certificate
func (u *certificateUsecase) ToggleAutoRenew(ctx context.Context, id uint, enabled bool) error {
	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("certificate not found: %w", err)
	}

	if cert.Type != domain.CertTypePublic {
		return fmt.Errorf("auto-renew is only supported for public certificates")
	}

	cert.AutoRenew = enabled
	return u.certRepo.Update(ctx, cert)
}

// DeleteCertificate deletes a certificate from the database
func (u *certificateUsecase) DeleteCertificate(ctx context.Context, id uint) error {
	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if cert.Type == domain.CertTypeCA {
		return fmt.Errorf("cannot delete CA certificate")
	}

	return u.certRepo.Delete(ctx, id)
}
