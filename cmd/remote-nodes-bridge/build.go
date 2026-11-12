// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/dns"
	xlog "github.com/xtls/xray-core/app/log"
	"github.com/xtls/xray-core/app/policy"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/geodata"
	clog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/proxy/vless"
	vlessin "github.com/xtls/xray-core/proxy/vless/inbound"
	vlessout "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/tls"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// buildConfig turns a spec into the Xray config the bridge runs. It builds the
// protobuf by hand, as xray-core's JSON loader would for the same settings: the
// loader drags in nearly the whole core. parity_test.go holds the two together.
//
// It is the Linux panel's bridge end, a VLESS + REALITY outbound per portal
// with a reverse tag and one freedom exit that blocks the LAN, plus what a
// router needs: each end bound to its own source address, name lookups kept
// behind the exit, and per-node counters.
//
// With a local entry in the spec it also carries that: a node's entry inbound,
// here on the router, whose clients go straight to the same exit. localTarget
// is where its REALITY takes a handshake that is not a client's, the bridge's
// own forwarder (target.go); it is not looked at without an entry.
func buildConfig(s *bridgespec.Spec, localTarget string) (*core.Config, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	dial := netip.MustParseAddr(s.DialAddr).Unmap()
	exit := netip.MustParseAddr(s.Exit.Addr).Unmap()

	blocked := make([]netip.Prefix, 0, len(s.Exit.BlockedOrDefault()))
	for _, raw := range s.Exit.BlockedOrDefault() {
		blocked = append(blocked, netip.MustParsePrefix(raw))
	}

	// Xray sends what no rule claims to the first outbound, so the exit goes
	// first: a stray flow lands behind the LAN block, never in a tunnel.
	outbounds := []*core.OutboundHandlerConfig{exitOutbound(exit, blocked)}
	// Name lookups carry the dns tag. Say outright that they leave through the
	// exit, from its address, rather than lean on it being the default.
	rules := []*router.RoutingRule{toExit(bridgespec.DNSTag)}
	var inbounds []*core.InboundHandlerConfig
	if s.Local != nil {
		entry, err := localInbound(s.Local, localTarget)
		if err != nil {
			return nil, fmt.Errorf("local: %w", err)
		}
		inbounds = append(inbounds, entry)
		rules = append(rules, toExit(bridgespec.LocalTag))
	}
	for _, n := range s.Nodes {
		link, err := tunnelOutbound(n, dial)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n.Tag, err)
		}
		outbounds = append(outbounds, link)
		rules = append(rules, toExit(n.ReverseTag))
	}

	dnsApp, err := dnsConfig(s.DNS.Servers, exit)
	if err != nil {
		return nil, err
	}
	return &core.Config{
		Inbound:  inbounds,
		Outbound: outbounds,
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(logConfig(s.LogLevelOrDefault())),
			serial.ToTypedMessage(&dispatcher.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(&stats.Config{}),
			serial.ToTypedMessage(&router.Config{Rule: rules}),
			serial.ToTypedMessage(dnsApp),
			serial.ToTypedMessage(countersPolicy()),
		},
	}, nil
}

// logConfig keeps the access log off: it would put every destination a
// subscription visits into the output the hub keeps.
func logConfig(level string) *xlog.Config {
	c := &xlog.Config{ErrorLogType: xlog.LogType_Console, AccessLogType: xlog.LogType_None}
	switch level {
	case "debug":
		c.ErrorLogLevel = clog.Severity_Debug
	case "info":
		c.ErrorLogLevel = clog.Severity_Info
	case "error":
		c.ErrorLogLevel = clog.Severity_Error
	case "none":
		c.ErrorLogType = xlog.LogType_None
	default:
		c.ErrorLogLevel = clog.Severity_Warning
	}
	return c
}

func toExit(inbound string) *router.RoutingRule {
	return &router.RoutingRule{
		TargetTag:  &router.RoutingRule_Tag{Tag: bridgespec.ExitTag},
		InboundTag: []string{inbound},
	}
}

// countersPolicy counts bytes per user. Each tunnel's outbound uses the node's
// tag as its user's email, so a node's traffic is one pair of counters.
func countersPolicy() *policy.Config {
	return &policy.Config{Level: map[uint32]*policy.Policy{
		0: {
			Timeout: &policy.Policy_Timeout{},
			Stats:   &policy.Policy_Stats{UserUplink: true, UserDownlink: true},
		},
	}}
}

