// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"errors"
	"io"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// goodReport is what the script writes when everything is up, with a time two
// minutes before now.
func goodReport() string {
	return strings.Join([]string{
		"# written by the router script",
		"domestic=1",
		"domestic_addr=192.168.1.20",
		"vpn=1",
		"vpn_kind=wireguard",
		"vpn_label=wg-vpn",
		"route_foreign=1",
		"route_vpn=1",
		"route_domestic=1",
		"ros=7.24.5",
		"free_mem=412000000",
		"free_disk=9000000",
		"updated=" + strconv.FormatInt(now.Add(-2*time.Minute).Unix(), 10),
		"",
	}, "\n")
}

func mustParse(t *testing.T, s string) Facts {
	t.Helper()
	f, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAWholeReport(t *testing.T) {
	got := mustParse(t, goodReport())
	want := Facts{
		Domestic: true, DomesticAddr: netip.MustParseAddr("192.168.1.20"), VPN: true, VPNKind: "wireguard", VPNLabel: "wg-vpn",
		RouteForeign: true, RouteVPN: true, RouteDomestic: true,
		ROS:     "7.24.5",
		FreeMem: Size{412000000, true}, FreeDisk: Size{9000000, true},
		Updated: now.Add(-2 * time.Minute),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestParsingLeavesNothingToTheLaterLines(t *testing.T) {
	for name, c := range map[string]struct {
		in   string
		want Facts
		bad  int
	}{
		"empty":       {"", Facts{}, 0},
		"only blanks": {"\n\n  \n\t\n", Facts{}, 0},
		"comments and blank lines": {
			"# top\n\n   # indented\ndomestic=1\n\n\t# after a tab\nvpn=1\n",
			Facts{Domestic: true, VPN: true}, 0,
		},
		"unknown keys are ignored, not counted": {
			"future_key=1\nanother=hello\nDomestic=1\ndomestic=1\n",
			Facts{Domestic: true}, 0, // keys are lower case, so Domestic is another key
		},
		"duplicate keys: the last wins": {
			"domestic=1\ndomestic=0\nvpn=0\nvpn=1\nvpn_label=a\nvpn_label=b\nros=7.11\nros=7.12\n",
			Facts{VPN: true, VPNLabel: "b", ROS: "7.12"}, 0,
		},
		"CRLF line endings": {
			"# note\r\ndomestic=1\r\nvpn=1\r\nvpn_label=wg\r\nupdated=1793361600\r\n",
			Facts{Domestic: true, VPN: true, VPNLabel: "wg", Updated: time.Unix(1793361600, 0).UTC()}, 0,
		},
		"no newline at the end": {"domestic=1\nvpn=1", Facts{Domestic: true, VPN: true}, 0},
		"spaces around the key and the value": {
			"  domestic = 1  \nvpn_label =  my vpn \n", Facts{Domestic: true, VPNLabel: "my vpn"}, 0,
		},
		"a value may hold an equals sign": {"vpn_label=a=b\n", Facts{VPNLabel: "a=b"}, 0},
		"a byte order mark":               {"\xef\xbb\xbfdomestic=1\nvpn=1\n", Facts{Domestic: true, VPN: true}, 0},
		"a mark is not a comment": {
			"domestic=1 # it's up\n", Facts{}, 1, // the whole rest of the line is the value
		},
		"a zero is a value": {
			"free_disk=0\nfree_mem=0\nupdated=0\n",
			Facts{FreeDisk: Size{0, true}, FreeMem: Size{0, true}, Updated: time.Unix(0, 0).UTC()}, 0,
		},
		"the last second that counts as a time": {
			"updated=4102444800\n", Facts{Updated: time.Unix(4102444800, 0).UTC()}, 0,
		},
		"lines that make no sense are skipped and counted": {
			strings.Join([]string{
				"no equals sign here",
				"=no key",
				"domestic=yes",
				"vpn=2",
				"route_vpn=",
				"route_foreign=true",
				"route_domestic=01",
				"free_mem=-5",
				"free_mem=12abc",
				"free_mem=1.5",
				"free_disk=",
				"updated=soon",
				"updated=-1",
				"updated=4102444801", // milliseconds, not seconds
				"updated=1793361600000",
				"domestic=1",
			}, "\n"),
			Facts{Domestic: true}, 15,
		},
		"a bad line leaves what an earlier one said": {
			"domestic=1\ndomestic=oops\nfree_mem=5\nfree_mem=lots\nupdated=1793361600\nupdated=junk\n",
			Facts{Domestic: true, FreeMem: Size{5, true}, Updated: time.Unix(1793361600, 0).UTC()}, 3,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := mustParse(t, c.in)
			if got.BadLines != c.bad {
				t.Errorf("BadLines = %d, want %d", got.BadLines, c.bad)
			}
			got.BadLines = 0
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestTheVPNKindIsOneTheServerKnows(t *testing.T) {
	for in, want := range map[string]string{
		"wireguard": "wireguard", "l2tp": "l2tp", "openvpn": "openvpn", "other": "other",
		"WireGuard": "wireguard", "OpenVPN": "openvpn",
		"sstp": "other", "ovpn-out1": "other", "": "",
	} {
		if got := mustParse(t, "vpn_kind="+in+"\n").VPNKind; got != want {
			t.Errorf("vpn_kind=%q: %q, want %q", in, got, want)
		}
	}
}

func TestTextFromTheRouterIsMadeSafeToShow(t *testing.T) {
	long := strings.Repeat("ب", 100)
	for name, c := range map[string]struct{ in, label string }{
		"control characters":  {"wg\x00\x07-vpn\x1b[0m", "wg-vpn[0m"},
		"invalid UTF-8":       {"wg\xff\xfe-vpn", "wg�-vpn"},
		"longer than allowed": {long, long[:128]}, // 64 characters of two bytes
		"Persian is kept":     {"وی‌پی‌ان", "وی‌پی‌ان"},
	} {
		got := mustParse(t, "vpn_label="+c.in+"\n").VPNLabel
		if got != c.label {
			t.Errorf("%s: %q, want %q", name, got, c.label)
		}
	}
	if got := mustParse(t, "ros="+strings.Repeat("7", 100)+"\n").ROS; len(got) != 32 {
		t.Errorf("a version of 100 characters kept %d", len(got))
	}
}

func TestParseRefusesAReportThatIsFarTooLong(t *testing.T) {
	atLimit := "#" + strings.Repeat("x", maxFile-1)
	if _, err := Parse(strings.NewReader(atLimit)); err != nil {
		t.Fatalf("a report of exactly %d bytes: %v", maxFile, err)
	}
	if _, err := Parse(strings.NewReader(atLimit + "x")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a report one byte over: %v, want ErrTooLarge", err)
	}
	// and it stops reading rather than taking all of it
	r := &countingReader{r: strings.NewReader(strings.Repeat("a", 10<<20))}
	if _, err := Parse(r); !errors.Is(err, ErrTooLarge) || r.n > maxFile+4096 {
		t.Fatalf("err = %v after reading %d bytes of 10 MiB", err, r.n)
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestAReaderThatFailsFailsTheParse(t *testing.T) {
	boom := errors.New("boom")
	f, err := Parse(io.MultiReader(strings.NewReader("domestic=1\nvpn=1\n"), iotest.ErrReader(boom)))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the reader's", err)
	}
	if !reflect.DeepEqual(f, Facts{}) {
		t.Fatalf("a failed read left facts behind: %+v", f)
	}
}

func TestTheDomesticAddressIsAnIPv4AddressOrNothing(t *testing.T) {
	for _, ok := range []string{
		"192.168.1.20", "10.0.0.2", "100.64.12.7", "203.0.113.9", "8.8.8.8", "172.16.0.1",
		"192.168.1.255", // a directed broadcast is nothing a script would send, and no worse than any other
	} {
		for _, in := range []string{"domestic_addr=" + ok + "\n", "  domestic_addr =  " + ok + "  \r\n", "\xef\xbb\xbfdomestic_addr=" + ok} {
			f := mustParse(t, in)
			if f.DomesticAddr != netip.MustParseAddr(ok) || f.BadLines != 0 {
				t.Errorf("%q: addr %v, %d bad lines", in, f.DomesticAddr, f.BadLines)
			}
		}
	}

	// the router says it has none, or its script predates the key: nothing, and nothing wrong
	for name, in := range map[string]string{
		"empty":                "domestic_addr=\n",
		"blank":                "domestic_addr=   \n",
		"no value at all":      "domestic=1\nvpn=1\n",
		"an empty report":      "",
		"next to a link down":  "domestic=0\ndomestic_addr=\n",
		"an unknown key by it": "domestic_addr6=2001:db8::1\ndomestic_address=192.168.1.20\nDomestic_Addr=192.168.1.20\n",
	} {
		if f := mustParse(t, in); f.DomesticAddr.IsValid() || f.BadLines != 0 {
			t.Errorf("%s: addr %v, %d bad lines", name, f.DomesticAddr, f.BadLines)
		}
	}

	// anything else is not an address, which counts as not knowing it
	for _, bad := range []string{
		"192.168.1.20/24", "192.168.1.20/32", "192.168.1", "192.168.1.256", "192.168.1.20.5", "192.168.1.", "192.168..1",
		"192.168.001.020", "01.2.3.4", "0x7f.0.0.1", "+1.2.3.4", "-1.2.3.4", " 1 .2.3.4", "1.2.3.4 5", "192.168.1.20,192.168.1.21",
		"192.168.1.20%eth0", "192.168.1.20:8080", "[192.168.1.20]",
		"fe80::1", "2001:db8::1", "::ffff:192.168.1.20", "::1", "::",
		"0.0.0.0", "127.0.0.1", "169.254.7.7", "224.0.0.1", "239.255.255.250", "255.255.255.255",
		"none", "unknown", "yes", "1", "0", "true", "192.168.1.20 # up", "\u0661\u0669\u0662.168.1.20",
	} {
		f := mustParse(t, "domestic=1\ndomestic_addr="+bad+"\nvpn=1\n")
		if f.DomesticAddr.IsValid() || f.BadLines != 1 {
			t.Errorf("%q: addr %v, %d bad lines, want none known and one bad line", bad, f.DomesticAddr, f.BadLines)
		}
		if !f.Domestic || !f.VPN {
			t.Errorf("%q: the lines around it were lost: %+v", bad, f)
		}
	}
}

// A second line for the address replaces the first, and a bad one leaves it
// unknown instead of keeping the first's. The other keys keep an earlier line
// through a bad one; for this one a stale address is worse than none, since an
// address that seems to have changed back is what the hub restarts a node for.
func TestAnAddressLineThatFailsLeavesTheAddressUnknown(t *testing.T) {
	for name, c := range map[string]struct {
		in   string
		want string // "" is unknown
		bad  int
	}{
		"the last good one wins":    {"domestic_addr=10.0.0.1\ndomestic_addr=10.0.0.2\n", "10.0.0.2", 0},
		"a bad one after a good":    {"domestic_addr=10.0.0.1\ndomestic_addr=oops\n", "", 1},
		"an empty one after":        {"domestic_addr=10.0.0.1\ndomestic_addr=\n", "", 0},
		"a good one after a bad":    {"domestic_addr=oops\ndomestic_addr=10.0.0.2\n", "10.0.0.2", 1},
		"a good one after an empty": {"domestic_addr=\ndomestic_addr=10.0.0.2\n", "10.0.0.2", 0},
	} {
		f := mustParse(t, c.in)
		got := ""
		if f.DomesticAddr.IsValid() {
			got = f.DomesticAddr.String()
		}
		if got != c.want || f.BadLines != c.bad {
			t.Errorf("%s: addr %q with %d bad lines, want %q and %d", name, got, f.BadLines, c.want, c.bad)
		}
	}
}

// The address is a fact of its own: it doesn't turn the entry on or off, and
// a report without one is as good as before.
func TestTheAddressDoesNotChangeWhatTheEntrySays(t *testing.T) {
	with := mustParse(t, goodReport())
	without := mustParse(t, strings.Replace(goodReport(), "domestic_addr=192.168.1.20\n", "", 1))
	if with.DomesticAddr.IsValid() == without.DomesticAddr.IsValid() {
		t.Fatal("the two reports should differ in the address")
	}
	a, b := with.Entry(now, MinRouterOS), without.Entry(now, MinRouterOS)
	if !reflect.DeepEqual(a, b) || !a.On {
		t.Errorf("entry with the address %+v, without %+v", a, b)
	}
}
