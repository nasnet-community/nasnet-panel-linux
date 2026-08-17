// Package netif enumerates network interfaces and classifies how each is attached.
package netif

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// KeyKind is Interface.Key's identity source. Non-KeyPermAddr = port-tied, not device.
type KeyKind string

const (
	KeyPermAddr KeyKind = "permaddr"
	KeyIDPath   KeyKind = "idpath"
	KeyIfName   KeyKind = "ifname"
)

// Interface is one enumerated NIC. Addresses come later from netlink.
type Interface struct {
	IfName       string
	PermMAC      string
	MAC          string
	IDPath       string
	KeyKind      KeyKind
	Key          string
	Source       Source
	Confidence   int
	ARPType      uint16
	Driver       string
	Carrier      bool
	OperState    string
	SpeedMbit    int
	MTU          int
	Phy          string
	USBSpeedMbit int
	Assignable   bool
}

// PermMACFunc resolves an interface's permanent hardware address.
type PermMACFunc func(ifName string) (string, error)

// Opts locates the trees to read. Empty roots default to the real ones.
type Opts struct {
	SysRoot  string // default "/sys"
	UdevRoot string // default "/run/udev"
	PermMAC  PermMACFunc
}

func (o Opts) sysRoot() string {
	if o.SysRoot == "" {
		return "/sys"
	}
	return o.SysRoot
}

func (o Opts) udevRoot() string {
	if o.UdevRoot == "" {
		return "/run/udev"
	}
	return o.UdevRoot
}

// List enumerates every interface under <SysRoot>/class/net, sorted by name.
func List(o Opts) ([]Interface, error) {
	base := filepath.Join(o.sysRoot(), "class", "net")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", base, err)
	}

	// collect facts first to catch dupe perm MACs before key choice
	type raw struct {
		in    Interface
		probe Probe
	}
	raws := make([]raw, 0, len(entries))
	permCount := map[string]int{}

	for _, e := range entries {
		name := e.Name()
		dir := filepath.Join(base, name)

		in := Interface{
			IfName:    name,
			MAC:       readString(dir, "address"),
			OperState: readString(dir, "operstate"),
			ARPType:   uint16(readInt(dir, "type")),
			MTU:       readInt(dir, "mtu"),
			SpeedMbit: readInt(dir, "speed"),
			Carrier:   readInt(dir, "carrier") == 1,
			Driver:    driverOf(dir),
		}
		if _, err := os.Stat(filepath.Join(dir, "phy80211")); err == nil {
			in.Phy = readString(dir, "phy80211", "name")
			if in.Phy == "" {
				in.Phy = "phy?"
			}
		}
		if o.PermMAC != nil {
			if pm, err := o.PermMAC(name); err == nil {
				in.PermMAC = pm
			}
		}
		if u := usbSpeedOf(dir); u > 0 {
			in.USBSpeedMbit = u
		}

		ifindex := readInt(dir, "ifindex")
		props := readUdevProps(o.udevRoot(), ifindex)
		in.IDPath = props["ID_PATH"]

		probe := Probe{
			IfName:              name,
			ARPType:             in.ARPType,
			Subsystem:           subsystemOf(dir),
			Driver:              in.Driver,
			HasPHY80211:         in.Phy != "",
			OnboardName:         props["ID_NET_NAME_ONBOARD"],
			USBSiblingFunctions: splitList(props["ID_USB_INTERFACES"]),
		}
		if kind := virtualKind(dir); kind != "" {
			probe.HasLinkInfoKind, probe.LinkKind = true, kind
		}

		if usable(in.PermMAC) {
			permCount[strings.ToLower(in.PermMAC)]++
		}
		raws = append(raws, raw{in: in, probe: probe})
	}

	out := make([]Interface, 0, len(raws))
	for _, r := range raws {
		in := r.in
		in.Source, in.Confidence = Classify(r.probe)
		in.Assignable = Assignable(in.Source)

		switch {
		case usable(in.PermMAC) && permCount[strings.ToLower(in.PermMAC)] == 1:
			in.KeyKind, in.Key = KeyPermAddr, strings.ToLower(in.PermMAC)
		case in.IDPath != "":
			in.KeyKind, in.Key = KeyIDPath, in.IDPath
		default:
			in.KeyKind, in.Key = KeyIfName, in.IfName
		}
		out = append(out, in)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].IfName < out[j].IfName })
	return out, nil
}

