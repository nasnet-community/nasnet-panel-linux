// SPDX-License-Identifier: AGPL-3.0-or-later

// Package bridgespec is the contract between the hub and the bridge
// program: the spec the hub sends on the bridge's stdin, the framing around
// it, and the shape of the status page the bridge serves. It uses the
// standard library only, so the hub can import it without the Xray core.
package bridgespec

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Version is the spec revision this package reads and writes. Hub and bridge
// ship in one image, so there is no negotiation: a different number is an error.
//
// 2 added Local, the entry on the router itself.
const Version = 2

// Limits that keep a bad spec from costing memory or time.
const (
	MaxNodes        = 16
	MaxBlocked      = 256
	MaxDNSServers   = 8
	MaxLocalClients = 64
	MaxServerNames  = 8
	MaxShortIDs     = 8
)

// Names the bridge gives its own Xray parts. A node's tags may not reuse them.
const (
	ExitTag  = "exit"
	DNSTag   = "dns"
	LocalTag = "local"
)

// VisionFlow is the only flow a tunnel may use besides none.
const VisionFlow = "xtls-rprx-vision"

// Spec is everything the bridge needs to run. The hub builds one from its
// state and router facts; the bridge runs it as is.
type Spec struct {
	Version int `json:"version"`
	// Nodes are the portals to dial. There may be none when Local is set.
	Nodes []Node `json:"nodes"`
	// Local is the entry on the router itself, nil while it is switched off.
	Local *Local `json:"local,omitempty"`
	// DialAddr is the source address of the connections to the portals: the
	// router's Domestic link.
	DialAddr string `json:"dial_addr"`
	Exit     Exit   `json:"exit"`
	DNS      DNS    `json:"dns"`
	// StatusAddr is where the status page listens: a loopback address and port.
	StatusAddr string `json:"status_addr"`
	// LogLevel is debug, info, warning (the default), error or none.
	LogLevel string `json:"log_level,omitempty"`
}

// Node is one portal the bridge dials. Its subscriptions' traffic arrives
// through the tunnel under ReverseTag and leaves through the shared exit.
type Node struct {
	// Tag names the tunnel and its counters. Unique within the spec.
	Tag string `json:"tag"`
	// Addr is the portal's IP address. Not a name: resolving one would send a
	// lookup out of the wrong interface.
	Addr string `json:"addr"`
	Port uint16 `json:"port"`
	// UUID is the id of the portal's client that carries the reverse tag.
	UUID string `json:"uuid"`
	// PublicKey is the portal's REALITY public key, base64url without padding.
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
	ServerName string `json:"server_name"`
	// Fingerprint defaults to chrome.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Flow is empty or VisionFlow, matching the portal's client.
	Flow string `json:"flow,omitempty"`
	// ReverseTag is the reverse tag the portal registers for this client.
	ReverseTag string `json:"reverse_tag"`
}

// Local is the entry on the router itself: a VLESS + REALITY inbound, as a
// node's entry is, whose clients are the subscriptions switched on for this
// router. What they send leaves through the shared exit, like what comes in
// through a tunnel.
type Local struct {
	// ListenAddr is where the inbound listens: the address the router forwards
	// its public port to, and whose replies it sends back out through the
	// Domestic link.
	ListenAddr string `json:"listen_addr"`
	Port       uint16 `json:"port"`
	// PrivateKey is the inbound's REALITY private key, base64url without padding.
	PrivateKey string `json:"private_key"`
	// ShortIDs and ServerNames are what a client may present.
	ShortIDs    []string `json:"short_ids"`
	ServerNames []string `json:"server_names"`
	// Target is the Reality target, host:port: where REALITY takes a handshake
	// that isn't a client's. The bridge reaches it from ListenAddr.
	Target string `json:"target"`
	// Flow is empty or VisionFlow, matching the clients' links.
	Flow string `json:"flow,omitempty"`
	// Clients are the subscriptions that may connect when the bridge starts.
	// The hub changes them afterwards over the control path, without a restart.
	Clients []Client `json:"clients,omitempty"`
	// Token is what the hub sends with every change of clients. The control
	// path listens on loopback, and this keeps it the hub's alone.
	Token string `json:"token"`
}

