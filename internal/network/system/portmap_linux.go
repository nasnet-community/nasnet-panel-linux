//go:build linux

package system

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// portmapControl binds to the uplink and stamps the mark the kill switch
// exempts. Same shape as the health probe's control.
func portmapControl(ifName string, mark uint32) dialControl {
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			if ifName != "" {
				serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifName)
			}
			if serr == nil && mark != 0 {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
			}
		}); err != nil {
			return err
		}
		return serr
	}
}

func NewPortMapper() PortMapper { return newPortMapClient(portmapControl) }
