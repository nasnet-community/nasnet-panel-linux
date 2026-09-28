package usecase

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// CertificateUsecase handles certificate management operations
type CertificateUsecase interface {
	// CA Management
	InitializeCA(ctx context.Context, cfg *certs.CAConfig) (*domain.AgentCertificate, error)
	ReinitializeCA(ctx context.Context, cfg *certs.CAConfig) (*domain.AgentCertificate, error) // Deletes all certs and creates new CA
	GetCA(ctx context.Context) (*domain.AgentCertificate, error)
	HasCA(ctx context.Context) bool

	// Agent certificates
	GenerateAgentCert(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error)
	RecordAgentCert(ctx context.Context, nodeID uint, info *certs.CertificateInfo) (*domain.AgentCertificate, error)
	GetAgentCert(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error)
	ListNodeCerts(ctx context.Context, nodeID uint) ([]*domain.AgentCertificate, error)
	GetCertificateBundle(ctx context.Context, nodeID uint) (*CertBundle, error)
	GenerateEphemeralClientCert(ctx context.Context) (*domain.AgentCertificate, error)

	// Revocation and cleanup
	RevokeCertificate(ctx context.Context, id uint) error
	RevokeByAdmin(ctx context.Context, id uint, confirm string) error
	RevokeNodeCerts(ctx context.Context, nodeID uint) (int, error)
	RetireSuperseded(ctx context.Context, nodeID uint, serial string) (int, error)
	RevokeOrphans(ctx context.Context) (int, error)
	PurgeRevokedExpired(ctx context.Context) (int64, error)
	DeleteCertificate(ctx context.Context, id uint, confirm string) error
	ListRevokedSerialNumbers(ctx context.Context) ([]string, error)
	SetOnRevokeCallback(fn func(ctx context.Context))
	SetSightingLookup(fn func(nodeID uint) (serial string, ok bool))

	// Queries
	ListCertificates(ctx context.Context) ([]*domain.AgentCertificate, error)
	ListExpiringSoon(ctx context.Context, days int) ([]*domain.AgentCertificate, error)
	GetCertificate(ctx context.Context, id uint) (*domain.AgentCertificate, error)
	RevealPrivateKey(ctx context.Context, id uint, confirm string) (string, error)
	SetExpiryNotifyLevel(ctx context.Context, id uint, level int) error
}

// CertBundle contains all certificates needed for an agent
type CertBundle struct {
	CACert    []byte // CA certificate PEM
	AgentCert []byte // Agent certificate PEM
	AgentKey  []byte // Agent private key PEM
	NodeID    uint
	NodeName  string
	NodeUUID  string
}

type certificateUsecase struct {
	certRepo      repository.AgentCertificateRepository
	nodeRepo      repository.NodeRepository
	onRevoke      func(ctx context.Context) // after any revoke: refresh watch, evict conns, push denylist
	sightedSerial func(nodeID uint) (string, bool)
}

// NewCertificateUsecase creates a new certificate usecase
func NewCertificateUsecase(certRepo repository.AgentCertificateRepository, nodeRepo repository.NodeRepository) CertificateUsecase {
	return &certificateUsecase{certRepo: certRepo, nodeRepo: nodeRepo}
}

// InitializeCA creates the CA certificate. Fails if one already exists.
func (u *certificateUsecase) InitializeCA(ctx context.Context, cfg *certs.CAConfig) (*domain.AgentCertificate, error) {
	if existing, err := u.certRepo.GetCA(ctx); err == nil && existing != nil {
		return nil, fmt.Errorf("CA already exists, use ReinitializeCA to create a new one")
	}
	caInfo, err := certs.GenerateCA(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA: %w", err)
	}
	parsed, err := certs.ParseCertificate(caInfo.Certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated CA: %w", err)
	}
	caCert := &domain.AgentCertificate{
		Type:         domain.CertTypeCA,
		CommonName:   parsed.Subject.CommonName,
		SerialNumber: parsed.SerialNumber.String(),
		Certificate:  caInfo.Certificate,
		PrivateKey:   caInfo.PrivateKey,
		NotBefore:    caInfo.NotBefore,
		NotAfter:     caInfo.NotAfter,
	}
	if err := u.certRepo.Create(ctx, caCert); err != nil {
		return nil, fmt.Errorf("failed to store CA certificate: %w", err)
	}
	logger.GetLogger().Info("[InitializeCA] CA certificate created successfully")
	return caCert, nil
}

func (u *certificateUsecase) GetCA(ctx context.Context) (*domain.AgentCertificate, error) {
	return u.certRepo.GetCA(ctx)
}

func (u *certificateUsecase) HasCA(ctx context.Context) bool {
	ca, err := u.certRepo.GetCA(ctx)
	return err == nil && ca != nil && !ca.IsExpired()
}

// ReinitializeCA deletes all certificates and creates a new CA
func (u *certificateUsecase) ReinitializeCA(ctx context.Context, cfg *certs.CAConfig) (*domain.AgentCertificate, error) {
	if err := u.certRepo.DeleteAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to delete existing certificates: %w", err)
	}
	logger.GetLogger().Info("[ReinitializeCA] All existing certificates deleted")
	return u.InitializeCA(ctx, cfg)
}

