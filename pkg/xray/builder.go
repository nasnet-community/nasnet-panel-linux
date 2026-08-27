package xray

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/geodata"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/proxy/blackhole"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/proxy/http"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/socks"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	vlessInbound "github.com/xtls/xray-core/proxy/vless/inbound"
	vlessOutbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/proxy/vmess"
	"github.com/xtls/xray-core/proxy/vmess/inbound"
	vmessOutbound "github.com/xtls/xray-core/proxy/vmess/outbound"
	"github.com/xtls/xray-core/proxy/wireguard"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/grpc"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/splithttp"
	"github.com/xtls/xray-core/transport/internet/tls"
	"github.com/xtls/xray-core/transport/internet/websocket"
)

// BuildInboundHandlerConfig constructs the protobuf message for adding an inbound
func BuildInboundHandlerConfig(cfg *InboundConfig) (*core.InboundHandlerConfig, error) {
	// 1. Build ReceiverSettings (IP, Port, Sniffing, StreamSettings)
	receiverSettings, err := buildReceiverSettings(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build receiver settings: %w", err)
	}

	// 2. Build ProxySettings (Protocol specific config)
	proxySettings, err := buildProxySettings(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build proxy settings: %w", err)
	}

	return &core.InboundHandlerConfig{
		Tag:              cfg.Tag,
		ReceiverSettings: serial.ToTypedMessage(receiverSettings),
		ProxySettings:    proxySettings,
	}, nil
}

func buildReceiverSettings(cfg *InboundConfig) (*proxyman.ReceiverConfig, error) {
	// Port Range
	portList := &net.PortList{
		Range: []*net.PortRange{
			{From: cfg.Port, To: cfg.Port},
		},
	}

	// Listen Address
	listenIP := net.ParseAddress(cfg.Listen)
	if cfg.Listen == "" {
		listenIP = net.AnyIP
	}

	// Stream Settings
	streamSettings, err := buildStreamSettings(cfg)
	if err != nil {
		return nil, err
	}

	// Sniffing
	var sniffing *proxyman.SniffingConfig
	if cfg.Sniffing != nil && cfg.Sniffing.Enabled {
		sniffing = &proxyman.SniffingConfig{
			Enabled:             true,
			DestinationOverride: cfg.Sniffing.DestOverride,
		}
	}

	return &proxyman.ReceiverConfig{
		PortList:         portList,
		Listen:           net.NewIPOrDomain(listenIP),
		StreamSettings:   streamSettings,
		SniffingSettings: sniffing,
	}, nil
}

