package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
)

// hmacSHA256 returns the HMAC-SHA256 of msg keyed by key.
func hmacSHA256(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}