// GenerateAgentCert issues and records a node certificate. The private key is
// attached to the returned value once and never stored.
func (u *certificateUsecase) GenerateAgentCert(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}
	ca, err := u.certRepo.GetCA(ctx)
	if err != nil {
		return nil, fmt.Errorf("CA not found, initialize CA first: %w", err)
	}
	info, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID:      nodeID,
		NodeName:    node.Name,
		IPAddresses: []string{node.IP},
		DNSNames:    []string{node.Name},
		ValidMonths: 12,
	}, ca.Certificate, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate agent certificate: %w", err)
	}
	rec, err := u.RecordAgentCert(ctx, nodeID, info)
	if err != nil {
		return nil, err
	}
	rec.PrivateKey = info.PrivateKey
	logger.GetLogger().WithField("node_id", nodeID).Info("[GenerateAgentCert] Agent certificate created successfully")
	return rec, nil
}

// RecordAgentCert stores an issued node certificate without its key.
func (u *certificateUsecase) RecordAgentCert(ctx context.Context, nodeID uint, info *certs.CertificateInfo) (*domain.AgentCertificate, error) {
	parsed, err := certs.ParseCertificate(info.Certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse agent cert: %w", err)
	}
	rec := &domain.AgentCertificate{
		Type:         domain.CertTypeAgent,
		NodeID:       &nodeID,
		CommonName:   parsed.Subject.CommonName,
		SerialNumber: parsed.SerialNumber.String(),
		Certificate:  info.Certificate,
		NotBefore:    info.NotBefore,
		NotAfter:     info.NotAfter,
	}
	if err := u.certRepo.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("failed to store agent certificate: %w", err)
	}
	return rec, nil
}

func (u *certificateUsecase) GetAgentCert(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error) {
	return u.certRepo.GetByNodeID(ctx, nodeID)
}

func (u *certificateUsecase) ListNodeCerts(ctx context.Context, nodeID uint) ([]*domain.AgentCertificate, error) {
	return u.certRepo.ListByNode(ctx, nodeID)
}

// GetCertificateBundle issues a fresh node cert for a deploy. The previous
// cert stays valid until the node is seen presenting the new one.
func (u *certificateUsecase) GetCertificateBundle(ctx context.Context, nodeID uint) (*CertBundle, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}
	ca, err := u.certRepo.GetCA(ctx)
	if err != nil {
		return nil, fmt.Errorf("CA not found: %w", err)
	}
	agentCert, err := u.GenerateAgentCert(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate agent cert: %w", err)
	}
	return &CertBundle{
		CACert:    ca.Certificate,
		AgentCert: agentCert.Certificate,
		AgentKey:  agentCert.PrivateKey,
		NodeID:    nodeID,
		NodeName:  node.Name,
		NodeUUID:  node.UUID,
	}, nil
}

// GenerateEphemeralClientCert generates a temporary client certificate for the panel to use.
// It is signed by the CA but not stored in the database.
func (u *certificateUsecase) GenerateEphemeralClientCert(ctx context.Context) (*domain.AgentCertificate, error) {
	// Get CA
	ca, err := u.certRepo.GetCA(ctx)
	if err != nil {
		return nil, fmt.Errorf("CA not found: %w", err)
	}

	// certs.GenerateAgentCert directly (the usecase variant persists to DB).
	clientCertInfo, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID:      0, // Panel ID?
		NodeName:    "panel-client",
		IPAddresses: []string{"127.0.0.1"}, // Client IP usually doesn't matter for client auth verification unless enforced
		DNSNames:    []string{"panel"},
		ValidMonths: 1, // Short lived
	}, ca.Certificate, ca.PrivateKey)

	if err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral client cert: %w", err)
	}

	return &domain.AgentCertificate{
		Type:        domain.CertTypeMaster, // Treat as master/client
		Certificate: clientCertInfo.Certificate,
		PrivateKey:  clientCertInfo.PrivateKey,
		NotBefore:   clientCertInfo.NotBefore,
		NotAfter:    clientCertInfo.NotAfter,
	}, nil
}

func (u *certificateUsecase) SetOnRevokeCallback(fn func(ctx context.Context)) { u.onRevoke = fn }

func (u *certificateUsecase) SetSightingLookup(fn func(nodeID uint) (string, bool)) {
	u.sightedSerial = fn
}

// revokeIDs marks certificates revoked and fires the revoke callback once.
func (u *certificateUsecase) revokeIDs(ctx context.Context, ids []uint) (int, error) {
	n := 0
	for _, id := range ids {
		if err := u.certRepo.Revoke(ctx, id); err != nil {
			return n, err
		}
		n++
	}
	if n > 0 && u.onRevoke != nil {
		go u.onRevoke(context.Background())
	}
	return n, nil
}

func (u *certificateUsecase) RevokeCertificate(ctx context.Context, id uint) error {
	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if cert.Type == domain.CertTypeCA {
		return ErrCAProtected
	}
	_, err = u.revokeIDs(ctx, []uint{id})
	return err
}

