package usecase

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/sni/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/acme"
	"gorm.io/gorm"
)

// SNIUsecase defines the interface for SNI business logic
type SNIUsecase interface {
	Create(ctx context.Context, name, domainName, certificate, privateKey, alpn string) (*domain.SNI, error)
	CreateWithPaths(ctx context.Context, name, domainName, certPath, keyPath, alpn string) (*domain.SNI, error)
	List(ctx context.Context) ([]*domain.SNI, error)
	GetByID(ctx context.Context, id uint) (*domain.SNI, error)
	GetByDomain(ctx context.Context, domainName string) (*domain.SNI, error)
	Update(ctx context.Context, id uint, name, domainName, certificate, privateKey, alpn string) error
	Delete(ctx context.Context, id uint) error
	ValidateCertificate(certificate string) (expiry time.Time, err error)
	ValidateCertKey(certPEM, keyPEM, domainName string) (expiry time.Time, sanWarning string, err error)

	// ACME Certificate Issuance
	IssueCertHTTP01(ctx context.Context, name, domainName string) (*domain.SNI, error)
	StartDNS01Challenge(ctx context.Context, domainName string) (*acme.DNS01Challenge, error)
	CompleteDNS01Challenge(ctx context.Context, name, domainName string) (*domain.SNI, error)
	RenewCertificate(ctx context.Context, id uint) error
	GetExpiringCertificates(ctx context.Context, days int) ([]*domain.SNI, error)
	MarkExpiryNotified(ctx context.Context, id uint, level int) error
	HasPendingChallenge(domainName string) bool
	GetPendingChallenge(domainName string) (*acme.DNS01Challenge, bool)

	// inbound_sni link tracking — kept in step by the node usecase on apply/clear.
	LinkInbound(ctx context.Context, inboundID, sniID, nodeID uint) error
	UnlinkInbound(ctx context.Context, inboundID uint) error
	CountInbounds(ctx context.Context, sniID uint) (int64, error)
	ListNodeIDs(ctx context.Context, sniID uint) ([]uint, error)

	// SetRepusher injects the node-side re-push hook (broken out of the
	// constructor to avoid a node<->sni import cycle).
	SetRepusher(r Repusher)
}

// Repusher re-applies node config after a cert changes. Implemented by the node
// usecase, injected via SetRepusher.
type Repusher interface {
	RepushForSNI(ctx context.Context, sniID uint)
}

type sniUsecase struct {
	sniRepo     repository.SNIRepository
	certManager *acme.CertManager
	repusher    Repusher
}

// NewSNIUsecase creates a new SNI usecase
func NewSNIUsecase(sniRepo repository.SNIRepository, certManager *acme.CertManager) SNIUsecase {
	return &sniUsecase{sniRepo: sniRepo, certManager: certManager}
}

func (u *sniUsecase) Create(ctx context.Context, name, domainName, certificate, privateKey, alpn string) (*domain.SNI, error) {
	// Validate inputs
	if name == "" {
		return nil, errors.New("name is required")
	}
	if domainName == "" {
		return nil, errors.New("domain is required")
	}
	if certificate == "" {
		return nil, errors.New("certificate is required")
	}
	if privateKey == "" {
		return nil, errors.New("private key is required")
	}

	// Normalize domain
	domainName = strings.TrimSpace(strings.ToLower(domainName))

	// Check for duplicate domain — distinguish "not found" from a real DB error.
	existing, err := u.sniRepo.FindByDomain(ctx, domainName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("an SNI with this domain already exists")
	}

	// Cert must parse, match the key, and be unexpired before we store it.
	expiry, _, err := u.ValidateCertKey(certificate, privateKey, domainName)
	if err != nil {
		return nil, err
	}

	// Set default ALPN if not provided
	if alpn == "" || alpn == "-" {
		alpn = "h2,http/1.1"
	}

	sni := &domain.SNI{
		Name:        strings.TrimSpace(name),
		Domain:      domainName,
		Certificate: strings.TrimSpace(certificate),
		PrivateKey:  strings.TrimSpace(privateKey),
		ALPN:        alpn,
		UsePathMode: false,
		ExpiresAt:   expiry,
	}

	if err := u.sniRepo.Create(ctx, sni); err != nil {
		return nil, err
	}

	return sni, nil
}

