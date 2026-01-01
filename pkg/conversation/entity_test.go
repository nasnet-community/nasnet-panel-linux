package conversation

import (
	"reflect"
	"testing"
)

func TestJSONMap_ValueScanRoundTrip(t *testing.T) {
	orig := JSONMap{"k1": "v1", "n": float64(42)}
	v, err := orig.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var back JSONMap
	if err := back.Scan(v.([]byte)); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("round trip mismatch: got %+v, want %+v", back, orig)
	}
}

// Scan(nil) yields an empty map rather than a nil one so callers can read
// from it without a nil check.
func TestJSONMap_Scan_NilGivesEmptyMap(t *testing.T) {
	var m JSONMap
	if err := m.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if m == nil {
		t.Error("expected non-nil empty map")
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %+v", m)
	}
}

func TestJSONMap_Scan_StringPayload(t *testing.T) {
	var m JSONMap
	if err := m.Scan(`{"a":1}`); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if v, ok := m["a"].(float64); !ok || v != 1 {
		t.Errorf("scan got %+v", m)
	}
}

func TestJSONMap_Scan_UnsupportedType(t *testing.T) {
	var m JSONMap
	if err := m.Scan(42); err == nil {
		t.Error("Scan(int) should error")
	}
}
