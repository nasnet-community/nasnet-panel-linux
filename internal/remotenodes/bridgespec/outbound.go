// SPDX-License-Identifier: AGPL-3.0-or-later

package bridgespec

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The kinds of outbound the bridge carries.
const (
	ProtoVLESS       = "vless"
	ProtoVMess       = "vmess"
	ProtoTrojan      = "trojan"
	ProtoShadowsocks = "shadowsocks"
	ProtoSOCKS       = "socks"
	ProtoHTTP        = "http"
)

// The transports an outbound may ride, and what secures them. Empty means the
// first of each.
const (
	NetTCP         = "tcp"
	NetWS          = "ws"
	NetHTTPUpgrade = "httpupgrade"
	NetXHTTP       = "xhttp"

	SecNone    = "none"
	SecTLS     = "tls"
	SecReality = "reality"
)

// How the outbounds are checked when the spec doesn't say.
const (
	DefaultProbeURL     = "https://www.google.com/generate_204"
	DefaultProbeSeconds = 30
	MinProbeSeconds     = 5
	MaxProbeSeconds     = 3600
)

// GroupPrefix starts the name of the group that stands in front of an outbound
// with a fallback. A tag may not start with it.
const GroupPrefix = "group-"

// Outbound is a server the traffic of a node, or of the local entry, can leave
// through instead of the shared exit. The bridge reaches it from the spec's
// OutboundAddr.
type Outbound struct {
	// Tag names it and its counters. Unique within the spec.
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	// Addr is the server's IP address. Not a name, for the reason a node's
	// isn't: the lookup would leave from the wrong address.
	Addr string `json:"addr"`
	Port uint16 `json:"port"`

	// ID is the user id of VLESS and VMess.
	ID string `json:"id,omitempty"`
	// Flow is VLESS's: empty or VisionFlow.
	Flow string `json:"flow,omitempty"`
	// Username and Password are the login of SOCKS and HTTP, where both may be
	// empty. Password alone is Trojan's and Shadowsocks's.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Method is the Shadowsocks cipher, or VMess's (empty means auto).
	Method string `json:"method,omitempty"`

	// Network is the transport, and Path, Host and Mode its settings: Path and
	// Host for all but tcp, Mode for xhttp alone.
	Network string `json:"network,omitempty"`
	Path    string `json:"path,omitempty"`
	Host    string `json:"host,omitempty"`
	Mode    string `json:"mode,omitempty"`

	// Security is what wraps the transport.
	Security string `json:"security,omitempty"`
	// ServerName is the name the handshake asks for. With tls it may be empty
	// only if nothing checks it, which nothing here allows, so it is required.
	ServerName  string   `json:"server_name,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	// PublicKey, ShortID and SpiderX are REALITY's. The key is base64url
	// without padding, as a node's is.
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`
	SpiderX   string `json:"spider_x,omitempty"`

	// Fallback sends the traffic through the shared exit for as long as the
	// outbound doesn't answer its check.
	Fallback bool `json:"fallback,omitempty"`
}

// Probe is how the bridge checks that an outbound answers: a web request
// through it, again and again.
type Probe struct {
	// URL is what it asks for. Empty means DefaultProbeURL.
	URL string `json:"url,omitempty"`
	// Seconds is the wait between two checks. Zero means DefaultProbeSeconds.
	Seconds int `json:"seconds,omitempty"`
}

// URLOrDefault is the address the check asks for.
func (p Probe) URLOrDefault() string {
	if p.URL == "" {
		return DefaultProbeURL
	}
	return p.URL
}

// SecondsOrDefault is the wait between two checks.
func (p Probe) SecondsOrDefault() int {
	if p.Seconds == 0 {
		return DefaultProbeSeconds
	}
	return p.Seconds
}

// NetworkOrDefault is the transport with the default filled in.
func (o Outbound) NetworkOrDefault() string {
	if o.Network == "" {
		return NetTCP
	}
	return o.Network
}

// SecurityOrDefault is the security with the default filled in.
func (o Outbound) SecurityOrDefault() string {
	if o.Security == "" {
		return SecNone
	}
	return o.Security
}

// FingerprintOrDefault is the uTLS fingerprint the handshake presents.
func (o Outbound) FingerprintOrDefault() string {
	if o.Fingerprint == "" {
		return "chrome"
	}
	return o.Fingerprint
}

