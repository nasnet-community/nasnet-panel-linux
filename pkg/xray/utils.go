package xray

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"

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

// realityFingerprints are the uTLS presets whose Client Hello offers
// X25519MLKEM768 ahead of X25519. REALITY servers on Xray 26.9.8 and later
// hand every other Client Hello to the dest site, so the client never gets in.
var realityFingerprints = map[string]bool{
	"": true, "chrome": true, "firefox": true, "safari": true,
	"hellochrome_auto": true, "hellochrome_131": true, "hellochrome_133": true,
	"hellofirefox_auto": true, "hellofirefox_148": true,
	"hellosafari_auto": true, "hellosafari_26_3": true,
}

// RealityFingerprintOK reports whether a REALITY client using fp gets past
// current servers. Empty means chrome.
func RealityFingerprintOK(fp string) bool {
	return realityFingerprints[strings.ToLower(strings.TrimSpace(fp))]
}

// RealityFingerprint returns fp, or chrome when fp would be turned away.
func RealityFingerprint(fp string) string {
	if RealityFingerprintOK(fp) {
		return fp
	}
	return "chrome"
}
