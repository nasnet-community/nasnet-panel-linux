package signing

import (
	"crypto/ed25519"
	"fmt"
	"os"
)

// Sign produces an Ed25519 signature of data using the given private key.
func Sign(data []byte, privateKey ed25519.PrivateKey) []byte {
	return ed25519.Sign(privateKey, data)
}

// Verify checks an Ed25519 signature against data and a public key.
func Verify(data, signature []byte, publicKey ed25519.PublicKey) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(publicKey, data, signature)
}

// SignFile reads a file and returns its Ed25519 signature.
func SignFile(path string, privateKey ed25519.PrivateKey) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return Sign(data, privateKey), nil
}
