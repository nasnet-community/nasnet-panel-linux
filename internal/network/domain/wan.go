package domain

import (
	"fmt"
	"net/netip"
	"strings"
)

// WANConfig is a complete replacement of one WAN's address and DNS intent.
// A nil ChangeRequest.WAN preserves intent for older role-only clients.
type WANConfig struct {
	Method        AddressMethod `json:"method"`
	StaticAddress string        `json:"static_address"`
	StaticGateway string        `json:"static_gateway"`
	GatewayOnLink bool          `json:"gateway_on_link"`
	DNSMode       string        `json:"dns_mode"` // default, custom, or vpn
	DNSServers    []string      `json:"dns_servers"`
}

func (n NetworkInterface) WANConfig() WANConfig {
	c := WANConfig{Method: n.Method, StaticAddress: n.StaticAddress, StaticGateway: n.StaticGateway,
		GatewayOnLink: n.GatewayOnLink, DNSMode: "default", DNSServers: []string{}}
	if c.Method == "" {
		c.Method = MethodDHCP4
	}
	if n.Slot.IsSecondary() {
		c.DNSMode = "vpn"
	} else if n.DNSServer != "" {
		c.DNSMode = "custom"
		c.DNSServers = append(c.DNSServers, n.DNSServer)
		if n.DNSServer2 != "" {
			c.DNSServers = append(c.DNSServers, n.DNSServer2)
		}
	}
	return c
}

// WANChange also honours the address fields exposed by the original API.
func (r ChangeRequest) WANChange(n NetworkInterface) *WANConfig {
	if r.WAN != nil {
		c := *r.WAN
		return &c
	}
	if r.Method == "" {
		return nil
	}
	c := n.WANConfig()
	c.Method, c.StaticAddress, c.StaticGateway = r.Method, r.StaticAddress, r.StaticGateway
	c.GatewayOnLink = false
	if r.Slot.IsSecondary() {
		c.DNSMode, c.DNSServers = "vpn", []string{}
	}
	return &c
}

// SetWAN clears inactive fields so switching back to DHCP cannot reuse a
// static gateway or a previous lease during the health loop's next tick.
func (n *NetworkInterface) SetWAN(c WANConfig) {
	n.Method, n.StaticAddress, n.StaticGateway = c.Method, "", ""
	n.GatewayOnLink, n.LearnedGateway = false, ""
	if c.Method == MethodStatic {
		n.StaticAddress, n.StaticGateway, n.GatewayOnLink = c.StaticAddress, c.StaticGateway, c.GatewayOnLink
	}
	n.DNSServer, n.DNSServer2, n.DNSDomains = "", "", ""
	if c.DNSMode == "custom" {
		if len(c.DNSServers) > 0 {
			n.DNSServer = c.DNSServers[0]
		}
		if len(c.DNSServers) > 1 {
			n.DNSServer2 = c.DNSServers[1]
		}
	}
	if n.Slot.IsDomestic() {
		n.DNSDomains = "~ir"
	}
}

func ValidWANIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4() && a.IsGlobalUnicast() && !a.IsLoopback() && a.As4()[0] != 0 && a.As4()[0] < 224
}

func subnetEndpoint(p netip.Prefix, a netip.Addr) bool {
	if p.Bits() >= 31 {
		return false
	} // RFC 3021 allows both /31 endpoints.
	base := p.Masked().Addr().As4()
	ip := a.As4()
	b := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	i := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	return i == b || i == b|(uint32(1)<<uint(32-p.Bits())-1)
}

func ValidateWAN(c WANConfig, slot UplinkSlot, source string) []Verdict {
	var vs []Verdict
	reject := func(field, msg string) {
		vs = append(vs, Verdict{Rule: "WAN_" + field, Level: LevelReject, Message: msg})
	}
	if strings.HasPrefix(source, "wwan_") {
		reject("method", "Cellular configuration is managed separately from this WAN editor.")
	}
	if c.Method != MethodDHCP4 && c.Method != MethodStatic {
		reject("method", "Choose automatic DHCP or static IPv4.")
	}
	if c.Method == MethodStatic {
		p, err := netip.ParsePrefix(c.StaticAddress)
		valid := err == nil && p.Addr().Is4() && p.Bits() >= 1 && ValidWANIPv4(p.Addr().String())
		if !valid {
			reject("address", "Enter an IPv4 address with a prefix length from 1 to 32.")
		} else if subnetEndpoint(p, p.Addr()) {
			reject("address", "Use a host address, not this subnet's network or broadcast address.")
		}
		if !ValidWANIPv4(c.StaticGateway) {
			reject("gateway", "Enter a unicast IPv4 gateway.")
		} else if valid {
			gw := netip.MustParseAddr(c.StaticGateway)
			if gw == p.Addr() {
				reject("gateway", "The gateway must differ from the WAN address.")
			} else if p.Contains(gw) && subnetEndpoint(p, gw) {
				reject("gateway", "The gateway cannot be this subnet's network or broadcast address.")
			} else if !p.Contains(gw) && !c.GatewayOnLink {
				reject("gateway_on_link", "Enable Gateway outside this subnet only if your ISP's gateway is directly reachable on this link.")
			}
		}
	}
	if slot.IsSecondary() {
		if c.DNSMode != "vpn" || len(c.DNSServers) != 0 {
			reject("dns", "Secondary WAN DNS is managed through the VPN.")
		}
	} else {
		switch c.DNSMode {
		case "default":
			if len(c.DNSServers) != 0 {
				reject("dns", "Router defaults cannot include custom DNS servers.")
			}
		case "custom":
			if len(c.DNSServers) < 1 || len(c.DNSServers) > 2 {
				reject("dns", "Enter one or two DNS servers.")
			}
			seen := map[string]bool{}
			for i, ip := range c.DNSServers {
				if !ValidWANIPv4(ip) {
					reject("dns", fmt.Sprintf("DNS server %d must be a unicast IPv4 address.", i+1))
				}
				if seen[ip] {
					reject("dns", "DNS servers must be different.")
				}
				seen[ip] = true
			}
		default:
			reject("dns", "Choose router defaults or custom DNS servers.")
		}
	}
	return vs
}

// InterfaceIntent excludes discovery and health facts, which may change while
// an apply is armed. Persist it in the snapshot for the standalone dead-man.
type InterfaceIntent struct {
	ID             uint
	Role           InterfaceRole
	Slot           UplinkSlot
	MasterID       *uint
	Method         AddressMethod
	StaticAddress  string
	StaticGateway  string
	GatewayOnLink  bool
	DNSServer      string
	DNSServer2     string
	DNSDomains     string
	LearnedGateway string
}
