// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidName(t *testing.T) {
	ok := map[string]string{
		"ascii":             "alice",
		"with spaces":       "Ali's phone",
		"persian":           "علی رضایی",
		"persian with zwnj": "نیم‌فاصله", // U+200C is a format character, not a control one
		"persian digits":    "گوشی ۱۲",
		"cjk":               "名前",
		"emoji":             "node 🔥",
		"one character":     "x",
		"64 ascii":          strings.Repeat("a", 64),
		"64 multibyte":      strings.Repeat("ع", 64), // 128 bytes
		"combining marks":   strings.Repeat("é", 32),
	}
	for name, s := range ok {
		if err := ValidName(s); err != nil {
			t.Errorf("%s: %q rejected: %v", name, s, err)
		}
	}
	bad := map[string]string{
		"empty":               "",
		"spaces only":         "   ",
		"leading space":       " alice",
		"trailing space":      "alice ",
		"65 ascii":            strings.Repeat("a", 65),
		"65 multibyte":        strings.Repeat("ع", 65),
		"newline":             "a\nb",
		"tab":                 "a\tb",
		"nul":                 "a\x00b",
		"delete":              "a\x7fb",
		"c1 control":          "a\u0085b",
		"escape":              "a\x1bb",
		"line separator":      "a b",
		"paragraph separator": "a b",
		"invalid utf-8":       "a\xffb",
		"lone continuation":   "\x80",
	}
	for name, s := range bad {
		err := ValidName(s)
		var inv *InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("%s: %q was accepted (err = %v)", name, s, err)
		}
	}
}

func TestCleanName(t *testing.T) {
	got, err := CleanName("  علی \n")
	if err != nil || got != "علی" {
		t.Fatalf("CleanName = %q, %v", got, err)
	}
	if _, err := CleanName(" \t "); err == nil {
		t.Fatal("a blank name was accepted")
	}
}

