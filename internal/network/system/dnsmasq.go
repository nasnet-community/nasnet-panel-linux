package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// DNSMasqConfig is everything the LAN resolver needs. Render it and run the daemon.
type DNSMasqConfig struct {
	BridgeName string
	ListenAddr string
	RangeLow   string
	RangeHigh  string
	LeaseHours int

	// DomesticServer answers DomesticSuffix out DomesticIfName (empty -> none)
	DomesticServer string
	DomesticSuffix string
	DomesticIfName string

	// ForeignServer is the default, queried out ForeignIfName
	ForeignServer string
	ForeignIfName string

	// From NftSetSupported(), never a version check. Gates DomainSets.
	NftSetSupported bool
	// DomainSets let dnsmasq write resolved addresses into an nft set, so domain
	// rules reach LAN clients without routing them through xray
	DomainSets []DomainSet
}

// DomainSet maps a DNS suffix onto the nft sets dnsmasq should populate.
type DomainSet struct {
	Suffix string
	V4Set  string
	V6Set  string
}

// RenderDNSMasq builds the config. dnsmasq serves the LAN; the box itself uses
// resolved. Two resolvers, same policy, neither depending on the other.
func RenderDNSMasq(c DNSMasqConfig) string {
	if c.LeaseHours <= 0 {
		c.LeaseHours = 12
	}

	var b strings.Builder
	b.WriteString("# Managed by nasnet. Do not edit — regenerated on every apply.\n\n")

	// bind-interfaces binds only <ListenAddr>:53, leaving resolved's stub alone.
	// DNSStubListener=no is never set: it once left the box with no resolver.
	fmt.Fprintf(&b, "interface=%s\n", c.BridgeName)
	b.WriteString("bind-interfaces\n")
	b.WriteString("except-interface=lo\n")
	if c.ListenAddr != "" {
		fmt.Fprintf(&b, "listen-address=%s\n", c.ListenAddr)
	}
	b.WriteString("no-hosts\n")
	b.WriteString("domain-needed\n")
	b.WriteString("bogus-priv\n")

	b.WriteString("\n# LAN DHCP\n")
	fmt.Fprintf(&b, "dhcp-range=%s,%s,%dh\n", c.RangeLow, c.RangeHigh, c.LeaseHours)
	if c.ListenAddr != "" {
		fmt.Fprintf(&b, "dhcp-option=option:dns-server,%s\n", c.ListenAddr)
	}
	// IPv4 only: IPv6 is disabled on uplinks, so there is nothing to hand out.
	b.WriteString("dhcp-authoritative\n")

	b.WriteString("\n# Split DNS. @interface binds the query to that link, so it\n")
	b.WriteString("# hits the oif rules at pref 20/21 and leaves by that uplink.\n")
	if c.DomesticServer != "" && c.DomesticSuffix != "" {
		line := fmt.Sprintf("server=/%s/%s", c.DomesticSuffix, c.DomesticServer)
		if c.DomesticIfName != "" {
			line += "@" + c.DomesticIfName
		}
		b.WriteString(line + "\n")
	}
	if c.ForeignServer != "" {
		line := "server=" + c.ForeignServer
		if c.ForeignIfName != "" {
			line += "@" + c.ForeignIfName
		}
		b.WriteString(line + "\n")
	}

	// An enrichment, never a replacement: the geoip sets still catch traffic to
	// addresses dnsmasq never resolved.
	if c.NftSetSupported && len(c.DomainSets) > 0 {
		b.WriteString("\n# Domain-based classification. Feature-detected, never\n")
		b.WriteString("# version-sniffed: some builds compile HAVE_NFTSET out.\n")
		for _, ds := range c.DomainSets {
			if ds.Suffix == "" {
				continue
			}
			if ds.V4Set != "" {
				fmt.Fprintf(&b, "nftset=/%s/inet#%s#%s\n", ds.Suffix, nft.TableName, ds.V4Set)
			}
			if ds.V6Set != "" {
				fmt.Fprintf(&b, "nftset=/%s/6#inet#%s#%s\n", ds.Suffix, nft.TableName, ds.V6Set)
			}
		}
	}

	return b.String()
}

// NftSetSupported feature-detects --nftset. Never version-sniff: the specs said
// Ubuntu's 2.90 had it compiled out, the actual target has it in.
func NftSetSupported(ctx context.Context, bin string) bool {
	if bin == "" {
		bin = "dnsmasq"
	}
	return exec.CommandContext(ctx, bin, "--test",
		"--nftset=/example.com/inet#"+nft.TableName+"#probe").Run() == nil
}

// DNSMasq owns the daemon's config file and lifecycle.
type DNSMasq struct {
	ConfPath string
	Bin      string
}

func NewDNSMasq() *DNSMasq {
	return &DNSMasq{ConfPath: "/etc/dnsmasq.d/nasnet-lan.conf", Bin: "dnsmasq"}
}

func (d *DNSMasq) Write(cfg DNSMasqConfig) error {
	if err := os.MkdirAll(filepath.Dir(d.ConfPath), 0o755); err != nil {
		return fmt.Errorf("dnsmasq conf dir: %w", err)
	}
	return os.WriteFile(d.ConfPath, []byte(RenderDNSMasq(cfg)), 0o644)
}

// DNSMasqStatus separates "no unit to start" from "the unit died", because the
// operator's next move differs: install a package, or look at why it crashed.
type DNSMasqStatus struct {
	Installed bool
	Running   bool
}

func (d *DNSMasq) Status(ctx context.Context) DNSMasqStatus {
	st := DNSMasqStatus{Installed: d.ServiceInstalled(ctx)}
	if !st.Installed {
		return st
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		st.Running = true // not systemd; nothing to report
		return st
	}
	out, _ := exec.CommandContext(ctx, "systemctl", "is-active", "dnsmasq").Output()
	st.Running = strings.TrimSpace(string(out)) == "active"
	return st
}

// ServiceInstalled reports whether there is a unit to start. dnsmasq-base ships
// the binary without one, so every other probe passes on a box that cannot serve.
func (d *DNSMasq) ServiceInstalled(ctx context.Context) bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return true // not systemd (tests, containers); nothing to check
	}
	out, err := exec.CommandContext(ctx, "systemctl", "list-unit-files", "dnsmasq.service").Output()
	return err == nil && strings.Contains(string(out), "dnsmasq.service")
}

// Restart validates first: a bad render must not leave the LAN without DHCP.
func (d *DNSMasq) Restart(ctx context.Context) error {
	bin := d.Bin
	if bin == "" {
		bin = "dnsmasq"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil // no dnsmasq here (tests, containers)
	}
	out, err := exec.CommandContext(ctx, bin, "--test", "--conf-file="+d.ConfPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("dnsmasq config is invalid: %w (output: %s)",
			err, strings.TrimSpace(string(out)))
	}
	if !d.ServiceInstalled(ctx) {
		return fmt.Errorf("the dnsmasq service is not installed, so the LAN would have " +
			"no DHCP and no resolver — install the dnsmasq package (dnsmasq-base alone " +
			"provides the binary but no service)")
	}
	return systemctl(ctx, "restart", "dnsmasq")
}

// A no-op when we never wrote a config, so we do not stop somebody else's.
func (d *DNSMasq) Stop(ctx context.Context) error {
	if err := os.Remove(d.ConfPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("remove dnsmasq conf: %w", err)
	}
	if !d.ServiceInstalled(ctx) {
		return nil // nothing to stop
	}
	return systemctl(ctx, "stop", "dnsmasq")
}
