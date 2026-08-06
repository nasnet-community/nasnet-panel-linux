package xray

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// Router-mode outbound tags
const (
	TagDirectForeign  = "direct-foreign"
	TagDirectDomestic = "direct-domestic"
)

// OrderedConfig represents the Xray config with fields in the desired JSON output order.
// The order of fields in this struct determines the order of keys in the JSON output.
type OrderedConfig struct {
	Log              map[string]interface{}   `json:"log,omitempty"`
	API              map[string]interface{}   `json:"api,omitempty"`
	DNS              map[string]interface{}   `json:"dns,omitempty"`
	Routing          map[string]interface{}   `json:"routing,omitempty"`
	Policy           map[string]interface{}   `json:"policy,omitempty"`
	Inbounds         []map[string]interface{} `json:"inbounds,omitempty"`
	Outbounds        []map[string]interface{} `json:"outbounds,omitempty"`
	Transport        map[string]interface{}   `json:"transport,omitempty"`
	Stats            map[string]interface{}   `json:"stats"`
	Reverse          map[string]interface{}   `json:"reverse,omitempty"`
	FakeDNS          interface{}              `json:"fakeDns,omitempty"`
	Metrics          map[string]interface{}   `json:"metrics,omitempty"`
	Observatory      map[string]interface{}   `json:"observatory,omitempty"`
	BurstObservatory map[string]interface{}   `json:"burstObservatory,omitempty"`
}

// FullConfigBuilder builds a complete xray config.json from database entities
type FullConfigBuilder struct {
	node           *nodeDomain.Node
	inbounds       []*nodeDomain.Inbound
	outbounds      []*nodeDomain.Outbound
	routing        []*nodeDomain.RoutingRule
	users          map[string][]*User // InboundTag -> Users
	apiEnabled     bool
	apiPort        int
	reverseProxies []*nodeDomain.ReverseProxy
	balancing      []*nodeDomain.BalancingRule
	routerMode     bool
}

// NewFullConfigBuilder creates a new config builder
func NewFullConfigBuilder(node *nodeDomain.Node) *FullConfigBuilder {
	// Convert value slices to pointer slices
	inbounds := make([]*nodeDomain.Inbound, len(node.Inbounds))
	for i := range node.Inbounds {
		inbounds[i] = &node.Inbounds[i]
	}
	outbounds := make([]*nodeDomain.Outbound, len(node.Outbounds))
	for i := range node.Outbounds {
		outbounds[i] = &node.Outbounds[i]
	}
	routing := make([]*nodeDomain.RoutingRule, len(node.RoutingRules))
	for i := range node.RoutingRules {
		routing[i] = &node.RoutingRules[i]
	}

	return &FullConfigBuilder{
		node:       node,
		inbounds:   inbounds,
		outbounds:  outbounds,
		routing:    routing,
		users:      make(map[string][]*User),
		apiEnabled: true,
		apiPort:    10085,
	}
}

// WithRouterMode emits the per group direct outbounds
func (b *FullConfigBuilder) WithRouterMode(enabled bool) *FullConfigBuilder {
	b.routerMode = enabled
	return b
}

// WithUsers sets the users for the inbounds
func (b *FullConfigBuilder) WithUsers(users map[string][]*User) *FullConfigBuilder {
	b.users = users
	return b
}

// WithInbounds sets the inbounds for the config
func (b *FullConfigBuilder) WithInbounds(inbounds []*nodeDomain.Inbound) *FullConfigBuilder {
	b.inbounds = inbounds
	return b
}

// WithOutbounds sets the outbounds for the config
func (b *FullConfigBuilder) WithOutbounds(outbounds []*nodeDomain.Outbound) *FullConfigBuilder {
	b.outbounds = outbounds
	return b
}

// WithRoutingRules sets the routing rules for the config
func (b *FullConfigBuilder) WithRoutingRules(rules []*nodeDomain.RoutingRule) *FullConfigBuilder {
	b.routing = rules
	return b
}

// WithAPI enables/disables the API and sets port
func (b *FullConfigBuilder) WithAPI(enabled bool, port int) *FullConfigBuilder {
	b.apiEnabled = enabled
	b.apiPort = port
	return b
}

// WithReverseProxies sets the reverse proxy entries for the config
func (b *FullConfigBuilder) WithReverseProxies(rps []*nodeDomain.ReverseProxy) *FullConfigBuilder {
	b.reverseProxies = rps
	return b
}

// WithBalancingRules sets the balancing rules for the config
func (b *FullConfigBuilder) WithBalancingRules(rules []*nodeDomain.BalancingRule) *FullConfigBuilder {
	b.balancing = rules
	return b
}

