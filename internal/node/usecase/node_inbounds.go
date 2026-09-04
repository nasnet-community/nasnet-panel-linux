package usecase

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// transports xray still accepts for an inbound (h2/http and quic are gone).
// "" defaults to tcp; hysteria2 uses "" since its transport is wired separately.
var validInboundNetworks = map[string]bool{
	"": true, "tcp": true, "raw": true, "ws": true, "websocket": true,
	"grpc": true, "gun": true, "kcp": true, "mkcp": true,
	"xhttp": true, "splithttp": true, "httpupgrade": true,
}

// validTLSFingerprints mirrors xray-core's PresetFingerprints + ModernFingerprints
// + OtherFingerprints keys (transport/internet/tls/tls.go HEAD). Case-sensitive.
// Empty string is also accepted (treated as HelloChrome_Auto by xray).
var validTLSFingerprints = map[string]bool{
	"": true,
	// PresetFingerprints (short aliases)
	"chrome": true, "firefox": true, "safari": true, "ios": true, "android": true,
	"edge": true, "360": true, "qq": true,
	"random": true, "randomized": true, "randomizednoalpn": true, "unsafe": true,
	// ModernFingerprints
	"hellofirefox_99": true, "hellofirefox_102": true, "hellofirefox_105": true, "hellofirefox_120": true,
	"hellochrome_83": true, "hellochrome_87": true, "hellochrome_96": true, "hellochrome_100": true,
	"hellochrome_102": true, "hellochrome_106_shuffle": true, "hellochrome_120": true, "hellochrome_131": true,
	"helloios_13": true, "helloios_14": true,
	"helloedge_85": true, "helloedge_106": true,
	"hellosafari_16_0": true, "hello360_11_0": true, "helloqq_11_1": true,
	// OtherFingerprints
	"hellogolang": true, "hellorandomized": true, "hellorandomizedalpn": true, "hellorandomizednoalpn": true,
	"hellofirefox_auto": true, "hellofirefox_55": true, "hellofirefox_56": true, "hellofirefox_63": true, "hellofirefox_65": true,
	"hellochrome_auto": true, "hellochrome_58": true, "hellochrome_62": true, "hellochrome_70": true, "hellochrome_72": true,
	"helloios_auto": true, "helloios_11_1": true, "helloios_12_1": true,
	"helloandroid_11_okhttp": true,
	"helloedge_auto":         true,
	"hellosafari_auto":       true,
	"hello360_auto":          true, "hello360_7_5": true,
	"helloqq_auto":             true,
	"hellochrome_100_psk":      true,
	"hellochrome_112_psk_shuf": true, "hellochrome_114_padding_psk_shuf": true,
	"hellochrome_115_pq": true, "hellochrome_115_pq_psk": true, "hellochrome_120_pq": true,
}

