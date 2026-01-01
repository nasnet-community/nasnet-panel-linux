package xray

import (
	"context"
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/xtls/xray-core/app/proxyman"
	handlerService "github.com/xtls/xray-core/app/proxyman/command"
	routerCommand "github.com/xtls/xray-core/app/router/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/proxy/http"
	hysteriaAccount "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/socks"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	vlessOutbound "github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/proxy/vmess"
	vmessOutbound "github.com/xtls/xray-core/proxy/vmess/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/grpc"
	"github.com/xtls/xray-core/transport/internet/httpupgrade"
	"github.com/xtls/xray-core/transport/internet/reality"
	"github.com/xtls/xray-core/transport/internet/splithttp"
	"github.com/xtls/xray-core/transport/internet/tls"
	"github.com/xtls/xray-core/transport/internet/websocket"
)

// AddInbound creates a new inbound on the Xray server
func (c *GRPCClient) AddInbound(ctx context.Context, target string, config *InboundConfig) error {
	log := logger.GetLogger()
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	// Debug log TLS config
	if config.Security == "tls" && config.TLS != nil {
		certLen := len(config.TLS.CertContent)
		keyLen := len(config.TLS.KeyContent)
		log.Infof("AddInbound %s: TLS config - ServerName=%s, CertContentLen=%d, KeyContentLen=%d, CertPath=%s",
			config.Tag, config.TLS.ServerName, certLen, keyLen, config.TLS.CertPath)
		if (certLen == 0 || keyLen == 0) && (config.TLS.CertPath == "" || config.TLS.KeyPath == "") {
			log.Warnf("AddInbound %s: TLS inbound has EMPTY certificate or key (and no paths provided)!", config.Tag)
		} else {
			// Log first 50 chars to verify PEM format
			certStart := config.TLS.CertContent
			if len(certStart) > 50 {
				certStart = certStart[:50]
			}
			keyStart := config.TLS.KeyContent
			if len(keyStart) > 50 {
				keyStart = keyStart[:50]
			}
			log.Infof("AddInbound %s: CertStart='%s...', KeyStart='%s...'", config.Tag, certStart, keyStart)
		}
	}

	// Use the builder to construct the protobuf message
	inboundHandlerConfig, err := BuildInboundHandlerConfig(config)
	if err != nil {
		return fmt.Errorf("failed to build inbound config: %w", err)
	}

	req := &handlerService.AddInboundRequest{
		Inbound: inboundHandlerConfig,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	_, err = client.AddInbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to add inbound %s at %s: %w", config.Tag, target, err)
	}

	log.Infof("Successfully added inbound %s to %s", config.Tag, target)
	return nil
}

// UpdateInbound updates an existing inbound by removing it and re-adding it.
// Note: This causes a momentary disconnection for users on this inbound.
func (c *GRPCClient) UpdateInbound(ctx context.Context, target string, config *InboundConfig) error {
	log := logger.GetLogger()

	// 1. Remove existing inbound
	if err := c.RemoveInbound(ctx, target, config.Tag); err != nil {
		// Log warning but proceed, in case it didn't exist or was partially broken
		log.Warnf("UpdateInbound: Failed to remove inbound %s (might not exist): %v", config.Tag, err)
	}

	// 2. Add new configuration
	if err := c.AddInbound(ctx, target, config); err != nil {
		return fmt.Errorf("failed to re-add inbound %s during update: %w", config.Tag, err)
	}

	log.Infof("Successfully updated inbound %s on %s", config.Tag, target)
	return nil
}