func buildStreamSettings(cfg *InboundConfig) (*internet.StreamConfig, error) {
	protocolName := cfg.Network
	// Normalize xhttp to splithttp for xray-core
	if protocolName == "xhttp" {
		protocolName = "splithttp"
	}

	sc := &internet.StreamConfig{
		ProtocolName: protocolName,
	}

	// Transport Settings
	var transportSettings []*internet.TransportConfig

	switch cfg.Network {
	case "tcp":
		// TCP usually has no special settings unless HTTP header is used
	case "ws":
		if cfg.WS != nil {
			wsConfig := &websocket.Config{
				Path: cfg.WS.Path,
				Host: cfg.WS.Host,
			}
			transportSettings = append(transportSettings, &internet.TransportConfig{
				ProtocolName: protocolName,
				Settings:     serial.ToTypedMessage(wsConfig),
			})
		}
	case "grpc":
		if cfg.GRPC != nil {
			grpcConfig := &grpc.Config{
				ServiceName: cfg.GRPC.ServiceName,
				MultiMode:   cfg.GRPC.MultiMode,
			}
			transportSettings = append(transportSettings, &internet.TransportConfig{
				ProtocolName: protocolName,
				Settings:     serial.ToTypedMessage(grpcConfig),
			})
		}
	case "xhttp", "splithttp":
		if cfg.XHTTP != nil {
			xhttpConfig := &splithttp.Config{
				Host: cfg.XHTTP.Host,
				Path: cfg.XHTTP.Path,
				Mode: cfg.XHTTP.Mode,
			}
			if len(cfg.XHTTP.Headers) > 0 {
				xhttpConfig.Headers = cfg.XHTTP.Headers
			}
			if cfg.XHTTP.NoGRPCHeader {
				xhttpConfig.NoGRPCHeader = true
			}
			if cfg.XHTTP.NoSSEHeader {
				xhttpConfig.NoSSEHeader = true
			}
			if cfg.XHTTP.ScMaxBufferedPosts > 0 {
				xhttpConfig.ScMaxBufferedPosts = cfg.XHTTP.ScMaxBufferedPosts
			}
			transportSettings = append(transportSettings, &internet.TransportConfig{
				ProtocolName: protocolName,
				Settings:     serial.ToTypedMessage(xhttpConfig),
			})
		}
	}

	if len(transportSettings) > 0 {
		sc.TransportSettings = transportSettings
	}

	// Security Settings
	sc.SecurityType = cfg.Security

	switch cfg.Security {
	case "tls":
		if cfg.TLS != nil {
			tlsConfig := &tls.Config{
				ServerName:   cfg.TLS.ServerName,
				NextProtocol: cfg.TLS.ALPN,
				MinVersion:   cfg.TLS.MinVersion,
				MaxVersion:   cfg.TLS.MaxVersion,
			}
			// Handle Certificates - either by content or by file path
			if cfg.TLS.CertContent != "" && cfg.TLS.KeyContent != "" {
				// Content mode - pass certificate bytes directly
				tlsConfig.Certificate = []*tls.Certificate{
					{
						Certificate: []byte(cfg.TLS.CertContent),
						Key:         []byte(cfg.TLS.KeyContent),
						Usage:       tls.Certificate_ENCIPHERMENT,
					},
				}
			} else if cfg.TLS.CertPath != "" && cfg.TLS.KeyPath != "" {
				// Path mode - use file paths
				tlsConfig.Certificate = []*tls.Certificate{
					{
						CertificatePath: cfg.TLS.CertPath,
						KeyPath:         cfg.TLS.KeyPath,
						Usage:           tls.Certificate_ENCIPHERMENT,
					},
				}
			}

			typedTLS := serial.ToTypedMessage(tlsConfig)
			sc.SecurityType = typedTLS.Type
			sc.SecuritySettings = []*serial.TypedMessage{typedTLS}
		}
	case "reality":
		if cfg.Reality != nil {
			realityConfig := &reality.Config{
				Show:        cfg.Reality.Show,
				Dest:        cfg.Reality.Dest,
				Xver:        cfg.Reality.Xver,
				ServerNames: cfg.Reality.ServerNames,
				PrivateKey:  []byte(cfg.Reality.PrivateKey),
				ShortIds:    convertShortIDs(cfg.Reality.ShortIDs),
				Fingerprint: cfg.Reality.Fingerprint,
			}

			typedReality := serial.ToTypedMessage(realityConfig)
			sc.SecurityType = typedReality.Type
			sc.SecuritySettings = []*serial.TypedMessage{typedReality}
		}
	}

	return sc, nil
}

// convertShortIDs converts string short IDs to [][]byte
func convertShortIDs(sids []string) [][]byte {
	result := make([][]byte, 0, len(sids))
	for _, sid := range sids {
		result = append(result, []byte(sid))
	}
	return result
}

func buildProxySettings(cfg *InboundConfig) (*serial.TypedMessage, error) {
	switch strings.ToLower(cfg.Protocol) {
	case "vless":
		vlessCfg := &vlessInbound.Config{
			Users:      []*protocol.User{},
			Decryption: "none",
		}
		if cfg.VLESS != nil {
			if cfg.VLESS.Decryption != "" {
				vlessCfg.Decryption = cfg.VLESS.Decryption
			}
		}
		return serial.ToTypedMessage(vlessCfg), nil
	case "vmess":
		return serial.ToTypedMessage(&inbound.Config{
			User: []*protocol.User{},
		}), nil
	case "trojan":
		return serial.ToTypedMessage(&trojan.ServerConfig{
			Users: []*protocol.User{},
		}), nil
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
	}
}

// === Outbound Builder ===

