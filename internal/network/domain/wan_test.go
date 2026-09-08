package domain

import "testing"

func TestWANStaticAddressValidation(t *testing.T) {
	for _, tt := range []struct {
		name, address, gateway string
		onlink, valid          bool
	}{
		{"normal", "10.0.2.15/24", "10.0.2.1", false, true},
		{"31 low", "10.0.2.0/31", "10.0.2.1", false, true},
		{"31 high", "10.0.2.1/31", "10.0.2.0", false, true},
		{"32", "10.0.2.15/32", "10.0.2.1", true, true},
		{"off subnet", "10.0.2.15/24", "10.0.3.1", true, true},
		{"32 needs explicit", "10.0.2.15/32", "10.0.2.1", false, false},
		{"off subnet needs explicit", "10.0.2.15/24", "10.0.3.1", false, false},
		{"self", "10.0.2.15/24", "10.0.2.15", false, false},
		{"broadcast", "10.0.2.255/24", "10.0.2.1", false, false},
		{"network", "10.0.2.0/24", "10.0.2.1", false, false},
		{"gateway broadcast", "10.0.2.15/24", "10.0.2.255", false, false},
		{"IPv6", "2001:db8::2/64", "10.0.2.1", false, false},
		{"injection", "10.0.2.15/24\nDNS=8.8.8.8", "10.0.2.1", false, false},
		{"loopback", "127.0.0.1/8", "127.0.0.2", false, false},
		{"bad prefix", "10.0.2.15/0", "10.0.2.1", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vs := ValidateWAN(WANConfig{Method: MethodStatic, StaticAddress: tt.address, StaticGateway: tt.gateway, GatewayOnLink: tt.onlink, DNSMode: "default"}, SlotDomestic, "ethernet")
			if Rejected(vs) == tt.valid {
				t.Fatalf("valid=%v, verdicts=%+v", tt.valid, vs)
			}
		})
	}
}

func TestWANDNSPolicy(t *testing.T) {
	for _, tt := range []struct {
		mode    string
		servers []string
		slot    UplinkSlot
		valid   bool
	}{
		{"default", nil, SlotDomestic, true}, {"custom", []string{"10.0.0.53"}, SlotDomestic, true},
		{"custom", []string{"10.0.0.53", "10.0.0.54"}, SlotDomestic2, true},
		{"custom", []string{"10.0.0.53", "10.0.0.53"}, SlotDomestic, false},
		{"custom", []string{"10.0.0.53", "10.0.0.54", "10.0.0.55"}, SlotDomestic, false},
		{"custom", []string{"8.8.8.8\nDomains=~."}, SlotDomestic, false},
		{"custom", nil, SlotDomestic, false}, {"isp", nil, SlotDomestic, false},
		{"custom", []string{"8.8.8.8"}, SlotSecondary, false}, {"vpn", nil, SlotSecondary, true},
	} {
		if got := ValidateWAN(WANConfig{Method: MethodDHCP4, DNSMode: tt.mode, DNSServers: tt.servers}, tt.slot, "ethernet"); Rejected(got) == tt.valid {
			t.Errorf("%+v: %+v", tt, got)
		}
	}
}

func TestWANDHCPTransitionClearsInactiveIntent(t *testing.T) {
	n := NetworkInterface{Role: RoleWAN, Slot: SlotDomestic, Method: MethodStatic, StaticAddress: "10.0.0.5/32", StaticGateway: "10.0.0.1", GatewayOnLink: true, LearnedGateway: "10.0.0.2", DNSServer: "10.0.0.53", DNSServer2: "10.0.0.54"}
	n.SetWAN(WANConfig{Method: MethodDHCP4, DNSMode: "default"})
	if n.StaticAddress != "" || n.StaticGateway != "" || n.GatewayOnLink || n.LearnedGateway != "" || n.DNSServer != "" || n.DNSServer2 != "" {
		t.Fatalf("stale intent: %+v", n)
	}
	if n.WANConfig().DNSMode != "default" || n.DNSDomains != "~ir" {
		t.Fatalf("split DNS lost: %+v", n)
	}
}

func TestWANValidationUsesProposedLANOverlap(t *testing.T) {
	in := ValidationInput{Rows: []NetworkInterface{{ID: 1, IfName: "eth0", Source: "eth_onboard", Role: RoleUnassigned, Present: true}}, LAN: &LANConfig{CIDR: "10.77.0.1/24"}, Req: ChangeRequest{InterfaceID: 1, Role: RoleWAN, Slot: SlotDomestic, WAN: &WANConfig{Method: MethodStatic, StaticAddress: "10.77.0.2/24", StaticGateway: "10.77.0.254", DNSMode: "default"}}}
	vs := Validate(in)
	if !Rejected(vs) {
		t.Fatalf("proposed overlap accepted: %+v", vs)
	}
	found := false
	for _, v := range vs {
		if v.Rule == "V14" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected overlap check, got %+v", vs)
	}
}
