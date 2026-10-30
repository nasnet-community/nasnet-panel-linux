// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pki is the plugin's certificate authority. There is one CA per hub:
// it signs every node's agent certificate and the client certificate the hub
// shows when it dials them, so a node trusts exactly one hub.
//
// What it issues follows what an unchanged agent checks. The CN carries the
// node id, which InstallCertificate compares across a renewal. The node's
// certificate is for serverAuth and the hub's for clientAuth, as the agent's
// TLS settings ask. Serials are random and read as plain decimals, the form
// UpdateCertDenylist takes. The package imports only the standard library.
package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"time"
)

const (
	// CAYears is how long a new CA lasts. Nothing renews it: a new CA would
	// mean enrolling every node again.
	CAYears = 10
	// CertMonths is how long a node or hub certificate lasts.
	CertMonths = 12
	// RenewBefore is how long before a certificate runs out the hub renews it.
	RenewBefore = 30 * 24 * time.Hour

	// HubCommonName is the CN of the hub's client certificate. It has to stay
	// clear of the node-<id>- form, which the agent refuses from a client.
	HubCommonName = "remote-nodes-hub"

	org = "NasNet Remote Nodes"
	// A node whose clock runs a little behind still takes a fresh certificate.
	skew = time.Hour
)

var (
	ErrBadCA         = errors.New("pki: the CA certificate and key can't be used")
	ErrCAExpired     = errors.New("pki: the CA has expired")
	ErrCANotYetValid = errors.New("pki: the CA is not valid yet, so the clock is probably wrong")
	ErrBadNodeID     = errors.New("pki: a node certificate needs a node id from 1 to 4294967295")
	ErrBadHost       = errors.New("pki: the host is neither an IP address nor a plain DNS name")
)

// CA signs certificates. It is safe for use from several goroutines.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

// Issued is a certificate with its key, as it is sent to a node or handed to
// the agent client. The hub keeps neither: the key of a node's certificate is
// written to the node once and dropped.
type Issued struct {
	CertPEM []byte
	KeyPEM  []byte
	// Serial is the decimal text the agent's denylist names a certificate by.
	Serial string
	// These are what the certificate says, to the second and in UTC.
	NotBefore time.Time
	NotAfter  time.Time
}

// NewCA makes a P-256 CA that is valid from an hour before now for CAYears.
func NewCA(now time.Time) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "NasNet Remote Nodes CA", Organization: []string{org}},
		NotBefore:             now.Add(-skew),
		NotAfter:              now.AddDate(CAYears, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0, // leaves only: nothing below this CA may sign
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return newCA(cert, key)
}

func newCA(cert *x509.Certificate, key *ecdsa.PrivateKey) (*CA, error) {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &CA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

// maxPEM bounds what Load parses. A CA and its key take about 1 KiB between them.
const maxPEM = 16 << 10

// Load reads a CA back from the PEM that CertPEM and KeyPEM gave. It takes
// one certificate and one P-256 key, in the SEC 1 or PKCS #8 form, and
// refuses anything else: a certificate that isn't a self-signed CA, a key
// that isn't the certificate's, a second block that may not be what it
// looks like. Whether the CA is still in date is not its business: the
// issuing calls check that against the time they are given.
//
// An error never carries any of the PEM it was given.
func Load(certPEM, keyPEM []byte) (*CA, error) {
	if len(certPEM) > maxPEM || len(keyPEM) > maxPEM {
		return nil, badCA("the PEM is too long")
	}
	cb, err := onlyBlock(certPEM, "certificate", "CERTIFICATE")
	if err != nil {
		return nil, err
	}
	kb, err := onlyBlock(keyPEM, "key", "EC PRIVATE KEY", "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, badCA("the certificate doesn't parse")
	}
	key, err := parseKey(kb)
	if err != nil {
		return nil, err
	}
	switch pub, ok := cert.PublicKey.(*ecdsa.PublicKey); {
	case !cert.BasicConstraintsValid || !cert.IsCA:
		return nil, badCA("the certificate is not a CA's")
	case cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0:
		return nil, badCA("the certificate may not sign certificates")
	case !ok || pub.Curve != elliptic.P256():
		return nil, badCA("the certificate's key is not P-256")
	case !pub.Equal(&key.PublicKey):
		return nil, badCA("the key is not the certificate's")
	case cert.CheckSignatureFrom(cert) != nil:
		return nil, badCA("the certificate is not self-signed")
	}
	return newCA(cert, key)
}

func badCA(detail string) error { return fmt.Errorf("%w: %s", ErrBadCA, detail) }

// onlyBlock wants exactly one PEM block, of one of the types, with no headers
// (an encrypted key has them, and nothing here could open it).
func onlyBlock(data []byte, what string, types ...string) (*pem.Block, error) {
	b, rest := pem.Decode(data)
	switch {
	case b == nil:
		return nil, badCA("the " + what + " is not PEM")
	case len(b.Headers) > 0:
		return nil, badCA("the " + what + " has PEM headers, so it may be encrypted")
	case !slices.Contains(types, b.Type):
		return nil, badCA("the " + what + " is the wrong kind of PEM block")
	case len(bytes.TrimSpace(rest)) > 0:
		return nil, badCA("the " + what + " has more in it than one block")
	}
	return b, nil
}

func parseKey(b *pem.Block) (*ecdsa.PrivateKey, error) {
	var key *ecdsa.PrivateKey
	if b.Type == "EC PRIVATE KEY" {
		k, err := x509.ParseECPrivateKey(b.Bytes)
		if err != nil {
			return nil, badCA("the key doesn't parse")
		}
		key = k
	} else {
		k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
		if err != nil {
			return nil, badCA("the key doesn't parse")
		}
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, badCA("the key is not an ECDSA key")
		}
		key = ec
	}
	if key.Curve != elliptic.P256() {
		return nil, badCA("the key is not P-256")
	}
	return key, nil
}

