// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The outbounds, held to the loader like the rest of the config. Each is
// written in the loader's flat client form ({"address", "port", "id", ...}),
// the way an admin would write it by hand, and outboundDeltas lists every
// other change a spec with outbounds makes to the config. The whole config has
// to come out equal, as in TestParityWithDeltas.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

const outboundID = "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f"

var outboundKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))

// testOutbounds are the kinds of outbound a hub can send, the same set
// bridgespec's own tests accept, on public addresses but for the one that
// Xray runs plain only to a private address.
func testOutbounds() map[string]bridgespec.Outbound {
	key16 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	key32 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	all := map[string]bridgespec.Outbound{
		"vless over reality": {Protocol: bridgespec.ProtoVLESS, Port: 443, ID: outboundID, Flow: bridgespec.VisionFlow,
			Security: bridgespec.SecReality, ServerName: "www.example.com", PublicKey: outboundKey, ShortID: "ab12", SpiderX: "/x"},
		"vless over reality with no short id": {Protocol: bridgespec.ProtoVLESS, Port: 443, ID: outboundID,
			Security: bridgespec.SecReality, ServerName: "www.example.com", PublicKey: outboundKey},
		"vless over ws and tls": {Protocol: bridgespec.ProtoVLESS, Port: 443, ID: outboundID, Network: bridgespec.NetWS, Path: "/ws", Host: "cdn.example.com",
			Security: bridgespec.SecTLS, ServerName: "cdn.example.com", ALPN: []string{"http/1.1"}},
		"vless over xhttp": {Protocol: bridgespec.ProtoVLESS, Port: 443, ID: outboundID, Network: bridgespec.NetXHTTP, Path: "/x", Mode: "packet-up",
			Security: bridgespec.SecTLS, ServerName: "cdn.example.com", Fingerprint: "firefox", ALPN: []string{"h2", "http/1.1"}},
		"vless over xhttp and reality": {Protocol: bridgespec.ProtoVLESS, Port: 443, ID: outboundID, Network: bridgespec.NetXHTTP, Path: "/x",
			Security: bridgespec.SecReality, ServerName: "www.example.com", PublicKey: outboundKey, ShortID: "0123456789abcdef"},
		"vless over httpupgrade, plain": {Protocol: bridgespec.ProtoVLESS, Port: 80, ID: outboundID, Network: bridgespec.NetHTTPUpgrade, Path: "/up", Host: "a.example.com",
			Addr: "192.168.88.7"},
		"vmess over tcp": {Protocol: bridgespec.ProtoVMess, Port: 10086, ID: outboundID},
		"vmess over ws and tls": {Protocol: bridgespec.ProtoVMess, Port: 443, ID: outboundID, Method: "aes-128-gcm", Network: bridgespec.NetWS, Path: "/",
			Security: bridgespec.SecTLS, ServerName: "v.example.com"},
		"vmess with chacha": {Protocol: bridgespec.ProtoVMess, Port: 10086, ID: outboundID, Method: "chacha20-poly1305"},
		"trojan":            {Protocol: bridgespec.ProtoTrojan, Port: 443, Password: "secret", Security: bridgespec.SecTLS, ServerName: "t.example.com"},
		"trojan over ws": {Protocol: bridgespec.ProtoTrojan, Port: 443, Password: "secret", Network: bridgespec.NetWS, Path: "/t",
			Security: bridgespec.SecTLS, ServerName: "t.example.com"},
		"trojan over httpupgrade and tls": {Protocol: bridgespec.ProtoTrojan, Port: 443, Password: "secret", Network: bridgespec.NetHTTPUpgrade, Path: "/u",
			Security: bridgespec.SecTLS, ServerName: "t.example.com", Fingerprint: "safari"},
		"shadowsocks":             {Protocol: bridgespec.ProtoShadowsocks, Port: 8388, Method: "chacha20-ietf-poly1305", Password: "secret"},
		"shadowsocks aes":         {Protocol: bridgespec.ProtoShadowsocks, Port: 8388, Method: "aes-256-gcm", Password: "secret"},
		"shadowsocks xchacha":     {Protocol: bridgespec.ProtoShadowsocks, Port: 8388, Method: "xchacha20-poly1305", Password: "secret"},
		"shadowsocks 128":         {Protocol: bridgespec.ProtoShadowsocks, Port: 8388, Method: "2022-blake3-aes-128-gcm", Password: key16},
		"shadowsocks 256":         {Protocol: bridgespec.ProtoShadowsocks, Port: 8388, Method: "2022-blake3-aes-256-gcm", Password: key32 + ":" + key32},
		"shadowsocks 2022 chacha": {Protocol: bridgespec.ProtoShadowsocks, Port: 8388, Method: "2022-blake3-chacha20-poly1305", Password: key32},
		"socks":                   {Protocol: bridgespec.ProtoSOCKS, Port: 1080},
		"socks, login":            {Protocol: bridgespec.ProtoSOCKS, Port: 1080, Username: "u", Password: "p"},
		"http":                    {Protocol: bridgespec.ProtoHTTP, Port: 3128, Username: "u", Password: "p"},
		"http, no login":          {Protocol: bridgespec.ProtoHTTP, Port: 3128},
		"http over tls":           {Protocol: bridgespec.ProtoHTTP, Port: 443, Security: bridgespec.SecTLS, ServerName: "proxy.example.com"},
	}
	for name, o := range all {
		if o.Addr == "" {
			o.Addr = "198.51.100.7"
		}
		all[name] = o
	}
	return all
}