// AddUser adds a user to an inbound handler on a specific target node
func (c *GRPCClient) AddUser(ctx context.Context, target string, inboundTag string, user *User) error {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	// Build the protocol-specific account
	var account *serial.TypedMessage
	switch user.Protocol {
	case ProtocolVMess:
		account = serial.ToTypedMessage(&vmess.Account{
			Id: user.UUID,
		})
	case ProtocolVLESS:
		account = serial.ToTypedMessage(&vless.Account{
			Id:         user.UUID,
			Flow:       user.Flow,
			Encryption: user.Encryption,
		})
	case ProtocolTrojan:
		account = serial.ToTypedMessage(&trojan.Account{
			Password: user.UUID,
		})
	case ProtocolHysteria2, "hysteria":
		account = serial.ToTypedMessage(&hysteriaAccount.Account{
			Auth: user.UUID,
		})
	default:
		return fmt.Errorf("unsupported protocol: %s", user.Protocol)
	}

	// Create the user
	protoUser := &protocol.User{
		Level:   user.Level,
		Email:   user.Email,
		Account: account,
	}

	// Create AddUserOperation
	addUserOp := &handlerService.AddUserOperation{
		User: protoUser,
	}

	// Build the AlterInbound request
	req := &handlerService.AlterInboundRequest{
		Tag:       inboundTag,
		Operation: serial.ToTypedMessage(addUserOp),
	}

	client := handlerService.NewHandlerServiceClient(conn)
	_, err = client.AlterInbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to add user %s to inbound %s at %s: %w", user.Email, inboundTag, target, err)
	}

	// Quiet log to avoid spamming on bulk adds
	// log := logger.GetLogger()
	// log.Debugf("Created xray user account: %s on node %s", user.Email, target)

	return nil
}

// RemoveUser removes a user from an inbound handler on a specific target node
func (c *GRPCClient) RemoveUser(ctx context.Context, target string, inboundTag, email string) error {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	// Create RemoveUserOperation
	removeUserOp := &handlerService.RemoveUserOperation{
		Email: email,
	}

	// Build the AlterInbound request
	req := &handlerService.AlterInboundRequest{
		Tag:       inboundTag,
		Operation: serial.ToTypedMessage(removeUserOp),
	}

	client := handlerService.NewHandlerServiceClient(conn)
	_, err = client.AlterInbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove user %s from inbound %s at %s: %w", email, inboundTag, target, err)
	}

	return nil
}

// GetInboundUsers retrieves users from an inbound handler on a target node
func (c *GRPCClient) GetInboundUsers(ctx context.Context, target string, inboundTag, email string) ([]*User, error) {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return nil, err
	}
	defer closeFunc()

	req := &handlerService.GetInboundUserRequest{
		Tag:   inboundTag,
		Email: email,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	resp, err := client.GetInboundUsers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get users from inbound %s at %s: %w", inboundTag, target, err)
	}

	users := make([]*User, 0, len(resp.Users))
	for _, u := range resp.Users {
		user := &User{
			Email: u.Email,
			Level: u.Level,
		}
		users = append(users, user)
	}

	return users, nil
}

// GetInboundUsersCount returns the count of users on an inbound on a target node
func (c *GRPCClient) GetInboundUsersCount(ctx context.Context, target string, inboundTag string) (int64, error) {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return 0, err
	}
	defer closeFunc()

	req := &handlerService.GetInboundUserRequest{
		Tag: inboundTag,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	resp, err := client.GetInboundUsersCount(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to get users count from inbound %s at %s: %w", inboundTag, target, err)
	}

	return resp.Count, nil
}

// ListInbounds returns all configured inbound handlers from a target node
func (c *GRPCClient) ListInbounds(ctx context.Context, target string, onlyTags bool) ([]*InboundInfo, error) {
	log := logger.GetLogger()
	// log.Debugf("[ListInbounds] Connecting to target: %s, onlyTags: %v", target, onlyTags)

	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		// log.Errorf("[ListInbounds] Failed to dial: %v", err)
		return nil, err
	}
	defer closeFunc()

	req := &handlerService.ListInboundsRequest{
		IsOnlyTags: onlyTags,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	resp, err := client.ListInbounds(ctx, req)
	if err != nil {
		// log.Errorf("[ListInbounds] gRPC call failed: %v", err)
		return nil, fmt.Errorf("failed to list inbounds at %s: %w", target, err)
	}

	// log.Debugf("[ListInbounds] Received %d inbounds from %s", len(resp.Inbounds), target)

	inbounds := make([]*InboundInfo, 0, len(resp.Inbounds))
	for _, in := range resp.Inbounds {
		info := &InboundInfo{
			Tag: in.Tag,
		}
		// log.Debugf("[ListInbounds] Processing inbound tag: %s", in.Tag)

		// Parse ReceiverSettings to extract port, listen, and stream settings
		if in.ReceiverSettings != nil {
			instance, err := in.ReceiverSettings.GetInstance()
			if err != nil {
				log.Warnf("[ListInbounds] Tag %s: Failed to parse ReceiverSettings: %v", in.Tag, err)
			} else if receiverConfig, ok := instance.(*proxyman.ReceiverConfig); ok {
				// Extract port from PortList
				if receiverConfig.PortList != nil && len(receiverConfig.PortList.Range) > 0 {
					info.Port = receiverConfig.PortList.Range[0].From
				}

				// Extract listen address
				if receiverConfig.Listen != nil {
					info.Listen = receiverConfig.Listen.String()
				}

				// Parse StreamSettings for network/security/transport config
				if receiverConfig.StreamSettings != nil {
					parseStreamSettings(info, receiverConfig.StreamSettings)
				}

				// Parse SniffingSettings
				if receiverConfig.SniffingSettings != nil {
					sniff := receiverConfig.SniffingSettings
					info.SniffingEnabled = sniff.Enabled
					info.SniffingMetadataOnly = sniff.MetadataOnly
					info.SniffingRouteOnly = sniff.RouteOnly
					info.SniffingDestOverride = sniff.DestinationOverride
				}
			}
		}

		// Parse ProxySettings to extract protocol type
		if in.ProxySettings != nil {
			info.Protocol = extractProtocolFromTypeName(in.ProxySettings.Type)
		}

		inbounds = append(inbounds, info)
	}

	return inbounds, nil
}

