package geoip

import (
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geofiles"
)

func embeddedIran(t *testing.T) ([]byte, []byte, bool) {
	t.Helper()
	ip, site, ok := geofiles.GetEmbeddedGeoFiles(geofiles.RegionIran)
	return ip, site, ok
}

// The production input: if this fails the build has no usable geoip.dat.
func TestEmbeddedCIDRSet_IranHasBothFamilies(t *testing.T) {
	s, err := EmbeddedCIDRSet("IR")
	if err != nil {
		t.Fatalf("EmbeddedCIDRSet: %v", err)
	}
	if len(s.V4) == 0 {
		t.Fatal("no IPv4 prefixes for IR")
	}
	// A real country list is thousands; a handful means the wrong field.
	if len(s.V4) < 100 {
		t.Errorf("only %d IPv4 prefixes for IR; the parse is probably wrong", len(s.V4))
	}
	for _, c := range s.V4[:10] {
		if !strings.Contains(c, "/") {
			t.Errorf("prefix %q is not in CIDR form", c)
		}
		if strings.Contains(c, ":") {
			t.Errorf("IPv6 prefix %q landed in the V4 list", c)
		}
	}
	for _, c := range s.V6 {
		if !strings.Contains(c, ":") {
			t.Errorf("V6 prefix %q is not IPv6", c)
		}
	}
}

func TestEmbeddedCIDRSet_UnknownCode(t *testing.T) {
	if _, err := EmbeddedCIDRSet("ZZ"); err == nil {
		t.Fatal("EmbeddedCIDRSet accepted an unknown country code")
	}
}

func TestParseGeoIP_CodeIsCaseInsensitive(t *testing.T) {
	data, _, ok := embeddedIran(t)
	if !ok {
		t.Skip("no embedded geoip.dat in this build")
	}
	lower, err := ParseGeoIP(data, "ir")
	if err != nil {
		t.Fatal(err)
	}
	upper, err := ParseGeoIP(data, "IR")
	if err != nil {
		t.Fatal(err)
	}
	if lower.Len() != upper.Len() {
		t.Errorf("case changed the result: %d vs %d", lower.Len(), upper.Len())
	}
}

func TestParseGeoIP_GarbageInput(t *testing.T) {
	if _, err := ParseGeoIP([]byte("not a protobuf"), "IR"); err == nil {
		t.Fatal("ParseGeoIP accepted garbage")
	}
}

func TestCIDRSet_Len(t *testing.T) {
	s := &CIDRSet{V4: []string{"1.0.0.0/8", "2.0.0.0/8"}, V6: []string{"2001:db8::/32"}}
	if s.Len() != 3 {
		t.Errorf("Len = %d, want 3", s.Len())
	}
}