// usable rejects empty/all-zero perm MAC (cheap USB-eth clones report these).
func usable(mac string) bool {
	m := strings.ToLower(strings.TrimSpace(mac))
	return m != "" && m != "00:00:00:00:00:00"
}

// knownBuses are subsystem names Classify switches on; others need a parent walk.
var knownBuses = map[string]bool{"pci": true, "usb": true, "platform": true}

// subsystemOf resolves the bus an interface's parent device sits on, walking
// up (e.g. virtio -> pci) so VM/cloud NICs still classify.
func subsystemOf(ifDir string) string {
	if s := readString(ifDir, "device", "subsystem_name"); s != "" {
		return s
	}
	devDir := filepath.Join(ifDir, "device")
	if s := symlinkBase(filepath.Join(devDir, "subsystem")); knownBuses[s] {
		return s
	}
	// climb toward /sys/devices
	real, err := filepath.EvalSymlinks(devDir)
	if err != nil {
		return symlinkBase(filepath.Join(devDir, "subsystem"))
	}
	for p := filepath.Dir(real); ; p = filepath.Dir(p) {
		base := filepath.Base(p)
		if p == "/" || p == "." || base == "devices" || base == "sys" {
			break
		}
		if s := symlinkBase(filepath.Join(p, "subsystem")); knownBuses[s] {
			return s
		}
	}
	// unrecognized; return immediate subsystem for logging
	return symlinkBase(filepath.Join(devDir, "subsystem"))
}

// driverOf reads the bound driver name.
func driverOf(ifDir string) string {
	if d := readString(ifDir, "device", "driver_name"); d != "" {
		return d
	}
	return symlinkBase(filepath.Join(ifDir, "device", "driver"))
}

// usbSpeedOf returns USB bus speed in Mbit/s, 0 if not USB.
func usbSpeedOf(ifDir string) int {
	if v := readInt(ifDir, "device", "usb_speed"); v > 0 {
		return v
	}
	real, err := filepath.EvalSymlinks(filepath.Join(ifDir, "device"))
	if err != nil {
		return 0
	}
	for p := real; ; p = filepath.Dir(p) {
		base := filepath.Base(p)
		if p == "/" || p == "." || base == "devices" || base == "sys" {
			return 0
		}
		if symlinkBase(filepath.Join(p, "subsystem")) != "usb" {
			continue
		}
		if v := readInt(p, "speed"); v > 0 {
			return v
		}
	}
}

// symlinkBase returns the symlink target's last path element, "" if none.
func symlinkBase(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// virtualKind reports the netlink link kind for a device-less interface.
func virtualKind(dir string) string {
	for _, k := range []struct{ probe, kind string }{
		{"bridge", "bridge"},
		{"bonding", "bond"},
	} {
		if _, err := os.Stat(filepath.Join(dir, k.probe)); err == nil {
			return k.kind
		}
	}
	// DEVTYPE names real hardware too: a modem is "wwan", a radio "wlan".
	if t := ueventDevType(dir); t != "" && t != "wwan" && t != "wlan" {
		return t
	}
	if _, err := os.Stat(filepath.Join(dir, "device")); os.IsNotExist(err) {
		if filepath.Base(dir) == "lo" {
			return "" // loopback classified by name + ARP type
		}
		return "other"
	}
	return ""
}

// ueventDevType extracts DEVTYPE from the interface's uevent file.
func ueventDevType(dir string) string {
	for _, line := range strings.Split(readString(dir, "uevent"), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "DEVTYPE="); ok {
			return v
		}
	}
	return ""
}

// readUdevProps parses /run/udev/data/n<ifindex>'s "E:KEY=value" lines.
func readUdevProps(udevRoot string, ifindex int) map[string]string {
	props := map[string]string{}
	if ifindex <= 0 {
		return props
	}
	b, err := os.ReadFile(filepath.Join(udevRoot, "data", "n"+strconv.Itoa(ifindex)))
	if err != nil {
		return props
	}
	for _, line := range strings.Split(string(b), "\n") {
		kv, ok := strings.CutPrefix(strings.TrimSpace(line), "E:")
		if !ok {
			continue
		}
		if k, v, found := strings.Cut(kv, "="); found {
			props[k] = v
		}
	}
	return props
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func readString(parts ...string) string {
	b, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readInt(parts ...string) int {
	v, err := strconv.Atoi(readString(parts...))
	if err != nil {
		return 0
	}
	return v
}
