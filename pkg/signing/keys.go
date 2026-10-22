package signing

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// PublicKeys are the release keys this build trusts. Two overlap during a rotation.
// Add one with the line `go run ./cmd/release-sign keygen` prints.
var PublicKeys = []ed25519.PublicKey{}

func mustKey(s string) ed25519.PublicKey {
	k, err := ParsePublicKey(s)
	if err != nil {
		panic(err)
	}
	return k
}

var _ = mustKey // used once a key is listed

// extraKey lets a drill build trust a test key: -ldflags "-X …/pkg/signing.extraKey=<base64>".
var extraKey string

// Trusted returns every key a signature may come from.
func Trusted() []ed25519.PublicKey {
	keys := append([]ed25519.PublicKey(nil), PublicKeys...)
	if k, err := ParsePublicKey(extraKey); err == nil {
		keys = append(keys, k)
	}
	return keys
}

// VerifyAny reports whether sig is a valid signature of data by any trusted key.
func VerifyAny(data, sig []byte) bool {
	for _, k := range Trusted() {
		if Verify(data, sig, k) {
			return true
		}
	}
	return false
}

// EncodeSig is a .sig file's contents: one line of base64.
func EncodeSig(sig []byte) string {
	return base64.StdEncoding.EncodeToString(sig) + "\n"
}

// ParseSig reads a .sig file, base64 or the raw 64 bytes older builds wrote.
func ParseSig(data []byte) ([]byte, error) {
	if len(data) == ed25519.SignatureSize {
		return data, nil
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("signature isn't base64: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature is %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}

// ParsePublicKey reads a base64 public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty public key")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("public key isn't base64: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key is %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// ParsePrivateKey reads a base64 private key: the 32-byte seed or the 64-byte key.
func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("private key isn't base64: %w", err)
	}
	switch len(b) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	}
	return nil, fmt.Errorf("private key is %d bytes, want %d or %d", len(b), ed25519.SeedSize, ed25519.PrivateKeySize)
}

// KeyID is a short name for a key, for logs and the panel's About line.
func KeyID(k ed25519.PublicKey) string {
	sum := sha256.Sum256(k)
	return hex.EncodeToString(sum[:4])
}
