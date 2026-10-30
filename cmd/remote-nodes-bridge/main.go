// SPDX-License-Identifier: AGPL-3.0-or-later

// Command remote-nodes-bridge runs on the router beside the hub. Each node
// is the portal of a VLESS + REALITY reverse tunnel; the bridge dials it from
// the Domestic link and carries the subscriptions' traffic out through the
// router's VPN.
//
// The hub starts it as another user and sends the spec (internal/remotenodes/
// bridgespec) on stdin. It runs until stdin closes or it gets SIGTERM, and
// serves its state as JSON on the loopback status address from the spec.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	// What the config needs, and no more: the outbound that dials, REALITY,
	// the exit, and the apps around them. Importing all of main/distro/all
	// would add the other protocols, the transports, the gRPC services and the
	// config loaders.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/dns"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/policy"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/app/router"
	_ "github.com/xtls/xray-core/app/stats"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/udp"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
)

// stopTimeout is how long the bridge gives Xray to close before it gives up.
const stopTimeout = 8 * time.Second

func main() {
	// by hand from a terminal nothing is coming: say so rather than wait
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "remote-nodes-bridge reads its spec from stdin; the hub starts it")
		os.Exit(2)
	}
	if err := noNewPrivs(); err != nil {
		fmt.Fprintln(os.Stderr, "remote-nodes-bridge:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Stdin, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "remote-nodes-bridge:", err)
		os.Exit(1)
	}
}

// run reads the spec from in, runs the bridge and returns once ctx is done
// or in is closed. Anything else on in after the spec is ignored.
func run(ctx context.Context, in io.Reader, logw io.Writer) error {
	spec, err := bridgespec.Read(in)
	if err != nil {
		return err
	}
	// Bind first: a taken port then fails before anything is built or dials.
	l, err := net.Listen("tcp", spec.StatusAddr)
	if err != nil {
		return fmt.Errorf("status page: %w", err)
	}
	b, err := newBridge(spec)
	if err != nil {
		l.Close()
		return err
	}
	if err := b.start(); err != nil {
		l.Close()
		b.close()
		return err
	}
	srv := b.serve(l)
	fmt.Fprintf(logw, "remote-nodes-bridge: %d tunnel(s), status on %s\n", len(spec.Nodes), l.Addr())

	// The hub holds stdin open for as long as it wants the bridge to run,
	// and closing it is how it says stop when a signal can't reach us.
	hubGone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, in)
		close(hubGone)
	}()
	select {
	case <-ctx.Done():
	case <-hubGone:
	}

	stopped := make(chan error, 1)
	go func() { stopped <- b.close() }()
	shutdown, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	srv.Shutdown(shutdown)
	select {
	case err := <-stopped:
		return err
	case <-shutdown.Done():
		return errors.New("xray did not stop in time")
	}
}
