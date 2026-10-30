// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backup seals the plugin's state into a file the owner keeps, and
// opens it again on a fresh install.
//
// The state holds what every node trusts and every link carries, and losing
// it means enrolling the nodes again. The file is encrypted with AES-256-GCM
// under a key scrypt makes from a passphrase. It is laid out like this, with
// numbers big-endian:
//
//	0   8   "RNBACKUP"
//	8   1   format version, 1
//	9   4   scrypt N
//	13  4   scrypt r
//	17  4   scrypt p
//	21  16  scrypt salt
//	37  12  AES-GCM nonce
//	49  ... the snapshot, encrypted, then GCM's 16-byte tag
//
// The first 49 bytes are the header. All of it is also fed to GCM as
// additional data, so a changed byte anywhere in the header, as well as in the
// body, stops the file from opening. The scrypt parameters live in the header
// so they can be raised later and files made today still open.
//
// The package knows nothing of what the snapshot holds. Calling Import is not
// the same as trusting the file: whoever knows the passphrase can seal
// anything, so the caller still has to check what comes out.
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
)

const (
	magic     = "RNBACKUP"
	formatV1  = 1
	saltLen   = 16
	nonceLen  = 12
	tagLen    = 16 // GCM's
	keyLen    = 32 // AES-256
	headerLen = len(magic) + 1 + 3*4 + saltLen + nonceLen

	// where the fields start
	versionAt = len(magic)
	paramsAt  = versionAt + 1
	saltAt    = paramsAt + 3*4
	nonceAt   = saltAt + saltLen
)

// MinPassphraseRunes is the shortest passphrase Export takes. Import doesn't
// hold a passphrase to it: the rule may change, and a file made under the old
// one still has to open.
const MinPassphraseRunes = 8

var (
	// ErrWeakPassphrase is Export's refusal of a passphrase that is too short.
	ErrWeakPassphrase = errors.New("backup: the passphrase is too short")

	// ErrBadFile is a file that is not a backup this build can open: wrong
	// magic, a format it doesn't know, cut short, or asking for more of the
	// hub than it will give.
	ErrBadFile = errors.New("backup: not a backup this build can open")

	// ErrBadPassphrase is GCM refusing to open a file whose header looked
	// right. GCM checks one tag over the header and the body, so a wrong
	// passphrase, a flipped byte and a file cut off in the middle all fail it
	// the same way and nothing says which it was. The message names the first
	// two because they are what people run into.
	ErrBadPassphrase = errors.New("backup: wrong passphrase, or the file was changed")
)

// kdf is what scrypt is asked for.
type kdf struct{ n, r, p uint32 }

// defaultKDF needs 32 MiB (128*N*r bytes) while it runs.
var defaultKDF = kdf{n: 1 << 15, r: 8, p: 1}

// What a file may ask of the hub. The header is read before anything shows the
// file is genuine, and scrypt wants 128*N*r bytes and time in proportion to
// N*r*p. These allow up to twice Export's N and p, which leaves room to raise
// the cost later without making today's files unreadable, but a crafted header
// can't make the hub allocate gigabytes (at most 64 MiB here) or spin for
// minutes. Go's scrypt runs its p lanes one after another, so a bigger p only
// costs time.
const (
	maxN = 1 << 16
	maxR = 8
	maxP = 2
)

func (k kdf) check() error {
	switch {
	case k.n < 2 || k.n&(k.n-1) != 0:
		return fmt.Errorf("%w: scrypt N is %d, not a power of two", ErrBadFile, k.n)
	case k.n > maxN || k.r > maxR || k.p > maxP:
		return fmt.Errorf("%w: scrypt asks for too much (N %d, r %d, p %d)", ErrBadFile, k.n, k.r, k.p)
	case k.r < 1 || k.p < 1:
		return fmt.Errorf("%w: scrypt r and p must be at least 1 (r %d, p %d)", ErrBadFile, k.r, k.p)
	}
	return nil
}

