// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

const outUUID = "7f8b1a9e-3c2d-4b5a-9e6f-0a1b2c3d4e5f"

// realityOut is a VLESS outbound over REALITY, given by name.
func realityOut() model.OutboundSettings {
	return model.OutboundSettings{
		Protocol: "vless", Host: "de.example.com", Port: 443, ID: outUUID, Flow: bridgespec.VisionFlow,
		Network: "tcp", Security: "reality", ServerName: "www.example.com", PublicKey: fixtureKey, ShortID: "ab12",
	}
}

// wsOut is a VMess outbound over plain ws, given by name.
func wsOut() model.OutboundSettings {
	return model.OutboundSettings{Protocol: "vmess", Host: "ws.example.com", Port: 80, ID: outUUID, Network: "ws", Path: "/ray", Security: "none"}
}

func outbound(id uint, name string, s model.OutboundSettings) model.Outbound {
	return model.Outbound{ID: id, Name: name, CreatedAt: epoch, Settings: s}
}

// outInput is input with outbounds, reached from the Foreign link's address.
// The names in the table resolve, and no other; looked counts the lookups.
func outInput(nodes []model.Node, outs ...model.Outbound) (specInput, *int) {
	in := input(nodes...)
	looked := new(int)
	names := map[string]string{"de.example.com": "198.51.100.7", "ws.example.com": "198.51.100.8"}
	in.outbounds, in.from = outs, netip.MustParseAddr("192.168.150.80")
	in.probe = bridgespec.Probe{URL: "https://example.com/204"}
	in.outAddr = func(o model.Outbound) (netip.Addr, error) {
		*looked++
		if a, err := netip.ParseAddr(o.Settings.Host); err == nil {
			return a, nil
		}
		if a, ok := names[o.Settings.Host]; ok {
			return netip.MustParseAddr(a), nil
		}
		return netip.Addr{}, errors.New("the host name does not resolve: no such host")
	}
	return in, looked
}

func via(n model.Node, outbound uint) model.Node {
	n.OutboundID = outbound
	return n
}

// An outbound nothing leaves through is not in the spec, so it can come and
// go without the bridge noticing; one a node leaves through is, once.
func TestOnlyTheOutboundsInUseAreInTheSpec(t *testing.T) {
	plain, _, err := buildSpec(input(readyNode(1), readyNode(2)))
	if err != nil {
		t.Fatal(err)
	}
	outs := []model.Outbound{outbound(1, "Frankfurt", realityOut()), outbound(2, "Paris", wsOut())}

	in, looked := outInput([]model.Node{readyNode(1), readyNode(2)}, outs...)
	unused, _, err := buildSpec(in)
	if err != nil || !reflect.DeepEqual(unused, plain) || specHash(unused) != specHash(plain) || *looked != 0 {
		t.Fatalf("with outbounds nothing uses: spec %+v, err %v, %d lookups", unused, err, *looked)
	}

	in, looked = outInput([]model.Node{readyNode(1), via(readyNode(2), 2)}, outs...)
	res, err := makeSpec(in)
	if err != nil || res.spec == nil {
		t.Fatalf("spec %v, err %v", res.spec, err)
	}
	spec := res.spec
	if len(spec.Outbounds) != 1 || spec.Outbounds[0].Tag != "out-2." || spec.Outbounds[0].Addr != "198.51.100.8" {
		t.Fatalf("outbounds %+v", spec.Outbounds)
	}
	if spec.Nodes[0].Via != "" || spec.Nodes[1].Via != "out-2." || spec.OutboundAddr != "192.168.150.80" || spec.Probe.URL != "https://example.com/204" {
		t.Errorf("spec %+v", spec)
	}
	if *looked != 1 || res.outs[2].tag != "out-2." || len(res.outs) != 1 {
		t.Errorf("%d lookups, uses %+v", *looked, res.outs)
	}
	if specHash(spec) == specHash(plain) {
		t.Error("a route to an outbound didn't change the hash")
	}
	must(t, spec.Validate())

	// two nodes through one outbound: it is there once
	in, _ = outInput([]model.Node{via(readyNode(1), 2), via(readyNode(2), 2)}, outs...)
	spec, _, _ = buildSpec(in)
	if len(spec.Outbounds) != 1 || spec.Nodes[0].Via != "out-2." || spec.Nodes[1].Via != "out-2." {
		t.Errorf("two nodes through one outbound: %+v", spec)
	}
	// and a node that is not ready doesn't bring its outbound in
	n2 := readyNode(2)
	n2.RealityPublicKey = ""
	alone, _, _ := buildSpec(input(readyNode(1), n2))
	in, looked = outInput([]model.Node{readyNode(1), via(n2, 1)}, outs...)
	spec, _, _ = buildSpec(in)
	if !reflect.DeepEqual(spec, alone) || *looked != 0 {
		t.Errorf("an outbound only a node that isn't ready leaves through: %+v, %d lookups", spec, *looked)
	}
}

