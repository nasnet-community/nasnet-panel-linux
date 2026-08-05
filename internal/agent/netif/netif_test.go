package netif

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSys builds a minimal /sys/class/net tree. Real sysfs uses symlinks for
// `device` and `subsystem`; we create directories and a marker file instead,
// which is what the reader is written against.
func fakeSys(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// enp1s0 — onboard PCI ethernet, carrier up, 1 Gbit
	write("class/net/enp1s0/type", "1\n")
	write("class/net/enp1s0/address", "aa:bb:cc:dd:ee:01\n")
	write("class/net/enp1s0/carrier", "1\n")
	write("class/net/enp1s0/operstate", "up\n")
	write("class/net/enp1s0/speed", "1000\n")
	write("class/net/enp1s0/mtu", "1500\n")
	write("class/net/enp1s0/ifindex", "2\n")
	write("class/net/enp1s0/device/subsystem_name", "pci\n")
	write("class/net/enp1s0/device/driver_name", "igc\n")

	// lo — must be enumerated but not assignable
	write("class/net/lo/type", "772\n")
	write("class/net/lo/address", "00:00:00:00:00:00\n")
	write("class/net/lo/carrier", "1\n")
	write("class/net/lo/operstate", "unknown\n")
	write("class/net/lo/mtu", "65536\n")
	write("class/net/lo/ifindex", "1\n")

	// enx-dongle — USB ethernet with a zero permanent MAC, so the key must
	// fall back to ID_PATH and the role becomes tied to the port.
	write("class/net/enxdongle/type", "1\n")
	write("class/net/enxdongle/address", "02:11:22:33:44:55\n")
	write("class/net/enxdongle/carrier", "0\n")
	write("class/net/enxdongle/operstate", "down\n")
	write("class/net/enxdongle/mtu", "1500\n")
	write("class/net/enxdongle/ifindex", "3\n")
	write("class/net/enxdongle/device/subsystem_name", "usb\n")
	write("class/net/enxdongle/device/driver_name", "ax88179_178a\n")
	write("class/net/enxdongle/device/usb_speed", "480\n")

	return root
}

func fakeUdev(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("data/n2", "E:ID_NET_NAME_ONBOARD=eno1\nE:ID_PATH=pci-0000:01:00.0\n")
	write("data/n3", "E:ID_PATH=pci-0000:00:14.0-usb-0:1:1.0\n")
	return root
}

func listFake(t *testing.T, perm PermMACFunc) []Interface {
	t.Helper()
	got, err := List(Opts{SysRoot: fakeSys(t), UdevRoot: fakeUdev(t), PermMAC: perm})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func byName(ifs []Interface, name string) *Interface {
	for i := range ifs {
		if ifs[i].IfName == name {
			return &ifs[i]
		}
	}
	return nil
}

func TestList_ReadsSysfsAndClassifies(t *testing.T) {
	perms := map[string]string{
		"enp1s0":    "aa:bb:cc:dd:ee:01",
		"enxdongle": "00:00:00:00:00:00", // cheap clone with a zeroed EEPROM
	}
	ifs := listFake(t, func(n string) (string, error) { return perms[n], nil })

	if len(ifs) != 3 {
		t.Fatalf("enumerated %d interfaces, want 3: %+v", len(ifs), ifs)
	}

	eth := byName(ifs, "enp1s0")
	if eth == nil {
		t.Fatal("enp1s0 missing")
	}
	if eth.Source != SourceEthOnboard {
		t.Errorf("enp1s0 source = %q, want %q", eth.Source, SourceEthOnboard)
	}
	if eth.KeyKind != KeyPermAddr || eth.Key != "aa:bb:cc:dd:ee:01" {
		t.Errorf("enp1s0 key = %s/%s, want permaddr/aa:bb:cc:dd:ee:01", eth.KeyKind, eth.Key)
	}
	if !eth.Carrier || eth.SpeedMbit != 1000 || eth.MTU != 1500 {
		t.Errorf("enp1s0 link facts wrong: %+v", eth)
	}
	if !eth.Assignable {
		t.Error("enp1s0 must be assignable")
	}

	lo := byName(ifs, "lo")
	if lo == nil || lo.Source != SourceLoopback || lo.Assignable {
		t.Errorf("lo wrong: %+v", lo)
	}
}

// A zero or duplicated permanent MAC is common on cheap AX88179/RTL8152
// clones. The key must fall back to udev ID_PATH, which ties the role to the
// USB port rather than the adapter.
func TestList_ZeroPermMACFallsBackToIDPath(t *testing.T) {
	perms := map[string]string{"enp1s0": "aa:bb:cc:dd:ee:01", "enxdongle": "00:00:00:00:00:00"}
	ifs := listFake(t, func(n string) (string, error) { return perms[n], nil })

	d := byName(ifs, "enxdongle")
	if d == nil {
		t.Fatal("enxdongle missing")
	}
	if d.KeyKind != KeyIDPath {
		t.Fatalf("KeyKind = %q, want %q", d.KeyKind, KeyIDPath)
	}
	if d.Key != "pci-0000:00:14.0-usb-0:1:1.0" {
		t.Errorf("Key = %q, want the ID_PATH", d.Key)
	}
	if d.USBSpeedMbit != 480 {
		t.Errorf("USBSpeedMbit = %d, want 480 (USB 2.0, ~280 Mbit/s ceiling)", d.USBSpeedMbit)
	}
}

// Two adapters reporting the same permanent MAC must both fall back, or they
// would collapse into one row and swap roles at random.
func TestList_DuplicatePermMACFallsBackToIDPath(t *testing.T) {
	ifs := listFake(t, func(string) (string, error) { return "aa:bb:cc:dd:ee:01", nil })
	for _, in := range ifs {
		if in.Source == SourceLoopback {
			continue
		}
		if in.KeyKind == KeyPermAddr {
			t.Errorf("%s kept a duplicated permaddr key", in.IfName)
		}
	}
}

// No ID_PATH and no usable permanent MAC leaves only the kernel name, which
// the UI must warn about.
func TestList_NoPermMACNoIDPathFallsBackToIfName(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "class/net/eth9")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, v := range map[string]string{
		"type": "1\n", "address": "de:ad:be:ef:00:09\n", "carrier": "0\n",
		"operstate": "down\n", "mtu": "1500\n", "ifindex": "9\n",
	} {
		if err := os.WriteFile(filepath.Join(p, f), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ifs, err := List(Opts{SysRoot: root, UdevRoot: t.TempDir(),
		PermMAC: func(string) (string, error) { return "", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if len(ifs) != 1 || ifs[0].KeyKind != KeyIfName || ifs[0].Key != "eth9" {
		t.Fatalf("got %+v, want one ifname-keyed row", ifs)
	}
}

func TestList_MissingSysRootIsAnError(t *testing.T) {
	if _, err := List(Opts{SysRoot: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("List accepted a missing sysfs root")
	}
}

func TestList_DefaultsRootsAndPermMAC(t *testing.T) {
	// Opts with no PermMAC must not panic; it degrades to the ifname key.
	o := Opts{SysRoot: fakeSys(t), UdevRoot: fakeUdev(t)}
	if _, err := List(o); err != nil {
		t.Fatalf("List with no PermMAC: %v", err)
	}
}
