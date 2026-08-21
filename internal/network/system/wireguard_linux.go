//go:build linux

package system

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type wgDevice struct{}

// NewWGDevice talks to the kernel module over netlink. Deliberately not the wg
// binary: wireguard-tools is a package that may not be installed, and its
// output is text to scrape rather than structs to read.
func NewWGDevice() WGDevice { return &wgDevice{} }

func (d *wgDevice) Ensure(ctx context.Context, ifName string, cfg WGApplyConfig) error {
	link, err := d.ensureLink(ifName, cfg.MTU)
	if err != nil {
		return err
	}

	if err := d.ensureAddress(ifName, link, cfg.Address); err != nil {
		return err
	}

	wgCfg, err := deviceConfig(cfg)
	if err != nil {
		return err
	}
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wireguard control: %w", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.ConfigureDevice(ifName, wgCfg); err != nil {
		return fmt.Errorf("configure %s: %w", ifName, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring %s up: %w", ifName, err)
	}

	// Best effort: the LAN's resolver is rendered separately, so a box without
	// resolved still gets working name lookups through dnsmasq.
	d.registerResolver(ctx, ifName, cfg.DNS)
	return nil
}

func (d *wgDevice) ensureLink(ifName string, mtu int) (netlink.Link, error) {
	if mtu <= 0 {
		// Same default the status API reports.
		mtu = domain.DefaultWGMTU
	}
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		var notFound netlink.LinkNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("look up %s: %w", ifName, err)
		}
		attrs := netlink.NewLinkAttrs()
		attrs.Name = ifName
		attrs.MTU = mtu
		if err := netlink.LinkAdd(&netlink.Wireguard{LinkAttrs: attrs}); err != nil {
			return nil, fmt.Errorf("create %s: %w", ifName, err)
		}
		if link, err = netlink.LinkByName(ifName); err != nil {
			return nil, fmt.Errorf("look up %s after creating it: %w", ifName, err)
		}
	}
	if link.Attrs().MTU != mtu {
		if err := netlink.LinkSetMTU(link, mtu); err != nil {
			return nil, fmt.Errorf("set MTU on %s: %w", ifName, err)
		}
	}
	return link, nil
}

// ensureAddress leaves exactly the configured address on the link. Switching
// profiles usually changes it, and a stale one would keep answering.
func (d *wgDevice) ensureAddress(ifName string, link netlink.Link, want netip.Prefix) error {
	if !want.IsValid() {
		return errors.New("no tunnel address")
	}
	addr := &netlink.Addr{IPNet: &net.IPNet{
		IP:   want.Addr().AsSlice(),
		Mask: net.CIDRMask(want.Bits(), 32),
	}}

	existing, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("list addresses on %s: %w", ifName, err)
	}
	for _, have := range existing {
		if have.IPNet != nil && have.IPNet.String() == addr.IPNet.String() {
			continue
		}
		if err := netlink.AddrDel(link, &have); err != nil {
			return fmt.Errorf("remove a stale address from %s: %w", ifName, err)
		}
	}
	if err := netlink.AddrReplace(link, addr); err != nil {
		return fmt.Errorf("address %s: %w", ifName, err)
	}
	return nil
}

func deviceConfig(cfg WGApplyConfig) (wgtypes.Config, error) {
	priv, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		return wgtypes.Config{}, fmt.Errorf("private key: %w", err)
	}
	pub, err := wgtypes.ParseKey(cfg.PeerPublicKey)
	if err != nil {
		return wgtypes.Config{}, fmt.Errorf("peer public key: %w", err)
	}

	peer := wgtypes.PeerConfig{
		PublicKey:         pub,
		ReplaceAllowedIPs: true,
	}
	if cfg.PresharedKey != "" {
		psk, err := wgtypes.ParseKey(cfg.PresharedKey)
		if err != nil {
			return wgtypes.Config{}, fmt.Errorf("preshared key: %w", err)
		}
		peer.PresharedKey = &psk
	}
	if cfg.Endpoint.IsValid() {
		peer.Endpoint = net.UDPAddrFromAddrPort(cfg.Endpoint)
	}
	if cfg.Keepalive > 0 {
		ka := cfg.Keepalive
		peer.PersistentKeepaliveInterval = &ka
	}
	for _, p := range cfg.AllowedIPs {
		peer.AllowedIPs = append(peer.AllowedIPs, net.IPNet{
			IP:   p.Addr().AsSlice(),
			Mask: net.CIDRMask(p.Bits(), p.Addr().BitLen()),
		})
	}

	mark := int(cfg.FirewallMark)
	out := wgtypes.Config{
		PrivateKey:   &priv,
		FirewallMark: &mark,
		ReplacePeers: true,
		Peers:        []wgtypes.PeerConfig{peer},
	}
	if cfg.ListenPort > 0 {
		port := cfg.ListenPort
		out.ListenPort = &port
	}
	return out, nil
}

// registerResolver points systemd-resolved at the tunnel for everything. The
// LAN gets its own answer from dnsmasq; this covers the box's own lookups.
func (d *wgDevice) registerResolver(ctx context.Context, ifName string, dns netip.Addr) {
	if !dns.IsValid() {
		return
	}
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return
	}
	_ = exec.CommandContext(ctx, "resolvectl", "dns", ifName, dns.String()).Run()
	_ = exec.CommandContext(ctx, "resolvectl", "domain", ifName, "~.").Run()
}

func (d *wgDevice) UpdateEndpoint(_ context.Context, ifName string, endpoint netip.AddrPort) error {
	if !endpoint.IsValid() {
		return errors.New("no endpoint")
	}
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wireguard control: %w", err)
	}
	defer func() { _ = client.Close() }()

	dev, err := client.Device(ifName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoWGDevice
		}
		return err
	}
	if len(dev.Peers) == 0 {
		return errors.New("the tunnel has no peer to move")
	}
	// UpdateOnly, so a race with a teardown cannot resurrect a peer.
	return client.ConfigureDevice(ifName, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{{
			PublicKey:  dev.Peers[0].PublicKey,
			UpdateOnly: true,
			Endpoint:   net.UDPAddrFromAddrPort(endpoint),
		}},
	})
}

func (d *wgDevice) Status(_ context.Context, ifName string) (*WGStatus, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("wireguard control: %w", err)
	}
	defer func() { _ = client.Close() }()

	dev, err := client.Device(ifName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoWGDevice
		}
		return nil, err
	}
	st := &WGStatus{ListenPort: dev.ListenPort}
	if len(dev.Peers) > 0 {
		p := dev.Peers[0]
		st.LastHandshake = p.LastHandshakeTime
		st.RxBytes, st.TxBytes = p.ReceiveBytes, p.TransmitBytes
		st.PublicKey = p.PublicKey.String()
		if p.Endpoint != nil {
			st.Endpoint = p.Endpoint.String()
		}
	}
	return st, nil
}

func (d *wgDevice) Delete(_ context.Context, ifName string) error {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("look up %s: %w", ifName, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete %s: %w", ifName, err)
	}
	return nil
}

func (d *wgDevice) List(context.Context) ([]string, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("link list: %w", err)
	}
	var out []string
	for _, l := range links {
		if IsWGLink(l.Attrs().Name) {
			out = append(out, l.Attrs().Name)
		}
	}
	return out, nil
}
