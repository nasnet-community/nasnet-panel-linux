// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// A node is in the bridge's spec when it is ready, and it is ready when
// everything the bridge needs to dial its portal is known. What is missing,
// if anything, is its gap:
//
//   - the install has finished: it is registered (not still installing)
//   - the install made the bridge's client id, which is how the bridge logs in
//   - the REALITY values of the portal are there: the public key, the short
//     id, and a server name (the SNI, else the Reality target's host)
//   - its Xray is known and is the pin: the bridge and the portal have to run
//     one build, and a node that hasn't said what it runs is not trusted to
//     run the right one (the skew guard)
//   - its address is known: a host name has been resolved, from the nodes'
//     source address, to an IP address of the family the bridge dials from
//   - the bridge accepts it: bridgespec's own checks
//   - no other node has the same address and port
//
// Whether the entry is on is not the node's: with it off there is no spec at
// all. And whether its agent answers isn't either, since the tunnel doesn't go
// through the agent.
type gapKind int

const (
	gapNone gapKind = iota
	gapNotRegistered
	gapNoBridgeID
	gapNoReality
	gapXrayUnknown
	gapXrayMismatch
	gapNoAddress
	gapRefused
	gapDuplicate
)

type gap struct {
	kind   gapKind
	detail string
}

func (g gap) ok() bool { return g.kind == gapNone }

func (g gap) String() string {
	switch g.kind {
	case gapNone:
		return "ready"
	case gapNotRegistered:
		return "its install has not finished"
	case gapNoBridgeID:
		return "it has no client id for the bridge"
	case gapNoReality:
		return "its REALITY values are missing: " + g.detail
	case gapXrayUnknown:
		return "it has not said which Xray it runs"
	case gapXrayMismatch:
		return "its Xray is " + g.detail + ", and the bridge runs " + xrayPin()
	case gapNoAddress:
		return "its address is not known: " + g.detail
	case gapRefused:
		return "the bridge's checks refuse it: " + g.detail
	case gapDuplicate:
		return "it has the same address and port as " + g.detail
	}
	return "unknown"
}

// tagOf is the name the bridge gives a node's tunnel, and idOfTag its inverse.
func tagOf(id uint) string { return "node-" + strconv.FormatUint(uint64(id), 10) }

func idOfTag(tag string) (uint, bool) {
	s, ok := strings.CutPrefix(tag, "node-")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(s, 10, 32)
	return uint(id), err == nil && id > 0
}

// specInput is what a spec is built from.
type specInput struct {
	nodes []model.Node
	// xray is the version known for a node, as xrayVersionOf makes it, or ""
	// when there is none.
	xray func(model.Node) string
	// addr is the IP address to dial a node at. It is asked only for a node
	// that is ready in every other way, so a lookup is never made for nothing.
	addr func(model.Node) (netip.Addr, error)

	dial, exit netip.Addr
	dns        []string
	statusAddr string
}

