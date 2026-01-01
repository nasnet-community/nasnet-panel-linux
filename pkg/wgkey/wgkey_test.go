package wgkey

import (
	"encoding/base64"
	"testing"
)

func TestGeneratePrivateKey_ValidAndClamped(t *testing.T) {
	k, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	b, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("private key = %d bytes, want 32", len(b))
	}
	// Curve25519 clamp: low 3 bits of b[0] are zero; top bit of b[31] is zero
	// and bit 6 is one. Catches a regression that drops the clamping.
	if b[0]&0x07 != 0 {
		t.Errorf("byte 0 = %#x, low 3 bits should be cleared", b[0])
	}
	if b[31]&0x80 != 0 {
		t.Errorf("byte 31 = %#x, top bit should be cleared", b[31])
	}
	if b[31]&0x40 == 0 {
		t.Errorf("byte 31 = %#x, bit 6 should be set", b[31])
	}
}

func TestGeneratePrivateKey_Unique(t *testing.T) {
	a, _ := GeneratePrivateKey()
	b, _ := GeneratePrivateKey()
	if a == b {
		t.Fatal("two private keys collided — RNG broken")
	}
}

func TestPublicKey_DeterministicAndDifferentFromPrivate(t *testing.T) {
	priv, err := GeneratePrivateKey()
	if err != nil {
		t.Fatalf("priv: %v", err)
	}
	p1, err := PublicKey(priv)
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	p2, _ := PublicKey(priv)
	if p1 != p2 {
		t.Fatal("PublicKey not deterministic for same private key")
	}
	if p1 == priv {
		t.Fatal("public key equals private key")
	}
	b, err := base64.StdEncoding.DecodeString(p1)
	if err != nil || len(b) != 32 {
		t.Errorf("public key decode: %d bytes (err=%v), want 32", len(b), err)
	}
}

func TestPublicKey_Errors(t *testing.T) {
	if _, err := PublicKey("!!!not-base64!!!"); err == nil {
		t.Error("invalid base64 accepted")
	}
	// Valid base64 but wrong byte length (16 instead of 32).
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := PublicKey(short); err == nil {
		t.Error("16-byte key accepted, want length error")
	}
}

func TestGeneratePresharedKey(t *testing.T) {
	a, err := GeneratePresharedKey()
	if err != nil {
		t.Fatalf("GeneratePresharedKey: %v", err)
	}
	b, err := base64.StdEncoding.DecodeString(a)
	if err != nil || len(b) != 32 {
		t.Errorf("preshared key decode: %d bytes (err=%v), want 32", len(b), err)
	}
	other, _ := GeneratePresharedKey()
	if a == other {
		t.Fatal("two preshared keys collided")
	}
}
