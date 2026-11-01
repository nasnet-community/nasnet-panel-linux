// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// readyNode is a node the bridge can carry, on the given id.
func readyNode(id uint) model.Node {
	n := model.Node{
		ID: id, Name: "node " + string(rune('0'+id)), Host: "203.0.113." + string(rune('0'+id)), SSHPort: 22, AgentPort: 9090, EntryPort: 443,
		State: model.NodeRegistered, CreatedAt: epoch, RealityTarget: "www.example.com:443", RealitySNI: "www.example.com",
		RealityPublicKey: fixtureKey, RealityShortID: "a1b2c3d4", AgentVersion: "1.3.0", XrayVersion: pin.Version, BridgeUUID: bridgeIDOf(id),
	}
	return n
}

// input is a spec input where every node runs the pinned Xray and an address
// is its host, unless a table says otherwise.
func input(nodes ...model.Node) specInput {
	return specInput{
		nodes: nodes,
		xray:  func(model.Node) string { return pin.Version },
		addr: func(n model.Node) (netip.Addr, error) {
			a, err := netip.ParseAddr(n.Host)
			if err != nil {
				return netip.Addr{}, errors.New("no table says " + n.Host)
			}
			return a, nil
		},
		dial: netip.MustParseAddr("192.168.50.18"), exit: netip.MustParseAddr("192.168.50.17"),
		dns: []string{"1.1.1.1", "8.8.8.8"}, statusAddr: "127.0.0.1:18090",
	}
}

func TestAReadyNodeIsOnePortalOfTheSpec(t *testing.T) {
	spec, gaps, err := buildSpec(input(readyNode(1)))
	if err != nil || spec == nil || !gaps[1].ok() {
		t.Fatalf("spec %v, gaps %v, err %v", spec, gaps, err)
	}
	want := bridgespec.Node{
		Tag: "node-1", Addr: "203.0.113.1", Port: 443, UUID: bridgeIDOf(1), PublicKey: fixtureKey, ShortID: "a1b2c3d4",
		ServerName: "www.example.com", Flow: "xtls-rprx-vision", ReverseTag: "rvs-to-node-1",
	}
	if len(spec.Nodes) != 1 || spec.Nodes[0] != want {
		t.Errorf("portal = %+v\nwant     %+v", spec.Nodes, want)
	}
	if spec.Version != bridgespec.Version || spec.DialAddr != "192.168.50.18" || spec.Exit.Addr != "192.168.50.17" ||
		len(spec.Exit.Blocked) != 0 || spec.DNS.Servers[0] != "1.1.1.1" || spec.DNS.Servers[1] != "8.8.8.8" || spec.StatusAddr != "127.0.0.1:18090" {
		t.Errorf("spec = %+v", spec)
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("the bridge refuses it: %v", err)
	}
}

// What keeps a node out of the spec, one at a time. The first thing missing is
// the one named, in the order of the list above buildSpec's types.
func TestWhatKeepsANodeOutOfTheSpec(t *testing.T) {
	for name, c := range map[string]struct {
		mod    func(*model.Node)
		xray   string // the version known; "" for none
		noXray bool   // leave it at the pin
		addr   error
		want   gapKind
		says   string
	}{
		"still installing":     {mod: func(n *model.Node) { n.State = model.NodeInstalling }, noXray: true, want: gapNotRegistered, says: "install has not finished"},
		"no bridge client":     {mod: func(n *model.Node) { n.BridgeUUID = "" }, noXray: true, want: gapNoBridgeID, says: "no client id"},
		"no public key":        {mod: func(n *model.Node) { n.RealityPublicKey = "" }, noXray: true, want: gapNoReality, says: "public key"},
		"no short id":          {mod: func(n *model.Node) { n.RealityShortID = "" }, noXray: true, want: gapNoReality, says: "short id"},
		"no sni and no target": {mod: func(n *model.Node) { n.RealitySNI, n.RealityTarget = "", "" }, noXray: true, want: gapNoReality, says: "server name"},
		"nothing of REALITY": {mod: func(n *model.Node) {
			n.RealitySNI, n.RealityTarget, n.RealityPublicKey, n.RealityShortID = "", "", "", ""
		}, noXray: true, want: gapNoReality, says: "public key, short id, server name"},
		"xray not known":             {mod: func(*model.Node) {}, xray: "", want: gapXrayUnknown, says: "not said which Xray"},
		"xray is another":            {mod: func(*model.Node) {}, xray: "26.10.20", want: gapXrayMismatch, says: "26.10.20, and the bridge runs " + pin.Version},
		"xray is a newer one":        {mod: func(*model.Node) {}, xray: "26.11.1", want: gapXrayMismatch, says: "26.11.1"},
		"a name that won't resolve":  {mod: func(n *model.Node) { n.Host = "gone.example.com" }, noXray: true, want: gapNoAddress, says: "no table says gone.example.com"},
		"public key too short":       {mod: func(n *model.Node) { n.RealityPublicKey = "AQID" }, noXray: true, want: gapRefused, says: "32-byte"},
		"short id not hex":           {mod: func(n *model.Node) { n.RealityShortID = "zz" }, noXray: true, want: gapRefused, says: "short_id"},
		"sni that is an address":     {mod: func(n *model.Node) { n.RealitySNI = "203.0.113.9" }, noXray: true, want: gapRefused, says: "server_name"},
		"a v6 node behind a v4 dial": {mod: func(n *model.Node) { n.Host = "2001:db8::7" }, noXray: true, want: gapRefused, says: "address family"},
	} {
		t.Run(name, func(t *testing.T) {
			n := readyNode(1)
			c.mod(&n)
			in := input(n)
			if !c.noXray {
				in.xray = func(model.Node) string { return c.xray }
			}
			spec, gaps, err := buildSpec(in)
			if err != nil || spec != nil {
				t.Fatalf("spec %v, err %v: a node that is not ready made a spec", spec, err)
			}
			g := gaps[1]
			if g.kind != c.want || !strings.Contains(g.String(), c.says) {
				t.Errorf("gap = %d %q, want kind %d saying %q", g.kind, g.String(), c.want, c.says)
			}
		})
	}
}