// validateInbound rejects configs xray would refuse, so the admin gets a real
// error instead of a dead "xray failed to start".
func validateInbound(inbound *domain.Inbound) error {
	switch inbound.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "wireguard",
		"http", "socks", "mixed", "dokodemo-door", "hysteria2":
	default:
		return fmt.Errorf("unsupported inbound protocol: %q", inbound.Protocol)
	}

	// tag is required and "api" is reserved (the builder injects it)
	if strings.TrimSpace(inbound.Tag) == "" {
		return errors.New("inbound tag is required")
	}
	if inbound.Tag == "api" {
		return errors.New(`inbound tag "api" is reserved`)
	}
	if inbound.Port < 1 || inbound.Port > 65535 {
		return fmt.Errorf("inbound port must be 1..65535, got %d", inbound.Port)
	}
	if err := validatePortRange(inbound.PortRange); err != nil {
		return err
	}

	// hysteria2 sets its own transport in the builder, so skip the network check
	if inbound.Protocol != "hysteria2" && !validInboundNetworks[inbound.Network] {
		return fmt.Errorf("unsupported inbound transport %q (h2/http and quic were removed from xray-core)", inbound.Network)
	}

	// plain proxies read raw bytes: non-tcp framing breaks them, and Reality too (TLS is ok)
	switch inbound.Protocol {
	case "socks", "http", "dokodemo-door", "mixed":
		if inbound.Network != "" && inbound.Network != "tcp" && inbound.Network != "raw" {
			return fmt.Errorf("%s inbound only supports tcp transport, got %q", inbound.Protocol, inbound.Network)
		}
		if inbound.Security == "reality" {
			return fmt.Errorf("%s inbound does not support Reality security", inbound.Protocol)
		}
	}

	if inbound.Protocol == "vless" && inbound.VLESSSettings != nil {
		flow := inbound.VLESSSettings.Flow
		switch flow {
		case "", "xtls-rprx-vision":
			// allowed
		case "xtls-rprx-vision-udp443":
			return errors.New(`vless inbound flow "xtls-rprx-vision-udp443" is outbound-only; use "xtls-rprx-vision" on the inbound`)
		default:
			return fmt.Errorf("unsupported vless inbound flow: %q", flow)
		}
		if flow == "xtls-rprx-vision" {
			isTCPVision := (inbound.Network == "tcp" || inbound.Network == "raw") && (inbound.Security == "tls" || inbound.Security == "reality")
			isXHTTPVision := inbound.Network == "xhttp"
			if !isTCPVision && !isXHTTPVision {
				return errors.New("XTLS Vision flow requires TCP network with TLS/Reality security, or XHTTP network")
			}
		}
		if err := validateVLESSDecryption(inbound.VLESSSettings); err != nil {
			return err
		}
	}

	if inbound.Security == "reality" {
		if err := validateReality(inbound); err != nil {
			return err
		}
	}

	if inbound.Security == "tls" {
		t := inbound.TLSSettings
		hasCert := t != nil && len(t.Certificates) > 0
		if !hasCert {
			return errors.New("tls requires at least one certificate (managed reference or file/content)")
		}
	}

	// unknown TLS fingerprint kills xray at load; xray lowercases, so match case-insensitively
	if inbound.TLSSettings != nil && inbound.TLSSettings.Fingerprint != "" {
		if !validTLSFingerprints[strings.ToLower(inbound.TLSSettings.Fingerprint)] {
			return fmt.Errorf("unknown tls fingerprint: %q", inbound.TLSSettings.Fingerprint)
		}
	}

	if err := validateFallbacks(inbound); err != nil {
		return err
	}

	if inbound.Protocol == "shadowsocks" {
		if err := validateShadowsocks(inbound); err != nil {
			return err
		}
	}

	if inbound.Protocol == "wireguard" {
		if err := validateWireGuard(inbound); err != nil {
			return err
		}
	}

	if inbound.Protocol == "dokodemo-door" {
		if err := validateDokodemo(inbound); err != nil {
			return err
		}
	}

	if (inbound.Protocol == "socks" || inbound.Protocol == "mixed") && inbound.SOCKSSettings != nil {
		if inbound.SOCKSSettings.Auth == "password" && len(inbound.SOCKSSettings.Accounts) == 0 {
			return errors.New(`socks auth "password" requires at least one account`)
		}
	}

	if err := validateStreamSettings(inbound); err != nil {
		return err
	}
	if err := validateSniffing(inbound.SniffingSettings); err != nil {
		return err
	}

	return nil
}

// validatePortRange checks a PortList string: comma-separated ports or "a-b" ranges, each 1..65535, a ≤ b.
func validatePortRange(pr string) error {
	pr = strings.TrimSpace(pr)
	if pr == "" {
		return nil
	}
	for _, part := range strings.Split(pr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("port range %q has an empty segment", pr)
		}
		lo, hi := part, part
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, hi = strings.TrimSpace(bounds[0]), strings.TrimSpace(bounds[1])
		}
		loN, err1 := strconv.Atoi(lo)
		hiN, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || loN < 1 || hiN > 65535 || loN > hiN {
			return fmt.Errorf("invalid port range segment %q (want 1..65535, e.g. \"1000-2000\" or \"443\")", part)
		}
	}
	return nil
}