// exitOutbound is where the tunnels' traffic leaves: freedom, bound to the exit
// address, refusing the blocked ranges. Xray blocks all a reverse tunnel
// carries unless a final rule allows it, hence the allow at the end.
func exitOutbound(addr netip.Addr, blocked []netip.Prefix) *core.OutboundHandlerConfig {
	block := &freedom.FinalRuleConfig{Action: freedom.RuleAction_Block}
	for _, p := range blocked {
		block.Ip = append(block.Ip, &geodata.IPRule{Value: &geodata.IPRule_Custom{
			Custom: &geodata.CIDRRule{Cidr: &geodata.CIDR{Ip: p.Addr().AsSlice(), Prefix: uint32(p.Bits())}},
		}})
	}

	// Force, not just prefer: when a lookup fails Xray must not fall back to
	// the system resolver, whose queries would leave from the wrong address.
	strategy := internet.DomainStrategy_FORCE_IP4
	if addr.Is6() {
		strategy = internet.DomainStrategy_FORCE_IP6
	}
	return &core.OutboundHandlerConfig{
		Tag: bridgespec.ExitTag,
		SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{
			Via: ipOrDomain(addr),
			StreamSettings: &internet.StreamConfig{
				SocketSettings: &internet.SocketConfig{DomainStrategy: strategy},
			},
		}),
		ProxySettings: serial.ToTypedMessage(&freedom.Config{
			FinalRules: []*freedom.FinalRuleConfig{block, {Action: freedom.RuleAction_Allow}},
		}),
	}
}

// tunnelOutbound dials one portal over VLESS + REALITY from the dial address,
// registering the reverse tag there. Unlike the panel's, it has an email, the
// node's tag, for the byte counters: the panel counts at the portal, but here
// the bridge is the only one counting.
func tunnelOutbound(n bridgespec.Node, dial netip.Addr) (*core.OutboundHandlerConfig, error) {
	id, err := uuid.ParseString(n.UUID)
	if err != nil {
		return nil, fmt.Errorf("uuid: %w", err)
	}
	pub, err := base64.RawURLEncoding.DecodeString(n.PublicKey)
	if err != nil || len(pub) != 32 {
		return nil, fmt.Errorf("public key: not 32 bytes of base64url")
	}
	shortID := make([]byte, 8)
	if _, err := hex.Decode(shortID, []byte(n.ShortID)); err != nil {
		return nil, fmt.Errorf("short id: %w", err)
	}
	// REALITY refuses these, and Xray only finds out on the first dial.
	fingerprint := strings.ToLower(n.FingerprintOrDefault())
	if fingerprint == "unsafe" || fingerprint == "hellogolang" || tls.GetFingerprint(fingerprint) == nil {
		return nil, fmt.Errorf("fingerprint %q is not one REALITY can use", fingerprint)
	}

	security := serial.ToTypedMessage(&reality.Config{
		Fingerprint: fingerprint,
		ServerName:  n.ServerName,
		PublicKey:   pub,
		ShortId:     shortID,
		SpiderX:     "/",
		SpiderY:     make([]int64, 10),
	})
	portal := netip.MustParseAddr(n.Addr).Unmap()
	account := &vless.Account{
		Id:         id.String(),
		Flow:       n.Flow,
		Encryption: "none",
		Reverse:    &vless.Reverse{Tag: n.ReverseTag},
	}
	return &core.OutboundHandlerConfig{
		Tag: n.Tag,
		SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{
			Via: ipOrDomain(dial),
			StreamSettings: &internet.StreamConfig{
				ProtocolName:     "tcp",
				SecurityType:     security.Type,
				SecuritySettings: []*serial.TypedMessage{security},
			},
		}),
		ProxySettings: serial.ToTypedMessage(&vlessout.Config{
			Vnext: &protocol.ServerEndpoint{
				Address: ipOrDomain(portal),
				Port:    uint32(n.Port),
				User:    &protocol.User{Email: n.Tag, Account: serial.ToTypedMessage(account)},
			},
		}),
	}, nil
}

