package domain

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// DetectWGInput tells a pasted URI from a pasted config file. The two forms
// cannot be confused, so the operator never has to say which one they have.
// Returns "uri", "conf", or "" for anything else.
func DetectWGInput(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(t), "wireguard://") {
		return "uri"
	}
	if strings.Contains(strings.ToLower(t), "[interface]") {
		return "conf"
	}
	return ""
}

// ParseWireGuardConfig accepts either form.
func ParseWireGuardConfig(s string) (WireGuardConfig, error) {
	switch DetectWGInput(s) {
	case "uri":
		return ParseWireGuardURI(s)
	case "conf":
		return ParseWireGuardConf(s)
	}
	return WireGuardConfig{}, ErrNotWireGuard
}

// Query keys seen in the wild. No standard exists for wireguard://, so this
// follows the v2rayN family and treats anything unknown as a notice.
var uriAliases = map[string]string{
	"publickey":            "publickey",
	"public_key":           "publickey",
	"peer_public_key":      "publickey",
	"pubkey":               "publickey",
	"presharedkey":         "presharedkey",
	"preshared_key":        "presharedkey",
	"psk":                  "presharedkey",
	"address":              "address",
	"ip":                   "address",
	"allowedips":           "allowedips",
	"allowed_ips":          "allowedips",
	"mtu":                  "mtu",
	"keepalive":            "keepalive",
	"persistentkeepalive":  "keepalive",
	"persistent_keepalive": "keepalive",
	"reserved":             "reserved",
}

// ParseWireGuardURI reads the v2rayN-family form:
//
//	wireguard://<urlencoded private key>@host:port?publickey=…&address=…#name
func ParseWireGuardURI(raw string) (WireGuardConfig, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return WireGuardConfig{}, fmt.Errorf("%w: %v", ErrNotWireGuard, err)
	}
	if !strings.EqualFold(u.Scheme, "wireguard") {
		return WireGuardConfig{}, ErrNotWireGuard
	}

	var cfg WireGuardConfig
	if u.User == nil || u.User.Username() == "" {
		return WireGuardConfig{}, fmt.Errorf("no private key in the URI: %w", ErrBadKey)
	}
	cfg.PrivateKey = u.User.Username()
	cfg.Peer.Endpoint = u.Host
	cfg.SuggestedName = u.Fragment

	var addresses, allowedIPs []string
	for key, vals := range u.Query() {
		if len(vals) == 0 || vals[0] == "" {
			continue
		}
		v := vals[0]
		switch uriAliases[strings.ToLower(key)] {
		case "publickey":
			cfg.Peer.PublicKey = v
		case "presharedkey":
			cfg.Peer.PresharedKey = v
		case "address":
			addresses = splitList(v)
		case "allowedips":
			allowedIPs = splitList(v)
		case "mtu":
			cfg.MTU, _ = strconv.Atoi(v)
		case "keepalive":
			cfg.Peer.PersistentKeepalive, _ = strconv.Atoi(v)
		case "reserved":
			// Kernel WireGuard cannot prepend the reserved bytes WARP and
			// AmneziaWG need, so the handshake would never complete. Silence
			// here would look like a dead server.
			return WireGuardConfig{}, fmt.Errorf(
				"%w: the \"reserved\" setting is a Cloudflare WARP or AmneziaWG feature that the Linux kernel driver cannot do", ErrReservedParam)
		default:
			cfg.Notices = append(cfg.Notices, fmt.Sprintf("Ignored the unsupported setting %q.", key))
		}
	}

	if err := cfg.finish(addresses, allowedIPs); err != nil {
		return WireGuardConfig{}, err
	}
	return cfg, nil
}

// Keys whose value is a shell command or a routing directive we own. Executing
// a line out of an uploaded file would be a remote root shell, and letting the
// file pick a routing table would fight the kill switch.
var refusedConfKeys = map[string]bool{
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"table": true, "saveconfig": true,
}