// buildReverse creates the reverse proxy configuration
func (b *FullConfigBuilder) buildReverse() map[string]interface{} {
	if len(b.reverseProxies) == 0 {
		return nil
	}

	var bridges []map[string]interface{}
	var portals []map[string]interface{}

	for _, rp := range b.reverseProxies {
		entry := map[string]interface{}{
			"tag":    rp.Tag,
			"domain": rp.Domain,
		}
		if rp.Type == "bridge" {
			bridges = append(bridges, entry)
		} else {
			portals = append(portals, entry)
		}
	}

	result := map[string]interface{}{}
	if len(bridges) > 0 {
		result["bridges"] = bridges
	}
	if len(portals) > 0 {
		result["portals"] = portals
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// Build generates the complete config.json as a string with ordered keys
func (b *FullConfigBuilder) Build() (string, error) {
	config := b.buildOrderedConfig()

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	return string(jsonBytes), nil
}

// buildOrderedConfig creates the full config structure with ordered fields for JSON output
func (b *FullConfigBuilder) buildOrderedConfig() *OrderedConfig {
	logConfig := map[string]interface{}{
		"loglevel": b.getLogLevel(),
		"dnsLog":   b.node.LogDNS,
	}
	// Set access log path: explicit LogAccess takes priority, then EnableAccessLog uses default
	if accessPath := b.node.GetAccessLogPath(); accessPath != "" {
		logConfig["access"] = accessPath
	}
	if b.node.LogError != "" {
		logConfig["error"] = b.node.LogError
	}

	config := &OrderedConfig{
		Log:         logConfig,
		Routing:     b.buildRouting(),
		Policy:      b.buildPolicy(),
		Inbounds:    b.buildInbounds(),
		Outbounds:   b.buildOutbounds(),
		Stats:       map[string]interface{}{},
		Observatory: b.buildObservatory(),
	}

	// Add API if enabled
	if b.apiEnabled {
		config.API = map[string]interface{}{
			"tag":      "api",
			"services": []string{"HandlerService", "LoggerService", "StatsService", "RoutingService"},
		}
	}

	// Add DNS if configured
	config.DNS = b.buildDNS()

	// Add FakeDNS if configured
	config.FakeDNS = b.buildFakeDNS()

	// Add Reverse if configured
	config.Reverse = b.buildReverse()

	return config
}

// buildPolicy creates the policy configuration with all bandwidth tier levels
func (b *FullConfigBuilder) buildPolicy() map[string]interface{} {
	levels := map[string]interface{}{}
	for _, tier := range bandwidth.AllTiers() {
		levels[strconv.Itoa(int(tier.Level))] = map[string]interface{}{
			"statsUserUplink":   true,
			"statsUserDownlink": true,
			"statsUserOnline":   true,
		}
	}
	return map[string]interface{}{
		"levels": levels,
		"system": map[string]interface{}{
			"statsInboundUplink":    true,
			"statsInboundDownlink":  true,
			"statsOutboundUplink":   true,
			"statsOutboundDownlink": true,
		},
	}
}

// buildInbounds creates the inbounds array
func (b *FullConfigBuilder) buildInbounds() []map[string]interface{} {
	inbounds := []map[string]interface{}{}

	// Add API inbound if enabled
	if b.apiEnabled {
		inbounds = append(inbounds, map[string]interface{}{
			"tag":      "api",
			"listen":   "127.0.0.1",
			"port":     b.apiPort,
			"protocol": "dokodemo-door",
			"settings": map[string]interface{}{
				"address": "127.0.0.1",
			},
		})
	}

	// Convert domain inbounds to config format
	for _, inb := range b.inbounds {
		if inb == nil {
			continue
		}
		inbounds = append(inbounds, b.convertInbound(inb))
	}

	return inbounds
}

// convertInbound converts a domain.Inbound to config format
func (b *FullConfigBuilder) convertInbound(inb *nodeDomain.Inbound) map[string]interface{} {
	// The panel's internal id is "hysteria2"
	protocolName := inb.Protocol
	if protocolName == "hysteria2" {
		protocolName = "hysteria"
	}
	cfg := map[string]interface{}{
		"tag":      inb.Tag,
		"protocol": protocolName,
	}
	// PortRange (e.g. "1000-2000" or "80,443")
	if inb.PortRange != "" {
		cfg["port"] = inb.PortRange
	} else {
		cfg["port"] = inb.Port
	}

	// Listen address
	if inb.Listen != "" {
		cfg["listen"] = inb.Listen
	} else {
		cfg["listen"] = "0.0.0.0"
	}

	// Settings based on protocol
	cfg["settings"] = b.buildInboundSettings(inb)

	switch inb.Protocol {
	case "hysteria2":
		// This xray-core models Hysteria2 as a TRANSPORT: streamSettings needs
		// network "hysteria" + hysteriaSettings{version:2,...}
		//TLS is mandatory
		hysteriaSettings := map[string]interface{}{"version": 2}
		hy := inb.GetHysteriaSettingsOrDefault()
		if hy.UdpIdleTimeout >= 2 && hy.UdpIdleTimeout <= 600 {
			hysteriaSettings["udpIdleTimeout"] = hy.UdpIdleTimeout
		}
		stream := map[string]interface{}{
			"network":          "hysteria",
			"security":         "tls",
			"hysteriaSettings": hysteriaSettings,
		}
		tlsSettings := inb.GetTLSSettingsOrDefault()
		if len(tlsSettings.Certificates) > 0 || tlsSettings.ServerName != "" {
			tlsMap := b.buildTLSSettings(tlsSettings)
			// Hysteria2 runs HTTP/3 over QUIC so the TLS ALPN MUST be exactly "h3"
			tlsMap["alpn"] = []string{"h3"}
			stream["tlsSettings"] = tlsMap
		}

		// Packet masking (finalmask, e.g. the salamander UDP mask)
		if inb.SockoptSettings != nil {
			stream["sockopt"] = b.buildSockoptSettings(inb.GetSockoptSettingsOrDefault())
		}
		if inb.FinalMask != nil {
			if fm := buildFinalMaskMap(inb.FinalMask); len(fm) > 0 {
				stream["finalmask"] = fm
			}
		}
		cfg["streamSettings"] = stream
	case "socks", "http", "dokodemo-door", "mixed":
		// Raw proxies read plaintext bytes off the wire, so non-tcp transports
		// (ws/grpc/xhttp framing) break them — force tcp. But TLS termination
		// IS valid (e.g. an HTTPS proxy), so honor tls security when set.
		stream := map[string]interface{}{
			"network":  "tcp",
			"security": "none",
		}
		if inb.Security == "tls" {
			stream["security"] = "tls"
			tlsSettings := inb.GetTLSSettingsOrDefault()
			if len(tlsSettings.Certificates) > 0 || tlsSettings.ServerName != "" {
				stream["tlsSettings"] = b.buildTLSSettings(tlsSettings)
			}
		}
		cfg["streamSettings"] = stream
	default:
		cfg["streamSettings"] = b.buildInboundStreamSettings(inb)
	}

	// Sniffing — skip for protocols that don't carry user TCP/UDP traffic
	// in a way xray can sniff (wireguard tunnels its own protocol; dokodemo
	// already specifies the destination; hysteria2 multiplexes QUIC).
	switch inb.Protocol {
	case "wireguard", "dokodemo-door", "hysteria2":
		// no-op
	default:
		sniffing := inb.GetSniffingSettingsOrDefault()
		sniffCfg := map[string]interface{}{
			"enabled":      sniffing.Enabled,
			"destOverride": sniffing.DestOverride,
			"metadataOnly": sniffing.MetadataOnly,
			"routeOnly":    sniffing.RouteOnly,
		}
		if len(sniffing.DomainsExcluded) > 0 {
			sniffCfg["domainsExcluded"] = sniffing.DomainsExcluded
		}
		if len(sniffing.IPsExcluded) > 0 {
			sniffCfg["ipsExcluded"] = sniffing.IPsExcluded
		}
		cfg["sniffing"] = sniffCfg
	}

	// Sockopt - Moved to streamSettings

	return cfg
}

// buildInboundSettings creates protocol-specific settings
func (b *FullConfigBuilder) buildInboundSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	switch inb.Protocol {
	case "vless":
		// Get default settings
		vlessSettings := inb.GetVLESSSettingsOrDefault()

		settings := map[string]interface{}{
			"clients":    []interface{}{},
			"decryption": "none",
		}
		if vlessSettings.Decryption != "" {
			settings["decryption"] = vlessSettings.Decryption
		}
		// Encryption is only for client (outbound)

		if users, ok := b.users[inb.Tag]; ok {
			clients := make([]interface{}, 0, len(users))
			for _, u := range users {
				// Per-user flow wins; falls back to the inbound default.
				flow := u.Flow
				if flow == "" {
					flow = vlessSettings.Flow
				}

				clientMap := map[string]interface{}{
					"id":    u.UUID,
					"flow":  flow,
					"email": u.Email,
					"level": u.Level,
				}
				clients = append(clients, clientMap)
			}
			settings["clients"] = clients
		}
		// Fallbacks
		if len(vlessSettings.Fallbacks) > 0 {
			settings["fallbacks"] = b.buildFallbacks(vlessSettings.Fallbacks)
		}
		return settings

	case "vmess":
		settings := map[string]interface{}{
			"clients": []interface{}{},
		}
		if users, ok := b.users[inb.Tag]; ok {
			clients := make([]interface{}, 0, len(users))
			for _, u := range users {
				clients = append(clients, map[string]interface{}{
					"id":      u.UUID,
					"alterId": u.AlterId,
					"email":   u.Email,
					"level":   u.Level,
				})
			}
			settings["clients"] = clients
		}
		return settings

	case "trojan":
		trojanSettings := inb.GetTrojanSettingsOrDefault()
		settings := map[string]interface{}{
			"clients": []interface{}{},
		}
		if users, ok := b.users[inb.Tag]; ok {
			clients := make([]interface{}, 0, len(users))
			for _, u := range users {
				clients = append(clients, map[string]interface{}{
					"password": u.UUID, // Trojan uses password, mapped from UUID/Pass
					"email":    u.Email,
					"level":    u.Level,
				})
			}
			settings["clients"] = clients
		}
		// Fallbacks
		if len(trojanSettings.Fallbacks) > 0 {
			settings["fallbacks"] = b.buildFallbacks(trojanSettings.Fallbacks)
		}
		return settings

	case "shadowsocks":
		return b.buildShadowsocksSettings(inb)

	case "wireguard":
		return b.buildWireGuardSettings(inb)

	case "http":
		return b.buildHTTPInboundSettings(inb)

	case "socks", "mixed":
		// "mixed" is SOCKS+HTTP on one port; same settings shape as SOCKS
		// (xray loads it via SocksServerConfig).
		return b.buildSOCKSSettings(inb)

	case "dokodemo-door":
		return b.buildDokodemoDoorSettings(inb)

	case "hysteria2":
		return b.buildHysteriaInboundSettings(inb)

	default:
		return map[string]interface{}{}
	}
}

// buildInboundStreamSettings creates stream settings for an inbound
func (b *FullConfigBuilder) buildInboundStreamSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	// Default to tcp if empty
	network := inb.Network
	if network == "" {
		network = "tcp"
	}

	// Default to none if empty
	security := inb.Security
	if security == "" {
		security = "none"
	}

	// Check if reality is actually configured (has privateKey), otherwise fall back to none
	if security == "reality" {
		realitySettings := inb.GetRealitySettingsOrDefault()
		if realitySettings.PrivateKey == "" {
			security = "none"
		}
		// REALITY only supports tcp, splithttp/xhttp, grpc
		if security == "reality" && network != "tcp" && network != "xhttp" && network != "splithttp" && network != "grpc" {
			security = "none"
		}
	}

	// Check if TLS is actually configured (has certificates), otherwise fall back to none
	if security == "tls" {
		tlsSettings := inb.GetTLSSettingsOrDefault()
		if len(tlsSettings.Certificates) == 0 && tlsSettings.ServerName == "" {
			security = "none"
		}
	}

	stream := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	// Network-specific settings
	switch network {
	case "tcp":
		stream["tcpSettings"] = b.buildTCPSettings(inb.GetTransportSettingsOrDefault())
	case "ws":
		stream["wsSettings"] = b.buildWSSettings(inb.GetTransportSettingsOrDefault())
	case "grpc":
		stream["grpcSettings"] = b.buildGRPCSettings(inb.GetTransportSettingsOrDefault())
	case "xhttp", "splithttp":
		stream["xhttpSettings"] = b.buildXHTTPSettings(inb.GetTransportSettingsOrDefault())
	case "httpupgrade":
		stream["httpupgradeSettings"] = b.buildHTTPUpgradeSettings(inb.GetTransportSettingsOrDefault())
	case "kcp":
		stream["kcpSettings"] = b.buildKCPSettings(inb.GetTransportSettingsOrDefault())
	}

	// Security settings
	switch security {
	case "tls":
		stream["tlsSettings"] = b.buildTLSSettings(inb.GetTLSSettingsOrDefault())
	case "reality":
		stream["realitySettings"] = b.buildRealityInboundSettings(inb.GetRealitySettingsOrDefault())
	}

	// Sockopt
	if inb.SockoptSettings != nil {
		stream["sockopt"] = b.buildSockoptSettings(inb.GetSockoptSettingsOrDefault())
	}

	if inb.FinalMask != nil {
		fm := buildFinalMaskMap(inb.FinalMask)
		if len(fm) > 0 {
			stream["finalmask"] = fm
		}
	}

	return stream
}

// buildTCPSettings creates TCP stream settings
func (b *FullConfigBuilder) buildTCPSettings(ts *nodeDomain.TransportSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if ts.HeaderType != "" && ts.HeaderType != "none" {
		settings["header"] = map[string]interface{}{
			"type": ts.HeaderType,
		}
		if ts.Path != "" {
			settings["header"].(map[string]interface{})["request"] = map[string]interface{}{
				"path": []string{ts.Path},
			}
		}
	}
	if ts.AcceptProxyProtocol {
		settings["acceptProxyProtocol"] = true
	}

	return settings
}

// buildWSSettings creates WebSocket stream settings
func (b *FullConfigBuilder) buildWSSettings(ts *nodeDomain.TransportSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if ts.Path != "" {
		settings["path"] = ts.Path
	}
	// xray-core has a dedicated "host" field; stuffing it into headers["Host"]
	// triggers a deprecation path. Emit host separately, custom headers alone.
	if ts.Host != "" {
		settings["host"] = ts.Host
	}
	if len(ts.Headers) > 0 {
		headers := map[string]interface{}{}
		for k, v := range ts.Headers {
			headers[k] = v
		}
		settings["headers"] = headers
	}
	if ts.HeartbeatPeriod > 0 {
		settings["heartbeatPeriod"] = ts.HeartbeatPeriod
	}
	if ts.AcceptProxyProtocol {
		settings["acceptProxyProtocol"] = true
	}

	return settings
}

// buildGRPCSettings creates gRPC stream settings
func (b *FullConfigBuilder) buildGRPCSettings(ts *nodeDomain.TransportSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if ts.ServiceName != "" {
		settings["serviceName"] = ts.ServiceName
	}
	if ts.GRPCAuthority != "" {
		settings["authority"] = ts.GRPCAuthority
	}
	if ts.GRPCMultiMode {
		settings["multiMode"] = true
	}
	if ts.GRPCIdleTimeout > 0 {
		settings["idle_timeout"] = ts.GRPCIdleTimeout
	}
	if ts.GRPCHealthCheckTimeout > 0 {
		settings["health_check_timeout"] = ts.GRPCHealthCheckTimeout
	}
	if ts.GRPCPermitWithoutStream {
		settings["permit_without_stream"] = true
	}
	if ts.GRPCInitialWindowsSize > 0 {
		settings["initial_windows_size"] = ts.GRPCInitialWindowsSize
	}
	if ts.UserAgent != "" {
		settings["user_agent"] = ts.UserAgent
	}

	return settings
}

