package httpclient

import (
	"context"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"golang.org/x/net/proxy"
)

// EgressGroup names which uplink group a feature uses
type EgressGroup uint32

const (
	EgressForeign    EgressGroup = 1
	EgressDomestic   EgressGroup = 2
	EgressAdvertised EgressGroup = 3 // Whichever uplink clients reach (e.g. ACME HTTP-01 needs it)
)

// Mark returns the fwmark
func (g EgressGroup) Mark() uint32 {
	switch g {
	case EgressForeign:
		return netmark.GroupMark(netmark.GroupForeign)
	case EgressDomestic:
		return netmark.GroupMark(netmark.GroupDomestic)
	}
	return 0
}

// DefaultGroupFor is the shipped classification.
func DefaultGroupFor(feat Feature) EgressGroup {
	switch feat {
	case FeatureACME:
		return EgressAdvertised
	default:
		return EgressForeign
	}
}

// SetRouterMode enables SO_MARK and records which group is advertised
func (f *Factory) SetRouterMode(enabled bool, advertisedGroup EgressGroup) {
	old := f.s.Load()
	f.s.Store(&state{
		proxyURL:   old.proxyURL,
		transport:  old.transport,
		enabled:    old.enabled,
		routerMode: enabled,
		advertised: advertisedGroup,
	})
}

// markFor resolves a group to a mark, 0 when router mode is off
func (f *Factory) markFor(g EgressGroup) uint32 {
	st := f.s.Load()
	if !st.routerMode {
		return 0
	}
	if g == EgressAdvertised {
		return st.advertised.Mark()
	}
	return g.Mark()
}

// markingDialer stamps SO_MARK. nil when off, so Docker keeps DefaultTransport.
func (f *Factory) markingDialer(g EgressGroup) func(context.Context, string, string) (net.Conn, error) {
	d := f.markingNetDialer(g)
	if d == nil {
		return nil
	}
	return d.DialContext
}

func (f *Factory) markingNetDialer(g EgressGroup) *net.Dialer {
	mark := f.markFor(g)
	if mark == 0 {
		return nil
	}
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = setSOMark(int(fd), mark)
			}); err != nil {
				return err
			}
			return serr
		},
	}
}

// proxyTransport rebuilds SOCKS5 over a marking dialer. proxy.Direct is
// unmarked and would find no default route.
func (f *Factory) proxyTransport(st *state, group EgressGroup) http.RoundTripper {
	base := f.markingNetDialer(group)
	if base == nil || st.proxyURL == nil {
		return st.transport
	}
	return buildSOCKS5Transport(st.proxyURL, base)
}

// *net.Dialer must satisfy proxy.Dialer.
var _ proxy.Dialer = (*net.Dialer)(nil)
