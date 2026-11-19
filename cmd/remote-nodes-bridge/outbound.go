// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/geodata"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/http"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/socks"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	vlessout "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/proxy/vmess"
	vmessout "github.com/xtls/xray-core/proxy/vmess/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/httpupgrade"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/splithttp"
	"github.com/xtls/xray-core/transport/internet/tls"
	"github.com/xtls/xray-core/transport/internet/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// The outbounds are built by hand like the rest of the config, each the way
// the loader builds the flat client form of its protocol ({"address", "port",
// "id", ...}). outbound_parity_test.go holds them together.

// outboundHandler is one of the spec's outbounds, sending from from. A probe
// with no source address passes the zero Addr and the system picks one.
func outboundHandler(o bridgespec.Outbound, from netip.Addr) (*core.OutboundHandlerConfig, error) {
	server, err := netip.ParseAddr(o.Addr)
	if err != nil {
		return nil, fmt.Errorf("addr: %q is not an IP address", o.Addr)
	}
	server = server.Unmap()
	// The loader refuses these, since nothing would hide the id or password
	// on the way. Its exceptions are the addresses that don't leave the site.
	if o.SecurityOrDefault() == bridgespec.SecNone && (o.Protocol == bridgespec.ProtoVLESS || o.Protocol == bridgespec.ProtoTrojan) &&
		!server.IsPrivate() && !server.IsLoopback() && !server.IsLinkLocalUnicast() {
		return nil, fmt.Errorf("security: Xray runs %s without tls or reality only to a private address, not %s", o.Protocol, server)
	}
	stream, err := outboundStream(o)
	if err != nil {
		return nil, err
	}
	settings, err := outboundSettings(o, server)
	if err != nil {
		return nil, err
	}
	sender := &proxyman.SenderConfig{StreamSettings: stream}
	if from.IsValid() {
		sender.Via = ipOrDomain(from)
	}
	return &core.OutboundHandlerConfig{
		Tag:            o.Tag,
		SenderSettings: serial.ToTypedMessage(sender),
		ProxySettings:  serial.ToTypedMessage(settings),
	}, nil
}

var (
	shadowsocksCiphers = map[string]shadowsocks.CipherType{
		"aes-128-gcm":             shadowsocks.CipherType_AES_128_GCM,
		"aes-256-gcm":             shadowsocks.CipherType_AES_256_GCM,
		"chacha20-poly1305":       shadowsocks.CipherType_CHACHA20_POLY1305,
		"chacha20-ietf-poly1305":  shadowsocks.CipherType_CHACHA20_POLY1305,
		"xchacha20-poly1305":      shadowsocks.CipherType_XCHACHA20_POLY1305,
		"xchacha20-ietf-poly1305": shadowsocks.CipherType_XCHACHA20_POLY1305,
	}
	vmessCiphers = map[string]protocol.SecurityType{
		"":                  protocol.SecurityType_AUTO,
		"auto":              protocol.SecurityType_AUTO,
		"aes-128-gcm":       protocol.SecurityType_AES128_GCM,
		"chacha20-poly1305": protocol.SecurityType_CHACHA20_POLY1305,
	}
)

