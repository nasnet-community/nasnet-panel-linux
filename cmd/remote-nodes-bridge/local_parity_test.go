// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The router's own entry is a node's entry, moved onto the router. The
// reference is the inbound the hub gives a node (nodecfg.Build), loaded through
// xray-core's loader. localDeltas lists every way the bridge's differs, and
// TestParityLocalEntry demands the whole config come out equal once they are
// applied, as TestParityWithDeltas does for the tunnels.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/proxy/vless"
	vlessin "github.com/xtls/xray-core/proxy/vless/inbound"
	"github.com/xtls/xray-core/transport/internet/reality"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// testForwarder stands in for the address of the bridge's target forwarder.
const testForwarder = "127.0.0.1:41000"

// localSpec is testSpec with the router's own entry switched on, the way the
// hub sends it: on the dial address, with Vision and two subscriptions.
func localSpec() *bridgespec.Spec {
	s := testSpec()
	s.Local = &bridgespec.Local{
		ListenAddr:  "192.168.50.18",
		Port:        8443,
		PrivateKey:  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		ShortIDs:    []string{"0123456789abcdef"},
		ServerNames: []string{"www.example.com"},
		Target:      "www.example.com:443",
		Flow:        bridgespec.VisionFlow,
		Clients: []bridgespec.Client{
			{Email: nodecfg.SubscriptionEmail(1), UUID: "11111111-2222-4333-8444-555555555551"},
			{Email: nodecfg.SubscriptionEmail(2), UUID: "11111111-2222-4333-8444-555555555552"},
		},
		Token: "0123456789abcdef0123456789abcdef",
	}
	return s
}

// nodeEntry is the entry inbound the hub gives a node for the same key, names
// and clients, as generic JSON.
func nodeEntry(t *testing.T, l *bridgespec.Local) map[string]any {
	t.Helper()
	in := nodecfg.Input{
		EntryPort:   int(l.Port),
		RealityDest: l.Target,
		ServerNames: l.ServerNames,
		PrivateKey:  l.PrivateKey,
		ShortIDs:    l.ShortIDs,
		Bridge: nodecfg.Bridge{
			UUID:       "99999999-2222-4333-8444-555555555559",
			Email:      nodecfg.BridgeEmail(1),
			ReverseTag: nodecfg.BridgeReverseTag(1),
		},
	}
	for _, c := range l.Clients {
		in.Clients = append(in.Clients, nodecfg.Client{UUID: c.UUID, Email: c.Email})
	}
	raw, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, ib := range cfg["inbounds"].([]any) {
		if entry := ib.(map[string]any); entry["tag"] == nodecfg.EntryTag {
			return entry
		}
	}
	t.Fatal("the node's config has no entry inbound")
	return nil
}

