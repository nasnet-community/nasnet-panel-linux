package system

import (
	"bytes"
	"net/netip"
	"testing"
)

func TestPMPWire(t *testing.T) {
	if got := pmpExternalAddrRequest(); !bytes.Equal(got, []byte{0, 0}) {
		t.Fatalf("external addr request: % x", got)
	}

	// ver 0, op 1 (UDP map), reserved, internal 51820, suggested 51820, lifetime 7200
	want := []byte{0x00, 0x01, 0x00, 0x00, 0xCA, 0x6C, 0xCA, 0x6C, 0x00, 0x00, 0x1C, 0x20}
	if got := pmpMapRequest("udp", 51820, 51820, 7200); !bytes.Equal(got, want) {
		t.Fatalf("map request:\n got % x\nwant % x", got, want)
	}
	if got := pmpMapRequest("tcp", 1, 1, 1); got[1] != 2 {
		t.Fatalf("tcp map opcode = %d, want 2", got[1])
	}

	// external-address response: op 0x80, result 0, epoch 1000, 203.0.113.7
	addr := []byte{0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x03, 0xE8, 0xCB, 0x00, 0x71, 0x07}
	r, ok := parsePMPResponse(addr)
	if !ok || r.Op != 0x80 || r.Result != 0 || r.Epoch != 1000 ||
		r.ExternalIP != netip.AddrFrom4([4]byte{203, 0, 113, 7}) {
		t.Fatalf("addr response: %+v ok=%v", r, ok)
	}

	// map response: op 0x81, granted external 60001, lifetime 3600
	mp := []byte{0x00, 0x81, 0x00, 0x00, 0x00, 0x00, 0x03, 0xE8,
		0xCA, 0x6C, 0xEA, 0x61, 0x00, 0x00, 0x0E, 0x10}
	r, ok = parsePMPResponse(mp)
	if !ok || r.Op != 0x81 || r.InternalPort != 51820 || r.ExternalPort != 60001 || r.Lifetime != 3600 {
		t.Fatalf("map response: %+v ok=%v", r, ok)
	}

	if _, ok := parsePMPResponse([]byte{2, 0x80, 0, 0}); ok {
		t.Fatal("wrong version parsed")
	}
	if _, ok := parsePMPResponse(addr[:7]); ok {
		t.Fatal("short packet parsed")
	}
}

func TestPCPWire(t *testing.T) {
	self := netip.AddrFrom4([4]byte{192, 0, 2, 10})
	ann := pcpAnnounceRequest(self)
	if len(ann) != 24 || ann[0] != 2 || ann[1] != 0 {
		t.Fatalf("announce: len=%d % x", len(ann), ann[:2])
	}
	// v4-mapped self address sits at bytes 8..24
	if !bytes.Equal(ann[8:24], []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 192, 0, 2, 10}) {
		t.Fatalf("client ip: % x", ann[8:24])
	}

	var nonce [12]byte
	copy(nonce[:], "abcdefghijkl")
	req := pcpMapRequest(self, nonce, "udp", 51820, 51820, 7200)
	if len(req) != 60 || req[0] != 2 || req[1] != 1 {
		t.Fatalf("map request: len=%d ver=%d op=%d", len(req), req[0], req[1])
	}
	if req[36] != 17 { // protocol byte after the 12-byte nonce
		t.Fatalf("proto byte = %d, want 17 (udp)", req[36])
	}

	// Build a success response by mirroring the request.
	resp := make([]byte, 60)
	resp[0], resp[1] = 2, 0x81                       // version, MAP|response
	resp[3] = 0                                      // result success
	copy(resp[4:8], []byte{0x00, 0x00, 0x1C, 0x20})  // lifetime 7200
	copy(resp[8:12], []byte{0x00, 0x00, 0x03, 0xE8}) // epoch 1000
	copy(resp[24:36], nonce[:])
	resp[36] = 17
	resp[40], resp[41] = 0xCA, 0x6C // internal 51820
	resp[42], resp[43] = 0xEA, 0x61 // assigned external 60001
	copy(resp[44:60], []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 203, 0, 113, 7})

	r, ok := parsePCPResponse(resp)
	if !ok || r.Opcode != 1 || r.Result != 0 || r.Lifetime != 7200 || r.Epoch != 1000 ||
		r.Nonce != nonce || r.InternalPort != 51820 || r.ExternalPort != 60001 ||
		r.ExternalIP != netip.AddrFrom4([4]byte{203, 0, 113, 7}) {
		t.Fatalf("pcp response: %+v ok=%v", r, ok)
	}

	if _, ok := parsePCPResponse(resp[:23]); ok {
		t.Fatal("short pcp parsed")
	}
	resp[0] = 0
	if _, ok := parsePCPResponse(resp); ok {
		t.Fatal("wrong version parsed")
	}
}
