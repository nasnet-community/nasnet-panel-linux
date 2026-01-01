package link

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseVLESS_Complex(t *testing.T) {
	linkStr := "vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none&security=tls&sni=cdn.example.com&alpn=h3%2Ch2%2Chttp%2F1.1&fp=firefox&type=xhttp&host=cdn.example.com&path=%2Fexample&mode=packet-up#Ultimate-proxy"

	out, err := Parse(linkStr)
	assert.NoError(t, err)
	assert.NotNil(t, out)

	assert.Equal(t, "vless", out.Protocol)
	assert.Equal(t, "example.com", out.Address)
	assert.Equal(t, 443, out.Port)
	assert.Equal(t, "Ultimate-proxy", out.Tag)
	assert.Equal(t, "tls", out.Security)
	assert.Equal(t, "xhttp", out.Network)

	// Check Protocol Settings
	protoSettings := out.GetVLESSSettingsOrDefault()
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", protoSettings.UUID)
	assert.Equal(t, "none", protoSettings.Encryption)

	// Check TLS Settings
	tlsSettings := out.GetTLSSettingsOrDefault()
	assert.Equal(t, "cdn.example.com", tlsSettings.ServerName)
	assert.Equal(t, "firefox", tlsSettings.Fingerprint)
	assert.Contains(t, tlsSettings.ALPN, "h3")
	assert.Contains(t, tlsSettings.ALPN, "http/1.1")

	// Check Transport Settings
	transport := out.GetTransportSettingsOrDefault()
	assert.Equal(t, "/example", transport.Path)
	assert.Equal(t, "cdn.example.com", transport.Host)
	assert.Equal(t, "packet-up", transport.Mode) // XHTTP mode
}

func TestParseVMess(t *testing.T) {
	// vmess://eyJhZGQiOiIxMjcuMC4wLjEiLCJhaWQiOjAsImFscG4iOiIiLCJmcCI6IiIsImhvc3QiOiIiLCJpZCI6ImIyYzY0NTQ5LWEwNjAtNDk4MS1hYWNlLWQ4OTMyZTEzMzM4ZSIsIm5ldCI6InRjcCIsInBhdGgiOiIiLCJwb3J0Ijo0NDMsInBzIjoiVGVzdCBWTWVzcyIsInNjeSI6ImF1dG8iLCJzbmkiOiIiLCJ0bHMiOiIiLCJ0eXBlIjoibm9uZSIsInYiOiIyIn0=
	// Plain payload: {"add":"127.0.0.1","aid":0,"alpn":"","fp":"","host":"","id":"b2c64549-a060-4981-aace-d8932e13338e","net":"tcp","path":"","port":443,"ps":"Test VMess","scy":"auto","sni":"","tls":"","type":"none","v":"2"}
	linkStr := "vmess://eyJhZGQiOiIxMjcuMC4wLjEiLCJhaWQiOjAsImFscG4iOiIiLCJmcCI6IiIsImhvc3QiOiIiLCJpZCI6ImIyYzY0NTQ5LWEwNjAtNDk4MS1hYWNlLWQ4OTMyZTEzMzM4ZSIsIm5ldCI6InRjcCIsInBhdGgiOiIiLCJwb3J0Ijo0NDMsInBzIjoiVGVzdCBWTWVzcyIsInNjeSI6ImF1dG8iLCJzbmkiOiIiLCJ0bHMiOiIiLCJ0eXBlIjoibm9uZSIsInYiOiIyIn0="

	out, err := Parse(linkStr)
	assert.NoError(t, err)
	assert.Equal(t, "vmess", out.Protocol)
	assert.Equal(t, "127.0.0.1", out.Address)
	assert.Equal(t, 443, out.Port)
	assert.Equal(t, "Test VMess", out.Tag)
}
