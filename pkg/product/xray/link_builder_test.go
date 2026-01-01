package xray

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const (
	oldUUID   = "11111111-1111-1111-1111-111111111111"
	newUUID   = "22222222-2222-2222-2222-222222222222"
	oldRemark = "oldRemark"
	newRemark = "newRemark"
	oldPass   = "old-trojan-pass"
	newPass   = "new-trojan-pass"
)

// vmessTemplate builds a minimal vmess:// link with std-base64 JSON payload.
// Note: port and aid must be JSON numbers, not strings — xray-knife rejects
// the string variant by returning an empty link.
func vmessTemplate(id, ps string) string {
	j := fmt.Sprintf(
		`{"add":"example.com","aid":0,"alpn":"","fp":"","host":"","id":%q,"net":"tcp","path":"","port":443,"ps":%q,"scy":"auto","sni":"","tls":"","type":"none","v":"2"}`,
		id, ps,
	)
	return "vmess://" + base64.StdEncoding.EncodeToString([]byte(j))
}

// vmessDecode tries the base64 encodings xray-knife / our rebuild may emit.
func vmessDecode(t *testing.T, link string) string {
	t.Helper()
	payload := strings.TrimPrefix(link, "vmess://")
	for _, dec := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
	} {
		if b, err := dec(payload); err == nil {
			return string(b)
		}
	}
	t.Fatalf("cannot base64-decode vmess payload: %s", payload)
	return ""
}

func TestBuildVlessLink_ReplacesIDAndRemark(t *testing.T) {
	lb := NewLinkBuilder()
	tmpl := "vless://" + oldUUID + "@example.com:443?security=tls&type=tcp&sni=example.com#" + oldRemark
	got, err := lb.BuildVlessLink(tmpl, newUUID, newRemark)
	if err != nil {
		t.Fatalf("BuildVlessLink: %v", err)
	}
	if !strings.Contains(got, newUUID) {
		t.Errorf("new uuid missing: %s", got)
	}
	if !strings.Contains(got, newRemark) {
		t.Errorf("new remark missing: %s", got)
	}
	if strings.Contains(got, oldUUID) {
		t.Errorf("old uuid leaked: %s", got)
	}
}

func TestBuildVmessLink_ReplacesIDAndRemark(t *testing.T) {
	lb := NewLinkBuilder()
	got, err := lb.BuildVmessLink(vmessTemplate(oldUUID, oldRemark), newUUID, newRemark)
	if err != nil {
		t.Fatalf("BuildVmessLink: %v", err)
	}
	if !strings.HasPrefix(got, "vmess://") {
		t.Fatalf("missing vmess:// prefix: %s", got)
	}
	json := vmessDecode(t, got)
	if !strings.Contains(json, newUUID) {
		t.Errorf("new uuid missing from payload: %s", json)
	}
	if !strings.Contains(json, newRemark) {
		t.Errorf("new remark missing from payload: %s", json)
	}
	if strings.Contains(json, oldUUID) {
		t.Errorf("old uuid leaked: %s", json)
	}
}

func TestBuildTrojanLink_ReplacesPasswordAndRemark(t *testing.T) {
	lb := NewLinkBuilder()
	tmpl := "trojan://" + oldPass + "@example.com:443?security=tls&type=tcp&sni=example.com#" + oldRemark
	got, err := lb.BuildTrojanLink(tmpl, newPass, newRemark)
	if err != nil {
		t.Fatalf("BuildTrojanLink: %v", err)
	}
	if !strings.Contains(got, newPass) || !strings.Contains(got, newRemark) {
		t.Errorf("missing new password or remark: %s", got)
	}
	if strings.Contains(got, oldPass) {
		t.Errorf("old password leaked: %s", got)
	}
}

func TestBuildVlessLink_BadTemplate(t *testing.T) {
	if _, err := NewLinkBuilder().BuildVlessLink("not-a-link", newUUID, newRemark); err == nil {
		t.Fatal("garbage template should error")
	}
}

func TestBuildLink_DispatchByScheme(t *testing.T) {
	lb := NewLinkBuilder()

	tmpl := "vless://" + oldUUID + "@example.com:443?security=tls&type=tcp#" + oldRemark
	if got, err := lb.BuildLink(tmpl, newUUID, newRemark); err != nil || !strings.Contains(got, newUUID) {
		t.Errorf("vless dispatch: got=%q err=%v", got, err)
	}

	if _, err := lb.BuildLink("", newUUID, newRemark); err == nil {
		t.Error("empty template should error")
	}

	if _, err := lb.BuildLink("ssh://x@h:22", newUUID, newRemark); err == nil {
		t.Error("ssh scheme should error")
	}
}

func TestBuildLinkWithFallback(t *testing.T) {
	lb := NewLinkBuilder()
	if got := lb.BuildLinkWithFallback("", "u", "e", "h", 1, "r"); got != "" {
		t.Errorf("empty template: got %q", got)
	}
	// Non-link template: placeholders get substituted and the result is
	// returned verbatim because xray-knife can't parse it.
	got := lb.BuildLinkWithFallback("literal-{uuid}-{name}", "ABC", "x@x", "h", 1, "XYZ")
	if got != "literal-ABC-XYZ" {
		t.Errorf("fallback = %q, want literal-ABC-XYZ", got)
	}
}

func TestBuildHysteria2Link(t *testing.T) {
	lb := NewLinkBuilder()
	tmpl := "hysteria2://placeholder@example.com:443?sni=example.com&insecure=1#old"

	link, err := lb.BuildLink(tmpl, newUUID, "MyNode")
	if err != nil {
		t.Fatalf("BuildLink hysteria2 failed: %v", err)
	}
	// auth (userinfo) must become the account UUID (server uses it as auth)
	if !strings.HasPrefix(link, "hysteria2://"+newUUID+"@example.com:443") {
		t.Errorf("auth not set to uuid: %s", link)
	}
	// query params preserved
	if !strings.Contains(link, "sni=example.com") || !strings.Contains(link, "insecure=1") {
		t.Errorf("query params lost: %s", link)
	}
	// remark updated
	if !strings.HasSuffix(link, "#MyNode") {
		t.Errorf("remark not set: %s", link)
	}
}

func TestBuildLinkWithFallback_Hysteria2Placeholders(t *testing.T) {
	lb := NewLinkBuilder()
	tmpl := "hysteria2://{uuid}@{host}:{port}?sni=cdn.example.com#{name}"
	link := lb.BuildLinkWithFallback(tmpl, newUUID, "u@e", "1.2.3.4", 8443, "Node A")
	if !strings.Contains(link, newUUID) || !strings.Contains(link, "1.2.3.4:8443") {
		t.Errorf("placeholders not substituted: %s", link)
	}
	if !strings.HasPrefix(link, "hysteria2://") {
		t.Errorf("scheme lost: %s", link)
	}
}
