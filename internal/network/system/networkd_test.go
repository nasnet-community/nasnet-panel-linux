package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

func TestRenderUplink_StaticDomestic(t *testing.T) {
	in := domain.NetworkInterface{
		IfName: "enp1s0", PermMAC: "aa:bb:cc:dd:ee:01", Slot: domain.SlotDomestic,
		Method: domain.MethodStatic, StaticAddress: "192.168.1.34/24",
		StaticGateway: "192.168.1.1", DNSServer: "217.218.127.127", DNSDomains: "~ir",
	}
	f := RenderUplink(in, 201)

	if f.Name != "10-nasnet-wan-domestic.network" {
		t.Errorf("Name = %q", f.Name)
	}
	for _, want := range []string{
		"PermanentMACAddress=aa:bb:cc:dd:ee:01",
		"Address=192.168.1.34/24",
		"IPv6AcceptRA=no",
		"LinkLocalAddressing=ipv4",
		"DNS=217.218.127.127",
		"Domains=~ir",
		"Gateway=192.168.1.1",
		"Table=201",
		"Destination=192.168.1.0/24",
		"Scope=link",
	} {
		if !strings.Contains(f.Content, want) {
			t.Errorf("missing %q in:\n%s", want, f.Content)
		}
	}
	// Match on permanent MAC, never the kernel name: a rename must not orphan it.
	if strings.Contains(f.Content, "Name=enp1s0") {
		t.Error("rendered file matches on the kernel name instead of the permanent MAC")
	}
	// wan1/wan2 are documentation shorthand.
	for _, forbidden := range []string{"wan1", "wan2"} {
		if strings.Contains(f.Content, forbidden) {
			t.Errorf("shorthand %q leaked into a rendered file", forbidden)
		}
	}
}

// RouteTable= keeps main free of defaults, forcing every egress choice explicit.
func TestRenderUplink_DHCPSecondaryKeepsMainEmpty(t *testing.T) {
	in := domain.NetworkInterface{
		IfName: "enp2s0", PermMAC: "aa:bb:cc:dd:ee:02", Slot: domain.SlotSecondary,
		Method: domain.MethodDHCP4, DNSServer: "1.1.1.1", DNSDomains: "~.",
	}
	f := RenderUplink(in, 202)

	for _, want := range []string{
		"DHCP=ipv4",
		"RouteTable=202",
		"UseDNS=no",
		"UseNTP=no",
		"DNS=1.1.1.1",
		"Domains=~.",
		// Dish API sits outside 100.64.0.0/10 and needs its own route.
		"Destination=192.168.100.0/24",
	} {
		if !strings.Contains(f.Content, want) {
			t.Errorf("missing %q in:\n%s", want, f.Content)
		}
	}
	if strings.Contains(f.Content, "Gateway=") {
		t.Error("a DHCP uplink must not carry a static Gateway=")
	}
}

// Whatever is left in the directory keeps being applied, at every boot.
func TestWriteFilesExactly_PrunesOurOwnStaleFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"20-nasnet-lan.netdev", "30-nasnet-lan.network",
		"40-nasnet-mgmt.network", "99-operator-vpn.network",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stale\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rendered := []UplinkFile{{Name: "10-nasnet-wan-domestic.network", Content: "fresh\n"}}
	if err := WriteFilesExactly(dir, rendered, "40-nasnet-mgmt.network"); err != nil {
		t.Fatalf("WriteFilesExactly: %v", err)
	}

	for name, want := range map[string]bool{
		"10-nasnet-wan-domestic.network": true,
		"40-nasnet-mgmt.network":         true,  // frozen, never re-rendered
		"99-operator-vpn.network":        true,  // not ours
		"20-nasnet-lan.netdev":           false, // the LAN is gone
		"30-nasnet-lan.network":          false,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if got := err == nil; got != want {
			t.Errorf("%s present = %v, want %v", name, got, want)
		}
	}
}

// An empty PermanentMACAddress= empties [Match], which matches every link.
func TestRender_NoPermanentMACMatchesByName(t *testing.T) {
	for name, content := range map[string]string{
		"uplink": RenderUplink(domain.NetworkInterface{
			IfName: "usb0", Slot: domain.SlotSecondary, Method: domain.MethodDHCP4,
		}, 202).Content,
		"mgmt": RenderMgmt(domain.NetworkInterface{
			IfName: "enp3s0", Role: domain.RoleMgmt,
		}, "192.168.99.1/24").Content,
	} {
		if strings.Contains(content, "PermanentMACAddress=\n") {
			t.Errorf("%s: empty PermanentMACAddress= matches every link:\n%s", name, content)
		}
		if !strings.Contains(content, "Name=") {
			t.Errorf("%s: no fallback match at all:\n%s", name, content)
		}
	}
}

// mgmt uses networkd's own DHCP server: no resolver, and it needs none.
func TestRenderMgmt_FrozenFileWithNoEgressPath(t *testing.T) {
	f := RenderMgmt(domain.NetworkInterface{
		IfName: "enp3s0", PermMAC: "aa:bb:cc:dd:ee:03", Role: domain.RoleMgmt,
	}, "192.168.99.1/24")

	if f.Name != "40-nasnet-mgmt.network" {
		t.Errorf("Name = %q", f.Name)
	}
	for _, want := range []string{
		"Address=192.168.99.1/24", "DHCPServer=yes", "ConfigureWithoutCarrier=yes",
		"EmitDNS=no", "EmitRouter=no", "PoolOffset=10", "PoolSize=20",
	} {
		if !strings.Contains(f.Content, want) {
			t.Errorf("missing %q in:\n%s", want, f.Content)
		}
	}
	// EmitRouter=no is what keeps mgmt from becoming an egress path.
	if strings.Contains(f.Content, "RouteTable=") || strings.Contains(f.Content, "Gateway=") {
		t.Errorf("mgmt must never be an egress path:\n%s", f.Content)
	}
}

