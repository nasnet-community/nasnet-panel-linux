// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const outboundID = "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f"

var outboundKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))

// realityOut is a VLESS outbound over REALITY, given by name.
func realityOut() OutboundSettings {
	return OutboundSettings{
		Protocol: "vless", Host: "de.example.com", Port: 443, ID: outboundID, Flow: "xtls-rprx-vision",
		Network: "tcp", Security: "reality", ServerName: "www.example.com", Fingerprint: "chrome",
		PublicKey: outboundKey, ShortID: "ab12",
	}
}

// trojanOut is a Trojan outbound over ws and tls, given by address.
func trojanOut() OutboundSettings {
	return OutboundSettings{
		Protocol: "trojan", Host: "198.51.100.7", Port: 8443, Password: "trojan-password-marker",
		Network: "ws", Path: "/ws", Security: "tls", ServerName: "cdn.example.com", ALPN: []string{"h2", "http/1.1"},
	}
}

// withOutbounds is withLocal with two outbounds, node 1 leaving through the
// first.
func withOutbounds(t *testing.T) State {
	t.Helper()
	s := withLocal(t)
	for _, o := range []Outbound{{Name: "Frankfurt", CreatedAt: t0, Settings: realityOut()}, {Name: "Paris", Fallback: true, CreatedAt: t0, Settings: trojanOut()}} {
		if _, err := s.AddOutbound(o); err != nil {
			t.Fatal(err)
		}
	}
	s.Nodes[0].OutboundID = 1
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOutboundSettingsAreCheckedAsTheBridgeWillUseThem(t *testing.T) {
	fine := map[string]func(*OutboundSettings){
		"vless over reality":    func(*OutboundSettings) {},
		"an address for a host": func(o *OutboundSettings) { o.Host = "2001:db8::5" },
		"transport and security unsaid": func(o *OutboundSettings) {
			*o = OutboundSettings{Protocol: "socks", Host: "198.51.100.9", Port: 1080}
		},
		"shadowsocks 2022": func(o *OutboundSettings) {
			*o = OutboundSettings{Protocol: "shadowsocks", Host: "198.51.100.9", Port: 8388, Method: "2022-blake3-aes-128-gcm",
				Password: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))}
		},
	}
	for name, mod := range fine {
		t.Run(name, func(t *testing.T) {
			s := realityOut()
			mod(&s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}

	bad := map[string]struct {
		mod  func(*OutboundSettings)
		says string
	}{
		"no host":                     {func(o *OutboundSettings) { o.Host = "" }, "settings.host: the host is required"},
		"a host that is none":         {func(o *OutboundSettings) { o.Host = "not a host" }, "settings.host"},
		"a host not in its form":      {func(o *OutboundSettings) { o.Host = "DE.example.com" }, `settings.host: it must be written "de.example.com"`},
		"no port":                     {func(o *OutboundSettings) { o.Port = 0 }, "settings.port"},
		"a port too large":            {func(o *OutboundSettings) { o.Port = 70000 }, "settings.port"},
		"a protocol the bridge lacks": {func(o *OutboundSettings) { o.Protocol = "wireguard" }, "settings.protocol"},
		"an id that is no UUID":       {func(o *OutboundSettings) { o.ID = "not-a-uuid" }, "settings.id"},
		"vision over ws":              {func(o *OutboundSettings) { o.Network = "ws" }, "settings."},
		"a server name that is none":  {func(o *OutboundSettings) { o.ServerName = "not a name" }, "settings.serverName"},
		"a public key that is none":   {func(o *OutboundSettings) { o.PublicKey = "AQID" }, "settings.publicKey"},
		"a short id that is no hex":   {func(o *OutboundSettings) { o.ShortID = "zz" }, "settings.shortId"},
		"a spider path with a space":  {func(o *OutboundSettings) { o.SpiderX = "/a b" }, "settings.spiderX"},
		"a transport host header": {func(o *OutboundSettings) {
			*o = trojanOut()
			o.HostHeader = "not a name"
		}, "settings.hostHeader"},
		"a password VLESS has none of": {func(o *OutboundSettings) { o.Password = "x" }, "settings.password"},
	}
	for name, c := range bad {
		t.Run(name, func(t *testing.T) {
			s := realityOut()
			c.mod(&s)
			err := s.Validate()
			var inv *InvalidError
			if !errors.As(err, &inv) || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("got %v, want an InvalidError saying %q", err, c.says)
			}
			if strings.Contains(err.Error(), "trojan-password-marker") {
				t.Fatalf("the error quotes the password: %v", err)
			}
		})
	}
}

func TestCleaningSettingsFillsWhatWasLeftOut(t *testing.T) {
	got, err := CleanOutboundSettings(OutboundSettings{Protocol: "http", Host: "  Proxy.Example.COM ", Port: 3128, ALPN: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	want := OutboundSettings{Protocol: "http", Host: "proxy.example.com", Port: 3128, Network: "tcp", Security: "none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !got.HostIsName() || trojanOut().HostIsName() {
		t.Error("a name and an address are told apart wrongly")
	}
	in := trojanOut()
	out, _ := CleanOutboundSettings(in)
	out.ALPN[0] = "changed"
	if in.ALPN[0] != "h2" {
		t.Error("the cleaned settings share their ALPN with what was given")
	}
}

func TestOutboundNamesAreShorterThanANodes(t *testing.T) {
	for _, ok := range []string{"x", strings.Repeat("a", 40), strings.Repeat("ع", 40), "فرانکفورت ۱"} {
		if _, err := CleanOutboundName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "  ", strings.Repeat("a", 41), "a\nb"} {
		if _, err := CleanOutboundName(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	if got, _ := CleanOutboundName("  Frankfurt \t"); got != "Frankfurt" {
		t.Errorf("trimmed to %q", got)
	}
}

func TestAddOutboundStopsAtTheCapAndRefusesTakenNames(t *testing.T) {
	s := NewState()
	for i := 1; i <= MaxOutbounds; i++ {
		o, err := s.AddOutbound(Outbound{Name: fmt.Sprintf("out %d", i), CreatedAt: t0, Settings: trojanOut()})
		if err != nil || o.ID != uint(i) {
			t.Fatalf("outbound %d: %+v, %v", i, o, err)
		}
	}
	if _, err := s.AddOutbound(Outbound{Name: "one more", CreatedAt: t0, Settings: trojanOut()}); !errors.Is(err, ErrOutboundLimit) {
		t.Fatalf("the seventeenth: %v", err)
	}
	if err := s.RemoveOutbound(3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddOutbound(Outbound{Name: "OUT 4", CreatedAt: t0, Settings: trojanOut()}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a name differing only in case: %v", err)
	}
	o, err := s.AddOutbound(Outbound{Name: "out 3", CreatedAt: t0, Settings: trojanOut()})
	if err != nil || o.ID != MaxOutbounds+1 {
		t.Fatalf("a freed name gets a new id: %+v, %v", o, err)
	}
	if _, err := s.AddOutbound(Outbound{Name: "bad", CreatedAt: t0}); err == nil {
		t.Fatal("an outbound with no settings was taken")
	}
	if err := s.RenameOutbound(1, "Out 2"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a rename onto a taken name: %v", err)
	}
	if err := s.RenameOutbound(1, "OUT 1"); err != nil {
		t.Fatalf("a rename of its own case: %v", err)
	}
	if err := s.RenameOutbound(99, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renaming one that isn't there: %v", err)
	}
}

func TestAnOutboundSomethingLeavesThroughIsNotRemoved(t *testing.T) {
	s := withOutbounds(t)
	s.Local.OutboundID = 2
	if nodes, router := s.LeavingThrough(1); !reflect.DeepEqual(nodes, []uint{1}) || router {
		t.Fatalf("outbound 1 is named by %v, router %v", nodes, router)
	}
	if nodes, router := s.LeavingThrough(0); nodes != nil || router {
		t.Fatalf("the VPN is no outbound: %v, %v", nodes, router)
	}
	for _, id := range []uint{1, 2} {
		if err := s.RemoveOutbound(id); !errors.Is(err, ErrOutboundInUse) {
			t.Errorf("outbound %d in use: %v", id, err)
		}
	}
	if err := s.RemoveOutbound(9); !errors.Is(err, ErrNotFound) {
		t.Errorf("one that isn't there: %v", err)
	}
	// a node removed takes its route with it
	if err := s.RemoveNode(1); err != nil {
		t.Fatal(err)
	}
	s.Local.OutboundID = 0
	for _, id := range []uint{1, 2} {
		if err := s.RemoveOutbound(id); err != nil {
			t.Errorf("outbound %d, used by nothing: %v", id, err)
		}
	}
	if len(s.Outbounds) != 0 || s.NextOutboundID != 3 {
		t.Fatalf("%d outbounds left, counter %d", len(s.Outbounds), s.NextOutboundID)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOutboundsAndTheirRoutesAreChecked(t *testing.T) {
	cases := map[string]func(*State){
		"zero outbound counter":         func(s *State) { s.NextOutboundID = 0 },
		"outbound id used twice":        func(s *State) { s.Outbounds[1].ID = 1 },
		"outbound id not below counter": func(s *State) { s.NextOutboundID = 2 },
		"outbound without id":           func(s *State) { s.Outbounds[0].ID = 0 },
		"outbound without a time":       func(s *State) { s.Outbounds[0].CreatedAt = time.Time{} },
		"outbound with a long name":     func(s *State) { s.Outbounds[0].Name = strings.Repeat("a", 41) },
		"two outbounds by one name":     func(s *State) { s.Outbounds[1].Name = "FRANKFURT" },
		"bad outbound settings":         func(s *State) { s.Outbounds[0].Settings.Port = 0 },
		"a node's route to nothing":     func(s *State) { s.Nodes[1].OutboundID = 9 },
		"the router's route to nothing": func(s *State) { s.Local.OutboundID = 9 },
		"a check that is no address":    func(s *State) { s.ProbeURL = "ftp://example.com/" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := withOutbounds(t)
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	s := withOutbounds(t)
	s.Outbounds[1].Name = "frankfurt"
	if err := s.Validate(); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a taken name: %v", err)
	}
	s = withOutbounds(t)
	s.ProbeURL, s.Local.OutboundID = "https://example.com/check", 2
	if err := s.Validate(); err != nil {
		t.Fatalf("a check of its own and the router leaving through one: %v", err)
	}
}

func TestCheckEditRefusesAnOutboundPastTheCap(t *testing.T) {
	before := NewState()
	for i := 0; i < MaxOutbounds; i++ {
		if _, err := before.AddOutbound(Outbound{Name: fmt.Sprint("o", i), CreatedAt: t0, Settings: trojanOut()}); err != nil {
			t.Fatal(err)
		}
	}
	edit := before.Clone()
	edit.Outbounds = append(edit.Outbounds, Outbound{ID: edit.NextOutboundID, Name: "more", CreatedAt: t0, Settings: trojanOut()})
	edit.NextOutboundID++
	if err := edit.CheckEdit(before); !errors.Is(err, ErrOutboundLimit) {
		t.Fatalf("a seventeenth outbound: %v", err)
	}
	edit = before.Clone()
	if err := edit.RemoveOutbound(edit.NextOutboundID - 1); err != nil {
		t.Fatal(err)
	}
	edit.NextOutboundID--
	if err := edit.CheckEdit(before); err == nil {
		t.Fatal("the outbounds' counter turned back and nothing said so")
	}
}

// A file written before there were outbounds is version 2 and says nothing of
// them. It loads with none, every route the VPN, and is version 3 from the
// next write on.
func TestAFileFromBeforeOutboundsLoadsWithEverythingThroughTheVPN(t *testing.T) {
	want := withLocal(t)
	b, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = 2
	for _, k := range []string{"outbounds", "next_outbound_id", "probe_url"} {
		delete(doc, k)
	}
	old, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(old)
	if err != nil {
		t.Fatalf("a version 2 file: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a version 2 file came back as\n%+v\nwant\n%+v", got, want)
	}
	if got.Outbounds != nil || got.NextOutboundID != 1 || got.Nodes[0].OutboundID != 0 || got.Local.OutboundID != 0 {
		t.Fatalf("outbounds %v, counter %d", got.Outbounds, got.NextOutboundID)
	}
	again, err := Encode(got)
	if err != nil || !bytes.Equal(again, b) {
		t.Fatalf("written again it is not this version's file: %v", err)
	}
	// and a file of this version that lost its counter is broken, not old
	doc["version"] = SchemaVersion
	broken, _ := json.Marshal(doc)
	if _, err := Decode(broken); err == nil {
		t.Fatal("a version 3 file with no outbound counter was taken")
	}
}

// The outbounds go through the file like the rest, their logins included: the
// file is the only copy.
func TestOutboundsSurviveTheFile(t *testing.T) {
	s := withOutbounds(t)
	s.ProbeURL, s.Local.OutboundID = "https://example.com/check", 2
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{outboundID, "trojan-password-marker", `"outbound_id": 1`, `"outbound_id": 2`, `"next_outbound_id": 3`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("the file does not hold %q", want)
		}
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("round trip changed the state:\n got %+v\nwant %+v", back, s)
	}
}