// localDeltas edits a node's entry inbound into the router's. This list is
// the documentation of every difference.
//
//  1. It is tagged "local", not "entry".
//  2. It listens on the entry's one address, not on every address: the router
//     forwards its public port there, and that address's replies leave by the
//     Domestic link.
//  3. It has no client for the bridge. There is no tunnel to register: the
//     bridge is this very process.
//  4. Its REALITY target is the bridge's forwarder, which reaches the real
//     target from the entry's address (target.go).
func localDeltas(t *testing.T, entry map[string]any, l *bridgespec.Local, forwarder string) {
	t.Helper()
	entry["tag"] = bridgespec.LocalTag // 1
	entry["listen"] = l.ListenAddr     // 2

	settings := entry["settings"].(map[string]any)
	clients := settings["clients"].([]any)
	last := clients[len(clients)-1].(map[string]any)
	if _, isBridge := last["reverse"]; !isBridge {
		t.Fatalf("the node's last client is not the bridge's: %v", last)
	}
	settings["clients"] = clients[:len(clients)-1] // 3

	entry["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["dest"] = forwarder // 4
}

// localReference is the loader's config for s: the panel's bridge config with
// the bridge's deltas, plus a node's entry with the entry's deltas and the
// one rule that sends it to the exit.
func localReference(t *testing.T, s *bridgespec.Spec) map[string]any {
	t.Helper()
	cfg := panelBridgeConfig(t, s)
	bridgeDeltas(t, cfg, s)

	entry := nodeEntry(t, s.Local)
	localDeltas(t, entry, s.Local, testForwarder)
	cfg["inbounds"] = []any{entry}

	// after the lookups' rule, before the tunnels'
	routing := cfg["routing"].(map[string]any)
	rules := routing["rules"].([]any)
	localRule := map[string]any{"type": "field", "inboundTag": []string{bridgespec.LocalTag}, "outboundTag": bridgespec.ExitTag}
	routing["rules"] = append([]any{rules[0], localRule}, rules[1:]...)
	return cfg
}

func TestParityLocalEntry(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*bridgespec.Spec)
	}{
		{"beside two nodes", func(*bridgespec.Spec) {}},
		{"with no node", func(s *bridgespec.Spec) { s.Nodes = nil }},
		{"no client yet", func(s *bridgespec.Spec) { s.Local.Clients = nil }},
		{"one client", func(s *bridgespec.Spec) { s.Local.Clients = s.Local.Clients[:1] }},
		{"capitals in a client's id", func(s *bridgespec.Spec) {
			s.Local.Clients[0].UUID = strings.ToUpper(s.Local.Clients[0].UUID)
		}},
		{"two short ids, one of a single byte", func(s *bridgespec.Spec) { s.Local.ShortIDs = []string{"ab", "0123456789abcdef"} }},
		{"two server names", func(s *bridgespec.Spec) { s.Local.ServerNames = []string{"www.example.com", "example.com"} }},
		{"another port", func(s *bridgespec.Spec) { s.Local.Port = 443 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := localSpec()
			tt.mutate(s)
			want := load(t, localReference(t, s))
			got, err := buildConfig(s, testForwarder)
			if err != nil {
				t.Fatal(err)
			}
			sameConfig(t, got, want)
		})
	}
}

// The entry on an IPv6 router keeps the same shape.
func TestParityLocalEntryIPv6(t *testing.T) {
	s := localSpec()
	s.DialAddr, s.Exit.Addr = "fd00::18", "fd00::17"
	s.Nodes[0].Addr, s.Nodes[1].Addr = "2001:db8::10", "2001:db8::20"
	s.DNS.Servers = []string{"2606:4700:4700::1111"}
	s.Local.ListenAddr = "fd00::18"
	cfg := localReference(t, s)
	cfg["outbounds"].([]any)[0].(map[string]any)["targetStrategy"] = "forceipv6"
	cfg["dns"].(map[string]any)["queryStrategy"] = "UseIPv6"
	got, err := buildConfig(s, testForwarder)
	if err != nil {
		t.Fatal(err)
	}
	sameConfig(t, got, load(t, cfg))
}

