package system

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// iwdNetworkDir is where iwd reads pre-seeded credentials
const iwdNetworkDir = "/var/lib/iwd"

type WifiNetwork struct {
	SSID      string `json:"ssid"`
	Security  string `json:"security"`
	SignalDBm int    `json:"signal_dbm"`
	Connected bool   `json:"connected"`
	Known     bool   `json:"known"`
}

// StationClient drives a radio in station mode.
//
// networkd cannot associate, so an uplink radio needs iwd. Once associated,
// networkd owns the addressing exactly as it does for an ethernet uplink —
// which is why a station radio's .network file is no different.
//
// One thing the health ladder must not assume: carrier is NOT a reliable signal
// that the upstream network is gone. A deauth drops it, but an AP that simply
// vanishes — power cut, out of range — leaves carrier at 1 and iwd reporting
// "connected" for well over 90s (measured on mac80211_hwsim, 2026-08-31). The
// gateway rung is what catches that: ARP to the gateway goes FAILED and the
// 3-failure damper fires in ~6s. Do not "fix" the carrier probe for this.
type StationClient interface {
	Scan(ctx context.Context, ifName string) error
	Networks(ctx context.Context, ifName string) ([]WifiNetwork, error)
	Connect(ctx context.Context, ifName, ssid, psk string) error
	Disconnect(ctx context.Context, ifName string) error
	Forget(ctx context.Context, ifName, ssid string) error
	State(ctx context.Context, ifName string) (string, error)
}

// validateStationCredentials rejects what cannot associate. iwd fails
// asynchronously with no error back, so this is the difference between a form
// error and silence. An empty PSK is an open network.
func validateStationCredentials(ssid, psk string) error {
	if ssid == "" {
		return fmt.Errorf("no SSID")
	}
	if psk != "" && len(psk) < wpa2MinPSKLen {
		return fmt.Errorf("the passphrase must be at least %d characters", wpa2MinPSKLen)
	}
	return nil
}

// SignalPercent maps dBm onto 0-100. -50 and above is full, -100 and below empty.
func SignalPercent(dbm int) int {
	switch {
	case dbm >= -50:
		return 100
	case dbm <= -100:
		return 0
	}
	return (dbm + 100) * 2
}

func iwdSecurityLabel(raw string) string {
	switch raw {
	case "psk":
		return "WPA2"
	case "open":
		return "Open"
	case "8021x":
		return "Enterprise"
	case "wep":
		return "WEP (insecure)"
	}
	return "Unknown"
}

// writeIWDNetworkFile pre-seeds a passphrase, which is iwd's documented way for
// an unattended system and also what makes the SSID a known network it
// autoconnects to at boot. 0600: it holds a secret.
func writeIWDNetworkFile(ssid, psk string) error {
	if err := os.MkdirAll(iwdNetworkDir, 0o700); err != nil {
		return fmt.Errorf("iwd network dir: %w", err)
	}
	body := fmt.Sprintf("# Managed by nasnet.\n[Security]\nPassphrase=%s\n", psk)
	return os.WriteFile(iwdNetworkFilePath(ssid), []byte(body), 0o600)
}

func removeIWDNetworkFile(ssid string) error {
	if err := os.Remove(iwdNetworkFilePath(ssid)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove iwd network file: %w", err)
	}
	return nil
}

func iwdNetworkFilePath(ssid string) string {
	return filepath.Join(iwdNetworkDir, iwdEscapeSSID(ssid)+".psk")
}

// iwdEscapeSSID follows iwd's filename convention: anything not plain printable
// ASCII is hex-encoded and wrapped in '='
func iwdEscapeSSID(ssid string) string {
	for _, r := range ssid {
		if r < 0x20 || r > 0x7e || r == '/' || r == '=' {
			return "=" + hex.EncodeToString([]byte(ssid)) + "="
		}
	}
	return ssid
}

// FakeStationClient is the in-memory implementation for CI
type FakeStationClient struct {
	mu        sync.Mutex
	Scanned   []string
	Nets      []WifiNetwork
	Connected map[string]string
	Err       error
}

func NewFakeStationClient() *FakeStationClient {
	return &FakeStationClient{Connected: map[string]string{}}
}

func (f *FakeStationClient) Scan(_ context.Context, ifName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Scanned = append(f.Scanned, ifName)
	return nil
}

func (f *FakeStationClient) Networks(context.Context, string) ([]WifiNetwork, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]WifiNetwork(nil), f.Nets...), f.Err
}

func (f *FakeStationClient) Connect(_ context.Context, ifName, ssid, psk string) error {
	if err := validateStationCredentials(ssid, psk); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Connected[ifName] = ssid
	return nil
}

func (f *FakeStationClient) Disconnect(_ context.Context, ifName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.Connected, ifName)
	return f.Err
}

func (f *FakeStationClient) Forget(_ context.Context, ifName, ssid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Connected[ifName] == ssid {
		delete(f.Connected, ifName)
	}
	return f.Err
}

func (f *FakeStationClient) State(_ context.Context, ifName string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if f.Connected[ifName] != "" {
		return "connected", nil
	}
	return "disconnected", nil
}
