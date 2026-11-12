// SPDX-License-Identifier: AGPL-3.0-or-later

// The shape of run (a context from the signals, a listener, a graceful
// shutdown) follows the Telegram MTProto plugin
// (github.com/nasnet-community/mtproto-remote-plugin), which is MIT licensed:
//
//	MIT License
//
//	Copyright (c) 2026 NASNET Community
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE.

// Command remote-nodes is the hub of the Remote Nodes plugin: a container on a
// MikroTik router that manages Linux nodes running nasnet-agent.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/manager"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netbind"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netroles"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/ui"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/realitydest"
)

// version and commit are set at build time with
// -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "unknown"
)

// settings come from the environment.
type settings struct {
	listen   string // LISTEN_ADDR, default :8080
	dataDir  string // DATA_DIR, default /data
	basePath string // BASE_PATH: where the router's proxy mounts the plugin

	licensesDir string // LICENSES_DIR, default /licenses: the files behind the page's Notices link

	// The plugin's three source addresses, as the manifest gives them to the
	// veth. They aren't checked against the interfaces here until run starts.
	addrs netroles.Set // ADDR_FOREIGN, ADDR_EXIT, ADDR_NODES

	bridgeBin string   // BRIDGE_BIN: the bridge program, default /remote-nodes-bridge
	bridgeUID uint32   // BRIDGE_UID, BRIDGE_GID: who it runs as when the hub is root on Linux
	bridgeGID uint32   //
	bridgeDNS []string // BRIDGE_DNS: the resolvers its exit asks, through the VPN
	factsPath string   // FACTS_PATH: the router's report, default <DATA_DIR>/router/state

	// release is where an install fetches the agent from: RELEASE_BASE_URL, or
	// the project's latest release.
	release artifacts.Source
}

const (
	// defaultBridgeBin is where the image puts the bridge program, beside the hub.
	defaultBridgeBin = "/remote-nodes-bridge"
	// defaultBridgeID is nobody and nogroup, which own nothing, so the bridge
	// can read nothing of the hub's.
	defaultBridgeID = 65534
)

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

func fromEnv(getenv func(string) string) (settings, error) {
	s := settings{
		listen:    orDefault(getenv("LISTEN_ADDR"), ":8080"),
		dataDir:   orDefault(getenv("DATA_DIR"), "/data"),
		bridgeBin: orDefault(getenv("BRIDGE_BIN"), defaultBridgeBin),
	}
	if _, _, err := net.SplitHostPort(s.listen); err != nil {
		return settings{}, fmt.Errorf("LISTEN_ADDR %q: %w", s.listen, err)
	}
	var err error
	if s.basePath, err = ui.BasePath(getenv("BASE_PATH")); err != nil {
		return settings{}, err
	}
	if s.addrs, err = netroles.FromEnv(getenv); err != nil {
		return settings{}, err
	}
	if s.bridgeUID, err = idFromEnv(getenv, "BRIDGE_UID"); err != nil {
		return settings{}, err
	}
	if s.bridgeGID, err = idFromEnv(getenv, "BRIDGE_GID"); err != nil {
		return settings{}, err
	}
	if s.bridgeDNS, err = dnsFromEnv(getenv("BRIDGE_DNS"), s.addrs.Exit.IP); err != nil {
		return settings{}, err
	}
	s.factsPath = orDefault(getenv("FACTS_PATH"), routerfacts.Path(s.dataDir))
	s.licensesDir = orDefault(getenv("LICENSES_DIR"), "/licenses")
	if s.release, err = artifacts.SourceFromEnv(getenv); err != nil {
		return settings{}, err
	}
	return s, nil
}

// idFromEnv reads a user or group id, 65534 when it is not set.
func idFromEnv(getenv func(string) string, name string) (uint32, error) {
	v := strings.TrimSpace(getenv(name))
	if v == "" {
		return defaultBridgeID, nil
	}
	id, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a user or group id", name, v)
	}
	return uint32(id), nil
}

