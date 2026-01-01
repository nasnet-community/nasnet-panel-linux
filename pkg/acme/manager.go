package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// Challenge types
const (
	ChallengeHTTP01 = "http-01"
	ChallengeDNS01  = "dns-01"
)

// Let's Encrypt directory URLs
const (
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
	LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
)

// CertResult holds the result of a certificate issuance
type CertResult struct {
	Certificate []byte
	PrivateKey  []byte
	NotBefore   time.Time
	ExpiresAt   time.Time
}

// DNS01Challenge holds info needed for DNS-01 verification
type DNS01Challenge struct {
	Domain       string
	TXTRecord    string // _acme-challenge.domain
	TXTValue     string // value to set in the _acme-challenge TXT record
	orderURL     string // Internal: order URL for completion
	authzURL     string // Internal: authorization URL
	challengeURL string // Internal: challenge URL
}

// CertManager manages ACME certificate operations
type CertManager struct {
	email      string
	staging    bool
	cacheDir   string
	client     *acme.Client
	accountKey crypto.Signer

	// For DNS-01 pending challenges
	pendingDNS map[string]*DNS01Challenge

	// For HTTP-01 active challenges (token -> keyAuth)
	httpChallenges map[string]string

	// Server TLS certificate (dynamically served)
	serverCert *tls.Certificate

	mu sync.RWMutex
}

// NewCertManager: nil httpClient = Go default. Pass a Factory-derived
// client for the outbound-proxy path. DNS-01 TXT/A still hit the system
// resolver — not proxyable via SOCKS5.
func NewCertManager(email, cacheDir string, staging bool, httpClient *http.Client) (*CertManager, error) {
	if email == "" {
		return nil, errors.New("email is required for Let's Encrypt account")
	}
	if cacheDir == "" {
		cacheDir = "/tmp/acme"
	}

	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	m := &CertManager{
		email:          email,
		staging:        staging,
		cacheDir:       cacheDir,
		pendingDNS:     make(map[string]*DNS01Challenge),
		httpChallenges: make(map[string]string),
	}

	// Load or create account key
	if err := m.loadOrCreateAccountKey(); err != nil {
		return nil, fmt.Errorf("failed to setup account key: %w", err)
	}

	// Create ACME client
	directoryURL := LetsEncryptProduction
	if staging {
		directoryURL = LetsEncryptStaging
	}

	m.client = &acme.Client{
		Key:          m.accountKey,
		DirectoryURL: directoryURL,
		HTTPClient:   httpClient,
	}

	return m, nil
}

// IsStaging returns true if using Let's Encrypt staging
func (m *CertManager) IsStaging() bool {
	return m.staging
}

// IssueWithHTTP01 gets a cert via HTTP-01. Needs port 80 available.
func (m *CertManager) IssueWithHTTP01(ctx context.Context, domain string) (*CertResult, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, errors.New("domain is required")
	}

	// Ensure account is registered
	if err := m.ensureAccount(ctx); err != nil {
		return nil, fmt.Errorf("failed to register account: %w", err)
	}

	// Create order
	order, err := m.client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Start temporary port 80 server for challenge responses
	shutdownSrv := m.startChallengeServer()
	defer shutdownSrv()

	// Process authorizations
	for _, authzURL := range order.AuthzURLs {
		if err := m.processHTTP01Auth(ctx, authzURL); err != nil {
			return nil, err
		}
	}

	// Finalize order
	return m.finalizeOrder(ctx, order, domain)
}