// ensureUniqueTag rejects a tag already used by another inbound on the node;
// duplicate tags stop xray from starting.
func (u *nodeUsecase) ensureUniqueTag(ctx context.Context, inbound *domain.Inbound) error {
	existing, err := u.nodeRepo.ListInboundsByNode(ctx, inbound.NodeID)
	if err != nil {
		return nil // don't block on a transient list error; push would surface it
	}
	for _, e := range existing {
		if e.ID != inbound.ID && e.Tag == inbound.Tag {
			return fmt.Errorf("an inbound with tag %q already exists on this node", inbound.Tag)
		}
	}
	return nil
}

// validateStreamSettings checks transport enums: tcp/raw headerType, xhttp/splithttp mode,
// and the httpupgrade "Host" header trap (use the dedicated host field).
func validateStreamSettings(inbound *domain.Inbound) error {
	ts := inbound.TransportSettings
	if ts == nil {
		return nil
	}
	if (inbound.Network == "tcp" || inbound.Network == "raw") && ts.HeaderType != "" {
		if ts.HeaderType != "none" && ts.HeaderType != "http" {
			return fmt.Errorf(`tcp headerType must be "none" or "http", got %q`, ts.HeaderType)
		}
	}
	if (inbound.Network == "xhttp" || inbound.Network == "splithttp") && ts.Mode != "" {
		switch ts.Mode {
		case "auto", "packet-up", "stream-up", "stream-one":
		default:
			return fmt.Errorf("xhttp mode must be auto/packet-up/stream-up/stream-one, got %q", ts.Mode)
		}
	}
	if inbound.Network == "httpupgrade" {
		for k := range ts.Headers {
			if strings.EqualFold(k, "Host") {
				return errors.New(`httpupgrade: set Host via the "host" field, not a "Host" custom header (xray rejects it)`)
			}
		}
	}
	return nil
}

// validateSniffing rejects destOverride values xray doesn't recognize.
func validateSniffing(s *domain.SniffingSettings) error {
	if s == nil {
		return nil
	}
	for _, p := range s.DestOverride {
		switch strings.ToLower(p) {
		case "http", "tls", "https", "ssl", "quic", "fakedns", "fakedns+others":
		default:
			return fmt.Errorf("unknown sniffing destOverride %q", p)
		}
	}
	return nil
}

// validateReality checks the REALITY server fields: base64url 32-byte privateKey,
// at least one serverName, a dest, xver ≤ 2, and a valid shortId.
func validateReality(inbound *domain.Inbound) error {
	r := inbound.RealitySettings
	if r == nil || r.PrivateKey == "" {
		return errors.New("reality requires a non-empty privateKey")
	}
	if pk, err := base64.RawURLEncoding.DecodeString(r.PrivateKey); err != nil || len(pk) != 32 {
		return errors.New("reality privateKey must be a base64url-encoded 32-byte x25519 key")
	}
	if len(r.ServerNames) == 0 {
		return errors.New("reality requires at least one serverName")
	}
	if strings.TrimSpace(r.Dest) == "" {
		return errors.New(`reality requires a "dest" target, e.g. "www.microsoft.com:443"`)
	}
	if r.Xver > 2 {
		return errors.New(`reality "xver" only accepts 0, 1, 2`)
	}
	if r.ShortID != "" && !isValidShortID(r.ShortID) {
		return fmt.Errorf("reality shortId %q must be hex, even length, and ≤16 chars", r.ShortID)
	}
	switch inbound.Network {
	case "tcp", "raw", "xhttp", "splithttp", "grpc":
	default:
		return fmt.Errorf("reality only supports tcp/xhttp/splithttp/grpc networks, got %q", inbound.Network)
	}
	return nil
}