// outboundSettings is the protocol's half of an outbound: the server and the
// login. Users carry no email: the outbound's own counters name it by its tag.
func outboundSettings(o bridgespec.Outbound, server netip.Addr) (proto.Message, error) {
	endpoint := func(account proto.Message) *protocol.ServerEndpoint {
		e := &protocol.ServerEndpoint{Address: ipOrDomain(server), Port: uint32(o.Port)}
		if account != nil {
			e.User = &protocol.User{Account: serial.ToTypedMessage(account)}
		}
		return e
	}
	switch o.Protocol {
	case bridgespec.ProtoVLESS:
		id, err := uuid.ParseString(o.ID)
		if err != nil {
			return nil, fmt.Errorf("id: not a UUID")
		}
		return &vlessout.Config{Vnext: endpoint(&vless.Account{Id: id.String(), Flow: o.Flow, Encryption: "none"})}, nil
	case bridgespec.ProtoVMess:
		id, err := uuid.ParseString(o.ID)
		if err != nil {
			return nil, fmt.Errorf("id: not a UUID")
		}
		cipher, ok := vmessCiphers[o.Method]
		if !ok {
			return nil, fmt.Errorf("method: %q is not a VMess cipher", o.Method)
		}
		return &vmessout.Config{Receiver: endpoint(&vmess.Account{
			Id:               id.String(),
			SecuritySettings: &protocol.SecurityConfig{Type: cipher},
		})}, nil
	case bridgespec.ProtoTrojan:
		return &trojan.ClientConfig{Server: endpoint(&trojan.Account{Password: o.Password})}, nil
	case bridgespec.ProtoShadowsocks:
		if bridgespec.Shadowsocks2022(o.Method) {
			return &shadowsocks_2022.ClientConfig{Address: ipOrDomain(server), Port: uint32(o.Port), Method: o.Method, Key: o.Password}, nil
		}
		cipher, ok := shadowsocksCiphers[o.Method]
		if !ok {
			return nil, fmt.Errorf("method: %q is not a Shadowsocks cipher the bridge carries", o.Method)
		}
		return &shadowsocks.ClientConfig{Server: endpoint(&shadowsocks.Account{Password: o.Password, CipherType: cipher})}, nil
	case bridgespec.ProtoSOCKS:
		var account proto.Message
		if o.Username != "" {
			account = &socks.Account{Username: o.Username, Password: o.Password}
		}
		return &socks.ClientConfig{Server: endpoint(account)}, nil
	case bridgespec.ProtoHTTP:
		var account proto.Message
		if o.Username != "" {
			account = &http.Account{Username: o.Username, Password: o.Password}
		}
		return &http.ClientConfig{Server: endpoint(account)}, nil
	}
	return nil, fmt.Errorf("protocol: %q is not one the bridge carries", o.Protocol)
}

var transportNames = map[string]string{
	bridgespec.NetTCP:         "tcp",
	bridgespec.NetWS:          "websocket",
	bridgespec.NetHTTPUpgrade: "httpupgrade",
	bridgespec.NetXHTTP:       "splithttp",
}

// outboundStream is the transport and what secures it. Plain tcp has none, as
// an outbound with no streamSettings has none from the loader.
func outboundStream(o bridgespec.Outbound) (*internet.StreamConfig, error) {
	network, security := o.NetworkOrDefault(), o.SecurityOrDefault()
	name, ok := transportNames[network]
	if !ok {
		return nil, fmt.Errorf("network: %q is not one the bridge carries", o.Network)
	}
	if network == bridgespec.NetTCP && security == bridgespec.SecNone {
		return nil, nil
	}
	s := &internet.StreamConfig{ProtocolName: name}
	if t := transportSettings(o, network); t != nil {
		s.TransportSettings = []*internet.TransportConfig{{ProtocolName: name, Settings: serial.ToTypedMessage(t)}}
	}

	var sec proto.Message
	var err error
	switch security {
	case bridgespec.SecNone:
	case bridgespec.SecTLS:
		sec, err = tlsSettings(o)
	case bridgespec.SecReality:
		sec, err = realitySettings(o)
	default:
		err = fmt.Errorf("security: %q is not one the bridge carries", o.Security)
	}
	if err != nil {
		return nil, err
	}
	if sec != nil {
		tm := serial.ToTypedMessage(sec)
		s.SecurityType = tm.Type
		s.SecuritySettings = []*serial.TypedMessage{tm}
	}
	return s, nil
}

// transportSettings are the settings of a transport other than tcp. The page
// always fills them in, so they are always written, even when empty.
func transportSettings(o bridgespec.Outbound, network string) proto.Message {
	switch network {
	case bridgespec.NetWS:
		path, ed := earlyData(o.Path)
		return &websocket.Config{Path: path, Host: o.Host, Ed: ed}
	case bridgespec.NetHTTPUpgrade:
		path, ed := earlyData(o.Path)
		return &httpupgrade.Config{Path: path, Host: o.Host, Ed: ed}
	case bridgespec.NetXHTTP:
		return xhttpSettings(o)
	}
	return nil
}

// earlyData takes ?ed=N off a path, as the loader does: it is the size of the
// early data, not part of the path.
func earlyData(path string) (string, uint32) {
	u, err := url.Parse(path)
	if err != nil {
		return path, 0
	}
	q := u.Query()
	if q.Get("ed") == "" {
		return path, 0
	}
	ed, _ := strconv.Atoi(q.Get("ed"))
	q.Del("ed")
	u.RawQuery = q.Encode()
	return u.String(), uint32(ed)
}