// outboundSpec is testSpec with one outbound, tagged the way the hub tags
// them, and the first node sent through it.
func outboundSpec(o bridgespec.Outbound) *bridgespec.Spec {
	s := testSpec()
	o.Tag = "out-1."
	s.Outbounds = []bridgespec.Outbound{o}
	s.OutboundAddr = "192.168.50.16"
	s.Nodes[0].Via = o.Tag
	return s
}

// outboundJSON is o in the loader's flat client form, sending from from.
func outboundJSON(o bridgespec.Outbound, from string) map[string]any {
	settings := map[string]any{"address": o.Addr, "port": o.Port}
	switch o.Protocol {
	case bridgespec.ProtoVLESS:
		settings["id"], settings["flow"], settings["encryption"] = o.ID, o.Flow, "none"
	case bridgespec.ProtoVMess:
		settings["id"], settings["security"] = o.ID, o.Method
	case bridgespec.ProtoTrojan:
		settings["password"] = o.Password
	case bridgespec.ProtoShadowsocks:
		settings["method"], settings["password"] = o.Method, o.Password
	case bridgespec.ProtoSOCKS, bridgespec.ProtoHTTP:
		if o.Username != "" {
			settings["user"], settings["pass"] = o.Username, o.Password
		}
	}
	out := map[string]any{"tag": o.Tag, "protocol": o.Protocol, "settings": settings}
	if from != "" {
		out["sendThrough"] = from
	}

	network, security := o.NetworkOrDefault(), o.SecurityOrDefault()
	if network == bridgespec.NetTCP && security == bridgespec.SecNone {
		return out
	}
	stream := map[string]any{"network": network, "security": security}
	switch network {
	case bridgespec.NetWS:
		stream["wsSettings"] = map[string]any{"path": o.Path, "host": o.Host}
	case bridgespec.NetHTTPUpgrade:
		stream["httpupgradeSettings"] = map[string]any{"path": o.Path, "host": o.Host}
	case bridgespec.NetXHTTP:
		stream["xhttpSettings"] = map[string]any{"path": o.Path, "host": o.Host, "mode": o.Mode}
	}
	switch security {
	case bridgespec.SecTLS:
		tls := map[string]any{"serverName": o.ServerName, "fingerprint": o.FingerprintOrDefault()}
		if len(o.ALPN) > 0 {
			tls["alpn"] = o.ALPN
		} else if o.Protocol == bridgespec.ProtoHTTP {
			// an HTTP proxy asks for HTTP/1.1 unless told otherwise (outboundDeltas 6)
			tls["alpn"] = []string{"http/1.1"}
		}
		stream["tlsSettings"] = tls
	case bridgespec.SecReality:
		stream["realitySettings"] = map[string]any{
			"serverName": o.ServerName, "fingerprint": o.FingerprintOrDefault(),
			"publicKey": o.PublicKey, "shortId": o.ShortID, "spiderX": o.SpiderX,
		}
	}
	out["streamSettings"] = stream
	return out
}

