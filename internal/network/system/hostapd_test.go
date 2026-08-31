package system

import (
	"context"
	"os"
	"strings"
	"testing"
)

func apConfig() HostapdConfig {
	return HostapdConfig{
		IfName: "wlp3s0", BridgeName: "lan0",
		SSID: "nasnet", PSK: "correct horse battery staple",
		CountryCode: "IR", Band: Band2G, Channel: 6,
	}
}

func testHostapd(t *testing.T) (*Hostapd, *int) {
	t.Helper()
	restarts := 0
	h := &Hostapd{
		ConfPath:     t.TempDir() + "/nasnet-ap.conf",
		Bin:          "/nonexistent/hostapd",
		DefaultsPath: t.TempDir() + "/default-hostapd",
		IsRunning:    func(context.Context) bool { return true },
		Restart:      func(context.Context) error { restarts++; return nil },
		Stopper:      func(context.Context) error { return nil },
	}
	return h, &restarts
}

// The AP joins the LAN bridge, so it inherits DHCP, DNS, classification and NAT
func TestRenderHostapd_BridgesIntoTheLAN(t *testing.T) {
	got, err := RenderHostapd(apConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"interface=wlp3s0", // the real kernel name; we never rename
		"bridge=lan0",
		"ssid=nasnet",
		"country_code=IR",
		"channel=6",
		"hw_mode=g",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderHostapd_WPA2OnlyWhenTheBinaryLacksSAE(t *testing.T) {
	got, err := RenderHostapd(apConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"wpa=2",
		"wpa_key_mgmt=WPA-PSK\n",
		"rsn_pairwise=CCMP",
		"wpa_passphrase=correct horse battery staple",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, banned := range []string{"TKIP", "wpa_pairwise", "SAE", "ieee80211w"} {
		if strings.Contains(got, banned) {
			t.Errorf("%q must not appear in a WPA2-only render:\n%s", banned, got)
		}
	}
}

// Transition mode: WPA3 phones use SAE, old clients keep WPA2-PSK
func TestRenderHostapd_TransitionModeWhenTheBinaryHasSAE(t *testing.T) {
	c := apConfig()
	c.EnableSAE = true
	got, err := RenderHostapd(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"wpa=2", // transition mode is still RSN; wpa=3 is not a thing
		"wpa_key_mgmt=WPA-PSK SAE",
		"ieee80211w=1",
		"rsn_pairwise=CCMP",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// hostapd falls back to wpa_passphrase for SAE, so one credential covers both
	if strings.Contains(got, "sae_password") {
		t.Error("sae_password duplicates the passphrase; rely on the fallback")
	}
}

func TestRenderHostapd_RefusesWithoutACountryCode(t *testing.T) {
	for _, code := range []string{"", "00"} {
		c := apConfig()
		c.CountryCode = code
		if _, err := RenderHostapd(c); err == nil {
			t.Errorf("RenderHostapd accepted country code %q", code)
		}
	}
}

func TestRenderHostapd_RefusesAWeakPSK(t *testing.T) {
	c := apConfig()
	c.PSK = "short"
	if _, err := RenderHostapd(c); err == nil {
		t.Error("RenderHostapd accepted a PSK shorter than the WPA2 minimum of 8")
	}
}

func TestRenderHostapd_RefusesWithNoBridge(t *testing.T) {
	c := apConfig()
	c.BridgeName = ""
	if _, err := RenderHostapd(c); err == nil {
		t.Error("an AP with no bridge has nothing to bridge into and must be refused")
	}
}

func TestRenderHostapd_FiveGigUsesHWModeA(t *testing.T) {
	c := apConfig()
	c.Band, c.Channel = Band5G, 36
	got, err := RenderHostapd(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "hw_mode=a") {
		t.Errorf("5 GHz must use hw_mode=a:\n%s", got)
	}
	if !strings.Contains(got, "ieee80211n=1") || !strings.Contains(got, "ieee80211ac=1") {
		t.Errorf("5 GHz should enable n and ac:\n%s", got)
	}
	if !strings.Contains(got, "ieee80211h=1") {
		t.Errorf("5 GHz needs ieee80211h so hostapd honours the regdomain:\n%s", got)
	}
}

func TestRenderHostapd_HiddenSSID(t *testing.T) {
	c := apConfig()
	c.Hidden = true
	got, _ := RenderHostapd(c)
	if !strings.Contains(got, "ignore_broadcast_ssid=1") {
		t.Error("hidden SSID not honoured")
	}
}

// Capabilities are properties of the binary, not of the OS version
func TestHostapdBinaryProbes_FalseForAMissingBinary(t *testing.T) {
	if HostapdSupportsAX(context.Background(), "/nonexistent/hostapd") {
		t.Error("HostapdSupportsAX returned true for a missing binary")
	}
	if HostapdSupportsSAE(context.Background(), "/nonexistent/hostapd") {
		t.Error("HostapdSupportsSAE returned true for a missing binary")
	}
}

func TestPickChannel_PrefersALowNonOverlapping2GChannel(t *testing.T) {
	ch, err := PickChannel(apCapableRadio(), Band2G)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 1 && ch != 6 && ch != 11 {
		t.Errorf("picked channel %d; want a non-overlapping 2.4 GHz channel", ch)
	}
}

func TestPickChannel_SkipsNoIRAndRadar(t *testing.T) {
	ch, err := PickChannel(apCapableRadio(), Band5G)
	if err != nil {
		t.Fatal(err)
	}
	if ch != 36 {
		t.Errorf("picked channel %d, want 36 — the others are radar, NO_IR or disabled", ch)
	}
}

func TestPickChannel_NoBeaconableChannelIsAnError(t *testing.T) {
	r := apCapableRadio()
	for i := range r.Bands[Band5G] {
		r.Bands[Band5G][i].NoIR = true
	}
	if _, err := PickChannel(r, Band5G); err == nil {
		t.Error("PickChannel returned a channel from a band with none available")
	}
}

func TestHostapdStart_RefusesAnAPIncapableRadio(t *testing.T) {
	h, _ := testHostapd(t)
	caps := apCapableRadio()
	caps.SupportsAP = false
	if err := h.Start(context.Background(), apConfig(), caps); err == nil {
		t.Error("Start accepted a radio with no AP support")
	}
}

// Auto channel resolves from what the radio may beacon on
func TestHostapdStart_PicksAChannelWhenUnset(t *testing.T) {
	h, restarts := testHostapd(t)
	c := apConfig()
	c.Channel = 0
	if err := h.Start(context.Background(), c, apCapableRadio()); err != nil {
		t.Fatal(err)
	}
	if *restarts != 1 {
		t.Fatalf("restarts = %d, want 1", *restarts)
	}
	body, err := os.ReadFile(h.ConfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "channel=1") {
		t.Errorf("auto channel did not resolve:\n%s", body)
	}
}

// The file holds the passphrase
func TestHostapdStart_ConfIs0600(t *testing.T) {
	h, _ := testHostapd(t)
	if err := h.Start(context.Background(), apConfig(), apCapableRadio()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(h.ConfPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("conf mode = %o, want 600", st.Mode().Perm())
	}
}

// The packaged unit reads DAEMON_CONF and refuses to start with it empty
func TestHostapdStart_WritesDaemonConf(t *testing.T) {
	h, _ := testHostapd(t)
	if err := h.Start(context.Background(), apConfig(), apCapableRadio()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(h.DefaultsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), h.ConfPath) {
		t.Errorf("DAEMON_CONF does not name our config:\n%s", body)
	}
}

// A needless restart deauthenticates every client
func TestHostapdEnsure_SkipsTheRestartWhenNothingChanged(t *testing.T) {
	ctx := context.Background()
	h, restarts := testHostapd(t)
	cfg, caps := apConfig(), apCapableRadio()

	if err := h.Ensure(ctx, cfg, caps); err != nil {
		t.Fatal(err)
	}
	if *restarts != 1 {
		t.Fatalf("first Ensure: %d restarts, want 1", *restarts)
	}
	if err := h.Ensure(ctx, cfg, caps); err != nil {
		t.Fatal(err)
	}
	if *restarts != 1 {
		t.Fatalf("unchanged Ensure restarted the daemon: %d", *restarts)
	}

	cfg.SSID = "renamed"
	if err := h.Ensure(ctx, cfg, caps); err != nil {
		t.Fatal(err)
	}
	if *restarts != 2 {
		t.Fatalf("changed config did not restart: %d", *restarts)
	}
}

func TestHostapdEnsure_RestartsADeadDaemonEvenUnchanged(t *testing.T) {
	h, restarts := testHostapd(t)
	h.IsRunning = func(context.Context) bool { return false }
	for i := 0; i < 2; i++ {
		if err := h.Ensure(context.Background(), apConfig(), apCapableRadio()); err != nil {
			t.Fatal(err)
		}
	}
	if *restarts != 2 {
		t.Fatalf("a dead daemon must be restarted every time: %d", *restarts)
	}
}

// Stop removes the config so a later Start cannot pick up a stale render
func TestHostapdStop_RemovesTheConfig(t *testing.T) {
	h, _ := testHostapd(t)
	if err := h.Start(context.Background(), apConfig(), apCapableRadio()); err != nil {
		t.Fatal(err)
	}
	if err := h.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.ConfPath); !os.IsNotExist(err) {
		t.Errorf("conf survived Stop: %v", err)
	}
	// Idempotent: a second Stop is what Reconcile does on every pass
	if err := h.Stop(context.Background()); err != nil {
		t.Errorf("second Stop failed: %v", err)
	}
}

func TestNewHostapd_UsesPaths(t *testing.T) {
	h := NewHostapd(Paths{HostapdDir: "/tmp/hostapd"})
	if h.ConfPath != "/tmp/hostapd/nasnet-ap.conf" {
		t.Errorf("ConfPath = %q", h.ConfPath)
	}
	if NewHostapd(Paths{}).ConfPath != "/etc/hostapd/nasnet-ap.conf" {
		t.Error("an empty HostapdDir must fall back to /etc/hostapd")
	}
}
