package usecase

import (
	"fmt"
	"strings"
)

type clientConfParams struct {
	PrivateKey      string
	Address         string // bare IP
	DNS             string
	MTU             int
	ServerPublicKey string
	PresharedKey    string
	Endpoint        string // host:port
}

// buildClientConf renders a WireGuard client .conf (full-tunnel).
func buildClientConf(p clientConfParams) string {
	if p.DNS == "" {
		p.DNS = "1.1.1.1"
	}
	if p.MTU == 0 {
		p.MTU = 1420
	}
	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", p.PrivateKey)
	fmt.Fprintf(&b, "Address = %s/32\n", p.Address)
	fmt.Fprintf(&b, "DNS = %s\n", p.DNS)
	fmt.Fprintf(&b, "MTU = %d\n\n", p.MTU)
	b.WriteString("[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", p.ServerPublicKey)
	if p.PresharedKey != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", p.PresharedKey)
	}
	fmt.Fprintf(&b, "Endpoint = %s\n", p.Endpoint)
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")
	b.WriteString("PersistentKeepalive = 25\n")
	return b.String()
}
