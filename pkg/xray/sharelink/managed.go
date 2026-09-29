// Package sharelink turns scanned share links into managed pool outbounds.
// It stays free of panel packages so the node agent can link it.
package sharelink

import (
	"encoding/json"
	"fmt"
	knife "github.com/lilendian0x00/xray-knife/v11/pkg/core/xray"
	"github.com/lilendian0x00/xray-knife/v11/utils"
	"github.com/xtls/xray-core/common/geodata"
	"github.com/xtls/xray-core/infra/conf"
	"net/url"
	"strings"
)

// ManagedOutbound preserves the full configuration used by the scanner.
func ManagedOutbound(raw string) (*conf.OutboundDetourConfig, string, string, string, error) {
	raw = strings.TrimSpace(raw)
	uri, err := url.Parse(raw)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("malformed share link")
	}
	common := "encryption security alpn fp type sni host path flow pbk sid spx pqv headerType serviceName mode allowInsecure quicSecurity key authority pcs"
	allowed := ""
	switch uri.Scheme {
	case "vless":
		allowed = common + " extra"
	case "trojan":
		allowed = common
	case "ss", "socks":
	case "wireguard":
		allowed = "address publickey secretkey presharedkey mtu keepalive allowedips reserved"
	case "hysteria2", "hy2":
		allowed = "sni insecure obfs obfs-password"
	case "vmess":
		decoded, e := utils.Base64Decode(strings.TrimPrefix(raw, "vmess://"))
		if e != nil {
			return nil, "", "", "", fmt.Errorf("only JSON VMess links are supported in pools")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(decoded, &fields) != nil {
			return nil, "", "", "", fmt.Errorf("invalid VMess JSON")
		}
		keys := "v ps add port id aid net path host tls sni alpn fp type scy allowinsecure pcs"
		for k := range fields {
			if !hasWord(keys, k) {
				return nil, "", "", "", fmt.Errorf("unsupported VMess setting %q", k)
			}
		}
	default:
		return nil, "", "", "", fmt.Errorf("unsupported pool protocol %q", uri.Scheme)
	}
	if uri.Scheme != "vmess" {
		q, e := url.ParseQuery(uri.RawQuery)
		if e != nil {
			return nil, "", "", "", fmt.Errorf("invalid link options")
		}
		for k, v := range q {
			if !hasWord(allowed, k) || len(v) != 1 {
				return nil, "", "", "", fmt.Errorf("unsupported or repeated setting %q", k)
			}
		}
	}
	p, err := knife.NewXrayService(false, false).CreateProtocol(raw)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("unsupported share link")
	}
	if err = p.Parse(); err != nil {
		return nil, "", "", "", fmt.Errorf("invalid connection settings")
	}
	out, err := p.(knife.Protocol).BuildOutboundDetourConfig(false)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("cannot build outbound")
	}
	if _, err = out.Build(); err != nil {
		return nil, "", "", "", fmt.Errorf("outbound rejected by Xray")
	}
	if err = RequireTransportSecurity(out); err != nil {
		return nil, "", "", "", err
	}
	info := p.ConvertToGeneralConfig()
	return out, info.Protocol, info.Address, info.Remark, nil
}

// RequireTransportSecurity applies the rule Xray 26.9 enforces when it loads a
// config: VLESS without TLS, REALITY or VLESS encryption, and Trojan without
// TLS, may only reach private addresses. The vendored core checks the flat
// settings form only, so a vnext/servers outbound passes Build here and then
// fails the node's whole config.
func RequireTransportSecurity(out *conf.OutboundDetourConfig) error {
	if out.StreamSetting != nil {
		if s := strings.ToLower(out.StreamSetting.Security); s != "" && s != "none" {
			return nil
		}
	}
	if out.Settings == nil {
		return nil
	}
	switch strings.ToLower(out.Protocol) {
	case "vless":
		var s conf.VLessOutboundConfig
		if json.Unmarshal(*out.Settings, &s) != nil {
			return nil
		}
		plain := func(encryption string) bool { return encryption == "" || encryption == "none" }
		if plain(s.Encryption) && isPublic(s.Address) {
			return fmt.Errorf("VLESS without TLS to a public server is rejected by Xray")
		}
		for _, v := range s.Vnext {
			for _, raw := range v.Users {
				var user struct {
					Encryption string `json:"encryption"`
				}
				_ = json.Unmarshal(raw, &user)
				if plain(user.Encryption) && isPublic(v.Address) {
					return fmt.Errorf("VLESS without TLS to a public server is rejected by Xray")
				}
			}
		}
	case "trojan":
		var s conf.TrojanClientConfig
		if json.Unmarshal(*out.Settings, &s) != nil {
			return nil
		}
		addresses := []*conf.Address{s.Address}
		for _, server := range s.Servers {
			addresses = append(addresses, server.Address)
		}
		for _, address := range addresses {
			if isPublic(address) {
				return fmt.Errorf("Trojan without TLS to a public server is rejected by Xray")
			}
		}
	}
	return nil
}

func isPublic(address *conf.Address) bool {
	if address == nil || address.Address == nil {
		return false
	}
	if address.Family().IsIP() {
		return !geodata.GetPrivateIPMatcher().Match(address.IP())
	}
	return !geodata.GetPrivateDomainMatcher().MatchAny(strings.TrimSuffix(strings.ToLower(address.Domain()), "."))
}
func hasWord(words, key string) bool {
	for _, word := range strings.Fields(words) {
		if word == key {
			return true
		}
	}
	return false
}

// EncodeManaged serialises a scanned outbound for storage. encoding/json writes
// nil json.RawMessage fields as null and Xray reads null back as a present
// value: a REALITY client with "dest": null becomes a server missing its
// serverNames. The stored JSON is rebuilt to prove it still loads.
func EncodeManaged(out *conf.OutboundDetourConfig) ([]byte, error) {
	data, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	DropNulls(config)
	if data, err = json.Marshal(config); err != nil {
		return nil, err
	}
	var check conf.OutboundDetourConfig
	if err = json.Unmarshal(data, &check); err != nil {
		return nil, err
	}
	if _, err = check.Build(); err != nil {
		return nil, fmt.Errorf("stored outbound rejected by Xray: %w", err)
	}
	return data, nil
}

// DropNulls removes null members from decoded JSON objects in place, walking
// nested objects and arrays. Array elements are kept to preserve positions.
func DropNulls(config map[string]any) {
	for key, value := range config {
		if value == nil {
			delete(config, key)
			continue
		}
		dropNested(value)
	}
}

func dropNested(value any) {
	switch v := value.(type) {
	case map[string]any:
		DropNulls(v)
	case []any:
		for _, item := range v {
			dropNested(item)
		}
	}
}
