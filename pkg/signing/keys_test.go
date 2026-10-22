package signing

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func withTrusted(t *testing.T, keys []ed25519.PublicKey, extra string) {
	t.Helper()
	oldKeys, oldExtra := PublicKeys, extraKey
	PublicKeys, extraKey = keys, extra
	t.Cleanup(func() { PublicKeys, extraKey = oldKeys, oldExtra })
}

func TestVerifyAny(t *testing.T) {
	pub, priv := genKeys(t)
	other, _ := genKeys(t)
	data := []byte("release.json")
	sig := Sign(data, priv)

	withTrusted(t, nil, "")
	if VerifyAny(data, sig) {
		t.Fatal("verified with no trusted keys")
	}
	withTrusted(t, []ed25519.PublicKey{other, pub}, "")
	if !VerifyAny(data, sig) {
		t.Fatal("second trusted key didn't verify")
	}
	if VerifyAny([]byte("tampered"), sig) {
		t.Fatal("tampered data verified")
	}
}

func TestTrustedExtraKey(t *testing.T) {
	pub, priv := genKeys(t)
	withTrusted(t, nil, base64.StdEncoding.EncodeToString(pub))
	if !VerifyAny([]byte("x"), Sign([]byte("x"), priv)) {
		t.Fatal("extra key not trusted")
	}
	withTrusted(t, nil, "not base64!")
	if len(Trusted()) != 0 {
		t.Fatal("a broken extra key was trusted")
	}
}

func TestParseSig(t *testing.T) {
	_, priv := genKeys(t)
	sig := Sign([]byte("x"), priv)
	for name, in := range map[string][]byte{"base64": []byte(EncodeSig(sig)), "raw": sig} {
		got, err := ParseSig(in)
		if err != nil || string(got) != string(sig) {
			t.Fatalf("%s: got %v, %v", name, got, err)
		}
	}
	if _, err := ParseSig([]byte("c2hvcnQ=")); err == nil {
		t.Fatal("short signature accepted")
	}
}

func TestParseKeys(t *testing.T) {
	pub, priv := genKeys(t)
	seed := base64.StdEncoding.EncodeToString(priv.Seed())
	full := base64.StdEncoding.EncodeToString(priv)
	for _, s := range []string{seed, full} {
		k, err := ParsePrivateKey(s)
		if err != nil || !k.Public().(ed25519.PublicKey).Equal(pub) {
			t.Fatalf("private key %q: %v", s[:6], err)
		}
	}
	if _, err := ParsePublicKey(base64.StdEncoding.EncodeToString(pub[:10])); err == nil {
		t.Fatal("short public key accepted")
	}
	if KeyID(pub) == "" || len(KeyID(pub)) != 8 {
		t.Fatalf("key id %q", KeyID(pub))
	}
}
