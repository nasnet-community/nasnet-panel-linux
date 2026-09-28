package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/certwatch"
	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	"github.com/sirupsen/logrus"
)

// hubTLSRecheck is how long a built hub TLS config is reused before the CA
// and the advertised host are read again.
const hubTLSRecheck = 30 * time.Second

// hubTLS serves the reverse-mode hub with a server certificate minted from
// the hub CA on demand, so creating the CA (or changing agent_hub_url) takes
// effect without a panel restart. With no CA every handshake is refused;
// there is no plaintext mode.
type hubTLS struct {
	certUC   nodeUC.CertificateUsecase
	settings settingDomain.SettingUsecase
	watch    *certwatch.Watch
	baseURL  string // env fallback when app_base_url is unset
	log      *logrus.Logger

	mu        sync.Mutex
	cfg       *tls.Config
	caSerial  string
	host      string
	notAfter  time.Time
	checkedAt time.Time
	warnedAt  time.Time
}

// Config is the listener's config; each handshake picks the current one.
func (h *hubTLS) Config() *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { return h.current() },
	}
}

func (h *hubTLS) current() (*tls.Config, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg != nil && time.Since(h.checkedAt) < hubTLSRecheck {
		return h.cfg, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ca, err := h.certUC.GetCA(ctx)
	if err != nil || ca == nil || len(ca.Certificate) == 0 || len(ca.PrivateKey) == 0 {
		if time.Since(h.warnedAt) > time.Minute {
			h.warnedAt = time.Now()
			h.log.Warn("Hub: refusing a node connection, there is no certificate authority yet (create it on the Certificates page)")
		}
		return nil, errors.New("hub has no certificate authority")
	}
	host := h.advertisedHost(ctx)
	if h.cfg != nil && ca.SerialNumber == h.caSerial && host == h.host && time.Until(h.notAfter) > 30*24*time.Hour {
		h.checkedAt = time.Now()
		return h.cfg, nil
	}

	cfg, notAfter, err := h.build(ca.Certificate, ca.PrivateKey, host)
	if err != nil {
		h.log.WithError(err).Error("Hub: failed to build the hub certificate")
		return nil, err
	}
	h.cfg, h.caSerial, h.host, h.notAfter, h.checkedAt = cfg, ca.SerialNumber, host, notAfter, time.Now()
	h.log.WithField("host", host).Info("Hub: serving a hub certificate from the current CA")
	return cfg, nil
}

// advertisedHost is the host remote nodes dial: agent_hub_url when set,
// else the base URL.
func (h *hubTLS) advertisedHost(ctx context.Context) string {
	for _, key := range []string{"agent_hub_url", "app_base_url"} {
		if v, err := h.settings.GetByKey(ctx, key); err == nil {
			if host := hostOf(v); host != "" {
				return host
			}
		}
	}
	return hostOf(h.baseURL)
}

// hostOf takes "https://host:port/path", "host:port" or "host".
func hostOf(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.Contains(v, "://") {
		if u, err := url.Parse(v); err == nil {
			return u.Hostname()
		}
		return ""
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(v, "[]")
}

func (h *hubTLS) build(caPEM, caKey []byte, host string) (*tls.Config, time.Time, error) {
	var ips, dns []string
	if host != "" {
		// Agents dialing an IP only accept it from the IP SANs.
		if net.ParseIP(host) != nil {
			ips = append(ips, host)
		} else {
			dns = append(dns, host)
		}
	}
	info, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID:      0,
		NodeName:    "hub-server",
		IPAddresses: ips,
		DNSNames:    dns,
		ValidMonths: 12,
	}, caPEM, caKey)
	if err != nil {
		return nil, time.Time{}, err
	}
	pair, err := tls.X509KeyPair(info.Certificate, info.PrivateKey)
	if err != nil {
		return nil, time.Time{}, err
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	w := h.watch
	return &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
		// Runs on resumed sessions too; refuses revoked certs in strict mode
		// before any stream opens.
		VerifyConnection: func(cs tls.ConnectionState) error {
			if w == nil || len(cs.PeerCertificates) == 0 {
				return nil
			}
			return w.CheckRevoked(cs.PeerCertificates[0])
		},
	}, info.NotAfter, nil
}
