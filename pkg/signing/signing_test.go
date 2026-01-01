package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

func genKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func TestSignVerify_RoundTrip(t *testing.T) {
	pub, priv := genKeys(t)
	data := []byte("agent-binary-v1")
	sig := Sign(data, priv)
	if !Verify(data, sig, pub) {
		t.Fatal("valid signature did not verify")
	}
}

func TestVerify_TamperedData(t *testing.T) {
	pub, priv := genKeys(t)
	sig := Sign([]byte("original"), priv)
	if Verify([]byte("tampered"), sig, pub) {
		t.Fatal("signature verified against tampered data")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	_, priv := genKeys(t)
	otherPub, _ := genKeys(t)
	data := []byte("payload")
	sig := Sign(data, priv)
	if Verify(data, sig, otherPub) {
		t.Fatal("signature verified under unrelated public key")
	}
}

// Malformed key/signature sizes are rejected before hitting ed25519.Verify.
func TestVerify_BadSizes(t *testing.T) {
	pub, priv := genKeys(t)
	data := []byte("payload")
	sig := Sign(data, priv)
	if Verify(data, sig, ed25519.PublicKey("short")) {
		t.Error("undersized public key accepted")
	}
	if Verify(data, []byte("short"), pub) {
		t.Error("undersized signature accepted")
	}
}

func TestSignFile(t *testing.T) {
	pub, priv := genKeys(t)
	path := filepath.Join(t.TempDir(), "artifact.bin")
	content := []byte("file contents to sign")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	sig, err := SignFile(path, priv)
	if err != nil {
		t.Fatalf("SignFile: %v", err)
	}
	if !Verify(content, sig, pub) {
		t.Fatal("file signature did not verify against its contents")
	}
}

func TestSignFile_Missing(t *testing.T) {
	_, priv := genKeys(t)
	if _, err := SignFile(filepath.Join(t.TempDir(), "nope.bin"), priv); err == nil {
		t.Fatal("expected error for missing file")
	}
}
