package system

import (
	"encoding/binary"
	"net/netip"
)

// NAT-PMP and PCP share gateway port 5351. The first byte is the version and
// tells them apart: 0 is NAT-PMP, 2 is PCP.
const (
	pmpVersion = 0
	pcpVersion = 2

	pmpOpExternalAddr = 0
	pmpOpMapUDP       = 1
	pmpOpMapTCP       = 2

	pcpOpAnnounce = 0
	pcpOpMap      = 1

	// NAT-PMP result codes (RFC 6886)
	pmpResultOK            = 0
	pmpResultNotAuthorized = 2

	// PCP result codes (RFC 6887)
	pcpResultOK              = 0
	pcpResultNotAuthorized   = 2
	pcpResultAddressMismatch = 12
)

func pmpProtoOp(proto string) uint8 {
	if proto == "tcp" {
		return pmpOpMapTCP
	}
	return pmpOpMapUDP
}

func pmpExternalAddrRequest() []byte { return []byte{pmpVersion, pmpOpExternalAddr} }

func pmpMapRequest(proto string, internal, external uint16, lifetime uint32) []byte {
	b := make([]byte, 12)
	b[1] = pmpProtoOp(proto)
	binary.BigEndian.PutUint16(b[4:], internal)
	binary.BigEndian.PutUint16(b[6:], external)
	binary.BigEndian.PutUint32(b[8:], lifetime)
	return b
}

type pmpResponse struct {
	Op           uint8
	Result       uint16
	Epoch        uint32
	ExternalIP   netip.Addr
	InternalPort uint16
	ExternalPort uint16
	Lifetime     uint32
}

func parsePMPResponse(b []byte) (pmpResponse, bool) {
	if len(b) < 8 || b[0] != pmpVersion || b[1] < 0x80 {
		return pmpResponse{}, false
	}
	r := pmpResponse{
		Op:     b[1],
		Result: binary.BigEndian.Uint16(b[2:]),
		Epoch:  binary.BigEndian.Uint32(b[4:]),
	}
	switch r.Op {
	case 0x80 | pmpOpExternalAddr:
		if len(b) < 12 {
			return pmpResponse{}, false
		}
		r.ExternalIP = netip.AddrFrom4([4]byte(b[8:12]))
	case 0x80 | pmpOpMapUDP, 0x80 | pmpOpMapTCP:
		if len(b) < 16 {
			return pmpResponse{}, false
		}
		r.InternalPort = binary.BigEndian.Uint16(b[8:])
		r.ExternalPort = binary.BigEndian.Uint16(b[10:])
		r.Lifetime = binary.BigEndian.Uint32(b[12:])
	default:
		return pmpResponse{}, false
	}
	return r, true
}

// put16 writes an IPv4 as the v4-mapped 16-byte form PCP wants.
func put16(dst []byte, a netip.Addr) {
	v6 := netip.AddrFrom16(a.As16())
	copy(dst, v6.AsSlice())
}

func pcpHeader(op uint8, lifetime uint32, self netip.Addr) []byte {
	b := make([]byte, 24)
	b[0], b[1] = pcpVersion, op
	binary.BigEndian.PutUint32(b[4:], lifetime)
	put16(b[8:24], self)
	return b
}

func pcpAnnounceRequest(self netip.Addr) []byte { return pcpHeader(pcpOpAnnounce, 0, self) }

func pcpMapRequest(self netip.Addr, nonce [12]byte, proto string, internal, external uint16, lifetime uint32) []byte {
	b := append(pcpHeader(pcpOpMap, lifetime, self), make([]byte, 36)...)
	copy(b[24:36], nonce[:])
	if proto == "tcp" {
		b[36] = 6
	} else {
		b[36] = 17
	}
	binary.BigEndian.PutUint16(b[40:], internal)
	binary.BigEndian.PutUint16(b[42:], external)
	// Suggested external address stays zero: the router's choice.
	put16(b[44:60], netip.AddrFrom4([4]byte{}))
	return b
}

type pcpResponse struct {
	Opcode       uint8
	Result       uint8
	Lifetime     uint32
	Epoch        uint32
	Nonce        [12]byte
	InternalPort uint16
	ExternalPort uint16
	ExternalIP   netip.Addr
}

func parsePCPResponse(b []byte) (pcpResponse, bool) {
	if len(b) < 24 || b[0] != pcpVersion || b[1]&0x80 == 0 {
		return pcpResponse{}, false
	}
	r := pcpResponse{
		Opcode:   b[1] &^ 0x80,
		Result:   b[3],
		Lifetime: binary.BigEndian.Uint32(b[4:]),
		Epoch:    binary.BigEndian.Uint32(b[8:]),
	}
	if r.Opcode == pcpOpMap {
		if len(b) < 60 {
			return pcpResponse{}, false
		}
		copy(r.Nonce[:], b[24:36])
		r.InternalPort = binary.BigEndian.Uint16(b[40:])
		r.ExternalPort = binary.BigEndian.Uint16(b[42:])
		r.ExternalIP = netip.AddrFrom16([16]byte(b[44:60])).Unmap()
	}
	return r, true
}
