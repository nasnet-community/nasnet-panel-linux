package xray

import (
	"testing"

	"github.com/xtls/xray-core/common/geodata"
)

// These guard the translation from configured matcher strings to xray-core's
// geodata.DomainRule / geodata.IPRule oneofs. Before the geodata refactor,
// "geosite:cn" was passed through as a flat router.Domain value for xray-core to
// split at load time; the prefix handling now lives in buildDomainRule, so a
// silent regression here would misroute traffic rather than fail loudly.

func TestBuildDomainRule_Geosite(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		typ       string
		wantFile  string
		wantCode  string
		wantAttrs string
	}{
		{"geosite prefix", "geosite:cn", "", geodata.DefaultGeoSiteDat, "CN", ""},
		{"geosite with attrs", "geosite:google@ads", "", geodata.DefaultGeoSiteDat, "GOOGLE", "ads"},
		{"geosite attrs lowercased", "geosite:google@ADS", "", geodata.DefaultGeoSiteDat, "GOOGLE", "ads"},
		{"ext file", "ext:custom.dat:mycode", "", "custom.dat", "MYCODE", ""},
		{"ext-site file", "ext-site:custom.dat:mycode", "", "custom.dat", "MYCODE", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDomainRule(tt.value, tt.typ)
			geo, ok := got.Value.(*geodata.DomainRule_Geosite)
			if !ok {
				t.Fatalf("expected geosite branch, got %T", got.Value)
			}
			if geo.Geosite.File != tt.wantFile {
				t.Errorf("File = %q, want %q", geo.Geosite.File, tt.wantFile)
			}
			if geo.Geosite.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", geo.Geosite.Code, tt.wantCode)
			}
			if geo.Geosite.Attrs != tt.wantAttrs {
				t.Errorf("Attrs = %q, want %q", geo.Geosite.Attrs, tt.wantAttrs)
			}
		})
	}
}

func TestBuildDomainRule_Custom(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		typ      string
		wantType geodata.Domain_Type
	}{
		// Empty type must map to Substr, the rename of the old Domain_Plain
		// default. Both are wire value 0.
		{"default is substr", "example.com", "", geodata.Domain_Substr},
		{"explicit domain", "example.com", "domain", geodata.Domain_Domain},
		{"explicit full", "example.com", "full", geodata.Domain_Full},
		{"explicit regex", "^ex.*$", "regex", geodata.Domain_Regex},
		{"type is case insensitive", "example.com", "FULL", geodata.Domain_Full},
		{"unknown type falls back to substr", "example.com", "bogus", geodata.Domain_Substr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDomainRule(tt.value, tt.typ)
			custom, ok := got.Value.(*geodata.DomainRule_Custom)
			if !ok {
				t.Fatalf("expected custom branch, got %T", got.Value)
			}
			if custom.Custom.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", custom.Custom.Type, tt.wantType)
			}
			if custom.Custom.Value != tt.value {
				t.Errorf("Value = %q, want %q", custom.Custom.Value, tt.value)
			}
		})
	}
}

func TestBuildDomainRule_PlainDefaultIsWireZero(t *testing.T) {
	// The old default was router.Domain_Plain. If Domain_Substr ever stops being
	// 0, previously-generated configs would change meaning.
	if geodata.Domain_Substr != 0 {
		t.Errorf("Domain_Substr = %d, want 0", geodata.Domain_Substr)
	}
}

func TestBuildIPRule_Geoip(t *testing.T) {
	got := buildIPRule("geoip:ir")
	geo, ok := got.Value.(*geodata.IPRule_Geoip)
	if !ok {
		t.Fatalf("expected geoip branch, got %T", got.Value)
	}
	if geo.Geoip.File != geodata.DefaultGeoIPDat {
		t.Errorf("File = %q, want %q", geo.Geoip.File, geodata.DefaultGeoIPDat)
	}
	if geo.Geoip.Code != "IR" {
		t.Errorf("Code = %q, want IR", geo.Geoip.Code)
	}
}

func TestBuildIPRule_CustomCIDR(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantPrefix uint32
	}{
		{"explicit prefix", "10.0.0.0/8", 8},
		{"bare ipv4 defaults to /32", "1.2.3.4", 32},
		{"bare ipv6 defaults to /128", "2001:db8::1", 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildIPRule(tt.value)
			custom, ok := got.Value.(*geodata.IPRule_Custom)
			if !ok {
				t.Fatalf("expected custom branch, got %T", got.Value)
			}
			if custom.Custom.Cidr.Prefix != tt.wantPrefix {
				t.Errorf("Prefix = %d, want %d", custom.Custom.Cidr.Prefix, tt.wantPrefix)
			}
			if len(custom.Custom.Cidr.Ip) == 0 {
				t.Errorf("Ip is empty for %q", tt.value)
			}
		})
	}
}