// BuildOutboundHandlerConfig constructs the protobuf message for adding an outbound
func BuildOutboundHandlerConfig(cfg *OutboundConfig) (*core.OutboundHandlerConfig, error) {
	// Validate config first
	if err := ValidateOutboundConfig(cfg); err != nil {
		return nil, err
	}

	// 1. Build SenderSettings (stream settings for outbound)
	senderSettings, err := buildSenderSettings(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build sender settings: %w", err)
	}

	// 2. Build ProxySettings (Protocol specific config)
	proxySettings, err := buildOutboundProxySettings(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build outbound proxy settings: %w", err)
	}

	return &core.OutboundHandlerConfig{
		Tag:            cfg.Tag,
		SenderSettings: serial.ToTypedMessage(senderSettings),
		ProxySettings:  proxySettings,
	}, nil
}

func buildSenderSettings(cfg *OutboundConfig) (*proxyman.SenderConfig, error) {
	sc := &proxyman.SenderConfig{}

	// WireGuard doesn't use stream settings
	if strings.ToLower(cfg.Protocol) == "wireguard" {
		return sc, nil
	}

	// Build stream settings if needed (for tcp, ws, grpc, etc.)
	if cfg.Network != "" && cfg.Network != "tcp" {
		streamSettings, err := buildOutboundStreamSettings(cfg)
		if err != nil {
			return nil, err
		}
		sc.StreamSettings = streamSettings
	} else if cfg.Security == "tls" || cfg.Security == "reality" {
		// Even TCP needs stream settings for TLS/Reality
		streamSettings, err := buildOutboundStreamSettings(cfg)
		if err != nil {
			return nil, err
		}
		sc.StreamSettings = streamSettings
	} else if cfg.Sockopt != nil {
		// TCP with Sockopt
		streamSettings, err := buildOutboundStreamSettings(cfg)
		if err != nil {
			return nil, err
		}
		sc.StreamSettings = streamSettings
	}

	return sc, nil
}