// isValidShortID: hex, even length, ≤16 chars. Empty means "accept clients with no shortId".
func isValidShortID(s string) bool {
	if len(s) > 16 || len(s)%2 != 0 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// validateVLESSDecryption: only "none"/empty or an ML-KEM value is valid, and
// enabling decryption rules out fallbacks.
func validateVLESSDecryption(v *domain.VLESSSettings) error {
	dec := strings.TrimSpace(v.Decryption)
	if dec == "" || dec == "none" {
		return nil
	}
	if len(v.Fallbacks) > 0 {
		return errors.New(`vless "decryption" (encryption enabled) cannot be combined with fallbacks`)
	}
	if !strings.HasPrefix(dec, "mlkem768x25519plus.") {
		return fmt.Errorf("unsupported vless decryption: %q (use \"none\" or an mlkem768x25519plus.* value)", dec)
	}
	return nil
}

// validateFallbacks: each fallback needs a dest, a path starting with "/" (if set), and xver ≤ 2.
func validateFallbacks(inbound *domain.Inbound) error {
	var fbs []domain.Fallback
	switch inbound.Protocol {
	case "vless":
		if inbound.VLESSSettings != nil {
			fbs = inbound.VLESSSettings.Fallbacks
		}
	case "trojan":
		if inbound.TrojanSettings != nil {
			fbs = inbound.TrojanSettings.Fallbacks
		}
	default:
		return nil
	}
	for i, fb := range fbs {
		if fallbackDestEmpty(fb.Dest) {
			return fmt.Errorf("fallback[%d] requires a non-empty dest", i)
		}
		if fb.Path != "" && !strings.HasPrefix(fb.Path, "/") {
			return fmt.Errorf(`fallback[%d] path must start with "/"`, i)
		}
		if fb.Xver > 2 {
			return fmt.Errorf("fallback[%d] xver only accepts 0, 1, 2", i)
		}
	}
	return nil
}

func fallbackDestEmpty(d interface{}) bool {
	if d == nil {
		return true
	}
	if s, ok := d.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// validateShadowsocks checks the 2022-blake3 server key length via validateShadowsocks2022Key.
func validateShadowsocks(inbound *domain.Inbound) error {
	ss := inbound.GetShadowsocksSettingsOrDefault()
	return validateShadowsocks2022Key(ss.Method, ss.Password)
}

// validateShadowsocks2022Key checks a 2022-blake3 method carries a base64 key of
// the cipher's length (16 for aes-128, else 32). Non-2022 methods pass through.
func validateShadowsocks2022Key(method, password string) error {
	m := strings.ToLower(method)
	if !strings.HasPrefix(m, "2022-blake3-") {
		return nil
	}
	if password == "" {
		return errors.New("shadowsocks 2022 methods require a base64 server key (password)")
	}
	want := 32
	if strings.Contains(m, "aes-128") {
		want = 16
	}
	key, err := base64.StdEncoding.DecodeString(password)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(password)
	}
	if err != nil || len(key) != want {
		return fmt.Errorf("shadowsocks %s key must be base64 of exactly %d bytes", method, want)
	}
	return nil
}

// validateWireGuard: secretKey required, addresses must be single-host CIDRs
// (/32 or /128) or bare IPs, and reserved is empty or 3 bytes of 0..255.
func validateWireGuard(inbound *domain.Inbound) error {
	wg := inbound.WireGuardSettings
	if wg == nil || strings.TrimSpace(wg.SecretKey) == "" {
		return errors.New("wireguard requires a non-empty secretKey")
	}
	for i, addr := range wg.Endpoint {
		if addr == "" {
			return fmt.Errorf("wireguard address[%d] is empty", i)
		}
		if strings.Contains(addr, "/") {
			prefix, err := netip.ParsePrefix(addr)
			if err != nil {
				return fmt.Errorf("wireguard address[%d] %q is not a valid CIDR", i, addr)
			}
			want := prefix.Addr().BitLen()
			if prefix.Bits() != want {
				return fmt.Errorf("wireguard address[%d] %q must be /%d (single host)", i, addr, want)
			}
		} else {
			if _, err := netip.ParseAddr(addr); err != nil {
				return fmt.Errorf("wireguard address[%d] %q is not a valid IP", i, addr)
			}
		}
	}
	if len(wg.Reserved) != 0 && len(wg.Reserved) != 3 {
		return errors.New("wireguard reserved must be empty or exactly 3 bytes")
	}
	for i, b := range wg.Reserved {
		if b < 0 || b > 255 {
			return fmt.Errorf("wireguard reserved[%d]=%d out of range 0..255", i, b)
		}
	}
	return nil
}

// validateDokodemo: needs address+port or followRedirect, and every portMap value must be "host:port".
func validateDokodemo(inbound *domain.Inbound) error {
	dd := inbound.DokodemoSettings
	if dd == nil {
		return nil
	}
	if !dd.FollowRedirect && dd.Address == "" && len(dd.PortMap) == 0 {
		return errors.New("dokodemo-door requires an address+port or followRedirect")
	}
	for k, v := range dd.PortMap {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("dokodemo portMap[%q] must be a \"host:port\" string", k)
		}
		if _, _, err := net.SplitHostPort(s); err != nil {
			return fmt.Errorf("dokodemo portMap[%q] must be \"host:port\", got %q", k, s)
		}
	}
	return nil
}

