// SPDX-License-Identifier: AGPL-3.0-or-later

package subscriptions

import (
	"net/url"
	"os/exec"
	"strings"
	"testing"

	// Test-only: the hub itself never imports the panel's xray package.
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

const (
	testUUID = "3f0c9a52-7d1e-4b8a-9c44-0e5b2a1d6f73"
	testPBK  = "x7QnLw4Rk2VtB9yD0sEoUj3HaFcN6mXz8PqIg1KvTbA"
)

func sample() LinkParams {
	return LinkParams{
		UUID: testUUID, Host: "203.0.113.24", Port: 443,
		ServerName: "www.example.com", PublicKey: testPBK, ShortID: "a1b2c3d4", Name: "Sara",
	}
}

// The panel's own generator is the reference: same string, byte for byte.
func TestBuildLinkMatchesThePanel(t *testing.T) {
	for _, name := range []string{"Sara", "Sara's phone", "علی و مینا", "a/b?c#d e", "100%"} {
		p := sample()
		p.Name = name
		got, err := BuildLink(p)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		info := &xray.InboundInfo{
			Protocol: "vless", Port: 443, Flow: DefaultFlow, Network: "tcp", Security: "reality",
			RealityConfig: &xray.RealityInfoConfig{ServerName: p.ServerName, PublicKey: p.PublicKey, ShortID: p.ShortID, Fingerprint: DefaultFingerprint},
		}
		want, err := xray.GenerateConfigLink(info, p.UUID, p.Host, name)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%q:\n got  %s\n want %s", name, got, want)
		}
	}
}

func TestBuildLinkFields(t *testing.T) {
	link, err := BuildLink(sample())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "vless" || u.User.Username() != testUUID || u.Hostname() != "203.0.113.24" || u.Port() != "443" {
		t.Fatalf("authority wrong: %s", link)
	}
	want := map[string]string{
		"security": "reality", "type": "tcp", "encryption": "none", "flow": "xtls-rprx-vision",
		"sni": "www.example.com", "pbk": testPBK, "sid": "a1b2c3d4", "fp": "chrome",
	}
	q := u.Query()
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("unexpected parameters: %v", q)
	}
	if u.Fragment != "Sara" {
		t.Errorf("name = %q", u.Fragment)
	}
}

func TestBuildLinkKeepsPersianNames(t *testing.T) {
	p := sample()
	p.Name = "سارا · موبایل"
	link, err := BuildLink(p)
	if err != nil {
		t.Fatal(err)
	}
	frag := link[strings.LastIndex(link, "#")+1:]
	if strings.ContainsAny(frag, "سارا") || !strings.Contains(frag, "%") {
		t.Fatalf("fragment is not percent-encoded: %q", frag)
	}
	back, err := url.PathUnescape(frag)
	if err != nil || back != p.Name {
		t.Fatalf("round trip: %q, %v", back, err)
	}
}

func TestBuildLinkBracketsIPv6(t *testing.T) {
	p := sample()
	p.Host = "2001:db8::24"
	link, err := BuildLink(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "@[2001:db8::24]:443?") {
		t.Fatalf("IPv6 host not bracketed: %s", link)
	}
	u, err := url.Parse(link)
	if err != nil || u.Hostname() != "2001:db8::24" || u.Port() != "443" {
		t.Fatalf("does not parse back: %v %v", u, err)
	}
}

func TestBuildLinkOverridesAndDefaults(t *testing.T) {
	p := sample()
	p.Fingerprint, p.Flow = "firefox", "xtls-rprx-vision"
	link, err := BuildLink(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "fp=firefox") {
		t.Errorf("fingerprint override lost: %s", link)
	}
	def, _ := BuildLink(sample())
	if !strings.Contains(def, "fp=chrome") || !strings.Contains(def, "flow=xtls-rprx-vision") {
		t.Errorf("defaults missing: %s", def)
	}
}

func TestBuildLinkRejectsBadInput(t *testing.T) {
	bad := map[string]func(*LinkParams){
		"uuid":        func(p *LinkParams) { p.UUID = "not-a-uuid" },
		"empty uuid":  func(p *LinkParams) { p.UUID = "" },
		"host":        func(p *LinkParams) { p.Host = "  " },
		"port zero":   func(p *LinkParams) { p.Port = 0 },
		"port high":   func(p *LinkParams) { p.Port = 70000 },
		"server name": func(p *LinkParams) { p.ServerName = "" },
		"public key":  func(p *LinkParams) { p.PublicKey = "" },
		"short id":    func(p *LinkParams) { p.ShortID = "xyz" },
		"odd sid":     func(p *LinkParams) { p.ShortID = "abc" },
		"long sid":    func(p *LinkParams) { p.ShortID = "0011223344556677aa" },
		"name":        func(p *LinkParams) { p.Name = " " },
	}
	for name, mutate := range bad {
		p := sample()
		mutate(&p)
		if link, err := BuildLink(p); err == nil {
			t.Errorf("%s: accepted, got %s", name, link)
		}
	}
}

// The hub imports this package, so it must stay light.
func TestLinkBuilderStaysLight(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, heavy := range []string{"xtls/xray-core", "sagernet", "gvisor", "gorm", "nasnet-panel-linux/pkg/xray", "nasnet-panel-linux/pkg/geoip", "internal/node/domain"} {
		if strings.Contains(string(out), heavy) {
			t.Errorf("the link builder links %s", heavy)
		}
	}
}