// startChallengeServer starts a temporary HTTP server on :80 to serve ACME
// HTTP-01 challenge responses. This is needed when the panel runs on a
// non-standard port and no reverse proxy forwards port 80.
// The listener is bound synchronously so it is ready before returning.
func (m *CertManager) startChallengeServer() func() {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/acme-challenge/", func(w http.ResponseWriter, r *http.Request) {
		token := path.Base(r.URL.Path)
		if key, ok := m.GetHTTPChallengeKey(token); ok {
			w.Write([]byte(key))
		} else {
			http.NotFound(w, r)
		}
	})

	// Bind synchronously so we know the port is ready before returning.
	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		log.Printf("[acme] warning: could not listen on :80: %v (challenge may still work via reverse proxy)", err)
		return func() {} // no-op shutdown
	}

	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[acme] warning: challenge server on :80 error: %v", err)
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}
}

// processHTTP01Auth handles a single HTTP-01 authorization with proper cleanup
func (m *CertManager) processHTTP01Auth(ctx context.Context, authzURL string) error {
	authz, err := m.client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("failed to get authorization: %w", err)
	}

	if authz.Status == acme.StatusValid {
		return nil
	}

	// Find HTTP-01 challenge
	var challenge *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == "http-01" {
			challenge = c
			break
		}
	}
	if challenge == nil {
		return errors.New("http-01 challenge not available")
	}

	// Get challenge token and response
	token := challenge.Token
	keyAuth, err := m.client.HTTP01ChallengeResponse(challenge.Token)
	if err != nil {
		return fmt.Errorf("failed to get challenge response: %w", err)
	}

	// Store challenge response for the HTTP handler to serve
	m.mu.Lock()
	m.httpChallenges[token] = keyAuth
	m.mu.Unlock()

	// Cleanup when this function returns (defer is scoped to this function, not a loop)
	defer func() {
		m.mu.Lock()
		delete(m.httpChallenges, token)
		m.mu.Unlock()
	}()

	// Accept the challenge
	_, err = m.client.Accept(ctx, challenge)
	if err != nil {
		return fmt.Errorf("failed to accept challenge: %w", err)
	}

	// Wait for authorization
	authz, err = m.client.WaitAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}
	if authz.Status != acme.StatusValid {
		return fmt.Errorf("authorization status: %s", authz.Status)
	}

	return nil
}

// GetHTTPChallengeKey returns the key authorization for a given token
func (m *CertManager) GetHTTPChallengeKey(token string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.httpChallenges[token]
	return key, ok
}

// GetDNS01Challenge starts a DNS-01 challenge and returns the TXT record info
func (m *CertManager) GetDNS01Challenge(ctx context.Context, domain string) (*DNS01Challenge, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, errors.New("domain is required")
	}

	if err := m.ensureAccount(ctx); err != nil {
		return nil, fmt.Errorf("failed to register account: %w", err)
	}

	order, err := m.client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	for _, authzURL := range order.AuthzURLs {
		authz, err := m.client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get authorization: %w", err)
		}

		if authz.Status == acme.StatusValid {
			continue
		}

		var challenge *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "dns-01" {
				challenge = c
				break
			}
		}
		if challenge == nil {
			return nil, errors.New("dns-01 challenge not available")
		}

		// Get DNS record value
		txtValue, err := m.client.DNS01ChallengeRecord(challenge.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to get DNS record: %w", err)
		}

		dnsChallenge := &DNS01Challenge{
			Domain:       domain,
			TXTRecord:    "_acme-challenge." + domain,
			TXTValue:     txtValue,
			orderURL:     order.URI,
			authzURL:     authzURL,
			challengeURL: challenge.URI,
		}

		// Store pending challenge
		m.mu.Lock()
		m.pendingDNS[domain] = dnsChallenge
		m.mu.Unlock()

		return dnsChallenge, nil
	}

	return nil, errors.New("no pending authorization found")
}