// === Inbound Management ===

// AddInbound creates an inbound in DB and pushes it to Xray
func (u *nodeUsecase) AddInbound(ctx context.Context, inbound *domain.Inbound) error {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"node_id":  inbound.NodeID,
		"tag":      inbound.Tag,
		"protocol": inbound.Protocol,
		"port":     inbound.Port,
	}).Info("[AddInbound] Creating new inbound")

	if err := validateInbound(inbound); err != nil {
		return err
	}

	node, err := u.nodeRepo.GetNode(ctx, inbound.NodeID)
	if err != nil {
		log.WithError(err).Error("[AddInbound] Node not found")
		return ErrNodeNotFound
	}

	// reject duplicate tags up front (xray won't start with them)
	if err := u.ensureUniqueTag(ctx, inbound); err != nil {
		return err
	}

	// 1. Create DB Record
	if err := u.nodeRepo.CreateInbound(ctx, inbound); err != nil {
		log.WithError(err).Error("[AddInbound] Failed to create inbound in DB")
		return err
	}

	// 2. Push to Xray - via agent
	// For agent nodes, push full config. If push fails (e.g. port conflict),
	// roll back the DB record so the broken inbound doesn't trigger drift loops.
	if err := u.pushConfigToAgent(ctx, node); err != nil {
		log.WithError(err).Error("[AddInbound] Config push failed, rolling back DB record")
		if delErr := u.nodeRepo.DeleteInbound(ctx, inbound.ID); delErr != nil {
			log.WithError(delErr).Error("[AddInbound] Failed to roll back inbound from DB")
		}
		return fmt.Errorf("failed to apply inbound config on node (port may be in use): %w", err)
	}

	// Mirror the inbound's managed-cert reference into inbound_sni. Done after a
	// successful push (a rolled-back inbound leaves no link). Outside any tx.
	u.syncSNILink(ctx, inbound)
	u.notifyInboundsChanged(ctx)

	log.WithFields(map[string]interface{}{
		"inbound_id": inbound.ID,
		"tag":        inbound.Tag,
	}).Info("[AddInbound] Inbound created successfully")
	return nil
}

// syncSNILink keeps inbound_sni in step with an inbound's TLSSettings. An
// inbound serves at most one managed SNI cert, so clear then relink.
func (u *nodeUsecase) syncSNILink(ctx context.Context, in *domain.Inbound) {
	if err := u.sniUsecase.UnlinkInbound(ctx, in.ID); err != nil {
		logger.GetLogger().WithError(err).Warn("[syncSNILink] failed to clear old SNI link")
	}
	if in.Security != "tls" || in.TLSSettings == nil {
		return
	}
	for _, c := range in.TLSSettings.Certificates {
		if c.SNIId > 0 {
			if err := u.sniUsecase.LinkInbound(ctx, in.ID, c.SNIId, in.NodeID); err != nil {
				logger.GetLogger().WithError(err).Warn("[syncSNILink] failed to link inbound to SNI")
			}
			return
		}
	}
}

func (u *nodeUsecase) ListInbounds(ctx context.Context, nodeID uint) ([]*domain.Inbound, error) {
	return u.nodeRepo.ListInboundsByNode(ctx, nodeID)
}

func (u *nodeUsecase) GetInbound(ctx context.Context, id uint) (*domain.Inbound, error) {
	return u.nodeRepo.GetInbound(ctx, id)
}

