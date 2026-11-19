// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const testID = "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f"

var testKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))

// The outbounds a hub could send, one of each kind the bridge carries.
func validOutbounds() map[string]Outbound {
	key16 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	key32 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	return map[string]Outbound{
		"vless over reality": {Protocol: ProtoVLESS, Port: 443, ID: testID, Flow: VisionFlow,
			Security: SecReality, ServerName: "www.example.com", PublicKey: testKey, ShortID: "ab12", SpiderX: "/x"},
		"vless over reality with no short id": {Protocol: ProtoVLESS, Port: 443, ID: testID,
			Security: SecReality, ServerName: "www.example.com", PublicKey: testKey},
		"vless over ws and tls": {Protocol: ProtoVLESS, Port: 443, ID: testID, Network: NetWS, Path: "/ws", Host: "cdn.example.com",
			Security: SecTLS, ServerName: "cdn.example.com", ALPN: []string{"http/1.1"}},
		"vless over xhttp": {Protocol: ProtoVLESS, Port: 443, ID: testID, Network: NetXHTTP, Path: "/x", Mode: "packet-up",
			Security: SecTLS, ServerName: "cdn.example.com", Fingerprint: "firefox", ALPN: []string{"h2", "http/1.1"}},
		"vless over httpupgrade, plain": {Protocol: ProtoVLESS, Port: 80, ID: testID, Network: NetHTTPUpgrade, Path: "/up", Host: "a.example.com"},
		"vmess over tcp":                {Protocol: ProtoVMess, Port: 10086, ID: testID},
		"vmess over ws and tls": {Protocol: ProtoVMess, Port: 443, ID: testID, Method: "aes-128-gcm", Network: NetWS, Path: "/",
			Security: SecTLS, ServerName: "v.example.com"},
		"trojan":          {Protocol: ProtoTrojan, Port: 443, Password: "secret", Security: SecTLS, ServerName: "t.example.com"},
		"trojan over ws":  {Protocol: ProtoTrojan, Port: 443, Password: "secret", Network: NetWS, Path: "/t", Security: SecTLS, ServerName: "t.example.com"},
		"shadowsocks":     {Protocol: ProtoShadowsocks, Port: 8388, Method: "chacha20-ietf-poly1305", Password: "secret"},
		"shadowsocks 128": {Protocol: ProtoShadowsocks, Port: 8388, Method: "2022-blake3-aes-128-gcm", Password: key16},
		"shadowsocks 256": {Protocol: ProtoShadowsocks, Port: 8388, Method: "2022-blake3-aes-256-gcm", Password: key32 + ":" + key32},
		"socks":           {Protocol: ProtoSOCKS, Port: 1080},
		"socks, login":    {Protocol: ProtoSOCKS, Port: 1080, Username: "u", Password: "p"},
		"http":            {Protocol: ProtoHTTP, Port: 3128, Username: "u", Password: "p"},
		"http over tls":   {Protocol: ProtoHTTP, Port: 443, Security: SecTLS, ServerName: "proxy.example.com"},
	}
}