// CompleteDNS01 completes a DNS-01 challenge after user has added TXT record
func (m *CertManager) CompleteDNS01(ctx context.Context, domain string) (*CertResult, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))

	m.mu.RLock()
	pending, ok := m.pendingDNS[domain]
	m.mu.RUnlock()

	if !ok {
		return nil, errors.New("no pending DNS-01 challenge for this domain")
	}

	// Clean up pending on exit
	defer func() {
		m.mu.Lock()
		delete(m.pendingDNS, domain)
		m.mu.Unlock()
	}()

	// Verify TXT record is present
	if err := m.verifyDNSRecord(pending.TXTRecord, pending.TXTValue); err != nil {
		return nil, fmt.Errorf("DNS verification failed: %w", err)
	}

	// Accept the challenge
	_, err := m.client.Accept(ctx, &acme.Challenge{URI: pending.challengeURL})
	if err != nil {
		return nil, fmt.Errorf("failed to accept challenge: %w", err)
	}

	// Wait for authorization
	authz, err := m.client.WaitAuthorization(ctx, pending.authzURL)
	if err != nil {
		return nil, fmt.Errorf("authorization failed: %w", err)
	}
	if authz.Status != acme.StatusValid {
		return nil, fmt.Errorf("authorization status: %s", authz.Status)
	}

	// Get order
	order, err := m.client.GetOrder(ctx, pending.orderURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	// Finalize order
	return m.finalizeOrder(ctx, order, domain)
}

// HasPendingChallenge checks if there's a pending DNS-01 challenge for a domain
func (m *CertManager) HasPendingChallenge(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.pendingDNS[strings.ToLower(domain)]
	return ok
}

// GetPendingChallenge retrieves pending DNS-01 challenge info
func (m *CertManager) GetPendingChallenge(domain string) (*DNS01Challenge, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.pendingDNS[strings.ToLower(domain)]
	return c, ok
}

// finalizeOrder generates CSR, finalizes order, and returns certificate
func (m *CertManager) finalizeOrder(ctx context.Context, order *acme.Order, domain string) (*CertResult, error) {
	// Generate private key for certificate
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate key: %w", err)
	}

	// Create CSR
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		DNSNames: []string{domain},
	}, certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	// Wait for order to be ready
	if order.Status != acme.StatusReady {
		order, err = m.client.WaitOrder(ctx, order.URI)
		if err != nil {
			return nil, fmt.Errorf("failed waiting for order: %w", err)
		}
	}

	// Finalize order
	der, _, err := m.client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Parse certificate to get expiry
	cert, err := x509.ParseCertificate(der[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Encode certificate chain to PEM
	var certPEM []byte
	for _, d := range der {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: d,
		})...)
	}

	// Encode private key to PEM
	keyBytes, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	return &CertResult{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		NotBefore:   cert.NotBefore,
		ExpiresAt:   cert.NotAfter,
	}, nil
}

// ensureAccount registers account if needed
func (m *CertManager) ensureAccount(ctx context.Context) error {
	acct := &acme.Account{
		Contact: []string{"mailto:" + m.email},
	}

	_, err := m.client.Register(ctx, acct, acme.AcceptTOS)
	if err != nil && err != acme.ErrAccountAlreadyExists {
		return err
	}
	return nil
}

// loadOrCreateAccountKey loads existing account key or creates new one
func (m *CertManager) loadOrCreateAccountKey() error {
	keyPath := filepath.Join(m.cacheDir, "account.key")

	// Try to load existing key
	data, err := os.ReadFile(keyPath)
	if err == nil {
		block, _ := pem.Decode(data)
		if block != nil && block.Type == "EC PRIVATE KEY" {
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err == nil {
				m.accountKey = key
				return nil
			}
		}
	}

	// Generate new key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	// Save key
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return err
	}

	m.accountKey = key
	return nil
}

// verifyDNSRecord checks if the TXT record is correctly set
func (m *CertManager) verifyDNSRecord(txtRecord, expectedValue string) error {
	records, err := net.LookupTXT(txtRecord)
	if err != nil {
		return fmt.Errorf("failed to lookup TXT record: %w", err)
	}

	for _, r := range records {
		if r == expectedValue {
			return nil
		}
	}

	return fmt.Errorf("TXT record not found or incorrect. Expected: %s", expectedValue)
}

