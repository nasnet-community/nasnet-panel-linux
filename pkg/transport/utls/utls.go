// Package utls provides uTLS-based transport credentials for gRPC connections.
// This enables TLS fingerprint mimicry to make traffic appear as legitimate
// browser connections (Chrome, Firefox, Safari, etc.) to bypass DPI detection.
package utls

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"sync"

	utls "github.com/refraction-networking/utls"
	"google.golang.org/grpc/credentials"
)

// Fingerprint represents a TLS client fingerprint to mimic
type Fingerprint string

const (
	// Browser fingerprints
	FingerprintChrome     Fingerprint = "chrome"
	FingerprintFirefox    Fingerprint = "firefox"
	FingerprintSafari     Fingerprint = "safari"
	FingerprintEdge       Fingerprint = "edge"
	FingerprintIOS        Fingerprint = "ios"
	FingerprintAndroid    Fingerprint = "android"
	FingerprintRandomized Fingerprint = "randomized"
	FingerprintRandom     Fingerprint = "random" // Randomly selects from common fingerprints
	FingerprintDefault    Fingerprint = ""       // Use Go's default TLS (no uTLS)
)

// commonFingerprints is the list of fingerprints used for random selection
var commonFingerprints = []Fingerprint{
	FingerprintChrome,
	FingerprintFirefox,
	FingerprintSafari,
	FingerprintEdge,
}

// fingerprintToHelloID maps fingerprint names to uTLS ClientHelloID
var fingerprintToHelloID = map[Fingerprint]utls.ClientHelloID{
	FingerprintChrome:     utls.HelloChrome_Auto,
	FingerprintFirefox:    utls.HelloFirefox_Auto,
	FingerprintSafari:     utls.HelloSafari_Auto,
	FingerprintEdge:       utls.HelloEdge_Auto,
	FingerprintIOS:        utls.HelloIOS_Auto,
	FingerprintAndroid:    utls.HelloAndroid_11_OkHttp,
	FingerprintRandomized: utls.HelloRandomized,
}

// GetHelloID converts a fingerprint string to a uTLS ClientHelloID
func GetHelloID(fp Fingerprint) utls.ClientHelloID {
	if fp == FingerprintRandom {
		// Pick a random fingerprint from common ones
		randomFP := commonFingerprints[rand.Intn(len(commonFingerprints))]
		return fingerprintToHelloID[randomFP]
	}
	if id, ok := fingerprintToHelloID[fp]; ok {
		return id
	}
	// Default to Chrome if unknown
	return utls.HelloChrome_Auto
}

// IsValidFingerprint checks if a fingerprint string is valid
func IsValidFingerprint(fp string) bool {
	switch Fingerprint(fp) {
	case FingerprintChrome, FingerprintFirefox, FingerprintSafari,
		FingerprintEdge, FingerprintIOS, FingerprintAndroid,
		FingerprintRandomized, FingerprintRandom, FingerprintDefault:
		return true
	default:
		return false
	}
}

// TransportCredentials implements grpc credentials.TransportCredentials using uTLS
type TransportCredentials struct {
	config       *tls.Config
	fingerprint  Fingerprint
	sniOverride  string                  // camouflage SNI domain (e.g. "www.google.com")
	sessionCache utls.ClientSessionCache // TLS session ticket cache
	mu           sync.Mutex
}

// CredentialOption configures optional TransportCredentials parameters.
type CredentialOption func(*TransportCredentials)

// WithSNIOverride sets a camouflage SNI domain for the ClientHello.
func WithSNIOverride(sni string) CredentialOption {
	return func(tc *TransportCredentials) {
		tc.sniOverride = sni
	}
}

// WithSessionCache attaches a TLS session ticket cache for session resumption.
func WithSessionCache(cache utls.ClientSessionCache) CredentialOption {
	return func(tc *TransportCredentials) {
		tc.sessionCache = cache
	}
}

// NewCredentials creates new uTLS-based transport credentials for gRPC.
func NewCredentials(config *tls.Config, fingerprint Fingerprint, opts ...CredentialOption) credentials.TransportCredentials {
	tc := &TransportCredentials{
		config:      config,
		fingerprint: fingerprint,
	}
	for _, o := range opts {
		o(tc)
	}
	return tc
}

