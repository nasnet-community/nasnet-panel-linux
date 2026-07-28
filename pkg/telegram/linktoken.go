package telegram

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

var (
	ErrBadLinkToken     = errors.New("telegram: invalid link token")
	ErrLinkTokenExpired = errors.New("telegram: link token expired")
)

const linkTokenPrefix = "lk_"

// SignLinkToken builds a compact, signed deep-link start parameter binding a
// subscription ID to whoever opens it. Layout before base64url:
//
//	subID(8) ‖ exp_unix(8) ‖ HMAC_SHA256(subID‖exp, secret)[:16]
//
// The result is "lk_" + 43 base64url chars = 46 total, within Telegram's
// 64-char start-param limit, and uses only the allowed [A-Za-z0-9_-] charset.
// Stateless: verification needs only the secret (no storage).
func SignLinkToken(subID uint64, secret string, ttl time.Duration) string {
	buf := make([]byte, 16, 32)
	binary.BigEndian.PutUint64(buf[0:8], subID)
	binary.BigEndian.PutUint64(buf[8:16], uint64(time.Now().Add(ttl).Unix()))
	mac := hmacSHA256([]byte(secret), buf)
	buf = append(buf, mac[:16]...)
	return linkTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
}

// ParseLinkToken verifies a token from SignLinkToken and returns the embedded
// subscription ID. Errors on a bad prefix/encoding (ErrBadLinkToken), signature
// mismatch (ErrBadLinkToken), or expiry (ErrLinkTokenExpired).
func ParseLinkToken(token, secret string) (uint64, error) {
	if !strings.HasPrefix(token, linkTokenPrefix) {
		return 0, ErrBadLinkToken
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, linkTokenPrefix))
	if err != nil || len(raw) != 32 {
		return 0, ErrBadLinkToken
	}
	expected := hmacSHA256([]byte(secret), raw[:16])
	if !hmac.Equal(expected[:16], raw[16:32]) {
		return 0, ErrBadLinkToken
	}
	if time.Now().Unix() > int64(binary.BigEndian.Uint64(raw[8:16])) {
		return 0, ErrLinkTokenExpired
	}
	return binary.BigEndian.Uint64(raw[0:8]), nil
}
