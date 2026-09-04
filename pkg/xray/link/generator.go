package link

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// Generate converts an Outbound struct into a shareable link string
func Generate(outbound *domain.Outbound) (string, error) {
	if outbound == nil {
		return "", fmt.Errorf("outbound is nil")
	}

	switch strings.ToLower(outbound.Protocol) {
	case "vless":
		return generateVLESS(outbound)
	case "vmess":
		return generateVMess(outbound)
	case "trojan":
		return generateTrojan(outbound)
	case "shadowsocks":
		return generateShadowsocks(outbound)
	case "socks":
		return generateSocks(outbound)
	case "http":
		return generateHTTP(outbound)
	case "wireguard":
		return generateWireGuard(outbound)
	case "hysteria2":
		return generateHysteria2(outbound)
	default:
		return "", fmt.Errorf("unsupported protocol for link generation: %s", outbound.Protocol)
	}
}

func generateVLESS(o *domain.Outbound) (string, error) {
	settings := o.GetVLESSSettingsOrDefault()
	if settings.UUID == "" {
		return "", fmt.Errorf("missing UUID for VLESS")
	}

	// Build query parameters
	params := url.Values{}

	// Encryption
	if settings.Encryption != "" {
		params.Set("encryption", settings.Encryption)
	} else {
		params.Set("encryption", "none")
	}

	// Security
	if o.Security != "" && o.Security != "none" {
		params.Set("security", o.Security)
	}

	// TLS settings
	if o.Security == "tls" {
		tlsSettings := o.GetTLSSettingsOrDefault()
		if tlsSettings != nil {
			if tlsSettings.ServerName != "" {
				params.Set("sni", tlsSettings.ServerName)
			}
			if tlsSettings.Fingerprint != "" {
				params.Set("fp", tlsSettings.Fingerprint)
			}
			if len(tlsSettings.ALPN) > 0 {
				params.Set("alpn", strings.Join(tlsSettings.ALPN, ","))
			}
		}
	}

	// Reality settings
	if o.Security == "reality" {
		realitySettings := o.GetRealitySettingsOrDefault()
		if realitySettings != nil {
			if realitySettings.ServerName != "" {
				params.Set("sni", realitySettings.ServerName)
			}
			if realitySettings.Fingerprint != "" {
				params.Set("fp", realitySettings.Fingerprint)
			}
			if realitySettings.PublicKey != "" {
				params.Set("pbk", realitySettings.PublicKey)
			}
			if realitySettings.ShortID != "" {
				params.Set("sid", realitySettings.ShortID)
			}
			if realitySettings.SpiderX != "" {
				params.Set("spx", realitySettings.SpiderX)
			}
			if len(realitySettings.ALPN) > 0 {
				params.Set("alpn", strings.Join(realitySettings.ALPN, ","))
			}
		}
	}

	// Network / Transport
	if o.Network != "" {
		params.Set("type", o.Network)
	} else {
		params.Set("type", "tcp")
	}

	// Transport settings
	transportSettings := o.GetTransportSettingsOrDefault()
	if transportSettings != nil {
		switch o.Network {
		case "ws":
			if transportSettings.Path != "" {
				params.Set("path", transportSettings.Path)
			}
			if transportSettings.Host != "" {
				params.Set("host", transportSettings.Host)
			}
		case "grpc":
			if transportSettings.ServiceName != "" {
				params.Set("serviceName", transportSettings.ServiceName)
			}
		case "xhttp", "splithttp":
			if transportSettings.Path != "" {
				params.Set("path", transportSettings.Path)
			}
			if transportSettings.Host != "" {
				params.Set("host", transportSettings.Host)
			}
			if transportSettings.Mode != "" {
				params.Set("mode", transportSettings.Mode)
			}
		case "tcp":
			// The HTTP camouflage header is built from all three of these; a
			// link carrying only headerType probes with an empty path and Host,
			// which the peer inbound rejects as a header mismatch.
			if transportSettings.HeaderType == "http" {
				params.Set("headerType", "http")
				if transportSettings.Path != "" {
					params.Set("path", transportSettings.Path)
				}
				if transportSettings.Host != "" {
					params.Set("host", transportSettings.Host)
				}
			}
		}
	}

	// Flow (for XTLS)
	if settings.Flow != "" {
		params.Set("flow", settings.Flow)
	}

	// Build the link
	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	link := fmt.Sprintf("vless://%s@%s:%d?%s#%s",
		settings.UUID,
		o.Address,
		o.Port,
		params.Encode(),
		url.PathEscape(fragment),
	)

	return link, nil
}

