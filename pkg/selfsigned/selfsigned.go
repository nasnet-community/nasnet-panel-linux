// Package selfsigned keeps the HTTPS certificate a panel without a domain
// makes for itself. It is created once and stored with the panel's settings,
// so browsers that accepted it keep accepting it; it is renewed shortly
// before it runs out, and served through GetCertificate, so a new one takes
// effect without a restart.
package selfsigned

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

const (
	// Apple's platforms refuse server certificates valid for longer, even
	// ones a user accepts by hand.
	validity    = 825 * 24 * time.Hour
	renewBefore = 30 * 24 * time.Hour
	commonName  = "NasNet Panel"
)

// Store keeps the certificate and its key as one PEM blob.
type Store interface {
	// Load returns "" when nothing is stored yet.
	Load(ctx context.Context) (string, error)
	Save(ctx context.Context, pemBlob string) error
}

// Manager serves the stored certificate and replaces it when asked to or
// when it is about to run out.
type Manager struct {
	store Store
	names func() []string
	// onIssue hears about certificates the manager issued on its own:
	// reason is "created", "replaced" (the stored one was unreadable) or
	// "renewed". old is nil for "created".
	onIssue func(ctx context.Context, reason string, old, cur *x509.Certificate)

	mu  sync.Mutex // one issuer at a time
	cur atomic.Pointer[tls.Certificate]
}

// NewManager returns a manager that names its certificates with names() at
// the time each one is issued.
func NewManager(store Store, names func() []string, onIssue func(ctx context.Context, reason string, old, cur *x509.Certificate)) *Manager {
	return &Manager{store: store, names: names, onIssue: onIssue}
}

// Ensure loads the stored certificate, or issues one, and renews it when it
// runs out within 30 days. A stored certificate that can't be read is
// replaced rather than left to take HTTPS down.
func (m *Manager) Ensure(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var old *x509.Certificate
	reason := ""
	if m.cur.Load() == nil {
		raw, err := m.store.Load(ctx)
		if err != nil {
			return fmt.Errorf("reading the stored certificate: %w", err)
		}
		if raw == "" {
			reason = "created"
		} else if cert, err := parse(raw); err != nil {
			logger.GetLogger().WithError(err).Warn("Self-signed certificate: the stored one is unreadable, issuing a new one")
			reason = "replaced"
		} else {
			m.cur.Store(cert)
		}
	}
	if cur := m.cur.Load(); cur != nil {
		if time.Until(cur.Leaf.NotAfter) >= renewBefore {
			return nil
		}
		old, reason = cur.Leaf, "renewed"
	}
	issued, err := m.issue(ctx)
	if err != nil {
		return err
	}
	if m.onIssue != nil {
		m.onIssue(ctx, reason, old, issued)
	}
	return nil
}

// Regenerate replaces the certificate with a new one named after the
// machine's addresses now. It returns the old and the new certificate.
func (m *Manager) Regenerate(ctx context.Context) (old, cur *x509.Certificate, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.cur.Load(); c != nil {
		old = c.Leaf
	}
	cur, err = m.issue(ctx)
	return old, cur, err
}

func (m *Manager) issue(ctx context.Context) (*x509.Certificate, error) {
	certPEM, keyPEM, err := Generate(m.names(), time.Now())
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if err := m.store.Save(ctx, string(certPEM)+string(keyPEM)); err != nil {
		return nil, fmt.Errorf("storing the certificate: %w", err)
	}
	m.cur.Store(&cert)
	return cert.Leaf, nil
}

// StartRenewal checks once a day whether the certificate needs renewing.
func (m *Manager) StartRenewal(ctx context.Context) {
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := m.Ensure(ctx); err != nil {
					logger.GetLogger().WithError(err).Error("Self-signed certificate: renewal failed")
				}
			}
		}
	}()
}

// Active reports whether the manager has a certificate to serve.
func (m *Manager) Active() bool { return m != nil && m.cur.Load() != nil }

// Leaf returns the certificate being served, or nil.
func (m *Manager) Leaf() *x509.Certificate {
	if m == nil {
		return nil
	}
	if c := m.cur.Load(); c != nil {
		return c.Leaf
	}
	return nil
}

// TLSConfig serves whatever certificate is current at each handshake.
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if c := m.cur.Load(); c != nil {
				return c, nil
			}
			return nil, errors.New("no self-signed certificate")
		},
	}
}

func parse(raw string) (*tls.Certificate, error) {
	cert, err := tls.X509KeyPair([]byte(raw), []byte(raw))
	if err != nil {
		return nil, err
	}
	if cert.Leaf == nil {
		return nil, errors.New("no certificate in the stored PEM")
	}
	return &cert, nil
}

// Generate issues a P-256 certificate for names, which may be host names or
// IP addresses, valid from an hour before now.
func Generate(names []string, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	notBefore := now.Add(-time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{commonName}},
		NotBefore:             notBefore,
		NotAfter:              notBefore.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

// AuditJSON describes a certificate for an audit entry: its fingerprint,
// expiry and, when given, why it was issued. "" for nil.
func AuditJSON(c *x509.Certificate, reason string) string {
	if c == nil {
		return ""
	}
	v := map[string]string{"fingerprint": Fingerprint(c), "expires": c.NotAfter.UTC().Format(time.DateOnly)}
	if reason != "" {
		v["reason"] = reason
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Fingerprint is the certificate's SHA-256 fingerprint as browsers show it.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// SPKIPin is the key's hash in curl's --pinnedpubkey form.
func SPKIPin(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return "sha256//" + base64.StdEncoding.EncodeToString(sum[:])
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7e || s[i] < 0x21 {
			return false
		}
	}
	return true
}

// LocalNames is what a new certificate is issued for: localhost, the host
// name, the extra hosts given (the panel's configured URLs), and every
// address on the machine's interfaces apart from link-local ones.
func LocalNames(extra ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.Trim(strings.TrimSpace(n), "[]")
		// x509 takes ASCII names only; skip an internationalized one
		if n != "" && !seen[strings.ToLower(n)] && isASCII(n) {
			seen[strings.ToLower(n)] = true
			out = append(out, n)
		}
	}
	add("localhost")
	if h, err := os.Hostname(); err == nil {
		add(h)
	}
	for _, e := range extra {
		add(e)
	}
	add("127.0.0.1")
	add("::1")
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.IsLinkLocalUnicast() || ipNet.IP.IsLoopback() {
				continue
			}
			add(ipNet.IP.String())
		}
	}
	return out
}
