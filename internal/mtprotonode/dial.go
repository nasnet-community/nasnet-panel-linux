package mtprotonode

import (
	"context"
	"net"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/link"
)

// WAN is one domestic uplink a link connection can be pinned to.
type WAN struct {
	Slot   string
	IfName string
	Pin    uint32 // the netmark pin index for this uplink
}

// Dialers are the two ways an MTProto node reaches the network.
type Dialers struct {
	// Link dials a node's agent over a domestic line, pinned when given a WAN; never the foreign exit.
	Link func(w *WAN) link.DialFunc
	// DC dials a Telegram datacentre out of the foreign exit; fail-closed too.
	DC link.DialFunc
}

const dialTimeout = 15 * time.Second

// plainDialers: off router mode the default route is both paths.
func plainDialers() Dialers {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	f := func(ctx context.Context, n, a string) (net.Conn, error) { return d.DialContext(ctx, n, a) }
	return Dialers{Link: func(*WAN) link.DialFunc { return f }, DC: f}
}