// buildXHTTPSettings creates XHTTP/SplitHTTP stream settings
func (b *FullConfigBuilder) buildXHTTPSettings(ts *nodeDomain.TransportSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if ts.Path != "" {
		settings["path"] = ts.Path
	}
	if ts.Host != "" {
		settings["host"] = ts.Host
	}
	if ts.Mode != "" {
		settings["mode"] = ts.Mode
	}
	if len(ts.Headers) > 0 {
		settings["headers"] = ts.Headers
	}
	if ts.NoSSEHeader {
		settings["noSSEHeader"] = true
	}
	if ts.NoGRPCHeader {
		settings["noGRPCHeader"] = true
	}
	if ts.XPaddingBytes != nil {
		settings["xPaddingBytes"] = xrayRange(ts.XPaddingBytes)
	}
	if ts.ScMaxEachPostBytes != nil {
		settings["scMaxEachPostBytes"] = xrayRange(ts.ScMaxEachPostBytes)
	}
	if ts.ScMinPostsIntervalMs != nil {
		settings["scMinPostsIntervalMs"] = xrayRange(ts.ScMinPostsIntervalMs)
	}
	if ts.ScMaxBufferedPosts > 0 {
		settings["scMaxBufferedPosts"] = ts.ScMaxBufferedPosts
	}
	if ts.ScStreamUpServerSecs != nil {
		settings["scStreamUpServerSecs"] = xrayRange(ts.ScStreamUpServerSecs)
	}
	if ts.Xmux != nil {
		xmux := map[string]interface{}{}
		// xray-core rejects configs that set both maxConcurrency and
		// maxConnections. When both present, prefer maxConcurrency
		// (the more common knob).
		hasConcurrency := ts.Xmux.MaxConcurrency != nil && ts.Xmux.MaxConcurrency.To > 0
		hasConnections := ts.Xmux.MaxConnections != nil && ts.Xmux.MaxConnections.To > 0
		if hasConcurrency {
			xmux["maxConcurrency"] = xrayRange(ts.Xmux.MaxConcurrency)
		} else if hasConnections {
			xmux["maxConnections"] = xrayRange(ts.Xmux.MaxConnections)
		}
		if ts.Xmux.CMaxReuseTimes != nil {
			xmux["cMaxReuseTimes"] = xrayRange(ts.Xmux.CMaxReuseTimes)
		}
		if ts.Xmux.HMaxRequestTimes != nil {
			xmux["hMaxRequestTimes"] = xrayRange(ts.Xmux.HMaxRequestTimes)
		}
		if ts.Xmux.HMaxReusableSecs != nil {
			xmux["hMaxReusableSecs"] = xrayRange(ts.Xmux.HMaxReusableSecs)
		}
		if ts.Xmux.HKeepAlivePeriod > 0 {
			xmux["hKeepAlivePeriod"] = ts.Xmux.HKeepAlivePeriod
		}
		if len(xmux) > 0 {
			settings["xmux"] = xmux
		}
	}
	if ts.Extra != "" {
		// xray-core parses "extra" as a SplitHTTPConfig object; a raw string
		// fails json.Unmarshal and bricks the config. Only emit when it parses
		// as an object; otherwise drop it rather than break the whole config.
		var extraMap map[string]interface{}
		if err := json.Unmarshal([]byte(ts.Extra), &extraMap); err == nil {
			settings["extra"] = extraMap
		}
	}
	if ts.XPaddingObfsMode {
		settings["xPaddingObfsMode"] = true
	}
	if ts.XPaddingKey != "" {
		settings["xPaddingKey"] = ts.XPaddingKey
	}
	if ts.XPaddingHeader != "" {
		settings["xPaddingHeader"] = ts.XPaddingHeader
	}
	if ts.XPaddingPlacement != "" {
		settings["xPaddingPlacement"] = ts.XPaddingPlacement
	}
	if ts.XPaddingMethod != "" {
		settings["xPaddingMethod"] = ts.XPaddingMethod
	}
	if ts.UplinkHTTPMethod != "" {
		settings["uplinkHTTPMethod"] = ts.UplinkHTTPMethod
	}
	if ts.SessionPlacement != "" {
		settings["sessionPlacement"] = ts.SessionPlacement
	}
	if ts.SessionKey != "" {
		settings["sessionKey"] = ts.SessionKey
	}
	if ts.SeqPlacement != "" {
		settings["seqPlacement"] = ts.SeqPlacement
	}
	if ts.SeqKey != "" {
		settings["seqKey"] = ts.SeqKey
	}
	if ts.UplinkDataPlacement != "" {
		settings["uplinkDataPlacement"] = ts.UplinkDataPlacement
	}
	if ts.UplinkDataKey != "" {
		settings["uplinkDataKey"] = ts.UplinkDataKey
	}
	if ts.UplinkChunkSize != nil {
		settings["uplinkChunkSize"] = xrayRange(ts.UplinkChunkSize)
	}
	if ts.ServerMaxHeaderBytes > 0 {
		settings["serverMaxHeaderBytes"] = ts.ServerMaxHeaderBytes
	}

	return settings
}

// buildHTTPUpgradeSettings creates HTTP Upgrade stream settings
func (b *FullConfigBuilder) buildHTTPUpgradeSettings(ts *nodeDomain.TransportSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if ts.Path != "" {
		settings["path"] = ts.Path
	}
	if ts.Host != "" {
		settings["host"] = ts.Host
	}
	if len(ts.Headers) > 0 {
		settings["headers"] = ts.Headers
	}
	if ts.AcceptProxyProtocol {
		settings["acceptProxyProtocol"] = true
	}

	return settings
}

// buildTLSSettings creates TLS security settings
func (b *FullConfigBuilder) buildTLSSettings(tls *nodeDomain.TLSSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if tls.ServerName != "" {
		settings["serverName"] = tls.ServerName
	}
	if len(tls.ALPN) > 0 {
		settings["alpn"] = tls.ALPN
	}
	if tls.Fingerprint != "" {
		settings["fingerprint"] = tls.Fingerprint
	}
	// NOTE: "allowInsecure" was removed from xray-core and, after 2026-06-01,
	// emitting it makes the whole config fail to load
	// (infra/conf/transport_internet.go PrintRemovedFeatureError). Deliberately
	// not emitted; use pinnedPeerCertSha256 / verifyPeerCertByName instead.
	if tls.RejectUnknownSNI {
		settings["rejectUnknownSni"] = true
	}
	if tls.EnableSessionResumption {
		settings["enableSessionResumption"] = true
	}
	if tls.DisableSystemRoot {
		settings["disableSystemRoot"] = true
	}
	if tls.MinVersion != "" {
		settings["minVersion"] = tls.MinVersion
	}
	if tls.MaxVersion != "" {
		settings["maxVersion"] = tls.MaxVersion
	}
	if tls.CipherSuites != "" {
		settings["cipherSuites"] = tls.CipherSuites
	}
	if tls.PinnedPeerCertSha256 != "" {
		settings["pinnedPeerCertSha256"] = tls.PinnedPeerCertSha256
	}
	if tls.VerifyPeerCertByName != "" {
		settings["verifyPeerCertByName"] = tls.VerifyPeerCertByName
	}
	if len(tls.CurvePreferences) > 0 {
		settings["curvePreferences"] = tls.CurvePreferences
	}
	if tls.MasterKeyLog != "" {
		settings["masterKeyLog"] = tls.MasterKeyLog
	}
	if len(tls.ECH) > 0 {
		// xray-core has no "ech" key — it reads flat echServerKeys /
		// echConfigList / echForceQuery / echSockopt on tlsSettings
		// (infra/conf/transport_internet.go). Nesting under "ech" is silently
		// ignored. The stored blob may be a JSON object, or (legacy frontend
		// textarea) a JSON-encoded string holding that object; unwrap then
		// splat its keys directly onto settings.
		var echParsed interface{}
		if err := json.Unmarshal(tls.ECH, &echParsed); err == nil {
			if s, ok := echParsed.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					var inner interface{}
					if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
						echParsed = inner
					}
				}
			}
			if echObj, ok := echParsed.(map[string]interface{}); ok {
				for k, v := range echObj {
					settings[k] = v
				}
			}
		}
	}

	// Certificates
	if len(tls.Certificates) > 0 {
		certs := []map[string]interface{}{}
		for _, cert := range tls.Certificates {
			c := map[string]interface{}{}

			// Certificate
			// If it looks like a PEM (contains "BEGIN ..."), treat as inline
			// Otherwise treat as file path (absolute or relative)
			if strings.Contains(cert.CertificateFile, "BEGIN") {
				// Inline content (split by newlines and filter empty)
				lines := strings.Split(cert.CertificateFile, "\n")
				filtered := make([]string, 0, len(lines))
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						filtered = append(filtered, line)
					}
				}
				c["certificate"] = filtered
			} else {
				c["certificateFile"] = cert.CertificateFile
			}

			// Key
			// If it looks like a PEM (contains "BEGIN ..."), treat as inline
			// Otherwise treat as file path
			if strings.Contains(cert.KeyFile, "BEGIN") {
				// Inline content (split by newlines and filter empty)
				lines := strings.Split(cert.KeyFile, "\n")
				filtered := make([]string, 0, len(lines))
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						filtered = append(filtered, line)
					}
				}
				c["key"] = filtered
			} else {
				c["keyFile"] = cert.KeyFile
			}

			if cert.Usage != "" {
				c["usage"] = cert.Usage
			}
			if cert.BuildChain {
				c["buildChain"] = true
			}
			if cert.OneTimeLoading {
				c["oneTimeLoading"] = true
			}
			if cert.OCSPStapling > 0 {
				c["ocspStapling"] = cert.OCSPStapling
			}
			certs = append(certs, c)
		}
		settings["certificates"] = certs
	}

	return settings
}

