//go:build linux

package system

import (
	"context"
	"os"
	"testing"
)

// The parse is against `iw phy info`, a human format. This runs it on whatever
// radios the box actually has and prints what it made of them, so a format
// change shows up as a diff a person can read.
//
//	sudo NASNET_REAL_RADIO=1 ./system.test -run TestRealRadioProbe -v
//
// mac80211_hwsim radios are real mac80211 devices, so hwsim counts.
func TestRealRadioProbe(t *testing.T) {
	if os.Getenv("NASNET_REAL_RADIO") != "1" {
		t.Skip("set NASNET_REAL_RADIO=1 on a box with a radio")
	}
	// TestMain empties PATH so the rest of the suite cannot shell out
	t.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")

	p, err := NewRadioProber()
	if err != nil {
		t.Fatal(err)
	}
	caps, err := p.Radios(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) == 0 {
		t.Fatal("no radios found; load mac80211_hwsim or plug one in")
	}

	code, err := ReadRegDomain(context.Background())
	if err != nil {
		t.Errorf("ReadRegDomain: %v", err)
	}
	t.Logf("regdomain %q unset=%v", code, RegDomainIsUnset(code))

	for _, c := range caps {
		t.Logf("%s ap=%v sta=%v maxap=%d concurrent=%v",
			c.Phy, c.SupportsAP, c.SupportsSTA, c.MaxAPInterfaces, c.APAndSTAConcurrent)
		for _, b := range []Band{Band2G, Band5G, Band6G} {
			all := c.Bands[b]
			if len(all) == 0 {
				continue
			}
			var beaconable []int
			for _, ch := range c.BeaconableChannels(b) {
				beaconable = append(beaconable, ch.Number)
			}
			t.Logf("  %s: %d channels, beaconable %v", b, len(all), beaconable)
			for _, ch := range all {
				if !ch.Beaconable() {
					t.Logf("    ch %d (%d MHz) blocked: noIR=%v radar=%v disabled=%v",
						ch.Number, ch.FreqMHz, ch.NoIR, ch.Radar, ch.DisabledByRegdomain)
				}
			}
		}
		// Every channel must classify into a band and carry a number, or the
		// picker silently drops it
		for b, chans := range c.Bands {
			for _, ch := range chans {
				if ch.Number == 0 {
					t.Errorf("%s %s: %d MHz got channel number 0", c.Phy, b, ch.FreqMHz)
				}
			}
		}
		if !c.SupportsAP && !c.SupportsSTA {
			t.Errorf("%s supports neither AP nor station; the parse missed the modes block", c.Phy)
		}
	}
}