// GroupTag names the group in front of the outbound when it has a fallback.
func (o Outbound) GroupTag() string { return GroupPrefix + o.Tag }

// String leaves the credentials out, like Node.
func (o Outbound) String() string {
	return fmt.Sprintf("outbound %s (%s to %s:%d)", o.Tag, o.Protocol, o.Addr, o.Port)
}

// CheckProbeURL says what is wrong with an address for the check.
func CheckProbeURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("%q is not an http or https address", raw)
	}
	if len(raw) > 255 {
		return fmt.Errorf("longer than 255 characters")
	}
	host := u.Hostname()
	if !validHostname(host) {
		if _, perr := parseIP("host", host); perr != nil {
			return fmt.Errorf("%q has no usable host", raw)
		}
	}
	return nil
}

func (p Probe) validate() error {
	if err := CheckProbeURL(p.URL); err != nil {
		return fmt.Errorf("probe.url: %w", err)
	}
	if p.Seconds != 0 && (p.Seconds < MinProbeSeconds || p.Seconds > MaxProbeSeconds) {
		return fmt.Errorf("probe.seconds: %d is not between %d and %d", p.Seconds, MinProbeSeconds, MaxProbeSeconds)
	}
	return nil
}

// The ciphers of Shadowsocks. The 2022 ones take a key of a fixed length, as
// base64, where the others take any password.
var (
	shadowsocksCiphers = map[string]bool{
		"aes-128-gcm": true, "aes-256-gcm": true,
		"chacha20-poly1305": true, "chacha20-ietf-poly1305": true,
		"xchacha20-poly1305": true, "xchacha20-ietf-poly1305": true,
	}
	shadowsocks2022Keys = map[string]int{
		"2022-blake3-aes-128-gcm":       16,
		"2022-blake3-aes-256-gcm":       32,
		"2022-blake3-chacha20-poly1305": 32,
	}
	vmessCiphers = map[string]bool{"": true, "auto": true, "aes-128-gcm": true, "chacha20-poly1305": true}
	xhttpModes   = map[string]bool{"": true, "auto": true, "packet-up": true, "stream-up": true, "stream-one": true}

	alpnRe = regexp.MustCompile(`^[a-z0-9][a-z0-9./-]{0,15}$`)
)

// Shadowsocks2022 says whether a cipher is one of the 2022 ones.
func Shadowsocks2022(method string) bool { return shadowsocks2022Keys[method] != 0 }

// CheckOutbound says what is wrong with an outbound's settings, the first
// thing it finds: everything but its tag and its address, which only mean
// something inside a spec. The hub runs it on what an admin enters.
func CheckOutbound(o Outbound) error {
	if o.Port == 0 {
		return fmt.Errorf("port: missing")
	}
	network, security := o.NetworkOrDefault(), o.SecurityOrDefault()
	switch network {
	case NetTCP, NetWS, NetHTTPUpgrade, NetXHTTP:
	default:
		return fmt.Errorf("network: %q is not tcp, ws, httpupgrade or xhttp", o.Network)
	}
	switch security {
	case SecNone, SecTLS, SecReality:
	default:
		return fmt.Errorf("security: %q is not none, tls or reality", o.Security)
	}

	if err := o.checkLogin(network, security); err != nil {
		return err
	}
	if err := o.checkTransport(network); err != nil {
		return err
	}
	return o.checkSecurity(security)
}