// Client is one subscription's login on the router's entry.
type Client struct {
	// Email names the client and its byte counters. Unique within the spec.
	Email string `json:"email"`
	UUID  string `json:"uuid"`
}

// Exit is where tunnel traffic leaves the bridge.
type Exit struct {
	// Addr is the source address of connections to the internet: the one the
	// router sends through its VPN. It differs from DialAddr, or the exit
	// would leave through the Domestic link.
	Addr string `json:"addr"`
	// Blocked lists ranges a tunnel may not reach, so no subscription gets at
	// the router's LAN. Empty means DefaultBlocked.
	Blocked []string `json:"blocked,omitempty"`
}

// DNS is how the bridge resolves the names subscriptions ask for. The
// queries leave through the exit, never through the system resolver.
type DNS struct {
	// Servers are resolver IP addresses, "ip" or "ip:port" (port 53 if
	// omitted). At least one: with none Xray falls back to the system resolver.
	Servers []string `json:"servers"`
}

// DefaultBlocked is the list the Linux panel's own bridge exit uses: the
// private, loopback, link-local, multicast and documentation ranges, spelled
// out so nothing has to load geoip.dat.
func DefaultBlocked() []string {
	return []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3",
		"::/127", "fc00::/7", "fe80::/10", "ff00::/8",
	}
}

// BlockedOrDefault is the exit's ranges with the default filled in.
func (e Exit) BlockedOrDefault() []string {
	if len(e.Blocked) == 0 {
		return DefaultBlocked()
	}
	return e.Blocked
}

// FingerprintOrDefault is the uTLS fingerprint the tunnel presents.
func (n Node) FingerprintOrDefault() string {
	if n.Fingerprint == "" {
		return "chrome"
	}
	return n.Fingerprint
}

// LogLevelOrDefault is the Xray log level.
func (s *Spec) LogLevelOrDefault() string {
	if s.LogLevel == "" {
		return "warning"
	}
	return s.LogLevel
}

// String leaves the ids out, so a spec that ends up in a log line doesn't
// hand the tunnels' credentials around.
func (s *Spec) String() string {
	tags := make([]string, len(s.Nodes))
	for i, n := range s.Nodes {
		tags[i] = n.Tag
	}
	local := "off"
	if s.Local != nil {
		local = s.Local.String()
	}
	return fmt.Sprintf("bridge spec v%d (nodes %s, local %s, dial %s, exit %s, status %s)",
		s.Version, strings.Join(tags, ","), local, s.DialAddr, s.Exit.Addr, s.StatusAddr)
}

// String leaves the key, the ids and the token out, like Spec.
func (l *Local) String() string {
	return fmt.Sprintf("%s:%d with %d client(s)", l.ListenAddr, l.Port, len(l.Clients))
}

// String leaves the id out.
func (c Client) String() string { return "client " + c.Email }

// String leaves the id out, like Spec.
func (n Node) String() string {
	return fmt.Sprintf("node %s (%s:%d)", n.Tag, n.Addr, n.Port)
}

var (
	tagRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	uuidRe        = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	shortIDRe     = regexp.MustCompile(`^([0-9a-fA-F]{2}){1,8}$`)
	fingerprintRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	labelRe       = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	emailRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)
	tokenRe       = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	logLevels     = map[string]bool{"debug": true, "info": true, "warning": true, "error": true, "none": true}
)