// Without an entry the bridge listens on nothing but its status page.
func TestNoLocalEntryNoInbound(t *testing.T) {
	cfg, err := buildConfig(testSpec(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbound) != 0 {
		t.Fatalf("%d inbound(s) with the entry off", len(cfg.Inbound))
	}
}

func TestLocalEntryStructure(t *testing.T) {
	s := localSpec()
	s.Local.ShortIDs = []string{"ab"}
	cfg, err := buildConfig(s, testForwarder)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbound) != 1 || cfg.Inbound[0].Tag != bridgespec.LocalTag {
		t.Fatalf("inbounds: %v", cfg.Inbound)
	}
	in := cfg.Inbound[0]

	recv := instance(t, in.ReceiverSettings).(*proxyman.ReceiverConfig)
	if got := recv.Listen.AsAddress().String(); got != "192.168.50.18" {
		t.Errorf("listens on %s", got)
	}
	if ports := recv.PortList.Ports(); len(ports) != 1 || ports[0] != 8443 {
		t.Errorf("listens on ports %v", ports)
	}
	if sn := recv.SniffingSettings; sn == nil || !sn.Enabled || strings.Join(sn.DestinationOverride, ",") != "http,tls" || sn.RouteOnly || sn.MetadataOnly {
		t.Errorf("sniffing: %v", sn)
	}
	st := recv.StreamSettings
	if st.ProtocolName != "tcp" || len(st.SecuritySettings) != 1 || st.SecurityType != st.SecuritySettings[0].Type {
		t.Fatalf("stream settings: %v", st)
	}
	rc := instance(t, st.SecuritySettings[0]).(*reality.Config)
	if rc.Dest != testForwarder || rc.Type != "tcp" || rc.Xver != 0 {
		t.Errorf("REALITY takes strangers to %s %q (xver %d), want the forwarder", rc.Type, rc.Dest, rc.Xver)
	}
	if !bytes.Equal(rc.PrivateKey, bytes.Repeat([]byte{9}, 32)) {
		t.Error("the private key is not the spec's")
	}
	if len(rc.ShortIds) != 1 || !bytes.Equal(rc.ShortIds[0], []byte{0xab, 0, 0, 0, 0, 0, 0, 0}) {
		t.Errorf("short ids %x, want ab padded to 8 bytes", rc.ShortIds)
	}
	if strings.Join(rc.ServerNames, ",") != "www.example.com" {
		t.Errorf("server names %v", rc.ServerNames)
	}
	// the client half of the message stays empty: this end only answers
	if len(rc.PublicKey) != 0 || rc.ServerName != "" || rc.Fingerprint != "" || rc.Show || rc.MasterKeyLog != "" {
		t.Errorf("REALITY settings carry more than a server's: %v", rc)
	}

	px := instance(t, in.ProxySettings).(*vlessin.Config)
	if px.Decryption != "none" || len(px.Fallbacks) != 0 {
		t.Errorf("decryption %q, %d fallbacks", px.Decryption, len(px.Fallbacks))
	}
	if len(px.Users) != 2 {
		t.Fatalf("%d users", len(px.Users))
	}
	for i, u := range px.Users {
		want := s.Local.Clients[i]
		acct := instance(t, u.Account).(*vless.Account)
		if u.Email != want.Email || u.Level != 0 || acct.Id != want.UUID || acct.Flow != bridgespec.VisionFlow {
			t.Errorf("user %d: %s level %d id %s flow %q", i, u.Email, u.Level, acct.Id, acct.Flow)
		}
		if acct.Reverse != nil || acct.Encryption != "" {
			t.Errorf("user %d carries more than a subscription's login: %v", i, acct)
		}
	}
}

// A spec without a flow gives clients without one, which is what the tests
// that run under the race detector use.
func TestLocalEntryWithoutFlow(t *testing.T) {
	s := localSpec()
	s.Local.Flow = ""
	cfg, err := buildConfig(s, testForwarder)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range instance(t, cfg.Inbound[0].ProxySettings).(*vlessin.Config).Users {
		if flow := instance(t, u.Account).(*vless.Account).Flow; flow != "" {
			t.Errorf("%s has flow %q", u.Email, flow)
		}
	}
}

// What the entry's clients send goes where the tunnels' traffic goes: the
// exit, with its blocked ranges, and nowhere else.
func TestLocalEntryGoesToTheExit(t *testing.T) {
	cfg, err := buildConfig(localSpec(), testForwarder)
	if err != nil {
		t.Fatal(err)
	}
	var rules []*router.RoutingRule
	for _, app := range cfg.App {
		if rc, ok := instance(t, app).(*router.Config); ok {
			rules = rc.Rule
		}
	}
	// lookups, the entry, then one per tunnel
	if len(rules) != 4 {
		t.Fatalf("%d rules", len(rules))
	}
	for i, wantTag := range []string{bridgespec.DNSTag, bridgespec.LocalTag, "rvs-from-n1", "rvs-from-n2"} {
		r := rules[i]
		if len(r.InboundTag) != 1 || r.InboundTag[0] != wantTag || r.GetTag() != bridgespec.ExitTag {
			t.Errorf("rule %d: from %v to %q, want from %s to the exit", i, r.InboundTag, r.GetTag(), wantTag)
		}
	}
	if cfg.Outbound[0].Tag != bridgespec.ExitTag {
		t.Errorf("the first outbound is %q: a flow no rule claims would not land on the exit", cfg.Outbound[0].Tag)
	}
}

func TestLocalEntryNeedsItsForwarder(t *testing.T) {
	for _, target := range []string{"", "127.0.0.1", "forwarder"} {
		if _, err := buildConfig(localSpec(), target); err == nil || !strings.Contains(err.Error(), "local") {
			t.Errorf("target %q: got %v", target, err)
		}
	}
}
