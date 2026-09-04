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

// Parse converts a link string into an Outbound struct
func Parse(linkStr string) (*domain.Outbound, error) {
	linkStr = strings.TrimSpace(linkStr)
	u, err := url.Parse(linkStr)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %v", err)
	}

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "vless", "trojan":
		return parseVlessTrojan(u)
	case "vmess":
		return parseVmess(linkStr)
	case "ss":
		return parseShadowsocks(u)
	case "socks", "http":
		return parseSocksHttp(u)
	case "freedom", "blackhole":
		return parseDirect(u)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", scheme)
	}
}

func parseVlessTrojan(u *url.URL) (*domain.Outbound, error) {
	q := u.Query()
	outbound := &domain.Outbound{
		Tag:      u.Fragment,
		Protocol: strings.ToLower(u.Scheme),
		Address:  u.Hostname(),
		Port:     getIntPort(u),
		Remark:   u.Fragment,
	}
	if outbound.Remark == "" {
		outbound.Remark = "Imported " + strings.ToUpper(outbound.Protocol)
	}

	// UUID / Password
	auth := u.User.Username()
	if auth == "" {
		return nil, fmt.Errorf("missing uuid/password")
	}

	if outbound.Protocol == "vless" {
		outbound.VLESSSettings = &domain.VLESSSettings{
			UUID:       auth,
			Encryption: q.Get("encryption"),
			Flow:       q.Get("flow"),
		}
	} else {
		// Trojan
		outbound.TrojanSettings = &domain.TrojanSettings{
			Password: auth,
		}
	}

	// TLS / Security
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	outbound.Security = security

	if security == "tls" {
		tlsSettings := &domain.TLSSettings{
			ServerName:  q.Get("sni"),
			Fingerprint: q.Get("fp"),
			ALPN:        validALPN(q.Get("alpn")),
		}
		outbound.TLSSettings = tlsSettings
	} else if security == "reality" {
		realitySettings := &domain.RealitySettings{
			ServerName:  q.Get("sni"),
			Fingerprint: q.Get("fp"),
			PublicKey:   q.Get("pbk"),
			ShortID:     q.Get("sid"),
			SpiderX:     q.Get("spiderx"),
			ALPN:        validALPN(q.Get("alpn")),
			Show:        true, // Implicitly enabled for reality links
		}
		outbound.RealitySettings = realitySettings
	}

	// Stream Settings
	streamType := q.Get("type")
	if streamType == "" {
		streamType = "tcp"
	}

	// Handle special 'xhttp' type query param
	if streamType == "xhttp" || streamType == "http" {
		// Differentiate standard http (h2) vs xhttp/split-http logic if needed
		// Usually if `mode` is present it is xhttp or quic
	}

	outbound.Network = streamType

	// Sockopt omitted for brevity/compatibility (add if needed)

	// Transport
	transport := &domain.TransportSettings{}

	switch streamType {
	case "ws":
		transport.Path = q.Get("path")
		transport.Host = q.Get("host")
	case "grpc":
		transport.ServiceName = q.Get("serviceName")
	case "http", "h2":
		transport.Path = q.Get("path")
		transport.Host = q.Get("host")
	case "xhttp":
		transport.Path = q.Get("path")
		transport.Host = q.Get("host")
		transport.Mode = q.Get("mode")
	case "tcp":
		headerType := q.Get("headerType")
		if headerType == "http" {
			transport.HeaderType = "http"
			// HTTP obfuscation carries the request path and Host header the
			// server matches on; without them the outbound is a bare TCP
			// connection the inbound will reject.
			transport.Path = q.Get("path")
			transport.Host = q.Get("host")
		}
	}
	outbound.TransportSettings = transport

	return outbound, nil
}

