package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are verbatim output from a real bridge with two DHCP clients and
// one static address. Everything the parsers filter is noise that was actually
// there, not noise somebody imagined.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "devices", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseLeases_Golden(t *testing.T) {
	ls := ParseLeases(strings.NewReader(string(fixture(t, "leases.txt"))))
	if len(ls) != 2 {
		t.Fatalf("got %d leases, want 2: %+v", len(ls), ls)
	}
	byMAC := map[string]Lease{}
	for _, l := range ls {
		byMAC[l.MAC] = l
	}
	for mac, want := range map[string]struct{ ip, host string }{
		"b8:27:eb:aa:bb:01": {"10.88.0.141", "my-nas"},
		"b2:27:eb:aa:bb:02": {"10.88.0.163", "pixel-7"},
	} {
		got, ok := byMAC[mac]
		if !ok {
			t.Errorf("no lease for %s", mac)
			continue
		}
		if got.IP != want.ip || got.Hostname != want.host {
			t.Errorf("%s = %s/%s, want %s/%s", mac, got.IP, got.Hostname, want.ip, want.host)
		}
		if got.Expiry.IsZero() {
			t.Errorf("%s has no expiry", mac)
		}
	}
}

func TestParseLeases_SkipsMalformedLinesWithoutFailing(t *testing.T) {
	in := strings.Join([]string{
		"garbage",
		"notanumber b8:27:eb:aa:bb:01 10.0.0.1 host id",
		"1786480400 not-a-mac 10.0.0.2 host id",
		"1786480400 b8:27:eb:aa:bb:03 10.0.0.3 good id",
		"",
	}, "\n")
	ls := ParseLeases(strings.NewReader(in))
	if len(ls) != 1 || ls[0].Hostname != "good" {
		t.Fatalf("got %+v, want only the well-formed line", ls)
	}
}

// dnsmasq writes * when the client offered no hostname.
func TestParseLeases_StarMeansNoHostname(t *testing.T) {
	ls := ParseLeases(strings.NewReader("1786480400 b8:27:eb:aa:bb:01 10.0.0.1 * id\n"))
	if len(ls) != 1 || ls[0].Hostname != "" {
		t.Fatalf("got %+v, want an empty hostname", ls)
	}
}