// buildRealityInboundSettings creates Reality security settings for inbound.
// xray-core constraints:
//   - shortIds must be hex, length ≤ 16 chars
//   - spiderX, when set, must start with "/"
func (b *FullConfigBuilder) buildRealityInboundSettings(reality *nodeDomain.RealitySettings) map[string]interface{} {
	settings := map[string]interface{}{
		"show": reality.Show,
	}

	if reality.Dest != "" {
		settings["dest"] = reality.Dest
	}
	if reality.Xver != 0 {
		settings["xver"] = reality.Xver
	}
	if len(reality.ServerNames) > 0 {
		settings["serverNames"] = reality.ServerNames
	}
	if reality.PrivateKey != "" {
		settings["privateKey"] = reality.PrivateKey
	}
	// xray-core requires a non-empty shortIds ARRAY, but an empty-string entry
	// is valid and means "accept clients that present no shortId". Emit the
	// stored value as-is (validateInbound enforces hex/even/≤16 upstream) or a
	// single "" entry. Do NOT synthesize a random id per build: it broke every
	// issued client link and, because the config hash changed every push, it
	// defeated the push-dedup/drift guards and restarted xray on every cycle.
	shortIds := make([]string, 0, len(reality.ShortIDs)+1)
	if reality.ShortID != "" {
		shortIds = append(shortIds, reality.ShortID)
	}
	for _, sid := range reality.ShortIDs {
		if sid != reality.ShortID {
			shortIds = append(shortIds, sid)
		}
	}
	if len(shortIds) == 0 {
		shortIds = append(shortIds, "")
	}
	settings["shortIds"] = shortIds
	if reality.SpiderX != "" {
		spx := reality.SpiderX
		if spx[0] != '/' {
			spx = "/" + spx
		}
		settings["spiderX"] = spx
	}
	// mldsa65Verify is the client half; the server needs mldsa65Seed. Emitting
	// mldsa65Verify on the inbound is harmless (ignored by the server branch)
	// but kept for links; the seed is what actually arms server-side PQ.
	if reality.Mldsa65Seed != "" {
		settings["mldsa65Seed"] = reality.Mldsa65Seed
	}
	if reality.Mldsa65Verify != "" {
		settings["mldsa65Verify"] = reality.Mldsa65Verify
	}
	if reality.MasterKeyLog != "" {
		settings["masterKeyLog"] = reality.MasterKeyLog
	}
	if len(reality.LimitFallbackUpload) > 0 {
		var v interface{}
		if json.Unmarshal(reality.LimitFallbackUpload, &v) == nil {
			settings["limitFallbackUpload"] = v
		}
	}
	if len(reality.LimitFallbackDownload) > 0 {
		var v interface{}
		if json.Unmarshal(reality.LimitFallbackDownload, &v) == nil {
			settings["limitFallbackDownload"] = v
		}
	}
	if reality.MinClientVer != "" {
		settings["minClientVer"] = reality.MinClientVer
	}
	if reality.MaxClientVer != "" {
		settings["maxClientVer"] = reality.MaxClientVer
	}
	if reality.MaxTimeDiff > 0 {
		settings["maxTimeDiff"] = reality.MaxTimeDiff
	}

	return settings
}

// buildRealityOutboundSettings creates Reality security settings for outbound
func (b *FullConfigBuilder) buildRealityOutboundSettings(reality *nodeDomain.RealitySettings) map[string]interface{} {
	settings := map[string]interface{}{
		"show": reality.Show,
	}

	// Outbound uses singular "serverName" not "serverNames"
	if reality.ServerName != "" {
		settings["serverName"] = reality.ServerName
	} else if len(reality.ServerNames) > 0 {
		settings["serverName"] = reality.ServerNames[0]
	}
	if reality.PublicKey != "" {
		settings["publicKey"] = reality.PublicKey
	}
	if reality.ShortID != "" {
		settings["shortId"] = reality.ShortID
	}
	if reality.SpiderX != "" {
		settings["spiderX"] = reality.SpiderX
	}
	if reality.Fingerprint != "" {
		settings["fingerprint"] = reality.Fingerprint
	}
	if len(reality.ALPN) > 0 {
		settings["alpn"] = reality.ALPN
	}
	if reality.Mldsa65Verify != "" {
		settings["mldsa65Verify"] = reality.Mldsa65Verify
	}

	return settings
}

// buildOutbounds creates the outbounds array
func (b *FullConfigBuilder) buildOutbounds() []map[string]interface{} {
	outbounds := []map[string]interface{}{}
	existingTags := make(map[string]bool)

	// First outbound is the default fallback, so foreign leads.
	if b.routerMode {
		plainDirect := map[string]interface{}{
			"tag":      "direct",
			"protocol": "freedom",
			"settings": map[string]interface{}{},
		}
		for _, g := range []struct {
			tag  string
			mark uint32
		}{
			{TagDirectForeign, netmark.GroupMark(netmark.GroupForeign)},
			{TagDirectDomestic, netmark.GroupMark(netmark.GroupDomestic)},
		} {
			outbounds = append(outbounds, b.cloneOutboundWithMark(plainDirect, g.tag, g.mark))
			existingTags[g.tag] = true
		}
	}

	// 1. Add User Outbounds FIRST (First one becomes default fallback)
	for _, out := range b.outbounds {
		if out == nil {
			continue
		}
		if !existingTags[out.Tag] {
			outbounds = append(outbounds, b.convertOutbound(out))
			existingTags[out.Tag] = true
		}
	}

	// 2. Add default 'direct' outbound if not present
	if !existingTags["direct"] {
		outbounds = append(outbounds, map[string]interface{}{
			"tag":      "direct",
			"protocol": "freedom",
			"settings": map[string]interface{}{},
		})
	}

	// Per-tier outbounds: clone default with sockopt.mark for TC rate limiting.
	activeTierLevels := b.getActiveBandwidthLevels()
	defaultOutbound := b.findDefaultOutbound(outbounds)
	for _, tier := range bandwidth.RateLimitedTiers() {
		tag := tier.OutboundTag()
		if existingTags[tag] {
			continue
		}
		if !activeTierLevels[tier.Level] {
			continue
		}
		outbounds = append(outbounds, b.cloneOutboundWithMark(defaultOutbound, tag, tier.Mark))
		existingTags[tag] = true
	}

	// 3. Add default 'blocked' outbound if not present
	if !existingTags["blocked"] {
		outbounds = append(outbounds, map[string]interface{}{
			"tag":      "blocked",
			"protocol": "blackhole",
			"settings": map[string]interface{}{
				"response": map[string]interface{}{
					"type": "none",
				},
			},
		})
	}

	// 4. Add IPv4 outbound if IPv4 routing rules are configured
	rs := b.node.GetRoutingSettingsOrDefault()
	if len(rs.IPv4Routing) > 0 && !existingTags["IPv4"] {
		outbounds = append(outbounds, map[string]interface{}{
			"tag":      "IPv4",
			"protocol": "freedom",
			"settings": map[string]interface{}{
				"domainStrategy": "UseIPv4",
			},
		})
	}

	return outbounds
}

// convertOutbound converts a domain.Outbound to config format
func (b *FullConfigBuilder) convertOutbound(out *nodeDomain.Outbound) map[string]interface{} {
	// The panel's internal id is "hysteria2"; this xray-core registers the
	// outbound loader under "hysteria" only (infra/conf/xray.go). Translate at
	// emit time — otherwise the whole config fails with "unknown config id".
	protocolName := out.Protocol
	if protocolName == "hysteria2" {
		protocolName = "hysteria"
	}
	cfg := map[string]interface{}{
		"tag":      out.Tag,
		"protocol": protocolName,
	}

	// Settings based on protocol
	cfg["settings"] = b.buildOutboundSettings(out)

	// Stream settings — skip transport for wireguard and hysteria2
	if out.Protocol == "hysteria2" {
		// This xray-core models Hysteria2 as a TRANSPORT: streamSettings needs
		// network "hysteria" + hysteriaSettings{version:2,...}; TLS is
		// mandatory. The client auth/udpIdleTimeout live here, NOT in protocol
		// settings (HysteriaClientConfig is only {version,address,port}). A bare
		// {security:tls} with no network fails to load.
		hy := out.GetHysteriaSettingsOrDefault()
		hysteriaSettings := map[string]interface{}{"version": 2}
		if hy.Auth != "" {
			hysteriaSettings["auth"] = hy.Auth
		}
		if hy.UdpIdleTimeout >= 2 && hy.UdpIdleTimeout <= 600 {
			hysteriaSettings["udpIdleTimeout"] = hy.UdpIdleTimeout
		}
		stream := map[string]interface{}{
			"network":          "hysteria",
			"security":         "tls",
			"hysteriaSettings": hysteriaSettings,
		}
		tlsSettings := out.GetTLSSettingsOrDefault()
		if tlsSettings.ServerName != "" || len(tlsSettings.Certificates) > 0 {
			stream["tlsSettings"] = b.buildTLSSettings(tlsSettings)
		}
		cfg["streamSettings"] = stream
	} else if out.Protocol != "wireguard" && (out.Network != "" || out.Security != "") {
		cfg["streamSettings"] = b.buildOutboundStreamSettings(out)
	}

	// SendThrough
	if out.SendThrough != "" {
		cfg["sendThrough"] = out.SendThrough
	}

	// Mux
	if out.MuxSettings != nil && out.MuxSettings.Enabled {
		mux := map[string]interface{}{
			"enabled": true,
		}
		if out.MuxSettings.Concurrency != 0 {
			mux["concurrency"] = out.MuxSettings.Concurrency
		}
		if out.MuxSettings.XudpConcurrency != 0 {
			mux["xudpConcurrency"] = out.MuxSettings.XudpConcurrency
		}
		if out.MuxSettings.XudpProxyUDP443 != "" {
			mux["xudpProxyUDP443"] = out.MuxSettings.XudpProxyUDP443
		}
		cfg["mux"] = mux
	}

	// ProxySettings
	if out.ProxySettings != nil && out.ProxySettings.Tag != "" {
		ps := map[string]interface{}{
			"tag": out.ProxySettings.Tag,
		}
		if out.ProxySettings.TransportLayer {
			ps["transportLayer"] = true
		}
		cfg["proxySettings"] = ps
	}

	return cfg
}

// buildOutboundSettings creates protocol-specific settings for outbounds
func (b *FullConfigBuilder) buildOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	switch out.Protocol {
	case "freedom":
		return b.buildFreedomSettings(out)
	case "blackhole":
		return b.buildBlackholeSettings(out)
	case "socks":
		return b.buildSOCKSOutboundSettings(out)
	case "http":
		return b.buildHTTPOutboundSettings(out)
	case "shadowsocks":
		return b.buildShadowsocksOutboundSettings(out)
	case "wireguard":
		return b.buildWireGuardOutboundSettings(out)
	case "vless":
		return b.buildVLESSOutboundSettings(out)
	case "vmess":
		return b.buildVMessOutboundSettings(out)
	case "trojan":
		return b.buildTrojanOutboundSettings(out)
	case "dns":
		return b.buildDNSOutboundSettings(out)
	case "loopback":
		return b.buildLoopbackSettings(out)
	case "hysteria2":
		return b.buildHysteriaOutboundSettings(out)
	default:
		return map[string]interface{}{}
	}
}