// parseStreamSettings extracts network/security/TLS settings from StreamConfig
func parseStreamSettings(info *InboundInfo, ss *internet.StreamConfig) {
	// Network type
	info.Network = ss.ProtocolName
	if info.Network == "" {
		info.Network = "tcp"
	} else if info.Network == "splithttp" {
		info.Network = "xhttp"
	}

	// Security type - normalize from full protobuf type name
	securityType := strings.ToLower(ss.SecurityType)
	if strings.Contains(securityType, "tls") {
		info.Security = "tls"
	} else if strings.Contains(securityType, "reality") {
		info.Security = "reality"
	} else {
		info.Security = "none"
	}

	// Parse TLS settings if present
	if strings.Contains(strings.ToLower(ss.SecurityType), "tls") && len(ss.SecuritySettings) > 0 {
		instance, err := ss.SecuritySettings[0].GetInstance()
		if err == nil {
			if tlsConfig, ok := instance.(*tls.Config); ok {
				info.TLSConfig = &TLSInfoConfig{
					SNI: tlsConfig.ServerName,
				}
				if len(tlsConfig.NextProtocol) > 0 {
					info.TLSConfig.ALPN = tlsConfig.NextProtocol
				}
				if tlsConfig.Fingerprint != "" {
					info.TLSConfig.Fingerprint = tlsConfig.Fingerprint
				}

				// Extract certificate info if available
				if len(tlsConfig.Certificate) > 0 {
					cert := tlsConfig.Certificate[0]
					if cert.CertificatePath != "" {
						// Path mode
						info.TLSConfig.CertPath = cert.CertificatePath
						info.TLSConfig.KeyPath = cert.KeyPath
					} else if len(cert.Certificate) > 0 {
						// Content mode
						info.TLSConfig.CertContent = string(cert.Certificate)
						info.TLSConfig.KeyContent = string(cert.Key)
					}
				}
			}
		}
	}

	// Parse Reality settings if present
	if strings.Contains(strings.ToLower(ss.SecurityType), "reality") && len(ss.SecuritySettings) > 0 {
		instance, err := ss.SecuritySettings[0].GetInstance()
		if err == nil {
			if realityConfig, ok := instance.(*reality.Config); ok {
				info.RealityConfig = &RealityInfoConfig{
					ServerName:  realityConfig.ServerName,
					Fingerprint: realityConfig.Fingerprint,
					PublicKey:   string(realityConfig.PublicKey),
					ShortID:     string(realityConfig.ShortId),
				}
			}
		}
	}

	// Parse transport-specific settings
	for _, ts := range ss.TransportSettings {
		if ts.Settings == nil {
			continue
		}

		instance, err := ts.Settings.GetInstance()
		if err != nil {
			continue
		}

		switch ss.ProtocolName {
		case "ws", "websocket":
			if wsConfig, ok := instance.(*websocket.Config); ok {
				info.WSPath = wsConfig.Path
				if wsConfig.Host != "" {
					info.WSHost = wsConfig.Host
				}
			}
		case "grpc", "gun":
			if grpcConfig, ok := instance.(*grpc.Config); ok {
				info.GRPCServiceName = grpcConfig.ServiceName
			}
		case "xhttp", "splithttp":
			if xhttpConfig, ok := instance.(*splithttp.Config); ok {
				info.XHTTPPath = xhttpConfig.Path
				info.XHTTPHost = xhttpConfig.Host
				info.XHTTPMode = xhttpConfig.Mode
			}
		case "httpupgrade":
			if huConfig, ok := instance.(*httpupgrade.Config); ok {
				info.HTTPUpgradePath = huConfig.Path
				info.HTTPUpgradeHost = huConfig.Host
			}
		}
	}
}