// checkLogin checks what the protocol itself needs, and that the fields of the
// other protocols are empty: one left behind would be a credential the bridge
// never uses and the hub still keeps.
func (o Outbound) checkLogin(network, security string) error {
	needID, needPassword, mayLogin, mayMethod := false, false, false, false
	switch o.Protocol {
	case ProtoVLESS:
		needID = true
		if o.Flow != "" && o.Flow != VisionFlow {
			return fmt.Errorf("flow: %q is not empty or %s", o.Flow, VisionFlow)
		}
		if o.Flow == VisionFlow && (network != NetTCP || security == SecNone) {
			return fmt.Errorf("flow: %s needs tcp with tls or reality", VisionFlow)
		}
	case ProtoVMess:
		needID, mayMethod = true, true
		if !vmessCiphers[o.Method] {
			return fmt.Errorf("method: %q is not a VMess cipher", o.Method)
		}
		if security == SecReality {
			return fmt.Errorf("security: VMess does not run over reality here")
		}
	case ProtoTrojan:
		needPassword = true
	case ProtoShadowsocks:
		needPassword, mayMethod = true, true
		if network != NetTCP || security != SecNone {
			return fmt.Errorf("network: Shadowsocks runs over plain tcp only")
		}
		if size := shadowsocks2022Keys[o.Method]; size != 0 {
			// "server key:user key" on a server with several users
			for _, part := range strings.Split(o.Password, ":") {
				if raw, err := base64.StdEncoding.DecodeString(part); err != nil || len(raw) != size {
					return fmt.Errorf("password: %s takes a %d-byte key as base64", o.Method, size)
				}
			}
		} else if !shadowsocksCiphers[o.Method] {
			return fmt.Errorf("method: %q is not a Shadowsocks cipher the bridge carries", o.Method)
		}
	case ProtoSOCKS, ProtoHTTP:
		mayLogin = true
		if (o.Username == "") != (o.Password == "") {
			return fmt.Errorf("username: a user name and a password go together")
		}
		if network != NetTCP || security == SecReality || (o.Protocol == ProtoSOCKS && security != SecNone) {
			return fmt.Errorf("network: %s runs over plain tcp, HTTP with tls too", o.Protocol)
		}
	default:
		return fmt.Errorf("protocol: %q is not vless, vmess, trojan, shadowsocks, socks or http", o.Protocol)
	}

	if needID && !uuidRe.MatchString(o.ID) {
		return fmt.Errorf("id: %q is not a UUID", redact(o.ID))
	}
	if !needID && o.ID != "" {
		return fmt.Errorf("id: %s has none", o.Protocol)
	}
	if o.Flow != "" && o.Protocol != ProtoVLESS {
		return fmt.Errorf("flow: %s has none", o.Protocol)
	}
	if needPassword && o.Password == "" {
		return fmt.Errorf("password: missing")
	}
	if !needPassword && !mayLogin && o.Password != "" {
		return fmt.Errorf("password: %s has none", o.Protocol)
	}
	if !mayLogin && o.Username != "" {
		return fmt.Errorf("username: %s has none", o.Protocol)
	}
	if !mayMethod && o.Method != "" {
		return fmt.Errorf("method: %s has none", o.Protocol)
	}
	for field, v := range map[string]string{"username": o.Username, "password": o.Password} {
		if len(v) > 255 || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("%s: longer than 255 characters, or with a line break in it", field)
		}
	}
	return nil
}

func (o Outbound) checkTransport(network string) error {
	if network == NetTCP {
		if o.Path != "" || o.Host != "" || o.Mode != "" {
			return fmt.Errorf("path: tcp has no path, host or mode")
		}
		return nil
	}
	if o.Path != "" && (!strings.HasPrefix(o.Path, "/") || len(o.Path) > 512 || strings.ContainsAny(o.Path, " \r\n\x00")) {
		return fmt.Errorf("path: %q must start with / and hold no spaces", o.Path)
	}
	if o.Host != "" && !validHostname(o.Host) {
		return fmt.Errorf("host: %q is not a host name", o.Host)
	}
	if network != NetXHTTP && o.Mode != "" {
		return fmt.Errorf("mode: only xhttp has one")
	}
	if !xhttpModes[o.Mode] {
		return fmt.Errorf("mode: %q is not auto, packet-up, stream-up or stream-one", o.Mode)
	}
	return nil
}

