package jwt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newManager returns a fresh JWT manager with sane test defaults.
func newManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(Config{
		SecretKey:          "test-secret-do-not-use-in-prod",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 24 * time.Hour,
		Issuer:             "nasnet-test",
	})
}

// newBlacklist returns a manager backed by an in-memory sqlite DB.
func newBlacklist(t *testing.T) *Blacklist {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&RevokedToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewBlacklist(db)
}

func TestSignAndValidate_RoundTrip(t *testing.T) {
	m := newManager(t)

	pair, err := m.GenerateTokenPair(42, 100200300, "alice", true, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("empty tokens")
	}
	if pair.AccessToken == pair.RefreshToken {
		t.Fatal("access and refresh tokens must not collide")
	}

	claims, err := m.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("validate access: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" || !claims.IsAdmin {
		t.Errorf("unexpected claims: %+v", claims)
	}
	if claims.TokenType != "access" {
		t.Errorf("expected access token, got %q", claims.TokenType)
	}
	if claims.ID == "" {
		t.Error("JTI must be set for blacklist revocation support")
	}
}

func TestWrongSecret_Rejects(t *testing.T) {
	good := newManager(t)
	pair, _ := good.GenerateTokenPair(1, 1, "bob", false, false)

	evil := NewManager(Config{SecretKey: "some-other-secret"})
	if _, err := evil.ValidateToken(pair.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("wrong-secret validation: want ErrInvalidToken, got %v", err)
	}
}

func TestExpiredToken_Rejects(t *testing.T) {
	m := newManager(t)
	// Backdate the token so it's already expired on return.
	pair, err := m.GenerateTokenPairWithExpiry(1, 1, "bob", false, false, -time.Second, time.Minute)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	_, err = m.ValidateAccessToken(pair.AccessToken)
	if !errors.Is(err, ErrExpiredToken) {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}
}

func TestTamperedPayload_Rejects(t *testing.T) {
	m := newManager(t)
	pair, _ := m.GenerateTokenPair(1, 1, "bob", false, false)

	// Flip a byte in the middle payload segment. JWTs are
	// header.payload.sig separated by dots; mutating the payload
	// invalidates the signature.
	parts := strings.Split(pair.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}
	// Append junk to payload — changes signed input.
	mangled := parts[0] + "." + parts[1] + "AAAA." + parts[2]
	if _, err := m.ValidateAccessToken(mangled); err == nil {
		t.Error("tampered payload must be rejected")
	}
}

func TestTokenTypeMismatch_Rejects(t *testing.T) {
	m := newManager(t)
	pair, _ := m.GenerateTokenPair(1, 1, "bob", false, false)

	// Access token consumed through refresh validator must reject.
	if _, err := m.ValidateRefreshToken(pair.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("wrong token-type: want ErrInvalidToken, got %v", err)
	}
	// Refresh token through access validator must reject.
	if _, err := m.ValidateAccessToken(pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("wrong token-type: want ErrInvalidToken, got %v", err)
	}
}

func TestBlacklistHit_RejectsToken(t *testing.T) {
	bl := newBlacklist(t)
	m := newManager(t)
	m.SetBlacklist(bl)

	pair, _ := m.GenerateTokenPair(1, 1, "bob", false, false)
	claims, err := m.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("pre-revoke validate: %v", err)
	}
	if err := m.RevokeToken(context.Background(), claims.ID, claims.ExpiresAt.Time); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := m.ValidateAccessToken(pair.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("blacklisted token: want ErrInvalidToken, got %v", err)
	}
}

func TestRevokeWithoutBlacklist_IsNoop(t *testing.T) {
	// RevokeToken returns nil even when no blacklist is configured so
	// callers don't have to guard every call site. Documented behavior.
	m := newManager(t)
	if err := m.RevokeToken(context.Background(), "some-id", time.Now().Add(time.Hour)); err != nil {
		t.Errorf("no-blacklist revoke should be no-op, got %v", err)
	}
}

func TestBlacklistCleanup_RemovesOnlyExpired(t *testing.T) {
	bl := newBlacklist(t)
	now := time.Now()
	// Active revocation.
	_ = bl.Revoke(context.Background(), "active-id", now.Add(time.Hour))
	// Expired revocation.
	_ = bl.Revoke(context.Background(), "expired-id", now.Add(-time.Hour))

	affected, err := bl.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 expired row cleaned, got %d", affected)
	}
	if !bl.IsRevoked(context.Background(), "active-id") {
		t.Error("active revocation must survive cleanup")
	}
	if bl.IsRevoked(context.Background(), "expired-id") {
		t.Error("expired revocation must be removed by cleanup")
	}
}

func TestNoneAlgorithm_Rejected(t *testing.T) {
	// Algorithm-confusion attacks: a token header claiming "none"
	// must not be accepted as valid even if claims parse. The
	// parser callback only returns the key for HMAC methods; any
	// other method returns ErrInvalidToken from the callback.
	m := newManager(t)
	// Hand-crafted "alg=none" token with empty signature.
	// Header: {"alg":"none","typ":"JWT"} → eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0
	// Payload: {"user_id":1,"token_type":"access","exp":9999999999} →
	// decoded manually is sufficient — any payload at all.
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJ1c2VyX2lkIjoxLCJ0b2tlbl90eXBlIjoiYWNjZXNzIiwiZXhwIjo5OTk5OTk5OTk5fQ."
	if _, err := m.ValidateToken(forged); err == nil {
		t.Error("alg=none must be rejected")
	}
}

func TestIssuerEmbedded(t *testing.T) {
	m := newManager(t)
	pair, _ := m.GenerateTokenPair(1, 1, "bob", false, false)
	claims, err := m.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.Issuer != "nasnet-test" {
		t.Errorf("issuer not propagated: got %q", claims.Issuer)
	}
}