// outboundDeltas edits the reference of a spec without outbounds into the
// one with them. This list is the documentation of every difference.
//
//  1. Each outbound follows the tunnels, sending from the outbound address.
//  2. The rule of a node, or of the entry, that leaves through an outbound
//     becomes two: the blocked ranges, by literal address, to the exit, whose
//     own rules refuse them; then the rest to the outbound, or to its group.
//  3. An outbound with a fallback has a group: the outbound alone, picked at
//     random, the exit when the check finds it down.
//  4. The check watches every outbound, all at once.
//  5. Every outbound counts its bytes, which the system policy turns on.
//  6. An HTTP proxy over TLS with no ALPN of its own asks for http/1.1 only:
//     offered h2, Xray would send its CONNECT over HTTP/2 (see outboundJSON).
func outboundDeltas(t *testing.T, cfg map[string]any, s *bridgespec.Spec) {
	t.Helper()
	for _, o := range s.Outbounds {
		cfg["outbounds"] = append(cfg["outbounds"].([]any), outboundJSON(o, s.OutboundAddr)) // 1
	}

	via := map[string]bridgespec.Outbound{}
	for _, o := range s.Outbounds {
		via[o.Tag] = o
	}
	leaves := map[string]string{} // inbound tag to the tag of its outbound
	for _, n := range s.Nodes {
		if n.Via != "" {
			leaves[n.ReverseTag] = n.Via
		}
	}
	if s.Local != nil && s.Local.Via != "" {
		leaves[bridgespec.LocalTag] = s.Local.Via
	}
	routing := cfg["routing"].(map[string]any)
	var rules []any
	for _, r := range routing["rules"].([]any) {
		in := firstTag(r.(map[string]any)["inboundTag"])
		tag, ok := leaves[in]
		if !ok {
			rules = append(rules, r)
			continue
		}
		rest := map[string]any{"type": "field", "inboundTag": []string{in}, "outboundTag": tag}
		if via[tag].Fallback {
			rest = map[string]any{"type": "field", "inboundTag": []string{in}, "balancerTag": via[tag].GroupTag()}
		}
		rules = append(rules, map[string]any{
			"type": "field", "inboundTag": []string{in}, "ip": s.Exit.BlockedOrDefault(), "outboundTag": bridgespec.ExitTag,
		}, rest) // 2
	}
	routing["rules"] = rules

	var balancers []any
	var tags []string
	for _, o := range s.Outbounds {
		tags = append(tags, o.Tag)
		if o.Fallback {
			balancers = append(balancers, map[string]any{
				"tag": o.GroupTag(), "selector": []string{o.Tag}, "strategy": map[string]any{"type": "random"}, "fallbackTag": bridgespec.ExitTag,
			}) // 3
		}
	}
	if balancers != nil {
		routing["balancers"] = balancers
	}

	cfg["observatory"] = map[string]any{
		"subjectSelector":   tags,
		"probeURL":          s.Probe.URLOrDefault(),
		"probeInterval":     fmt.Sprintf("%ds", s.Probe.SecondsOrDefault()),
		"enableConcurrency": true,
	} // 4

	cfg["policy"].(map[string]any)["system"] = map[string]any{"statsOutboundUplink": true, "statsOutboundDownlink": true} // 5
}

// firstTag reads a rule's inbound tag, whether the rule came through JSON or not.
func firstTag(v any) string {
	switch tags := v.(type) {
	case []string:
		return tags[0]
	case []any:
		return tags[0].(string)
	}
	return ""
}

// outboundReference is the loader's config for a spec with outbounds.
func outboundReference(t *testing.T, s *bridgespec.Spec) map[string]any {
	t.Helper()
	var cfg map[string]any
	if s.Local != nil {
		cfg = localReference(t, s)
	} else {
		cfg = panelBridgeConfig(t, s)
		bridgeDeltas(t, cfg, s)
	}
	outboundDeltas(t, cfg, s)
	return cfg
}