// Validate checks a spec the way the bridge will use it, so the hub finds out
// when it builds one and the bridge never starts on a half-valid one. It
// reports the first problem it finds.
func (s *Spec) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("version: %d is not supported, want %d", s.Version, Version)
	}
	if !logLevels[s.LogLevelOrDefault()] {
		return fmt.Errorf("log_level: %q is not debug, info, warning, error or none", s.LogLevel)
	}

	dial, err := parseIP("dial_addr", s.DialAddr)
	if err != nil {
		return err
	}
	exit, err := parseIP("exit.addr", s.Exit.Addr)
	if err != nil {
		return err
	}
	// Loopback is the exception: a dev run or a test has no second address.
	if dial == exit && !dial.IsLoopback() {
		return fmt.Errorf("exit.addr: same as dial_addr (%s), so exit traffic would leave through the dial link", dial)
	}

	blocked, err := parseBlocked(s.Exit.BlockedOrDefault())
	if err != nil {
		return err
	}
	if err := s.validateDNS(exit, blocked); err != nil {
		return err
	}

	status, err := netip.ParseAddrPort(s.StatusAddr)
	if err != nil {
		return fmt.Errorf("status_addr: %q is not an address and port", s.StatusAddr)
	}
	if !status.Addr().IsLoopback() || status.Port() == 0 {
		return fmt.Errorf("status_addr: %s must be a loopback address with a port", s.StatusAddr)
	}

	if err := s.validateNodes(dial); err != nil {
		return err
	}
	return s.validateLocal(exit, status)
}

func (s *Spec) validateNodes(dial netip.Addr) error {
	if len(s.Nodes) == 0 && s.Local == nil {
		return fmt.Errorf("nodes: none, and no local entry either")
	}
	if len(s.Nodes) > MaxNodes {
		return fmt.Errorf("nodes: %d is more than %d", len(s.Nodes), MaxNodes)
	}

	// Every tag lands in the Xray config; two with one name would shadow each other.
	used := map[string]string{ExitTag: "the exit", DNSTag: "the dns", LocalTag: "the local entry"}
	claim := func(field, tag string) error {
		if !tagRe.MatchString(tag) {
			return fmt.Errorf("%s: %q must be 1-63 lowercase letters, digits, '.', '_' or '-'", field, tag)
		}
		if prev, dup := used[tag]; dup {
			return fmt.Errorf("%s: %q is already used by %s", field, tag, prev)
		}
		used[tag] = field
		return nil
	}
	ports := map[netip.AddrPort]string{}

	for i, n := range s.Nodes {
		at := func(field string) string { return fmt.Sprintf("nodes[%d].%s", i, field) }
		if err := claim(at("tag"), n.Tag); err != nil {
			return err
		}
		if err := claim(at("reverse_tag"), n.ReverseTag); err != nil {
			return err
		}
		addr, err := parseIP(at("addr"), n.Addr)
		if err != nil {
			return err
		}
		if addr.Is4() != dial.Is4() {
			return fmt.Errorf("%s: %s is not the same address family as dial_addr %s", at("addr"), addr, dial)
		}
		if n.Port == 0 {
			return fmt.Errorf("%s: missing", at("port"))
		}
		ap := netip.AddrPortFrom(addr, n.Port)
		if prev, dup := ports[ap]; dup {
			return fmt.Errorf("%s: %s is already used by %s", at("addr"), ap, prev)
		}
		ports[ap] = at("addr")

		if !uuidRe.MatchString(n.UUID) {
			return fmt.Errorf("%s: %q is not a UUID", at("uuid"), redact(n.UUID))
		}
		if raw, err := base64.RawURLEncoding.DecodeString(n.PublicKey); err != nil || len(raw) != 32 {
			return fmt.Errorf("%s: not a 32-byte base64url key", at("public_key"))
		}
		if !shortIDRe.MatchString(n.ShortID) {
			return fmt.Errorf("%s: %q must be 2 to 16 hex digits, in pairs", at("short_id"), n.ShortID)
		}
		if !validHostname(n.ServerName) {
			return fmt.Errorf("%s: %q is not a host name", at("server_name"), n.ServerName)
		}
		if n.Fingerprint != "" && !fingerprintRe.MatchString(n.Fingerprint) {
			return fmt.Errorf("%s: %q is not a fingerprint name", at("fingerprint"), n.Fingerprint)
		}
		if n.Flow != "" && n.Flow != VisionFlow {
			return fmt.Errorf("%s: %q is not empty or %s", at("flow"), n.Flow, VisionFlow)
		}
	}
	return nil
}

