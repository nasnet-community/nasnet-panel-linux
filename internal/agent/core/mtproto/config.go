// Package mtproto implements the agent-side MTProto (telemt) protocol core: a
// userspace TCP relay with no nftables, tunnel IPs or certificates. Identity is
// email = secret, embedded directly in the rendered config. One `telemt` process
// is supervised per core_inbound; traffic is accounted by scraping telemt's
// loopback Prometheus metrics endpoint. When the node marks tunnel egress
// (router mode), telemt reaches Telegram through a loopback SOCKS5 relay whose
// sockets carry the mark (relay.go).
//
// This file is the pure config renderer. telemt inotify-reloads its
// [access.*] user set live, so the rendered config is split into a restart-
// sensitive "core" section and a hot-reloadable "access" section (see mtproto.go).
//
// Every key emitted here is a real telemt 3.4.x config key, verified against the
// binary's `general.config_strict` validator (which rejects unknown keys). Note
// in particular that there is NO `[access.user_modes]`: camouflage modes are set
// globally in [general.modes], and an emitted per-user table was silently
// ignored.
package mtproto

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mtprotoproto "github.com/nasnet-community/nasnet-panel-linux/internal/core/protocol/mtproto"
)

// Modes is the set of camouflage modes an inbound offers (shared with the hub).
type Modes = mtprotoproto.Modes

// Settings is the mtproto core_inbounds.settings blob. The definition is shared
// with the hub (which writes it and builds client links from the same modes) so
// the two cannot drift.
type Settings = mtprotoproto.Settings

// parseSettings unmarshals settings JSON and applies defaults.
func parseSettings(raw json.RawMessage) (Settings, error) {
	return mtprotoproto.ParseSettings(raw)
}

// User is one pushed credential rendered into [access.*]. Identity is Email
// (the hub-side email, falling back to username — resolved by the Core).
type User struct {
	Email   string
	Secret  string
	KLimit  int
	Enabled bool

	// ExpiresAt is the subscription's expiry, pushed so the relay stops the user
	// on its own even if the hub cannot reach the node. Zero => no expiry.
	ExpiresAt time.Time
	// RateLimitUpBps / RateLimitDownBps cap this user's throughput. 0 => fall
	// back to the inbound-wide default in Settings, and 0 there => unlimited.
	RateLimitUpBps   int64
	RateLimitDownBps int64
}

// RenderInput is everything the renderer needs for one telemt inbound.
type RenderInput struct {
	InboundID string
	Listen    string // bind address; "" or 0.0.0.0 => all interfaces
	Port      int    // public listen port
	// RelayAddr, when set, is the loopback SOCKS5 proxy telemt reaches Telegram
	// through ([[upstreams]] socks5): the marked relay, or Xray's inbound
	// (no user); empty => direct.
	RelayAddr string
	RelayUser string
	RelayPass string
	Dir       string // per-inbound config/runtime dir (absolute)
	Settings  Settings
	Users     []User
}

func (in RenderInput) configPath() string { return filepath.Join(in.Dir, "config.toml") }

func (in RenderInput) port() int {
	if in.Port != 0 {
		return in.Port
	}
	return 443
}

// bindIP returns the address telemt should bind, or "" for all interfaces. A
// wildcard is normalized away so the renderer emits the plain [server] port form
// (no [[server.listeners]]) in the common case.
func (in RenderInput) bindIP() string {
	ip := strings.TrimSpace(in.Listen)
	if ip == "" || ip == "0.0.0.0" || ip == "::" || ip == "*" {
		return ""
	}
	return ip
}

// metricsPort resolves the loopback Prometheus port (settings override, else the
// 14300+id scheme shared with the hub's port checks).
func (in RenderInput) metricsPort() int {
	_, metrics := in.Settings.LoopbackPortsAt(idOffset(in.InboundID))
	return metrics
}

// relayPort resolves the loopback egress relay port (settings override, else
// the 12300+id scheme).
func (in RenderInput) relayPort() int {
	relay, _ := in.Settings.LoopbackPortsAt(idOffset(in.InboundID))
	return relay
}

