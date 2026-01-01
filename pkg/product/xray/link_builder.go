package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/link"
)

// LinkBuilder provides methods for parsing and generating VPN config links
type LinkBuilder struct{}

// NewLinkBuilder creates a new link builder
func NewLinkBuilder() *LinkBuilder {
	return &LinkBuilder{}
}

// BuildVlessLink parses a template VLESS link and regenerates it with new credentials
func (lb *LinkBuilder) BuildVlessLink(templateLink, uuid, remark string) (string, error) {
	out, err := link.Parse(templateLink)
	if err != nil {
		return "", fmt.Errorf("failed to parse vless template: %w", err)
	}
	if out.VLESSSettings == nil {
		return "", fmt.Errorf("template is not a vless link")
	}

	out.VLESSSettings.UUID = uuid
	out.Remark = remark
	defaultFingerprint(out)

	return link.Generate(out)
}

// defaultFingerprint fills in a uTLS fingerprint when the template omits one.
//
// Templates without an explicit fp= previously came back with fp=chrome, because
// the link library we used injected that default. Dropping it silently would
// change the TLS fingerprint these configs present on the wire, so the default
// is applied here instead.
func defaultFingerprint(out *domain.Outbound) {
	if out.TLSSettings != nil && out.TLSSettings.Fingerprint == "" {
		out.TLSSettings.Fingerprint = "chrome"
	}
	if out.RealitySettings != nil && out.RealitySettings.Fingerprint == "" {
		out.RealitySettings.Fingerprint = "chrome"
	}
}

// BuildVmessLink parses a template VMess link and regenerates it with new credentials.
//
// The payload is rebuilt by hand rather than round-tripped through Parse/Generate
// so that template fields we don't model (or don't recognise) survive untouched —
// only "id" and "ps" are overwritten. Parse is still called first, to reject
// malformed templates before we edit them.
func (lb *LinkBuilder) BuildVmessLink(templateLink, uuid, remark string) (string, error) {
	if _, err := link.Parse(templateLink); err != nil {
		return "", fmt.Errorf("failed to parse vmess template: %w", err)
	}

	payload := strings.TrimPrefix(templateLink, "vmess://")
	decoded, err := decodeBase64Permissive(payload)
	if err != nil {
		return "", fmt.Errorf("failed to decode vmess payload: %w", err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(decoded, &obj); err != nil {
		return "", fmt.Errorf("failed to parse vmess json: %w", err)
	}
	obj["id"] = uuid
	obj["ps"] = remark

	out, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("failed to encode vmess json: %w", err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(out), nil
}

// decodeBase64Permissive tries the encodings vmess templates can be produced
// with: std / raw-std / url / raw-url. Returns the first successful decode.
func decodeBase64Permissive(s string) ([]byte, error) {
	for _, dec := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	} {
		if b, err := dec(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 payload")
}

// BuildTrojanLink parses a template Trojan link and regenerates it with new credentials
func (lb *LinkBuilder) BuildTrojanLink(templateLink, uuid, remark string) (string, error) {
	out, err := link.Parse(templateLink)
	if err != nil {
		return "", fmt.Errorf("failed to parse trojan template: %w", err)
	}
	if out.TrojanSettings == nil {
		return "", fmt.Errorf("template is not a trojan link")
	}

	out.TrojanSettings.Password = uuid
	out.Remark = remark
	defaultFingerprint(out)

	return link.Generate(out)
}

// BuildLink detects protocol and builds the appropriate link
func (lb *LinkBuilder) BuildLink(templateLink, uuid, remark string) (string, error) {
	if templateLink == "" {
		return "", fmt.Errorf("empty template link")
	}

	// Detect protocol from scheme
	uri, err := url.Parse(templateLink)
	if err != nil {
		return "", fmt.Errorf("failed to parse link URL: %w", err)
	}

	switch strings.ToLower(uri.Scheme) {
	case "vless":
		return lb.BuildVlessLink(templateLink, uuid, remark)
	case "vmess":
		return lb.BuildVmessLink(templateLink, uuid, remark)
	case "trojan":
		return lb.BuildTrojanLink(templateLink, uuid, remark)
	case "hysteria2", "hysteria":
		return lb.BuildHysteria2Link(templateLink, uuid, remark)
	default:
		return "", fmt.Errorf("unsupported protocol: %s", uri.Scheme)
	}
}

// BuildHysteria2Link regenerates a hysteria2 template link
func (lb *LinkBuilder) BuildHysteria2Link(templateLink, uuid, remark string) (string, error) {
	u, err := url.Parse(templateLink)
	if err != nil {
		return "", fmt.Errorf("failed to parse hysteria2 template: %w", err)
	}
	u.User = url.User(uuid)
	if remark != "" {
		u.Fragment = remark
	}
	return u.String(), nil
}

// BuildLinkWithFallback tries a structured rebuild first, falls back to
// placeholder replacement when the template isn't a link we can parse.
func (lb *LinkBuilder) BuildLinkWithFallback(templateLink, uuid, email, host string, port int, remark string) string {
	if templateLink == "" {
		return ""
	}

	// Pre-replace placeholders so the parser sees a valid UUID and {name}
	// doesn't survive literal into the output.
	preprocessed := strings.NewReplacer(
		"{uuid}", uuid,
		"{host}", host,
		"{port}", fmt.Sprintf("%d", port),
		"{name}", remark,
	).Replace(templateLink)

	// Try a structured parse/regenerate with the preprocessed link
	built, err := lb.BuildLink(preprocessed, uuid, remark)
	if err == nil && built != "" {
		return built
	}

	// Fallback to simple placeholder replacement (already done above, so just return)
	return preprocessed
}
