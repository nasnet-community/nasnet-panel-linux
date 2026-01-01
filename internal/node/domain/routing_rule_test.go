package domain

import (
	"reflect"
	"testing"
)

func TestDomainMatcherSlice_Value_Nil(t *testing.T) {
	var d DomainMatcherSlice
	v, err := d.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != nil {
		t.Errorf("nil slice should marshal to nil driver value, got %v", v)
	}
}

func TestDomainMatcherSlice_ValueScanRoundTrip(t *testing.T) {
	orig := DomainMatcherSlice{
		{Type: DomainTypeFull, Value: "a.com"},
		{Type: DomainTypeRegex, Value: ".*\\.b\\.com"},
	}
	v, err := orig.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var back DomainMatcherSlice
	if err := back.Scan(v.([]byte)); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", back, orig)
	}
}

func TestDomainMatcherSlice_Scan(t *testing.T) {
	// nil clears the slice.
	d := DomainMatcherSlice{{Type: "plain", Value: "x"}}
	if err := d.Scan(nil); err != nil || d != nil {
		t.Errorf("Scan(nil) = %v, slice=%v", err, d)
	}

	// string payload is accepted alongside []byte.
	var fromStr DomainMatcherSlice
	if err := fromStr.Scan(`[{"type":"full","value":"a.com"}]`); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if len(fromStr) != 1 || fromStr[0].Value != "a.com" {
		t.Errorf("string scan = %+v", fromStr)
	}

	// unsupported types are rejected.
	var bad DomainMatcherSlice
	if err := bad.Scan(123); err == nil {
		t.Error("Scan(int) should error")
	}
}

func TestRoutingRule_GetMatcherSummary(t *testing.T) {
	if got := (&RoutingRule{}).GetMatcherSummary(); got != "none" {
		t.Errorf("empty rule summary = %q, want none", got)
	}

	r := &RoutingRule{
		DomainRules: DomainMatcherSlice{{Type: "full", Value: "a.com"}},
		GeoIPRules:  []string{"geoip:cn"},
		PortRules:   []string{"443"},
	}
	if got := r.GetMatcherSummary(); got != "domains, geoip, ports" {
		t.Errorf("summary = %q", got)
	}
}