// xhttpSettings is xhttp with the loader's defaults, which only the loader
// fills in: the core reads an empty field as zero, not as the default.
func xhttpSettings(o bridgespec.Outbound) *splithttp.Config {
	mode := o.Mode
	if mode == "" {
		mode = "auto"
	}
	none := func() *splithttp.RangeConfig { return &splithttp.RangeConfig{} }
	return &splithttp.Config{
		Host:                 o.Host,
		Path:                 o.Path,
		Mode:                 mode,
		XPaddingBytes:        none(),
		XPaddingKey:          "x_padding",
		XPaddingHeader:       "X-Padding",
		XPaddingPlacement:    "queryInHeader",
		XPaddingMethod:       "repeat-x",
		UplinkHTTPMethod:     "POST",
		SessionIDPlacement:   "path",
		SeqPlacement:         "path",
		UplinkDataPlacement:  splithttp.PlacementAuto,
		UplinkDataKey:        "X-Data",
		UplinkChunkSize:      none(),
		ScMaxEachPostBytes:   none(),
		ScMinPostsIntervalMs: none(),
		ScStreamUpServerSecs: none(),
		SessionIDLength:      none(),
		Xmux: &splithttp.XmuxConfig{
			MaxConcurrency:   none(),
			MaxConnections:   &splithttp.RangeConfig{From: 3, To: 3},
			CMaxReuseTimes:   none(),
			HMaxRequestTimes: &splithttp.RangeConfig{From: 600, To: 900},
			HMaxReusableSecs: &splithttp.RangeConfig{From: 1800, To: 3000},
		},
	}
}

// tlsSettings is the client half of TLS. Nothing here can turn the check of
// the server's certificate off.
func tlsSettings(o bridgespec.Outbound) (*tls.Config, error) {
	fingerprint := strings.ToLower(o.FingerprintOrDefault())
	if fingerprint != "unsafe" && tls.GetFingerprint(fingerprint) == nil {
		return nil, fmt.Errorf("fingerprint: %q is not one Xray knows", fingerprint)
	}
	c := &tls.Config{ServerName: o.ServerName, Fingerprint: fingerprint}
	switch {
	case len(o.ALPN) > 0:
		c.NextProtocol = slices.Clone(o.ALPN)
	case o.Protocol == bridgespec.ProtoHTTP:
		// Offered h2, Xray sends its CONNECT over HTTP/2, which a plain
		// HTTP/1.1 proxy doesn't understand. This one ALPN alone also makes
		// the fingerprint's hello ask for nothing else.
		c.NextProtocol = []string{"http/1.1"}
	}
	return c, nil
}

// realitySettings is the client half of REALITY, as a tunnel's is, with the
// spider path read the way the loader reads it.
func realitySettings(o bridgespec.Outbound) (*reality.Config, error) {
	fingerprint := strings.ToLower(o.FingerprintOrDefault())
	if fingerprint == "unsafe" || fingerprint == "hellogolang" || tls.GetFingerprint(fingerprint) == nil {
		return nil, fmt.Errorf("fingerprint: %q is not one REALITY can use", fingerprint)
	}
	pub, err := base64.RawURLEncoding.DecodeString(o.PublicKey)
	if err != nil || len(pub) != 32 {
		return nil, fmt.Errorf("public_key: not 32 bytes of base64url")
	}
	if len(o.ShortID) > 16 {
		return nil, fmt.Errorf("short_id: longer than 16 hex digits")
	}
	// all 8 bytes, a shorter id padded with zeros
	shortID := make([]byte, 8)
	if _, err := hex.Decode(shortID, []byte(o.ShortID)); err != nil {
		return nil, fmt.Errorf("short_id: not hex digits")
	}
	spiderX, spiderY, err := spider(o.SpiderX)
	if err != nil {
		return nil, err
	}
	return &reality.Config{
		Fingerprint: fingerprint,
		ServerName:  o.ServerName,
		PublicKey:   pub,
		ShortId:     shortID,
		SpiderX:     spiderX,
		SpiderY:     spiderY,
	}, nil
}

