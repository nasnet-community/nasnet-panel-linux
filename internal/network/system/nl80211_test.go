package system

import "testing"

func apCapableRadio() RadioCaps {
	return RadioCaps{
		Phy: "phy0", SupportsAP: true, SupportsSTA: true, MaxAPInterfaces: 1,
		APAndSTAConcurrent: false,
		Bands: map[Band][]Channel{
			Band2G: {
				{Number: 1, FreqMHz: 2412},
				{Number: 6, FreqMHz: 2437},
				{Number: 11, FreqMHz: 2462},
			},
			Band5G: {
				{Number: 36, FreqMHz: 5180},
				{Number: 52, FreqMHz: 5260, Radar: true},
				{Number: 100, FreqMHz: 5500, NoIR: true},
				{Number: 149, FreqMHz: 5745, DisabledByRegdomain: true},
			},
		},
	}
}

// Turns a mysterious hostapd death into a UI message
func TestBeaconableChannels_ExcludesNoIRRadarAndDisabled(t *testing.T) {
	got := apCapableRadio().BeaconableChannels(Band5G)
	if len(got) != 1 {
		t.Fatalf("got %d beaconable 5 GHz channels, want 1: %+v", len(got), got)
	}
	if got[0].Number != 36 {
		t.Errorf("beaconable channel = %d, want 36", got[0].Number)
	}
}

func TestCanBeacon(t *testing.T) {
	r := apCapableRadio()
	if !r.CanBeacon(Band2G) {
		t.Error("2 GHz should be beaconable")
	}
	if !r.CanBeacon(Band5G) {
		t.Error("5 GHz should be beaconable — channel 36 is clear")
	}
	if r.CanBeacon(Band6G) {
		t.Error("6 GHz has no channels at all and must not be beaconable")
	}
}

// Regdomain 00 marks nearly all 5 GHz NO_IR, which is the state behind
// "Channel N (primary) not allowed for AP mode"
func TestCanBeacon_AllNoIRMeansNoBand(t *testing.T) {
	r := apCapableRadio()
	for i := range r.Bands[Band5G] {
		r.Bands[Band5G][i].NoIR = true
	}
	if r.CanBeacon(Band5G) {
		t.Error("a band with every channel NO_IR reported beaconable")
	}
}

func TestRadioCaps_STAOnlyRadio(t *testing.T) {
	r := apCapableRadio()
	r.SupportsAP = false
	if r.CanBeacon(Band2G) {
		t.Error("a radio with no AP support reported it can beacon")
	}
}

func TestFakeRadioProber(t *testing.T) {
	var _ RadioProber = &FakeRadioProber{}
	f := &FakeRadioProber{Caps: []RadioCaps{apCapableRadio()}}

	got, err := f.RadioFor(nil, "phy0")
	if err != nil || got == nil || got.Phy != "phy0" {
		t.Fatalf("RadioFor(phy0) = %+v, %v", got, err)
	}
	if _, err := f.RadioFor(nil, "phy9"); err == nil {
		t.Error("RadioFor accepted an unknown phy")
	}
}

func TestBandForFreq(t *testing.T) {
	cases := map[int]Band{2412: Band2G, 2484: Band2G, 5180: Band5G, 5745: Band5G, 5955: Band6G, 900: ""}
	for mhz, want := range cases {
		if got := bandForFreq(mhz); got != want {
			t.Errorf("bandForFreq(%d) = %q, want %q", mhz, got, want)
		}
	}
}

func TestChannelForFreq(t *testing.T) {
	cases := map[int]int{2412: 1, 2437: 6, 2462: 11, 2484: 14, 5180: 36, 5745: 149, 5955: 1}
	for mhz, want := range cases {
		if got := channelForFreq(mhz); got != want {
			t.Errorf("channelForFreq(%d) = %d, want %d", mhz, got, want)
		}
	}
}

// Trimmed real `iw phy phy0 info` output. The parse is against this shape.
const iwPhyInfoSample = `Wiphy phy0
	max # scan SSIDs: 4
	Supported interface modes:
		 * IBSS
		 * managed
		 * AP
		 * AP/VLAN
		 * monitor
	Band 1:
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2484 MHz [14] (disabled)
	Band 2:
		Frequencies:
			* 5180 MHz [36] (20.0 dBm)
			* 5260 MHz [52] (23.0 dBm) (radar detection)
			* 5500 MHz [100] (23.0 dBm) (no IR, radar detection)
			* 5745 MHz [149] (disabled)
	valid interface combinations:
		 * #{ managed } <= 1, #{ AP, P2P-client, P2P-GO } <= 1, #{ P2P-device } <= 1,
		   total <= 3, #channels <= 2
`

func TestParseIftypes(t *testing.T) {
	types, combos := parseIftypes(iwPhyInfoSample)
	if !types[nl80211IftypeAP] {
		t.Error("AP mode not detected")
	}
	if !types[nl80211IftypeStation] {
		t.Error("managed mode not detected")
	}
	if combos.maxAP != 1 {
		t.Errorf("maxAP = %d, want 1", combos.maxAP)
	}
	// managed and AP in separate groups is not concurrency
	if !combos.apAndSTA {
		t.Error("a line naming both managed and AP should report concurrency")
	}
}

func TestParseIftypes_APOnlyRadio(t *testing.T) {
	types, _ := parseIftypes("Wiphy phy1\n\tSupported interface modes:\n\t\t * AP\n\tBand 1:\n")
	if types[nl80211IftypeStation] {
		t.Error("station mode reported on an AP-only radio")
	}
}

