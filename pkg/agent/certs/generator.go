package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// clockSkewAllowance backdates NotBefore so a node whose clock trails the
// hub's still accepts a freshly issued certificate.
const clockSkewAllowance = time.Hour

// CertificateInfo holds generated certificate and key
type CertificateInfo struct {
	Certificate []byte // PEM-encoded certificate
	PrivateKey  []byte // PEM-encoded private key
	NotBefore   time.Time
	NotAfter    time.Time
}

// CAConfig holds configuration for CA certificate generation
type CAConfig struct {
	CommonName   string
	Organization string
	Country      string
	ValidYears   int
}

// DefaultCAConfig returns sensible CA defaults
func DefaultCAConfig() *CAConfig {
	return &CAConfig{
		CommonName:   "NasNet Agent CA",
		Organization: "NasNet",
		Country:      "US",
		ValidYears:   10,
	}
}

// GenerateCA creates a new Certificate Authority
func GenerateCA(cfg *CAConfig) (*CertificateInfo, error) {
	if cfg == nil {
		cfg = DefaultCAConfig()
	}

	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	notBefore := now.Add(-clockSkewAllowance)
	notAfter := now.AddDate(cfg.ValidYears, 0, 0)

	// Create CA certificate template
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: []string{cfg.Organization},
			Country:      []string{cfg.Country},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}

	// Self-sign the CA certificate
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	// Encode to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CA private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &CertificateInfo{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
	}, nil
}

// AgentCertConfig holds configuration for agent certificate generation
type AgentCertConfig struct {
	NodeID      uint
	NodeName    string
	IPAddresses []string // IP addresses to include in SAN
	DNSNames    []string // DNS names to include in SAN
	ValidMonths int
}

// GenerateAgentCert creates a certificate for a node agent signed by the CA
func GenerateAgentCert(cfg *AgentCertConfig, caCert, caKey []byte) (*CertificateInfo, error) {
	// Parse CA certificate
	caCertBlock, _ := pem.Decode(caCert)
	if caCertBlock == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	parsedCACert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse CA private key
	caKeyBlock, _ := pem.Decode(caKey)
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed to decode CA private key PEM")
	}
	parsedCAKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA private key: %w", err)
	}

	// Generate agent private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate agent private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	notBefore := now.Add(-clockSkewAllowance)
	validMonths := cfg.ValidMonths
	if validMonths == 0 {
		validMonths = 12 // Default 1 year
	}
	notAfter := now.AddDate(0, validMonths, 0)

	// Parse IP addresses
	var ipAddresses []net.IP
	for _, ipStr := range cfg.IPAddresses {
		if ip := net.ParseIP(ipStr); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		}
	}

	// Create agent certificate template
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("node-%d-%s", cfg.NodeID, cfg.NodeName),
			Organization: []string{"NasNet Agent"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           ipAddresses,
		DNSNames:              cfg.DNSNames,
	}

	// Sign with CA
	certDER, err := x509.CreateCertificate(rand.Reader, template, parsedCACert, &privateKey.PublicKey, parsedCAKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent certificate: %w", err)
	}

	// Encode to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &CertificateInfo{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
	}, nil
}

// MasterCertConfig holds configuration for master client certificate generation
type MasterCertConfig struct {
	CommonName  string
	ValidMonths int
}

// GenerateMasterCert creates a client certificate for the master to authenticate with agents
func GenerateMasterCert(cfg *MasterCertConfig, caCert, caKey []byte) (*CertificateInfo, error) {
	if cfg == nil {
		cfg = &MasterCertConfig{
			CommonName:  "nasnet-hub-master",
			ValidMonths: 12,
		}
	}

	// Parse CA certificate
	caCertBlock, _ := pem.Decode(caCert)
	if caCertBlock == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	parsedCACert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse CA private key
	caKeyBlock, _ := pem.Decode(caKey)
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed to decode CA private key PEM")
	}
	parsedCAKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA private key: %w", err)
	}

	// Generate master private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate master private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	notBefore := now.Add(-clockSkewAllowance)
	notAfter := now.AddDate(0, cfg.ValidMonths, 0)

	// Create master certificate template
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: []string{"NasNet Master"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Sign with CA
	certDER, err := x509.CreateCertificate(rand.Reader, template, parsedCACert, &privateKey.PublicKey, parsedCAKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create master certificate: %w", err)
	}

	// Encode to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal master private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &CertificateInfo{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
	}, nil
}

// VerifyCertificate validates a certificate against a CA
func VerifyCertificate(certPEM, caCertPEM []byte) error {
	// Parse certificate
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse CA certificate
	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return fmt.Errorf("failed to decode CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Create cert pool with CA
	certPool := x509.NewCertPool()
	certPool.AddCert(caCert)

	// Verify
	opts := x509.VerifyOptions{
		Roots: certPool,
	}

	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	return nil
}

// ParseCertificate parses a PEM-encoded certificate and returns info
func ParseCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}
