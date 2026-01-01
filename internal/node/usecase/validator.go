package usecase

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

var (
	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ErrInvalidDNSConfig wraps user-facing validation errors from DNS / FakeDNS
// settings so the HTTP layer can surface them as 400 instead of 500.
var ErrInvalidDNSConfig = errors.New("invalid dns config")

func ValidateOutbound(out *domain.Outbound) error {
	protocol := strings.ToLower(out.Protocol)

	// 1. Tag Validation
	if len(out.Tag) < 2 {
		return errors.New("tag must be at least 2 characters")
	}

	// sendThrough: IP, CIDR, or "origin"/"srcip" — a bare hostname fails at load
	if err := validateSendThrough(out.SendThrough); err != nil {
		return err
	}

	// 2. Protocols that don't use top-level address/port
	switch protocol {
	case "freedom":
		// no remote endpoint; just check its fragment/noise knobs
		return validateFreedomOutbound(out)
	case "blackhole", "loopback":
		return nil
	case "dns":
		// address lives in dns_settings, not top-level
		dns := out.GetDNSOutboundSettingsOrDefault()
		if dns.NonIPQuery != "" && !validNonIPQueryValues[dns.NonIPQuery] {
			return fmt.Errorf("invalid nonIPQuery %q: must be drop, skip, or reject", dns.NonIPQuery)
		}
		return nil
	case "wireguard":
		// uses peer endpoints, not top-level address/port
		wg := out.GetWireGuardSettingsOrDefault()
		if wg.SecretKey == "" {
			return errors.New("secret key is required for wireguard outbound")
		}
		if len(wg.Peers) == 0 {
			return errors.New("at least one peer is required for wireguard outbound")
		}
		for i, p := range wg.Peers {
			if p.PublicKey == "" {
				return fmt.Errorf("peer[%d]: public key is required", i)
			}
		}
		return nil
	}

	// 3. Proxy Protocol Validation (VMess, VLESS, Trojan, Socks, Shadowsocks, HTTP)

	// Address
	if out.Address == "" {
		return errors.New("address is required")
	}
	// Check if valid IP or Domain
	if net.ParseIP(out.Address) == nil {
		// Not an IP, assume domain. Check length/format loosely
		if len(out.Address) < 3 || (!strings.Contains(out.Address, ".") && out.Address != "localhost") {
			return errors.New("invalid address (must be IP or domain)")
		}
	}

	// Port
	if out.Port < 1 || out.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	// Protocol Specifics
	switch protocol {
	case "vmess":
		vm := out.GetVMessSettingsOrDefault()
		if vm.UUID != "" && !uuidRegex.MatchString(vm.UUID) {
			return errors.New("invalid VMess UUID format")
		}
	case "vless":
		vl := out.GetVLESSSettingsOrDefault()
		if vl.UUID != "" && !uuidRegex.MatchString(vl.UUID) {
			return errors.New("invalid VLESS UUID format")
		}
		// outbound flow: "", vision, or vision-udp443 (udp443 is outbound-only)
		switch vl.Flow {
		case "", "xtls-rprx-vision", "xtls-rprx-vision-udp443":
		default:
			return fmt.Errorf("unsupported vless outbound flow: %q", vl.Flow)
		}
		// encryption must match server: "none" or an ML-KEM form
		if enc := strings.TrimSpace(vl.Encryption); enc != "" && enc != "none" &&
			!strings.HasPrefix(enc, "mlkem768x25519plus.") {
			return fmt.Errorf("unsupported vless encryption: %q (use \"none\" or an mlkem768x25519plus.* value)", vl.Encryption)
		}
	case "shadowsocks":
		ss := out.GetShadowsocksSettingsOrDefault()
		if ss.Password == "" {
			return errors.New("password is required")
		}
		validMethods := map[string]bool{
			"2022-blake3-aes-128-gcm":       true,
			"2022-blake3-aes-256-gcm":       true,
			"2022-blake3-chacha20-poly1305": true,
			"aes-256-gcm":                   true,
			"aes-128-gcm":                   true,
			"chacha20-poly1305":             true,
			"xchacha20-poly1305":            true,
			"none":                          true,
			"plain":                         true,
		}
		if !validMethods[strings.ToLower(ss.Method)] {
			return errors.New("invalid or unsupported shadowsocks method")
		}
		// 2022 methods need a base64 key matching the cipher length
		if err := validateShadowsocks2022Key(ss.Method, ss.Password); err != nil {
			return err
		}
	case "trojan":
		ts := out.GetTrojanSettingsOrDefault()
		if ts.Password == "" {
			return errors.New("password is required for trojan outbound")
		}
	case "socks":
		// SOCKS validation
		s := out.GetSOCKSSettingsOrDefault()
		if s.Auth == "password" && (len(s.Accounts) == 0 || s.Accounts[0].User == "") {
			return errors.New("username is required for socks password auth")
		}
	}

	// reality outbound needs a base64url 32-byte publicKey, valid shortId, known fingerprint
	if strings.ToLower(out.Security) == "reality" {
		r := out.RealitySettings
		if r == nil || r.PublicKey == "" {
			return errors.New("reality outbound requires a publicKey")
		}
		if pk, err := base64.RawURLEncoding.DecodeString(r.PublicKey); err != nil || len(pk) != 32 {
			return errors.New("reality publicKey must be a base64url-encoded 32-byte x25519 key")
		}
		if r.ShortID != "" && !isValidShortID(r.ShortID) {
			return fmt.Errorf("reality shortId %q must be hex, even length, and ≤16 chars", r.ShortID)
		}
		if r.Fingerprint != "" && !validTLSFingerprints[strings.ToLower(r.Fingerprint)] {
			return fmt.Errorf("unknown reality fingerprint: %q", r.Fingerprint)
		}
	}

	// unknown TLS fingerprint kills xray at load; xray lowercases, so match case-insensitively
	if out.TLSSettings != nil && out.TLSSettings.Fingerprint != "" {
		if !validTLSFingerprints[strings.ToLower(out.TLSSettings.Fingerprint)] {
			return fmt.Errorf("unknown tls fingerprint: %q", out.TLSSettings.Fingerprint)
		}
	}

	return nil
}

// validateSendThrough allows empty, an IP, a CIDR, or "origin"/"srcip"; a bare hostname is rejected.
func validateSendThrough(st string) error {
	st = strings.TrimSpace(st)
	if st == "" {
		return nil
	}
	host := st
	if i := strings.Index(st, "/"); i >= 0 {
		host = st[:i]
	}
	if host == "origin" || host == "srcip" {
		return nil
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf(`sendThrough %q must be an IP, an IP CIDR, "origin", or "srcip"`, st)
	}
	return nil
}

// validateFreedomOutbound checks freedom's fragment (needs length+interval) and noise (type+packet).
func validateFreedomOutbound(out *domain.Outbound) error {
	fs := out.FreedomSettings
	if fs == nil {
		return nil
	}
	if f := fs.Fragment; f != nil {
		if strings.TrimSpace(f.Length) == "" {
			return errors.New(`freedom fragment requires a non-empty length (e.g. "100-200")`)
		}
		if strings.TrimSpace(f.Interval) == "" {
			return errors.New(`freedom fragment requires a non-empty interval (e.g. "10-20")`)
		}
	}
	for i, n := range fs.Noise {
		switch strings.ToLower(n.Type) {
		case "rand", "str", "hex", "base64":
		default:
			return fmt.Errorf("freedom noise[%d] type must be rand/str/hex/base64, got %q", i, n.Type)
		}
		if strings.TrimSpace(n.Packet) == "" {
			return fmt.Errorf("freedom noise[%d] requires a non-empty packet", i)
		}
	}
	return nil
}

var validQueryStrategies = map[string]bool{
	"UseIP": true, "UseIPv4": true, "UseIPv6": true, "UseSystem": true,
}

// dnsTagRegex matches xray-core inbound/outbound tags. Permits letters, digits,
// underscore, hyphen, dot, and colon (used in synthesized tags).
var dnsTagRegex = regexp.MustCompile(`^[a-zA-Z0-9_:.-]+$`)

// dnsErr wraps a validation message with ErrInvalidDNSConfig so the HTTP
// layer can map to 400 via errors.Is.
func dnsErr(format string, a ...interface{}) error {
	return fmt.Errorf("%w: "+format, append([]interface{}{ErrInvalidDNSConfig}, a...)...)
}

// ValidateDNSSettings validates a DNSSettings struct before persisting.
func ValidateDNSSettings(settings *domain.DNSSettings) error {
	if settings == nil {
		return nil
	}

	// QueryStrategy
	if settings.QueryStrategy != "" && !validQueryStrategies[settings.QueryStrategy] {
		return dnsErr("invalid query_strategy %q: must be UseIP, UseIPv4, UseIPv6, or UseSystem", settings.QueryStrategy)
	}

	// ClientIP
	if settings.ClientIP != "" && net.ParseIP(settings.ClientIP) == nil {
		return dnsErr("invalid client_ip %q: must be a valid IP address", settings.ClientIP)
	}

	// Top-level tag
	if settings.Tag != "" {
		if len(settings.Tag) > 100 {
			return dnsErr("tag must be at most 100 characters")
		}
		if !dnsTagRegex.MatchString(settings.Tag) {
			return dnsErr("invalid tag %q: only letters, digits, _ - . : allowed", settings.Tag)
		}
	}

	// Servers
	seenServerTags := make(map[string]int)
	for i, s := range settings.Servers {
		if s.Address == "" {
			return dnsErr("server[%d]: address is required", i)
		}
		if s.Port < 0 || s.Port > 65535 {
			return dnsErr("server[%d]: port must be 0-65535", i)
		}
		if s.QueryStrategy != "" && !validQueryStrategies[s.QueryStrategy] {
			return dnsErr("server[%d]: invalid query_strategy %q", i, s.QueryStrategy)
		}
		if s.ClientIP != "" && net.ParseIP(s.ClientIP) == nil {
			return dnsErr("server[%d]: invalid client_ip %q", i, s.ClientIP)
		}
		if s.Tag != "" {
			if len(s.Tag) > 100 {
				return dnsErr("server[%d]: tag must be at most 100 characters", i)
			}
			if !dnsTagRegex.MatchString(s.Tag) {
				return dnsErr("server[%d]: invalid tag %q: only letters, digits, _ - . : allowed", i, s.Tag)
			}
			if prev, dup := seenServerTags[s.Tag]; dup {
				return dnsErr("server[%d]: tag %q already used by server[%d]", i, s.Tag, prev)
			}
			seenServerTags[s.Tag] = i
		}
		for j, ip := range s.ExpectedIPs {
			if !isValidCIDROrGeoIP(ip) {
				return dnsErr("server[%d].expected_ips[%d]: invalid value %q", i, j, ip)
			}
		}
		for j, ip := range s.UnexpectedIPs {
			if !isValidCIDROrGeoIP(ip) {
				return dnsErr("server[%d].unexpected_ips[%d]: invalid value %q", i, j, ip)
			}
		}
	}

	// Hosts: keys are domain rules, values are string or []string. Each value
	// item must be a valid IP, a domain alias, or a #rcode marker.
	for key, val := range settings.Hosts {
		if strings.TrimSpace(key) == "" {
			return dnsErr("hosts: empty domain key")
		}
		switch v := val.(type) {
		case string:
			if !isValidHostsValue(v) {
				return dnsErr("hosts[%q]: invalid value %q (expected IP, domain, or #rcode)", key, v)
			}
		case []interface{}:
			for j, item := range v {
				str, ok := item.(string)
				if !ok {
					return dnsErr("hosts[%q][%d]: must be a string", key, j)
				}
				if !isValidHostsValue(str) {
					return dnsErr("hosts[%q][%d]: invalid value %q", key, j, str)
				}
			}
		case []string:
			for j, item := range v {
				if !isValidHostsValue(item) {
					return dnsErr("hosts[%q][%d]: invalid value %q", key, j, item)
				}
			}
		default:
			return dnsErr("hosts[%q]: value must be a string or array of strings", key)
		}
	}

	return nil
}

// isValidCIDROrGeoIP returns true for a value xray-core's IP-rule parser will
// accept: geoip: / ext: / ext-ip: prefix forms, an IP, or a CIDR. Honors the
// optional leading "!" reverse-match prefix from cutReversePrefix in
// xray-core HEAD common/geodata/rule_parser.go.
func isValidCIDROrGeoIP(s string) bool {
	for strings.HasPrefix(s, "!") {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "geoip:") {
		return len(s) > len("geoip:")
	}
	if strings.HasPrefix(s, "ext:") {
		// Format: ext:<file>:<code>
		return strings.Count(s[len("ext:"):], ":") >= 1 && len(s) > len("ext:")
	}
	if strings.HasPrefix(s, "ext-ip:") {
		return strings.Count(s[len("ext-ip:"):], ":") >= 1 && len(s) > len("ext-ip:")
	}
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	if net.ParseIP(s) != nil {
		return true
	}
	return false
}

// isValidHostsValue reports whether a hosts entry value is parseable by
// xray-core's HostsWrapper. Accepts a literal IP, a domain string (with or
// without the standard prefix forms supported by ParseDomainRule), or a
// `#<rcode>` shorthand for an RCode error reply.
func isValidHostsValue(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "#") {
		// xray reads strconv.Atoi on rest of string
		rest := v[1:]
		if rest == "" {
			return false
		}
		for _, r := range rest {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	if net.ParseIP(v) != nil {
		return true
	}
	// Anything else is treated as a domain. Strip prefix forms, then sanity
	// check the remainder is a plausible hostname.
	body := v
	for _, p := range []string{"domain:", "full:", "regexp:", "keyword:", "geosite:", "ext:", "ext-domain:", "dotless:"} {
		if strings.HasPrefix(body, p) {
			body = body[len(p):]
			break
		}
	}
	if body == "" {
		return false
	}
	// Allow letters/digits/_/-/./ for domain bodies (regex bodies allowed too).
	return strings.ContainsAny(body, ".") || strings.HasPrefix(v, "regexp:") || body == "localhost"
}

// ValidateFakeDNSPools validates a slice of FakeDNSPool entries before
// persisting. Mirrors xray-core's runtime guard (LRU size must fit the
// CIDR's address space).
func ValidateFakeDNSPools(pools []domain.FakeDNSPool) error {
	for i, p := range pools {
		ipPool := strings.TrimSpace(p.IPPool)
		if ipPool == "" {
			return dnsErr("pool[%d]: ip_pool is required", i)
		}
		_, ipnet, err := net.ParseCIDR(ipPool)
		if err != nil {
			return dnsErr("pool[%d]: invalid ip_pool %q: %v", i, ipPool, err)
		}
		ones, bits := ipnet.Mask.Size()
		rooms := bits - ones
		if rooms <= 0 {
			return dnsErr("pool[%d]: ip_pool %q has no host bits", i, ipPool)
		}
		if p.LRUSize <= 0 {
			return dnsErr("pool[%d]: lru_size must be > 0", i)
		}
		// xray-core rejects when math.Log2(lruSize) >= rooms. Mirror exactly:
		// 2^rooms is the address-space size; LRU must be strictly less.
		if rooms < 63 && p.LRUSize >= int64(1)<<rooms {
			return dnsErr("pool[%d]: lru_size %d is bigger than address space of %s (max %d)", i, p.LRUSize, ipPool, (int64(1)<<rooms)-1)
		}
	}
	return nil
}

var validNonIPQueryValues = map[string]bool{
	"": true, "drop": true, "skip": true, "reject": true,
}
