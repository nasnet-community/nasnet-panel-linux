// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// RealityKeys are the three secrets-and-names a node's entry is made of: the
// private key the node keeps, the public key every link carries, and the short
// id a client names. They are text as Xray writes them.
type RealityKeys struct {
	PrivateKey string // X25519, base64url without padding
	PublicKey  string // the same
	ShortID    string // 8 random bytes in hex
}

// NewRealityKeys makes a key pair and a short id, here, with no call to a
// node. The key is a fresh X25519 key from crypto/ecdh; the text is what
// `xray x25519` prints, the raw bytes in base64url without padding.
func NewRealityKeys() (RealityKeys, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return RealityKeys{}, err
	}
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return RealityKeys{}, err
	}
	return RealityKeys{
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		PublicKey:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		ShortID:    hex.EncodeToString(id),
	}, nil
}

// IsZero says there are no keys, which Provision reads as "make new ones".
func (k RealityKeys) IsZero() bool { return k == RealityKeys{} }

// Valid checks keys that were kept: the private key is 32 bytes, the public key
// is the one that belongs to it, and the short id is hex Xray takes. A message
// never quotes a key.
func (k RealityKeys) Valid() error {
	raw, err := base64.RawURLEncoding.DecodeString(k.PrivateKey)
	if err != nil || len(raw) != 32 {
		return &model.InvalidError{Msg: "the REALITY private key must be 32 bytes in base64url without padding"}
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return &model.InvalidError{Msg: "the REALITY private key can't be used"}
	}
	pub, err := base64.RawURLEncoding.DecodeString(k.PublicKey)
	if err != nil || subtle.ConstantTimeCompare(pub, priv.PublicKey().Bytes()) != 1 {
		return &model.InvalidError{Msg: "the REALITY public key isn't the one that belongs to the private key"}
	}
	if n := len(k.ShortID); n == 0 || n > 16 || n%2 != 0 || strings.Trim(strings.ToLower(k.ShortID), "0123456789abcdef") != "" {
		return &model.InvalidError{Msg: "the REALITY short id must be 2 to 16 hex digits, an even number of them"}
	}
	return nil
}