func (s *Spec) validateLocal(exit netip.Addr, status netip.AddrPort) error {
	l := s.Local
	if l == nil {
		return nil
	}
	listen, err := parseIP("local.listen_addr", l.ListenAddr)
	if err != nil {
		return err
	}
	// Loopback is the exception, as it is for the dial address.
	if listen == exit && !listen.IsLoopback() {
		return fmt.Errorf("local.listen_addr: same as exit.addr (%s), so replies to clients would leave through the exit", listen)
	}
	if l.Port == 0 {
		return fmt.Errorf("local.port: missing")
	}
	if netip.AddrPortFrom(listen, l.Port) == status {
		return fmt.Errorf("local.port: %s is the status page's address", status)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(l.PrivateKey); err != nil || len(raw) != 32 {
		return fmt.Errorf("local.private_key: not a 32-byte base64url key")
	}

	if len(l.ShortIDs) == 0 {
		return fmt.Errorf("local.short_ids: none")
	}
	if len(l.ShortIDs) > MaxShortIDs {
		return fmt.Errorf("local.short_ids: %d is more than %d", len(l.ShortIDs), MaxShortIDs)
	}
	for i, id := range l.ShortIDs {
		if !shortIDRe.MatchString(id) {
			return fmt.Errorf("local.short_ids[%d]: %q must be 2 to 16 hex digits, in pairs", i, id)
		}
	}
	if len(l.ServerNames) == 0 {
		return fmt.Errorf("local.server_names: none")
	}
	if len(l.ServerNames) > MaxServerNames {
		return fmt.Errorf("local.server_names: %d is more than %d", len(l.ServerNames), MaxServerNames)
	}
	for i, name := range l.ServerNames {
		if !validHostname(name) {
			return fmt.Errorf("local.server_names[%d]: %q is not a host name", i, name)
		}
	}
	if _, _, err := SplitTarget(l.Target); err != nil {
		return fmt.Errorf("local.target: %w", err)
	}
	if l.Flow != "" && l.Flow != VisionFlow {
		return fmt.Errorf("local.flow: %q is not empty or %s", l.Flow, VisionFlow)
	}
	if !tokenRe.MatchString(l.Token) {
		return fmt.Errorf("local.token: must be 16 to 128 letters, digits, '_' or '-'")
	}
	return s.validateClients()
}

// validateClients holds every client's email apart from the others and from
// the nodes' tags: a tunnel counts its bytes under its tag, as if the tag were
// an email, so a client by that name would be counted into the tunnel.
func (s *Spec) validateClients() error {
	clients := s.Local.Clients
	if len(clients) > MaxLocalClients {
		return fmt.Errorf("local.clients: %d is more than %d", len(clients), MaxLocalClients)
	}
	emails := map[string]string{}
	for i, n := range s.Nodes {
		emails[n.Tag] = fmt.Sprintf("nodes[%d].tag", i)
	}
	ids := map[string]string{}
	for i, c := range clients {
		at := func(field string) string { return fmt.Sprintf("local.clients[%d].%s", i, field) }
		if !emailRe.MatchString(c.Email) {
			return fmt.Errorf("%s: %q must be 1-64 letters, digits, '.', '_', '@' or '-'", at("email"), c.Email)
		}
		if prev, dup := emails[c.Email]; dup {
			return fmt.Errorf("%s: %q is already used by %s", at("email"), c.Email, prev)
		}
		emails[c.Email] = at("email")
		if !uuidRe.MatchString(c.UUID) {
			return fmt.Errorf("%s: %q is not a UUID", at("uuid"), redact(c.UUID))
		}
		id := strings.ToLower(c.UUID)
		if prev, dup := ids[id]; dup {
			return fmt.Errorf("%s: the same id as %s", at("uuid"), prev)
		}
		ids[id] = at("uuid")
	}
	return nil
}

// SplitTarget reads a Reality target, "host:port" with a host name or an IP
// address.
func SplitTarget(s string) (host string, port uint16, err error) {
	host, raw, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a host and a port", s)
	}
	n, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || n == 0 {
		return "", 0, fmt.Errorf("%q has no usable port", s)
	}
	if a, err := netip.ParseAddr(host); err == nil {
		if a.Zone() != "" || a.IsUnspecified() || a.IsMulticast() {
			return "", 0, fmt.Errorf("%q is not a unicast address", host)
		}
		return a.Unmap().String(), uint16(n), nil
	}
	if !validHostname(host) {
		return "", 0, fmt.Errorf("%q is not a host name", host)
	}
	return host, uint16(n), nil
}

