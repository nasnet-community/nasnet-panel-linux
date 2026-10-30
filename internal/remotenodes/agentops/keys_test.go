// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/crypto/curve25519"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

func TestNewRealityKeysAreValidAndPairUp(t *testing.T) {
	seenPriv, seenID := map[string]bool{}, map[string]bool{}
	for range 50 {
		k, err := NewRealityKeys()
		if err != nil {
			t.Fatal(err)
		}
		priv, err := base64.RawURLEncoding.DecodeString(k.PrivateKey)
		if err != nil || len(priv) != 32 {
			t.Fatalf("private key %q: %d bytes, %v", k.PrivateKey, len(priv), err)
		}
		pub, err := base64.RawURLEncoding.DecodeString(k.PublicKey)
		if err != nil || len(pub) != 32 {
			t.Fatalf("public key %q: %d bytes, %v", k.PublicKey, len(pub), err)
		}
		// Xray writes them without padding, in the URL alphabet
		for _, s := range []string{k.PrivateKey, k.PublicKey} {
			if strings.ContainsAny(s, "=+/") || len(s) != 43 {
				t.Errorf("%q isn't base64url without padding", s)
			}
		}
		// the public key is the private key's, worked out by another implementation
		want, err := curve25519.X25519(priv, curve25519.Basepoint)
		if err != nil || string(want) != string(pub) {
			t.Fatalf("the public key isn't the private key's")
		}
		if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(k.ShortID) {
			t.Errorf("short id %q", k.ShortID)
		}
		if b, err := hex.DecodeString(k.ShortID); err != nil || len(b) != 8 {
			t.Errorf("short id %q", k.ShortID)
		}
		if seenPriv[k.PrivateKey] || seenID[k.ShortID] {
			t.Fatal("a key or an id came up twice")
		}
		seenPriv[k.PrivateKey], seenID[k.ShortID] = true, true

		if err := k.Valid(); err != nil {
			t.Fatalf("keys it made aren't valid: %v", err)
		}
		// and a node config takes them
		if _, err := nodecfg.Build(nodecfg.Input{
			EntryPort: 443, RealityDest: "www.example.com:443", ServerNames: []string{"www.example.com"},
			PrivateKey: k.PrivateKey, ShortIDs: []string{k.ShortID},
			Bridge: nodecfg.Bridge{UUID: "11111111-2222-4333-8444-555555555555", Email: nodecfg.BridgeEmail(1), ReverseTag: "r1"},
		}); err != nil {
			t.Fatalf("nodecfg refuses them: %v", err)
		}
	}
}

func TestValidKeysRefusesWhatDoesntPairUp(t *testing.T) {
	k, _ := NewRealityKeys()
	other, _ := NewRealityKeys()
	for name, bad := range map[string]RealityKeys{
		"no keys":                    {},
		"a public key of another":    {PrivateKey: k.PrivateKey, PublicKey: other.PublicKey, ShortID: k.ShortID},
		"a private key too short":    {PrivateKey: k.PrivateKey[:40], PublicKey: k.PublicKey, ShortID: k.ShortID},
		"a private key with padding": {PrivateKey: k.PrivateKey + "=", PublicKey: k.PublicKey, ShortID: k.ShortID},
		"no public key":              {PrivateKey: k.PrivateKey, ShortID: k.ShortID},
		"an odd short id":            {PrivateKey: k.PrivateKey, PublicKey: k.PublicKey, ShortID: "abc"},
		"a short id not hex":         {PrivateKey: k.PrivateKey, PublicKey: k.PublicKey, ShortID: "zz"},
		"a short id too long":        {PrivateKey: k.PrivateKey, PublicKey: k.PublicKey, ShortID: strings.Repeat("ab", 9)},
		"no short id":                {PrivateKey: k.PrivateKey, PublicKey: k.PublicKey},
	} {
		err := bad.Valid()
		var inv *model.InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), k.PrivateKey) || strings.Contains(err.Error(), other.PublicKey) {
			t.Errorf("%s: the message quotes a key: %q", name, err)
		}
	}
	if !(RealityKeys{}).IsZero() || k.IsZero() {
		t.Error("IsZero")
	}
	// a short id may be shorter, and in either case
	k.ShortID = "AB12"
	if err := k.Valid(); err != nil {
		t.Errorf("a short id of 4 hex digits: %v", err)
	}
}
