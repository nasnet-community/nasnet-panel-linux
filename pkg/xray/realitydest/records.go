package realitydest

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
)

// TLS record and handshake constants used by the layout check.
const (
	recordHeaderLen = 5

	recordTypeChangeCipherSpec byte = 20
	recordTypeAlert            byte = 21
	recordTypeHandshake        byte = 22
	recordTypeApplicationData  byte = 23

	typeServerHello byte = 2

	extSupportedVersions uint16 = 43
	extKeyShare          uint16 = 51

	versionTLS12 uint16 = 0x0303
	versionTLS13 uint16 = 0x0304
)

// helloRetryRequestRandom marks a ServerHello that is really a HelloRetryRequest
// (RFC 8446 §4.1.3).
var helloRetryRequestRandom = []byte{
	0xCF, 0x21, 0xAD, 0x74, 0xE5, 0x9A, 0x61, 0x11, 0xBE, 0x1D, 0x8C, 0x02, 0x1E, 0x65, 0xB8, 0x91,
	0xC2, 0xA2, 0x11, 0x16, 0x7A, 0xBB, 0x8C, 0x5E, 0x07, 0x9E, 0x09, 0xE2, 0xC8, 0xA8, 0x33, 0x9C,
}

type record struct {
	typ     byte
	version uint16
	length  int // header included
	body    []byte
	partial bool
}

type serverHello struct {
	retry            bool
	supportedVersion uint16 // 0 when the extension is absent (TLS 1.2 or older)
	cipherSuite      uint16
	group            tls.CurveID
	keyLen           int
}

func (h *serverHello) versionName() string {
	switch {
	case h.supportedVersion == versionTLS13:
		return "TLS 1.3"
	case h.supportedVersion != 0:
		return fmt.Sprintf("TLS 0x%04x", h.supportedVersion)
	default:
		return "TLS 1.2"
	}
}

// flight is the server's half of the handshake as REALITY would see it.
type flight struct {
	records []record
	hello   *serverHello
	total   int
	largest int
}

func parseFlight(b []byte) flight {
	var f flight
	for len(b) >= recordHeaderLen {
		r := record{
			typ:     b[0],
			version: binary.BigEndian.Uint16(b[1:3]),
			length:  recordHeaderLen + int(binary.BigEndian.Uint16(b[3:5])),
		}
		if len(b) < r.length {
			r.body, r.partial = b[recordHeaderLen:], true
			b = nil
		} else {
			r.body = b[recordHeaderLen:r.length]
			b = b[r.length:]
		}
		f.records = append(f.records, r)
		f.total += r.length
		if r.length > f.largest {
			f.largest = r.length
		}
	}
	if len(f.records) > 0 {
		first := f.records[0]
		if first.typ == recordTypeHandshake && !first.partial && len(first.body) > 4 && first.body[0] == typeServerHello {
			f.hello = parseServerHello(first.body[4:])
		}
	}
	return f
}

// parseServerHello reads the fields REALITY inspects. It returns nil on any
// structural problem so the caller reports the handshake error instead.
func parseServerHello(b []byte) *serverHello {
	// legacy_version(2) random(32) session_id<0..32> cipher_suite(2) compression(1) extensions
	if len(b) < 2+32+1 {
		return nil
	}
	h := &serverHello{retry: bytes.Equal(b[2:34], helloRetryRequestRandom)}
	b = b[34:]
	sidLen := int(b[0])
	b = b[1:]
	if len(b) < sidLen+2+1+2 {
		return nil
	}
	b = b[sidLen:]
	h.cipherSuite = binary.BigEndian.Uint16(b[0:2])
	b = b[3:]
	extLen := int(binary.BigEndian.Uint16(b[0:2]))
	b = b[2:]
	if len(b) < extLen {
		return nil
	}
	b = b[:extLen]
	for len(b) >= 4 {
		typ := binary.BigEndian.Uint16(b[0:2])
		n := int(binary.BigEndian.Uint16(b[2:4]))
		b = b[4:]
		if len(b) < n {
			return nil
		}
		data := b[:n]
		b = b[n:]
		switch typ {
		case extSupportedVersions:
			if len(data) == 2 {
				h.supportedVersion = binary.BigEndian.Uint16(data)
			}
		case extKeyShare:
			if len(data) >= 2 {
				h.group = tls.CurveID(binary.BigEndian.Uint16(data[0:2]))
			}
			if !h.retry && len(data) >= 4 {
				h.keyLen = int(binary.BigEndian.Uint16(data[2:4]))
			}
		}
	}
	return h
}