// spider splits REALITY's spider path from the settings in its query, the
// loader's way.
func spider(x string) (string, []int64, error) {
	if x == "" {
		x = "/"
	}
	u, err := url.Parse(x)
	if err != nil || !strings.HasPrefix(x, "/") {
		return "", nil, fmt.Errorf("spider_x: %q is not a path", x)
	}
	y := make([]int64, 10)
	q := u.Query()
	read := func(param string, i int) {
		if v := q.Get(param); v != "" {
			bounds := strings.Split(v, "-")
			if len(bounds) == 1 {
				bounds = append(bounds, bounds[0])
			}
			y[i], _ = strconv.ParseInt(bounds[0], 10, 64)
			y[i+1], _ = strconv.ParseInt(bounds[1], 10, 64)
		}
		q.Del(param)
	}
	read("p", 0) // padding
	read("c", 2) // concurrency
	read("t", 4) // times
	read("i", 6) // interval
	read("r", 8) // return
	u.RawQuery = q.Encode()
	return u.String(), y, nil
}

// routeTo is the rules for what arrives under one inbound tag. Through an
// outbound, the blocked ranges still go to the exit first, whose own rules
// refuse them: an outbound's server may sit on the router's LAN, or reach it.
// Only literal addresses match, as the router resolves no names.
//
// An outbound left out of the config keeps a rule that names it when it has
// no fallback. Xray refuses what a rule sends to a tag no outbound has, and
// promises not to hand it to the default outbound instead.
func routeTo(inbound string, via *bridgespec.Outbound, blocked []netip.Prefix, refused map[string]string) []*router.RoutingRule {
	if via == nil {
		return []*router.RoutingRule{toExit(inbound)}
	}
	stay := toExit(inbound)
	stay.Ip = ipRules(blocked)
	leave := &router.RoutingRule{
		TargetTag:  &router.RoutingRule_Tag{Tag: via.Tag},
		InboundTag: []string{inbound},
	}
	_, left := refused[via.Tag]
	switch {
	case left && via.Fallback:
		leave.TargetTag = &router.RoutingRule_Tag{Tag: bridgespec.ExitTag}
	case via.Fallback && !left:
		leave.TargetTag = &router.RoutingRule_BalancingTag{BalancingTag: via.GroupTag()}
	}
	return []*router.RoutingRule{stay, leave}
}

// groups stand in front of the outbounds with a fallback: each holds its one
// outbound, which the check drops while it doesn't answer, and then its
// traffic goes to the exit. An outbound not checked yet counts as answering.
func groups(outbounds []bridgespec.Outbound, refused map[string]string) []*router.BalancingRule {
	var out []*router.BalancingRule
	for _, o := range outbounds {
		if _, left := refused[o.Tag]; o.Fallback && !left {
			out = append(out, &router.BalancingRule{
				Tag:              o.GroupTag(),
				OutboundSelector: []string{o.Tag},
				Strategy:         "random",
				FallbackTag:      bridgespec.ExitTag,
			})
		}
	}
	return out
}

// observatoryConfig checks every outbound the config holds, all at once, each
// time the spec's wait is over. What it finds is the status page's, and the groups'. It
// selects by the start of a tag; the spec keeps one outbound's tag from
// starting another's.
func observatoryConfig(s *bridgespec.Spec, refused map[string]string) *observatory.Config {
	var tags []string
	for _, o := range s.Outbounds {
		if _, left := refused[o.Tag]; !left {
			tags = append(tags, o.Tag)
		}
	}
	return &observatory.Config{
		SubjectSelector:   tags,
		ProbeUrl:          s.Probe.URLOrDefault(),
		ProbeInterval:     int64(time.Duration(s.Probe.SecondsOrDefault()) * time.Second),
		EnableConcurrency: true,
	}
}

// ipRules is a list of ranges as routing and freedom rules both take it.
func ipRules(prefixes []netip.Prefix) []*geodata.IPRule {
	out := make([]*geodata.IPRule, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, &geodata.IPRule{Value: &geodata.IPRule_Custom{
			Custom: &geodata.CIDRRule{Cidr: &geodata.CIDR{Ip: p.Addr().AsSlice(), Prefix: uint32(p.Bits())}},
		}})
	}
	return out
}
