//go:build linux

package mtprotonode

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/nasnet-community/mtproto-remote-plugin/link"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// NewDialers: off router mode there are no policy-routing marks to set.
func NewDialers(routerMode bool) Dialers {
	if !routerMode {
		return plainDialers()
	}
	domestic := netmark.GroupMark(netmark.GroupDomestic)
	foreign := netmark.GroupMark(netmark.GroupForeign)
	return Dialers{
		Link: func(w *WAN) link.DialFunc {
			mark := domestic
			if w != nil && w.Pin != 0 {
				mark = netmark.WithPin(mark, w.Pin)
			}
			d := httpclient.MarkedDialer(mark, dialTimeout)
			if w != nil && w.IfName != "" {
				// a pin mark alone drifts to a sibling line when its own is down
				inner, ifname := d.Control, w.IfName
				d.Control = func(network, address string, c syscall.RawConn) error {
					if inner != nil {
						if err := inner(network, address, c); err != nil {
							return err
						}
					}
					var serr error
					if err := c.Control(func(fd uintptr) {
						serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname)
					}); err != nil {
						return err
					}
					return serr
				}
			}
			return func(ctx context.Context, n, a string) (net.Conn, error) { return d.DialContext(ctx, n, a) }
		},
		DC: func(ctx context.Context, n, a string) (net.Conn, error) {
			return httpclient.MarkedDialer(foreign, dialTimeout).DialContext(ctx, n, a)
		},
	}
}
