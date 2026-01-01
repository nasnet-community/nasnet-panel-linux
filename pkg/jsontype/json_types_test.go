package jsontype

import (
	"reflect"
	"testing"
)

func TestStringSlice_Value_Nil(t *testing.T) {
	var s StringSlice
	v, err := s.Value()
	if err != nil || v != nil {
		t.Errorf("nil slice = (%v, %v), want (nil, nil)", v, err)
	}
}

func TestStringSlice_ValueScanRoundTrip(t *testing.T) {
	orig := StringSlice{"a", "b", "c"}
	v, err := orig.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var back StringSlice
	if err := back.Scan(v.([]byte)); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("round trip = %v, want %v", back, orig)
	}
}

func TestStringSlice_Scan(t *testing.T) {
	// nil scan clears the slice.
	s := StringSlice{"existing"}
	if err := s.Scan(nil); err != nil || s != nil {
		t.Errorf("Scan(nil) = %v, slice=%v", err, s)
	}

	// string payload is accepted alongside []byte (some drivers return string).
	var fromStr StringSlice
	if err := fromStr.Scan(`["x","y"]`); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if !reflect.DeepEqual(fromStr, StringSlice{"x", "y"}) {
		t.Errorf("string scan = %v", fromStr)
	}

	// unsupported types are rejected before json.Unmarshal sees them.
	var bad StringSlice
	if err := bad.Scan(123); err == nil {
		t.Error("Scan(int) should error")
	}
}

func TestStringSlice_Contains(t *testing.T) {
	s := StringSlice{"alpha", "beta"}
	if !s.Contains("alpha") {
		t.Error("alpha missing from slice")
	}
	if s.Contains("gamma") {
		t.Error("gamma reported present")
	}
	if (StringSlice{}).Contains("anything") {
		t.Error("empty slice claims to contain values")
	}
}

func TestStringMap_Value_Nil(t *testing.T) {
	var m StringMap
	v, err := m.Value()
	if err != nil || v != nil {
		t.Errorf("nil map = (%v, %v), want (nil, nil)", v, err)
	}
}

func TestStringMap_ValueScanRoundTrip(t *testing.T) {
	orig := StringMap{"k1": "v1", "k2": "v2"}
	v, err := orig.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var back StringMap
	if err := back.Scan(v.([]byte)); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("round trip = %v, want %v", back, orig)
	}
}

func TestStringMap_Scan(t *testing.T) {
	m := StringMap{"old": "v"}
	if err := m.Scan(nil); err != nil || m != nil {
		t.Errorf("Scan(nil) = %v, map=%v", err, m)
	}

	var fromStr StringMap
	if err := fromStr.Scan(`{"a":"1"}`); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if !reflect.DeepEqual(fromStr, StringMap{"a": "1"}) {
		t.Errorf("string scan = %v", fromStr)
	}

	var bad StringMap
	if err := bad.Scan(3.14); err == nil {
		t.Error("Scan(float) should error")
	}
}