func (u *nodeUsecase) ToggleInboundDisabled(ctx context.Context, id uint) (*domain.Inbound, error) {
	log := logger.GetLogger()

	if err := u.nodeRepo.ToggleInboundDisabled(ctx, id); err != nil {
		log.WithError(err).WithField("inbound_id", id).Error("[ToggleInboundDisabled] Failed to toggle")
		return nil, err
	}

	inbound, err := u.nodeRepo.GetInboundWithNode(ctx, id)
	if err != nil {
		return nil, err
	}

	log.WithFields(map[string]interface{}{
		"inbound_id":  id,
		"is_disabled": inbound.IsDisabled,
	}).Info("[ToggleInboundDisabled] Toggled inbound")
	u.notifyInboundsChanged(ctx)

	// Push updated config to agent
	if inbound.Node != nil {
		if pushErr := u.pushConfigToAgent(ctx, inbound.Node); pushErr != nil {
			log.WithError(pushErr).Warn("[ToggleInboundDisabled] Config push failed (will be retried by drift detection)")
		}
	}

	return inbound, nil
}

func (u *nodeUsecase) DeleteInbound(ctx context.Context, id uint) error {
	log := logger.GetLogger()
	log.WithField("inbound_id", id).Warn("[DeleteInbound] Deleting inbound")

	inbound, err := u.nodeRepo.GetInboundWithNode(ctx, id)
	if err != nil {
		log.WithError(err).WithField("inbound_id", id).Error("[DeleteInbound] Inbound not found")
		return err
	}

	// Check if any reverse proxy references this inbound's tag
	if inbound.Node != nil {
		refs, refErr := u.nodeRepo.ListReverseProxiesByReferencedTag(ctx, inbound.NodeID, inbound.Tag)
		if refErr == nil && len(refs) > 0 {
			return fmt.Errorf("cannot delete: inbound '%s' is referenced by reverse proxy '%s'", inbound.Tag, refs[0].Tag)
		}
	}

	// 1. Soft-delete associated accounts
	accounts, err := u.accountRepo.ListByInboundID(ctx, id)
	if err == nil {
		for _, account := range accounts {
			if err := u.accountRepo.Delete(ctx, account.ID); err != nil {
				log.WithError(err).Warnf("[DeleteInbound] Failed to soft-delete account %d", account.ID)
			}
		}
	} else {
		log.WithError(err).Warn("[DeleteInbound] Failed to list accounts for cleanup")
	}

	// 1b. Cascade delete hosts for this inbound
	if err := u.nodeRepo.DeleteHostsByInbound(ctx, id); err != nil {
		log.WithError(err).Error("[DeleteInbound] Failed to delete hosts for inbound")
		return fmt.Errorf("failed to cascade-delete hosts: %w", err)
	}

	// 2. Delete from DB
	if err := u.nodeRepo.DeleteInbound(ctx, id); err != nil {
		log.WithError(err).Error("[DeleteInbound] Failed to delete from DB")
		return err
	}

	// Drop any inbound_sni link so a deleted inbound stops pinning its domain.
	if err := u.sniUsecase.UnlinkInbound(ctx, id); err != nil {
		log.WithError(err).Warn("[DeleteInbound] Failed to clear SNI link")
	}

	// 3. Delete from Xray - via agent
	if inbound.Node != nil {
		// For agent nodes, push full config
		if err := u.pushConfigToAgent(ctx, inbound.Node); err != nil {
			log.Warnf("[DeleteInbound] Failed to push config to agent: %v", err)
		}
	}

	u.notifyInboundsChanged(ctx)

	log.WithFields(map[string]interface{}{
		"inbound_id": id,
		"tag":        inbound.Tag,
	}).Info("[DeleteInbound] Inbound deleted successfully")
	return nil
}

