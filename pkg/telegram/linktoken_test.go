package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestLinkToken_RoundTrip(t *testing.T) {
	const secret = "test-secret"
	tok := SignLinkToken(4242, secret, 15*time.Minute)
	if tok == "" {
		t.Fatal("empty token")
	}
	if len(tok) > 64 {
		t.Fatalf("token %d chars, exceeds Telegram start-param limit of 64", len(tok))
	}
	subID, err := ParseLinkToken(tok, secret)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if subID != 4242 {
		t.Fatalf("subID = %d, want 4242", subID)
	}
}

func TestLinkToken_WrongSecretFails(t *testing.T) {
	tok := SignLinkToken(1, "secret-a", 15*time.Minute)
	if _, err := ParseLinkToken(tok, "secret-b"); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestLinkToken_TamperedFails(t *testing.T) {
	const secret = "test-secret"
	tok := SignLinkToken(7, secret, 15*time.Minute)
	payload := tok[len(linkTokenPrefix):]
	flipped := "A"
	if payload[0] == 'A' {
		flipped = "B"
	}
	bad := linkTokenPrefix + flipped + payload[1:]
	if _, err := ParseLinkToken(bad, secret); err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestLinkToken_NonCanonicalEncodingFails(t *testing.T) {
	const secret = "test-secret"
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	tok := SignLinkToken(13, secret, 15*time.Minute)
	last := tok[len(tok)-1]
	idx := strings.IndexByte(alphabet, last)
	if idx < 0 {
		t.Fatalf("final char %q outside base64url alphabet", last)
	}
	if idx%4 != 0 {
		t.Fatalf("encoder produced non-canonical final char %q (index %d)", last, idx)
	}

	malleated := tok[:len(tok)-1] + string(alphabet[idx+1])
	if _, err := ParseLinkToken(malleated, secret); err == nil {
		t.Fatal("expected error for non-canonical token encoding")
	}
}

func TestLinkToken_ExpiredFails(t *testing.T) {
	const secret = "test-secret"
	tok := SignLinkToken(9, secret, -1*time.Minute) // already expired
	if _, err := ParseLinkToken(tok, secret); err != ErrLinkTokenExpired {
		t.Fatalf("expected ErrLinkTokenExpired, got %v", err)
	}
}

func TestLinkToken_GarbageFails(t *testing.T) {
	const secret = "test-secret"
	for _, g := range []string{"", "lk_", "notatoken", "xx_abcdef"} {
		if _, err := ParseLinkToken(g, secret); err == nil {
			t.Errorf("expected error for %q", g)
		}
	}
}
