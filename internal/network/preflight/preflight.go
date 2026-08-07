// Package preflight gates router mode at boot.
package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/netif"
)

// Env is the observed environment, kept separate from the judgement so Check
// stays pure.
type Env struct {
	OSID           string // /etc/os-release ID
	OSVersionID    string // /etc/os-release VERSION_ID
	InContainer    bool
	HasNetAdmin    bool
	NetworkdActive bool
	NMMasked       bool
	// TakeoverDone: before it, netplan and NM legitimately still own the network.
	TakeoverDone   bool
	AssignableNICs int
}

// Result collects problems. Fatal stops router mode, Warn only shows in the UI.
type Result struct {
	Fatal []string
	Warn  []string
}

func (r Result) OK() bool { return len(r.Fatal) == 0 }

// Check judges an Env. Touches nothing.
func Check(e Env) Result {
	var r Result

	if e.OSID != "ubuntu" || e.OSVersionID != "24.04" {
		r.Fatal = append(r.Fatal, fmt.Sprintf(
			"router mode requires Ubuntu 24.04 (found %q %q)", e.OSID, e.OSVersionID))
	}
	if e.InContainer {
		r.Fatal = append(r.Fatal,
			"router mode is unsupported in a container: dual-WAN needs the host network namespace")
	}
	if !e.HasNetAdmin {
		r.Fatal = append(r.Fatal,
			"missing CAP_NET_ADMIN; reinstall via nasnet-tool so the unit gets ambient capabilities")
	}
	if !e.NetworkdActive {
		r.Fatal = append(r.Fatal, "systemd-networkd is not running")
	}

	if !e.NMMasked {
		if e.TakeoverDone {
			r.Fatal = append(r.Fatal,
				"NetworkManager is unmasked after the nasnet takeover; two daemons cannot own one link")
		} else {
			r.Warn = append(r.Warn,
				"network not managed by nasnet yet — assign roles to finish setup")
		}
	}
	if e.AssignableNICs < 2 {
		r.Warn = append(r.Warn, fmt.Sprintf(
			"%d assignable interface(s) found; dual-WAN needs two — single-uplink mode has no failover and no split routing",
			e.AssignableNICs))
	}

	return r
}

// Probe reads the real environment. takeoverDone comes from the database, not
// the filesystem.
func Probe(takeoverDone bool) (Env, error) {
	e := Env{TakeoverDone: takeoverDone}

	id, ver := osRelease()
	e.OSID, e.OSVersionID = id, ver
	e.InContainer = inContainer()
	e.HasNetAdmin = hasNetAdmin()
	e.NetworkdActive = unitActive("systemd-networkd")
	e.NMMasked = unitMasked("NetworkManager")

	// Platform check
	if e.OSID != "ubuntu" || e.OSVersionID != "24.04" {
		return e, nil
	}

	ifs, err := netif.List(netif.Opts{})
	if err != nil {
		return e, fmt.Errorf("enumerate interfaces: %w", err)
	}
	for _, in := range ifs {
		if in.Assignable && uplinkCandidate(in.Source) {
			e.AssignableNICs++
		}
	}
	return e, nil
}

// uplinkCandidate filters to interfaces that could carry a WAN. netif marks
// bridges assignable for the stage 2 LAN role, so counting those would report
// 7 uplinks on a one-NIC box running docker.
func uplinkCandidate(s netif.Source) bool {
	switch s {
	case netif.SourceEthOnboard, netif.SourceEthPCI, netif.SourceEthUSB, netif.SourceEthPlatform,
		netif.SourceWifiPCI, netif.SourceWifiUSB,
		netif.SourceTetherAndroid, netif.SourceTetherIPhone,
		netif.SourceWWANUSB, netif.SourceWWANPCIe:
		return true
	}
	return false
}

func osRelease() (id, versionID string) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "ID":
			id = v
		case "VERSION_ID":
			versionID = v
		}
	}
	return id, versionID
}

// inContainer compares our netns against PID 1's. Own netns = no dual-WAN.
func inContainer() bool {
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	if err1 != nil || err2 != nil {
		return false
	}
	return self != init
}

// hasNetAdmin looks for CAP_NET_ADMIN (bit 12) in CapEff.
func hasNetAdmin() bool {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		hex, ok := strings.CutPrefix(line, "CapEff:")
		if !ok {
			continue
		}
		var v uint64
		if _, err := fmt.Sscanf(strings.TrimSpace(hex), "%x", &v); err != nil {
			return false
		}
		const capNetAdmin = 12
		return v&(1<<capNetAdmin) != 0
	}
	return false
}

func unitActive(unit string) bool {
	out, _ := exec.Command("systemctl", "is-active", unit).Output()
	return strings.TrimSpace(string(out)) == "active"
}

func unitMasked(unit string) bool {
	out, _ := exec.Command("systemctl", "is-enabled", unit).Output()
	return unitDisarmed(string(out))
}

// unitDisarmed reads `systemctl is-enabled` output. not-found (absent unit,
// exit 4) and masked-runtime both mean the daemon won't grab a link — treating
// them as unmasked would make a box without NetworkManager fail preflight.
func unitDisarmed(state string) bool {
	switch strings.TrimSpace(state) {
	case "masked", "masked-runtime", "not-found":
		return true
	}
	return false
}
