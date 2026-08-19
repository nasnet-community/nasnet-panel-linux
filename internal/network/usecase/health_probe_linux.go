//go:build linux

package usecase

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Production Probe. The bound socket hits the oif rule, so it uses that uplink.
type kernelProbe struct{}

func NewKernelProbe() Probe { return &kernelProbe{} }

func (kernelProbe) Carrier(_ context.Context, ifName string) (bool, error) {
	b, err := os.ReadFile("/sys/class/net/" + ifName + "/carrier")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(b)) == "1", nil
}

func (kernelProbe) GatewayReachable(ctx context.Context, ifName, gateway string) (bool, error) {
	if gateway == "" {
		return false, fmt.Errorf("no gateway for %s", ifName)
	}
	d := &net.Dialer{
		Timeout: 2 * time.Second,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifName)
			}); err != nil {
				return err
			}
			return serr
		},
	}
	// UDP connect forces ARP with no listener on the far side.
	conn, err := d.DialContext(ctx, "udp4", net.JoinHostPort(gateway, "9"))
	if err != nil {
		return false, nil
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{0}); err != nil {
		return false, nil
	}
	return true, nil
}

// probeControl binds to the uplink and stamps the mark the kill switch expects.
func probeControl(ifName string, mark uint32) func(string, string, syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifName)
			if serr == nil && mark != 0 {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
			}
		}); err != nil {
			return err
		}
		return serr
	}
}

func (kernelProbe) ProbeTarget(ctx context.Context, ifName string, mark uint32, t ProbeTarget) ProbeResult {
	res := ProbeResult{Target: t}
	d := &net.Dialer{Timeout: 2 * time.Second, Control: probeControl(ifName, mark)}
	start := time.Now()

	switch t.Proto {
	case "dns":
		conn, err := d.DialContext(ctx, "udp4", t.Address)
		if err != nil {
			res.Err = err.Error()
			return res
		}
		defer conn.Close()
		id, q := dnsQuery("example.com")
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write(q); err != nil {
			res.Err = err.Error()
			return res
		}
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil || !dnsAnswers(id, buf[:n]) {
			res.Err = "no answer"
			return res
		}
	default: // tcp: a completed handshake is the whole proof
		conn, err := d.DialContext(ctx, "tcp4", t.Address)
		if err != nil {
			res.Err = err.Error()
			return res
		}
		conn.Close()
	}

	res.OK, res.RTT = true, time.Since(start)
	return res
}
