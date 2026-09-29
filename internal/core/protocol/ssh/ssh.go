// Package sshproto holds hub-side SSH protocol helpers: server host-key
// generation (stored in core_inbounds.settings) and client share-link building.
package sshproto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"strconv"

	gossh "golang.org/x/crypto/ssh"
)

// GenerateHostKeyPEM creates a fresh ed25519 SSH host key. It returns the
// PKCS#8 PEM (to store in the inbound settings + push to the agent) and the
// SHA256 fingerprint (for display / client pinning).
func GenerateHostKeyPEM() (string, string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("marshal key: %w", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("derive signer: %w", err)
	}
	fp := gossh.FingerprintSHA256(signer.PublicKey())
	return pemStr, fp, nil
}

// BuildShareLink renders a client share link:
//
//	ssh://<base64url(user:password)>@host:port#label
//
// The label is URL-escaped so emoji/spaces survive.
func BuildShareLink(host string, port int, username, password, label string) string {
	creds := base64.RawURLEncoding.EncodeToString([]byte(username + ":" + password))
	link := fmt.Sprintf("ssh://%s@%s", creds, net.JoinHostPort(host, strconv.Itoa(port)))
	if label != "" {
		link += "#" + url.PathEscape(label)
	}
	return link
}