// localInbound is the entry on the router: VLESS over TCP with REALITY, the
// inbound nodecfg gives a node, listening on one address. Sniffing is on as it
// is there, so a flow that names its site is sent on by that name and the
// exit's resolver, not the client's, says where it is.
func localInbound(l *bridgespec.Local, target string) (*core.InboundHandlerConfig, error) {
	if _, _, err := net.SplitHostPort(target); err != nil {
		return nil, fmt.Errorf("target %q is not an address and port", target)
	}
	private, err := base64.RawURLEncoding.DecodeString(l.PrivateKey)
	if err != nil || len(private) != 32 {
		return nil, fmt.Errorf("private key: not 32 bytes of base64url")
	}
	shortIDs := make([][]byte, len(l.ShortIDs))
	for i, raw := range l.ShortIDs {
		// REALITY compares all 8 bytes, a shorter id padded with zeros.
		shortIDs[i] = make([]byte, 8)
		if _, err := hex.Decode(shortIDs[i], []byte(raw)); err != nil {
			return nil, fmt.Errorf("short id: %w", err)
		}
	}
	users := make([]*protocol.User, len(l.Clients))
	for i, c := range l.Clients {
		if users[i], err = localUser(c, l.Flow); err != nil {
			return nil, err
		}
	}

	security := serial.ToTypedMessage(&reality.Config{
		Dest:                  target,
		Type:                  "tcp",
		ServerNames:           l.ServerNames,
		PrivateKey:            private,
		ShortIds:              shortIDs,
		LimitFallbackUpload:   &reality.LimitFallback{},
		LimitFallbackDownload: &reality.LimitFallback{},
	})
	listen := netip.MustParseAddr(l.ListenAddr).Unmap()
	return &core.InboundHandlerConfig{
		Tag: bridgespec.LocalTag,
		ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{
			PortList: &xnet.PortList{Range: []*xnet.PortRange{xnet.SinglePortRange(xnet.Port(l.Port))}},
			Listen:   ipOrDomain(listen),
			StreamSettings: &internet.StreamConfig{
				ProtocolName:     "tcp",
				SecurityType:     security.Type,
				SecuritySettings: []*serial.TypedMessage{security},
			},
			SniffingSettings: &proxyman.SniffingConfig{
				Enabled:             true,
				DestinationOverride: []string{"http", "tls"},
			},
		}),
		ProxySettings: serial.ToTypedMessage(&vlessin.Config{Users: users, Decryption: "none"}),
	}, nil
}

// localUser is a subscription as the entry's inbound knows it. Its email names
// its byte counters, as a tunnel's tag names the tunnel's.
func localUser(c bridgespec.Client, flow string) (*protocol.User, error) {
	id, err := uuid.ParseString(c.UUID)
	if err != nil {
		return nil, fmt.Errorf("%s: uuid: %w", c.Email, err)
	}
	return &protocol.User{
		Email:   c.Email,
		Account: serial.ToTypedMessage(&vless.Account{Id: id.String(), Flow: flow}),
	}, nil
}

// dnsConfig points Xray's resolver at the spec's servers over UDP, tagged so
// the router can send the queries out through the exit. With no server Xray
// would resolve through the system instead.
func dnsConfig(servers []string, exit netip.Addr) (*dns.Config, error) {
	strategy := dns.QueryStrategy_USE_IP4
	if exit.Is6() {
		strategy = dns.QueryStrategy_USE_IP6
	}
	c := &dns.Config{Tag: bridgespec.DNSTag, QueryStrategy: strategy}
	for _, raw := range servers {
		ap, err := bridgespec.ParseServer(raw)
		if err != nil {
			return nil, fmt.Errorf("dns server %q: %w", raw, err)
		}
		c.NameServer = append(c.NameServer, &dns.NameServer{
			Address: &xnet.Endpoint{
				Network: xnet.Network_UDP,
				Address: ipOrDomain(ap.Addr()),
				Port:    uint32(ap.Port()),
			},
			// The loader gives servers with the same settings one id.
			PolicyID: 1,
		})
	}
	return c, nil
}

func ipOrDomain(a netip.Addr) *xnet.IPOrDomain {
	return xnet.NewIPOrDomain(xnet.IPAddress(a.AsSlice()))
}