// RenderResult is the split config: Core is the restart-sensitive section
// ([general]/[server]/[censorship]/[[upstreams]]), Access is the hot-reloadable
// [access.*] section. telemt inotify-reloads Access live; only a Core change
// requires a process restart.
type RenderResult struct {
	Core   string
	Access string
}

// Config returns the full config.toml (Core followed by Access).
func (r RenderResult) Config() string { return r.Core + r.Access }

// Render builds the split config.toml for one telemt inbound. It returns an
// error when the requested settings are internally inconsistent: a SOCKS
// upstream is incompatible with middle-proxy / adtag, because telemt's
// middle-proxy sockets do not go through [[upstreams]] and would leave
// unmarked, or around Xray.
func Render(in RenderInput) (RenderResult, error) {
	s := in.Settings
	if in.RelayAddr != "" && (s.MiddleProxy || s.AdtagDefault != "") {
		return RenderResult{}, fmt.Errorf("mtproto: the Telegram middle proxy and ad tag need direct egress outside router mode")
	}
	useMiddleProxy := s.MiddleProxy
	// An ad tag only reaches Telegram through the middle proxy; emitting one in
	// direct mode configures a knob the relay ignores.
	adtag := s.AdtagDefault
	if !useMiddleProxy {
		adtag = ""
	}

	// --- Core (restart-sensitive) ---
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("[general]\n")
	w("use_middle_proxy = %t\n", useMiddleProxy)
	w("me_socks_kdf_policy = \"strict\"\n")
	w("log_level = %s\n", tomlString(logLevel(s.LogLevel)))
	w("disable_colors = true\n")
	if adtag != "" {
		w("ad_tag = %s\n", tomlString(adtag))
	}
	// EffectiveModes, not Modes: FakeTLS with no SNI domain configured has no
	// usable client secret, so the relay must not advertise it either.
	modes := s.EffectiveModes()
	w("\n[general.modes]\n")
	w("classic = %t\n", modes.Classic)
	w("secure = %t\n", modes.Secure)
	w("tls = %t\n", modes.TLS)
	w("\n[server]\n")
	w("port = %d\n", in.port())
	if ip := in.bindIP(); ip != "" {
		// A specific bind address is only expressible through the listener list;
		// [server] port stays as the advertised/default port.
		w("listeners = [{ ip = %s, port = %d }]\n", tomlString(ip), in.port())
	}
	if s.ListenBacklog > 0 {
		w("listen_backlog = %d\n", s.ListenBacklog)
	}
	w("metrics_listen = %s\n", tomlString(fmt.Sprintf("127.0.0.1:%d", in.metricsPort())))
	w("metrics_whitelist = [\"127.0.0.1/32\"]\n")
	w("[server.api]\n")
	w("enabled = false\n")
	if domains := s.TLSDomains(); len(domains) > 0 {
		w("\n[censorship]\n")
		w("tls_domain = %s\n", tomlString(domains[0]))
		if len(domains) > 1 {
			w("tls_domains = %s\n", tomlStringArray(domains))
		}
		w("unknown_sni_action = %s\n", tomlString(unknownSNIAction(s.UnknownSNIAction)))
		w("mask = %t\n", boolOr(s.Mask, true))
		if mh := strings.TrimSpace(s.MaskHost); mh != "" {
			w("mask_host = %s\n", tomlString(mh))
		}
		w("tls_emulation = %t\n", boolOr(s.TLSEmulation, true))
	}
	w("\n[[upstreams]]\n")
	if in.RelayAddr == "" {
		w("type = \"direct\"\n")
	} else {
		w("type = \"socks5\"\n")
		w("address = %s\n", tomlString(in.RelayAddr))
		if in.RelayUser != "" {
			w("username = %s\n", tomlString(in.RelayUser))
			w("password = %s\n", tomlString(in.RelayPass))
		}
	}

	// --- Access (hot-reloadable) ---
	var a strings.Builder
	aw := func(format string, args ...any) { fmt.Fprintf(&a, format, args...) }

	if s.UniqueIPWindowSecs > 0 {
		// Scalar, not a per-user table — telemt rejects a table here.
		aw("\n[access]\n")
		aw("user_max_unique_ips_window_secs = %d\n", s.UniqueIPWindowSecs)
	}
	aw("\n[access.users]\n")
	for _, u := range in.Users {
		aw("%s = %s\n", tomlString(u.Email), tomlString(u.Secret))
	}
	aw("\n[access.user_enabled]\n")
	for _, u := range in.Users {
		aw("%s = %t\n", tomlString(u.Email), u.Enabled)
	}
	aw("\n[access.user_max_unique_ips]\n")
	for _, u := range in.Users {
		aw("%s = %d\n", tomlString(u.Email), maxUniqueIPs(u.KLimit))
	}
	// Per-user ad tags are only emitted when an adtag is live (middle-proxy only).
	if adtag != "" {
		aw("\n[access.user_ad_tags]\n")
		for _, u := range in.Users {
			aw("%s = %s\n", tomlString(u.Email), tomlString(adtag))
		}
	}
	// Expiry is defence-in-depth: the hub also flips user_enabled, but a node
	// that loses hub contact still stops selling access past the paid date.
	var expiring []User
	for _, u := range in.Users {
		if !u.ExpiresAt.IsZero() {
			expiring = append(expiring, u)
		}
	}
	if len(expiring) > 0 {
		aw("\n[access.user_expirations]\n")
		for _, u := range expiring {
			// telemt wants an RFC 3339 datetime, unquoted (a TOML datetime).
			aw("%s = %s\n", tomlString(u.Email), u.ExpiresAt.UTC().Format(time.RFC3339))
		}
	}
	// Rate limits: the per-user value wins, else the inbound-wide default.
	type limit struct {
		email    string
		up, down int64
	}
	var limits []limit
	for _, u := range in.Users {
		up, down := u.RateLimitUpBps, u.RateLimitDownBps
		if up == 0 && down == 0 {
			up, down = s.RateLimitUpBps, s.RateLimitDownBps
		}
		if up > 0 || down > 0 {
			limits = append(limits, limit{u.Email, up, down})
		}
	}
	if len(limits) > 0 {
		aw("\n[access.user_rate_limits]\n")
		for _, l := range limits {
			aw("%s = { up_bps = %d, down_bps = %d }\n", tomlString(l.email), l.up, l.down)
		}
	}

	return RenderResult{Core: b.String(), Access: a.String()}, nil
}