// dnsFromEnv reads BRIDGE_DNS, a list of resolvers separated by commas, each an
// address with an optional port, or the defaults when it is empty. The bridge's exit sends the queries and
// refuses the ranges it blocks (the router's own LAN among them), and the
// resolvers have to be of the exit's family, so a list that can't work is
// refused here, not when the bridge first tries to start.
func dnsFromEnv(v string, exit netip.Addr) ([]string, error) {
	var out []string
	for _, f := range strings.Split(v, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		// the defaults are IPv4, so they are held to the same rules: an IPv6
		// exit has to be given resolvers of its own
		out = append(out, manager.DefaultDNS...)
	}
	if len(out) > bridgespec.MaxDNSServers {
		return nil, fmt.Errorf("BRIDGE_DNS has %d resolvers, and at most %d are used", len(out), bridgespec.MaxDNSServers)
	}
	blocked := bridgespec.DefaultBlocked()
	for _, f := range out {
		ap, err := bridgespec.ParseServer(f)
		if err != nil {
			return nil, fmt.Errorf("BRIDGE_DNS %q is not an IP address with an optional port", f)
		}
		if ap.Addr().IsUnspecified() || ap.Addr().IsMulticast() || ap.Addr().Is4() != exit.Is4() {
			return nil, fmt.Errorf("BRIDGE_DNS %q is not a unicast address of the same family as %s", f, netroles.EnvExit)
		}
		for _, b := range blocked {
			if netip.MustParsePrefix(b).Contains(ap.Addr()) {
				return nil, fmt.Errorf("BRIDGE_DNS %q is inside %s, which the bridge's exit refuses to reach", f, b)
			}
		}
	}
	return out, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)
	s, err := fromEnv(os.Getenv)
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer stop()
		err = run(ctx, log, s, nil)
	}
	if err != nil {
		log.Error("remote-nodes stopped", "err", err)
		os.Exit(1)
	}
}

// boundTo is the address a role's connections leave from: its own when an
// interface here holds it, and otherwise none, which leaves the choice to the
// system, as on a laptop or in a lab. The router sends each role's address
// where it should go, so on the router the address is always held.
func boundTo(a netroles.Addr) netip.Addr {
	if a.Local {
		return a.IP
	}
	return netip.Addr{}
}

// localProbe checks a Reality target from this machine, for the router's own
// entry: the same check a node's agent runs, dialled from the address the
// entry reaches its target from, name lookup included.
func localProbe(from netip.Addr) agentops.LocalProbe {
	dial := netbind.Dialer(from).DialContext
	return func(ctx context.Context, req *pb.RealityDestProbeRequest) (*pb.RealityDestProbeResponse, error) {
		r := realitydest.FromProtoRequest(req)
		r.Dial = dial
		report, err := realitydest.Probe(ctx, r)
		if err != nil {
			return nil, err
		}
		return realitydest.ReportToProto(report), nil
	}
}

// opsFor makes the node operations for a CA. Everything bound for a node leaves
// from the nodes' address, and the agent and Xray builds come from the holder of
// the downloads, which fetches them over the foreign link. The manager asks for
// the operations again when another CA takes its place, since the Env keeps the
// hub's certificate that it made from the one it was given.
func opsFor(nodes netip.Addr, files agentops.Files, log *slog.Logger, st *store.Store) func(*pki.CA) manager.Ops {
	return func(ca *pki.CA) manager.Ops {
		return manager.EnvOps(&agentops.Env{Local: nodes, CA: ca, Files: files, Log: log, LiveNode: liveNode(st)})
	}
}

// liveNode answers from the state whether a set-up node has an id, so that an
// install can tell a server that is already one of them under another address.
func liveNode(st *store.Store) func(uint) (string, bool) {
	return func(id uint) (string, bool) {
		if st == nil {
			return "", false
		}
		state := st.Read()
		n := state.FindNode(id)
		if n == nil || n.State != model.NodeRegistered {
			return "", false
		}
		return n.Name, true
	}
}

// connectorOf opens a node's agent with the operations the manager has for the
// CA it holds when asked, so that after a backup replaces the CA the next link
// is made under the new one. While the CA can't be used the answer is a plain
// error, and the node reads as one that can't be reached.
func connectorOf(ops func() (manager.Ops, error)) manager.Connector {
	return manager.ConnectorFunc(func(ctx context.Context, n model.Node) (manager.Client, error) {
		o, err := ops()
		if err != nil {
			return nil, err
		}
		return o.Connect(ctx, n)
	})
}

// counted is the state file with the usage file's writes added to its own.
// /health reports the sum, which is what a soak test reads the rate of flash
// writes from.
type counted struct {
	*store.Store
	usage *usage.Collector
}

func (c counted) Writes() uint64 { return c.Store.Writes() + c.usage.Writes() }

// logf is a log.Printf-style function that writes to a slog logger.
func logf(log *slog.Logger, level slog.Level, from string) func(string, ...any) {
	return func(format string, args ...any) {
		log.Log(context.Background(), level, fmt.Sprintf(format, args...), "from", from)
	}
}

// shutdownGrace is how long stopping the server, the jobs and the manager may
// take together. The container isn't given long.
const shutdownGrace = 5 * time.Second

