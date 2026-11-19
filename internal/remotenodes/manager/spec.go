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
//   - the outbound it leaves through, if it names one, can be used: its host
//     name resolves and the bridge accepts it. When it can't, a node whose
//     outbound has its fallback on leaves through the exit until it can.
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
	gapOutbound
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
	case gapOutbound:
		return "the outbound it leaves through can't be used: " + g.detail
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

// outTag is the name the bridge gives an outbound. The closing dot keeps one
// tag from being the start of another ("out-1." of "out-10."), which the
// bridge refuses, since Xray picks a group's members by the start of a tag.
func outTag(id uint) string { return "out-" + strconv.FormatUint(uint64(id), 10) + "." }

func idOfOutTag(tag string) (uint, bool) {
	s, ok := strings.CutPrefix(tag, "out-")
	if !ok {
		return 0, false
	}
	if s, ok = strings.CutSuffix(s, "."); !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(s, 10, 32)
	return uint(id), err == nil && id > 0
}

// specOutbound is an outbound as the bridge reaches it, at addr.
//
// An outbound given by name is reached at an address, so on a plain transport
// other than tcp, with no host of its own for it, the request would carry the
// address where the server expects its name: the name is put there instead.
func specOutbound(o model.Outbound, addr netip.Addr) bridgespec.Outbound {
	bo := o.Settings.BridgeOutbound()
	bo.Tag, bo.Addr, bo.Fallback = outTag(o.ID), addr.String(), o.Fallback
	if bo.Host == "" && bo.SecurityOrDefault() == bridgespec.SecNone && bo.NetworkOrDefault() != bridgespec.NetTCP && o.Settings.HostIsName() {
		bo.Host = o.Settings.Host
	}
	return bo
}

// outUse is what became of an outbound that something in the spec leaves
// through.
type outUse struct {
	name     string
	tag      string // its tag in the spec, "" when it isn't in it
	err      string // why it can't be used, when it can't
	fallback bool   // and so what leaves through it leaves through the exit
}

// why is what keeps what leaves through it out of the spec: the gap's words.
func (u outUse) why() string { return u.name + ": " + u.err }

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
	// local is the router's own entry, nil while it is off or not ready.
	local *bridgespec.Local

	// outbounds are the state's, and localVia the one the router's entry
	// leaves through, 0 for the exit. outAddr is the IP address to reach one
	// at, asked only for one that a node or the entry in the spec leaves
	// through. from is where the bridge connects to them from, the Foreign
	// link, and probe how it checks them.
	outbounds []model.Outbound
	localVia  uint
	outAddr   func(model.Outbound) (netip.Addr, error)
	from      netip.Addr
	probe     bridgespec.Probe
}

// specResult is a spec and what keeps each node out of it, what became of
// each outbound something in it leaves through, and, when the router's entry
// was left out for its outbound, why.
type specResult struct {
	spec     *bridgespec.Spec
	gaps     map[uint]gap
	outs     map[uint]outUse
	localGap string
}

// buildSpec makes the bridge's spec from the nodes that are ready and the
// router's own entry, and says what keeps each of the other nodes out. It
// returns no spec when no node is ready and there is no entry. An error means the spec as a whole is wrong (the exit is the dial
// address, say), which no node can be blamed for; every node is then left out
// of it, and the gaps hold what each one lacks on its own.
func buildSpec(in specInput) (*bridgespec.Spec, map[uint]gap, error) {
	res, err := makeSpec(in)
	return res.spec, res.gaps, err
}