func buildOutboundStreamSettings(cfg *OutboundConfig) (*internet.StreamConfig, error) {
	protocolName := cfg.Network
	if protocolName == "" {
		protocolName = "tcp"
	}
	if protocolName == "xhttp" {
		protocolName = "splithttp"
	}

	sc := &internet.StreamConfig{
		ProtocolName: protocolName,
	}

	// Transport Settings
	var transportSettings []*internet.TransportConfig

	switch cfg.Network {
	case "ws":
		if cfg.WS != nil {
			wsConfig := &websocket.Config{
				Path: cfg.WS.Path,
				Host: cfg.WS.Host,
			}
			transportSettings = append(transportSettings, &internet.TransportConfig{
				ProtocolName: protocolName,
				Settings:     serial.ToTypedMessage(wsConfig),
			})
		}
	case "grpc":
		if cfg.GRPC != nil {
			grpcConfig := &grpc.Config{
				ServiceName: cfg.GRPC.ServiceName,
				MultiMode:   cfg.GRPC.MultiMode,
			}
			transportSettings = append(transportSettings, &internet.TransportConfig{
				ProtocolName: protocolName,
				Settings:     serial.ToTypedMessage(grpcConfig),
			})
		}
	case "xhttp", "splithttp":
		if cfg.XHTTP != nil {
			xhttpConfig := &splithttp.Config{
				Host: cfg.XHTTP.Host,
				Path: cfg.XHTTP.Path,
				Mode: cfg.XHTTP.Mode,
			}
			transportSettings = append(transportSettings, &internet.TransportConfig{
				ProtocolName: protocolName,
				Settings:     serial.ToTypedMessage(xhttpConfig),
			})
		}
	}

	if len(transportSettings) > 0 {
		sc.TransportSettings = transportSettings
	}

	// Security Settings (client-side TLS/Reality)
	switch cfg.Security {
	case "tls":
		if cfg.TLS != nil {
			tlsConfig := &tls.Config{
				ServerName:   cfg.TLS.ServerName,
				NextProtocol: cfg.TLS.ALPN,
			}
			// xray-core removed `allowInsecure` (verify nothing) and points at
			// `verifyPeerCertByName`/`pinnedPeerCertSha256` instead. Name-only
			// verification is the documented replacement and keeps self-signed
			// outbounds working, so map onto it when we know the expected name.
			// With no serverName there is nothing to verify against, so the
			// connection stays strictly verified.
			if cfg.TLS.AllowInsecure && cfg.TLS.ServerName != "" {
				tlsConfig.VerifyPeerCertByName = []string{cfg.TLS.ServerName}
			}
			if cfg.TLS.Fingerprint != "" {
				tlsConfig.Fingerprint = cfg.TLS.Fingerprint
			}
			typedTLS := serial.ToTypedMessage(tlsConfig)
			sc.SecurityType = typedTLS.Type
			sc.SecuritySettings = []*serial.TypedMessage{typedTLS}
		}
	case "reality":
		if cfg.Reality != nil {
			realityConfig := &reality.Config{
				ServerName:  cfg.Reality.ServerName,
				Fingerprint: cfg.Reality.Fingerprint,
				PublicKey:   []byte(cfg.Reality.PublicKey),
				ShortId:     []byte(cfg.Reality.ShortID),
				SpiderX:     cfg.Reality.SpiderX,
			}
			typedReality := serial.ToTypedMessage(realityConfig)
			sc.SecurityType = typedReality.Type
			sc.SecuritySettings = []*serial.TypedMessage{typedReality}
		}
	}

	// Socket Settings (Sockopt)
	if cfg.Sockopt != nil {
		var tfo int32
		if cfg.Sockopt.TcpFastOpen {
			tfo = 1 // Enable
		}
		var tproxy internet.SocketConfig_TProxyMode
		switch strings.ToLower(cfg.Sockopt.Tproxy) {
		case "tproxy":
			tproxy = internet.SocketConfig_TProxy
		case "redirect":
			tproxy = internet.SocketConfig_Redirect
		default:
			tproxy = internet.SocketConfig_Off
		}

		var domainStrategy internet.DomainStrategy
		switch strings.ToLower(cfg.Sockopt.DomainStrategy) {
		case "useip":
			domainStrategy = internet.DomainStrategy_USE_IP
		case "useipv4":
			domainStrategy = internet.DomainStrategy_USE_IP4
		case "useipv6":
			domainStrategy = internet.DomainStrategy_USE_IP6
		default:
			domainStrategy = internet.DomainStrategy_AS_IS
		}

		sc.SocketSettings = &internet.SocketConfig{
			Mark:           int32(cfg.Sockopt.Mark),
			Tfo:            tfo,
			Tproxy:         tproxy,
			DomainStrategy: domainStrategy,
			DialerProxy:    cfg.Sockopt.DialerProxy,
			TcpMptcp:       cfg.Sockopt.TcpMptcp,
			Interface:      cfg.Sockopt.Interface,
			V6Only:         cfg.Sockopt.V6Only,
		}
	}

	return sc, nil
}

func buildOutboundProxySettings(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	switch strings.ToLower(cfg.Protocol) {
	case "freedom":
		// Freedom outbound - direct connection
		return serial.ToTypedMessage(&freedom.Config{}), nil
	case "blackhole":
		return serial.ToTypedMessage(&blackhole.Config{}), nil
	case "vless":
		return buildVLESSOutbound(cfg)
	case "vmess":
		return buildVMessOutbound(cfg)
	case "trojan":
		return buildTrojanOutbound(cfg)
	case "socks":
		return buildSocksOutbound(cfg)
	case "shadowsocks":
		return buildShadowsocksOutbound(cfg)
	case "http":
		return buildHTTPOutbound(cfg)
	case "wireguard":
		return buildWireGuardOutbound(cfg)
	default:
		return nil, fmt.Errorf("unsupported outbound protocol: %s", cfg.Protocol)
	}
}

func buildSocksOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	server := &protocol.ServerEndpoint{
		Address: net.NewIPOrDomain(net.ParseAddress(cfg.Address)),
		Port:    cfg.Port,
	}

	if cfg.Username != "" && cfg.Pass != "" {
		account := &socks.Account{
			Username: cfg.Username,
			Password: cfg.Pass,
		}
		user := &protocol.User{
			Account: serial.ToTypedMessage(account),
			Level:   cfg.Level,
			Email:   cfg.Email,
		}
		server.User = user
	}

	return serial.ToTypedMessage(&socks.ClientConfig{
		Server: server,
	}), nil
}

func buildShadowsocksOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	// Normalize cipher method
	methodStr := strings.ToLower(cfg.Method)
	var methodType shadowsocks.CipherType
	switch methodStr {
	case "aes-128-gcm":
		methodType = shadowsocks.CipherType_AES_128_GCM
	case "aes-256-gcm":
		methodType = shadowsocks.CipherType_AES_256_GCM
	case "chacha20-poly1305", "chacha20-ietf-poly1305":
		methodType = shadowsocks.CipherType_CHACHA20_POLY1305
	case "xchacha20-poly1305", "xchacha20-ietf-poly1305":
		methodType = shadowsocks.CipherType_XCHACHA20_POLY1305
	case "none", "plain", "zero":
		// xray-core dropped the unencrypted Shadowsocks ciphers. CipherType_
		// UNKNOWN falls through getCipher()'s default to "Unsupported cipher.",
		// so refuse here with a message that says what actually happened.
		return nil, fmt.Errorf("shadowsocks method %q is no longer supported by xray-core; pick an AEAD cipher", cfg.Method)
	default:
		// Default to AES-128-GCM if unknown
		methodType = shadowsocks.CipherType_AES_128_GCM
	}

	account := &shadowsocks.Account{
		Password:   cfg.Password,
		CipherType: methodType,
		IvCheck:    cfg.IVCheck,
	}

	user := &protocol.User{
		Account: serial.ToTypedMessage(account),
		Level:   cfg.Level,
		Email:   cfg.Email,
	}

	server := &protocol.ServerEndpoint{
		Address: net.NewIPOrDomain(net.ParseAddress(cfg.Address)),
		Port:    cfg.Port,
		User:    user,
	}

	return serial.ToTypedMessage(&shadowsocks.ClientConfig{
		Server: server,
	}), nil
}

func buildHTTPOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	server := &protocol.ServerEndpoint{
		Address: net.NewIPOrDomain(net.ParseAddress(cfg.Address)),
		Port:    cfg.Port,
	}

	if cfg.Username != "" && cfg.Pass != "" {
		account := &http.Account{
			Username: cfg.Username,
			Password: cfg.Pass,
		}
		user := &protocol.User{
			Account: serial.ToTypedMessage(account),
			Level:   cfg.Level,
			Email:   cfg.Email,
		}
		server.User = user
	}

	return serial.ToTypedMessage(&http.ClientConfig{
		Server: server,
	}), nil
}

func buildWireGuardOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	wg := cfg.WireGuard
	if wg == nil {
		return serial.ToTypedMessage(&wireguard.DeviceConfig{IsClient: true}), nil
	}

	var ds wireguard.DeviceConfig_DomainStrategy
	switch wg.DomainStrategy {
	case "ForceIP4":
		ds = wireguard.DeviceConfig_FORCE_IP4
	case "ForceIP6":
		ds = wireguard.DeviceConfig_FORCE_IP6
	case "ForceIP46":
		ds = wireguard.DeviceConfig_FORCE_IP46
	case "ForceIP64":
		ds = wireguard.DeviceConfig_FORCE_IP64
	default:
		ds = wireguard.DeviceConfig_FORCE_IP
	}

	peers := make([]*wireguard.PeerConfig, 0, len(wg.Peers))
	for _, p := range wg.Peers {
		// PeerConfig.KeepAlive changed from uint32 to string upstream; the value
		// is concatenated straight into "persistent_keepalive_interval=" in the
		// WireGuard UAPI config, so it must stay a bare seconds integer. Zero
		// maps to "" (unset) rather than "0".
		keepAlive := ""
		if p.KeepAlive > 0 {
			keepAlive = strconv.Itoa(int(p.KeepAlive))
		}
		peers = append(peers, &wireguard.PeerConfig{
			PublicKey:    p.PublicKey,
			PreSharedKey: p.PreSharedKey,
			Endpoint:     p.Endpoint,
			KeepAlive:    keepAlive,
			AllowedIps:   p.AllowedIPs,
		})
	}

	return serial.ToTypedMessage(&wireguard.DeviceConfig{
		SecretKey: wg.SecretKey,
		Endpoint:  wg.Endpoint,
		Peers:     peers,
		Mtu:       int32(wg.MTU),
		// NumWorkers was removed from wireguard.DeviceConfig upstream; xray-core
		// now sizes its worker pool internally, so wg.NumWorkers is ignored.
		Reserved:       wg.Reserved,
		DomainStrategy: ds,
		IsClient:       true,
		NoKernelTun:    wg.NoKernelTun,
	}), nil
}

func buildVLESSOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	if cfg.Encryption == "" {
		cfg.Encryption = "none"
	}
	account := &vless.Account{
		Id:         cfg.UUID,
		Flow:       cfg.Flow,
		Encryption: cfg.Encryption,
	}

	user := &protocol.User{
		Level:   cfg.Level,
		Email:   cfg.Email,
		Account: serial.ToTypedMessage(account),
	}

	serverEndpoint := &protocol.ServerEndpoint{
		Address: net.NewIPOrDomain(net.ParseAddress(cfg.Address)),
		Port:    cfg.Port,
		User:    user,
	}

	return serial.ToTypedMessage(&vlessOutbound.Config{
		Vnext: serverEndpoint,
	}), nil
}

func buildVMessOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	account := &vmess.Account{
		Id:           cfg.UUID,
		TestsEnabled: cfg.Experiments,
	}

	user := &protocol.User{
		Level:   cfg.Level,
		Email:   cfg.Email,
		Account: serial.ToTypedMessage(account),
	}

	serverEndpoint := &protocol.ServerEndpoint{
		Address: net.NewIPOrDomain(net.ParseAddress(cfg.Address)),
		Port:    cfg.Port,
		User:    user,
	}

	return serial.ToTypedMessage(&vmessOutbound.Config{
		Receiver: serverEndpoint,
	}), nil
}

func buildTrojanOutbound(cfg *OutboundConfig) (*serial.TypedMessage, error) {
	account := &trojan.Account{
		Password: cfg.Password,
	}

	user := &protocol.User{
		Level:   cfg.Level,
		Email:   cfg.Email,
		Account: serial.ToTypedMessage(account),
	}

	server := &protocol.ServerEndpoint{
		Address: net.NewIPOrDomain(net.ParseAddress(cfg.Address)),
		Port:    cfg.Port,
		User:    user,
	}

	return serial.ToTypedMessage(&trojan.ClientConfig{
		Server: server,
	}), nil
}

// === Routing Rule Builder ===