// scryptKey is a variable so a test can watch what is done with the key.
var scryptKey = scrypt.Key

// Export encrypts snapshot under a key made from the passphrase. A new random
// salt and nonce go into every file, so exporting the same state twice gives
// two different files.
func Export(snapshot []byte, passphrase string) ([]byte, error) {
	return seal(snapshot, passphrase, defaultKDF, rand.Reader)
}

func seal(snapshot []byte, passphrase string, k kdf, random io.Reader) ([]byte, error) {
	if utf8.RuneCountInString(passphrase) < MinPassphraseRunes {
		return nil, fmt.Errorf("%w: use at least %d characters", ErrWeakPassphrase, MinPassphraseRunes)
	}
	if err := k.check(); err != nil {
		return nil, err // never write a file that wouldn't open
	}
	hdr := make([]byte, headerLen)
	copy(hdr, magic)
	hdr[versionAt] = formatV1
	binary.BigEndian.PutUint32(hdr[paramsAt:], k.n)
	binary.BigEndian.PutUint32(hdr[paramsAt+4:], k.r)
	binary.BigEndian.PutUint32(hdr[paramsAt+8:], k.p)
	if _, err := io.ReadFull(random, hdr[saltAt:]); err != nil { // the salt and the nonce
		return nil, err
	}
	aead, err := newGCM(passphrase, hdr[saltAt:nonceAt], k)
	if err != nil {
		return nil, err
	}
	return append(hdr, aead.Seal(nil, hdr[nonceAt:], snapshot, hdr)...), nil
}

// Import opens a file Export made and returns the snapshot. ErrBadFile means
// the file isn't one this build can open, and ErrBadPassphrase that it didn't
// open under this passphrase.
func Import(data []byte, passphrase string) ([]byte, error) {
	k, err := readHeader(data)
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(passphrase, data[saltAt:nonceAt], k)
	if err != nil {
		return nil, err
	}
	snapshot, err := aead.Open(nil, data[nonceAt:headerLen], data[headerLen:], data[:headerLen])
	if err != nil {
		return nil, ErrBadPassphrase
	}
	return snapshot, nil
}

// readHeader checks everything about the file that doesn't need the key, and
// does it before scrypt runs, since scrypt is where a crafted file could do
// harm.
func readHeader(data []byte) (kdf, error) {
	switch {
	case len(data) < headerLen:
		return kdf{}, fmt.Errorf("%w: the file is too short", ErrBadFile)
	case string(data[:len(magic)]) != magic:
		return kdf{}, fmt.Errorf("%w: it isn't a backup file", ErrBadFile)
	case data[versionAt] > formatV1:
		return kdf{}, fmt.Errorf("%w: it is format %d and this build reads up to %d", ErrBadFile, data[versionAt], formatV1)
	case data[versionAt] != formatV1:
		return kdf{}, fmt.Errorf("%w: unknown format %d", ErrBadFile, data[versionAt])
	}
	k := kdf{
		n: binary.BigEndian.Uint32(data[paramsAt:]),
		r: binary.BigEndian.Uint32(data[paramsAt+4:]),
		p: binary.BigEndian.Uint32(data[paramsAt+8:]),
	}
	if err := k.check(); err != nil {
		return kdf{}, err
	}
	if len(data) < headerLen+tagLen {
		return kdf{}, fmt.Errorf("%w: the file is cut short", ErrBadFile)
	}
	return k, nil
}

// newGCM makes the cipher for a passphrase. The key is wiped as soon as the
// cipher has its own copy of it, and so is the passphrase's copy as bytes
// (the string it came from can't be).
func newGCM(passphrase string, salt []byte, k kdf) (cipher.AEAD, error) {
	pass := []byte(passphrase)
	defer clear(pass)
	key, err := scryptKey(pass, salt, int(k.n), int(k.r), int(k.p), keyLen)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