// buildFreedomSettings creates Freedom protocol settings
func (b *FullConfigBuilder) buildFreedomSettings(out *nodeDomain.Outbound) map[string]interface{} {
	settings := map[string]interface{}{}
	fs := out.GetFreedomSettingsOrDefault()

	if fs.DomainStrategy != "" {
		settings["domainStrategy"] = fs.DomainStrategy
	}
	if fs.Redirect != "" {
		settings["redirect"] = fs.Redirect
	}
	if fs.UserLevel > 0 {
		settings["userLevel"] = fs.UserLevel
	}
	if fs.Fragment != nil {
		frag := map[string]interface{}{}
		if fs.Fragment.Packets != "" {
			frag["packets"] = fs.Fragment.Packets
		}
		if fs.Fragment.Length != "" {
			frag["length"] = fs.Fragment.Length
		}
		if fs.Fragment.Interval != "" {
			frag["interval"] = fs.Fragment.Interval
		}
		if fs.Fragment.MaxSplit != "" {
			frag["maxSplit"] = fs.Fragment.MaxSplit
		}
		if len(frag) > 0 {
			settings["fragment"] = frag
		}
	}
	if len(fs.Noise) > 0 {
		noises := make([]map[string]interface{}, 0, len(fs.Noise))
		for _, n := range fs.Noise {
			noise := map[string]interface{}{}
			if n.Type != "" {
				noise["type"] = n.Type
			}
			if n.Packet != "" {
				noise["packet"] = n.Packet
			}
			if n.Delay != "" {
				noise["delay"] = n.Delay
			}
			if n.ApplyTo != "" {
				noise["applyTo"] = n.ApplyTo
			}
			noises = append(noises, noise)
		}
		settings["noises"] = noises
	}
	if fs.ProxyProtocol > 0 {
		settings["proxyProtocol"] = fs.ProxyProtocol
	}
	return settings
}

// buildBlackholeSettings creates Blackhole protocol settings
func (b *FullConfigBuilder) buildBlackholeSettings(out *nodeDomain.Outbound) map[string]interface{} {
	bs := out.GetBlackholeSettingsOrDefault()
	settings := map[string]interface{}{}

	response := map[string]interface{}{
		"type": "none",
	}
	if bs.ResponseType != "" {
		response["type"] = bs.ResponseType
	}
	settings["response"] = response

	return settings
}

// buildSOCKSOutboundSettings creates SOCKS outbound settings
func (b *FullConfigBuilder) buildSOCKSOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	settings := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"address": out.Address,
				"port":    out.Port,
			},
		},
	}

	s := out.GetSOCKSSettingsOrDefault()
	// Outbound SOCKS authentication (if we are connecting TO a socks proxy)
	if len(s.Accounts) > 0 {
		settings["servers"].([]map[string]interface{})[0]["users"] = []map[string]interface{}{
			{
				"user": s.Accounts[0].User,
				"pass": s.Accounts[0].Pass,
			},
		}
	}
	return settings
}

// buildHTTPOutboundSettings creates HTTP outbound settings
func (b *FullConfigBuilder) buildHTTPOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	settings := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"address": out.Address,
				"port":    out.Port,
			},
		},
	}

	h := out.GetHTTPSettingsOrDefault()
	if len(h.Accounts) > 0 {
		settings["servers"].([]map[string]interface{})[0]["users"] = []map[string]interface{}{
			{
				"user": h.Accounts[0].User,
				"pass": h.Accounts[0].Pass,
			},
		}
	}
	return settings
}

// buildShadowsocksOutboundSettings creates Shadowsocks outbound settings
func (b *FullConfigBuilder) buildShadowsocksOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	ss := out.GetShadowsocksSettingsOrDefault()
	settings := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"address":  out.Address,
				"port":     out.Port,
				"method":   ss.Method,
				"password": ss.Password,
			},
		},
	}

	if ss.IVCheck {
		settings["servers"].([]map[string]interface{})[0]["ivCheck"] = true
	}
	if ss.Level > 0 {
		settings["servers"].([]map[string]interface{})[0]["level"] = ss.Level
	}
	if ss.Email != "" {
		settings["servers"].([]map[string]interface{})[0]["email"] = ss.Email
	}
	if ss.UoT {
		settings["servers"].([]map[string]interface{})[0]["uot"] = true
		if ss.UoTVersion > 0 {
			settings["servers"].([]map[string]interface{})[0]["uotVersion"] = ss.UoTVersion
		}
	}

	return settings
}

// buildWireGuardOutboundSettings creates WireGuard outbound settings
func (b *FullConfigBuilder) buildWireGuardOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	wg := out.GetWireGuardSettingsOrDefault()
	settings := map[string]interface{}{}

	if wg.SecretKey != "" {
		settings["secretKey"] = wg.SecretKey
	}
	if len(wg.Endpoint) > 0 {
		settings["address"] = wg.Endpoint
	}
	if wg.MTU > 0 {
		settings["mtu"] = wg.MTU
	}
	if wg.NumWorkers > 0 {
		settings["workers"] = wg.NumWorkers
	}
	if len(wg.Reserved) > 0 {
		settings["reserved"] = wg.Reserved
	}
	if wg.DomainStrategy != "" {
		settings["domainStrategy"] = wg.DomainStrategy
	}
	if wg.NoKernelTun {
		settings["noKernelTun"] = true
	}

	if len(wg.Peers) > 0 {
		peers := make([]map[string]interface{}, 0, len(wg.Peers))
		for _, p := range wg.Peers {
			peer := map[string]interface{}{}
			if p.PublicKey != "" {
				peer["publicKey"] = p.PublicKey
			}
			if p.PreSharedKey != "" {
				peer["preSharedKey"] = p.PreSharedKey
			}
			if p.Endpoint != "" {
				peer["endpoint"] = p.Endpoint
			}
			if p.KeepAlive > 0 {
				peer["keepAlive"] = p.KeepAlive
			}
			if len(p.AllowedIPs) > 0 {
				peer["allowedIPs"] = p.AllowedIPs
			}
			peers = append(peers, peer)
		}
		settings["peers"] = peers
	}

	return settings
}

// buildVLESSOutboundSettings creates VLESS outbound settings
func (b *FullConfigBuilder) buildVLESSOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	v := out.GetVLESSSettingsOrDefault()
	encryption := v.Encryption
	if encryption == "" {
		encryption = "none"
	}
	user := map[string]interface{}{
		"id":         v.UUID,
		"encryption": encryption,
	}
	if v.Flow != "" {
		user["flow"] = v.Flow
	}

	return map[string]interface{}{
		"vnext": []map[string]interface{}{
			{
				"address": out.Address,
				"port":    out.Port,
				"users":   []map[string]interface{}{user},
			},
		},
	}
}

// buildVMessOutboundSettings creates VMess outbound settings
func (b *FullConfigBuilder) buildVMessOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	v := out.GetVMessSettingsOrDefault()
	user := map[string]interface{}{
		"id":       v.UUID,
		"alterId":  v.AlterId,
		"security": v.Security,
	}
	if v.Experiments != "" {
		user["experiments"] = v.Experiments
	}

	return map[string]interface{}{
		"vnext": []map[string]interface{}{
			{
				"address": out.Address,
				"port":    out.Port,
				"users":   []map[string]interface{}{user},
			},
		},
	}
}

// buildTrojanOutboundSettings creates Trojan outbound settings
func (b *FullConfigBuilder) buildTrojanOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	t := out.GetTrojanSettingsOrDefault()
	server := map[string]interface{}{
		"address":  out.Address,
		"port":     out.Port,
		"password": t.Password,
	}
	if t.Level > 0 {
		server["level"] = t.Level
	}
	if t.Email != "" {
		server["email"] = t.Email
	}
	return map[string]interface{}{
		"servers": []map[string]interface{}{server},
	}
}

// buildDNSOutboundSettings creates DNS outbound settings
func (b *FullConfigBuilder) buildDNSOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	dns := out.GetDNSOutboundSettingsOrDefault()
	settings := map[string]interface{}{}

	if dns.Network != "" {
		settings["network"] = dns.Network
	}
	if dns.Address != "" {
		settings["address"] = dns.Address
	}
	if dns.Port > 0 {
		settings["port"] = dns.Port
	}
	if dns.UserLevel > 0 {
		settings["userLevel"] = dns.UserLevel
	}
	if dns.NonIPQuery != "" {
		settings["nonIPQuery"] = dns.NonIPQuery
	}
	if len(dns.BlockTypes) > 0 {
		settings["blockTypes"] = dns.BlockTypes
	}
	return settings
}

// buildLoopbackSettings creates Loopback outbound settings
func (b *FullConfigBuilder) buildLoopbackSettings(out *nodeDomain.Outbound) map[string]interface{} {
	lb := out.GetLoopbackSettingsOrDefault()
	settings := map[string]interface{}{}

	if lb.InboundTag != "" {
		settings["inboundTag"] = lb.InboundTag
	}
	return settings
}

// buildOutboundStreamSettings creates stream settings for an outbound

func (b *FullConfigBuilder) buildOutboundStreamSettings(out *nodeDomain.Outbound) map[string]interface{} {
	stream := map[string]interface{}{}

	if out.Network != "" {
		stream["network"] = out.Network
	}
	security := out.Security
	network := out.Network

	// REALITY only supports tcp, splithttp/xhttp, grpc
	if security == "reality" && network != "tcp" && network != "xhttp" && network != "splithttp" && network != "grpc" {
		security = "none"
	}

	if security != "" {
		stream["security"] = security
	} else if out.Protocol == "trojan" || out.Protocol == "vless" {
		// Default to none if not specified but protocol suggests potential security
		stream["security"] = "none"
	}

	// Security settings
	switch security {
	case "tls":
		stream["tlsSettings"] = b.buildTLSSettings(out.GetTLSSettingsOrDefault())
	case "reality":
		stream["realitySettings"] = b.buildRealityOutboundSettings(out.GetRealitySettingsOrDefault())
	}

	// Sockopt
	if out.SockoptSettings != nil {
		stream["sockopt"] = b.buildSockoptSettings(out.GetSockoptSettingsOrDefault())
	}

	// Network-specific settings
	ts := out.GetTransportSettingsOrDefault()
	switch out.Network {
	case "tcp":
		stream["tcpSettings"] = b.buildTCPSettings(ts)
	case "ws":
		stream["wsSettings"] = b.buildWSSettings(ts)
	case "grpc":
		stream["grpcSettings"] = b.buildGRPCSettings(ts)
	case "xhttp", "splithttp":
		stream["xhttpSettings"] = b.buildXHTTPSettings(ts)
	case "httpupgrade":
		stream["httpupgradeSettings"] = b.buildHTTPUpgradeSettings(ts)
	case "kcp":
		stream["kcpSettings"] = b.buildKCPSettings(ts)
	}

	if out.FinalMask != nil {
		fm := buildFinalMaskMap(out.FinalMask)
		if len(fm) > 0 {
			stream["finalmask"] = fm
		}
	}

	return stream
}