// extractProtocolFromTypeName extracts the protocol name from ProxySettings.Type
func extractProtocolFromTypeName(typeName string) string {
	switch {
	case strings.Contains(typeName, "vless"):
		return "vless"
	case strings.Contains(typeName, "vmess"):
		return "vmess"
	case strings.Contains(typeName, "trojan"):
		return "trojan"
	case strings.Contains(typeName, "shadowsocks"):
		return "shadowsocks"
	case strings.Contains(typeName, "socks"):
		return "socks"
	case strings.Contains(typeName, "http"):
		return "http"
	case strings.Contains(typeName, "freedom"):
		return "freedom"
	case strings.Contains(typeName, "blackhole"):
		return "blackhole"
	default:
		return "unknown"
	}
}

// RemoveInbound removes an inbound handler by tag from a target node
func (c *GRPCClient) RemoveInbound(ctx context.Context, target string, tag string) error {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	req := &handlerService.RemoveInboundRequest{
		Tag: tag,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	_, err = client.RemoveInbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove inbound %s at %s: %w", tag, target, err)
	}

	return nil
}

// === Outbound Operations ===

// AddOutbound creates a new outbound on the Xray server
func (c *GRPCClient) AddOutbound(ctx context.Context, target string, config *OutboundConfig) error {
	log := logger.GetLogger()
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	outboundHandlerConfig, err := BuildOutboundHandlerConfig(config)
	if err != nil {
		return fmt.Errorf("failed to build outbound config: %w", err)
	}

	req := &handlerService.AddOutboundRequest{
		Outbound: outboundHandlerConfig,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	_, err = client.AddOutbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to add outbound %s at %s: %w", config.Tag, target, err)
	}

	log.Infof("Successfully added outbound %s to %s", config.Tag, target)
	return nil
}

// RemoveOutbound removes an outbound handler by tag from a target node
func (c *GRPCClient) RemoveOutbound(ctx context.Context, target string, tag string) error {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	req := &handlerService.RemoveOutboundRequest{
		Tag: tag,
	}

	client := handlerService.NewHandlerServiceClient(conn)
	_, err = client.RemoveOutbound(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove outbound %s at %s: %w", tag, target, err)
	}

	return nil
}

// ListOutbounds returns all configured outbound handlers from a target node
func (c *GRPCClient) ListOutbounds(ctx context.Context, target string) ([]*OutboundInfo, error) {
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return nil, err
	}
	defer closeFunc()

	req := &handlerService.ListOutboundsRequest{}

	client := handlerService.NewHandlerServiceClient(conn)
	resp, err := client.ListOutbounds(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list outbounds at %s: %w", target, err)
	}

	log := logger.GetLogger()
	log.Infof("[ListOutbounds] Received %d outbounds from %s", len(resp.Outbounds), target)

	outbounds := make([]*OutboundInfo, 0, len(resp.Outbounds))
	for _, out := range resp.Outbounds {
		info := &OutboundInfo{
			Tag: out.Tag,
		}

		// Parse ProxySettings to extract protocol type and server address/port
		if out.ProxySettings != nil {
			info.Protocol = extractProtocolFromTypeName(out.ProxySettings.Type)
			log.Infof("[ListOutbounds] Tag=%s, Type=%s, Protocol=%s", out.Tag, out.ProxySettings.Type, info.Protocol)
			parseOutboundProxySettings(info, out.ProxySettings)
		} else {
			log.Warnf("[ListOutbounds] Tag=%s has nil ProxySettings", out.Tag)
		}

		// Parse SenderSettings to extract stream settings
		if out.SenderSettings != nil {
			instance, err := out.SenderSettings.GetInstance()
			if err == nil {
				if senderConfig, ok := instance.(*proxyman.SenderConfig); ok {
					if senderConfig.StreamSettings != nil {
						parseOutboundStreamSettings(info, senderConfig.StreamSettings)
					}
				}
			}
		}

		log.Infof("[ListOutbounds] Parsed: Tag=%s, Protocol=%s, Address=%s, Port=%d, Username=%s",
			info.Tag, info.Protocol, info.Address, info.Port, info.Username)

		outbounds = append(outbounds, info)
	}

	return outbounds, nil
}