func TestRenderSysctl(t *testing.T) {
	got := RenderSysctl([]string{"enp1s0", "enp2s0"}, false)

	// Kernel takes max(all, per-interface), so both must be set.
	for _, want := range []string{
		"net.ipv4.conf.all.rp_filter = 2",
		"net.ipv4.conf.enp1s0.rp_filter = 2",
		"net.ipv4.conf.enp2s0.rp_filter = 2",
		"net.ipv4.tcp_fwmark_accept = 1",
		"net.ipv4.fwmark_reflect = 1",
		"net.ipv4.conf.enp1s0.arp_ignore = 1",
		"net.ipv4.conf.enp1s0.arp_announce = 2",
		"net.ipv4.conf.enp2s0.arp_ignore = 1",
		"net.ipv4.conf.enp2s0.arp_announce = 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// arp_filter decides by route lookup, and RouteTable= empties main.
	if strings.Contains(got, "arp_filter") {
		t.Error("arp_filter must not be set; arp_ignore consults no routing state")
	}
	// Stage 1 has no LAN.
	if strings.Contains(got, "ip_forward = 1") {
		t.Error("forwarding enabled without a LAN")
	}
	if !strings.Contains(RenderSysctl([]string{"enp1s0"}, true), "net.ipv4.ip_forward = 1") {
		t.Error("forwarding=true did not emit ip_forward")
	}
}

func TestRenderRTTables(t *testing.T) {
	got := RenderRTTables(map[int]string{201: "nasnet-wan1", 202: "nasnet-wan2"})
	for _, want := range []string{"201\tnasnet-wan1", "202\tnasnet-wan2"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// UseDNS=no with no DNS= leaves the box routing fine and resolving nothing.
// Each uplink carries the resolver its own slot should use.
func TestRenderUplink_CarriesPerLinkDNS(t *testing.T) {
	domestic := RenderUplink(domain.NetworkInterface{
		IfName: "enp1s0", PermMAC: "aa:bb:cc:dd:ee:01",
		Role: domain.RoleWAN, Slot: domain.SlotDomestic, Method: domain.MethodDHCP4,
	}, 201).Content
	for _, want := range []string{"DNS=" + DefaultDomesticDNS, "Domains=~ir"} {
		if !strings.Contains(domestic, want) {
			t.Errorf("missing %q in the domestic uplink:\n%s", want, domestic)
		}
	}

	secondary := RenderUplink(domain.NetworkInterface{
		IfName: "enp2s0", PermMAC: "aa:bb:cc:dd:ee:02",
		Role: domain.RoleWAN, Slot: domain.SlotSecondary, Method: domain.MethodDHCP4,
	}, 202).Content
	// "~." is the catch-all routing domain: everything not claimed by another
	// link resolves here.
	for _, want := range []string{"DNS=" + DefaultForeignDNS, "Domains=~."} {
		if !strings.Contains(secondary, want) {
			t.Errorf("missing %q in the secondary uplink:\n%s", want, secondary)
		}
	}
}

// An operator-set resolver wins over the slot default.
func TestRenderUplink_OperatorDNSOverridesTheDefault(t *testing.T) {
	got := RenderUplink(domain.NetworkInterface{
		IfName: "enp1s0", PermMAC: "aa:bb:cc:dd:ee:01",
		Role: domain.RoleWAN, Slot: domain.SlotDomestic, Method: domain.MethodDHCP4,
		DNSServer: "10.0.0.53", DNSDomains: "~corp",
	}, 201).Content
	if !strings.Contains(got, "DNS=10.0.0.53") || !strings.Contains(got, "Domains=~corp") {
		t.Errorf("operator DNS was ignored:\n%s", got)
	}
	if strings.Contains(got, DefaultDomesticDNS) {
		t.Errorf("the slot default leaked in alongside the operator's:\n%s", got)
	}
}

// Router mode is IPv4-only. A live IPv6 stack would route around the policy —
// an AAAA answer leaves by whatever the kernel picked, with no group mark.
func TestRenderSysctl_DisablesIPv6(t *testing.T) {
	got := RenderSysctl([]string{"enp1s0", "enp2s0"}, false)
	for _, want := range []string{
		"net.ipv6.conf.all.disable_ipv6 = 1",
		"net.ipv6.conf.default.disable_ipv6 = 1",
		"net.ipv6.conf.enp1s0.disable_ipv6 = 1",
		"net.ipv6.conf.enp2s0.disable_ipv6 = 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// The bridge is created after the drop-in is written, so it needs its own line.
func TestRenderSysctlWithLAN_DisablesIPv6OnTheBridge(t *testing.T) {
	got := RenderSysctlWithLAN([]string{"enp1s0"}, "lan0")
	if !strings.Contains(got, "net.ipv6.conf.lan0.disable_ipv6 = 1") {
		t.Errorf("the bridge keeps IPv6:\n%s", got)
	}
}