// buildRouting creates the routing configuration
func (b *FullConfigBuilder) buildRouting() map[string]interface{} {
	rs := b.node.GetRoutingSettingsOrDefault()

	routing := map[string]interface{}{
		"domainStrategy": rs.DomainStrategy,
		"rules":          []map[string]interface{}{},
	}
	// Fall back to IPIfNonMatch when empty/unset
	if rs.DomainStrategy == "" {
		routing["domainStrategy"] = "IPIfNonMatch"
	}

	rules := []map[string]interface{}{}

	// Add API rule if enabled
	if b.apiEnabled {
		rules = append(rules, map[string]interface{}{
			"type":        "field",
			"inboundTag":  []string{"api"},
			"outboundTag": "api",
		})
	}

	// Add bandwidth tier routing rules BEFORE other rules so they take priority
	// over catch-all rules like "network: tcp,udp → outbound"
	for _, tier := range bandwidth.RateLimitedTiers() {
		emails := b.getEmailsForLevel(tier.Level)
		if len(emails) == 0 {
			continue
		}
		rules = append(rules, map[string]interface{}{
			"type":        "field",
			"user":        emails,
			"outboundTag": tier.OutboundTag(),
		})
	}

	// Convert domain routing rules (both preset and manual rules from DB)
	for _, rule := range b.routing {
		if rule == nil || !rule.Enabled {
			continue
		}
		rules = append(rules, b.convertRoutingRule(rule))
	}

	// no catch-all — xray falls back to the first outbound when nothing matches,
	// so putting user outbounds first makes the first one the implicit default

	routing["rules"] = rules

	// Build balancing rules
	if len(b.balancing) > 0 {
		balancers := []map[string]interface{}{}
		for _, bal := range b.balancing {
			if !bal.Enabled {
				continue
			}
			entry := map[string]interface{}{
				"tag":      bal.Tag,
				"selector": []string(bal.OutboundSelectors),
			}
			if bal.Strategy != "" && bal.Strategy != "random" {
				entry["strategy"] = map[string]interface{}{
					"type": bal.Strategy,
				}
			}
			if bal.FallbackTag != "" {
				entry["fallbackTag"] = bal.FallbackTag
			}
			balancers = append(balancers, entry)
		}
		if len(balancers) > 0 {
			routing["balancers"] = balancers
		}
	}

	return routing
}

// buildObservatory: required when balancers need probe data. xray-core
// panics in RandomStrategy.InjectContext when FallbackTag is set but
// Observatory feature is missing. leastping/leastload silently degrade
// without it. probeUrl defaults to OutboundTestURL.
func (b *FullConfigBuilder) buildObservatory() map[string]interface{} {
	if len(b.balancing) == 0 {
		return nil
	}

	selectorSet := make(map[string]struct{})
	needs := false
	for _, bal := range b.balancing {
		if bal == nil || !bal.Enabled {
			continue
		}
		switch strings.ToLower(bal.Strategy) {
		case "leastping", "leastload":
			needs = true
		case "random", "":
			if bal.FallbackTag != "" {
				needs = true
			}
		}
		for _, s := range bal.OutboundSelectors {
			s = strings.TrimSpace(s)
			if s != "" {
				selectorSet[s] = struct{}{}
			}
		}
	}
	if !needs || len(selectorSet) == 0 {
		return nil
	}

	selectors := make([]string, 0, len(selectorSet))
	for s := range selectorSet {
		selectors = append(selectors, s)
	}
	sort.Strings(selectors)

	probeURL := "https://www.google.com/generate_204"
	if rs := b.node.GetRoutingSettingsOrDefault(); rs != nil && strings.TrimSpace(rs.OutboundTestURL) != "" {
		probeURL = strings.TrimSpace(rs.OutboundTestURL)
	}

	return map[string]interface{}{
		"subjectSelector":   selectors,
		"probeUrl":          probeURL,
		"probeInterval":     "10s",
		"enableConcurrency": true,
	}
}

// getActiveBandwidthLevels returns a set of tier levels that have at least one user
func (b *FullConfigBuilder) getActiveBandwidthLevels() map[uint32]bool {
	levels := make(map[uint32]bool)
	for _, users := range b.users {
		for _, u := range users {
			if u.Level > 0 {
				levels[u.Level] = true
			}
		}
	}
	return levels
}

// getEmailsForLevel returns all user emails that have the specified Xray level
func (b *FullConfigBuilder) getEmailsForLevel(level uint32) []string {
	seen := make(map[string]bool)
	var emails []string
	for _, users := range b.users {
		for _, u := range users {
			if u.Level == level && !seen[u.Email] {
				emails = append(emails, u.Email)
				seen[u.Email] = true
			}
		}
	}
	return emails
}

// findDefaultOutbound determines which outbound is the "default" for user traffic.
// It checks routing rules for a catch-all (network: tcp,udp) and returns the targeted
// outbound. Falls back to the first non-system outbound, then to a simple freedom outbound.
func (b *FullConfigBuilder) findDefaultOutbound(outbounds []map[string]interface{}) map[string]interface{} {
	// Look for a catch-all routing rule (network: tcp,udp) to find the default outbound tag
	systemTags := map[string]bool{"api": true, "blocked": true, "direct": true}
	for _, rule := range b.routing {
		if rule == nil || !rule.Enabled {
			continue
		}
		// A catch-all is a rule with network: [tcp, udp] (or similar) and no domain/IP/user filters
		if len(rule.NetworkRules) > 0 && len(rule.DomainRules) == 0 && len(rule.GeoIPRules) == 0 &&
			len(rule.UserEmails) == 0 && len(rule.IPCIDRRules) == 0 && rule.OutboundTag != "" &&
			!systemTags[rule.OutboundTag] {
			// Find this outbound in the built outbounds
			for _, out := range outbounds {
				if out["tag"] == rule.OutboundTag {
					return out
				}
			}
		}
	}

	// Fallback: first non-system outbound
	for _, out := range outbounds {
		tag, _ := out["tag"].(string)
		if !systemTags[tag] && tag != "" {
			return out
		}
	}

	// Final fallback: freedom
	return map[string]interface{}{
		"protocol": "freedom",
		"settings": map[string]interface{}{},
	}
}

// cloneOutboundWithMark deep-copies an outbound config and injects sockopt.mark.
func (b *FullConfigBuilder) cloneOutboundWithMark(src map[string]interface{}, tag string, mark uint32) map[string]interface{} {
	cloned := deepCopyMap(src)
	cloned["tag"] = tag

	// Inject sockopt.mark into streamSettings
	ss, ok := cloned["streamSettings"].(map[string]interface{})
	if !ok {
		ss = map[string]interface{}{}
		cloned["streamSettings"] = ss
	}
	sockopt, ok := ss["sockopt"].(map[string]interface{})
	if !ok {
		sockopt = map[string]interface{}{}
		ss["sockopt"] = sockopt
	}
	sockopt["mark"] = mark

	return cloned
}

// deepCopyMap creates a deep copy of a map[string]interface{}.
func deepCopyMap(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case map[string]interface{}:
			dst[k] = deepCopyMap(val)
		case []interface{}:
			cp := make([]interface{}, len(val))
			copy(cp, val)
			dst[k] = cp
		case []string:
			cp := make([]string, len(val))
			copy(cp, val)
			dst[k] = cp
		case []map[string]interface{}:
			cp := make([]map[string]interface{}, len(val))
			for i, m := range val {
				cp[i] = deepCopyMap(m)
			}
			dst[k] = cp
		default:
			dst[k] = v
		}
	}
	return dst
}

// buildDNS creates the dns configuration from the node's DNSSettings.
// Returns nil when DNS is not configured (DNS section omitted from xray config).
func (b *FullConfigBuilder) buildDNS() map[string]interface{} {
	ds := b.node.GetDNSSettingsOrDefault()
	if ds == nil {
		return nil
	}

	dns := map[string]interface{}{}

	// Servers
	if len(ds.Servers) > 0 {
		servers := make([]interface{}, 0, len(ds.Servers))
		for i := range ds.Servers {
			s := &ds.Servers[i]
			if isSimpleDNSServer(s) {
				// Simple server: just the address string
				servers = append(servers, s.Address)
			} else {
				// Full server object
				obj := map[string]interface{}{
					"address": s.Address,
				}
				if s.Port > 0 {
					obj["port"] = s.Port
				}
				if len(s.Domains) > 0 {
					obj["domains"] = s.Domains
				}
				if len(s.ExpectedIPs) > 0 {
					obj["expectedIPs"] = s.ExpectedIPs
				}
				if len(s.UnexpectedIPs) > 0 {
					obj["unexpectedIPs"] = s.UnexpectedIPs
				}
				if s.SkipFallback {
					obj["skipFallback"] = true
				}
				if s.QueryStrategy != "" {
					obj["queryStrategy"] = s.QueryStrategy
				}
				if s.Tag != "" {
					obj["tag"] = s.Tag
				}
				if s.ClientIP != "" {
					obj["clientIp"] = s.ClientIP
				}
				if s.TimeoutMs > 0 {
					obj["timeoutMs"] = s.TimeoutMs
				}
				if s.DisableCache != nil {
					obj["disableCache"] = *s.DisableCache
				}
				if s.ServeStale != nil {
					obj["serveStale"] = *s.ServeStale
				}
				if s.ServeExpiredTTL != nil {
					obj["serveExpiredTTL"] = *s.ServeExpiredTTL
				}
				if s.FinalQuery {
					obj["finalQuery"] = true
				}
				servers = append(servers, obj)
			}
		}
		dns["servers"] = servers
	}

	// Hosts
	if len(ds.Hosts) > 0 {
		dns["hosts"] = ds.Hosts
	}

	// Global options
	if ds.ClientIP != "" {
		dns["clientIp"] = ds.ClientIP
	}
	if ds.QueryStrategy != "" {
		dns["queryStrategy"] = ds.QueryStrategy
	}
	if ds.DisableCache {
		dns["disableCache"] = true
	}
	if ds.DisableFallback {
		dns["disableFallback"] = true
	}
	if ds.DisableFallbackIfMatch {
		dns["disableFallbackIfMatch"] = true
	}
	if ds.Tag != "" {
		dns["tag"] = ds.Tag
	}
	if ds.ServeStale {
		dns["serveStale"] = true
	}
	if ds.ServeExpiredTTL != nil {
		dns["serveExpiredTTL"] = *ds.ServeExpiredTTL
	}
	if ds.EnableParallelQuery {
		dns["enableParallelQuery"] = true
	}
	if ds.UseSystemHosts {
		dns["useSystemHosts"] = true
	}

	if len(dns) == 0 {
		return nil
	}

	return dns
}