func TestParityEachKindOfOutbound(t *testing.T) {
	all := testOutbounds()
	names := slices.Sorted(maps.Keys(all))
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			s := outboundSpec(all[name])
			got, err := buildConfig(s, "")
			if err != nil {
				t.Fatal(err)
			}
			sameConfig(t, got, load(t, outboundReference(t, s)))
		})
	}
}

func TestParityOutboundRoutes(t *testing.T) {
	all := testOutbounds()
	for _, tt := range []struct {
		name   string
		mutate func(*bridgespec.Spec)
	}{
		{"one node through an outbound, the other through the exit", func(*bridgespec.Spec) {}},
		{"with a fallback", func(s *bridgespec.Spec) { s.Outbounds[0].Fallback = true }},
		{"both nodes through one", func(s *bridgespec.Spec) { s.Nodes[1].Via = s.Nodes[0].Via }},
		{"an outbound nobody uses", func(s *bridgespec.Spec) { s.Nodes[0].Via = "" }},
		{"two outbounds, one with a fallback, ten and more", func(s *bridgespec.Spec) {
			o := all["trojan"]
			o.Tag, o.Fallback = "out-10.", true
			s.Outbounds = append(s.Outbounds, o)
			s.Nodes[1].Via = o.Tag
		}},
		{"a check of one's own", func(s *bridgespec.Spec) { s.Probe = bridgespec.Probe{URL: "http://203.0.113.9/ok", Seconds: 5} }},
		{"a blocked list of the spec's own", func(s *bridgespec.Spec) {
			s.Exit.Blocked = []string{"10.0.0.0/8", "fc00::/7"}
			s.DNS.Servers = []string{"1.1.1.1"}
		}},
		{"from the exit's address", func(s *bridgespec.Spec) { s.OutboundAddr = s.Exit.Addr }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := outboundSpec(all["vless over reality"])
			tt.mutate(s)
			cfg := outboundReference(t, s)
			if s.Exit.Blocked != nil {
				// the panel's exit has the default list; the rule takes the spec's
				exit := cfg["outbounds"].([]any)[0].(map[string]any)
				exit["settings"].(map[string]any)["finalRules"].([]any)[0].(map[string]any)["ip"] = s.Exit.Blocked
			}
			got, err := buildConfig(s, "")
			if err != nil {
				t.Fatal(err)
			}
			sameConfig(t, got, load(t, cfg))
		})
	}
}

// The router's own entry leaves through an outbound the way a node does.
func TestParityLocalEntryThroughAnOutbound(t *testing.T) {
	all := testOutbounds()
	for _, tt := range []struct {
		name   string
		mutate func(*bridgespec.Spec)
	}{
		{"the entry alone through it", func(s *bridgespec.Spec) { s.Local.Via, s.Nodes[0].Via = "out-1.", "" }},
		{"the entry and a node, with a fallback", func(s *bridgespec.Spec) {
			s.Local.Via, s.Outbounds[0].Fallback = "out-1.", true
		}},
		{"with no node", func(s *bridgespec.Spec) { s.Nodes, s.Local.Via = nil, "out-1." }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := localSpec()
			o := all["shadowsocks 128"]
			o.Tag = "out-1."
			s.Outbounds, s.OutboundAddr = []bridgespec.Outbound{o}, "192.168.50.16"
			s.Nodes[0].Via = o.Tag
			tt.mutate(s)
			got, err := buildConfig(s, testForwarder)
			if err != nil {
				t.Fatal(err)
			}
			sameConfig(t, got, load(t, outboundReference(t, s)))
		})
	}
}