func (s *Spec) validateDNS(exit netip.Addr, blocked []netip.Prefix) error {
	servers := s.DNS.Servers
	if len(servers) == 0 {
		return fmt.Errorf("dns.servers: none, and without one Xray resolves through the system")
	}
	if len(servers) > MaxDNSServers {
		return fmt.Errorf("dns.servers: %d is more than %d", len(servers), MaxDNSServers)
	}
	for i, raw := range servers {
		field := fmt.Sprintf("dns.servers[%d]", i)
		ap, err := ParseServer(raw)
		if err != nil {
			return fmt.Errorf("%s: %q is not an IP address with an optional port", field, raw)
		}
		if ap.Addr().IsUnspecified() || ap.Addr().IsMulticast() {
			return fmt.Errorf("%s: %s is not a unicast address", field, raw)
		}
		if ap.Addr().Is4() != exit.Is4() {
			return fmt.Errorf("%s: %s is not the same address family as exit.addr %s", field, raw, exit)
		}
		// The exit's rules apply to the resolver's traffic too.
		for _, p := range blocked {
			if p.Contains(ap.Addr()) {
				return fmt.Errorf("%s: %s is inside blocked range %s, so the exit would refuse the query", field, raw, p)
			}
		}
	}
	return nil
}

// ParseServer reads a DNS server entry: "ip" or "ip:port", port 53 if omitted.
func ParseServer(s string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		if ap.Port() == 0 {
			return netip.AddrPort{}, fmt.Errorf("port 0")
		}
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return netip.AddrPort{}, fmt.Errorf("not an address")
	}
	return netip.AddrPortFrom(a.Unmap(), 53), nil
}

func parseIP(field, s string) (netip.Addr, error) {
	if s == "" {
		return netip.Addr{}, fmt.Errorf("%s: missing", field)
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("%s: %q is not an IP address", field, s)
	}
	a = a.Unmap()
	if a.IsUnspecified() || a.IsMulticast() {
		return netip.Addr{}, fmt.Errorf("%s: %s is not a unicast address", field, s)
	}
	return a, nil
}

func parseBlocked(list []string) ([]netip.Prefix, error) {
	if len(list) > MaxBlocked {
		return nil, fmt.Errorf("exit.blocked: %d is more than %d", len(list), MaxBlocked)
	}
	out := make([]netip.Prefix, len(list))
	for i, raw := range list {
		p, err := netip.ParsePrefix(raw)
		if err != nil || p.Addr().Zone() != "" {
			return nil, fmt.Errorf("exit.blocked[%d]: %q is not a CIDR range", i, raw)
		}
		// Xray folds ::ffff:a.b.c.d into IPv4 before it matches, so a range
		// spelled that way would never apply.
		if p.Addr().Is4In6() {
			return nil, fmt.Errorf("exit.blocked[%d]: %q is an IPv4-mapped range, write it as IPv4", i, raw)
		}
		out[i] = p
	}
	return out, nil
}

func validHostname(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !labelRe.MatchString(label) {
			return false
		}
	}
	return true
}

// redact keeps the start of an id so a log line can tell which one was bad.
func redact(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[:4] + "..."
}
