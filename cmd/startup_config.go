package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/config"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/acme"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
)

// Resolve persisted settings before validating credentials or local TLS. A
// missing/empty setting retains the environment value, except stored booleans.
func resolveStartupConfig(cfg *config.Config, getSetting func(string) (string, error)) error {
	for _, setting := range []struct {
		key string
		dst *string
	}{
		{"admin_password_hash", &cfg.Admin.PasswordHash},
		{"acme_email", &cfg.ACME.Email},
	} {
		value, err := getSetting(setting.key)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to read startup setting %s", setting.key)
		}
		if err == nil && value != "" {
			*setting.dst = value
		}
	}
	for _, setting := range []struct {
		key string
		dst *bool
	}{
		{"acme_enabled", &cfg.ACME.Enabled},
		{"acme_staging", &cfg.ACME.Staging},
	} {
		value, err := getSetting(setting.key)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to read startup setting %s", setting.key)
		}
		if err == nil {
			*setting.dst = value == "true"
		}
	}
	if err := cfg.Admin.Validate(); err != nil {
		return err
	}
	return resolveStartupTLSFiles(cfg, getSetting)
}

func resolveStartupTLSFiles(cfg *config.Config, getSetting func(string) (string, error)) error {
	for _, setting := range []struct {
		key string
		dst *string
	}{
		{"tls_cert_file", &cfg.App.TLSCertFile},
		{"tls_key_file", &cfg.App.TLSKeyFile},
	} {
		value, err := getSetting(setting.key)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to read startup setting %s", setting.key)
		}
		if err == nil && value != "" {
			*setting.dst = value
		}
	}
	return nil
}

func initACMEManager(cfg *config.Config, client *http.Client) (*acme.CertManager, error) {
	if !cfg.ACME.Enabled {
		return nil, nil
	}
	var manager *acme.CertManager
	var err error
	if strings.TrimSpace(cfg.ACME.Email) == "" || !strings.Contains(cfg.ACME.Email, "@") {
		err = errors.New("ACME_EMAIL is required and must be an email address when ACME is enabled")
	} else {
		manager, err = acme.NewCertManager(cfg.ACME.Email, cfg.ACME.CacheDir, cfg.ACME.Staging, client)
	}
	if err != nil {
		// Manual panel certificates have priority. Keep ACME available for
		// inbound certificates when possible, but an unused ACME failure must
		// not prevent the panel from serving its valid manual certificate.
		if cfg.App.TLSCertFile != "" && cfg.App.TLSKeyFile != "" {
			logger.GetLogger().WithError(err).Warn("ACME unavailable; panel TLS will use the configured certificate files")
			return nil, nil
		}
		return nil, fmt.Errorf("failed to initialize ACME: %w", err)
	}
	return manager, nil
}

type serverCertificateManager interface {
	EnsureServerCert(context.Context, string) error
	ServerTLSConfig() *tls.Config
	StartServerCertRenewal(context.Context, string)
}

// A nil config with no error means local HTTP was explicitly left available.
// APP_BASE_URL alone does not request local TLS: a reverse proxy may own HTTPS.
// Once manual certificates or ACME request local TLS, failures must stop boot.
func prepareServerTLS(ctx context.Context, cfg *config.Config, manager serverCertificateManager) (*tls.Config, error) {
	certFile, keyFile := cfg.App.TLSCertFile, cfg.App.TLSKeyFile
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("local TLS requires both TLS_CERT_FILE and TLS_KEY_FILE (or their saved panel settings)")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load panel TLS certificate and key: %w", err)
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
	}
	if !cfg.ACME.Enabled {
		return nil, nil
	}
	if manager == nil {
		return nil, errors.New("local TLS requested but the ACME certificate manager is unavailable")
	}
	parsed, err := url.Parse(cfg.App.BaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, errors.New("APP_BASE_URL must be an HTTPS URL with a hostname when ACME is enabled")
	}
	domain := parsed.Hostname()
	if err := manager.EnsureServerCert(ctx, domain); err != nil {
		return nil, fmt.Errorf("failed to obtain panel TLS certificate: %w", err)
	}
	tlsConfig := manager.ServerTLSConfig()
	if tlsConfig == nil {
		return nil, errors.New("ACME did not provide a TLS configuration")
	}
	manager.StartServerCertRenewal(ctx, domain)
	return tlsConfig, nil
}