// ClientHandshake performs the TLS handshake using uTLS
func (c *TransportCredentials) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	// Clone config to avoid race conditions
	cfg := c.config.Clone()

	// Extract host from authority for SNI
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		// Authority might not have a port
		host = authority
	}

	// Use SNI override for camouflage if configured, otherwise use actual host
	sniHost := host
	if c.sniOverride != "" {
		sniHost = c.sniOverride
	}
	cfg.ServerName = sniHost

	// Get the ClientHelloID for the fingerprint
	helloID := GetHelloID(c.fingerprint)

	// Convert tls.Certificate to utls.Certificate
	utlsCerts := make([]utls.Certificate, len(cfg.Certificates))
	for i, cert := range cfg.Certificates {
		utlsCerts[i] = utls.Certificate{
			Certificate:                 cert.Certificate,
			PrivateKey:                  cert.PrivateKey,
			OCSPStaple:                  cert.OCSPStaple,
			SignedCertificateTimestamps: cert.SignedCertificateTimestamps,
			Leaf:                        cert.Leaf,
		}
	}

	// Build uTLS config with ALPN and session ticket support
	utlsConfig := &utls.Config{
		ServerName:             sniHost,
		NextProtos:             []string{"h2"},
		RootCAs:                cfg.RootCAs,
		Certificates:           utlsCerts,
		InsecureSkipVerify:     cfg.InsecureSkipVerify,
		MinVersion:             cfg.MinVersion,
		MaxVersion:             cfg.MaxVersion,
		SessionTicketsDisabled: false,
		VerifyPeerCertificate:  cfg.VerifyPeerCertificate,
	}
	if c.sessionCache != nil {
		utlsConfig.ClientSessionCache = c.sessionCache
	}

	// Create uTLS connection
	uConn := utls.UClient(rawConn, utlsConfig, helloID)

	// Apply SNI override at the wire level (updates the ClientHello SNI extension)
	if c.sniOverride != "" {
		uConn.SetSNI(c.sniOverride)
	}

	// Perform handshake with context
	errChan := make(chan error, 1)
	go func() {
		errChan <- uConn.Handshake()
	}()

	select {
	case err := <-errChan:
		if err != nil {
			uConn.Close()
			return nil, nil, fmt.Errorf("uTLS handshake failed: %w", err)
		}
	case <-ctx.Done():
		uConn.Close()
		return nil, nil, ctx.Err()
	}

	return uConn, TLSInfo{State: uConn.ConnectionState()}, nil
}

// ServerHandshake is not implemented as uTLS is primarily for clients
func (c *TransportCredentials) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, fmt.Errorf("uTLS credentials do not support server-side handshake")
}

// Info returns protocol info
func (c *TransportCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{
		SecurityProtocol: "tls",
		SecurityVersion:  "1.2",
		ServerName:       c.config.ServerName,
	}
}

// Clone creates a copy of the credentials
func (c *TransportCredentials) Clone() credentials.TransportCredentials {
	return &TransportCredentials{
		config:       c.config.Clone(),
		fingerprint:  c.fingerprint,
		sniOverride:  c.sniOverride,
		sessionCache: c.sessionCache,
	}
}

// OverrideServerName overrides the server name for TLS verification
func (c *TransportCredentials) OverrideServerName(serverName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.ServerName = serverName
	return nil
}

// TLSInfo contains TLS connection state information
type TLSInfo struct {
	State utls.ConnectionState
	credentials.CommonAuthInfo
}

// AuthType returns the authentication type
func (t TLSInfo) AuthType() string {
	return "tls"
}

// GetSecurityValue returns the TLS connection state
func (t TLSInfo) GetSecurityValue() credentials.ChannelzSecurityValue {
	return &credentials.TLSChannelzSecurityValue{
		StandardName: t.State.NegotiatedProtocol,
	}
}

// NewLRUSessionCache creates a uTLS LRU session ticket cache with the given capacity.
func NewLRUSessionCache(capacity int) utls.ClientSessionCache {
	return utls.NewLRUClientSessionCache(capacity)
}
