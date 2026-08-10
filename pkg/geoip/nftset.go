package geoip

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geofiles"
)

// CIDRSet is one country's prefixes (V6 is unused)
type CIDRSet struct {
	Code string
	V4   []string
	V6   []string
}

func (s *CIDRSet) Len() int { return len(s.V4) + len(s.V6) }

// EmbeddedCIDRSet is the list used until an upstream refresh lands, and after
// one fails.
func EmbeddedCIDRSet(code string) (*CIDRSet, error) {
	data, _, ok := geofiles.GetEmbeddedGeoFiles(geofiles.RegionIran)
	if !ok || len(data) == 0 {
		return nil, fmt.Errorf("no embedded geoip.dat in this build")
	}
	return ParseGeoIP(data, code)
}

// ParseGeoIP extracts one country's prefixes from a GeoIPList protobuf, decoded
// off the wire rather than pulling in a generated dependency for two fields.
func ParseGeoIP(data []byte, code string) (*CIDRSet, error) {
	want := strings.ToUpper(strings.TrimSpace(code))
	if want == "" {
		return nil, fmt.Errorf("empty country code")
	}

	entries, err := protoFields(data, 1)
	if err != nil {
		return nil, fmt.Errorf("parse GeoIPList: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("geoip.dat has no entries")
	}

	for _, entry := range entries {
		codes, err := protoFields(entry, 1)
		if err != nil || len(codes) == 0 {
			continue
		}
		if strings.ToUpper(string(codes[0])) != want {
			continue
		}

		out := &CIDRSet{Code: want}
		cidrs, err := protoFields(entry, 2)
		if err != nil {
			return nil, fmt.Errorf("parse %s cidr list: %w", want, err)
		}
		for _, c := range cidrs {
			ipBytes, err := protoFields(c, 1)
			if err != nil || len(ipBytes) == 0 {
				continue
			}
			// prefix=0 is a legal /0 that encodes to nothing, so not an error.
			prefix, _ := protoVarintField(c, 2)
			ip := net.IP(ipBytes[0])
			switch len(ip) {
			case net.IPv4len:
				out.V4 = append(out.V4, fmt.Sprintf("%s/%d", ip.String(), prefix))
			case net.IPv6len:
				if v4 := ip.To4(); v4 != nil && prefix >= 96 {
					// v4-mapped: emit as v4 so it lands in the right typed set.
					out.V4 = append(out.V4, fmt.Sprintf("%s/%d", v4.String(), prefix-96))
					continue
				}
				out.V6 = append(out.V6, fmt.Sprintf("%s/%d", ip.String(), prefix))
			}
		}
		if out.Len() == 0 {
			return nil, fmt.Errorf("country %s has no prefixes", want)
		}
		return out, nil
	}
	return nil, fmt.Errorf("country %s not found in geoip.dat", want)
}

// protoFields returns every length-delimited field with the given number
func protoFields(buf []byte, field int) ([][]byte, error) {
	var out [][]byte
	for i := 0; i < len(buf); {
		key, n := binary.Uvarint(buf[i:])
		if n <= 0 {
			return nil, fmt.Errorf("bad varint key at %d", i)
		}
		i += n
		num, wire := int(key>>3), int(key&0x7)

		switch wire {
		case 0:
			_, n := binary.Uvarint(buf[i:])
			if n <= 0 {
				return nil, fmt.Errorf("bad varint value at %d", i)
			}
			i += n
		case 2:
			l, n := binary.Uvarint(buf[i:])
			if n <= 0 {
				return nil, fmt.Errorf("bad length at %d", i)
			}
			i += n
			end := i + int(l)
			if end > len(buf) || end < i {
				return nil, fmt.Errorf("length %d overruns the buffer at %d", l, i)
			}
			if num == field {
				out = append(out, buf[i:end])
			}
			i = end
		case 5:
			i += 4
		case 1:
			i += 8
		default:
			return nil, fmt.Errorf("unsupported wire type %d at %d", wire, i)
		}
		if i > len(buf) {
			return nil, fmt.Errorf("truncated message")
		}
	}
	return out, nil
}

// protoVarintField returns the first varint field with the given number
func protoVarintField(buf []byte, field int) (uint64, error) {
	for i := 0; i < len(buf); {
		key, n := binary.Uvarint(buf[i:])
		if n <= 0 {
			return 0, fmt.Errorf("bad varint key at %d", i)
		}
		i += n
		num, wire := int(key>>3), int(key&0x7)

		switch wire {
		case 0:
			v, n := binary.Uvarint(buf[i:])
			if n <= 0 {
				return 0, fmt.Errorf("bad varint at %d", i)
			}
			if num == field {
				return v, nil
			}
			i += n
		case 2:
			l, n := binary.Uvarint(buf[i:])
			if n <= 0 {
				return 0, fmt.Errorf("bad length at %d", i)
			}
			i += n + int(l)
		case 5:
			i += 4
		case 1:
			i += 8
		default:
			return 0, fmt.Errorf("unsupported wire type %d", wire)
		}
	}
	return 0, fmt.Errorf("field %d not found", field)
}