// EnsureServerCert loads a cached server certificate or issues a new one via HTTP-01.
// The resulting tls.Certificate is stored in the manager for dynamic TLS serving.
func (m *CertManager) EnsureServerCert(ctx context.Context, domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return errors.New("domain is required")
	}

	serverDir := filepath.Join(m.cacheDir, "server")
	if err := os.MkdirAll(serverDir, 0700); err != nil {
		return fmt.Errorf("failed to create server cert cache dir: %w", err)
	}

	certPath := filepath.Join(serverDir, domain+".crt")
	keyPath := filepath.Join(serverDir, domain+".key")

	// Try loading cached cert
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		// Check expiry — renew if less than 30 days remaining
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
			if time.Until(leaf.NotAfter) > 30*24*time.Hour {
				log.Printf("[acme] loaded cached server cert for %s (expires %s)", domain, leaf.NotAfter.Format(time.DateOnly))
				m.mu.Lock()
				m.serverCert = &cert
				m.mu.Unlock()
				return nil
			}
			log.Printf("[acme] cached cert for %s expires soon (%s), renewing", domain, leaf.NotAfter.Format(time.DateOnly))
		}
	}

	// Issue new cert
	log.Printf("[acme] issuing server cert for %s via HTTP-01", domain)
	result, err := m.IssueWithHTTP01(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to issue server cert: %w", err)
	}

	// Cache to disk
	if err := os.WriteFile(certPath, result.Certificate, 0600); err != nil {
		return fmt.Errorf("failed to cache server cert: %w", err)
	}
	if err := os.WriteFile(keyPath, result.PrivateKey, 0600); err != nil {
		return fmt.Errorf("failed to cache server key: %w", err)
	}

	// Load as tls.Certificate
	cert, err := tls.X509KeyPair(result.Certificate, result.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to parse issued cert: %w", err)
	}

	m.mu.Lock()
	m.serverCert = &cert
	m.mu.Unlock()

	log.Printf("[acme] server cert issued for %s (expires %s)", domain, result.ExpiresAt.Format(time.DateOnly))
	return nil
}

// ServerTLSConfig returns a *tls.Config that dynamically serves the current server certificate.
func (m *CertManager) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			m.mu.RLock()
			cert := m.serverCert
			m.mu.RUnlock()
			if cert == nil {
				return nil, errors.New("no server certificate available")
			}
			return cert, nil
		},
	}
}

// StartServerCertRenewal runs a background goroutine that checks every 12 hours
// and renews the server certificate if it expires within 30 days.
func (m *CertManager) StartServerCertRenewal(ctx context.Context, domain string) {
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.mu.RLock()
				cert := m.serverCert
				m.mu.RUnlock()

				if cert == nil {
					continue
				}
				leaf, err := x509.ParseCertificate(cert.Certificate[0])
				if err != nil {
					log.Printf("[acme] renewal: failed to parse current cert: %v", err)
					continue
				}
				if time.Until(leaf.NotAfter) > 30*24*time.Hour {
					continue
				}
				log.Printf("[acme] renewal: cert for %s expires %s, renewing", domain, leaf.NotAfter.Format(time.DateOnly))
				renewCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				if err := m.EnsureServerCert(renewCtx, domain); err != nil {
					log.Printf("[acme] renewal: failed to renew cert for %s: %v", domain, err)
				} else {
					log.Printf("[acme] renewal: successfully renewed cert for %s", domain)
				}
				cancel()
			}
		}
	}()
}

// ValidateDomainIP checks if domain resolves to expected IP
func (m *CertManager) ValidateDomainIP(domain, expectedIP string) error {
	ips, err := net.LookupIP(domain)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}

	for _, ip := range ips {
		if ip.String() == expectedIP {
			return nil
		}
	}

	return fmt.Errorf("domain does not point to expected IP %s (resolved: %v)", expectedIP, ips)
}
