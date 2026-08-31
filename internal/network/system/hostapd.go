package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// wpa2MinPSKLen is what hostapd refuses at startup, so catch it here instead
const wpa2MinPSKLen = 8

const hostapdConfName = "nasnet-ap.conf"

type HostapdConfig struct {
	// IfName is the real kernel device. We never rename, so a rename orphans this.
	IfName string
	// BridgeName is required: an AP that bridges nowhere has no DHCP, no DNS
	// and no route out.
	BridgeName string

	SSID        string
	PSK         string
	CountryCode string

	Band    Band
	Channel int
	Hidden  bool

	RequireHT  bool
	RequireVHT bool
	// EnableAX and EnableSAE are only set after probing the binary
	EnableAX  bool
	EnableSAE bool
}

// RenderHostapd refuses every combination hostapd would reject at startup. A
// form error beats a daemon that dies 200 ms after the operator clicks Apply.
func RenderHostapd(c HostapdConfig) (string, error) {
	if c.IfName == "" {
		return "", fmt.Errorf("no interface")
	}
	if c.BridgeName == "" {
		return "", fmt.Errorf("an access point must join a LAN bridge; enable the LAN first")
	}
	if c.SSID == "" {
		return "", fmt.Errorf("no SSID")
	}
	if len(c.PSK) < wpa2MinPSKLen {
		return "", fmt.Errorf("the passphrase must be at least %d characters", wpa2MinPSKLen)
	}
	if RegDomainIsUnset(c.CountryCode) {
		return "", fmt.Errorf("a country code is required: the default regulatory domain marks " +
			"nearly all 5 GHz as no-initiating-radiation, and hostapd refuses to start")
	}
	if c.Channel <= 0 {
		return "", fmt.Errorf("no channel selected")
	}

	hwMode := "g"
	if c.Band == Band5G || c.Band == Band6G {
		hwMode = "a"
	}

	var b strings.Builder
	b.WriteString("# Managed by nasnet. Do not edit — regenerated on every apply.\n\n")
	fmt.Fprintf(&b, "interface=%s\n", c.IfName)
	// hostapd does the enslaving, not a .network file, so the AP inherits the
	// whole LAN plane and gets no addressing of its own
	fmt.Fprintf(&b, "bridge=%s\n", c.BridgeName)
	b.WriteString("driver=nl80211\n")
	b.WriteString("ctrl_interface=/var/run/hostapd\n")
	b.WriteString("ctrl_interface_group=0\n\n")

	fmt.Fprintf(&b, "ssid=%s\n", c.SSID)
	fmt.Fprintf(&b, "country_code=%s\n", normalizeRegDomain(c.CountryCode))
	// ieee80211d/h make hostapd honour the regdomain instead of assuming the world one
	b.WriteString("ieee80211d=1\n")
	if hwMode == "a" {
		b.WriteString("ieee80211h=1\n")
	}
	fmt.Fprintf(&b, "hw_mode=%s\n", hwMode)
	fmt.Fprintf(&b, "channel=%d\n", c.Channel)
	if c.Hidden {
		b.WriteString("ignore_broadcast_ssid=1\n")
	}
	b.WriteString("\n")

	b.WriteString("ieee80211n=1\n")
	b.WriteString("wmm_enabled=1\n")
	if c.RequireHT {
		b.WriteString("require_ht=1\n")
	}
	if hwMode == "a" {
		b.WriteString("ieee80211ac=1\n")
		if c.RequireVHT {
			b.WriteString("require_vht=1\n")
		}
	}
	if c.EnableAX {
		b.WriteString("ieee80211ax=1\n")
	}
	b.WriteString("\n")

	b.WriteString("auth_algs=1\n")
	b.WriteString("wpa=2\n")
	if c.EnableSAE {
		// Transition mode. ieee80211w=1 is optional MFP, which is what makes the
		// mix legal; =2 would kick every pre-802.11w client off.
		b.WriteString("wpa_key_mgmt=WPA-PSK SAE\n")
		b.WriteString("ieee80211w=1\n")
	} else {
		b.WriteString("wpa_key_mgmt=WPA-PSK\n")
	}
	b.WriteString("rsn_pairwise=CCMP\n")
	// hostapd uses this for SAE too when sae_password is unset
	fmt.Fprintf(&b, "wpa_passphrase=%s\n", c.PSK)

	return b.String(), nil
}

// hostapdBinaryHas greps for a symbol the build only carries when the matching
// CONFIG_* was set. hostapd has no capability-reporting flag.
func hostapdBinaryHas(ctx context.Context, bin string, symbols ...string) bool {
	if bin == "" {
		bin = "hostapd"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return false
	}
	args := []string{"-c"}
	for _, s := range symbols {
		args = append(args, "-e", s)
	}
	out, err := exec.CommandContext(ctx, "grep", append(args, path)...).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0"
}

func HostapdSupportsAX(ctx context.Context, bin string) bool {
	return hostapdBinaryHas(ctx, bin, "he_oper", "ieee80211ax")
}

func HostapdSupportsSAE(ctx context.Context, bin string) bool {
	return hostapdBinaryHas(ctx, bin, "sae_groups", "sae_pwe")
}

