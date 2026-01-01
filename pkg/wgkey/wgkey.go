// Package wgkey generates WireGuard Curve25519 key material.
package wgkey

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// GeneratePrivateKey returns a base64 WireGuard private key.
func GeneratePrivateKey() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	// curve25519 clamp
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	return base64.StdEncoding.EncodeToString(k[:]), nil
}

// PublicKey derives the base64 public key for a base64 private key.
func PublicKey(privB64 string) (string, error) {
	priv, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil || len(priv) != 32 {
		return "", fmt.Errorf("wgkey: invalid private key")
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// GeneratePresharedKey returns a base64 32-byte preshared key.
func GeneratePresharedKey() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k[:]), nil
}