// buildFakeDNS builds the fakedns config section from node's FakeDNS pools.
func (b *FullConfigBuilder) buildFakeDNS() interface{} {
	pools := b.node.GetFakeDNSSettingsOrDefault()
	if len(pools) == 0 {
		return nil
	}

	var result []map[string]interface{}
	for _, p := range pools {
		pool := map[string]interface{}{}
		if p.IPPool != "" {
			pool["ipPool"] = p.IPPool
		}
		if p.LRUSize > 0 {
			// xray-core HEAD `infra/conf/fakedns.go` reads JSON key
			// "poolSize" into LRUSize. The internal proto field is named
			// LruSize but the user-facing JSON tag is poolSize.
			pool["poolSize"] = p.LRUSize
		}
		if len(pool) > 0 {
			result = append(result, pool)
		}
	}
	if len(result) == 0 {
		return nil
	}
	if len(result) == 1 {
		return result[0] // Single pool: emit as object
	}
	return result // Multiple pools: emit as array
}

// isSimpleDNSServer returns true if the server only has an address (no extra fields).
func isSimpleDNSServer(s *nodeDomain.DNSServer) bool {
	return s.Port == 0 &&
		len(s.Domains) == 0 &&
		len(s.ExpectedIPs) == 0 &&
		len(s.UnexpectedIPs) == 0 &&
		!s.SkipFallback &&
		s.QueryStrategy == "" &&
		s.Tag == "" &&
		s.ClientIP == "" &&
		s.TimeoutMs == 0 &&
		s.DisableCache == nil &&
		s.ServeStale == nil &&
		s.ServeExpiredTTL == nil &&
		!s.FinalQuery
}

// convertRoutingRule converts a domain.RoutingRule to config format
func (b *FullConfigBuilder) convertRoutingRule(rule *nodeDomain.RoutingRule) map[string]interface{} {
	cfg := map[string]interface{}{
		"type": "field",
	}

	if rule.RuleTag != "" {
		cfg["ruleTag"] = rule.RuleTag
	}

	if rule.OutboundTag != "" {
		cfg["outboundTag"] = rule.OutboundTag
	} else if rule.BalancingTag != "" {
		cfg["balancerTag"] = rule.BalancingTag
	}

	// Convert DomainRules to domain strings for Xray config
	if len(rule.DomainRules) > 0 {
		domains := make([]string, len(rule.DomainRules))
		for i, d := range rule.DomainRules {
			// Values with a recognized xray prefix (geosite:, ext:) are used as-is
			if strings.HasPrefix(d.Value, "geosite:") || strings.HasPrefix(d.Value, "ext:") || strings.HasPrefix(d.Value, "ext-domain:") || strings.HasPrefix(d.Value, "dotless:") {
				domains[i] = d.Value
			} else if d.Type != "" && d.Type != "plain" {
				// Format: "type:value" for xray (e.g., "domain:google.com", "regexp:.*\.cn$")
				domains[i] = d.Type + ":" + d.Value
			} else {
				domains[i] = d.Value
			}
		}
		cfg["domain"] = domains
	}

	// GeoIP rules
	if len(rule.GeoIPRules) > 0 {
		cfg["ip"] = []string(rule.GeoIPRules)
	}

	// IP CIDR rules
	if len(rule.IPCIDRRules) > 0 {
		ips := cfg["ip"]
		if ips == nil {
			cfg["ip"] = []string(rule.IPCIDRRules)
		} else {
			// Merge with GeoIP rules
			existing := ips.([]string)
			cfg["ip"] = append(existing, []string(rule.IPCIDRRules)...)
		}
	}

	if len(rule.InboundTags) > 0 {
		cfg["inboundTag"] = []string(rule.InboundTags)
	}

	if len(rule.UserEmails) > 0 {
		cfg["user"] = []string(rule.UserEmails)
	}

	if len(rule.PortRules) > 0 {
		// Join ports with comma if multiple
		portStr := ""
		for i, p := range rule.PortRules {
			if i > 0 {
				portStr += ","
			}
			portStr += p
		}
		cfg["port"] = portStr
	}

	if len(rule.NetworkRules) > 0 {
		// Join networks with comma if multiple
		netStr := ""
		for i, n := range rule.NetworkRules {
			if i > 0 {
				netStr += ","
			}
			netStr += n
		}
		cfg["network"] = netStr
	}

	if len(rule.ProtocolRules) > 0 {
		cfg["protocol"] = []string(rule.ProtocolRules)
	}

	// Source IP matchers
	if len(rule.SourceIPs) > 0 {
		cfg["source"] = []string(rule.SourceIPs)
	}

	// Source port matchers
	if len(rule.SourcePorts) > 0 {
		sourcePortStr := ""
		for i, p := range rule.SourcePorts {
			if i > 0 {
				sourcePortStr += ","
			}
			sourcePortStr += p
		}
		cfg["sourcePort"] = sourcePortStr
	}

	// Attributes (HTTP header matching)
	if len(rule.Attributes) > 0 {
		cfg["attrs"] = map[string]string(rule.Attributes)
	}

	// Process name matching
	if len(rule.ProcessNames) > 0 {
		cfg["process"] = []string(rule.ProcessNames)
	}

	// Local IP matchers
	if len(rule.LocalIPs) > 0 {
		cfg["localIP"] = []string(rule.LocalIPs)
	}

	// Local port matchers
	if len(rule.LocalPorts) > 0 {
		localPortStr := strings.Join([]string(rule.LocalPorts), ",")
		cfg["localPort"] = localPortStr
	}

	return cfg
}

// buildShadowsocksSettings emits xray-core's multi-user shape per cipher:
// 2022-blake3-aes-*-gcm via EIH (top-level password is PSK, per-user is
// identity key); AEAD legacy via clients[]; 2022-blake3-chacha20-poly1305
// and none/plain are single-user only.
func (b *FullConfigBuilder) buildShadowsocksSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	ss := inb.GetShadowsocksSettingsOrDefault()

	settings := map[string]interface{}{
		"method": ss.Method,
	}

	method := strings.ToLower(ss.Method)
	is2022AES := strings.HasPrefix(method, "2022-blake3-aes-")
	isAEADLegacy := method == "aes-128-gcm" || method == "aes-256-gcm" ||
		method == "chacha20-poly1305" || method == "chacha20-ietf-poly1305" ||
		method == "xchacha20-poly1305" || method == "xchacha20-ietf-poly1305"

	users := b.users[inb.Tag]

	switch {
	case is2022AES && len(users) > 0:
		// PSK at top level, EIH identity keys per client.
		if ss.Password != "" {
			settings["password"] = ss.Password
		}
		clients := make([]interface{}, 0, len(users))
		for _, u := range users {
			c := map[string]interface{}{
				"password": u.UUID,
				"email":    u.Email,
				"level":    u.Level,
			}
			clients = append(clients, c)
		}
		settings["clients"] = clients

	case isAEADLegacy && len(users) > 1:
		// Multi-user AEAD legacy: `clients[]` only. xray-core reads each
		// client's cipher from its own `method` field (infra/conf
		// /shadowsocks.go); omitting it yields CipherType_UNKNOWN → the whole
		// config fails to build with "unsupported cipher method".
		clients := make([]interface{}, 0, len(users))
		for _, u := range users {
			c := map[string]interface{}{
				"method":   ss.Method,
				"password": u.UUID,
				"email":    u.Email,
				"level":    u.Level,
			}
			clients = append(clients, c)
		}
		settings["clients"] = clients

	case len(users) > 0:
		// Single-user shape (single-user-only ciphers OR single user on
		// any cipher).
		u := users[0]
		settings["password"] = u.UUID
		settings["email"] = u.Email
		settings["level"] = u.Level

	case ss.Password != "":
		settings["password"] = ss.Password
	}

	if ss.Email != "" && settings["email"] == nil {
		settings["email"] = ss.Email
	}

	if ss.Network != "" {
		settings["network"] = ss.Network
	} else {
		// xray defaults an absent network to TCP-only; the panel's intent
		// (and default) is tcp+udp, so emit it explicitly to avoid silently
		// disabling UDP on rows saved with an empty network.
		settings["network"] = "tcp,udp"
	}
	if ss.IVCheck {
		settings["ivCheck"] = true
	}
	return settings
}

// buildWireGuardSettings: symmetric inbound/outbound. xray-core requires
// each address entry /32 (v4) or /128 (v6).
func (b *FullConfigBuilder) buildWireGuardSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	wg := inb.GetWireGuardSettingsOrDefault()
	settings := map[string]interface{}{}

	if wg.SecretKey != "" {
		settings["secretKey"] = wg.SecretKey
	}
	if wg.MTU > 0 {
		settings["mtu"] = wg.MTU
	}
	if len(wg.Endpoint) > 0 {
		settings["address"] = wg.Endpoint
	}
	if wg.NumWorkers > 0 {
		// xray-core reads this as "workers" (infra/conf/wireguard.go);
		// "numWorkers" was silently ignored.
		settings["workers"] = wg.NumWorkers
	}
	if len(wg.Reserved) > 0 {
		settings["reserved"] = wg.Reserved
	}
	if wg.DomainStrategy != "" {
		settings["domainStrategy"] = wg.DomainStrategy
	}
	if wg.NoKernelTun {
		settings["noKernelTun"] = true
	}

	// Build peers
	if len(wg.Peers) > 0 {
		peers := []map[string]interface{}{}
		for _, p := range wg.Peers {
			peer := map[string]interface{}{}
			if p.PublicKey != "" {
				peer["publicKey"] = p.PublicKey
			}
			if p.PreSharedKey != "" {
				peer["preSharedKey"] = p.PreSharedKey
			}
			if p.Endpoint != "" {
				peer["endpoint"] = p.Endpoint
			}
			if p.KeepAlive > 0 {
				peer["keepAlive"] = p.KeepAlive
			}
			if len(p.AllowedIPs) > 0 {
				peer["allowedIps"] = p.AllowedIPs
			}
			peers = append(peers, peer)
		}
		settings["peers"] = peers
	}

	return settings
}