func (o Outbound) checkSecurity(security string) error {
	if security == SecNone {
		if o.ServerName != "" || o.Fingerprint != "" || len(o.ALPN) != 0 || o.PublicKey != "" || o.ShortID != "" || o.SpiderX != "" {
			return fmt.Errorf("security: none takes no server name, fingerprint, ALPN or REALITY settings")
		}
		return nil
	}
	if !validHostname(o.ServerName) {
		return fmt.Errorf("server_name: %q is not a host name", o.ServerName)
	}
	if o.Fingerprint != "" && !fingerprintRe.MatchString(o.Fingerprint) {
		return fmt.Errorf("fingerprint: %q is not a fingerprint name", o.Fingerprint)
	}
	if security == SecTLS {
		if o.PublicKey != "" || o.ShortID != "" || o.SpiderX != "" {
			return fmt.Errorf("security: tls takes no REALITY settings")
		}
		if len(o.ALPN) > 4 {
			return fmt.Errorf("alpn: %d is more than 4", len(o.ALPN))
		}
		for _, a := range o.ALPN {
			if !alpnRe.MatchString(a) {
				return fmt.Errorf("alpn: %q is not a protocol name", a)
			}
		}
		return nil
	}
	if len(o.ALPN) != 0 {
		return fmt.Errorf("alpn: reality takes none")
	}
	if n := o.NetworkOrDefault(); n != NetTCP && n != NetXHTTP {
		return fmt.Errorf("security: reality runs over tcp or xhttp, not %s", n)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(o.PublicKey); err != nil || len(raw) != 32 {
		return fmt.Errorf("public_key: not a 32-byte base64url key")
	}
	// REALITY allows an empty short id
	if o.ShortID != "" && !shortIDRe.MatchString(o.ShortID) {
		return fmt.Errorf("short_id: %q must be 2 to 16 hex digits, in pairs", o.ShortID)
	}
	if o.SpiderX != "" && (!strings.HasPrefix(o.SpiderX, "/") || len(o.SpiderX) > 255 || strings.ContainsAny(o.SpiderX, " \r\n\x00")) {
		return fmt.Errorf("spider_x: %q must start with / and hold no spaces", o.SpiderX)
	}
	return nil
}

// ProbeArg is the argument that makes the bridge program try one outbound and
// leave, in place of running a spec: it reads a ProbeRequest on stdin and
// writes a ProbeResult to stdout.
const ProbeArg = "probe"

// How long a probe may take when the request doesn't say.
const (
	DefaultProbeTimeout = 10
	MaxProbeTimeout     = 30
)

// ProbeRequest is one outbound to try, apart from any running bridge. The hub
// asks before it keeps an outbound, and when an admin presses Test.
type ProbeRequest struct {
	// Outbound is what to try. Its Tag may be empty.
	Outbound Outbound `json:"outbound"`
	// From is the source address to connect from, as Spec.OutboundAddr. Empty
	// leaves the choice to the system, for a machine that has no such address.
	From string `json:"from,omitempty"`
	// Connect asks for a web request through the outbound. Without it the
	// outbound is only built, which tells whether Xray accepts its settings.
	Connect bool `json:"connect,omitempty"`
	// URL is what the request asks for. Empty means DefaultProbeURL.
	URL string `json:"url,omitempty"`
	// TimeoutSeconds bounds the request. Zero means DefaultProbeTimeout.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// ProbeResult is how the try went.
type ProbeResult struct {
	// OK is whether the outbound was built and, with Connect, answered.
	OK bool `json:"ok"`
	// DelayMS is how long the answer took, and Status its HTTP status.
	DelayMS int64 `json:"delay_ms,omitempty"`
	Status  int   `json:"status,omitempty"`
	// Error is what went wrong, in a sentence an admin can read.
	Error string `json:"error,omitempty"`
}

// Validate checks a request the way the bridge will use it.
func (r ProbeRequest) Validate() error {
	if r.From != "" {
		from, err := parseIP("from", r.From)
		if err != nil {
			return err
		}
		addr, err := parseIP("outbound.addr", r.Outbound.Addr)
		if err != nil {
			return err
		}
		if addr.Is4() != from.Is4() {
			return fmt.Errorf("outbound.addr: %s is not the same address family as from %s", addr, from)
		}
	} else if _, err := parseIP("outbound.addr", r.Outbound.Addr); err != nil {
		return err
	}
	if err := CheckOutbound(r.Outbound); err != nil {
		return fmt.Errorf("outbound.%w", err)
	}
	if err := CheckProbeURL(r.URL); err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if r.TimeoutSeconds < 0 || r.TimeoutSeconds > MaxProbeTimeout {
		return fmt.Errorf("timeout_seconds: %d is not between 0 and %d", r.TimeoutSeconds, MaxProbeTimeout)
	}
	return nil
}

// TimeoutOrDefault is how long the request may take, in seconds.
func (r ProbeRequest) TimeoutOrDefault() int {
	if r.TimeoutSeconds == 0 {
		return DefaultProbeTimeout
	}
	return r.TimeoutSeconds
}

// URLOrDefault is the address the request asks for.
func (r ProbeRequest) URLOrDefault() string { return Probe{URL: r.URL}.URLOrDefault() }
