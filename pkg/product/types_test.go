package product

import "testing"

func TestProductType_IsValid(t *testing.T) {
	valid := []ProductType{ProductTypeXray, ProductTypeOpenVPN, ProductTypeWireGuard}
	for _, p := range valid {
		if !p.IsValid() {
			t.Errorf("%q should be valid", p)
		}
	}
	for _, p := range []ProductType{"", "ssh", "unknown"} {
		if p.IsValid() {
			t.Errorf("%q should NOT be valid", p)
		}
	}
}

func TestProductType_String(t *testing.T) {
	if got := ProductTypeXray.String(); got != "xray" {
		t.Errorf("String() = %q, want xray", got)
	}
}