// With no SNI of its own a node is dialled under the Reality target's name.
func TestTheServerNameFallsBackToTheTargetsHost(t *testing.T) {
	n := readyNode(1)
	n.RealitySNI, n.RealityTarget = "", "cover.example.org:8443"
	spec, _, err := buildSpec(input(n))
	if err != nil || spec == nil || spec.Nodes[0].ServerName != "cover.example.org" {
		t.Fatalf("spec %+v, err %v", spec, err)
	}
	n.RealitySNI = "other.example.net" // its own wins
	spec, _, _ = buildSpec(input(n))
	if spec.Nodes[0].ServerName != "other.example.net" {
		t.Errorf("server name = %q", spec.Nodes[0].ServerName)
	}
}

func TestTheNodesAreByIDWhateverOrderTheyComeIn(t *testing.T) {
	n3, n1, n2, n4 := readyNode(3), readyNode(1), readyNode(2), readyNode(4)
	n4.State = model.NodeInstalling
	n2.RealityPublicKey = ""
	spec, gaps, err := buildSpec(input(n3, n4, n1, n2))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, p := range spec.Nodes {
		tags = append(tags, p.Tag)
	}
	if strings.Join(tags, ",") != "node-1,node-3" {
		t.Errorf("tags = %v", tags)
	}
	if !gaps[1].ok() || !gaps[3].ok() || gaps[2].kind != gapNoReality || gaps[4].kind != gapNotRegistered || len(gaps) != 4 {
		t.Errorf("gaps = %+v", gaps)
	}
}

func TestTwoNodesAtOneAddressAndPortLeaveTheSecondOut(t *testing.T) {
	a, b := readyNode(1), readyNode(2)
	b.Host = a.Host
	spec, gaps, err := buildSpec(input(b, a))
	if err != nil || spec == nil || len(spec.Nodes) != 1 || spec.Nodes[0].Tag != "node-1" {
		t.Fatalf("spec %+v, err %v", spec, err)
	}
	if g := gaps[2]; g.kind != gapDuplicate || !strings.Contains(g.String(), "node 1") {
		t.Errorf("node 2's gap = %q", g)
	}
	// the same address on another port is another node
	b.EntryPort = 8443
	if spec, _, _ = buildSpec(input(a, b)); len(spec.Nodes) != 2 {
		t.Errorf("two ports at one address made %d portals", len(spec.Nodes))
	}
}

// A fault in the spec's own parts is no node's, and is not mistaken for one.
func TestAFaultInTheSpecAsAWholeIsNotBlamedOnANode(t *testing.T) {
	for name, c := range map[string]struct {
		mod  func(*specInput)
		want string
	}{
		"the exit is the dial address":  {func(in *specInput) { in.exit = in.dial }, "same as dial_addr"},
		"no resolver":                   {func(in *specInput) { in.dns = nil }, "dns.servers"},
		"a resolver the exit blocks":    {func(in *specInput) { in.dns = []string{"192.168.1.1"} }, "blocked range"},
		"a status page not on loopback": {func(in *specInput) { in.statusAddr = "0.0.0.0:18090" }, "loopback"},
	} {
		t.Run(name, func(t *testing.T) {
			in := input(readyNode(1), readyNode(2))
			c.mod(&in)
			spec, gaps, err := buildSpec(in)
			if spec != nil || err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("spec %v, err %v, want one saying %q", spec, err, c.want)
			}
			if !gaps[1].ok() || !gaps[2].ok() {
				t.Errorf("the nodes were blamed: %+v", gaps)
			}
		})
	}
	// but with no node to carry, there is no fault to find
	in := input()
	in.dns = nil
	if spec, _, err := buildSpec(in); spec != nil || err != nil {
		t.Errorf("an empty spec: %v, %v", spec, err)
	}
}