// buildSockoptSettings creates socket options settings
func (b *FullConfigBuilder) buildSockoptSettings(s *nodeDomain.SockoptSettings) map[string]interface{} {
	settings := map[string]interface{}{}

	if s.Mark > 0 {
		settings["mark"] = s.Mark
	}
	if s.TcpFastOpen {
		settings["tcpFastOpen"] = true
	}
	if s.Tproxy != "" {
		settings["tproxy"] = s.Tproxy
	}
	if s.DomainStrategy != "" {
		settings["domainStrategy"] = s.DomainStrategy
	}
	if s.DialerProxy != "" {
		settings["dialerProxy"] = s.DialerProxy
	}
	if s.TcpKeepAliveInterval > 0 {
		settings["tcpKeepAliveInterval"] = s.TcpKeepAliveInterval
	}
	if s.TcpKeepAliveIdle > 0 {
		settings["tcpKeepAliveIdle"] = s.TcpKeepAliveIdle
	}
	if s.TcpCongestion != "" {
		settings["tcpCongestion"] = s.TcpCongestion
	}
	if s.TcpWindowClamp > 0 {
		settings["tcpWindowClamp"] = s.TcpWindowClamp
	}
	if s.TcpUserTimeout > 0 {
		settings["tcpUserTimeout"] = s.TcpUserTimeout
	}
	if s.TcpMaxSeg > 0 {
		settings["tcpMaxSeg"] = s.TcpMaxSeg
	}
	if s.TcpMptcp {
		settings["tcpMptcp"] = true
	}
	if s.AcceptProxyProtocol {
		settings["acceptProxyProtocol"] = true
	}
	if s.Interface != "" {
		settings["interface"] = s.Interface
	}
	if s.V6Only {
		settings["v6only"] = true
	}
	if s.Penetrate {
		settings["penetrate"] = true
	}
	if s.AddressPortStrategy != "" {
		settings["addressPortStrategy"] = s.AddressPortStrategy
	}
	if len(s.TrustedXForwardedFor) > 0 {
		settings["trustedXForwardedFor"] = s.TrustedXForwardedFor
	}
	if s.HappyEyeballs != nil {
		// xray-core keys: tryDelayMs / maxConcurrentTry (infra/conf
		// /transport_internet.go HappyEyeballsConfig). The old tryDelay /
		// maxConcurrency keys were silently ignored.
		he := map[string]interface{}{}
		if s.HappyEyeballs.TryDelay > 0 {
			he["tryDelayMs"] = s.HappyEyeballs.TryDelay
		}
		if s.HappyEyeballs.MaxConcurrency > 0 {
			he["maxConcurrentTry"] = s.HappyEyeballs.MaxConcurrency
		}
		if len(he) > 0 {
			settings["happyEyeballs"] = he
		}
	}
	if len(s.CustomSockopt) > 0 {
		// xray-core's CustomSockoptConfig is all-string fields named
		// system/network/level/opt/value/type (infra/conf/transport_internet.go).
		// The old numeric level + optName/optValue keys made the whole config
		// unparseable (json: cannot unmarshal number into string). Convert.
		cs := make([]map[string]interface{}, 0, len(s.CustomSockopt))
		for _, c := range s.CustomSockopt {
			entry := map[string]interface{}{}
			if c.Level != 0 {
				entry["level"] = strconv.Itoa(c.Level)
			}
			if c.OptName != 0 {
				entry["opt"] = strconv.Itoa(c.OptName)
			}
			if c.OptValue != nil {
				entry["value"] = fmt.Sprintf("%v", c.OptValue)
			}
			cs = append(cs, entry)
		}
		settings["customSockopt"] = cs
	}

	return settings
}

// buildHTTPInboundSettings creates HTTP proxy inbound settings
func (b *FullConfigBuilder) buildHTTPInboundSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	http := inb.GetHTTPSettingsOrDefault()
	settings := map[string]interface{}{}

	if http.AllowTransparent {
		settings["allowTransparent"] = true
	}
	if http.Timeout > 0 {
		settings["timeout"] = http.Timeout
	}
	if http.UserLevel > 0 {
		settings["userLevel"] = http.UserLevel
	}

	// Build accounts
	if len(http.Accounts) > 0 {
		accounts := []map[string]interface{}{}
		for _, acc := range http.Accounts {
			account := map[string]interface{}{}
			if acc.User != "" {
				account["user"] = acc.User
			}
			if acc.Pass != "" {
				account["pass"] = acc.Pass
			}
			accounts = append(accounts, account)
		}
		settings["accounts"] = accounts
	}

	return settings
}

// buildSOCKSSettings creates SOCKS proxy inbound settings
func (b *FullConfigBuilder) buildSOCKSSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	socks := inb.GetSOCKSSettingsOrDefault()
	settings := map[string]interface{}{}

	if socks.Auth != "" {
		settings["auth"] = socks.Auth
	}
	if socks.UDP {
		settings["udp"] = true
	}
	if socks.IP != "" {
		settings["ip"] = socks.IP
	}
	if socks.UserLevel > 0 {
		settings["userLevel"] = socks.UserLevel
	}

	// Build accounts
	if len(socks.Accounts) > 0 {
		accounts := []map[string]interface{}{}
		for _, acc := range socks.Accounts {
			account := map[string]interface{}{}
			if acc.User != "" {
				account["user"] = acc.User
			}
			if acc.Pass != "" {
				account["pass"] = acc.Pass
			}
			accounts = append(accounts, account)
		}
		settings["accounts"] = accounts
	}

	return settings
}

// buildFinalMaskMap: domain.FinalMask → xray-core shape. Accepts either
// raw JSON object or JSON-string-of-JSON (auto-unwrapped); else passes
// through so xray can surface a real parse error.
//
// xray-core wants finalmask.tcp / finalmask.udp as ARRAYS of Mask objects
// ([]conf.Mask), while quicParams is a single object. A lone Mask object is
// wrapped into a one-element array so a config entered as {"type":…} still
// loads (otherwise: "cannot unmarshal object into …finalmask.udp of type
// []conf.Mask").
func buildFinalMaskMap(fm *nodeDomain.FinalMask) map[string]interface{} {
	out := map[string]interface{}{}
	add := func(key string, raw json.RawMessage, asArray bool) {
		if len(raw) == 0 {
			return
		}
		var parsed interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return
		}
		if s, ok := parsed.(string); ok {
			trimmed := strings.TrimSpace(s)
			if trimmed == "" {
				return
			}
			var inner interface{}
			if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
				parsed = inner
			} else {
				parsed = s
			}
		}
		if asArray {
			if _, isArr := parsed.([]interface{}); !isArr {
				parsed = []interface{}{parsed}
			}
		}
		out[key] = parsed
	}
	add("tcp", fm.TCP, true)
	add("udp", fm.UDP, true)
	add("quicParams", fm.QuicParams, false)
	return out
}

// buildKCPSettings creates mKCP stream settings.
// xray-core upstream removed `header` and `seed` fields; emit only the
// numeric tuning knobs. Defaults applied by xray when absent.
func (b *FullConfigBuilder) buildKCPSettings(ts *nodeDomain.TransportSettings) map[string]interface{} {
	settings := map[string]interface{}{}
	if ts.KCPMtu > 0 {
		settings["mtu"] = ts.KCPMtu
	}
	if ts.KCPTti > 0 {
		settings["tti"] = ts.KCPTti
	}
	if ts.KCPUplinkCapacity > 0 {
		settings["uplinkCapacity"] = ts.KCPUplinkCapacity
	}
	if ts.KCPDownlinkCapacity > 0 {
		settings["downlinkCapacity"] = ts.KCPDownlinkCapacity
	}
	if ts.KCPCwndMultiplier > 0 {
		settings["cwndMultiplier"] = ts.KCPCwndMultiplier
	}
	if ts.KCPMaxSendingWindow > 0 {
		settings["maxSendingWindow"] = ts.KCPMaxSendingWindow
	}
	return settings
}

// xrayRange renders a domain RangeConfig into the form xray-core's Int32Range
func xrayRange(r *nodeDomain.RangeConfig) interface{} {
	if r == nil {
		return nil
	}
	if r.From == r.To {
		return int(r.From)
	}
	return fmt.Sprintf("%d-%d", r.From, r.To)
}

// buildFallbacks converts domain Fallback slice to config format
func (b *FullConfigBuilder) buildFallbacks(fallbacks []nodeDomain.Fallback) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(fallbacks))
	for _, fb := range fallbacks {
		f := map[string]interface{}{}
		if fb.Alpn != "" {
			f["alpn"] = fb.Alpn
		}
		if fb.Path != "" {
			f["path"] = fb.Path
		}
		if fb.Dest != nil {
			f["dest"] = fb.Dest
		}
		if fb.Xver > 0 {
			f["xver"] = fb.Xver
		}
		if fb.Name != "" {
			f["name"] = fb.Name
		}
		if fb.Type != "" {
			f["type"] = fb.Type
		}
		result = append(result, f)
	}
	return result
}

// buildDokodemoDoorSettings creates Dokodemo-door transparent proxy settings
func (b *FullConfigBuilder) buildDokodemoDoorSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	settings := map[string]interface{}{}
	dd := inb.GetDokodemoSettingsOrDefault()

	if dd.Address != "" {
		settings["address"] = dd.Address
	}
	if dd.Port > 0 {
		settings["port"] = dd.Port
	}
	if dd.Networks != "" {
		settings["network"] = dd.Networks
	}
	if dd.UserLevel > 0 {
		settings["userLevel"] = dd.UserLevel
	}
	if dd.FollowRedirect {
		settings["followRedirect"] = true
	}
	if len(dd.PortMap) > 0 {
		settings["portMap"] = dd.PortMap
	}
	return settings
}

// buildHysteriaInboundSettings builds the inbound protocol `settings` block,
// which in this xray-core is HysteriaServerConfig = {version, clients}. All
// transport knobs (udpIdleTimeout, congestion, masquerade) live in
// streamSettings.hysteriaSettings instead (set in convertInbound), NOT here —
// HysteriaServerConfig has no such fields and would silently ignore them.
func (b *FullConfigBuilder) buildHysteriaInboundSettings(inb *nodeDomain.Inbound) map[string]interface{} {
	settings := map[string]interface{}{
		"version": 2,
	}
	clients := []map[string]interface{}{}
	if users, ok := b.users[inb.Tag]; ok {
		for _, u := range users {
			client := map[string]interface{}{
				"auth":  u.UUID,
				"email": u.Email,
				"level": u.Level,
			}
			clients = append(clients, client)
		}
	}
	if len(clients) > 0 {
		settings["clients"] = clients
	}
	return settings
}

// buildHysteriaOutboundSettings this xray-core's HysteriaClientConfig is only {version, address, port}
func (b *FullConfigBuilder) buildHysteriaOutboundSettings(out *nodeDomain.Outbound) map[string]interface{} {
	settings := map[string]interface{}{
		"version": 2,
	}
	if out.Address != "" {
		settings["address"] = out.Address
	}
	if out.Port > 0 {
		settings["port"] = out.Port
	}
	return settings
}
