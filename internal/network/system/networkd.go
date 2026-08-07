package system

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// One rendered networkd unit
type UplinkFile struct {
	Name    string
	Content string
}

// Starlink dish API, outside 100.64.0.0/10, so it needs its own route
const dishSubnet = "192.168.100.0/24"

// RenderUplink renders an uplink's .network file
func RenderUplink(in domain.NetworkInterface, table int) UplinkFile {
	var b strings.Builder

	b.WriteString("# Managed by nasnet. Do not edit (regenerated on every apply).\n")
	b.WriteString("[Match]\n")
	fmt.Fprintf(&b, "PermanentMACAddress=%s\n", in.PermMAC)

	b.WriteString("\n[Network]\n")
	if in.Method == domain.MethodStatic && in.StaticAddress != "" {
		fmt.Fprintf(&b, "Address=%s\n", in.StaticAddress)
	} else {
		b.WriteString("DHCP=ipv4\n")
	}
	// No IPv6 means no IPv6 bypassing the routing policy.
	b.WriteString("IPv6AcceptRA=no\n")
	b.WriteString("LinkLocalAddressing=ipv4\n")
	if in.DNSServer != "" {
		fmt.Fprintf(&b, "DNS=%s\n", in.DNSServer)
	}
	if in.DNSDomains != "" {
		// Routing domain per link, resolved by systemd-resolved.
		fmt.Fprintf(&b, "Domains=%s\n", in.DNSDomains)
	}

	if in.Method == domain.MethodDHCP4 {
		b.WriteString("\n[DHCPv4]\n")
		fmt.Fprintf(&b, "RouteTable=%d\n", table)
		b.WriteString("UseDNS=no\n")
		b.WriteString("UseNTP=no\n")
		b.WriteString("RouteMetric=100\n")
	}

	if in.Method == domain.MethodStatic && in.StaticGateway != "" {
		b.WriteString("\n[Route]\n")
		fmt.Fprintf(&b, "Gateway=%s\n", in.StaticGateway)
		fmt.Fprintf(&b, "Table=%d\n", table)

		if connected := connectedSubnet(in.StaticAddress); connected != "" {
			b.WriteString("\n[Route]\n")
			fmt.Fprintf(&b, "Destination=%s\n", connected)
			b.WriteString("Scope=link\n")
			fmt.Fprintf(&b, "Table=%d\n", table)
		}
	}

	if in.Slot == domain.SlotSecondary {
		b.WriteString("\n[Route]\n")
		fmt.Fprintf(&b, "Destination=%s\n", dishSubnet)
		b.WriteString("Scope=link\n")
		fmt.Fprintf(&b, "Table=%d\n", table)
	}

	slot := string(in.Slot)
	if slot == "" {
		slot = "unassigned"
	}
	return UplinkFile{Name: fmt.Sprintf("10-nasnet-wan-%s.network", slot), Content: b.String()}
}

// RenderMgmt renders the management port. Written once at role assignment, then frozen.
func RenderMgmt(in domain.NetworkInterface, cidr string) UplinkFile {
	var b strings.Builder
	b.WriteString("# Managed by nasnet. Written once at role assignment, then frozen.\n")
	b.WriteString("[Match]\n")
	fmt.Fprintf(&b, "PermanentMACAddress=%s\n", in.PermMAC)
	b.WriteString("\n[Network]\n")
	fmt.Fprintf(&b, "Address=%s\n", cidr)
	b.WriteString("IPv6AcceptRA=no\n")
	b.WriteString("LinkLocalAddressing=ipv4\n")
	b.WriteString("DHCPServer=yes\n")
	b.WriteString("ConfigureWithoutCarrier=yes\n")
	b.WriteString("\n[DHCPServer]\n")
	b.WriteString("PoolOffset=10\n")
	b.WriteString("PoolSize=20\n")
	// EmitRouter=no keeps mgmt from becoming an egress path.
	b.WriteString("EmitDNS=no\n")
	b.WriteString("EmitRouter=no\n")
	return UplinkFile{Name: "40-nasnet-mgmt.network", Content: b.String()}
}