// BuildRoutingRule constructs the protobuf message for a routing rule
func BuildRoutingRule(cfg *RoutingRuleConfig) (*router.RoutingRule, error) {
	rule := &router.RoutingRule{
		RuleTag: cfg.RuleTag,
	}

	// Set target (outbound or balancing)
	if cfg.OutboundTag != "" {
		rule.TargetTag = &router.RoutingRule_Tag{Tag: cfg.OutboundTag}
	} else if cfg.BalancingTag != "" {
		rule.TargetTag = &router.RoutingRule_BalancingTag{BalancingTag: cfg.BalancingTag}
	}

	// Build domain matchers
	for _, d := range cfg.Domains {
		rule.Domain = append(rule.Domain, buildDomainRule(d.Value, d.Type))
	}

	// Build GeoIP matchers
	for _, country := range cfg.GeoIP {
		rule.Ip = append(rule.Ip, buildIPRule(country))
	}

	// Build IP CIDR matchers
	for _, cidr := range cfg.IPCIDR {
		rule.Ip = append(rule.Ip, buildIPRule(cidr))
	}

	// Build destination port list
	if len(cfg.Ports) > 0 {
		portList, err := buildRoutingPortList(cfg.Ports)
		if err != nil {
			return nil, fmt.Errorf("failed to build port list: %w", err)
		}
		rule.PortList = portList
	}

	// Build source port list
	if len(cfg.SourcePorts) > 0 {
		sourcePortList, err := buildRoutingPortList(cfg.SourcePorts)
		if err != nil {
			return nil, fmt.Errorf("failed to build source port list: %w", err)
		}
		rule.SourcePortList = sourcePortList
	}

	// Build source IP matchers (for source IP filtering)
	for _, ip := range cfg.SourceIPs {
		rule.SourceIp = append(rule.SourceIp, buildIPRule(ip))
	}

	// Build network matchers — accept comma-separated entries like
	// ["tcp,udp"] (the form xray-core's JSON config uses), and dedupe.
	seenNet := map[net.Network]bool{}
	for _, n := range cfg.Networks {
		for _, item := range strings.Split(n, ",") {
			switch strings.TrimSpace(strings.ToLower(item)) {
			case "tcp":
				if !seenNet[net.Network_TCP] {
					rule.Networks = append(rule.Networks, net.Network_TCP)
					seenNet[net.Network_TCP] = true
				}
			case "udp":
				if !seenNet[net.Network_UDP] {
					rule.Networks = append(rule.Networks, net.Network_UDP)
					seenNet[net.Network_UDP] = true
				}
			}
		}
	}

	// Set protocols
	rule.Protocol = cfg.Protocols

	// Set inbound tags
	rule.InboundTag = cfg.InboundTags

	// Set user emails
	rule.UserEmail = cfg.UserEmails

	// Set attributes
	rule.Attributes = cfg.Attributes

	// Set process names
	rule.Process = cfg.ProcessNames

	// Build local IP matchers
	for _, ip := range cfg.LocalIPs {
		rule.LocalIp = append(rule.LocalIp, buildIPRule(ip))
	}

	// Build local port list
	if len(cfg.LocalPorts) > 0 {
		localPortList, err := buildRoutingPortList(cfg.LocalPorts)
		if err != nil {
			return nil, fmt.Errorf("failed to build local port list: %w", err)
		}
		rule.LocalPortList = localPortList
	}

	// VLESS Reverse Proxy route ports (Hysteria inbounds honor these too).
	if len(cfg.VlessRoutes) > 0 {
		vlessRouteList, err := buildRoutingPortList(cfg.VlessRoutes)
		if err != nil {
			return nil, fmt.Errorf("failed to build vless route list: %w", err)
		}
		rule.VlessRouteList = vlessRouteList
	}

	// Match notification webhook. xray-core drops a webhook with no url.
	if cfg.WebhookURL != "" {
		rule.Webhook = &router.WebhookConfig{
			Url:           cfg.WebhookURL,
			Deduplication: cfg.WebhookDeduplication,
			Headers:       cfg.WebhookHeaders,
		}
	}

	return rule, nil
}

// buildRoutingPortList parses port strings like "80", "443", "1000-2000"
func buildRoutingPortList(ports []string) (*net.PortList, error) {
	portList := &net.PortList{}

	for _, p := range ports {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Check for port range
		if strings.Contains(p, "-") {
			parts := strings.SplitN(p, "-", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid port range: %s", p)
			}
			from, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", parts[0])
			}
			to, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", parts[1])
			}
			portList.Range = append(portList.Range, &net.PortRange{
				From: uint32(from),
				To:   uint32(to),
			})
		} else {
			// Single port
			port, err := strconv.ParseUint(p, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", p)
			}
			portList.Range = append(portList.Range, &net.PortRange{
				From: uint32(port),
				To:   uint32(port),
			})
		}
	}

	return portList, nil
}