// ParseWireGuardConf reads the wg-quick file format. Nothing in it is ever
// executed: the script keys are refused by name.
func ParseWireGuardConf(raw string) (WireGuardConfig, error) {
	var (
		cfg                   WireGuardConfig
		section               string
		peers                 int
		addresses, allowedIPs []string
		dnsValues             []string
	)

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(stripComment(line))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			if section == "peer" {
				peers++
				if peers > 1 {
					return WireGuardConfig{}, ErrMultiplePeers
				}
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		rawKey := strings.TrimSpace(key)
		k := strings.ToLower(rawKey)
		v := strings.TrimSpace(value)

		if refusedConfKeys[k] {
			return WireGuardConfig{}, fmt.Errorf("%w: %s", ErrScriptKey, rawKey)
		}

		switch section {
		case "interface":
			switch k {
			case "privatekey":
				cfg.PrivateKey = v
			case "address":
				addresses = append(addresses, splitList(v)...)
			case "dns":
				dnsValues = append(dnsValues, splitList(v)...)
			case "mtu":
				cfg.MTU, _ = strconv.Atoi(v)
			case "listenport":
				cfg.ListenPort, _ = strconv.Atoi(v)
			default:
				cfg.Notices = append(cfg.Notices, fmt.Sprintf("Ignored the unsupported setting %q.", rawKey))
			}
		case "peer":
			switch k {
			case "publickey":
				cfg.Peer.PublicKey = v
			case "presharedkey":
				cfg.Peer.PresharedKey = v
			case "allowedips":
				allowedIPs = append(allowedIPs, splitList(v)...)
			case "endpoint":
				cfg.Peer.Endpoint = v
			case "persistentkeepalive":
				cfg.Peer.PersistentKeepalive, _ = strconv.Atoi(v)
			default:
				cfg.Notices = append(cfg.Notices, fmt.Sprintf("Ignored the unsupported setting %q.", rawKey))
			}
		}
	}

	if peers == 0 {
		return WireGuardConfig{}, fmt.Errorf("%w: no [Peer] section", ErrNotWireGuard)
	}
	cfg.setDNS(dnsValues)
	if err := cfg.finish(addresses, allowedIPs); err != nil {
		return WireGuardConfig{}, err
	}
	return cfg, nil
}

// finish applies the shared IPv4-only filtering and the defaults, so both input
// forms cannot drift apart.
func (c *WireGuardConfig) finish(addresses, allowedIPs []string) error {
	v4Addrs, droppedAddrs := keepIPv4Prefixes(addresses)
	if len(v4Addrs) == 0 {
		return ErrNoIPv4Address
	}
	c.Address = v4Addrs[0]
	if len(v4Addrs) > 1 {
		c.Notices = append(c.Notices, fmt.Sprintf(
			"Used the first tunnel address %s and ignored the rest.", c.Address))
	}

	v4Allowed, droppedAllowed := keepIPv4Prefixes(allowedIPs)
	if len(v4Allowed) == 0 {
		// wg-quick's own default. Say so, because it decides what the tunnel
		// carries and therefore what the kill switch drops.
		v4Allowed = []string{"0.0.0.0/0"}
		if len(allowedIPs) == 0 {
			c.Notices = append(c.Notices, "No allowed IPs were given, so the tunnel carries everything (0.0.0.0/0).")
		}
	}
	c.Peer.AllowedIPs = v4Allowed

	if droppedAddrs+droppedAllowed > 0 {
		c.Notices = append(c.Notices, "IPv6 entries were ignored — this router is IPv4 only.")
	}
	return nil
}

// setDNS keeps the first IPv4 resolver. A v6-only resolver is unreachable here,
// and falling back to the public default is better than a dead one.
func (c *WireGuardConfig) setDNS(values []string) {
	for _, v := range values {
		ip, err := netip.ParseAddr(v)
		if err != nil {
			continue // a search domain, not a resolver
		}
		if ip.Is4() {
			c.DNS = ip.String()
			return
		}
	}
	if len(values) > 0 {
		c.Notices = append(c.Notices, "IPv6 entries were ignored — this router is IPv4 only.")
	}
}

// keepIPv4Prefixes returns the IPv4 CIDRs and how many non-IPv4 ones it dropped.
func keepIPv4Prefixes(in []string) (out []string, dropped int) {
	for _, s := range in {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			// A bare address where a CIDR belongs is common enough to fix.
			if a, aerr := netip.ParseAddr(s); aerr == nil && a.Is4() {
				out = append(out, netip.PrefixFrom(a, 32).String())
				continue
			}
			dropped++
			continue
		}
		if !p.Addr().Is4() {
			dropped++
			continue
		}
		out = append(out, p.String())
	}
	return out, dropped
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stripComment drops a trailing "#" comment. No accepted value contains one.
func stripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}