func TestCleanHost(t *testing.T) {
	ok := map[string]string{
		"203.0.113.9":                    "203.0.113.9",
		"  203.0.113.9 ":                 "203.0.113.9",
		"127.0.0.1":                      "127.0.0.1",
		"10.0.2.2":                       "10.0.2.2",
		"2001:DB8::1":                    "2001:db8::1",
		"::1":                            "::1",
		"Example.COM":                    "example.com",
		"node-1.example.com":             "node-1.example.com",
		"localhost":                      "localhost",
		"a.b-c.d1.example":               "a.b-c.d1.example",
		"xn--mgbh0fb.example":            "xn--mgbh0fb.example",
		"1.example.com":                  "1.example.com", // a digit-only first label is fine
		strings.Repeat("a", 63) + ".com": strings.Repeat("a", 63) + ".com",
	}
	for in, want := range ok {
		got, err := CleanHost(in)
		if err != nil || got != want {
			t.Errorf("CleanHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "   ", "0.0.0.0", "::", "fe80::1%eth0",
		"1.2.3", "256.1.1.1", "01.2.3.4", "1.2.3.4.5", "999",
		"-a.example", "a-.example", "a..example", ".example", "example.com.",
		"a b.example", "a_b.example", "example.com:443", "http://example.com", "example.com/x",
		"[::1]", "user@example.com", "مثال.ایران",
		strings.Repeat("a", 64) + ".com",
		strings.Repeat("a.", 127) + "com", // 256 characters
	}
	for _, in := range bad {
		if got, err := CleanHost(in); err == nil {
			t.Errorf("CleanHost(%q) = %q, want an error", in, got)
		}
	}
}

func TestValidPort(t *testing.T) {
	for _, p := range []int{1, 22, 443, 9090, 65535} {
		if err := ValidPort(p); err != nil {
			t.Errorf("port %d: %v", p, err)
		}
	}
	for _, p := range []int{-1, 0, 65536, 100000} {
		if err := ValidPort(p); err == nil {
			t.Errorf("port %d was accepted", p)
		}
	}
}

func TestValidUUID(t *testing.T) {
	for _, s := range []string{uuidA, uuidB, "00000000-0000-0000-0000-000000000000"} {
		if !validUUID(s) {
			t.Errorf("%s rejected", s)
		}
	}
	for _, s := range []string{
		"", "6f1c1c1e2b3a4c5d8e9f0a1b2c3d4e5f", "6F1C1C1E-2B3A-4C5D-8E9F-0A1B2C3D4E5F",
		"6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5", "6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5fa",
		"6f1c1c1e_2b3a-4c5d-8e9f-0a1b2c3d4e5f", "6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5g",
		"{6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5f}",
	} {
		if validUUID(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestNodeValidate(t *testing.T) {
	full := goodNode("Tehran 1", "203.0.113.9")
	full.ID = 1
	full.SSHUser = "root"
	full.Arch = "arm64"
	full.RealityTarget = "www.example.com:443"
	full.RealitySNI = "www.example.com"
	full.RealityPublicKey = "pub"
	full.RealityPrivateKey = "priv"
	full.RealityShortID = "0123456789abcdef"
	full.AgentVersion = "1.3.0"
	full.XrayVersion = "26.10.21"
	full.CertNotBefore = t0
	full.CertNotAfter = t0.AddDate(1, 0, 0)
	full.SSHHostKey = "SHA256:abc"
	full.BridgeUUID = uuidC
	if err := full.Validate(); err != nil {
		t.Fatalf("a full node: %v", err)
	}
	fresh := goodNode("Tehran 1", "203.0.113.9")
	fresh.ID, fresh.State = 1, NodeInstalling
	if err := fresh.Validate(); err != nil {
		t.Fatalf("a node that has only just been added: %v", err)
	}

	cases := map[string]func(*Node){
		"no id":                    func(n *Node) { n.ID = 0 },
		"empty name":               func(n *Node) { n.Name = "" },
		"control in name":          func(n *Node) { n.Name = "a\nb" },
		"empty host":               func(n *Node) { n.Host = "" },
		"host not canonical":       func(n *Node) { n.Host = "Example.com" },
		"host with spaces":         func(n *Node) { n.Host = " 203.0.113.9" },
		"ssh port 0":               func(n *Node) { n.SSHPort = 0 },
		"agent port too big":       func(n *Node) { n.AgentPort = 65536 },
		"entry port 0":             func(n *Node) { n.EntryPort = 0 },
		"ssh and entry collide":    func(n *Node) { n.SSHPort = n.EntryPort },
		"ssh and agent collide":    func(n *Node) { n.SSHPort = n.AgentPort },
		"agent and entry":          func(n *Node) { n.AgentPort = n.EntryPort },
		"unknown state":            func(n *Node) { n.State = "sleeping" },
		"no creation time":         func(n *Node) { n.CreatedAt = time.Time{} },
		"target without port":      func(n *Node) { n.RealityTarget = "www.example.com" },
		"target with bad port":     func(n *Node) { n.RealityTarget = "www.example.com:0" },
		"target with bad host":     func(n *Node) { n.RealityTarget = "not a host:443" },
		"target port not a number": func(n *Node) { n.RealityTarget = "www.example.com:https" },
		"sni is an address":        func(n *Node) { n.RealitySNI = "203.0.113.9" },
		"sni with a slash":         func(n *Node) { n.RealitySNI = "a/b.example" },
		"cert ends before start":   func(n *Node) { n.CertNotBefore, n.CertNotAfter = t0, t0.Add(-time.Hour) },
		"cert of no length":        func(n *Node) { n.CertNotBefore, n.CertNotAfter = t0, t0 },
		"arch too long":            func(n *Node) { n.Arch = strings.Repeat("a", 17) },
		"version with control":     func(n *Node) { n.AgentVersion = "1.0\n" },
		"version far too long":     func(n *Node) { n.XrayVersion = strings.Repeat("1", 65) },
		"host key too long":        func(n *Node) { n.SSHHostKey = strings.Repeat("x", 129) },
		"user not ascii":           func(n *Node) { n.SSHUser = "rööt" },
		"bridge id not a uuid":     func(n *Node) { n.BridgeUUID = "bridge" },
		"bridge id in capitals":    func(n *Node) { n.BridgeUUID = strings.ToUpper(uuidA) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			n := full
			mutate(&n)
			if err := n.Validate(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// A bridge client is one more login on the node's entry. Xray keys a login by
// its id, so two nodes' bridges, or a bridge and a subscription, may not have
// the same one, however the state came to be that way.
func TestABridgeClientIDIsALoginLikeAnyOther(t *testing.T) {
	s := small(t)
	// uuidA is subscription 1's client on node 1
	s.Nodes[0].BridgeUUID = uuidA
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "shares its client id") {
		t.Fatalf("a bridge id that is a subscription's: %v", err)
	}

	s = small(t)
	s.Nodes[0].BridgeUUID = "11111111-2222-4333-8444-666666666666"
	s.Nodes[1].BridgeUUID = s.Nodes[0].BridgeUUID
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "bridge client id") {
		t.Fatalf("one bridge id on two nodes: %v", err)
	}

	s.Nodes[1].BridgeUUID = "11111111-2222-4333-8444-777777777777"
	if err := s.Validate(); err != nil {
		t.Fatalf("two different bridge ids: %v", err)
	}
	// and a node may have none yet
	s.Nodes[1].BridgeUUID = ""
	if err := s.Validate(); err != nil {
		t.Fatalf("a node with no bridge id: %v", err)
	}
}

func TestValidationErrorsAreInvalidErrors(t *testing.T) {
	// the API turns these into bad_request, wrapped or not
	n := goodNode("x", "203.0.113.1")
	n.ID, n.SSHPort = 1, 0
	var inv *InvalidError
	if err := n.Validate(); !errors.As(err, &inv) || !strings.Contains(err.Error(), "ssh port") {
		t.Fatalf("a bad ssh port: %v", err)
	}
	s := small(t)
	s.Nodes[0].Host = "nope nope"
	if err := s.Validate(); !errors.As(err, &inv) || !strings.Contains(err.Error(), "node 1") {
		t.Fatalf("a bad node inside a state: %v", err)
	}
}