// run serves until ctx ends, then stops everything in order. listening, if
// set, is told the address once the port is open.
func run(ctx context.Context, log *slog.Logger, s settings, listening func(net.Addr)) error {
	index, err := ui.New(s.basePath)
	if err != nil {
		return err
	}
	st, err := store.Open(s.dataDir)
	if err != nil {
		return err
	}
	coll, err := usage.Open(usage.Config{Dir: s.dataDir, Logf: logf(log, slog.LevelWarn, "usage")})
	if err != nil {
		return err
	}
	engine := jobs.New(jobs.Config{Log: log, StepKey: manager.StepKey, ErrorCode: manager.JobErrorCode, ErrorInfo: manager.JobErrorInfo})
	sup, err := bridgectl.New(bridgectl.Config{
		Binary:     s.bridgeBin,
		Credential: &bridgectl.Credential{UID: s.bridgeUID, GID: s.bridgeGID},
		Logf:       logf(log, slog.LevelInfo, "bridge"),
	})
	if err != nil {
		return err
	}
	addrs := s.addrs.Check(netroles.Held(), func(msg string) { log.Warn(msg) })
	downloads := artifacts.NewHolder(artifacts.NewClient(boundTo(addrs.Downloads)), s.release)
	// the connector asks the manager for the operations, and the manager is made with the connector
	var mgr *manager.Manager
	mgr, err = manager.New(manager.Config{
		Store: st, Usage: coll, Jobs: engine, Bridge: sup, Facts: routerfacts.NewReader(s.factsPath),
		Asks: routerfacts.NewAsks(s.factsPath), LocalProbe: localProbe(boundTo(addrs.Nodes)),
		Connector: connectorOf(func() (manager.Ops, error) { return mgr.Ops() }),
		NewOps:    opsFor(boundTo(addrs.Nodes), downloads, log, st),
		Downloads: downloads,
		Resolver:  addrs.Nodes.Resolver(),
		Addrs:     addrs, DNS: s.bridgeDNS,
		Log: log,
	})
	if err != nil {
		return err
	}
	handler := api.New(api.Config{
		Store: counted{st, coll}, Jobs: engine, Live: mgr, Version: version, Commit: commit,
		Subs: mgr, Backup: mgr, NodeOps: mgr, Local: mgr,
		BasePath: s.basePath, Index: index, Log: log,
		Licenses: os.DirFS(s.licensesDir),
	})
	ln, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	if listening != nil {
		listening(ln.Addr())
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	// the counters flush once more on their way out, so they have a context
	// of their own, which is ended after the manager has
	collCtx, stopColl := context.WithCancel(context.Background())
	collDone := make(chan struct{})
	go func() {
		defer close(collDone)
		coll.Run(collCtx)
	}()
	go mgr.Run(context.Background())

	log.Info("remote-nodes listening", "version", version, "commit", commit, "addr", ln.Addr().String(), "base", s.basePath, "data", s.dataDir,
		"bridge", s.bridgeBin, "facts", s.factsPath, "nodes_addr", addrs.Nodes.String(), "exit_addr", addrs.Exit.String(), "downloads_addr", addrs.Downloads.String())

	var serveErr error
	select {
	case serveErr = <-errc:
	case <-ctx.Done():
		log.Info("remote-nodes stopping")
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	stopErr := stopInOrder(sctx, log,
		// stop accepting requests, let those in flight finish, and end the jobs: an
		// event stream only ends with its job, so Shutdown waits on the engine
		stage{"requests and jobs", func(ctx context.Context) error {
			shut := make(chan error, 1)
			go func() { shut <- srv.Shutdown(ctx) }()
			jobsErr := engine.Close(ctx)
			return errors.Join(<-shut, jobsErr)
		}},
		stage{"manager", mgr.Close},
		stage{"usage counters", func(ctx context.Context) error {
			stopColl()
			select {
			case <-collDone:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}},
		// last, so the tunnels carry traffic for as long as the hub does
		stage{"bridge", func(context.Context) error { sup.Stop(); return nil }},
	)
	if serveErr == nil {
		serveErr = <-errc
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, stopErr)
}

// stage is one thing to stop.
type stage struct {
	name string
	stop func(context.Context) error
}

// stopInOrder stops each stage after the one before it has stopped. A stage
// that fails, or runs out of time, doesn't keep the later ones from stopping:
// each is logged and the failures are returned together.
func stopInOrder(ctx context.Context, log *slog.Logger, stages ...stage) error {
	var errs []error
	for _, st := range stages {
		if err := st.stop(ctx); err != nil {
			log.Error("stopping failed", "what", st.name, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", st.name, err))
		}
	}
	return errors.Join(errs...)
}