// The loader's own readings of a path: the size of the early data taken off
// a ws or httpupgrade path, the spider's settings off REALITY's.
func TestParityOutboundPaths(t *testing.T) {
	all := testOutbounds()
	for _, tt := range []struct {
		name string
		base string
		edit func(*bridgespec.Outbound)
	}{
		{"early data on ws", "vless over ws and tls", func(o *bridgespec.Outbound) { o.Path = "/ws?ed=2048" }},
		{"early data beside another setting", "vless over ws and tls", func(o *bridgespec.Outbound) { o.Path = "/ws?a=1&ed=2560" }},
		{"early data on httpupgrade", "trojan over httpupgrade and tls", func(o *bridgespec.Outbound) { o.Path = "/u?ed=1024" }},
		{"a query that is not early data", "vless over ws and tls", func(o *bridgespec.Outbound) { o.Path = "/ws?x=1" }},
		{"no path at all", "vless over ws and tls", func(o *bridgespec.Outbound) { o.Path, o.Host = "", "" }},
		{"the spider's settings", "vless over reality", func(o *bridgespec.Outbound) { o.SpiderX = "/s?p=10-20&c=3&t=1-2-3&q=keep" }},
		{"every xhttp mode", "vless over xhttp", func(o *bridgespec.Outbound) { o.Mode = "stream-one" }},
		{"xhttp with no mode", "vless over xhttp", func(o *bridgespec.Outbound) { o.Mode = "" }},
		{"a randomized fingerprint", "trojan", func(o *bridgespec.Outbound) { o.Fingerprint = "randomized" }},
		{"an HTTP proxy's own ALPN", "http over tls", func(o *bridgespec.Outbound) { o.ALPN = []string{"h2", "http/1.1"} }},
		{"the fingerprint that is no fingerprint", "trojan", func(o *bridgespec.Outbound) { o.Fingerprint = "unsafe" }},
		{"an id in capitals", "vmess over tcp", func(o *bridgespec.Outbound) { o.ID = strings.ToUpper(o.ID) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := all[tt.base]
			tt.edit(&o)
			s := outboundSpec(o)
			got, err := buildConfig(s, "")
			if err != nil {
				t.Fatal(err)
			}
			sameConfig(t, got, load(t, outboundReference(t, s)))
		})
	}
}

func TestParityOutboundsIPv6(t *testing.T) {
	all := testOutbounds()
	s := testSpec()
	s.DialAddr, s.Exit.Addr, s.OutboundAddr = "fd00::18", "fd00::17", "fd00::16"
	s.Nodes[0].Addr, s.Nodes[1].Addr = "2001:db8::10", "2001:db8::20"
	s.DNS.Servers = []string{"2606:4700:4700::1111"}
	for i, name := range []string{"vless over reality", "socks, login"} {
		o := all[name]
		o.Tag, o.Addr, o.Fallback = fmt.Sprintf("out-%d.", i+1), fmt.Sprintf("2001:db8::7%d", i), i == 1
		s.Outbounds = append(s.Outbounds, o)
		s.Nodes[i].Via = o.Tag
	}
	cfg := outboundReference(t, s)
	cfg["outbounds"].([]any)[0].(map[string]any)["targetStrategy"] = "forceipv6"
	cfg["dns"].(map[string]any)["queryStrategy"] = "UseIPv6"
	got, err := buildConfig(s, "")
	if err != nil {
		t.Fatal(err)
	}
	sameConfig(t, got, load(t, cfg))
}

// Every kind of outbound the parity tests cover is one the spec accepts, and
// the other way: nothing the spec accepts goes untested.
func TestParityCoversEveryProtocolTransportAndSecurity(t *testing.T) {
	seen := map[string]bool{}
	for _, o := range testOutbounds() {
		if err := bridgespec.CheckOutbound(o); err != nil {
			t.Errorf("%s: %v", o, err)
		}
		seen[o.Protocol] = true
		seen["network "+o.NetworkOrDefault()] = true
		seen["security "+o.SecurityOrDefault()] = true
	}
	var missing []string
	for _, want := range []string{
		bridgespec.ProtoVLESS, bridgespec.ProtoVMess, bridgespec.ProtoTrojan, bridgespec.ProtoShadowsocks, bridgespec.ProtoSOCKS, bridgespec.ProtoHTTP,
		"network tcp", "network ws", "network httpupgrade", "network xhttp",
		"security none", "security tls", "security reality",
	} {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no parity case for %v", missing)
	}
}