// --- pure helpers ---

// maxUniqueIPs maps a pushed K-limit to telemt's user_max_unique_ips value.
// KLimit is written as-is (0 => unlimited); a nonsensical negative clamps to 0.
func maxUniqueIPs(k int) int {
	if k < 0 {
		return 0
	}
	return k
}

// logLevel validates the configured telemt log level, falling back to "normal".
func logLevel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug", "verbose", "normal", "silent":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "normal"
	}
}

// unknownSNIAction validates [censorship] unknown_sni_action. "accept" (proxy
// the stranger to the mask host) is the safe default: dropping is a DPI tell.
func unknownSNIAction(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "drop":
		return "drop"
	default:
		return "accept"
	}
}

// boolOr resolves an optional bool setting to its default when unset.
func boolOr(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// tomlString renders a TOML basic string: wrap in double quotes, escaping
// backslash and double-quote. Sufficient for domains, emails, hex secrets and
// adtags — none of which legitimately carry control characters.
func tomlString(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			// Any other control character would end the line or be rejected
			// by the TOML parser, taking the whole config (and live reload)
			// down with it.
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlStringArray renders a TOML array of basic strings.
func tomlStringArray(vs []string) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, tomlString(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// idOffset derives a small, stable per-inbound port offset from the inbound ID.
// A numeric ID is used directly (mod 1000); any other string is FNV-1a folded
// into [0,1000). Deterministic so metrics/relay ports are stable across restarts.
func idOffset(inboundID string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(inboundID)); err == nil {
		if n < 0 {
			n = -n
		}
		return n % 1000
	}
	var h uint32 = 2166136261
	for i := 0; i < len(inboundID); i++ {
		h ^= uint32(inboundID[i])
		h *= 16777619
	}
	return int(h % 1000)
}