// parseOutboundStreamSettings extracts network/security/TLS/Reality settings from StreamConfig for outbound
func parseOutboundStreamSettings(info *OutboundInfo, ss *internet.StreamConfig) {
	// Network type
	info.Network = ss.ProtocolName
	if info.Network == "" {
		info.Network = "tcp"
	} else if info.Network == "splithttp" {
		info.Network = "xhttp"
	}

	// Security type
	securityType := strings.ToLower(ss.SecurityType)
	if strings.Contains(securityType, "tls") {
		info.Security = "tls"
	} else if strings.Contains(securityType, "reality") {
		info.Security = "reality"
	} else {
		info.Security = "none"
	}

	// Parse TLS settings
	if strings.Contains(securityType, "tls") && len(ss.SecuritySettings) > 0 {
		instance, err := ss.SecuritySettings[0].GetInstance()
		if err == nil {
			if tlsCfg, ok := instance.(*tls.Config); ok {
				info.TLSServerName = tlsCfg.ServerName
				info.TLSFingerprint = tlsCfg.Fingerprint
				info.TLSALPN = tlsCfg.NextProtocol
				// tls.Config.AllowInsecure no longer exists upstream, so it
				// cannot be read back from a live config. info.AllowInsecure
				// stays false.
			}
		}
	}

	// Parse Reality settings
	if strings.Contains(securityType, "reality") && len(ss.SecuritySettings) > 0 {
		instance, err := ss.SecuritySettings[0].GetInstance()
		if err == nil {
			if realityCfg, ok := instance.(*reality.Config); ok {
				info.RealityServerName = realityCfg.ServerName
				info.RealityFingerprint = realityCfg.Fingerprint
				info.RealityPublicKey = string(realityCfg.PublicKey)
				info.RealityShortID = string(realityCfg.ShortId)
				info.RealitySpiderX = realityCfg.SpiderX
			}
		}
	}

	// Parse transport settings
	for _, ts := range ss.TransportSettings {
		if ts.Settings == nil {
			continue
		}
		instance, err := ts.Settings.GetInstance()
		if err != nil {
			continue
		}

		switch ss.ProtocolName {
		case "ws", "websocket":
			if wsConfig, ok := instance.(*websocket.Config); ok {
				info.WSPath = wsConfig.Path
				info.WSHost = wsConfig.Host
			}
		case "grpc", "gun":
			if grpcConfig, ok := instance.(*grpc.Config); ok {
				info.GRPCServiceName = grpcConfig.ServiceName
			}
		case "xhttp", "splithttp":
			if xhttpConfig, ok := instance.(*splithttp.Config); ok {
				info.XHTTPPath = xhttpConfig.Path
				info.XHTTPHost = xhttpConfig.Host
				info.XHTTPMode = xhttpConfig.Mode
			}
		case "httpupgrade":
			if huConfig, ok := instance.(*httpupgrade.Config); ok {
				info.HTTPUpgradePath = huConfig.Path
				info.HTTPUpgradeHost = huConfig.Host
			}
		}
	}

	// Parse SocketSettings (sockopt)
	if ss.SocketSettings != nil {
		sockopt := ss.SocketSettings
		info.SockoptMark = uint32(sockopt.Mark)
		info.SockoptTcpFastOpen = sockopt.Tfo > 0
		info.SockoptTproxy = sockopt.Tproxy.String()
		info.SockoptDomainStrategy = sockopt.DomainStrategy.String()
		info.SockoptDialerProxy = sockopt.DialerProxy
		info.SockoptTcpMptcp = sockopt.TcpMptcp
		info.SockoptInterface = sockopt.Interface
	}
}

