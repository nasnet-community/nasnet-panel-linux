package link

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
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
			if transportSettings.HeaderType == "http" {
				params.Set("headerType", "http")
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
