// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

// validSpec is a spec the hub could send: two addresses on the router's veth,
// one node, public resolvers.
func validSpec() *Spec {
	return &Spec{
		Version: Version,
		Nodes: []Node{{
			Tag:        "n1",
			Addr:       "203.0.113.10",
			Port:       443,
			UUID:       "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f",
			PublicKey:  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)),
			ShortID:    "0123456789abcdef",
			ServerName: "www.example.com",
			Flow:       VisionFlow,
			ReverseTag: "rvs-from-n1",
		}},
		DialAddr:   "192.168.50.18",
		Exit:       Exit{Addr: "192.168.50.17"},
		DNS:        DNS{Servers: []string{"1.1.1.1", "8.8.8.8:53"}},
		StatusAddr: "127.0.0.1:18080",
	}
}

// extraNode is a second node that clashes with nothing in validSpec.
func extraNode(i int) Node {
	n := validSpec().Nodes[0]
	n.Tag = fmt.Sprintf("n%d", i)
	n.ReverseTag = fmt.Sprintf("rvs-from-n%d", i)
	n.Addr = fmt.Sprintf("203.0.113.%d", 10+i)
	return n
}

func TestValidSpec(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
		want   string // part of the error
	}{
		{"wrong version", func(s *Spec) { s.Version = 2 }, "version"},
		{"no version", func(s *Spec) { s.Version = 0 }, "version"},
		{"unknown log level", func(s *Spec) { s.LogLevel = "verbose" }, "log_level"},

		{"no nodes", func(s *Spec) { s.Nodes = nil }, "nodes: none"},
		{"too many nodes", func(s *Spec) {
			s.Nodes = nil
			for i := 0; i <= MaxNodes; i++ {
				s.Nodes = append(s.Nodes, extraNode(i))
			}
		}, "more than"},
		{"duplicate tag", func(s *Spec) {
			n := extraNode(2)
			n.Tag = "n1"
			s.Nodes = append(s.Nodes, n)
		}, "already used"},
		{"duplicate reverse tag", func(s *Spec) {
			n := extraNode(2)
			n.ReverseTag = "rvs-from-n1"
			s.Nodes = append(s.Nodes, n)
		}, "already used"},
		{"reverse tag is the node's tag", func(s *Spec) { s.Nodes[0].ReverseTag = "n1" }, "already used"},
		{"tag is the exit's", func(s *Spec) { s.Nodes[0].Tag = ExitTag }, "already used"},
		{"reverse tag is the dns tag", func(s *Spec) { s.Nodes[0].ReverseTag = DNSTag }, "already used"},
		{"tag with a space", func(s *Spec) { s.Nodes[0].Tag = "n 1" }, "lowercase"},
		{"tag in capitals", func(s *Spec) { s.Nodes[0].Tag = "N1" }, "lowercase"},
		{"empty tag", func(s *Spec) { s.Nodes[0].Tag = "" }, "lowercase"},
		{"empty reverse tag", func(s *Spec) { s.Nodes[0].ReverseTag = "" }, "lowercase"},

		{"portal is a name", func(s *Spec) { s.Nodes[0].Addr = "portal.example.com" }, "not an IP"},
		{"portal missing", func(s *Spec) { s.Nodes[0].Addr = "" }, "missing"},
		{"portal unspecified", func(s *Spec) { s.Nodes[0].Addr = "0.0.0.0" }, "unicast"},
		{"portal with a zone", func(s *Spec) { s.Nodes[0].Addr = "fe80::1%eth0" }, "not an IP"},
		{"portal in the other family", func(s *Spec) { s.Nodes[0].Addr = "2001:db8::1" }, "address family"},
		{"port missing", func(s *Spec) { s.Nodes[0].Port = 0 }, "port: missing"},
		{"two nodes at one portal", func(s *Spec) {
			n := extraNode(2)
			n.Addr = "203.0.113.10"
			s.Nodes = append(s.Nodes, n)
		}, "already used"},

		{"uuid not a uuid", func(s *Spec) { s.Nodes[0].UUID = "not-a-uuid" }, "not a UUID"},
		{"uuid missing", func(s *Spec) { s.Nodes[0].UUID = "" }, "not a UUID"},
		{"uuid without dashes", func(s *Spec) { s.Nodes[0].UUID = "7f8b1a9e3c2d4b5a9e6f0a1b2c3d4e5f" }, "not a UUID"},
		{"public key missing", func(s *Spec) { s.Nodes[0].PublicKey = "" }, "public_key"},
		{"public key too short", func(s *Spec) {
			s.Nodes[0].PublicKey = base64.RawURLEncoding.EncodeToString(make([]byte, 31))
		}, "public_key"},
		{"public key padded", func(s *Spec) {
			s.Nodes[0].PublicKey = base64.URLEncoding.EncodeToString(make([]byte, 32))
		}, "public_key"},
		{"short id missing", func(s *Spec) { s.Nodes[0].ShortID = "" }, "short_id"},
		{"short id odd", func(s *Spec) { s.Nodes[0].ShortID = "abc" }, "short_id"},
		{"short id too long", func(s *Spec) { s.Nodes[0].ShortID = "0123456789abcdef01" }, "short_id"},
		{"short id not hex", func(s *Spec) { s.Nodes[0].ShortID = "zz" }, "short_id"},
		{"server name missing", func(s *Spec) { s.Nodes[0].ServerName = "" }, "server_name"},
		{"server name is an address", func(s *Spec) { s.Nodes[0].ServerName = "203.0.113.5" }, "server_name"},
		{"server name with a space", func(s *Spec) { s.Nodes[0].ServerName = "www example.com" }, "server_name"},
		{"server name label ends in a hyphen", func(s *Spec) { s.Nodes[0].ServerName = "www-.example.com" }, "server_name"},
		{"server name with an empty label", func(s *Spec) { s.Nodes[0].ServerName = "www..example.com" }, "server_name"},
		{"server name too long", func(s *Spec) { s.Nodes[0].ServerName = strings.Repeat("a.", 130) + "com" }, "server_name"},
		{"fingerprint in capitals", func(s *Spec) { s.Nodes[0].Fingerprint = "Chrome" }, "fingerprint"},
		{"flow not vision", func(s *Spec) { s.Nodes[0].Flow = "xtls-rprx-direct" }, "flow"},

		{"dial missing", func(s *Spec) { s.DialAddr = "" }, "dial_addr: missing"},
		{"dial is a name", func(s *Spec) { s.DialAddr = "router.lan" }, "dial_addr"},
		{"exit missing", func(s *Spec) { s.Exit.Addr = "" }, "exit.addr: missing"},
		{"exit equals dial", func(s *Spec) { s.Exit.Addr = s.DialAddr }, "same as dial_addr"},

		{"blocked entry is not a range", func(s *Spec) { s.Exit.Blocked = []string{"10.0.0.0"} }, "exit.blocked[0]"},
		{"blocked entry is junk", func(s *Spec) { s.Exit.Blocked = []string{"10.0.0.0/8", "lan"} }, "exit.blocked[1]"},
		{"blocked entry is IPv4-mapped", func(s *Spec) { s.Exit.Blocked = []string{"::ffff:10.0.0.0/104"} }, "IPv4-mapped"},
		{"too many blocked", func(s *Spec) {
			s.Exit.Blocked = nil
			for i := 0; i <= MaxBlocked; i++ {
				s.Exit.Blocked = append(s.Exit.Blocked, fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
			}
		}, "more than"},

		{"no resolvers", func(s *Spec) { s.DNS.Servers = nil }, "dns.servers: none"},
		{"too many resolvers", func(s *Spec) {
			s.DNS.Servers = nil
			for i := 0; i <= MaxDNSServers; i++ {
				s.DNS.Servers = append(s.DNS.Servers, fmt.Sprintf("9.9.9.%d", i+1))
			}
		}, "more than"},
		{"resolver is a name", func(s *Spec) { s.DNS.Servers = []string{"dns.google"} }, "not an IP"},
		{"resolver is localhost", func(s *Spec) { s.DNS.Servers = []string{"localhost"} }, "not an IP"},
		{"resolver with a scheme", func(s *Spec) { s.DNS.Servers = []string{"tcp://1.1.1.1"} }, "not an IP"},
		{"resolver port 0", func(s *Spec) { s.DNS.Servers = []string{"1.1.1.1:0"} }, "not an IP"},
		{"resolver multicast", func(s *Spec) { s.DNS.Servers = []string{"224.0.0.251"} }, "unicast"},
		{"resolver in the other family", func(s *Spec) { s.DNS.Servers = []string{"2606:4700:4700::1111"} }, "address family"},
		{"resolver in the LAN", func(s *Spec) { s.DNS.Servers = []string{"192.168.88.1"} }, "refuse"},
		{"resolver in a custom blocked range", func(s *Spec) {
			s.Exit.Blocked = []string{"1.0.0.0/8"}
		}, "refuse"},

		{"status address missing", func(s *Spec) { s.StatusAddr = "" }, "status_addr"},
		{"status without a port", func(s *Spec) { s.StatusAddr = "127.0.0.1" }, "status_addr"},
		{"status on port 0", func(s *Spec) { s.StatusAddr = "127.0.0.1:0" }, "loopback address with a port"},
		{"status off loopback", func(s *Spec) { s.StatusAddr = "192.168.50.18:8080" }, "loopback"},
		{"status on all interfaces", func(s *Spec) { s.StatusAddr = "0.0.0.0:8080" }, "loopback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSpec()
			tt.mutate(s)
			err := s.Validate()
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"every field", func(s *Spec) {
			s.Nodes = append(s.Nodes, extraNode(2), extraNode(3))
			s.Nodes[1].Fingerprint = "firefox"
			s.Nodes[2].Flow = ""
			s.LogLevel = "debug"
			s.Exit.Blocked = []string{"10.0.0.0/8", "192.168.0.0/16", "fc00::/7"}
			s.DNS.Servers = []string{"9.9.9.9", "1.1.1.1:5353"}
		}},
		{"loopback shares one address", func(s *Spec) {
			s.DialAddr, s.Exit.Addr = "127.0.0.1", "127.0.0.1"
			s.Nodes[0].Addr = "127.0.0.1"
			s.Exit.Blocked = []string{"10.0.0.0/8"}
			s.DNS.Servers = []string{"127.0.0.1:5353"}
		}},
		{"IPv6 end to end", func(s *Spec) {
			s.DialAddr, s.Exit.Addr = "fd00::18", "fd00::17"
			s.Nodes[0].Addr = "2001:db8::1"
			s.DNS.Servers = []string{"2606:4700:4700::1111", "[2001:4860:4860::8888]:53"}
			s.StatusAddr = "[::1]:18080"
		}},
		{"IPv4-mapped addresses count as IPv4", func(s *Spec) { s.Nodes[0].Addr = "::ffff:203.0.113.10" }},
		{"a node's id in capitals", func(s *Spec) { s.Nodes[0].UUID = strings.ToUpper(s.Nodes[0].UUID) }},
		{"short id of one byte", func(s *Spec) { s.Nodes[0].ShortID = "ab" }},
		{"single-label server name", func(s *Spec) { s.Nodes[0].ServerName = "localhost" }},
		{"a resolver outside a narrower blocked list", func(s *Spec) {
			s.Exit.Blocked = []string{"192.168.0.0/16"}
			s.DNS.Servers = []string{"10.1.2.3"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSpec()
			tt.mutate(s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	s := validSpec()
	if got := s.Nodes[0].FingerprintOrDefault(); got != "chrome" {
		t.Errorf("fingerprint default = %q", got)
	}
	s.Nodes[0].Fingerprint = "safari"
	if got := s.Nodes[0].FingerprintOrDefault(); got != "safari" {
		t.Errorf("fingerprint = %q", got)
	}
	if got := s.LogLevelOrDefault(); got != "warning" {
		t.Errorf("log level default = %q", got)
	}
	s.LogLevel = "error"
	if got := s.LogLevelOrDefault(); got != "error" {
		t.Errorf("log level = %q", got)
	}
}

// An empty blocked list must fall back to the default and never to "nothing
// is blocked": a hub bug that drops the list must not open the router's LAN.
func TestEmptyBlockedMeansDefault(t *testing.T) {
	e := Exit{Addr: "192.168.50.17"}
	got := e.BlockedOrDefault()
	want := DefaultBlocked()
	if len(got) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("empty list gave %v", got)
	}
	e.Blocked = []string{"10.0.0.0/8"}
	if got := e.BlockedOrDefault(); len(got) != 1 {
		t.Fatalf("explicit list gave %v", got)
	}
}

// The default list has to keep the router's LAN and the usual private ranges
// out of reach, and leave public addresses alone.
func TestDefaultBlockedCoversPrivateSpace(t *testing.T) {
	var prefixes []netip.Prefix
	for _, c := range DefaultBlocked() {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			t.Fatalf("%q: %v", c, err)
		}
		prefixes = append(prefixes, p)
	}
	blocked := func(s string) bool {
		a := netip.MustParseAddr(s)
		for _, p := range prefixes {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	for _, s := range []string{
		"10.1.2.3", "172.16.0.1", "172.31.255.254", "192.168.88.1", "192.168.50.17", "100.64.0.1",
		"127.0.0.1", "169.254.1.1", "0.0.0.0", "224.0.0.1", "255.255.255.255",
		"::1", "fe80::1", "fd12:3456::1", "ff02::1",
	} {
		if !blocked(s) {
			t.Errorf("%s is not blocked", s)
		}
	}
	for _, s := range []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34", "172.15.255.255", "172.32.0.1", "192.167.255.255",
		"100.63.255.255", "100.128.0.1", "223.255.255.255", "2606:4700:4700::1111", "2001:4860:4860::8888",
	} {
		if blocked(s) {
			t.Errorf("%s is blocked", s)
		}
	}
}

func TestParseServer(t *testing.T) {
	tests := []struct {
		in   string
		want string
		bad  bool
	}{
		{in: "1.1.1.1", want: "1.1.1.1:53"},
		{in: "1.1.1.1:5353", want: "1.1.1.1:5353"},
		{in: "2606:4700:4700::1111", want: "[2606:4700:4700::1111]:53"},
		{in: "[2606:4700:4700::1111]:853", want: "[2606:4700:4700::1111]:853"},
		{in: "::ffff:1.1.1.1", want: "1.1.1.1:53"},
		{in: "", bad: true},
		{in: "dns.example", bad: true},
		{in: "1.1.1.1:0", bad: true},
		{in: "1.1.1.1:99999", bad: true},
		{in: "fe80::1%eth0", bad: true},
	}
	for _, tt := range tests {
		got, err := ParseServer(tt.in)
		if tt.bad {
			if err == nil {
				t.Errorf("%q: accepted as %s", tt.in, got)
			}
			continue
		}
		if err != nil || got.String() != tt.want {
			t.Errorf("%q: got %s, %v; want %s", tt.in, got, err, tt.want)
		}
	}
}

func TestStringKeepsIDsOut(t *testing.T) {
	s := validSpec()
	for _, out := range []string{
		fmt.Sprint(s), fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s),
		fmt.Sprint(s.Nodes[0]), fmt.Sprintf("%+v", s.Nodes[0]), fmt.Sprintf("%v", s.Nodes),
	} {
		if strings.Contains(out, s.Nodes[0].UUID) {
			t.Errorf("%q has the node's id", out)
		}
		if !strings.Contains(out, "n1") {
			t.Errorf("%q does not name the node", out)
		}
	}
}
