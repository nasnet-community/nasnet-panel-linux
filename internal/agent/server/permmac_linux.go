//go:build linux

package server

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// permanentMAC reads IFLA_PERM_ADDRESS. Not sysfs: device/net/<if>/address is
// the same inode as class/net/<if>/address, so it gives the live MAC and a
// `ip link set address` override would move a role to another NIC.
//
// Empty is fine — veth and blank-EEPROM USB adapters have no permanent addr,
// and netif falls back to udev ID_PATH.
func permanentMAC(ifName string) (string, error) {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return "", fmt.Errorf("netlink link %s: %w", ifName, err)
	}
	perm := link.Attrs().PermHWAddr
	if len(perm) == 0 {
		return "", nil
	}
	return perm.String(), nil
}