// CreateWithPaths creates an SNI using file paths instead of content
func (u *sniUsecase) CreateWithPaths(ctx context.Context, name, domainName, certPath, keyPath, alpn string) (*domain.SNI, error) {
	// Validate inputs
	if name == "" {
		return nil, errors.New("name is required")
	}
	if domainName == "" {
		return nil, errors.New("domain is required")
	}
	if certPath == "" {
		return nil, errors.New("certificate path is required")
	}
	if keyPath == "" {
		return nil, errors.New("key path is required")
	}

	// Normalize domain
	domainName = strings.TrimSpace(strings.ToLower(domainName))

	// Check for duplicate domain — distinguish "not found" from a real DB error.
	existing, err := u.sniRepo.FindByDomain(ctx, domainName)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("an SNI with this domain already exists")
	}

	// Read and validate the referenced files now so a bad path or mismatched
	// pair is caught at import instead of breaking a node at push time.
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read certificate file: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read key file: %w", err)
	}
	expiry, _, err := u.ValidateCertKey(string(certPEM), string(keyPEM), domainName)
	if err != nil {
		return nil, err
	}

	// Set default ALPN if not provided
	if alpn == "" || alpn == "-" {
		alpn = "h2,http/1.1"
	}

	sni := &domain.SNI{
		Name:        strings.TrimSpace(name),
		Domain:      domainName,
		CertPath:    strings.TrimSpace(certPath),
		KeyPath:     strings.TrimSpace(keyPath),
		ALPN:        alpn,
		UsePathMode: true,
		ExpiresAt:   expiry,
	}

	if err := u.sniRepo.Create(ctx, sni); err != nil {
		return nil, err
	}

	return sni, nil
}

func (u *sniUsecase) List(ctx context.Context) ([]*domain.SNI, error) {
	return u.sniRepo.FindAll(ctx)
}

func (u *sniUsecase) GetByID(ctx context.Context, id uint) (*domain.SNI, error) {
	return u.sniRepo.FindByID(ctx, id)
}

func (u *sniUsecase) GetByDomain(ctx context.Context, domainName string) (*domain.SNI, error) {
	return u.sniRepo.FindByDomain(ctx, domainName)
}

func (u *sniUsecase) Update(ctx context.Context, id uint, name, domainName, certificate, privateKey, alpn string) error {
	sni, err := u.sniRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if name != "" {
		sni.Name = strings.TrimSpace(name)
	}
	if domainName != "" {
		domainName = strings.TrimSpace(strings.ToLower(domainName))
		// Check for duplicate if domain changed
		if domainName != sni.Domain {
			existing, fErr := u.sniRepo.FindByDomain(ctx, domainName)
			if fErr != nil && !errors.Is(fErr, gorm.ErrRecordNotFound) {
				return fErr
			}
			if existing != nil && existing.ID != id {
				return errors.New("an SNI with this domain already exists")
			}
		}
		sni.Domain = domainName
	}

	// Cert/key may be updated independently; validate the resulting pair.
	certChanged := certificate != "" || privateKey != ""
	if certChanged {
		newCert := sni.Certificate
		if certificate != "" {
			newCert = strings.TrimSpace(certificate)
		}
		newKey := sni.PrivateKey
		if privateKey != "" {
			newKey = strings.TrimSpace(privateKey)
		}
		if !sni.UsePathMode {
			expiry, _, vErr := u.ValidateCertKey(newCert, newKey, sni.Domain)
			if vErr != nil {
				return vErr
			}
			sni.ExpiresAt = expiry
		}
		sni.Certificate = newCert
		sni.PrivateKey = newKey
		// A manual overwrite ends any ACME lineage and re-arms expiry alerts.
		sni.IsAutoIssued = false
		sni.ChallengeType = ""
		sni.ExpiryNotifyLevel = 0
	}
	if alpn != "" {
		sni.ALPN = alpn
	}

	if err := u.sniRepo.Update(ctx, sni); err != nil {
		return err
	}
	// Only cert/key are resolved live at push time, so only those need a re-push.
	if certChanged {
		u.repush(ctx, id)
	}
	return nil
}