func parseVmess(linkStr string) (*domain.Outbound, error) {
	b64 := strings.TrimPrefix(linkStr, "vmess://")
	decoded, err := tryDecodeBase64(b64)
	if err != nil {
		return nil, fmt.Errorf("vmess base64 decode error: %v", err)
	}

	var v struct {
		V    string      `json:"v"`
		PS   string      `json:"ps"`
		Add  string      `json:"add"`
		Port interface{} `json:"port"` // int or string
		ID   string      `json:"id"`
		AID  interface{} `json:"aid"`
		Scy  string      `json:"scy"`
		Net  string      `json:"net"`
		Type string      `json:"type"`
		Host string      `json:"host"`
		Path string      `json:"path"`
		TLS  string      `json:"tls"`
		SNI  string      `json:"sni"`
		ALPN string      `json:"alpn"`
		FP   string      `json:"fp"`
	}

	if err := json.Unmarshal(decoded, &v); err != nil {
		return nil, fmt.Errorf("invalid vmess json: %v", err)
	}

	port, _ := strconv.Atoi(fmt.Sprintf("%v", v.Port))
	alterId, _ := strconv.Atoi(fmt.Sprintf("%v", v.AID))

	outbound := &domain.Outbound{
		Tag:      v.PS,
		Protocol: "vmess",
		Address:  v.Add,
		Port:     port,
		Remark:   v.PS,
		Security: v.TLS,
		Network:  v.Net,
	}
	if outbound.Security == "" {
		outbound.Security = "none"
	}
	if outbound.Remark == "" {
		outbound.Remark = "Imported VMess"
	}

	outbound.VMessSettings = &domain.VMessSettings{
		UUID:     v.ID,
		AlterId:  alterId,
		Security: v.Scy, // Security cipher method
	}

	if v.TLS == "tls" {
		outbound.TLSSettings = &domain.TLSSettings{
			ServerName:  v.SNI,
			Fingerprint: v.FP,
			ALPN:        validALPN(v.ALPN),
		}
	} else if v.TLS == "reality" {
		outbound.RealitySettings = &domain.RealitySettings{
			ServerName:  v.SNI,
			Fingerprint: v.FP,
			Show:        true,
			// JSON vmess usually doesn't have pbk/sid standard fields, assume standard or missing
		}
	}

	transport := &domain.TransportSettings{}
	if v.Net == "ws" {
		transport.Path = v.Path
		transport.Host = v.Host
	} else if v.Net == "grpc" {
		transport.ServiceName = v.Path
	} else if v.Net == "h2" || v.Net == "http" {
		transport.Path = v.Path
		transport.Host = v.Host
	}
	outbound.TransportSettings = transport

	return outbound, nil
}

func parseShadowsocks(u *url.URL) (*domain.Outbound, error) {
	var method, pass, host string
	var port int

	if u.User != nil {
		method = u.User.Username()
		pass, _ = u.User.Password()
		host = u.Hostname()
		port = getIntPort(u)
	} else {
		// Simplified fallback for base64 host part handling or just error
		return nil, fmt.Errorf("parse legacy ss manually if needed")
	}

	outbound := &domain.Outbound{
		Tag:      u.Fragment,
		Protocol: "shadowsocks",
		Address:  host,
		Port:     port,
		Remark:   u.Fragment,
		Security: "none",
	}
	if outbound.Remark == "" {
		outbound.Remark = "Imported SS"
	}

	outbound.ShadowsocksSettings = &domain.ShadowsocksSettings{
		Method:   method,
		Password: pass,
		Network:  "tcp,udp", // Default
	}

	return outbound, nil
}

func parseSocksHttp(u *url.URL) (*domain.Outbound, error) {
	outbound := &domain.Outbound{
		Tag:      u.Fragment,
		Protocol: strings.ToLower(u.Scheme),
		Address:  u.Hostname(),
		Port:     getIntPort(u),
		Remark:   u.Fragment,
		Security: "none",
	}
	if outbound.Remark == "" {
		outbound.Remark = "Imported " + strings.ToUpper(outbound.Protocol)
	}

	user := u.User.Username()
	pass, _ := u.User.Password()

	if outbound.Protocol == "socks" {
		settings := &domain.SOCKSSettings{Auth: "noauth"}
		if user != "" || pass != "" {
			settings.Auth = "password"
			settings.Accounts = []domain.SOCKSAccount{{User: user, Pass: pass}}
		}
		outbound.SOCKSSettings = settings
	} else {
		// HTTP
		settings := &domain.HTTPSettings{}
		if user != "" || pass != "" {
			settings.Accounts = []domain.HTTPAccount{{User: user, Pass: pass}}
		}
		outbound.HTTPSettings = settings
	}

	return outbound, nil
}

func parseDirect(u *url.URL) (*domain.Outbound, error) {
	outbound := &domain.Outbound{
		Tag:      u.Fragment,
		Protocol: strings.ToLower(u.Scheme),
		Address:  "127.0.0.1",
		Port:     0,
		Remark:   u.Fragment,
	}
	if outbound.Remark == "" {
		outbound.Remark = "Imported Direct"
	}

	if outbound.Protocol == "freedom" {
		outbound.FreedomSettings = &domain.FreedomSettings{
			DomainStrategy: "AsIs",
		}
	} else {
		outbound.BlackholeSettings = &domain.BlackholeSettings{
			ResponseType: "none",
		}
	}

	return outbound, nil
}

func getIntPort(u *url.URL) int {
	p := u.Port()
	if p == "" {
		return 0
	}
	i, _ := strconv.Atoi(p)
	return i
}

func validALPN(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "%2C", ",")
	parts := strings.Split(s, ",")
	var res []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			res = append(res, t)
		}
	}
	return res
}

func tryDecodeBase64(s string) ([]byte, error) {
	if l := len(s) % 4; l > 0 {
		s += strings.Repeat("=", 4-l)
	}
	return base64.StdEncoding.DecodeString(s)
}
