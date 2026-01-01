package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDeploymentToken_RoundTrip(t *testing.T) {
	m := NewTokenManager("secret")
	tok, err := m.GenerateDeploymentToken(42, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	claims, err := m.ValidateDeploymentToken(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.NodeID != 42 {
		t.Errorf("NodeID = %d, want 42", claims.NodeID)
	}
	if claims.Type != "deployment" {
		t.Errorf("Type = %q, want deployment", claims.Type)
	}
}

// A token minted with a different secret must not validate.
func TestDeploymentToken_WrongSecret(t *testing.T) {
	tok, err := NewTokenManager("real").GenerateDeploymentToken(1, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := NewTokenManager("attacker").ValidateDeploymentToken(tok); err == nil {
		t.Fatal("token verified under wrong secret")
	}
}

func TestDeploymentToken_Expired(t *testing.T) {
	m := NewTokenManager("secret")
	tok, err := m.GenerateDeploymentToken(1, -time.Minute)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := m.ValidateDeploymentToken(tok); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestDeploymentToken_Garbage(t *testing.T) {
	if _, err := NewTokenManager("secret").ValidateDeploymentToken("not.a.jwt"); err == nil {
		t.Fatal("garbage string accepted")
	}
}

// A correctly-signed token whose Type isn't "deployment" is rejected — guards
// against reusing some other HS256 token against this endpoint.
func TestDeploymentToken_WrongType(t *testing.T) {
	claims := DeploymentClaims{
		NodeID: 1,
		Type:   "session",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := NewTokenManager("secret").ValidateDeploymentToken(signed); err == nil {
		t.Fatal("non-deployment token type accepted")
	}
}
