package usecase

import (
	"testing"
)

// Lock the user-managed protocol set so a regression here can't reintroduce
// xray's "proxy is not a UserManager" log spam.
func TestProtocolSupportsUserManagement(t *testing.T) {
	managed := []string{"vmess", "vless", "trojan", "shadowsocks", "unknown"}
	unmanaged := []string{"socks", "http", "dokodemo-door", "wireguard"}

	for _, p := range managed {
		if !protocolSupportsUserManagement(p) {
			t.Errorf("%s should be user-managed", p)
		}
	}
	for _, p := range unmanaged {
		if protocolSupportsUserManagement(p) {
			t.Errorf("%s should NOT be user-managed", p)
		}
	}
}
