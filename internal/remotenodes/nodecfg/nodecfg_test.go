// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestBuildGolden(t *testing.T) {
	got, err := nodecfg.Build(sample())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), got) {
		gl, wl := strings.Split(string(got), "\n"), strings.Split(strings.TrimRight(string(want), "\n"), "\n")
		for i := 0; i < max(len(gl), len(wl)); i++ {
			if i >= len(gl) || i >= len(wl) || gl[i] != wl[i] {
				t.Fatalf("output differs from %s at line %d (run with -update if that is intended):\n got: %q\nwant: %q",
					path, i+1, lineOr(gl, i), lineOr(wl, i))
			}
		}
	}
}

func lineOr(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "<end of file>"
}

func TestBuildIsDeterministic(t *testing.T) {
	// the embedded list included
	in := sample()
	in.DomesticCIDRs = nil
	a, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		b, err := nodecfg.Build(in)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("build %d differs from the first", i+2)
		}
	}
}

func TestBuildLeavesTheInputAlone(t *testing.T) {
	in := sample()
	in.DomesticCIDRs = []string{"10.1.2.3/8", "5.22.0.0/17"} // the first gets normalised in the output
	before := sample()
	before.DomesticCIDRs = slices.Clone(in.DomesticCIDRs)
	if _, err := nodecfg.Build(in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, before) {
		t.Fatalf("Build changed its input:\n got %+v\nwant %+v", in, before)
	}
}

func TestBuildUsesTheEmbeddedDomesticList(t *testing.T) {
	in := sample()
	in.DomesticCIDRs = nil
	rule := at(t, decode(t, in), "routing", "rules", 2)
	if got := at(t, rule, "ruleTag"); got != "domestic-direct" {
		t.Fatalf("third rule is %v, want domestic-direct", got)
	}
	ips := toStrings(t, at(t, rule, "ip"))
	if want := nodecfg.DomesticCIDRs(); !slices.Equal(ips, want) {
		t.Fatalf("the rule carries %d ranges, the embedded list has %d", len(ips), len(want))
	}
}

func TestBuildOverridesTheDomesticList(t *testing.T) {
	in := sample()
	in.DomesticCIDRs = []string{"10.1.2.3/8", "2001:db8:ffff::1/32"}
	ips := toStrings(t, at(t, decode(t, in), "routing", "rules", 2, "ip"))
	// host bits are cleared, order is kept
	if want := []string{"10.0.0.0/8", "2001:db8::/32"}; !slices.Equal(ips, want) {
		t.Fatalf("domestic ranges are %v, want %v", ips, want)
	}
}

func TestBuildDefaults(t *testing.T) {
	cfg := decode(t, sample())
	if got := at(t, cfg, "inbounds", 0, "port"); got != float64(nodecfg.DefaultAPIPort) {
		t.Errorf("API port defaults to %v, want %d", got, nodecfg.DefaultAPIPort)
	}
	if got := at(t, cfg, "log", "loglevel"); got != "warning" {
		t.Errorf("log level defaults to %v, want warning", got)
	}

	in := sample()
	in.APIPort, in.LogLevel = 20085, "Debug"
	cfg = decode(t, in)
	if got := at(t, cfg, "inbounds", 0, "port"); got != float64(20085) {
		t.Errorf("API port is %v, want 20085", got)
	}
	if got := at(t, cfg, "log", "loglevel"); got != "debug" {
		t.Errorf("log level is %v, want debug", got)
	}
}

// A node with no subscriptions yet still gets its bridge client; the tunnel
// doesn't wait for the first subscription.
func TestBuildWithoutClients(t *testing.T) {
	in := sample()
	in.Clients = nil
	clients := at(t, decode(t, in), "inbounds", 1, "settings", "clients").([]any)
	if len(clients) != 1 || at(t, clients[0], "email") != nodecfg.BridgeEmail(7) {
		t.Fatalf("clients = %v, want only the bridge", clients)
	}
}