func TestParseChannels(t *testing.T) {
	chans := parseChannels(iwPhyInfoSample)
	byNum := map[int]Channel{}
	for _, c := range chans {
		byNum[c.Number] = c
	}
	if len(chans) != 7 {
		t.Fatalf("parsed %d channels, want 7: %+v", len(chans), chans)
	}
	if !byNum[1].Beaconable() || !byNum[36].Beaconable() {
		t.Error("clear channels reported unusable")
	}
	if !byNum[52].Radar {
		t.Error("channel 52 radar flag lost")
	}
	if !byNum[100].NoIR || !byNum[100].Radar {
		t.Error("channel 100 should be both NO_IR and radar")
	}
	if !byNum[149].DisabledByRegdomain || !byNum[14].DisabledByRegdomain {
		t.Error("disabled channels not flagged")
	}
	for _, n := range []int{52, 100, 149, 14} {
		if byNum[n].Beaconable() {
			t.Errorf("channel %d must not be beaconable", n)
		}
	}
}

func TestParseTrailingCount(t *testing.T) {
	cases := map[string]int{
		" * #{ AP } <= 4, total <= 4": 4,
		" * #{ AP } <= 1,":            1,
		"no limit here":               0,
	}
	for line, want := range cases {
		if got := parseTrailingCount(line); got != want {
			t.Errorf("parseTrailingCount(%q) = %d, want %d", line, got, want)
		}
	}
}

// Real `iw phy phy0 info` from iw 6.7 (Ubuntu 24.04) against mac80211_hwsim.
// The fractional MHz is the point: iw 5.x printed "2412 MHz", 6.x prints
// "2412.0 MHz", and reading only the former dropped every channel — which made
// every radio look like it had no AP support at all.
const iwPhyInfo67Sample = `Wiphy phy0
	wiphy index: 0
	max # scan SSIDs: 4
	Supported interface modes:
		 * IBSS
		 * managed
		 * AP
		 * AP/VLAN
		 * monitor
		 * mesh point
		 * P2P-client
		 * P2P-GO
		 * P2P-device
	Band 1:
		Frequencies:
			* 2412.0 MHz [1] (20.0 dBm)
			* 2437.0 MHz [6] (20.0 dBm)
			* 2484.0 MHz [14] (20.0 dBm) (disabled)
	Band 2:
		Frequencies:
			* 5180.0 MHz [36] (20.0 dBm) (no IR)
			* 5260.0 MHz [52] (20.0 dBm) (radar detection)
			* 5955.0 MHz [1] (20.0 dBm)
	valid interface combinations:
		 * #{ managed } <= 2048, #{ AP, mesh point } <= 8, #{ P2P-client, P2P-GO } <= 1,
		   total <= 2048, #channels <= 1
`

func TestParseChannels_ToleratesFractionalMHz(t *testing.T) {
	chans := parseChannels(iwPhyInfo67Sample)
	if len(chans) != 6 {
		t.Fatalf("parsed %d channels, want 6: %+v", len(chans), chans)
	}
	byFreq := map[int]Channel{}
	for _, c := range chans {
		byFreq[c.FreqMHz] = c
	}
	if got := byFreq[2412]; got.Number != 1 || !got.Beaconable() {
		t.Errorf("2412 MHz = %+v", got)
	}
	if !byFreq[5180].NoIR || !byFreq[5260].Radar || !byFreq[2484].DisabledByRegdomain {
		t.Error("flags lost on the 6.x format")
	}
	// The bracketed number wins: 5955 MHz is 6 GHz channel 1, not 191
	if got := byFreq[5955]; got.Number != 1 {
		t.Errorf("5955 MHz got channel %d, want the bracketed 1", got.Number)
	}
}

// A radio must survive the whole probe on this format, or V11 rejects every AP
func TestParseIftypes_And_Channels_On67(t *testing.T) {
	types, _ := parseIftypes(iwPhyInfo67Sample)
	if !types[nl80211IftypeAP] || !types[nl80211IftypeStation] {
		t.Fatal("AP or managed mode missed on the 6.x format")
	}
	caps := RadioCaps{Phy: "phy0", SupportsAP: true, Bands: map[Band][]Channel{}}
	for _, ch := range parseChannels(iwPhyInfo67Sample) {
		if b := bandForFreq(ch.FreqMHz); b != "" {
			caps.Bands[b] = append(caps.Bands[b], ch)
		}
	}
	if !caps.CanBeacon(Band2G) {
		t.Error("2.4 GHz reported unusable; channels 1 and 6 are clear")
	}
	if caps.CanBeacon(Band5G) {
		t.Error("5 GHz has only a NO_IR and a radar channel, so it must not beacon")
	}
	if _, err := PickChannel(caps, Band2G); err != nil {
		t.Errorf("PickChannel: %v", err)
	}
}

func TestParseMHz(t *testing.T) {
	cases := map[string]int{"2412": 2412, "2412.0": 2412, " 5180.0 ": 5180, "": 0, "abc": 0, "-5": 0}
	for in, want := range cases {
		if got := parseMHz(in); got != want {
			t.Errorf("parseMHz(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestBracketNumber(t *testing.T) {
	if n, ok := bracketNumber("* 2412.0 MHz [1] (20.0 dBm)"); !ok || n != 1 {
		t.Errorf("got %d %v", n, ok)
	}
	if _, ok := bracketNumber("* 2412.0 MHz (20.0 dBm)"); ok {
		t.Error("a line with no bracket reported a number")
	}
}