// RenderNetworkdConf keeps networkd's hands off our routing policy
func RenderNetworkdConf() UplinkFile {
	return UplinkFile{
		Name: "10-nasnet.conf",
		Content: "# Managed by nasnet. Do not edit (regenerated on every apply).\n" +
			"[Network]\n" +
			"ManageForeignRoutingPolicyRules=no\n" +
			"ManageForeignRoutes=no\n",
	}
}

// EnsureNetworkdConf writes the drop-in and restarts networkd when it changed
func EnsureNetworkdConf(ctx context.Context, p Paths) error {
	f := RenderNetworkdConf()
	path := filepath.Join(p.NetworkdConfDir, f.Name)
	if old, err := os.ReadFile(path); err == nil && string(old) == f.Content {
		return nil
	}
	if err := WriteFiles(p.NetworkdConfDir, []UplinkFile{f}); err != nil {
		return err
	}
	err := exec.CommandContext(ctx, "systemctl", "restart", "systemd-networkd").Run()
	if errors.Is(err, exec.ErrNotFound) {
		return nil // no systemd here (tests, containers)
	}
	if err != nil {
		return err
	}
	return waitNetworkdReady(ctx)
}

// waitNetworkdReady polls until networkd answers again. systemctl returns once
// the unit is active, but its dbus alias is registered a moment later, and the
// apply's own reload lands in that gap: "Unit dbus-org.freedesktop.network1
// .service not found", which fails the whole apply.
func waitNetworkdReady(ctx context.Context) error {
	var last error
	for range 20 {
		if err := exec.CommandContext(ctx, "networkctl", "reload").Run(); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("systemd-networkd did not come back after the restart: %w", last)
}

// RenderRTTables is cosmetic. the kernel only cares about the numbers.
func RenderRTTables(tables map[int]string) string {
	nums := make([]int, 0, len(tables))
	for n := range tables {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	var b strings.Builder
	b.WriteString("# Managed by nasnet.\n")
	for _, n := range nums {
		fmt.Fprintf(&b, "%d\t%s\n", n, tables[n])
	}
	return b.String()
}

// RenderSysctl builds the sysctl.d drop-in. A drop-in, not [Network]
// IPv4Forwarding=, which needs systemd 256; we target 255.
func RenderSysctl(uplinkNames []string, forwarding bool) string {
	names := append([]string(nil), uplinkNames...)
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Managed by nasnet. Router mode kernel settings.\n\n")

	if forwarding {
		b.WriteString("# forwarding, for LAN clients\n")
		b.WriteString("net.ipv4.ip_forward = 1\n\n")
	}

	b.WriteString("# Loose rp_filter: strict silently drops dual-WAN return traffic.\n")
	b.WriteString("# Kernel takes max(all, per-interface), so set both.\n")
	b.WriteString("net.ipv4.conf.all.rp_filter = 2\n")
	for _, n := range names {
		fmt.Fprintf(&b, "net.ipv4.conf.%s.rp_filter = 2\n", n)
	}

	b.WriteString("\n# accept()ed sockets inherit the SYN's mark, so TCP inbounds reply out\n")
	b.WriteString("# the uplink they arrived on with no application change.\n")
	b.WriteString("net.ipv4.tcp_fwmark_accept = 1\n")

	b.WriteString("\n# Socketless kernel packets (RST, ICMP errors) inherit the incoming mark.\n")
	b.WriteString("# Without it our ICMP frag-needed has no route and PMTU black-holes.\n")
	b.WriteString("net.ipv4.fwmark_reflect = 1\n")

	b.WriteString("\n# ARP hardening. Deliberately not the route-lookup variant: RouteTable=\n")
	b.WriteString("# moves each uplink's connected route out of main, so it would only\n")
	b.WriteString("# resolve by accident.\n")
	for _, n := range names {
		fmt.Fprintf(&b, "net.ipv4.conf.%s.arp_ignore = 1\n", n)
		fmt.Fprintf(&b, "net.ipv4.conf.%s.arp_announce = 2\n", n)
	}

	return b.String()
}

// WriteFiles writes rendered units into dir
func WriteFiles(dir string, files []UplinkFile) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	for _, f := range files {
		p := filepath.Join(dir, f.Name)
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	return nil
}

// connectedSubnet turns "192.168.1.34/24" into "192.168.1.0/24".
func connectedSubnet(cidr string) string {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return ""
	}
	return p.Masked().String()
}