func TestBuildAcceptsUnusualButValidInput(t *testing.T) {
	cases := map[string]func(*nodecfg.Input){
		"ipv4 dest":        func(in *nodecfg.Input) { in.RealityDest = "203.0.113.9:443" },
		"ipv6 dest":        func(in *nodecfg.Input) { in.RealityDest = "[2001:db8::1]:8443" },
		"upper case ids":   func(in *nodecfg.Input) { in.Clients[0].UUID = strings.ToUpper(uuidSub1) },
		"short id of 2":    func(in *nodecfg.Input) { in.ShortIDs = []string{"ab"} },
		"port 65535":       func(in *nodecfg.Input) { in.EntryPort = 65535 },
		"tag with dots":    func(in *nodecfg.Input) { in.Bridge.ReverseTag = "rvs.to_node-7" },
		"single-label sni": func(in *nodecfg.Input) { in.ServerNames = []string{"localhost"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := sample()
			mutate(&in)
			if _, err := nodecfg.Build(in); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*nodecfg.Input)
		want   string
	}{
		{"entry port 0", func(in *nodecfg.Input) { in.EntryPort = 0 }, "entry port 0"},
		{"entry port too high", func(in *nodecfg.Input) { in.EntryPort = 65536 }, "entry port 65536"},
		{"entry port negative", func(in *nodecfg.Input) { in.EntryPort = -1 }, "entry port -1"},
		{"api port too high", func(in *nodecfg.Input) { in.APIPort = 70000 }, "API port 70000"},
		{"api port negative", func(in *nodecfg.Input) { in.APIPort = -5 }, "API port -5"},
		{"entry on the api port", func(in *nodecfg.Input) { in.APIPort = in.EntryPort }, "can't both use port 8443"},
		{"entry on the default api port", func(in *nodecfg.Input) { in.EntryPort = nodecfg.DefaultAPIPort }, "can't both use port 10085"},

		{"dest empty", func(in *nodecfg.Input) { in.RealityDest = "" }, "host:port"},
		{"dest without port", func(in *nodecfg.Input) { in.RealityDest = "www.example.com" }, "host:port"},
		{"dest without host", func(in *nodecfg.Input) { in.RealityDest = ":443" }, "host:port"},
		{"dest with a scheme", func(in *nodecfg.Input) { in.RealityDest = "https://www.example.com:443" }, "host:port"},
		{"dest port 0", func(in *nodecfg.Input) { in.RealityDest = "www.example.com:0" }, "usable port"},
		{"dest port too high", func(in *nodecfg.Input) { in.RealityDest = "www.example.com:65536" }, "usable port"},
		{"dest port not a number", func(in *nodecfg.Input) { in.RealityDest = "www.example.com:https" }, "usable port"},
		{"dest host with a space", func(in *nodecfg.Input) { in.RealityDest = "www example.com:443" }, "usable host"},

		{"no server names", func(in *nodecfg.Input) { in.ServerNames = nil }, "at least one REALITY server name"},
		{"empty server name", func(in *nodecfg.Input) { in.ServerNames = []string{"example.com", ""} }, "server name"},
		{"wildcard server name", func(in *nodecfg.Input) { in.ServerNames = []string{"*.example.com"} }, "server name"},
		{"server name with a port", func(in *nodecfg.Input) { in.ServerNames = []string{"example.com:443"} }, "server name"},
		{"server name with a scheme", func(in *nodecfg.Input) { in.ServerNames = []string{"https://example.com"} }, "server name"},
		{"server name with an empty label", func(in *nodecfg.Input) { in.ServerNames = []string{"example..com"} }, "server name"},
		{"server name starting with a dash", func(in *nodecfg.Input) { in.ServerNames = []string{"-example.com"} }, "server name"},
		{"server name too long", func(in *nodecfg.Input) { in.ServerNames = []string{strings.Repeat("a", 64) + ".com"} }, "server name"},

		{"private key empty", func(in *nodecfg.Input) { in.PrivateKey = "" }, "private key"},
		{"private key too short", func(in *nodecfg.Input) { in.PrivateKey = testKey[:42] }, "private key"},
		{"private key too long", func(in *nodecfg.Input) { in.PrivateKey = testKey + "A" }, "private key"},
		{"private key padded", func(in *nodecfg.Input) { in.PrivateKey = testKey + "=" }, "private key"},
		{"private key in standard base64", func(in *nodecfg.Input) { in.PrivateKey = strings.Replace(testKey, "AQID", "+/ID", 1) }, "private key"},
		{"private key with a stray character", func(in *nodecfg.Input) { in.PrivateKey = "!" + testKey[1:] }, "private key"},

		{"no short ids", func(in *nodecfg.Input) { in.ShortIDs = nil }, "at least one REALITY short id"},
		{"empty short id", func(in *nodecfg.Input) { in.ShortIDs = []string{""} }, "short id"},
		{"odd short id", func(in *nodecfg.Input) { in.ShortIDs = []string{"abc"} }, "short id"},
		{"long short id", func(in *nodecfg.Input) { in.ShortIDs = []string{"0123456789abcdef01"} }, "short id"},
		{"short id not hex", func(in *nodecfg.Input) { in.ShortIDs = []string{"zz"} }, "short id"},

		{"client uuid", func(in *nodecfg.Input) { in.Clients[1].UUID = "not-a-uuid" }, "client 2"},
		{"client uuid unhyphenated", func(in *nodecfg.Input) { in.Clients[0].UUID = strings.ReplaceAll(uuidSub1, "-", "") }, "client 1"},
		{"client uuid with a stray character", func(in *nodecfg.Input) { in.Clients[0].UUID = strings.Replace(uuidSub1, "2f8e", "2g8e", 1) }, "client 1"},
		{"client uuid with a dash in a digit's place", func(in *nodecfg.Input) { in.Clients[0].UUID = strings.Replace(uuidSub1, "3f0b", "3-0b", 1) }, "client 1"},
		{"client uuid with a dash missing and a digit more", func(in *nodecfg.Input) { in.Clients[0].UUID = strings.Replace(uuidSub1, "0b-3a", "0b3a-", 1) }, "client 1"},
		{"client uuid twice", func(in *nodecfg.Input) { in.Clients[1].UUID = strings.ToUpper(uuidSub1) }, "used twice"},
		{"client uuid is the bridge's", func(in *nodecfg.Input) { in.Clients[0].UUID = uuidBridge }, "used twice"},
		{"client email empty", func(in *nodecfg.Input) { in.Clients[0].Email = "" }, "client 1: email"},
		{"client email with a space", func(in *nodecfg.Input) { in.Clients[0].Email = "sub 1@x" }, "client 1: email"},
		{"client email with the stats separator", func(in *nodecfg.Input) { in.Clients[0].Email = "a>>>b" }, "client 1: email"},
		{"client email not ascii", func(in *nodecfg.Input) { in.Clients[0].Email = "مشترک@x" }, "client 1: email"},
		{"client email twice", func(in *nodecfg.Input) { in.Clients[1].Email = strings.ToUpper(in.Clients[0].Email) }, "used twice"},
		{"client email is the bridge's", func(in *nodecfg.Input) { in.Clients[0].Email = in.Bridge.Email }, "used twice"},

		{"bridge uuid", func(in *nodecfg.Input) { in.Bridge.UUID = "x" }, "bridge"},
		{"bridge email", func(in *nodecfg.Input) { in.Bridge.Email = "" }, "bridge: email"},
		{"bridge tag empty", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "" }, "reverse tag"},
		{"bridge tag with a space", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "rvs to" }, "reverse tag"},
		{"bridge tag too long", func(in *nodecfg.Input) { in.Bridge.ReverseTag = strings.Repeat("a", 65) }, "reverse tag"},
		{"bridge tag starting with a dash", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "-rvs" }, "reverse tag"},
		{"bridge tag api", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "api" }, "reverse tag"},
		{"bridge tag entry", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "entry" }, "reverse tag"},
		{"bridge tag direct", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "direct" }, "reverse tag"},
		{"bridge tag blocked", func(in *nodecfg.Input) { in.Bridge.ReverseTag = "blocked" }, "reverse tag"},

		{"log level", func(in *nodecfg.Input) { in.LogLevel = "verbose" }, `log level "verbose"`},

		{"domestic range without a length", func(in *nodecfg.Input) { in.DomesticCIDRs = []string{"5.22.0.0"} }, "not a CIDR"},
		{"domestic range nonsense", func(in *nodecfg.Input) { in.DomesticCIDRs = []string{"5.22.0.0/17", "nonsense"} }, `"nonsense" is not a CIDR`},
		{"domestic range out of range", func(in *nodecfg.Input) { in.DomesticCIDRs = []string{"300.1.0.0/16"} }, "not a CIDR"},
		{"domestic range IPv4-mapped", func(in *nodecfg.Input) { in.DomesticCIDRs = []string{"::ffff:5.22.0.0/113"} }, "IPv4-mapped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := sample()
			c.mutate(&in)
			out, err := nodecfg.Build(in)
			if err == nil {
				t.Fatalf("Build accepted it and returned %d bytes", len(out))
			}
			if !errors.Is(err, nodecfg.ErrInvalid) {
				t.Errorf("error doesn't wrap ErrInvalid: %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q doesn't mention %q", err, c.want)
			}
			if out != nil {
				t.Errorf("Build returned %d bytes along with the error", len(out))
			}
		})
	}
}

