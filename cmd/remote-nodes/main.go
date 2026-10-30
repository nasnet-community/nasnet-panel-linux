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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/ui"
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
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

func fromEnv(getenv func(string) string) (settings, error) {
	s := settings{
		listen:  orDefault(getenv("LISTEN_ADDR"), ":8080"),
		dataDir: orDefault(getenv("DATA_DIR"), "/data"),
	}
	if _, _, err := net.SplitHostPort(s.listen); err != nil {
		return settings{}, fmt.Errorf("LISTEN_ADDR %q: %w", s.listen, err)
	}
	var err error
	if s.basePath, err = ui.BasePath(getenv("BASE_PATH")); err != nil {
		return settings{}, err
	}
	return s, nil
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

// run serves until ctx ends, then lets requests in flight finish. listening,
// if set, is told the address once the port is open.
func run(ctx context.Context, log *slog.Logger, s settings, listening func(net.Addr)) error {
	st, err := store.Open(s.dataDir)
	if err != nil {
		return err
	}
	handler := api.New(api.Config{
		Store: st, Version: version, Commit: commit,
		BasePath: s.basePath, Index: ui.New(s.basePath), Log: log,
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
	log.Info("remote-nodes listening", "version", version, "commit", commit, "addr", ln.Addr().String(), "base", s.basePath, "data", s.dataDir)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("remote-nodes stopping")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