// CertPEM is the CA certificate, which goes to each node as its ca.crt.
func (c *CA) CertPEM() []byte { return bytes.Clone(c.certPEM) }

// KeyPEM is the CA's private key, for the state file and nowhere else.
func (c *CA) KeyPEM() []byte { return bytes.Clone(c.keyPEM) }

// Certificate is the parsed CA certificate. Treat it as read-only.
func (c *CA) Certificate() *x509.Certificate { return c.cert }

// IssueNodeCert signs the certificate a node's agent serves. It names the
// node node-<id>-<name> and is good for serverAuth only, so it can't stand in
// for the hub's client certificate. host is the address the hub dials: an
// IP address goes in as an IP SAN and anything else as a DNS name, since a
// client checks an address against the one and a name against the other.
//
// The id is what the agent matches when it is handed a renewal. name is
// taken as the user typed it and made fit for a CN (see commonName).
func (c *CA) IssueNodeCert(id uint, name, host string, now time.Time) (*Issued, error) {
	if id == 0 || uint64(id) > math.MaxUint32 {
		return nil, ErrBadNodeID
	}
	ips, dns, err := sans(host)
	if err != nil {
		return nil, err
	}
	return c.issue(&x509.Certificate{
		Subject:     pkix.Name{CommonName: commonName(id, name), Organization: []string{org}},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: ips,
		DNSNames:    dns,
	}, now)
}

// IssueHubClientCert signs the certificate the hub presents when it dials an
// agent. It is for clientAuth only. Making one is cheap, so the hub can make
// a fresh one when it starts instead of keeping it.
func (c *CA) IssueHubClientCert(now time.Time) (*Issued, error) {
	return c.issue(&x509.Certificate{
		Subject:     pkix.Name{CommonName: HubCommonName, Organization: []string{org}},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, now)
}

// issue gives tmpl a new P-256 key, a serial and a CertMonths life that
// starts an hour back, never reaching outside the CA's own. A time outside
// the CA's life is refused, not worked around: a router that booted with the
// wrong clock would otherwise hand out certificates that are expired or not
// yet valid once the clock is right.
func (c *CA) issue(tmpl *x509.Certificate, now time.Time) (*Issued, error) {
	now = now.UTC()
	switch {
	case now.Before(c.cert.NotBefore):
		return nil, fmt.Errorf("%w (it starts %s, the time is %s)", ErrCANotYetValid, c.cert.NotBefore.Format(time.RFC3339), now.Format(time.RFC3339))
	case now.After(c.cert.NotAfter):
		return nil, fmt.Errorf("%w (it ended %s)", ErrCAExpired, c.cert.NotAfter.Format(time.RFC3339))
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if tmpl.SerialNumber, err = newSerial(); err != nil {
		return nil, err
	}
	tmpl.NotBefore, tmpl.NotAfter = now.Add(-skew), now.AddDate(0, CertMonths, 0)
	if tmpl.NotBefore.Before(c.cert.NotBefore) {
		tmpl.NotBefore = c.cert.NotBefore
	}
	if tmpl.NotAfter.After(c.cert.NotAfter) {
		tmpl.NotAfter = c.cert.NotAfter
	}
	tmpl.BasicConstraintsValid = true // and not a CA

	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &Issued{
		CertPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:    pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		Serial:    leaf.SerialNumber.String(),
		NotBefore: leaf.NotBefore,
		NotAfter:  leaf.NotAfter,
	}, nil
}

// newSerial is 128 random bits and never zero, which is no serial at all.
func newSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	for {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, err
		}
		if n.Sign() > 0 {
			return n, nil
		}
	}
}

// NeedsRenewal is true when a certificate has RenewBefore or less left. A
// zero notAfter, which is how a node with no certificate on record reads, is
// long expired.
func NeedsRenewal(notAfter, now time.Time) bool {
	return notAfter.Sub(now) <= RenewBefore
}
