//go:build linux

package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type sysfsRadioProber struct {
	sysRoot string
}

// NewRadioProber reads sysfs and a narrow slice of `iw phy info` rather than
// opening a raw generic-netlink socket. Everything we need is there and sysfs
// needs no capability beyond read access.
func NewRadioProber() (RadioProber, error) {
	return &sysfsRadioProber{sysRoot: "/sys"}, nil
}

func (p *sysfsRadioProber) Radios(ctx context.Context) ([]RadioCaps, error) {
	base := filepath.Join(p.sysRoot, "class", "ieee80211")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no radios on this box
		}
		return nil, fmt.Errorf("read %s: %w", base, err)
	}

	var out []RadioCaps
	for _, e := range entries {
		caps, err := p.probeOne(ctx, e.Name())
		if err != nil {
			continue // a radio we cannot read is a radio we will not offer
		}
		out = append(out, *caps)
	}
	return out, nil
}

func (p *sysfsRadioProber) RadioFor(ctx context.Context, phy string) (*RadioCaps, error) {
	dir := filepath.Join(p.sysRoot, "class", "ieee80211", phy)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("radio %q not found: %w", phy, err)
	}
	return p.probeOne(ctx, phy)
}

func (p *sysfsRadioProber) probeOne(ctx context.Context, phy string) (*RadioCaps, error) {
	// One `iw phy info` call: the iftype list and the frequency list both live in it
	out, err := runIW(ctx, "phy", phy, "info")
	if err != nil {
		return nil, err
	}

	caps := &RadioCaps{Phy: phy, Bands: map[Band][]Channel{}}
	types, combos := parseIftypes(out)
	caps.SupportsAP = types[nl80211IftypeAP]
	caps.SupportsSTA = types[nl80211IftypeStation]
	caps.MaxAPInterfaces = combos.maxAP
	caps.APAndSTAConcurrent = combos.apAndSTA

	chans := parseChannels(out)
	if len(chans) == 0 {
		return nil, fmt.Errorf("no frequencies reported for %s", phy)
	}
	for _, ch := range chans {
		if b := bandForFreq(ch.FreqMHz); b != "" {
			caps.Bands[b] = append(caps.Bands[b], ch)
		}
	}
	return caps, nil
}

func runIW(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "iw", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("iw %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