func TestEmails(t *testing.T) {
	if got, want := nodecfg.SubscriptionEmail(12), "sub-12@remote-nodes"; got != want {
		t.Errorf("SubscriptionEmail(12) = %q, want %q", got, want)
	}
	if got, want := nodecfg.BridgeEmail(3), "rvs-t3@reverse"; got != want {
		t.Errorf("BridgeEmail(3) = %q, want %q", got, want)
	}

	for _, id := range []int{1, 2, 10, 99, 123456} {
		if got, ok := nodecfg.ParseSubscriptionEmail(nodecfg.SubscriptionEmail(id)); !ok || got != id {
			t.Errorf("subscription %d came back as %d, %v", id, got, ok)
		}
		if got, ok := nodecfg.ParseBridgeEmail(nodecfg.BridgeEmail(id)); !ok || got != id {
			t.Errorf("bridge of node %d came back as %d, %v", id, got, ok)
		}
		if _, ok := nodecfg.ParseBridgeEmail(nodecfg.SubscriptionEmail(id)); ok {
			t.Errorf("subscription %d parsed as a bridge", id)
		}
		if _, ok := nodecfg.ParseSubscriptionEmail(nodecfg.BridgeEmail(id)); ok {
			t.Errorf("bridge of node %d parsed as a subscription", id)
		}
	}

	for _, s := range []string{
		"", "sub-", "sub-@remote-nodes", "sub-0@remote-nodes", "sub--1@remote-nodes", "sub-+1@remote-nodes",
		"sub-01@remote-nodes", "sub-1@remote-nodes ", " sub-1@remote-nodes", "sub-1@reverse", "sub-1", "1@remote-nodes",
		"rvs-t1@reverse", "SUB-1@remote-nodes", "sub-1x@remote-nodes",
	} {
		if id, ok := nodecfg.ParseSubscriptionEmail(s); ok {
			t.Errorf("ParseSubscriptionEmail(%q) = %d, true", s, id)
		}
	}
	for _, s := range []string{
		"", "rvs-t", "rvs-t@reverse", "rvs-t0@reverse", "rvs-t01@reverse", "rvs-p1@reverse", "rvs-t1@remote-nodes",
		"rvs-t1", "sub-1@remote-nodes", "rvs-tx@reverse",
	} {
		if id, ok := nodecfg.ParseBridgeEmail(s); ok {
			t.Errorf("ParseBridgeEmail(%q) = %d, true", s, id)
		}
	}
}
