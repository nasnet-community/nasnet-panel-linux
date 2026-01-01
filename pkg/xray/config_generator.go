package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// GenerateConfigLink creates a proper config link from InboundInfo
// uuid: the user's UUID to insert into the link
// host: the server's public hostname/IP
// remark: the display name for the config
func GenerateConfigLink(info *InboundInfo, uuid, host string, remark string) (string, error) {
	if info == nil {
		return "", fmt.Errorf("inbound info is nil")
	}

	port := fmt.Sprintf("%d", info.Port)

	switch strings.ToLower(info.Protocol) {
	case "vless":
		return generateVlessLink(info, uuid, host, port, remark), nil
	case "vmess":
		return generateVmessLink(info, uuid, host, port, remark), nil
	case "trojan":
		return generateTrojanLink(info, uuid, host, port, remark), nil
	case "hysteria2", "hysteria":
		return generateHysteria2Link(info, uuid, host, port, remark), nil
	case "wireguard":
		return generateWireGuardLink(info, host, port, remark), nil
	default:
		return "", fmt.Errorf("unsupported protocol: %s", info.Protocol)
	}
}

// generateWireGuardLink builds a wireguard:// client URI
func generateWireGuardLink(info *InboundInfo, host, port, remark string) string {
	if info.WGPrivateKey == "" || info.WGServerPublicKey == "" {
		return ""
	}
	params := url.Values{}
	params.Set("publickey", info.WGServerPublicKey)
	if info.WGPresharedKey != "" {
		params.Set("presharedkey", info.WGPresharedKey)
	}
	addr := info.WGAddress
	if addr != "" && !strings.Contains(addr, "/") {
		addr += "/32"
	}
	if addr != "" {
		params.Set("address", addr)
	}
	if info.WGMTU > 0 {
		params.Set("mtu", strconv.Itoa(info.WGMTU))
	}
	if len(info.WGReserved) == 3 {
		params.Set("reserved", fmt.Sprintf("%d,%d,%d", info.WGReserved[0], info.WGReserved[1], info.WGReserved[2]))
	}
	// The client private key is base64 (has +/=/); url.User encodes it safely.
	return fmt.Sprintf("wireguard://%s@%s:%s?%s#%s",
		url.User(info.WGPrivateKey).String(), host, port, params.Encode(), url.PathEscape(remark))
}

// generateHysteria2Link builds a hysteria2:// client URI
func generateHysteria2Link(info *InboundInfo, uuid, host, port, remark string) string {
	params := url.Values{}
	if info.TLSConfig != nil {
		if info.TLSConfig.SNI != "" {
			params.Set("sni", info.TLSConfig.SNI)
		}
		if info.TLSConfig.AllowInsecure {
			params.Set("insecure", "1")
		}
	}
	// client must have the same password as the server
	if info.HysteriaObfsPassword != "" {
		params.Set("obfs", "salamander")
		params.Set("obfs-password", info.HysteriaObfsPassword)
	}
	// Port hopping
	if info.PortRange != "" {
		params.Set("mport", info.PortRange)
	}
	queryStr := ""
	if len(params) > 0 {
		queryStr = "?" + params.Encode()
	}
	return fmt.Sprintf("hysteria2://%s@%s:%s%s#%s",
		uuid, host, port, queryStr, url.PathEscape(remark))
}

// applyTLSParams sets sni/alpn/fp/allowInsecure on the query string when the
// inbound actually negotiates TLS. The cross-protocol logic is the same so we
// share it between VLESS, Trojan, and VMess (URI form) generators.
func applyTLSParams(params url.Values, info *InboundInfo) {
	if info.TLSConfig == nil {
		return
	}
	if info.TLSConfig.SNI != "" {
		params.Set("sni", info.TLSConfig.SNI)
	}
	if len(info.TLSConfig.ALPN) > 0 {
		params.Set("alpn", strings.Join(info.TLSConfig.ALPN, ","))
	}
	if info.TLSConfig.Fingerprint != "" {
		params.Set("fp", info.TLSConfig.Fingerprint)
	}
	if info.TLSConfig.AllowInsecure {
		params.Set("allowInsecure", "1")
	}
}

