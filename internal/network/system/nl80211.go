package system

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// nl80211 interface types. Stable kernel ABI.
const (
	nl80211IftypeStation = 2
	nl80211IftypeAP      = 3
)

type Band string

const (
	Band2G Band = "2g"
	Band5G Band = "5g"
	Band6G Band = "6g"
)

// Channel is one frequency and the three separate reasons we might not beacon on it
type Channel struct {
	Number  int `json:"number"`
	FreqMHz int `json:"freq_mhz"`
	// NoIR is "no initiating radiation": we may listen but not beacon. Regdomain
	// 00 sets it on nearly all 5 GHz, which is what kills hostapd at startup.
	NoIR bool `json:"no_ir"`
	// Radar means DFS, which we do not implement
	Radar               bool `json:"radar"`
	DisabledByRegdomain bool `json:"disabled_by_regdomain"`
}

func (c Channel) Beaconable() bool {
	return !c.NoIR && !c.Radar && !c.DisabledByRegdomain
}

// RadioCaps is probed, never inferred from a kernel or distro version
type RadioCaps struct {
	Phy             string             `json:"phy"`
	SupportsAP      bool               `json:"supports_ap"`
	SupportsSTA     bool               `json:"supports_sta"`
	Bands           map[Band][]Channel `json:"bands"`
	MaxAPInterfaces int                `json:"max_ap_interfaces"`
	// APAndSTAConcurrent is diagnostics only. We never offer it even when a
	// radio claims it: same-channel lock, no DFS, halved throughput.
	APAndSTAConcurrent bool `json:"ap_and_sta_concurrent"`
}

func (c RadioCaps) BeaconableChannels(b Band) []Channel {
	if !c.SupportsAP {
		return nil
	}
	var out []Channel
	for _, ch := range c.Bands[b] {
		if ch.Beaconable() {
			out = append(out, ch)
		}
	}
	return out
}

func (c RadioCaps) CanBeacon(b Band) bool { return len(c.BeaconableChannels(b)) > 0 }

// RadioProber is behind an interface because CI runs unprivileged with no radios
type RadioProber interface {
	Radios(ctx context.Context) ([]RadioCaps, error)
	RadioFor(ctx context.Context, phy string) (*RadioCaps, error)
}

type FakeRadioProber struct {
	Caps []RadioCaps
	Err  error
}

func (f *FakeRadioProber) Radios(context.Context) ([]RadioCaps, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]RadioCaps(nil), f.Caps...), nil
}

func (f *FakeRadioProber) RadioFor(_ context.Context, phy string) (*RadioCaps, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	for i := range f.Caps {
		if f.Caps[i].Phy == phy {
			c := f.Caps[i]
			return &c, nil
		}
	}
	return nil, fmt.Errorf("radio %q not found", phy)
}

// bandForFreq keeps 5.9 and 6 GHz out of the 5 GHz bucket
func bandForFreq(mhz int) Band {
	switch {
	case mhz >= 2400 && mhz < 2500:
		return Band2G
	case mhz >= 4900 && mhz < 5900:
		return Band5G
	case mhz >= 5900:
		return Band6G
	}
	return ""
}

func channelForFreq(mhz int) int {
	switch {
	case mhz == 2484:
		return 14
	case mhz >= 2412 && mhz <= 2472:
		return (mhz - 2407) / 5
	case mhz >= 5955:
		return (mhz - 5950) / 5
	case mhz >= 5000 && mhz < 5900:
		return (mhz - 5000) / 5
	}
	return 0
}

func normalizeRegDomain(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

type iftypeCombos struct {
	maxAP    int
	apAndSTA bool
}

// parseIftypes reads the "Supported interface modes" and the combination blocks
func parseIftypes(out string) (map[int]bool, iftypeCombos) {
	types := map[int]bool{}
	var combos iftypeCombos
	inModes := false

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Supported interface modes:"):
			inModes = true
			continue
		case inModes && strings.HasPrefix(line, "* "):
			switch strings.TrimPrefix(line, "* ") {
			case "AP":
				types[nl80211IftypeAP] = true
			case "managed":
				types[nl80211IftypeStation] = true
			}
			continue
		case inModes:
			inModes = false
		}

		if strings.Contains(line, "#{") && strings.Contains(line, "AP") {
			// e.g. "#{ managed } <= 1, #{ AP, P2P-client } <= 1,"
			if strings.Contains(line, "managed") && !strings.Contains(line, "#{ managed, AP") {
				combos.apAndSTA = true
			}
			if n := parseTrailingCount(line); n > combos.maxAP {
				combos.maxAP = n
			}
		}
	}
	if combos.maxAP == 0 && types[nl80211IftypeAP] {
		combos.maxAP = 1
	}
	return types, combos
}

// parseTrailingCount pulls the "<= N" limit out of a combination line
func parseTrailingCount(line string) int {
	_, rest, ok := strings.Cut(line, "<=")
	if !ok {
		return 0
	}
	fields := strings.FieldsFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return n
}

// parseChannels keeps the three flags that decide whether an AP may transmit.
//
// Two formats in the wild: iw 5.x prints "* 2412 MHz [1]", iw 6.x prints
// "* 2412.0 MHz [1]". Anything that cannot be read as a frequency is skipped
// rather than guessed at.
func parseChannels(out string) []Channel {
	var chans []Channel
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "* ") || !strings.Contains(line, "MHz") {
			continue
		}
		mhzField, _, _ := strings.Cut(strings.TrimPrefix(line, "* "), " MHz")
		mhz := parseMHz(mhzField)
		if mhz == 0 {
			continue
		}
		lower := strings.ToLower(line)
		ch := Channel{
			FreqMHz:             mhz,
			NoIR:                strings.Contains(lower, "no ir"),
			Radar:               strings.Contains(lower, "radar detection"),
			DisabledByRegdomain: strings.Contains(lower, "disabled"),
		}
		// iw prints the channel number in brackets. Trust it over arithmetic:
		// it is right about 6 GHz and about anything the bands gain later.
		if n, ok := bracketNumber(line); ok {
			ch.Number = n
		} else {
			ch.Number = channelForFreq(mhz)
		}
		chans = append(chans, ch)
	}
	return chans
}

// parseMHz reads "2412" and "2412.0" alike, truncating any fraction. 0 means
// the field was not a frequency.
func parseMHz(field string) int {
	whole, _, _ := strings.Cut(strings.TrimSpace(field), ".")
	mhz, err := strconv.Atoi(whole)
	if err != nil || mhz <= 0 {
		return 0
	}
	return mhz
}

// bracketNumber pulls the channel number out of "* 2412.0 MHz [1] (20.0 dBm)"
func bracketNumber(line string) (int, bool) {
	_, rest, ok := strings.Cut(line, "[")
	if !ok {
		return 0, false
	}
	inner, _, ok := strings.Cut(rest, "]")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(inner))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