// PickChannel prefers the non-overlapping 2.4 GHz channels; on 5 GHz it takes
// the lowest clear one, which avoids DFS without implementing it.
func PickChannel(caps RadioCaps, b Band) (int, error) {
	avail := caps.BeaconableChannels(b)
	if len(avail) == 0 {
		return 0, fmt.Errorf("no usable channel on %s: every channel is radar-required, "+
			"no-initiating-radiation, or disabled by the regulatory domain (%s)", b, caps.Phy)
	}

	sort.Slice(avail, func(i, j int) bool { return avail[i].Number < avail[j].Number })

	if b == Band2G {
		for _, want := range []int{1, 6, 11} {
			for _, ch := range avail {
				if ch.Number == want {
					return want, nil
				}
			}
		}
	}
	return avail[0].Number, nil
}

// Hostapd owns the config file and the daemon
type Hostapd struct {
	ConfPath string
	Bin      string
	// DefaultsPath is /etc/default/hostapd, where the packaged unit reads DAEMON_CONF
	DefaultsPath string
	// Seams for tests; NewHostapd points them at systemctl
	IsRunning func(context.Context) bool
	Restart   func(context.Context) error
	Stopper   func(context.Context) error
}

func NewHostapd(p Paths) *Hostapd {
	dir := p.HostapdDir
	if dir == "" {
		dir = "/etc/hostapd"
	}
	h := &Hostapd{
		ConfPath:     filepath.Join(dir, hostapdConfName),
		Bin:          "hostapd",
		DefaultsPath: "/etc/default/hostapd",
	}
	h.IsRunning = func(ctx context.Context) bool {
		return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "hostapd").Run() == nil
	}
	h.Restart = func(ctx context.Context) error { return systemctl(ctx, "restart", "hostapd") }
	h.Stopper = func(ctx context.Context) error { return systemctl(ctx, "stop", "hostapd") }
	return h
}

// rendered runs the whole pre-flight and returns the config text. Shared by
// Start and Ensure so the two cannot disagree about what correct looks like.
func (h *Hostapd) rendered(ctx context.Context, c HostapdConfig, caps RadioCaps) (string, error) {
	if !caps.SupportsAP {
		return "", fmt.Errorf("radio %s does not support AP mode", caps.Phy)
	}
	if c.Channel == 0 {
		ch, err := PickChannel(caps, c.Band)
		if err != nil {
			return "", err
		}
		c.Channel = ch
	}

	// The regdomain moves before hostapd ever sees the channel. Guarded on iw
	// being present so tests and dev machines render without shelling out.
	if _, err := exec.LookPath("iw"); err == nil {
		if err := SetRegDomain(ctx, c.CountryCode); err != nil {
			return "", fmt.Errorf("set regulatory domain: %w", err)
		}
		// Re-probe: the channel list is what just moved
		if p, perr := NewRadioProber(); perr == nil {
			if fresh, ferr := p.RadioFor(ctx, caps.Phy); ferr == nil && !fresh.CanBeacon(c.Band) {
				return "", fmt.Errorf("radio %s cannot beacon on %s under regulatory domain %s",
					caps.Phy, c.Band, c.CountryCode)
			}
		}
	}

	// The binary decides the security and PHY ceiling, never a knob
	c.EnableAX = c.EnableAX && HostapdSupportsAX(ctx, h.Bin)
	c.EnableSAE = HostapdSupportsSAE(ctx, h.Bin)
	return RenderHostapd(c)
}

func (h *Hostapd) write(conf string) error {
	if err := os.MkdirAll(filepath.Dir(h.ConfPath), 0o755); err != nil {
		return fmt.Errorf("hostapd conf dir: %w", err)
	}
	// 0600: it holds the passphrase
	if err := os.WriteFile(h.ConfPath, []byte(conf), 0o600); err != nil {
		return fmt.Errorf("write hostapd conf: %w", err)
	}
	body := fmt.Sprintf("# Managed by nasnet.\nDAEMON_CONF=\"%s\"\n", h.ConfPath)
	if err := os.WriteFile(h.DefaultsPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", h.DefaultsPath, err)
	}
	return nil
}

func (h *Hostapd) Start(ctx context.Context, c HostapdConfig, caps RadioCaps) error {
	conf, err := h.rendered(ctx, c, caps)
	if err != nil {
		return err
	}
	if err := h.write(conf); err != nil {
		return err
	}
	return h.Restart(ctx)
}

// Ensure restarts only when the config changed or the unit is down. A needless
// restart deauthenticates every client.
func (h *Hostapd) Ensure(ctx context.Context, c HostapdConfig, caps RadioCaps) error {
	conf, err := h.rendered(ctx, c, caps)
	if err != nil {
		return err
	}
	if cur, rerr := os.ReadFile(h.ConfPath); rerr == nil && string(cur) == conf && h.IsRunning(ctx) {
		return nil
	}
	if err := h.write(conf); err != nil {
		return err
	}
	return h.Restart(ctx)
}

// Stop is idempotent: Reconcile calls it on every pass with no AP configured
func (h *Hostapd) Stop(ctx context.Context) error {
	if err := h.Stopper(ctx); err != nil {
		return err
	}
	if err := os.Remove(h.ConfPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove hostapd conf: %w", err)
	}
	return nil
}

// EnsureUnitActive converges a unit without a needless restart
func EnsureUnitActive(ctx context.Context, unit string, want bool) error {
	active := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).Run() == nil
	switch {
	case want && !active:
		return systemctl(ctx, "start", unit)
	case !want && active:
		return systemctl(ctx, "stop", unit)
	}
	return nil
}
