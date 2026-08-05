package netif

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests model the shapes real sysfs actually uses, which the plain-file
// fake tree in netif_test.go does not: `device`, `subsystem` and `driver` are
// symlinks, there is no `subsystem_name` or `driver_name` file anywhere, and a
// physical NIC's `uevent` carries no DEVTYPE line.
//
// Verified against Ubuntu 24.04 / kernel 6.8: `/sys/class/net/eth0/device`
// lists only {device, driver, features, modalias, net, power, status,
// subsystem, uevent, vendor} — no *_name files — and `device/subsystem`
// resolves to `../../../../../bus/virtio` on a virtio NIC.

type sysTree struct {
	t    *testing.T
	root string
}

func newSysTree(t *testing.T) *sysTree {
	t.Helper()
	return &sysTree{t: t, root: t.TempDir()}
}

func (s *sysTree) dir(rel string) string {
	s.t.Helper()
	p := filepath.Join(s.root, rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		s.t.Fatal(err)
	}
	return p
}

func (s *sysTree) file(rel, content string) {
	s.t.Helper()
	p := filepath.Join(s.root, rel)
	s.dir(filepath.Dir(rel))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

// link creates <rel> pointing at <target>, both relative to the tree root.
func (s *sysTree) link(rel, target string) {
	s.t.Helper()
	p := filepath.Join(s.root, rel)
	s.dir(filepath.Dir(rel))
	if err := os.Symlink(filepath.Join(s.root, target), p); err != nil {
		s.t.Fatal(err)
	}
}

// nicFiles writes the per-interface attribute files every NIC has.
func (s *sysTree) nicFiles(netDir, mac, ifindex string) {
	s.file(netDir+"/type", "1\n")
	s.file(netDir+"/address", mac+"\n")
	s.file(netDir+"/carrier", "1\n")
	s.file(netDir+"/operstate", "up\n")
	s.file(netDir+"/mtu", "1500\n")
	s.file(netDir+"/ifindex", ifindex+"\n")
}

func (s *sysTree) list() []Interface {
	s.t.Helper()
	ifs, err := List(Opts{SysRoot: s.root, UdevRoot: s.t.TempDir()})
	if err != nil {
		s.t.Fatalf("List: %v", err)
	}
	return ifs
}

// A virtio NIC's own subsystem is `virtio`, which matches no classifier case.
// Its bus has to be found by climbing to the parent PCI device, or every NIC
// on every VM and cloud instance is SourceUnknown and never assignable.
func TestList_VirtioNICResolvesToPCIViaParent(t *testing.T) {
	s := newSysTree(t)
	dev := "devices/pci0000:00/0000:00:03.0"
	virt := dev + "/virtio0"

	s.dir("bus/pci")
	s.dir("bus/virtio/drivers/virtio_net")
	s.link(dev+"/subsystem", "bus/pci")
	s.link(virt+"/subsystem", "bus/virtio")
	s.link(virt+"/driver", "bus/virtio/drivers/virtio_net")

	s.nicFiles(virt+"/net/eth0", "92:00:06:3b:c1:67", "2")
	s.file(virt+"/net/eth0/uevent", "INTERFACE=eth0\nIFINDEX=2\n") // no DEVTYPE
	s.link(virt+"/net/eth0/device", virt)
	s.link("class/net/eth0", virt+"/net/eth0")

	eth := byName(s.list(), "eth0")
	if eth == nil {
		t.Fatal("eth0 missing")
	}
	if eth.Source != SourceEthPCI {
		t.Errorf("source = %q, want %q (virtio parent is a PCI device)", eth.Source, SourceEthPCI)
	}
	if eth.Driver != "virtio_net" {
		t.Errorf("driver = %q, want virtio_net from the device/driver symlink", eth.Driver)
	}
	if !eth.Assignable {
		t.Error("a virtio NIC must be assignable; otherwise no VM can hold a role")
	}
}

// A discrete PCI NIC: subsystem symlink resolves straight to bus/pci.
func TestList_PCINICReadsSubsystemAndDriverSymlinks(t *testing.T) {
	s := newSysTree(t)
	dev := "devices/pci0000:00/0000:02:00.0"

	s.dir("bus/pci/drivers/r8169")
	s.link(dev+"/subsystem", "bus/pci")
	s.link(dev+"/driver", "bus/pci/drivers/r8169")

	s.nicFiles(dev+"/net/enp2s0", "aa:bb:cc:dd:ee:02", "3")
	s.link(dev+"/net/enp2s0/device", dev)
	s.link("class/net/enp2s0", dev+"/net/enp2s0")

	in := byName(s.list(), "enp2s0")
	if in == nil {
		t.Fatal("enp2s0 missing")
	}
	if in.Source != SourceEthPCI || in.Driver != "r8169" {
		t.Errorf("got source=%q driver=%q, want eth_pci/r8169", in.Source, in.Driver)
	}
}

// USB bus speed lives on the enclosing USB device, not on the interface node,
// so the reader must climb to find it.
func TestList_USBSpeedComesFromTheEnclosingUSBDevice(t *testing.T) {
	s := newSysTree(t)
	usbDev := "devices/pci0000:00/0000:00:14.0/usb1/1-1"
	usbIf := usbDev + "/1-1:1.0"

	s.dir("bus/usb/drivers/ax88179_178a")
	s.link(usbDev+"/subsystem", "bus/usb")
	s.file(usbDev+"/speed", "480\n")
	s.link(usbIf+"/subsystem", "bus/usb")
	s.link(usbIf+"/driver", "bus/usb/drivers/ax88179_178a")

	s.nicFiles(usbIf+"/net/enx0", "02:11:22:33:44:55", "4")
	s.link(usbIf+"/net/enx0/device", usbIf)
	s.link("class/net/enx0", usbIf+"/net/enx0")

	in := byName(s.list(), "enx0")
	if in == nil {
		t.Fatal("enx0 missing")
	}
	if in.Source != SourceEthUSB {
		t.Errorf("source = %q, want %q", in.Source, SourceEthUSB)
	}
	if in.USBSpeedMbit != 480 {
		t.Errorf("USBSpeedMbit = %d, want 480 from the parent USB device", in.USBSpeedMbit)
	}
}

// Bridges and VLANs announce themselves through uevent DEVTYPE; a physical NIC
// has no DEVTYPE line and must not be mistaken for a virtual device.
func TestList_UeventDevTypeClassifiesVirtualLinks(t *testing.T) {
	s := newSysTree(t)

	s.nicFiles("class/net/br0", "02:42:aa:bb:cc:dd", "5")
	s.file("class/net/br0/uevent", "DEVTYPE=bridge\nINTERFACE=br0\nIFINDEX=5\n")

	s.nicFiles("class/net/eth0.7", "aa:bb:cc:dd:ee:07", "6")
	s.file("class/net/eth0.7/uevent", "DEVTYPE=vlan\nINTERFACE=eth0.7\nIFINDEX=6\n")

	// A veth: no DEVTYPE and no device node at all.
	s.nicFiles("class/net/veth9", "5e:11:22:33:44:99", "7")
	s.file("class/net/veth9/uevent", "INTERFACE=veth9\nIFINDEX=7\n")

	ifs := s.list()
	for _, c := range []struct {
		name       string
		want       Source
		assignable bool
	}{
		{"br0", SourceVirtBridge, true},
		{"eth0.7", SourceVirtVLAN, true},
		{"veth9", SourceVirtOther, false},
	} {
		in := byName(ifs, c.name)
		if in == nil {
			t.Errorf("%s missing", c.name)
			continue
		}
		if in.Source != c.want {
			t.Errorf("%s source = %q, want %q", c.name, in.Source, c.want)
		}
		if in.Assignable != c.assignable {
			t.Errorf("%s assignable = %v, want %v", c.name, in.Assignable, c.assignable)
		}
	}
}

// The regression this whole file exists for: with only the plan's plain-file
// reader, a NIC whose facts are all behind symlinks yields Subsystem="" and
// Driver="", classifies as SourceUnknown, and is silently unassignable.
func TestList_SymlinkOnlyNICIsNotUnknown(t *testing.T) {
	s := newSysTree(t)
	dev := "devices/platform/ff0e0000.ethernet"

	s.dir("bus/platform/drivers/macb")
	s.link(dev+"/subsystem", "bus/platform")
	s.link(dev+"/driver", "bus/platform/drivers/macb")

	s.nicFiles(dev+"/net/eth0", "aa:bb:cc:dd:ee:10", "2")
	s.link(dev+"/net/eth0/device", dev)
	s.link("class/net/eth0", dev+"/net/eth0")

	in := byName(s.list(), "eth0")
	if in == nil {
		t.Fatal("eth0 missing")
	}
	if in.Source == SourceUnknown {
		t.Fatal("symlink-only NIC classified as unknown — it would never be assignable")
	}
	if in.Source != SourceEthPlatform {
		t.Errorf("source = %q, want %q", in.Source, SourceEthPlatform)
	}
}
