package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestResolveStartupConfigUsesSavedCredentialsAndTLS(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("saved-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, envHash := range []string{"", "$2a$10"} {
		cfg := &config.Config{
			Admin: config.AdminConfig{Username: "admin", PasswordHash: envHash},
			App:   config.AppConfig{TLSCertFile: "env.crt"},
			ACME:  config.ACMEConfig{Enabled: true, Email: "outdated"},
		}
		saved := map[string]string{
			"admin_password_hash": string(hash),
			"tls_key_file":        "saved.key", "tls_cert_file": "saved.crt",
			"acme_enabled": "false", "acme_email": "saved@example.com", "acme_staging": "true",
		}
		if err := resolveStartupConfig(cfg, startupSettings(saved)); err != nil {
			t.Fatal(err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte("saved-password")); err != nil {
			t.Fatalf("login must use the saved credential: %v", err)
		}
		if cfg.App.TLSKeyFile != "saved.key" || cfg.App.TLSCertFile != "saved.crt" || cfg.ACME.Enabled || !cfg.ACME.Staging || cfg.ACME.Email != "saved@example.com" {
			t.Fatal("saved TLS settings were not resolved before validation")
		}
	}
}

func TestResolveStartupConfigRejectsUnusableCredentials(t *testing.T) {
	for _, value := range []string{"", "$2a$10"} {
		cfg := &config.Config{Admin: config.AdminConfig{Username: "admin", PasswordHash: value}}
		if err := resolveStartupConfig(cfg, startupSettings(nil)); err == nil || !strings.Contains(err.Error(), "single quotes") {
			t.Fatalf("unusable initial credentials must stop startup, got %v", err)
		}
	}
	err := resolveStartupConfig(&config.Config{}, func(string) (string, error) { return "", errors.New("db unavailable") })
	if err == nil {
		t.Fatal("must not ignore failure to read the current credentials")
	}
}

func startupSettings(settings map[string]string) func(string) (string, error) {
	return func(key string) (string, error) {
		if value, ok := settings[key]; ok {
			return value, nil
		}
		return "", gorm.ErrRecordNotFound
	}
}

func TestPrepareServerTLSManualCertificateServesHTTPS(t *testing.T) {
	certFile, keyFile, roots := startupCertificate(t)
	cfg := &config.Config{App: config.AppConfig{TLSCertFile: certFile, TLSKeyFile: keyFile}}
	tlsConfig, err := prepareServerTLS(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.TLS = tlsConfig
	server.StartTLS()
	t.Cleanup(server.Close)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "panel.example.com"}}}
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.TLS == nil {
		t.Fatalf("expected HTTPS response, got %v", response.Status)
	}
}

func TestPrepareServerTLSNeverDowngradesRequestedTLS(t *testing.T) {
	for name, cfg := range map[string]*config.Config{
		"certificate only": {App: config.AppConfig{TLSCertFile: "panel.crt"}},
		"key only":         {App: config.AppConfig{TLSKeyFile: "panel.key"}},
		"missing files":    {App: config.AppConfig{TLSCertFile: "missing.crt", TLSKeyFile: "missing.key"}},
		"missing manager":  {ACME: config.ACMEConfig{Enabled: true}, App: config.AppConfig{BaseURL: "https://panel.example.com"}},
	} {
		t.Run(name, func(t *testing.T) {
			if result, err := prepareServerTLS(context.Background(), cfg, nil); err == nil || result != nil {
				t.Fatalf("requested TLS must fail instead of selecting HTTP: config=%v err=%v", result, err)
			}
		})
	}
	manager := &fakeStartupACME{err: errors.New("challenge failed")}
	cfg := &config.Config{ACME: config.ACMEConfig{Enabled: true}, App: config.AppConfig{BaseURL: "https://panel.example.com"}}
	if result, err := prepareServerTLS(context.Background(), cfg, manager); err == nil || result != nil || manager.renewed {
		t.Fatalf("ACME failure must abort TLS startup: config=%v err=%v renewed=%v", result, err, manager.renewed)
	}
	for _, baseURL := range []string{"", "https://", "://invalid", "http://panel.example.com"} {
		cfg.App.BaseURL = baseURL
		if _, err := prepareServerTLS(context.Background(), cfg, &fakeStartupACME{}); err == nil {
			t.Fatalf("ACME accepted invalid public HTTPS URL %q", baseURL)
		}
	}
}