func (u *sniUsecase) Delete(ctx context.Context, id uint) error {
	// Refuse to delete a domain that inbounds still serve — otherwise the next
	// config push resolves a missing cert and the node's TLS breaks.
	n, err := u.sniRepo.CountInbounds(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("domain is in use by %d inbound(s); detach or replace its certificate first", n)
	}
	return u.sniRepo.Delete(ctx, id)
}

func (u *sniUsecase) ValidateCertificate(certificate string) (time.Time, error) {
	block, _ := pem.Decode([]byte(certificate))
	if block == nil {
		return time.Time{}, errors.New("invalid PEM format: could not decode certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, errors.New("invalid certificate: " + err.Error())
	}

	if cert.NotAfter.Before(time.Now()) {
		return cert.NotAfter, errors.New("certificate has expired")
	}

	return cert.NotAfter, nil
}

// ValidateCertKey verifies the cert parses, the private key matches it, and the
// cert is unexpired. A SAN/CN mismatch with the domain is returned as a warning
// (wildcards and multi-SAN certs are legitimate), not a hard error.
func (u *sniUsecase) ValidateCertKey(certPEM, keyPEM, domainName string) (time.Time, string, error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return time.Time{}, "", errors.New("certificate and private key do not match: " + err.Error())
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return time.Time{}, "", errors.New("invalid certificate: " + err.Error())
	}
	if leaf.NotAfter.Before(time.Now()) {
		return leaf.NotAfter, "", errors.New("certificate has expired")
	}
	var sanWarning string
	if d := strings.TrimSpace(strings.ToLower(domainName)); d != "" {
		if err := leaf.VerifyHostname(d); err != nil {
			sanWarning = "certificate does not cover " + d
		}
	}
	return leaf.NotAfter, sanWarning, nil
}

// === ACME Certificate Issuance Methods ===

// IssueCertHTTP01 obtains a certificate using HTTP-01 challenge (requires port 80)
func (u *sniUsecase) IssueCertHTTP01(ctx context.Context, name, domainName string) (*domain.SNI, error) {
	if u.certManager == nil {
		return nil, errors.New("ACME not configured")
	}

	domainName = strings.TrimSpace(strings.ToLower(domainName))

	// Check for duplicate
	existing, _ := u.sniRepo.FindByDomain(ctx, domainName)
	if existing != nil {
		return nil, errors.New("an SNI with this domain already exists")
	}

	// Issue certificate
	result, err := u.certManager.IssueWithHTTP01(ctx, domainName)
	if err != nil {
		return nil, err
	}

	// Create SNI record
	sni := &domain.SNI{
		Name:          name,
		Domain:        domainName,
		Certificate:   string(result.Certificate),
		PrivateKey:    string(result.PrivateKey),
		ALPN:          "h2,http/1.1",
		IsAutoIssued:  true,
		ChallengeType: acme.ChallengeHTTP01,
		ExpiresAt:     result.ExpiresAt,
		AutoRenew:     true,
	}

	if err := u.sniRepo.Create(ctx, sni); err != nil {
		return nil, err
	}

	return sni, nil
}

// StartDNS01Challenge starts a DNS-01 challenge and returns TXT record info
func (u *sniUsecase) StartDNS01Challenge(ctx context.Context, domainName string) (*acme.DNS01Challenge, error) {
	if u.certManager == nil {
		return nil, errors.New("ACME not configured")
	}

	domainName = strings.TrimSpace(strings.ToLower(domainName))

	// Check for duplicate
	existing, _ := u.sniRepo.FindByDomain(ctx, domainName)
	if existing != nil {
		return nil, errors.New("an SNI with this domain already exists")
	}

	return u.certManager.GetDNS01Challenge(ctx, domainName)
}

