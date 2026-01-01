package xray

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"

	"golang.org/x/crypto/curve25519"
)

// KeyPair holds X25519 keys
type KeyPair struct {
	PrivateKey string
	PublicKey  string
}

// GenerateX25519Keys generates a Private/Public key pair for Xray Reality/VLESS
func GenerateX25519Keys() (*KeyPair, error) {
	var privateKey [32]byte
	if _, err := io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return nil, err
	}

	var publicKey [32]byte
	curve25519.ScalarBaseMult(&publicKey, &privateKey)

	// Xray uses RawURLEncoding (no padding)
	privStr := base64.RawURLEncoding.EncodeToString(privateKey[:])
	pubStr := base64.RawURLEncoding.EncodeToString(publicKey[:])

	return &KeyPair{
		PrivateKey: privStr,
		PublicKey:  pubStr,
	}, nil
}

// GenerateShortID generates a random hex string of specified length (usually 8-16 chars)
func GenerateShortID(length int) (string, error) {
	// Length is in characters (hex nibbles), so bytes = length / 2
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