func TestAnIPv6BridgeTakesIPv6Nodes(t *testing.T) {
	in := input(readyNode(1))
	in.dial, in.exit = netip.MustParseAddr("fd00::18"), netip.MustParseAddr("fd00::17")
	in.dns = []string{"2606:4700:4700::1111"}
	in.nodes[0].Host = "2001:db8::7"
	spec, _, err := buildSpec(in)
	if err != nil || spec == nil || spec.Nodes[0].Addr != "2001:db8::7" {
		t.Fatalf("spec %+v, err %v", spec, err)
	}
}

func TestSpecHashesTellSpecsApart(t *testing.T) {
	a, _, _ := buildSpec(input(readyNode(1), readyNode(2)))
	again, _, _ := buildSpec(input(readyNode(2), readyNode(1)))
	if specHash(a) != specHash(again) {
		t.Error("the same spec made in another order has another hash")
	}
	for name, mod := range map[string]func(*specInput){
		"a node less":       func(in *specInput) { in.nodes = in.nodes[:1] },
		"another port":      func(in *specInput) { in.nodes[0].EntryPort = 8443 },
		"another key":       func(in *specInput) { in.nodes[0].RealityPublicKey = "AgIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA" },
		"another bridge id": func(in *specInput) { in.nodes[0].BridgeUUID = bridgeIDOf(9) },
		"another resolver":  func(in *specInput) { in.dns = []string{"9.9.9.9"} },
		"another exit":      func(in *specInput) { in.exit = netip.MustParseAddr("192.168.50.99") },
		"another status":    func(in *specInput) { in.statusAddr = "127.0.0.1:18091" },
	} {
		in := input(readyNode(1), readyNode(2))
		mod(&in)
		b, _, err := buildSpec(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if specHash(b) == specHash(a) {
			t.Errorf("%s: the hash didn't change", name)
		}
	}
}

func TestTags(t *testing.T) {
	for _, id := range []uint{1, 2, 10, 4294967295} {
		got, ok := idOfTag(tagOf(id))
		if !ok || got != id {
			t.Errorf("tag of %d came back as %d, %v", id, got, ok)
		}
	}
	for _, s := range []string{"", "node-", "node-0", "node-x", "node--1", "exit", "dns", "rvs-to-node-1", "node-99999999999"} {
		if id, ok := idOfTag(s); ok {
			t.Errorf("idOfTag(%q) = %d", s, id)
		}
	}
}

func TestXrayVersions(t *testing.T) {
	for in, want := range map[string]string{
		"Xray 26.10.21 (Xray, Penetrates Everything.) 1b1647c (go1.26.1 linux/amd64)": "26.10.21",
		"Xray 26.10.21": "26.10.21", "26.10.21": "26.10.21", "v26.10.21": "26.10.21", "  26.10.21\n": "26.10.21",
		"Xray v1.8.24 (Xray, Penetrates Everything.) deadbee": "1.8.24",
		"unknown": "", "": "", "Xray": "", "Xray (go1.26.1 linux/amd64)": "", "1b1647c": "", "26": "", "v": "", "26.10.21-rc1": "",
	} {
		if got := xrayVersionOf(in); got != want {
			t.Errorf("xrayVersionOf(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"1.3.0": "1.3.0", "v1.3.0": "1.3.0", " 1.3.0 ": "1.3.0", "dev": "dev", "version-9": "version-9", "v": "v", "": "",
		"1.3.0\x00\x01 beta": "1.3.0 beta", "1.3é.0": "1.3.0",
	} {
		if got := agentVersionOf(in); got != want {
			t.Errorf("agentVersionOf(%q) = %q, want %q", in, got, want)
		}
	}
	if got := agentVersionOf(strings.Repeat("9", 100)); len(got) != 64 {
		t.Errorf("a long version is %d characters", len(got))
	}
	for v, want := range map[string]xrayState{"": xrayUnknown, pin.Version: xrayOK, "26.10.20": xrayMismatch, "27.1.1": xrayMismatch} {
		if got := checkXray(v); got != want {
			t.Errorf("checkXray(%q) = %v, want %v", v, got, want)
		}
	}
}