func TestParseNeigh_Golden(t *testing.T) {
	ns, err := ParseNeigh(fixture(t, "neigh.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ns {
		if strings.Contains(n.IP, ":") {
			t.Errorf("IPv6 neighbour survived: %+v", n)
		}
	}
	// One MAC holds two IPv4 addresses here: a lease and a static.
	count := 0
	for _, n := range ns {
		if n.MAC == "b2:27:eb:aa:bb:02" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("got %d addresses for the dual-addressed client, want 2: %+v", count, ns)
	}
}

// IPv6 link-local survives on the bridge even with IPv6 off. It is never the
// address a client is reached on, and it would shadow the real one.
func TestParseNeigh_DropsLinkLocal(t *testing.T) {
	in := `[{"dst":"fe80::ba27:ebff:feaa:bb01","lladdr":"b8:27:eb:aa:bb:01","state":["STALE"]}]`
	ns, err := ParseNeigh([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 0 {
		t.Errorf("got %+v, want nothing", ns)
	}
}

func TestParseNeigh_DropsUnresolved(t *testing.T) {
	in := `[{"dst":"10.0.0.9","state":["FAILED"]},
	        {"dst":"10.0.0.8","lladdr":"b8:27:eb:aa:bb:01","state":["INCOMPLETE"]}]`
	ns, err := ParseNeigh([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 0 {
		t.Errorf("got %+v, want nothing — neither entry names a reachable device", ns)
	}
}

func TestParseFDB_Golden(t *testing.T) {
	es, err := ParseFDB(fixture(t, "fdb.json"), "capbr0")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 {
		t.Fatalf("got %d entries, want the 2 clients: %+v", len(es), es)
	}
	byMAC := map[string]FDBEntry{}
	for _, e := range es {
		byMAC[e.MAC] = e
	}
	for mac, port := range map[string]string{
		"b8:27:eb:aa:bb:01": "cv1",
		"b2:27:eb:aa:bb:02": "cv2",
	} {
		got, ok := byMAC[mac]
		if !ok {
			t.Errorf("client %s missing", mac)
			continue
		}
		if got.Port != port {
			t.Errorf("%s learned on %q, want %q", mac, got.Port, port)
		}
	}
}

// The ports' own MACs carry no "self" flag but are state "permanent", so
// filtering on the flag alone lets every bridge member through as a device.
func TestParseFDB_DropsPortOwnMACs(t *testing.T) {
	in := `[{"mac":"36:f0:53:82:40:db","ifname":"cv1","used":10,"updated":10,
	         "flags":[],"master":"br0","state":"permanent"}]`
	es, err := ParseFDB([]byte(in), "br0")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 0 {
		t.Errorf("a port's own MAC was reported as a device: %+v", es)
	}
}

func TestParseFDB_DropsMulticastAndForeignBridges(t *testing.T) {
	in := `[{"mac":"33:33:00:00:00:01","ifname":"br0","flags":["self"],"state":"permanent"},
	        {"mac":"01:00:5e:00:00:01","ifname":"br0","flags":["self"],"state":"permanent"},
	        {"mac":"b8:27:eb:aa:bb:09","ifname":"other1","used":1,"updated":1,
	         "flags":[],"master":"otherbr","state":""}]`
	es, err := ParseFDB([]byte(in), "br0")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 0 {
		t.Errorf("got %+v, want nothing", es)
	}
}

// Every vlan-carrying entry is duplicated, and the copies can disagree on age.
func TestParseFDB_DeduplicatesKeepingTheFreshest(t *testing.T) {
	in := `[{"mac":"b8:27:eb:aa:bb:01","ifname":"cv1","vlan":1,"used":9,"updated":7,
	         "flags":[],"master":"br0","state":""},
	        {"mac":"b8:27:eb:aa:bb:01","ifname":"cv1","used":9,"updated":3,
	         "flags":[],"master":"br0","state":""}]`
	es, err := ParseFDB([]byte(in), "br0")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(es), es)
	}
	if es[0].Updated != 3 {
		t.Errorf("Updated = %d, want the freshest sighting (3)", es[0].Updated)
	}
}

func TestParseFDB_EmptyOutputIsNotAnError(t *testing.T) {
	for _, in := range []string{"", "  ", "[]"} {
		if _, err := ParseFDB([]byte(in), "br0"); err != nil {
			t.Errorf("ParseFDB(%q) = %v", in, err)
		}
	}
}

func TestSanitizeHostname(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"my-nas", "my-nas"},
		{"pixel-7", "pixel-7"},
		{"host.lan", "host.lan"},
		{"WORK_PC", "WORK_PC"},
		{"*", ""},
		{"", ""},
		{"  ", ""},
		// A device list is a trust surface: these must not render as names.
		{"Routеr", ""},                // Cyrillic e
		{"admin‮", ""},                // RTL override
		{"a\x00b", ""},                // NUL
		{"a\nb", ""},                  // newline
		{"has space", ""},             // would also break the file format
		{strings.Repeat("a", 64), ""}, // over the label limit
		{strings.Repeat("a", 63), strings.Repeat("a", 63)},
	} {
		if got := SanitizeHostname(tc.in); got != tc.want {
			t.Errorf("SanitizeHostname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseAgeingTicks(t *testing.T) {
	// 30000 is the stock default, measured on the target.
	got, err := parseAgeingTicks("30000\n")
	if err != nil || got != 300 {
		t.Errorf("parseAgeingTicks(30000) = %d, %v, want 300", got, err)
	}
	// Ageing off means nothing ever expires, so no device could read offline.
	for _, in := range []string{"0", "-1", "nonsense", ""} {
		if _, err := parseAgeingTicks(in); err == nil {
			t.Errorf("parseAgeingTicks(%q) returned no error", in)
		}
	}
}

func TestBridgeAgeingSeconds_MissingBridgeIsAnError(t *testing.T) {
	if _, err := BridgeAgeingSeconds("definitely-not-a-bridge"); err == nil {
		t.Error("a missing bridge should be an error, not a silent zero")
	}
}