// applyRealityParams sets sni/pbk/sid/fp/spx when the inbound runs Reality.
func applyRealityParams(params url.Values, info *InboundInfo) {
	if info.RealityConfig == nil {
		return
	}
	if info.RealityConfig.ServerName != "" {
		params.Set("sni", info.RealityConfig.ServerName)
	}
	if info.RealityConfig.PublicKey != "" {
		params.Set("pbk", info.RealityConfig.PublicKey)
	}
	if info.RealityConfig.ShortID != "" {
		params.Set("sid", info.RealityConfig.ShortID)
	}
	if info.RealityConfig.Fingerprint != "" {
		params.Set("fp", info.RealityConfig.Fingerprint)
	}
	if info.RealityConfig.SpiderX != "" {
		params.Set("spx", info.RealityConfig.SpiderX)
	}
}

// applyTransportParams sets path/host/serviceName/mode/headerType for the
// configured network transport. Identical wire format across VLESS/VMess/Trojan
// URI variants, so factoring is safe.
func applyTransportParams(params url.Values, info *InboundInfo) {
	switch getNetworkType(info.Network) {
	case "ws":
		if info.WSPath != "" {
			params.Set("path", info.WSPath)
		}
		if info.WSHost != "" {
			params.Set("host", info.WSHost)
		}
	case "grpc":
		if info.GRPCServiceName != "" {
			params.Set("serviceName", info.GRPCServiceName)
		}
	case "tcp":
		if info.HeaderType != "" && info.HeaderType != "none" {
			params.Set("headerType", info.HeaderType)
		}
		if info.HTTPPath != "" {
			params.Set("path", info.HTTPPath)
		}
	case "http", "h2":
		if info.HTTPPath != "" {
			params.Set("path", info.HTTPPath)
		}
	case "xhttp", "splithttp":
		if info.XHTTPPath != "" {
			params.Set("path", info.XHTTPPath)
		}
		if info.XHTTPHost != "" {
			params.Set("host", info.XHTTPHost)
		}
		if info.XHTTPMode != "" {
			params.Set("mode", info.XHTTPMode)
		}
	case "httpupgrade":
		if info.HTTPUpgradePath != "" {
			params.Set("path", info.HTTPUpgradePath)
		}
		if info.HTTPUpgradeHost != "" {
			params.Set("host", info.HTTPUpgradeHost)
		}
	}
}

