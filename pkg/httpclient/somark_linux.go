//go:build linux

package httpclient

import "golang.org/x/sys/unix"

// SO_MARK needs CAP_NET_ADMIN or CAP_NET_RAW
func setSOMark(fd int, mark uint32) error {
	return unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, int(mark))
}