// verdict applies reality's Server() rules in order. An empty reason means the
// layout is one REALITY can hijack.
func (f flight) verdict(who subject) (reason, message string) {
	if len(f.records) == 0 {
		return "", "" // nothing came back; the handshake error explains it
	}
	first := f.records[0]
	if first.typ == recordTypeAlert {
		return "", "" // the handshake error carries the alert text
	}
	if first.typ != recordTypeHandshake || first.partial || len(first.body) == 0 || first.body[0] != typeServerHello {
		return ReasonUnexpectedRec, fmt.Sprintf("%s opened with a %s record instead of a ServerHello.", who.start, recordTypeName(first.typ))
	}
	if first.version != versionTLS12 {
		return ReasonRecordVersion, fmt.Sprintf("%s sent its ServerHello in a record marked 0x%04x; REALITY requires 0x0303 on every record.", who.start, first.version)
	}
	if f.hello == nil {
		return ReasonUnexpectedRec, fmt.Sprintf("%s sent a ServerHello REALITY cannot parse.", who.start)
	}
	h := f.hello
	if h.retry {
		return ReasonHelloRetry, fmt.Sprintf("%s asked to restart the key exchange with %s (HelloRetryRequest). REALITY gives up before a second round trip, so clients would see the site's real certificate.", who.start, groupName(h.group))
	}
	if h.supportedVersion != versionTLS13 {
		return ReasonTLSVersion, fmt.Sprintf("%s negotiated %s. REALITY needs TLS 1.3.", who.start, h.versionName())
	}
	switch h.cipherSuite {
	case tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256:
	default:
		return ReasonCipherSuite, fmt.Sprintf("%s chose %s, which is not a TLS 1.3 cipher suite.", who.start, tls.CipherSuiteName(h.cipherSuite))
	}
	x25519 := h.group == tls.X25519 && h.keyLen == 32
	mlkem := h.group == tls.X25519MLKEM768 && h.keyLen == 1120
	if !x25519 && !mlkem {
		return ReasonKeyShare, fmt.Sprintf("%s answered with %s. REALITY can only mirror X25519 or X25519MLKEM768 key exchanges, so clients would see the site's real certificate.", who.start, groupName(h.group))
	}
	for i, r := range f.records {
		if r.length > maxRecordBytes {
			return ReasonRecordTooLarge, fmt.Sprintf("%s sent a %d-byte handshake record; REALITY reads at most %d bytes per record. Its certificate chain is too large.", who.start, r.length, maxRecordBytes)
		}
		if i == 0 {
			continue
		}
		if r.version != versionTLS12 {
			return ReasonRecordVersion, fmt.Sprintf("%s sent a record marked 0x%04x; REALITY requires 0x0303 on every record.", who.start, r.version)
		}
		if i == 1 {
			if r.typ != recordTypeChangeCipherSpec || r.length != 6 {
				return ReasonNoCCS, fmt.Sprintf("%s skipped the ChangeCipherSpec record after its ServerHello. REALITY expects the TLS 1.3 middlebox-compatible layout.", who.start)
			}
			continue
		}
		if r.typ != recordTypeApplicationData {
			return ReasonUnexpectedRec, fmt.Sprintf("%s sent a %s record where REALITY expects encrypted handshake data.", who.start, recordTypeName(r.typ))
		}
	}
	return "", ""
}

func recordTypeName(t byte) string {
	switch t {
	case recordTypeChangeCipherSpec:
		return "ChangeCipherSpec"
	case recordTypeAlert:
		return "alert"
	case recordTypeHandshake:
		return "handshake"
	case recordTypeApplicationData:
		return "application data"
	}
	return fmt.Sprintf("type %d", t)
}

func groupName(g tls.CurveID) string {
	switch g {
	case tls.CurveP256:
		return "P-256"
	case tls.CurveP384:
		return "P-384"
	case tls.CurveP521:
		return "P-521"
	case tls.X25519:
		return "X25519"
	case tls.X25519MLKEM768:
		return "X25519MLKEM768"
	case 0x6399:
		return "X25519Kyber768Draft00"
	case 0:
		return "no key share"
	}
	return fmt.Sprintf("group 0x%04x", uint16(g))
}