// buildDomainRule converts a configured domain matcher into xray-core's
// geodata.DomainRule form.
//
// Routing rules used to carry a flat router.Domain, where "geosite:cn" and
// "ext:file:code" were passed through verbatim for xray-core to resolve at load
// time. That flat type is gone: geo lookups are now an explicit GeoSiteRule
// oneof branch, so the prefixes have to be split here instead.
//
// This mirrors geodata.ParseDomainRule, minus its checkFile() step — that opens
// geosite.dat from disk and validates the code, which would make config
// generation fail wherever the geo files aren't present. typ is the caller's
// explicit type field and is preserved as-is rather than re-derived from
// xray-style value prefixes.
func buildDomainRule(value, typ string) *geodata.DomainRule {
	file, codeWithAttrs := "", ""
	switch {
	case strings.HasPrefix(value, "geosite:"):
		file, codeWithAttrs = geodata.DefaultGeoSiteDat, strings.TrimPrefix(value, "geosite:")
	case strings.HasPrefix(value, "ext:"), strings.HasPrefix(value, "ext-domain:"), strings.HasPrefix(value, "ext-site:"):
		_, rest, _ := strings.Cut(value, ":")
		if f, c, ok := strings.Cut(rest, ":"); ok {
			file, codeWithAttrs = f, c
		}
	}

	if file != "" && codeWithAttrs != "" {
		code, attrs, _ := strings.Cut(codeWithAttrs, "@")
		return &geodata.DomainRule{Value: &geodata.DomainRule_Geosite{
			Geosite: &geodata.GeoSiteRule{
				File:  file,
				Code:  strings.ToUpper(code),
				Attrs: strings.ToLower(attrs),
			},
		}}
	}

	// Plain matcher. Domain_Plain was renamed to Domain_Substr upstream; both
	// are wire value 0, so the default is unchanged.
	domainType := geodata.Domain_Substr
	switch strings.ToLower(typ) {
	case "regex":
		domainType = geodata.Domain_Regex
	case "domain":
		domainType = geodata.Domain_Domain
	case "full":
		domainType = geodata.Domain_Full
	}
	return &geodata.DomainRule{Value: &geodata.DomainRule_Custom{
		Custom: &geodata.Domain{Type: domainType, Value: value},
	}}
}

// buildIPRule converts a configured IP matcher into xray-core's geodata.IPRule
// form. Routing rules previously used router.GeoIP for both cases, with an empty
// CountryCode signalling "custom CIDR"; that is now an explicit oneof branch.
//
// Mirrors geodata.ParseIPRules minus its checkFile() step, for the same reason
// as buildDomainRule.
func buildIPRule(value string) *geodata.IPRule {
	if code := strings.TrimPrefix(value, "geoip:"); code != value {
		return &geodata.IPRule{Value: &geodata.IPRule_Geoip{
			Geoip: &geodata.GeoIPRule{
				File: geodata.DefaultGeoIPDat,
				Code: strings.ToUpper(code),
			},
		}}
	}
	ip, prefix := parseIPPrefix(value)
	return &geodata.IPRule{Value: &geodata.IPRule_Custom{
		Custom: &geodata.CIDRRule{
			Cidr: &geodata.CIDR{Ip: ip, Prefix: prefix},
		},
	}}
}

// parseIPPrefix splits an IP or CIDR string into xray's wire form: raw address
// bytes (4 for IPv4, 16 for IPv6) plus a prefix length, defaulting to the
// address width when the value carries no mask. Unparseable input is passed
// through as its own bytes, which is what xray-core does with a rule it can't
// read.
func parseIPPrefix(value string) ([]byte, uint32) {
	host, bits, hasBits := strings.Cut(value, "/")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return []byte(host), 32
	}
	// A ::ffff:1.2.3.4 form has to collapse to 4 bytes so the width below
	// matches the bytes we emit.
	addr = addr.Unmap()

	var raw []byte
	if addr.Is4() {
		b := addr.As4()
		raw = b[:]
	} else {
		b := addr.As16()
		raw = b[:]
	}

	if hasBits {
		if n, err := strconv.ParseUint(bits, 10, 32); err == nil {
			return raw, uint32(n)
		}
	}
	return raw, uint32(addr.BitLen())
}

// ValidateOutboundConfig validates the outbound configuration
func ValidateOutboundConfig(cfg *OutboundConfig) error {
	if cfg.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}

	// Skip address/port validation for freedom/blackhole/wireguard
	if cfg.Protocol != "freedom" && cfg.Protocol != "blackhole" && cfg.Protocol != "wireguard" {
		if cfg.Address == "" {
			return fmt.Errorf("address is required for protocol %s", cfg.Protocol)
		}
		if cfg.Port == 0 {
			return fmt.Errorf("port is required for protocol %s", cfg.Protocol)
		}
	}

	switch strings.ToLower(cfg.Protocol) {
	case "vless", "vmess":
		if cfg.UUID == "" {
			return fmt.Errorf("uuid is required for %s", cfg.Protocol)
		}
	case "trojan", "shadowsocks":
		if cfg.Password == "" {
			return fmt.Errorf("password is required for %s", cfg.Protocol)
		}
	}

	return nil
}
