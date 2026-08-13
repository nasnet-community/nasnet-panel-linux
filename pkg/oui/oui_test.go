package oui

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff"},
		{"aabb.ccdd.eeff", "aa:bb:cc:dd:ee:ff"},
		{"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:dd:ee:ff"},
		{"", ""},
		{"not-a-mac", ""},
		{"aa:bb:cc", ""},
		// 20-octet infiniband parses but is not a device MAC.
		{"00:00:00:00:fe:80:00:00:00:00:00:00:02:00:5e:10:00:00:00:01", ""},
	} {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsRandomized(t *testing.T) {
	// Second-least-significant bit of the first octet.
	for _, tc := range []struct {
		mac  string
		want bool
	}{
		{"00:1a:2b:3c:4d:5e", false}, // globally unique
		{"02:1a:2b:3c:4d:5e", true},  // locally administered
		{"aa:bb:cc:dd:ee:ff", true},  // 0xaa = 10101010
		{"b8:27:eb:00:11:22", false}, // Raspberry Pi
		{"", false},
	} {
		if got := IsRandomized(tc.mac); got != tc.want {
			t.Errorf("IsRandomized(%q) = %v, want %v", tc.mac, got, tc.want)
		}
	}
}

func TestIsGroup(t *testing.T) {
	// The FDB is full of these; they are never devices.
	for _, tc := range []struct {
		mac  string
		want bool
	}{
		{"33:33:00:00:00:01", true}, // IPv6 multicast
		{"01:00:5e:00:00:01", true}, // IPv4 multicast
		{"ff:ff:ff:ff:ff:ff", true}, // broadcast
		{"b8:27:eb:00:11:22", false},
	} {
		if got := IsGroup(tc.mac); got != tc.want {
			t.Errorf("IsGroup(%q) = %v, want %v", tc.mac, got, tc.want)
		}
	}
}

func TestLookup(t *testing.T) {
	// Long-standing MA-L assignments. If the registry ever drops one the
	// generator's own floor check would have failed first.
	for _, tc := range []struct{ mac, want string }{
		{"b8:27:eb:11:22:33", "Raspberry Pi Foundation"},
		{"00:00:00:11:22:33", "XEROX CORPORATION"},
	} {
		got, ok := Lookup(tc.mac)
		if !ok || got != tc.want {
			t.Errorf("Lookup(%q) = %q,%v, want %q", tc.mac, got, ok, tc.want)
		}
	}
}

// A locally-administered prefix may collide with a real assignment. Reporting
// that as the vendor would be a coincidence presented as fact.
func TestLookup_RandomizedNeverResolves(t *testing.T) {
	if v, ok := Lookup("02:00:00:11:22:33"); ok {
		t.Errorf("randomized MAC resolved to %q", v)
	}
	// 0x02 set on a prefix that is otherwise a real assignment.
	if v, ok := Lookup("ba:27:eb:11:22:33"); ok {
		t.Errorf("randomized MAC resolved to %q", v)
	}
}

func TestLookup_UnknownAndMalformed(t *testing.T) {
	for _, mac := range []string{"", "nonsense", "aa:bb"} {
		if _, ok := Lookup(mac); ok {
			t.Errorf("Lookup(%q) reported a vendor", mac)
		}
	}
}

// The embedded table must actually be there: an empty one degrades silently to
// "no vendor known" for every device.
func TestTableIsPopulated(t *testing.T) {
	once.Do(load)
	if len(table) < 30000 {
		t.Fatalf("table has %d entries; the embedded registry looks truncated", len(table))
	}
}