// parseOutboundProxySettings extracts server address/port from protocol-specific outbound configs
func parseOutboundProxySettings(info *OutboundInfo, proxySettings *serial.TypedMessage) {
	if proxySettings == nil {
		return
	}

	instance, err := proxySettings.GetInstance()
	if err != nil {
		return
	}

	// Extract ServerEndpoint from different protocol configs
	switch cfg := instance.(type) {
	case *socks.ClientConfig:
		if cfg.Server != nil {
			extractServerEndpoint(info, cfg.Server)
		}
	case *http.ClientConfig:
		if cfg.Server != nil {
			extractServerEndpoint(info, cfg.Server)
		}
	case *shadowsocks.ClientConfig:
		if cfg.Server != nil {
			extractServerEndpoint(info, cfg.Server)
		}
	case *vlessOutbound.Config:
		if cfg.Vnext != nil {
			extractServerEndpoint(info, cfg.Vnext)
		}
	case *vmessOutbound.Config:
		if cfg.Receiver != nil {
			extractServerEndpoint(info, cfg.Receiver)
		}
	case *trojan.ClientConfig:
		if cfg.Server != nil {
			extractServerEndpoint(info, cfg.Server)
		}
	case *freedom.Config:
		info.DomainStrategy = cfg.DomainStrategy.String()
		if cfg.DestinationOverride != nil && cfg.DestinationOverride.Server != nil {
			dest := cfg.DestinationOverride.Server
			if dest.Address != nil {
				info.Redirect = dest.Address.String()
			}
		}
	}
}

// extractServerEndpoint extracts address/port and user credentials from a protocol.ServerEndpoint
func extractServerEndpoint(info *OutboundInfo, endpoint *protocol.ServerEndpoint) {
	if endpoint == nil {
		return
	}

	if endpoint.Address != nil {
		// Use AsAddress().String() to get properly formatted IP address
		addr := endpoint.Address.AsAddress()
		if addr != nil {
			info.Address = addr.String()
		}
	}
	info.Port = endpoint.Port

	// Parse user's Level and Email
	if endpoint.User != nil {
		info.Level = endpoint.User.Level
		info.Email = endpoint.User.Email

		// Parse account for credentials
		if endpoint.User.Account != nil {
			parseUserAccount(info, endpoint.User.Account)
		}
	}
}

// parseUserAccount extracts credentials from protocol-specific account types
func parseUserAccount(info *OutboundInfo, account *serial.TypedMessage) {
	if account == nil {
		return
	}

	instance, err := account.GetInstance()
	if err != nil {
		return
	}

	switch acc := instance.(type) {
	case *socks.Account:
		info.Username = acc.Username
		info.Password = acc.Password
	case *vless.Account:
		info.UUID = acc.Id
		info.Flow = acc.Flow
		info.Encryption = acc.Encryption
	case *vmess.Account:
		info.UUID = acc.Id
		if acc.SecuritySettings != nil {
			info.Encryption = acc.SecuritySettings.Type.String()
		}
	case *trojan.Account:
		info.Password = acc.Password
	case *shadowsocks.Account:
		info.Password = acc.Password
		info.Method = acc.CipherType.String()
		info.IVCheck = acc.IvCheck
	}
}

// === Routing Rule Operations ===

// AddRoutingRule adds a new routing rule to the Xray router
func (c *GRPCClient) AddRoutingRule(ctx context.Context, target string, cfg *RoutingRuleConfig) error {
	log := logger.GetLogger()
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	// Build the routing rule
	rule, err := BuildRoutingRule(cfg)
	if err != nil {
		return fmt.Errorf("failed to build routing rule: %w", err)
	}

	req := &routerCommand.AddRuleRequest{
		Config:       serial.ToTypedMessage(rule),
		ShouldAppend: cfg.ShouldAppend,
	}

	client := routerCommand.NewRoutingServiceClient(conn)
	_, err = client.AddRule(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to add routing rule %s: %w", cfg.RuleTag, err)
	}

	log.Infof("Successfully added routing rule %s to %s (target: %s)", cfg.RuleTag, target, cfg.OutboundTag)
	return nil
}

// RemoveRoutingRule removes a routing rule by its tag
func (c *GRPCClient) RemoveRoutingRule(ctx context.Context, target string, ruleTag string) error {
	log := logger.GetLogger()
	conn, ctx, closeFunc, err := c.dial(ctx, target)
	if err != nil {
		return err
	}
	defer closeFunc()

	req := &routerCommand.RemoveRuleRequest{
		RuleTag: ruleTag,
	}

	client := routerCommand.NewRoutingServiceClient(conn)
	_, err = client.RemoveRule(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove routing rule %s: %w", ruleTag, err)
	}

	log.Infof("Successfully removed routing rule %s from %s", ruleTag, target)
	return nil
}