// applyFragmentParams encodes anti-censorship fragment settings as a single
// fragment=packets,length,interval query value. Empty Fragment is a no-op.
func applyFragmentParams(params url.Values, info *InboundInfo) {
	if info.Fragment == nil {
		return
	}
	parts := []string{info.Fragment.Packets, info.Fragment.Length, info.Fragment.Interval}
	hasAny := false
	for _, p := range parts {
		if p != "" {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}
	params.Set("fragment", strings.Join(parts, ","))
}

// generateVlessLink creates a VLESS link
func generateVlessLink(info *InboundInfo, uuid, host, port, remark string) string {
	params := url.Values{}

	// Security
	security := getSecurity(info.Security)
	params.Set("security", security)

	// Network type
	network := getNetworkType(info.Network)
	params.Set("type", network)

	// Encryption (use MLKEM/custom encryption if set, otherwise "none")
	if info.VLESSEncryption != "" {
		params.Set("encryption", info.VLESSEncryption)
	} else {
		params.Set("encryption", "none")
	}

	if security == "tls" {
		applyTLSParams(params, info)
	}

	// Flow (XTLS-Vision)
	if info.Flow != "" {
		params.Set("flow", info.Flow)
	}

	if security == "reality" {
		applyRealityParams(params, info)
	}

	applyTransportParams(params, info)
	applyFragmentParams(params, info)

	return fmt.Sprintf("vless://%s@%s:%s?%s#%s",
		uuid, host, port, params.Encode(), url.PathEscape(remark))
}

// generateVmessLink: VMess JSON share-link (v2rayN style). Schema:
// https://github.com/2dust/v2rayN/wiki/Description-of-VMess-share-link
// Populates every spec field so v2rayN/NG, v2box, streisand all parse consistently.
func generateVmessLink(info *InboundInfo, uuid, host, port, remark string) string {
	alterID := "0"
	if info.VMessAlterId > 0 {
		alterID = strconv.FormatUint(uint64(info.VMessAlterId), 10)
	}
	scy := info.VMessSecurity
	if scy == "" {
		scy = "auto"
	}

	cfg := map[string]interface{}{
		"v":    "2",
		"ps":   remark,
		"add":  host,
		"port": port,
		"id":   uuid,
		"aid":  alterID,
		"scy":  scy,
		"net":  getNetworkType(info.Network),
		"type": "none",
		"host": "",
		"path": "",
		"tls":  "",
		"sni":  "",
		"alpn": "",
		"fp":   "",
	}

	// Transport-specific settings
	switch getNetworkType(info.Network) {
	case "ws":
		if info.WSPath != "" {
			cfg["path"] = info.WSPath
		}
		if info.WSHost != "" {
			cfg["host"] = info.WSHost
		}
	case "grpc":
		if info.GRPCServiceName != "" {
			cfg["path"] = info.GRPCServiceName
		}
	case "tcp":
		if info.HeaderType != "" {
			cfg["type"] = info.HeaderType
		}
		if info.HTTPPath != "" {
			cfg["path"] = info.HTTPPath
		}
	case "http", "h2":
		if info.HTTPPath != "" {
			cfg["path"] = info.HTTPPath
		}
	case "xhttp", "splithttp":
		if info.XHTTPPath != "" {
			cfg["path"] = info.XHTTPPath
		}
		if info.XHTTPHost != "" {
			cfg["host"] = info.XHTTPHost
		}
		if info.XHTTPMode != "" {
			cfg["mode"] = info.XHTTPMode
		}
	case "httpupgrade":
		if info.HTTPUpgradePath != "" {
			cfg["path"] = info.HTTPUpgradePath
		}
		if info.HTTPUpgradeHost != "" {
			cfg["host"] = info.HTTPUpgradeHost
		}
	}

	// Security: TLS or Reality. The fields are identical between v2rayN's
	// "tls" and "reality" modes; only the "tls" key value differs.
	switch getSecurity(info.Security) {
	case "tls":
		cfg["tls"] = "tls"
		if info.TLSConfig != nil {
			if info.TLSConfig.SNI != "" {
				cfg["sni"] = info.TLSConfig.SNI
			}
			if len(info.TLSConfig.ALPN) > 0 {
				cfg["alpn"] = strings.Join(info.TLSConfig.ALPN, ",")
			}
			if info.TLSConfig.Fingerprint != "" {
				cfg["fp"] = info.TLSConfig.Fingerprint
			}
			if info.TLSConfig.AllowInsecure {
				cfg["allowInsecure"] = true
			}
		}
	case "reality":
		cfg["tls"] = "reality"
		if info.RealityConfig != nil {
			if info.RealityConfig.ServerName != "" {
				cfg["sni"] = info.RealityConfig.ServerName
			}
			if info.RealityConfig.Fingerprint != "" {
				cfg["fp"] = info.RealityConfig.Fingerprint
			}
			if info.RealityConfig.PublicKey != "" {
				cfg["pbk"] = info.RealityConfig.PublicKey
			}
			if info.RealityConfig.ShortID != "" {
				cfg["sid"] = info.RealityConfig.ShortID
			}
			if info.RealityConfig.SpiderX != "" {
				cfg["spx"] = info.RealityConfig.SpiderX
			}
		}
	}

	if info.Fragment != nil {
		parts := []string{info.Fragment.Packets, info.Fragment.Length, info.Fragment.Interval}
		for _, p := range parts {
			if p != "" {
				cfg["fragment"] = strings.Join(parts, ",")
				break
			}
		}
	}

	jsonBytes, _ := json.Marshal(cfg)
	encoded := base64.StdEncoding.EncodeToString(jsonBytes)

	return "vmess://" + encoded
}

// generateTrojanLink creates a Trojan link
func generateTrojanLink(info *InboundInfo, uuid, host, port, remark string) string {
	params := url.Values{}

	// Security
	security := getSecurity(info.Security)
	if security != "none" {
		params.Set("security", security)
	}

	// Network type
	network := getNetworkType(info.Network)
	params.Set("type", network)

	if security == "tls" {
		applyTLSParams(params, info)
	}
	if security == "reality" {
		applyRealityParams(params, info)
	}

	applyTransportParams(params, info)
	applyFragmentParams(params, info)

	queryStr := ""
	if len(params) > 0 {
		queryStr = "?" + params.Encode()
	}

	return fmt.Sprintf("trojan://%s@%s:%s%s#%s",
		uuid, host, port, queryStr, url.PathEscape(remark))
}

func getNetworkType(network string) string {
	if network == "" {
		return "tcp"
	}
	switch strings.ToLower(network) {
	case "websocket":
		return "ws"
	default:
		return strings.ToLower(network)
	}
}

func getSecurity(security string) string {
	if security == "" {
		return "none"
	}
	return strings.ToLower(security)
}