func generateVMess(o *domain.Outbound) (string, error) {
	settings := o.GetVMessSettingsOrDefault()
	if settings.UUID == "" {
		return "", fmt.Errorf("missing UUID for VMess")
	}

	// Build VMess JSON object
	vmessObj := map[string]interface{}{
		"v":    "2",
		"ps":   o.Remark,
		"add":  o.Address,
		"port": o.Port,
		"id":   settings.UUID,
		"aid":  settings.AlterId,
		"scy":  "auto", // Default
		"net":  o.Network,
		"type": "none",
		"host": "",
		"path": "",
		"tls":  "",
		"sni":  "",
		"alpn": "",
		"fp":   "",
	}

	if settings.Security != "" {
		vmessObj["scy"] = settings.Security
	}

	if o.Network == "" {
		vmessObj["net"] = "tcp"
	}

	// TLS
	if o.Security == "tls" {
		vmessObj["tls"] = "tls"
		tlsSettings := o.GetTLSSettingsOrDefault()
		if tlsSettings != nil {
			vmessObj["sni"] = tlsSettings.ServerName
			vmessObj["fp"] = tlsSettings.Fingerprint
			if len(tlsSettings.ALPN) > 0 {
				vmessObj["alpn"] = strings.Join(tlsSettings.ALPN, ",")
			}
		}
	} else if o.Security == "reality" {
		vmessObj["tls"] = "reality"
		realitySettings := o.GetRealitySettingsOrDefault()
		if realitySettings != nil {
			vmessObj["sni"] = realitySettings.ServerName
			vmessObj["fp"] = realitySettings.Fingerprint
			// VMess reality link usually not standard in JSON, but if used:
		}
	}

	// Transport settings
	transportSettings := o.GetTransportSettingsOrDefault()
	if transportSettings != nil {
		vmessObj["host"] = transportSettings.Host
		vmessObj["path"] = transportSettings.Path
		if o.Network == "grpc" {
			vmessObj["path"] = transportSettings.ServiceName
		}
		// In a vmess link "net" is the transport and "type" is the header
		// obfuscation, but only for tcp — for xhttp and grpc the same field
		// means the mode, so writing a header type there breaks the config.
		if o.Network == "tcp" && transportSettings.HeaderType != "" {
			vmessObj["type"] = transportSettings.HeaderType
		}
	}

	// Encode to JSON then base64
	jsonData, err := json.Marshal(vmessObj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal vmess json: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString(jsonData)
	return "vmess://" + encoded, nil
}

func generateTrojan(o *domain.Outbound) (string, error) {
	settings := o.GetTrojanSettingsOrDefault()
	if settings.Password == "" {
		return "", fmt.Errorf("missing password for Trojan")
	}

	params := url.Values{}

	// Security
	if o.Security != "" && o.Security != "none" {
		params.Set("security", o.Security)
	}

	// TLS settings
	if o.Security == "tls" {
		tlsSettings := o.GetTLSSettingsOrDefault()
		if tlsSettings != nil {
			if tlsSettings.ServerName != "" {
				params.Set("sni", tlsSettings.ServerName)
			}
			if tlsSettings.Fingerprint != "" {
				params.Set("fp", tlsSettings.Fingerprint)
			}
			if len(tlsSettings.ALPN) > 0 {
				params.Set("alpn", strings.Join(tlsSettings.ALPN, ","))
			}
		}
	}

	// Reality settings
	if o.Security == "reality" {
		realitySettings := o.GetRealitySettingsOrDefault()
		if realitySettings != nil {
			if realitySettings.ServerName != "" {
				params.Set("sni", realitySettings.ServerName)
			}
			if realitySettings.Fingerprint != "" {
				params.Set("fp", realitySettings.Fingerprint)
			}
			if realitySettings.PublicKey != "" {
				params.Set("pbk", realitySettings.PublicKey)
			}
			if realitySettings.ShortID != "" {
				params.Set("sid", realitySettings.ShortID)
			}
			if realitySettings.SpiderX != "" {
				params.Set("spx", realitySettings.SpiderX)
			}
			if len(realitySettings.ALPN) > 0 {
				params.Set("alpn", strings.Join(realitySettings.ALPN, ","))
			}
		}
	}

	// Network
	if o.Network != "" {
		params.Set("type", o.Network)
	}

	// Transport settings
	transportSettings := o.GetTransportSettingsOrDefault()
	if transportSettings != nil {
		switch o.Network {
		case "ws":
			if transportSettings.Path != "" {
				params.Set("path", transportSettings.Path)
			}
			if transportSettings.Host != "" {
				params.Set("host", transportSettings.Host)
			}
		case "grpc":
			if transportSettings.ServiceName != "" {
				params.Set("serviceName", transportSettings.ServiceName)
			}
		case "tcp":
			// See generateVLESS: the camouflage header needs all three params.
			if transportSettings.HeaderType == "http" {
				params.Set("headerType", "http")
				if transportSettings.Path != "" {
					params.Set("path", transportSettings.Path)
				}
				if transportSettings.Host != "" {
					params.Set("host", transportSettings.Host)
				}
			}
		}
	}

	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	link := fmt.Sprintf("trojan://%s@%s:%d?%s#%s",
		url.PathEscape(settings.Password),
		o.Address,
		o.Port,
		params.Encode(),
		url.PathEscape(fragment),
	)

	return link, nil
}

func generateShadowsocks(o *domain.Outbound) (string, error) {
	settings := o.GetShadowsocksSettingsOrDefault()
	if settings.Password == "" {
		return "", fmt.Errorf("missing password for Shadowsocks")
	}

	method := settings.Method
	if method == "" {
		method = "aes-128-gcm"
	}

	// SIP002 format: ss://base64(method:password)@host:port#tag
	userInfo := fmt.Sprintf("%s:%s", method, settings.Password)
	encoded := base64.URLEncoding.EncodeToString([]byte(userInfo))

	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	link := fmt.Sprintf("ss://%s@%s:%d#%s",
		encoded,
		o.Address,
		o.Port,
		url.PathEscape(fragment),
	)

	return link, nil
}

func generateSocks(o *domain.Outbound) (string, error) {
	settings := o.GetSOCKSSettingsOrDefault()

	var userInfo string
	if settings.Auth == "password" && len(settings.Accounts) > 0 {
		userInfo = fmt.Sprintf("%s:%s@",
			url.PathEscape(settings.Accounts[0].User),
			url.PathEscape(settings.Accounts[0].Pass),
		)
	}

	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	link := fmt.Sprintf("socks://%s%s:%d#%s",
		userInfo,
		o.Address,
		o.Port,
		url.PathEscape(fragment),
	)

	return link, nil
}

// generateWireGuard builds a wireguard:// link. Field names and encoding mirror
// xray-knife's own parser (pkg/core/xray/wireguard.go): the secret key goes in
// userinfo (path-unescaped on the way back), the peer endpoint is the host, and
// the local addresses live in the "address" query param.
func generateWireGuard(o *domain.Outbound) (string, error) {
	settings := o.GetWireGuardSettingsOrDefault()
	if settings.SecretKey == "" {
		return "", fmt.Errorf("missing secretKey for WireGuard")
	}
	if len(settings.Peers) == 0 {
		return "", fmt.Errorf("missing peer for WireGuard")
	}
	peer := settings.Peers[0]
	if peer.PublicKey == "" {
		return "", fmt.Errorf("missing peer publicKey for WireGuard")
	}
	// Without local addresses the parser ends up with a single empty address,
	// which xray rejects with an opaque config error further downstream.
	if len(settings.Endpoint) == 0 {
		return "", fmt.Errorf("missing local addresses for WireGuard")
	}

	// The peer's own endpoint wins; fall back to the outbound's address:port.
	endpoint := peer.Endpoint
	if endpoint == "" {
		if o.Address == "" || o.Port == 0 {
			return "", fmt.Errorf("missing endpoint for WireGuard")
		}
		endpoint = fmt.Sprintf("%s:%d", o.Address, o.Port)
	}

	params := url.Values{}
	params.Set("publickey", peer.PublicKey)
	if peer.PreSharedKey != "" {
		params.Set("presharedkey", peer.PreSharedKey)
	}
	if len(settings.Endpoint) > 0 { // local addresses (CIDR), despite the name
		params.Set("address", strings.Join(settings.Endpoint, ","))
	}
	if settings.MTU > 0 {
		params.Set("mtu", strconv.Itoa(settings.MTU))
	}
	if peer.KeepAlive > 0 {
		params.Set("keepalive", strconv.Itoa(peer.KeepAlive))
	}
	if len(peer.AllowedIPs) > 0 {
		params.Set("allowedips", strings.Join(peer.AllowedIPs, ","))
	}
	if len(settings.Reserved) > 0 {
		reserved := make([]string, len(settings.Reserved))
		for i, r := range settings.Reserved {
			reserved[i] = strconv.Itoa(r)
		}
		params.Set("reserved", strings.Join(reserved, ","))
	}

	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	link := url.URL{
		Scheme:   "wireguard",
		User:     url.User(settings.SecretKey),
		Host:     endpoint,
		RawQuery: params.Encode(),
		Fragment: fragment,
	}
	return link.String(), nil
}

// generateHysteria2 builds a hysteria2:// link for xray-knife, which tests
// Hysteria2 through its embedded sing-box core. The parser reads the auth string
// straight out of userinfo without unescaping, so characters that would break
// the URI are rejected rather than silently mangled.
func generateHysteria2(o *domain.Outbound) (string, error) {
	settings := o.GetHysteriaSettingsOrDefault()
	if settings.Auth == "" {
		return "", fmt.Errorf("missing auth for Hysteria2")
	}
	if o.Address == "" || o.Port == 0 {
		return "", fmt.Errorf("missing address/port for Hysteria2")
	}
	// The parser reads the auth back as the re-encoded userinfo without
	// unescaping it, so any value that does not survive that round-trip would
	// authenticate with a silently different password.
	if url.User(settings.Auth).String() != settings.Auth {
		return "", fmt.Errorf("Hysteria2 auth contains characters that cannot be expressed in a link")
	}

	params := url.Values{}
	if tlsSettings := o.GetTLSSettingsOrDefault(); tlsSettings != nil && tlsSettings.ServerName != "" {
		params.Set("sni", tlsSettings.ServerName)
	}
	// Deliberately no insecure= param: the tester treats it as a hard override
	// that would outrank the node's own TLS verification setting.

	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	return fmt.Sprintf("hysteria2://%s@%s:%d?%s#%s",
		settings.Auth,
		o.Address,
		o.Port,
		params.Encode(),
		url.PathEscape(fragment),
	), nil
}

func generateHTTP(o *domain.Outbound) (string, error) {
	settings := o.GetHTTPSettingsOrDefault()

	var userInfo string
	if len(settings.Accounts) > 0 {
		userInfo = fmt.Sprintf("%s:%s@",
			url.PathEscape(settings.Accounts[0].User),
			url.PathEscape(settings.Accounts[0].Pass),
		)
	}

	fragment := o.Remark
	if fragment == "" {
		fragment = o.Tag
	}

	link := fmt.Sprintf("http://%s%s:%d#%s",
		userInfo,
		o.Address,
		o.Port,
		url.PathEscape(fragment),
	)

	return link, nil
}