// RevokeByAdmin revokes on an operator's request; a live node's current cert
// needs the node name typed as confirmation.
func (u *certificateUsecase) RevokeByAdmin(ctx context.Context, id uint, confirm string) error {
	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := u.requireConfirmForCurrent(ctx, cert, confirm); err != nil {
		return err
	}
	return u.RevokeCertificate(ctx, id)
}

// requireConfirmForCurrent guards the CA and the cert a live node relies on.
func (u *certificateUsecase) requireConfirmForCurrent(ctx context.Context, cert *domain.AgentCertificate, confirm string) error {
	if cert.Type == domain.CertTypeCA {
		return ErrCAProtected
	}
	if cert.Type != domain.CertTypeAgent || cert.NodeID == nil || cert.IsRevoked {
		return nil
	}
	node, err := u.nodeRepo.GetNode(ctx, *cert.NodeID)
	if err != nil || node == nil {
		return nil // orphan: no live node depends on it
	}
	current := false
	if newest, err := u.certRepo.GetByNodeID(ctx, node.ID); err == nil && newest.ID == cert.ID {
		current = true
	}
	if u.sightedSerial != nil {
		if s, ok := u.sightedSerial(node.ID); ok && s == cert.SerialNumber {
			current = true
		}
	}
	if current && confirm != node.Name {
		return ErrCertConfirmMismatch
	}
	return nil
}

// RevokeNodeCerts revokes every unrevoked cert issued to a node.
func (u *certificateUsecase) RevokeNodeCerts(ctx context.Context, nodeID uint) (int, error) {
	list, err := u.certRepo.ListByNode(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	var ids []uint
	for _, c := range list {
		if !c.IsRevoked {
			ids = append(ids, c.ID)
		}
	}
	return u.revokeIDs(ctx, ids)
}

// RetireSuperseded revokes a node's older certs once it presents its newest
// one. It never touches the cert the node was just seen with.
func (u *certificateUsecase) RetireSuperseded(ctx context.Context, nodeID uint, serial string) (int, error) {
	list, err := u.certRepo.ListByNode(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	var newest *domain.AgentCertificate
	for _, c := range list {
		if !c.IsRevoked && !c.DeletedAt.Valid {
			newest = c
			break
		}
	}
	if newest == nil || newest.SerialNumber != serial {
		return 0, nil
	}
	var ids []uint
	for _, c := range list {
		if c.ID != newest.ID && !c.IsRevoked {
			ids = append(ids, c.ID)
		}
	}
	return u.revokeIDs(ctx, ids)
}

func (u *certificateUsecase) RevokeOrphans(ctx context.Context) (int, error) {
	list, err := u.certRepo.ListOrphans(ctx)
	if err != nil {
		return 0, err
	}
	ids := make([]uint, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	return u.revokeIDs(ctx, ids)
}

func (u *certificateUsecase) PurgeRevokedExpired(ctx context.Context) (int64, error) {
	return u.certRepo.PurgeRevokedExpired(ctx)
}

// DeleteCertificate soft-deletes a certificate. A valid one is revoked first,
// so its serial stays on the denylist until it expires.
func (u *certificateUsecase) DeleteCertificate(ctx context.Context, id uint, confirm string) error {
	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := u.requireConfirmForCurrent(ctx, cert, confirm); err != nil {
		return err
	}
	if !cert.IsRevoked && !cert.IsExpired() {
		if _, err := u.revokeIDs(ctx, []uint{id}); err != nil {
			return err
		}
	}
	return u.certRepo.Delete(ctx, id)
}

func (u *certificateUsecase) ListRevokedSerialNumbers(ctx context.Context) ([]string, error) {
	return u.certRepo.ListRevokedSerialNumbers(ctx)
}

func (u *certificateUsecase) ListCertificates(ctx context.Context) ([]*domain.AgentCertificate, error) {
	return u.certRepo.ListAll(ctx)
}

func (u *certificateUsecase) ListExpiringSoon(ctx context.Context, days int) ([]*domain.AgentCertificate, error) {
	return u.certRepo.ListExpiringSoon(ctx, days)
}

func (u *certificateUsecase) GetCertificate(ctx context.Context, id uint) (*domain.AgentCertificate, error) {
	return u.certRepo.GetByID(ctx, id)
}

// RevealPrivateKey returns a stored key; the CA's needs its name typed.
func (u *certificateUsecase) RevealPrivateKey(ctx context.Context, id uint, confirm string) (string, error) {
	cert, err := u.certRepo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if len(cert.PrivateKey) == 0 {
		return "", ErrNoStoredKey
	}
	if cert.Type == domain.CertTypeCA && confirm != cert.CommonName {
		return "", ErrCertConfirmMismatch
	}
	return string(cert.PrivateKey), nil
}

func (u *certificateUsecase) SetExpiryNotifyLevel(ctx context.Context, id uint, level int) error {
	return u.certRepo.SetExpiryNotifyLevel(ctx, id, level)
}