func TestCheckOutboundAccepts(t *testing.T) {
	for name, o := range validOutbounds() {
		t.Run(name, func(t *testing.T) {
			if err := CheckOutbound(o); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckOutbound(t *testing.T) {
	all := validOutbounds()
	tests := []struct {
		name   string
		base   string
		mutate func(*Outbound)
		want   string // part of the error
	}{
		{"no port", "socks", func(o *Outbound) { o.Port = 0 }, "port"},
		{"an unknown protocol", "socks", func(o *Outbound) { o.Protocol = "wireguard" }, "protocol"},
		{"a transport it doesn't carry", "vless over ws and tls", func(o *Outbound) { o.Network = "grpc" }, "network"},
		{"an unknown security", "trojan", func(o *Outbound) { o.Security = "xtls" }, "security"},

		{"vless with no id", "vless over reality", func(o *Outbound) { o.ID = "" }, "id"},
		{"vless with a bad id", "vless over reality", func(o *Outbound) { o.ID = "not-a-uuid" }, "id"},
		{"an unknown flow", "vless over reality", func(o *Outbound) { o.Flow = "xtls-rprx-direct" }, "flow"},
		{"vision over ws", "vless over ws and tls", func(o *Outbound) { o.Flow = VisionFlow }, "flow"},
		{"vision with no security", "vless over reality", func(o *Outbound) {
			o.Security, o.ServerName, o.PublicKey, o.ShortID, o.SpiderX = "", "", "", "", ""
		}, "flow"},
		{"vless with a password", "vless over reality", func(o *Outbound) { o.Password = "x" }, "password"},
		{"vless with a method", "vless over reality", func(o *Outbound) { o.Method = "auto" }, "method"},

		{"vmess with an unknown cipher", "vmess over tcp", func(o *Outbound) { o.Method = "rc4" }, "method"},
		{"vmess over reality", "vmess over tcp", func(o *Outbound) {
			o.Security, o.ServerName, o.PublicKey = SecReality, "www.example.com", testKey
		}, "reality"},
		{"vmess with a flow", "vmess over tcp", func(o *Outbound) { o.Flow = VisionFlow }, "flow"},

		{"trojan with no password", "trojan", func(o *Outbound) { o.Password = "" }, "password"},
		{"trojan with an id", "trojan", func(o *Outbound) { o.ID = testID }, "id"},
		{"a password with a line break", "trojan", func(o *Outbound) { o.Password = "a\nb" }, "password"},

		{"shadowsocks with an unknown cipher", "shadowsocks", func(o *Outbound) { o.Method = "rc4-md5" }, "method"},
		{"shadowsocks with no cipher", "shadowsocks", func(o *Outbound) { o.Method = "" }, "method"},
		{"shadowsocks over ws", "shadowsocks", func(o *Outbound) { o.Network = NetWS }, "plain tcp"},
		{"a 2022 key of the wrong length", "shadowsocks 256", func(o *Outbound) {
			o.Password = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
		}, "32-byte"},
		{"a 2022 key that isn't base64", "shadowsocks 128", func(o *Outbound) { o.Password = "plain words" }, "16-byte"},

		{"socks with half a login", "socks", func(o *Outbound) { o.Username = "u" }, "go together"},
		{"socks over tls", "socks", func(o *Outbound) { o.Security, o.ServerName = SecTLS, "s.example.com" }, "plain tcp"},
		{"http over ws", "http", func(o *Outbound) { o.Network = NetWS }, "plain tcp"},
		{"socks with a user name alone and a method", "socks", func(o *Outbound) { o.Method = "auto" }, "method"},

		{"a path on tcp", "trojan", func(o *Outbound) { o.Path = "/x" }, "tcp has no path"},
		{"a path with no slash", "vless over ws and tls", func(o *Outbound) { o.Path = "ws" }, "path"},
		{"a host that is no name", "vless over ws and tls", func(o *Outbound) { o.Host = "a b" }, "host"},
		{"a mode on ws", "vless over ws and tls", func(o *Outbound) { o.Mode = "auto" }, "mode"},
		{"an unknown xhttp mode", "vless over xhttp", func(o *Outbound) { o.Mode = "fast" }, "mode"},

		{"a server name with no security", "socks", func(o *Outbound) { o.ServerName = "a.example.com" }, "none takes no"},
		{"tls with no server name", "trojan", func(o *Outbound) { o.ServerName = "" }, "server_name"},
		{"tls with an address for a name", "trojan", func(o *Outbound) { o.ServerName = "203.0.113.5" }, "server_name"},
		{"tls with a public key", "trojan", func(o *Outbound) { o.PublicKey = testKey }, "REALITY"},
		{"a bad fingerprint", "trojan", func(o *Outbound) { o.Fingerprint = "Chrome 120" }, "fingerprint"},
		{"too many ALPN", "trojan", func(o *Outbound) { o.ALPN = []string{"h2", "h3", "http/1.1", "a", "b"} }, "alpn"},
		{"a bad ALPN", "trojan", func(o *Outbound) { o.ALPN = []string{"H2 "} }, "alpn"},
		{"reality with ALPN", "vless over reality", func(o *Outbound) { o.ALPN = []string{"h2"} }, "alpn"},
		{"reality with a short key", "vless over reality", func(o *Outbound) { o.PublicKey = "abcd" }, "public_key"},
		{"reality with a bad short id", "vless over reality", func(o *Outbound) { o.ShortID = "abc" }, "short_id"},
		{"reality with a bad spider path", "vless over reality", func(o *Outbound) { o.SpiderX = "x y" }, "spider_x"},
		{"reality over ws", "vless over reality", func(o *Outbound) { o.Flow, o.Network = "", NetWS }, "tcp or xhttp"},
		{"a vmess cipher the core no longer reads", "vmess over tcp", func(o *Outbound) { o.Method = "zero" }, "method"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, ok := all[tt.base]
			if !ok {
				t.Fatalf("no outbound called %q", tt.base)
			}
			tt.mutate(&o)
			err := CheckOutbound(o)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error with %q", err, tt.want)
			}
		})
	}
}

// withOutbound is validSpec with one outbound, and its node sent through it.
func withOutbound() *Spec {
	s := validSpec()
	o := validOutbounds()["vless over reality"]
	o.Tag, o.Addr = "out-1", "198.51.100.7"
	s.Outbounds = []Outbound{o}
	s.OutboundAddr = "192.168.50.16"
	s.Nodes[0].Via = "out-1"
	return s
}

func TestValidateAcceptsOutbounds(t *testing.T) {
	tests := map[string]func(*Spec){
		"a node through an outbound": func(*Spec) {},
		"an outbound nobody uses":    func(s *Spec) { s.Nodes[0].Via = "" },
		"the entry through one": func(s *Spec) {
			s.Local = validLocal()
			s.Local.Via = "out-1"
		},
		"two accounts on one server": func(s *Spec) {
			o := s.Outbounds[0]
			o.Tag = "out-2."
			s.Outbounds = append(s.Outbounds, o)
		},
		"ten and more, each closed with a dot": func(s *Spec) {
			s.Outbounds[0].Tag, s.Nodes[0].Via = "out-1.", "out-1."
			o := s.Outbounds[0]
			o.Tag = "out-10."
			s.Outbounds = append(s.Outbounds, o)
		},
		"a fallback":               func(s *Spec) { s.Outbounds[0].Fallback = true },
		"a check of one's own":     func(s *Spec) { s.Probe = Probe{URL: "http://203.0.113.9/ok", Seconds: 60} },
		"the exit's address again": func(s *Spec) { s.OutboundAddr = s.Exit.Addr },
		"all on loopback": func(s *Spec) {
			s.DialAddr, s.Exit.Addr, s.OutboundAddr = "127.0.0.1", "127.0.0.1", "127.0.0.1"
			s.Nodes[0].Addr, s.Outbounds[0].Addr = "127.0.0.1", "127.0.0.1"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			s := withOutbound()
			mutate(s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateOutbounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{"no address to connect from", func(s *Spec) { s.OutboundAddr = "" }, "outbound_addr: missing"},
		{"the dial address", func(s *Spec) { s.OutboundAddr = s.DialAddr }, "dial link"},
		{"a name for the server", func(s *Spec) { s.Outbounds[0].Addr = "server.example.com" }, "outbounds[0].addr"},
		{"the other address family", func(s *Spec) { s.Outbounds[0].Addr = "2001:db8::7" }, "address family"},
		{"no tag", func(s *Spec) { s.Outbounds[0].Tag = "" }, "outbounds[0].tag"},
		{"a tag that reads as a group", func(s *Spec) { s.Outbounds[0].Tag = "group-1" }, "outbounds[0].tag"},
		{"a tag twice", func(s *Spec) {
			s.Outbounds = append(s.Outbounds, s.Outbounds[0])
		}, "used twice"},
		{"a node's tag", func(s *Spec) { s.Outbounds[0].Tag, s.Nodes[0].Via = "n1", "" }, "already used by an outbound"},
		{"the exit's tag", func(s *Spec) { s.Outbounds[0].Tag, s.Nodes[0].Via = ExitTag, "" }, "already used"},
		{"bad settings", func(s *Spec) { s.Outbounds[0].ID = "" }, "outbounds[0].id"},
		{"too many", func(s *Spec) {
			for i := 2; i <= MaxOutbounds+1; i++ {
				o := s.Outbounds[0]
				o.Tag = fmt.Sprintf("out-%d.", i)
				s.Outbounds = append(s.Outbounds, o)
			}
		}, "more than"},
		{"a tag that starts another outbound's", func(s *Spec) {
			o := s.Outbounds[0]
			o.Tag = "out-10"
			s.Outbounds = append(s.Outbounds, o)
		}, "is the start of"},
		{"a tag that starts a node's", func(s *Spec) { s.Outbounds[0].Tag, s.Nodes[0].Via = "n", "n" }, "is the start of"},
		{"a tag that starts the exit's", func(s *Spec) { s.Outbounds[0].Tag, s.Nodes[0].Via = "ex", "ex" }, "is the start of"},
		{"a node through one that isn't there", func(s *Spec) { s.Nodes[0].Via = "out-9" }, "nodes[0].via"},
		{"a node through one, with none", func(s *Spec) { s.Outbounds, s.OutboundAddr = nil, "" }, "nodes[0].via"},
		{"the entry through one that isn't there", func(s *Spec) {
			s.Local = validLocal()
			s.Local.Via = "out-9"
		}, "local.via"},
		{"a check that is no web address", func(s *Spec) { s.Probe.URL = "ftp://example.com/x" }, "probe.url"},
		{"a check with a login in it", func(s *Spec) { s.Probe.URL = "https://u:p@example.com/" }, "probe.url"},
		{"a check with no host", func(s *Spec) { s.Probe.URL = "https:///x" }, "probe.url"},
		{"a check too often", func(s *Spec) { s.Probe.Seconds = 1 }, "probe.seconds"},
		{"a check too seldom", func(s *Spec) { s.Probe.Seconds = MaxProbeSeconds + 1 }, "probe.seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := withOutbound()
			tt.mutate(s)
			err := s.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error with %q", err, tt.want)
			}
		})
	}
}

func TestOutboundDefaults(t *testing.T) {
	var o Outbound
	if o.NetworkOrDefault() != NetTCP || o.SecurityOrDefault() != SecNone || o.FingerprintOrDefault() != "chrome" {
		t.Errorf("defaults: %s, %s, %s", o.NetworkOrDefault(), o.SecurityOrDefault(), o.FingerprintOrDefault())
	}
	var p Probe
	if p.URLOrDefault() != DefaultProbeURL || p.SecondsOrDefault() != DefaultProbeSeconds {
		t.Errorf("probe defaults: %s every %d s", p.URLOrDefault(), p.SecondsOrDefault())
	}
	o.Tag = "out-3"
	if o.GroupTag() != "group-out-3" {
		t.Errorf("group tag: %s", o.GroupTag())
	}
}

func TestOutboundStringKeepsCredentialsOut(t *testing.T) {
	o := validOutbounds()["trojan"]
	o.Tag, o.Addr = "out-1", "198.51.100.7"
	if got := o.String(); strings.Contains(got, o.Password) || !strings.Contains(got, "out-1") {
		t.Errorf("String() = %q", got)
	}
	s := withOutbound()
	if got := s.String(); strings.Contains(got, testID) || !strings.Contains(got, "1 outbound(s)") {
		t.Errorf("Spec.String() = %q", got)
	}
}

func TestProbeRequest(t *testing.T) {
	good := func() ProbeRequest {
		o := validOutbounds()["trojan"]
		o.Addr = "198.51.100.7"
		return ProbeRequest{Outbound: o, From: "192.168.50.16", Connect: true}
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	unbound := good()
	unbound.From = ""
	if err := unbound.Validate(); err != nil {
		t.Fatalf("with no address to connect from: %v", err)
	}
	if r := good(); r.TimeoutOrDefault() != DefaultProbeTimeout || r.URLOrDefault() != DefaultProbeURL {
		t.Errorf("defaults: %d s, %s", r.TimeoutOrDefault(), r.URLOrDefault())
	}
	tests := []struct {
		name   string
		mutate func(*ProbeRequest)
		want   string
	}{
		{"a name for the server", func(r *ProbeRequest) { r.Outbound.Addr = "t.example.com" }, "outbound.addr"},
		{"a name, unbound", func(r *ProbeRequest) { r.From, r.Outbound.Addr = "", "t.example.com" }, "outbound.addr"},
		{"the other address family", func(r *ProbeRequest) { r.Outbound.Addr = "2001:db8::7" }, "address family"},
		{"a bad address to connect from", func(r *ProbeRequest) { r.From = "router" }, "from"},
		{"bad settings", func(r *ProbeRequest) { r.Outbound.Password = "" }, "outbound.password"},
		{"a bad address to ask for", func(r *ProbeRequest) { r.URL = "ftp://example.com" }, "url"},
		{"too long a wait", func(r *ProbeRequest) { r.TimeoutSeconds = MaxProbeTimeout + 1 }, "timeout_seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := good()
			tt.mutate(&r)
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error with %q", err, tt.want)
			}
		})
	}
}