// Changing an outbound that is in use, or a route, is a new spec. Changing one
// that isn't, isn't.
func TestTheHashFollowsTheOutboundsInUse(t *testing.T) {
	outs := func() []model.Outbound {
		return []model.Outbound{outbound(1, "Frankfurt", realityOut()), outbound(2, "Paris", wsOut())}
	}
	base, _ := outInput([]model.Node{via(readyNode(1), 1), readyNode(2)}, outs()...)
	a, _, _ := buildSpec(base)
	for name, c := range map[string]struct {
		mod    func(*specInput)
		change bool
	}{
		"the outbound in use renamed":        {func(in *specInput) { in.outbounds[0].Name = "Berlin" }, false},
		"an outbound not in use changed":     {func(in *specInput) { in.outbounds[1].Settings.Path = "/other" }, false},
		"its fallback switched":              {func(in *specInput) { in.outbounds[0].Fallback = true }, true},
		"its port changed":                   {func(in *specInput) { in.outbounds[0].Settings.Port = 8443 }, true},
		"a node sent through another":        {func(in *specInput) { in.nodes[0].OutboundID = 2 }, true},
		"the other node sent through one":    {func(in *specInput) { in.nodes[1].OutboundID = 1 }, true},
		"the node sent back through the VPN": {func(in *specInput) { in.nodes[0].OutboundID = 0 }, true},
		"another check":                      {func(in *specInput) { in.probe.URL = "https://example.org/" }, true},
	} {
		in, _ := outInput([]model.Node{via(readyNode(1), 1), readyNode(2)}, outs()...)
		c.mod(&in)
		b, _, err := buildSpec(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if changed := specHash(b) != specHash(a); changed != c.change {
			t.Errorf("%s: the hash changed %v, want %v", name, changed, c.change)
		}
	}
	// the check is in the spec only with an outbound
	in := input(readyNode(1))
	in.probe = bridgespec.Probe{URL: "https://example.org/"}
	plain, _, _ := buildSpec(input(readyNode(1)))
	if spec, _, _ := buildSpec(in); specHash(spec) != specHash(plain) {
		t.Error("a check with no outbound changed the spec")
	}
}

// An outbound whose name doesn't resolve, or that the bridge refuses, can't go
// into the spec. What leaves through it leaves through the exit when its
// fallback is on, and is left out when it is off.
func TestWhatLeavesThroughAnOutboundThatCantBeUsed(t *testing.T) {
	for name, c := range map[string]struct {
		host     string
		fallback bool
		says     string
	}{
		"a name that won't resolve, with the fallback":    {"gone.example.com", true, "does not resolve"},
		"a name that won't resolve, without the fallback": {"gone.example.com", false, "does not resolve"},
		"refused by the bridge, with the fallback":        {"2001:db8::7", true, "address family"},
		"refused by the bridge, without the fallback":     {"2001:db8::7", false, "address family"},
	} {
		t.Run(name, func(t *testing.T) {
			s := realityOut()
			s.Host = c.host
			o := outbound(1, "Frankfurt", s)
			o.Fallback = c.fallback
			in, _ := outInput([]model.Node{via(readyNode(1), 1), readyNode(2)}, o)
			res, err := makeSpec(in)
			if err != nil || res.spec == nil {
				t.Fatalf("spec %v, err %v", res.spec, err)
			}
			use := res.outs[1]
			if use.tag != "" || use.fallback != c.fallback || !strings.Contains(use.err, c.says) || len(res.spec.Outbounds) != 0 {
				t.Errorf("use %+v, outbounds %+v", use, res.spec.Outbounds)
			}
			g := res.gaps[1]
			if c.fallback {
				if !g.ok() || len(res.spec.Nodes) != 2 || res.spec.Nodes[0].Via != "" {
					t.Errorf("with the fallback the node should leave through the exit: gap %q, nodes %+v", g, res.spec.Nodes)
				}
				return
			}
			if g.kind != gapOutbound || !strings.Contains(g.String(), "the outbound it leaves through can't be used: Frankfurt: ") ||
				!strings.Contains(g.String(), c.says) {
				t.Errorf("gap = %q", g)
			}
			if len(res.spec.Nodes) != 1 || res.spec.Nodes[0].Tag != "node-2" {
				t.Errorf("nodes %+v", res.spec.Nodes)
			}
		})
	}
}

// localBlockFor is the router's own entry as the bridge has it, switched on
// and ready.
func localBlockFor() *bridgespec.Local {
	return &bridgespec.Local{
		ListenAddr: "192.168.150.1", Port: DefaultLocalPort, PrivateKey: fixtureKey, ShortIDs: []string{"a1b2"},
		ServerNames: []string{"www.example.com"}, Target: "www.example.com:443", Flow: bridgespec.VisionFlow,
		Token: strings.Repeat("a", 32),
	}
}

func TestTheRoutersEntryLeavesThroughAnOutboundToo(t *testing.T) {
	good := outbound(1, "Frankfurt", realityOut())
	in, _ := outInput(nil, good)
	in.local, in.localVia = localBlockFor(), 1
	res, err := makeSpec(in)
	if err != nil || res.spec == nil || res.spec.Local == nil || res.spec.Local.Via != "out-1." || len(res.spec.Outbounds) != 1 || res.localGap != "" {
		t.Fatalf("spec %+v, err %v", res.spec, err)
	}
	if in.local.Via != "" {
		t.Error("the input's entry was changed")
	}
	must(t, res.spec.Validate())

	// switched off, its route brings nothing in
	in.local = nil
	if res, _ := makeSpec(in); res.spec != nil || len(res.outs) != 0 {
		t.Fatalf("an entry that is off: %+v", res)
	}

	gone := good
	gone.Settings.Host = "gone.example.com"
	in, _ = outInput([]model.Node{readyNode(1)}, gone)
	in.local, in.localVia = localBlockFor(), 1
	res, err = makeSpec(in)
	if err != nil || res.spec == nil || res.spec.Local != nil || len(res.spec.Nodes) != 1 || !strings.Contains(res.localGap, "Frankfurt: the host name does not resolve") {
		t.Fatalf("without the fallback the entry is left out: %+v, gap %q, err %v", res.spec, res.localGap, err)
	}
	// with no node beside it, there is nothing left to run
	in.nodes = nil
	if res, _ := makeSpec(in); res.spec != nil || res.localGap == "" {
		t.Fatalf("the entry alone, left out: %+v", res)
	}
	gone.Fallback = true
	in, _ = outInput(nil, gone)
	in.local, in.localVia = localBlockFor(), 1
	if res, _ := makeSpec(in); res.spec == nil || res.spec.Local == nil || res.spec.Local.Via != "" || res.localGap != "" {
		t.Fatalf("with the fallback the entry leaves through the exit: %+v", res)
	}
}

// Decision 4: a name on a plain transport other than tcp goes into the
// transport's host, since the bridge connects to an address.
func TestANameIsTheHostOfAPlainTransport(t *testing.T) {
	addr := netip.MustParseAddr("198.51.100.8")
	for name, c := range map[string]struct {
		mod  func(*model.OutboundSettings)
		want string
	}{
		"ws by name":                    {func(*model.OutboundSettings) {}, "ws.example.com"},
		"httpupgrade by name":           {func(s *model.OutboundSettings) { s.Network = "httpupgrade" }, "ws.example.com"},
		"xhttp by name":                 {func(s *model.OutboundSettings) { s.Network = "xhttp" }, "ws.example.com"},
		"a host of its own":             {func(s *model.OutboundSettings) { s.HostHeader = "cdn.example.org" }, "cdn.example.org"},
		"over tls":                      {func(s *model.OutboundSettings) { s.Security, s.ServerName = "tls", "ws.example.com" }, ""},
		"over tcp":                      {func(s *model.OutboundSettings) { s.Network, s.Path = "tcp", "" }, ""},
		"by address":                    {func(s *model.OutboundSettings) { s.Host = "198.51.100.8" }, ""},
		"transport and security unsaid": {func(s *model.OutboundSettings) { s.Network, s.Security, s.Path = "", "", "" }, ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := wsOut()
			c.mod(&s)
			o := outbound(7, "x", s)
			o.Fallback = true
			got := specOutbound(o, addr)
			if got.Host != c.want || got.Tag != "out-7." || got.Addr != "198.51.100.8" || !got.Fallback || got.ID != outUUID {
				t.Errorf("got %+v, want the host %q", got, c.want)
			}
		})
	}
}

func TestOutboundTags(t *testing.T) {
	for _, id := range []uint{1, 2, 10, 4294967295} {
		got, ok := idOfOutTag(outTag(id))
		if !ok || got != id {
			t.Errorf("tag of %d came back as %d, %v", id, got, ok)
		}
	}
	for _, s := range []string{"", "out-", "out-.", "out-0.", "out-1", "node-1", "out-x.", "group-out-1.", "out-1.."} {
		if id, ok := idOfOutTag(s); ok {
			t.Errorf("idOfOutTag(%q) = %d", s, id)
		}
	}
	if strings.HasPrefix(outTag(10), outTag(1)) {
		t.Error("one outbound's tag is the start of another's, which the bridge refuses")
	}
}