// CompleteDNS01Challenge completes DNS-01 after user adds TXT record
func (u *sniUsecase) CompleteDNS01Challenge(ctx context.Context, name, domainName string) (*domain.SNI, error) {
	if u.certManager == nil {
		return nil, errors.New("ACME not configured")
	}

	domainName = strings.TrimSpace(strings.ToLower(domainName))

	// Complete the challenge
	result, err := u.certManager.CompleteDNS01(ctx, domainName)
	if err != nil {
		return nil, err
	}

	// Create SNI record
	sni := &domain.SNI{
		Name:          name,
		Domain:        domainName,
		Certificate:   string(result.Certificate),
		PrivateKey:    string(result.PrivateKey),
		ALPN:          "h2,http/1.1",
		IsAutoIssued:  true,
		ChallengeType: acme.ChallengeDNS01,
		ExpiresAt:     result.ExpiresAt,
		AutoRenew:     true,
	}

	if err := u.sniRepo.Create(ctx, sni); err != nil {
		return nil, err
	}

	return sni, nil
}

// RenewCertificate renews an auto-issued certificate
func (u *sniUsecase) RenewCertificate(ctx context.Context, id uint) error {
	if u.certManager == nil {
		return errors.New("ACME not configured")
	}

	sni, err := u.sniRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if !sni.IsAutoIssued {
		return errors.New("cannot auto-renew manually issued certificate")
	}

	// Renew using the same challenge type
	var result *acme.CertResult
	switch sni.ChallengeType {
	case acme.ChallengeHTTP01:
		result, err = u.certManager.IssueWithHTTP01(ctx, sni.Domain)
	default:
		return errors.New("cannot auto-renew DNS-01 certificates (requires manual intervention)")
	}

	if err != nil {
		sni.IssueError = err.Error()
		u.sniRepo.Update(ctx, sni)
		return err
	}

	// Update certificate
	sni.Certificate = string(result.Certificate)
	sni.PrivateKey = string(result.PrivateKey)
	sni.ExpiresAt = result.ExpiresAt
	sni.IssueError = ""

	if err := u.sniRepo.Update(ctx, sni); err != nil {
		return err
	}
	// New cert material on record — push it to every node that serves it.
	u.repush(ctx, id)
	return nil
}

// GetExpiringCertificates returns certs (auto-issued or manual) expiring within days.
func (u *sniUsecase) GetExpiringCertificates(ctx context.Context, days int) ([]*domain.SNI, error) {
	return u.sniRepo.FindExpiring(ctx, days)
}

// MarkExpiryNotified records the smallest expiry threshold already alerted so the
// scheduler sends one notification per threshold instead of every pass.
func (u *sniUsecase) MarkExpiryNotified(ctx context.Context, id uint, level int) error {
	sni, err := u.sniRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	sni.ExpiryNotifyLevel = level
	return u.sniRepo.Update(ctx, sni)
}

// HasPendingChallenge checks if there's a pending DNS-01 challenge
func (u *sniUsecase) HasPendingChallenge(domainName string) bool {
	if u.certManager == nil {
		return false
	}
	return u.certManager.HasPendingChallenge(domainName)
}

// GetPendingChallenge retrieves pending DNS-01 challenge info
func (u *sniUsecase) GetPendingChallenge(domainName string) (*acme.DNS01Challenge, bool) {
	if u.certManager == nil {
		return nil, false
	}
	return u.certManager.GetPendingChallenge(domainName)
}

func (u *sniUsecase) SetRepusher(r Repusher) { u.repusher = r }

func (u *sniUsecase) LinkInbound(ctx context.Context, inboundID, sniID, nodeID uint) error {
	return u.sniRepo.LinkInbound(ctx, inboundID, sniID, nodeID)
}

func (u *sniUsecase) UnlinkInbound(ctx context.Context, inboundID uint) error {
	return u.sniRepo.UnlinkInbound(ctx, inboundID)
}

func (u *sniUsecase) CountInbounds(ctx context.Context, sniID uint) (int64, error) {
	return u.sniRepo.CountInbounds(ctx, sniID)
}

func (u *sniUsecase) ListNodeIDs(ctx context.Context, sniID uint) ([]uint, error) {
	return u.sniRepo.ListNodeIDs(ctx, sniID)
}

// repush pushes the cert's new material to every node that serves it. Detached
// context + goroutine so it never blocks the caller and never touches an agent
// inside a DB transaction.
func (u *sniUsecase) repush(ctx context.Context, sniID uint) {
	if u.repusher != nil {
		go u.repusher.RepushForSNI(context.WithoutCancel(ctx), sniID)
	}
}