func TestPrepareServerTLSAllowsReverseProxyAndACME(t *testing.T) {
	cfg := &config.Config{App: config.AppConfig{BaseURL: "https://panel.example.com"}}
	if result, err := prepareServerTLS(context.Background(), cfg, nil); err != nil || result != nil {
		t.Fatalf("external HTTPS proxy must allow local HTTP: config=%v err=%v", result, err)
	}
	cfg.ACME.Enabled = true
	manager := &fakeStartupACME{config: &tls.Config{MinVersion: tls.VersionTLS12}}
	if result, err := prepareServerTLS(context.Background(), cfg, manager); err != nil || result != manager.config || !manager.renewed || manager.domain != "panel.example.com" {
		t.Fatalf("ACME TLS configuration not installed: config=%v err=%v", result, err)
	}
}

func TestInitACMEManagerFailsForInvalidSetup(t *testing.T) {
	for _, email := range []string{"", "not-an-email"} {
		if manager, err := initACMEManager(&config.Config{ACME: config.ACMEConfig{Enabled: true, Email: email}}, nil); err == nil || manager != nil {
			t.Fatalf("invalid ACME email must abort startup, got %v", err)
		}
	}
	cacheFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(cacheFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if manager, err := initACMEManager(&config.Config{ACME: config.ACMEConfig{Enabled: true, Email: "admin@example.com", CacheDir: cacheFile}}, nil); err == nil || manager != nil {
		t.Fatalf("failed ACME initialization must abort startup, got %v", err)
	}
}

func TestStartupManualTLSWinsOverUnavailableACME(t *testing.T) {
	certFile, keyFile, _ := startupCertificate(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("saved-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Admin: config.AdminConfig{Username: "admin"},
		App:   config.AppConfig{TLSCertFile: certFile, BaseURL: "https://panel.example.com"},
		ACME:  config.ACMEConfig{Enabled: true},
	}
	// The saved key completes a partial environment configuration. The saved
	// password also permits startup without an environment password hash.
	if err := resolveStartupConfig(cfg, startupSettings(map[string]string{
		"tls_key_file": keyFile, "admin_password_hash": string(hash),
	})); err != nil {
		t.Fatal(err)
	}
	manager, err := initACMEManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tlsConfig, err := prepareServerTLS(context.Background(), cfg, manager); err != nil || tlsConfig == nil || len(tlsConfig.Certificates) != 1 {
		t.Fatalf("manual HTTPS must survive unavailable ACME: config=%v err=%v", tlsConfig, err)
	}
	cfg.App.TLSCertFile = "missing.crt"
	if tlsConfig, err := prepareServerTLS(context.Background(), cfg, manager); err == nil || tlsConfig != nil {
		t.Fatal("an unusable manual certificate must never downgrade to HTTP")
	}
}

type fakeStartupACME struct {
	err     error
	config  *tls.Config
	domain  string
	renewed bool
}

func (m *fakeStartupACME) EnsureServerCert(_ context.Context, domain string) error {
	m.domain = domain
	return m.err
}
func (m *fakeStartupACME) ServerTLSConfig() *tls.Config                   { return m.config }
func (m *fakeStartupACME) StartServerCertRenewal(context.Context, string) { m.renewed = true }

func startupCertificate(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "panel.example.com"},
		DNSNames: []string{"panel.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "panel.crt"), filepath.Join(dir, "panel.key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("cannot trust test certificate")
	}
	return certFile, keyFile, roots
}