// buildSpec makes the bridge's spec from the nodes that are ready, and says
// what keeps each of the others out. It returns no spec when no node is
// ready. An error means the spec as a whole is wrong (the exit is the dial
// address, say), which no node can be blamed for; every node is then left out
// of it, and the gaps hold what each one lacks on its own.
func buildSpec(in specInput) (*bridgespec.Spec, map[uint]gap, error) {
	gaps := map[uint]gap{}
	base := bridgespec.Spec{
		Version:    bridgespec.Version,
		DialAddr:   in.dial.String(),
		Exit:       bridgespec.Exit{Addr: in.exit.String()},
		DNS:        bridgespec.DNS{Servers: slices.Clone(in.dns)},
		StatusAddr: in.statusAddr,
	}

	nodes := slices.Clone(in.nodes)
	slices.SortFunc(nodes, func(a, b model.Node) int { return int(a.ID) - int(b.ID) })
	var portals []bridgespec.Node
	taken := map[netip.AddrPort]string{}
	for _, n := range nodes {
		p, g := portalOf(n, in)
		if g.ok() {
			ap := netip.AddrPortFrom(netip.MustParseAddr(p.Addr), p.Port)
			if other, dup := taken[ap]; dup {
				g = gap{gapDuplicate, other}
			} else {
				taken[ap] = n.Name
			}
		}
		gaps[n.ID] = g
		if g.ok() {
			portals = append(portals, p)
		}
	}
	if len(portals) == 0 {
		return nil, gaps, nil
	}

	// A fault in the parts that are not a node's would fail every portal alike,
	// so it is looked for with one that is known to be good.
	if err := globalFault(base, in.dial); err != nil {
		return nil, gaps, err
	}
	var kept []bridgespec.Node
	for _, p := range portals {
		one := base
		one.Nodes = []bridgespec.Node{p}
		if err := one.Validate(); err != nil {
			id, _ := idOfTag(p.Tag)
			gaps[id] = gap{gapRefused, err.Error()}
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return nil, gaps, nil
	}
	spec := base
	spec.Nodes = kept
	if err := spec.Validate(); err != nil {
		return nil, gaps, err
	}
	return &spec, gaps, nil
}

// portalOf is a node as the bridge dials it, or what it lacks.
func portalOf(n model.Node, in specInput) (bridgespec.Node, gap) {
	switch {
	case n.State != model.NodeRegistered:
		return bridgespec.Node{}, gap{kind: gapNotRegistered}
	case n.BridgeUUID == "":
		return bridgespec.Node{}, gap{kind: gapNoBridgeID}
	}
	name := serverName(n)
	var missing []string
	if n.RealityPublicKey == "" {
		missing = append(missing, "public key")
	}
	if n.RealityShortID == "" {
		missing = append(missing, "short id")
	}
	if name == "" {
		missing = append(missing, "server name")
	}
	if len(missing) > 0 {
		return bridgespec.Node{}, gap{gapNoReality, strings.Join(missing, ", ")}
	}
	switch v := in.xray(n); checkXray(v) {
	case xrayUnknown:
		return bridgespec.Node{}, gap{kind: gapXrayUnknown}
	case xrayMismatch:
		return bridgespec.Node{}, gap{gapXrayMismatch, v}
	}
	addr, err := in.addr(n)
	if err != nil {
		return bridgespec.Node{}, gap{gapNoAddress, err.Error()}
	}
	return bridgespec.Node{
		Tag:        tagOf(n.ID),
		Addr:       addr.String(),
		Port:       uint16(n.EntryPort),
		UUID:       n.BridgeUUID,
		PublicKey:  n.RealityPublicKey,
		ShortID:    n.RealityShortID,
		ServerName: name,
		Flow:       bridgespec.VisionFlow,
		ReverseTag: nodecfg.BridgeReverseTag(int(n.ID)),
	}, gap{}
}

// serverName is the name the bridge presents to the portal: the SNI, or, when
// the node has none of its own, the host of the Reality target.
func serverName(n model.Node) string {
	if n.RealitySNI != "" {
		return n.RealitySNI
	}
	host, _, err := net.SplitHostPort(n.RealityTarget)
	if err != nil {
		return ""
	}
	return host
}

// globalFault is what is wrong with the spec apart from its nodes: the
// addresses, the resolvers, the status page.
func globalFault(base bridgespec.Spec, dial netip.Addr) error {
	probe := bridgespec.Node{
		Tag: "probe", Addr: "192.0.2.1", Port: 443, UUID: "00000000-0000-4000-8000-000000000000",
		PublicKey: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), ShortID: "00", ServerName: "probe.example",
		ReverseTag: "probe-reverse",
	}
	if dial.Is6() {
		probe.Addr = "2001:db8::1"
	}
	base.Nodes = []bridgespec.Node{probe}
	return base.Validate()
}

// specHash tells one spec from another: two with the same hash start the same
// bridge, so the second is not worth a restart.
func specHash(s *bridgespec.Spec) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("manager: a bridge spec can't be encoded: %v", err)) // it holds only strings and numbers
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