// makeSpec is buildSpec with what it found of the outbounds.
//
// Only an outbound that a node or the entry in the spec leaves through goes
// into it, so one that nothing uses can be added, changed and removed without
// a restart of the bridge.
func makeSpec(in specInput) (specResult, error) {
	gaps := map[uint]gap{}
	res := specResult{gaps: gaps, outs: map[uint]outUse{}}
	base := bridgespec.Spec{
		Version:    bridgespec.Version,
		DialAddr:   in.dial.String(),
		Exit:       bridgespec.Exit{Addr: in.exit.String()},
		DNS:        bridgespec.DNS{Servers: slices.Clone(in.dns)},
		StatusAddr: in.statusAddr,
		Local:      in.local,
	}

	nodes := slices.Clone(in.nodes)
	slices.SortFunc(nodes, func(a, b model.Node) int { return int(a.ID) - int(b.ID) })
	var portals []bridgespec.Node
	via := map[string]uint{} // the outbound each portal leaves through, by tag
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
			via[p.Tag] = n.OutboundID
		}
	}
	if len(portals) == 0 && in.local == nil {
		return res, nil
	}

	// A fault in the parts that are not a node's would fail every portal alike,
	// so it is looked for with one that is known to be good.
	if err := globalFault(base, in.dial); err != nil {
		return res, err
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

	outs := in.wantedOutbounds(base, kept, via, res.outs)
	var carried []bridgespec.Node
	for _, p := range kept {
		id := via[p.Tag]
		if id == 0 {
			carried = append(carried, p)
			continue
		}
		switch use := res.outs[id]; {
		case use.tag != "":
			p.Via = use.tag
		case !use.fallback:
			nid, _ := idOfTag(p.Tag)
			gaps[nid] = gap{gapOutbound, use.why()}
			continue
		}
		carried = append(carried, p)
	}
	local := in.local
	if local != nil && in.localVia != 0 {
		switch use := res.outs[in.localVia]; {
		case use.tag != "":
			l := *local
			l.Via = use.tag
			local = &l
		case !use.fallback:
			res.localGap, local = use.why(), nil
		}
	}
	if len(carried) == 0 && local == nil {
		return res, nil
	}
	spec := base
	spec.Nodes, spec.Local = carried, local
	if len(outs) > 0 {
		spec.Outbounds, spec.OutboundAddr, spec.Probe = outs, in.from.String(), in.probe
	}
	if err := spec.Validate(); err != nil {
		return res, err
	}
	res.spec = &spec
	return res, nil
}

// wantedOutbounds are the outbounds the kept portals and the router's entry
// leave through, as the bridge reaches them, by id. What became of each,
// whether it went in or not, goes into uses.
func (in specInput) wantedOutbounds(base bridgespec.Spec, kept []bridgespec.Node, via map[string]uint, uses map[uint]outUse) []bridgespec.Outbound {
	wanted := map[uint]bool{}
	for _, p := range kept {
		if id := via[p.Tag]; id != 0 {
			wanted[id] = true
		}
	}
	if in.local != nil && in.localVia != 0 {
		wanted[in.localVia] = true
	}
	var outs []bridgespec.Outbound
	for _, o := range in.outbounds {
		if !wanted[o.ID] {
			continue
		}
		use := outUse{name: o.Name, fallback: o.Fallback}
		addr, err := in.outAddr(o)
		if err != nil {
			use.err = err.Error()
		} else {
			bo := specOutbound(o, addr)
			if err := outboundFault(base, in.dial, in.from, in.probe, bo); err != nil {
				use.err = "the bridge's checks refuse it: " + err.Error()
			} else {
				use.tag = bo.Tag
				outs = append(outs, bo)
			}
		}
		uses[o.ID] = use
		delete(wanted, o.ID)
	}
	// a route to an outbound the state doesn't have, which its checks keep from happening
	for id := range wanted {
		uses[id] = outUse{name: fmt.Sprintf("outbound %d", id), err: "it is not there"}
	}
	return outs
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

// outboundFault is what the bridge finds wrong with an outbound, on its own in
// a spec that is otherwise known to be good.
func outboundFault(base bridgespec.Spec, dial, from netip.Addr, probe bridgespec.Probe, o bridgespec.Outbound) error {
	base.Outbounds, base.OutboundAddr, base.Probe = []bridgespec.Outbound{o}, from.String(), probe
	return globalFault(base, dial)
}

// specHash tells one spec from another: two with the same hash start the same
// bridge, so the second is not worth a restart. The clients of the router's
// own entry are left out of it: the bridge takes a change of those while it
// runs (see syncLocalClients).
func specHash(s *bridgespec.Spec) string {
	if s.Local != nil {
		cp, l := *s, *s.Local
		l.Clients = nil
		cp.Local = &l
		s = &cp
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("manager: a bridge spec can't be encoded: %v", err)) // it holds only strings and numbers
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