func (u *nodeUsecase) UpdateInbound(ctx context.Context, inbound *domain.Inbound) error {
	log := logger.GetLogger()
	if err := validateInbound(inbound); err != nil {
		return err
	}
	if err := u.ensureUniqueTag(ctx, inbound); err != nil {
		return err
	}

	// snapshot the row so we can roll back if the push fails — one bad edit would
	// otherwise wedge every later config push for the node
	prev, prevErr := u.nodeRepo.GetInbound(ctx, inbound.ID)

	// 1. Update DB
	if err := u.nodeRepo.UpdateInbound(ctx, inbound); err != nil {
		return err
	}

	// 2. Update Xray - via agent
	node, err := u.nodeRepo.GetNode(ctx, inbound.NodeID)
	if err == nil {
		if pushErr := u.pushConfigToAgent(ctx, node); pushErr != nil {
			// Roll back to the previous good state and restore the live config.
			if prevErr == nil && prev != nil {
				if rbErr := u.nodeRepo.UpdateInbound(ctx, prev); rbErr != nil {
					log.WithError(rbErr).Error("[UpdateInbound] Failed to roll back inbound after push failure")
				} else if rbPushErr := u.pushConfigToAgent(ctx, node); rbPushErr != nil {
					log.WithError(rbPushErr).Error("[UpdateInbound] Failed to re-push config after rollback")
				}
			}
			return fmt.Errorf("inbound reverted: failed to apply config on node: %w", pushErr)
		}
	}

	// Keep inbound_sni in step with the (possibly changed) cert reference.
	u.syncSNILink(ctx, inbound)
	u.notifyInboundsChanged(ctx)

	return nil
}

// === Inbound Synchronization ===

func (u *nodeUsecase) SyncInbounds(ctx context.Context, nodeID uint) (*SyncResult, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, ErrNodeNotFound
	}

	dbInbounds, err := u.nodeRepo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	if err := u.pushConfigToAgent(ctx, node); err != nil {
		return nil, fmt.Errorf("failed to push config to agent: %w", err)
	}
	u.notifyInboundsChanged(ctx)
	return &SyncResult{
		Restored: len(dbInbounds),
		Kept:     0,
		Imported: 0,
		Errors:   0,
	}, nil
}

func (u *nodeUsecase) DiscoverInbounds(ctx context.Context, nodeID uint) ([]*domain.Inbound, error) {
	if _, err := u.nodeRepo.GetNode(ctx, nodeID); err != nil {
		return nil, ErrNodeNotFound
	}

	// Discovery doesn't make sense (agent manages Xray config from DB)
	// Just return an empty list as they should use AddInbound to create new inbounds
	return []*domain.Inbound{}, nil
}

// SyncCertificatesFromNodes iterates all nodes and syncs their TLS certificates
func (u *nodeUsecase) SyncCertificatesFromNodes(ctx context.Context) (int, error) {
	// Agent should handle certificate reporting via heartbeat or separate channel.
	return 0, nil
}

// GetRealtimeUsers retrieves live users and their traffic stats from the node
func (u *nodeUsecase) GetRealtimeUsers(ctx context.Context, nodeID uint) ([]*domain.InboundUsers, error) {
	node, err := u.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	result := make([]*domain.InboundUsers, 0)

	// Map to store stats by email
	userStats := make(map[string]struct {
		Up   int64
		Down int64
	})

	// Fetch stats to find active users
	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	stats, err := client.GetXrayStats(ctx, false)
	if err != nil {
		// Log error but continue with empty stats
		logger.GetLogger().Warnf("GetRealtimeUsers: Failed to get stats from agent node %d: %v", nodeID, err)
	} else {
		// Merge Up and Down stats
		for email, up := range stats.UserUplink {
			s := userStats[email]
			s.Up = up
			userStats[email] = s
		}
		for email, down := range stats.UserDownlink {
			s := userStats[email]
			s.Down = down
			userStats[email] = s
		}
	}

	// Since we can't associate users to specific inbounds easily without DB mapping,
	// we will create one group "Active Users (Agent)" containing all users found in stats.

	users := make([]*domain.XrayUser, 0)
	for email, stat := range userStats {
		users = append(users, &domain.XrayUser{
			Email:    email,
			Uplink:   stat.Up,
			Downlink: stat.Down,
			Traffic:  stat.Up + stat.Down,
		})
	}

	result = append(result, &domain.InboundUsers{
		InboundTag: "agent-reported",
		Protocol:   "mixed",
		Port:       0,
		Users:      users,
	})

	return result, nil
}
