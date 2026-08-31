package netif

import "testing"

// The order in the spec is load-bearing: loopback before wifi before virtual
// before bus-specific, because a wifi device also has a PCI or USB subsystem
// and a bridge also has no `device` symlink.
func TestClassify_TableDrivenOrder(t *testing.T) {
	cases := []struct {
		name       string
		probe      Probe
		wantSource Source
		minConf    int
	}{
		{
			name:       "loopback by name",
			probe:      Probe{IfName: "lo", ARPType: 772},
			wantSource: SourceLoopback, minConf: 100,
		},
		{
			name:       "loopback by arptype even with another name",
			probe:      Probe{IfName: "lo0", ARPType: 772},
			wantSource: SourceLoopback, minConf: 100,
		},
		{
			name:       "pci wifi wins over the pci bus",
			probe:      Probe{IfName: "wlp3s0", Subsystem: "pci", HasPHY80211: true, Driver: "iwlwifi"},
			wantSource: SourceWifiPCI, minConf: 100,
		},
		{
			name:       "usb wifi",
			probe:      Probe{IfName: "wlx00", Subsystem: "usb", HasPHY80211: true, Driver: "mt7601u"},
			wantSource: SourceWifiUSB, minConf: 100,
		},
		{
			name:       "bridge is virtual, not unknown",
			probe:      Probe{IfName: "br0", HasLinkInfoKind: true, LinkKind: "bridge"},
			wantSource: SourceVirtBridge, minConf: 100,
		},
		{
			name:       "vlan",
			probe:      Probe{IfName: "enp1s0.7", HasLinkInfoKind: true, LinkKind: "vlan"},
			wantSource: SourceVirtVLAN, minConf: 100,
		},
		{
			name:       "bond",
			probe:      Probe{IfName: "bond0", HasLinkInfoKind: true, LinkKind: "bond"},
			wantSource: SourceVirtBond, minConf: 100,
		},
		{
			name:       "wireguard is virt_other and must be hidden",
			probe:      Probe{IfName: "wg0", HasLinkInfoKind: true, LinkKind: "wireguard"},
			wantSource: SourceVirtOther, minConf: 100,
		},
		{
			name:       "iphone tether by driver",
			probe:      Probe{IfName: "enx00", Subsystem: "usb", Driver: "ipheth"},
			wantSource: SourceTetherIPhone, minConf: 100,
		},
		{
			name:       "usb wwan by driver",
			probe:      Probe{IfName: "wwan0", Subsystem: "usb", Driver: "qmi_wwan"},
			wantSource: SourceWWANUSB, minConf: 100,
		},
		{
			name:       "usb wwan by raw-ip arptype",
			probe:      Probe{IfName: "wwan0", Subsystem: "usb", Driver: "cdc_mbim", ARPType: 519},
			wantSource: SourceWWANUSB, minConf: 100,
		},
		{
			name: "android tether: cdc_ncm plus an MTP/ADB sibling is the strongest signal",
			probe: Probe{IfName: "enp0s20u1", Subsystem: "usb", Driver: "cdc_ncm",
				USBSiblingFunctions: []string{"mtp", "adb"}},
			wantSource: SourceTetherAndroid, minConf: 70,
		},
		{
			// What udev actually hands us: ff4201 is ADB, ffff00 is MTP.
			name: "android tether: the same siblings as udev spells them",
			probe: Probe{IfName: "enp0s20u2", Subsystem: "usb", Driver: "rndis_host",
				USBSiblingFunctions: []string{"ffff00", "ff4201"}},
			wantSource: SourceTetherAndroid, minConf: 70,
		},
		{
			name:       "cdc_ncm with no phone hint is a generic dongle, low confidence",
			probe:      Probe{IfName: "enx11", Subsystem: "usb", Driver: "cdc_ncm"},
			wantSource: SourceEthUSB, minConf: 1,
		},
		{
			name:       "plain usb ethernet",
			probe:      Probe{IfName: "enx22", Subsystem: "usb", Driver: "ax88179_178a"},
			wantSource: SourceEthUSB, minConf: 100,
		},
		{
			name:       "pcie wwan by driver",
			probe:      Probe{IfName: "wwan0", Subsystem: "pci", Driver: "mhi_net"},
			wantSource: SourceWWANPCIe, minConf: 100,
		},
		{
			name:       "onboard ethernet",
			probe:      Probe{IfName: "eno1", Subsystem: "pci", Driver: "igc", OnboardName: "eno1"},
			wantSource: SourceEthOnboard, minConf: 100,
		},
		{
			name:       "discrete pci ethernet",
			probe:      Probe{IfName: "enp2s0", Subsystem: "pci", Driver: "r8169"},
			wantSource: SourceEthPCI, minConf: 100,
		},
		{
			name:       "platform ethernet",
			probe:      Probe{IfName: "eth0", Subsystem: "platform", Driver: "macb"},
			wantSource: SourceEthPlatform, minConf: 100,
		},
		{
			name:       "nothing matched",
			probe:      Probe{IfName: "weird0"},
			wantSource: SourceUnknown, minConf: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, conf := Classify(c.probe)
			if got != c.wantSource {
				t.Errorf("source = %q, want %q", got, c.wantSource)
			}
			if conf < c.minConf {
				t.Errorf("confidence = %d, want >= %d", conf, c.minConf)
			}
			if conf > 100 || conf < 0 {
				t.Errorf("confidence %d out of range", conf)
			}
		})
	}
}

// A low-confidence device must never be silently admitted to a LAN role, so
// the classifier has to report the ambiguity rather than guess.
func TestClassify_AmbiguousUSBIsBelowFullConfidence(t *testing.T) {
	_, conf := Classify(Probe{IfName: "enx11", Subsystem: "usb", Driver: "cdc_ether"})
	if conf >= 100 {
		t.Errorf("cdc_ether with no hints reported confidence %d; it is genuinely ambiguous", conf)
	}
}

func TestAssignable(t *testing.T) {
	hidden := []Source{SourceLoopback, SourceVirtOther}
	for _, s := range hidden {
		if Assignable(s) {
			t.Errorf("%q must be hidden from the UI entirely", s)
		}
	}
	for _, s := range []Source{SourceEthOnboard, SourceEthPCI, SourceEthUSB, SourceWifiPCI, SourceVirtBridge} {
		if !Assignable(s) {
			t.Errorf("%q must be assignable", s)
		}
	}
}

// Two radios whose phy name is unreadable must never alias into one value, or
// V13 treats them as the same radio.
func TestPhyFallback_NeverAliasesTwoRadios(t *testing.T) {
	a := phyFallback("wlan0", "")
	b := phyFallback("wlan1", "")
	if a == b {
		t.Fatalf("both unreadable radios resolved to %q", a)
	}
	if got := phyFallback("wlan0", "phy3"); got != "phy3" {
		t.Fatalf("a readable name was overridden: %q", got)
	}
}
